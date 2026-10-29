// ISUCON app analyzer: crawl screens per persona and record every API request.
// Usage: node crawl.mjs [analyzer.config.json]
import { chromium } from "playwright";
import fs from "node:fs";
import path from "node:path";
import { pathToFileURL } from "node:url";
import { outRoot } from "./outdir.mjs";

const cfgPath = process.argv[2] ?? "analyzer.config.json";
const cfg = JSON.parse(fs.readFileSync(cfgPath, "utf8"));
const base = new URL(cfg.baseURL);
const ts = new Date().toISOString().replace(/[-:]/g, "").replace(/\..+/, "").replace("T", "-");
const outBase = outRoot(cfg, cfgPath); // main checkout's out/ even from a worktree (outdir.mjs)
const outDir = path.join(outBase, ts);
fs.mkdirSync(path.join(outDir, "shots"), { recursive: true });

const ignoreRe = (cfg.ignoreUrlPatterns ?? ["^/assets/", "\\.(png|jpe?g|svg|gif|ico|css|js|map|woff2?|ttf)(\\?|$)"]).map((p) => new RegExp(p));
const idRes = (cfg.idPatterns ?? ["[0-9A-HJKMNP-TV-Z]{26}", "[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}", "\\d{2,}"]).map((p) => new RegExp(p, "g"));
const apiPrefixes = cfg.apiPrefixes ?? ["/api/"];
const dwellMs = cfg.dwellMs ?? 4000;
const maxPages = cfg.maxPages ?? 40;
const navTimeout = cfg.navTimeoutMs ?? 15000;
const concurrency = cfg.concurrency ?? 3;       // pages crawled in parallel per persona
const settleMs = cfg.settleMs ?? 800;           // short wait for the first XHRs (networkidle never settles on polling pages)
const apiTimeoutMs = cfg.apiTimeoutMs ?? 15000;            // timeout for fetch() calls made from flows via the api() helper (makeApi); page-issued calls cannot be aborted
const maxInflightPerEndpoint = cfg.maxInflightPerEndpoint ?? 2; // extra concurrent calls to the same endpoint are aborted: one is enough to measure, more just fill the server's pool
const flowTimeoutMs = cfg.flowTimeoutMs ?? 120000;         // a flow running longer is cut; what was captured so far is reported
const totalBudgetMs = cfg.totalBudgetMs ?? 240000;         // per-persona page-crawl budget
// hosts that count as "the app" even though the origin differs (tenant/user subdomains): resolve keys + cfg.sameOriginHosts
const sameOriginRes = [...Object.keys(cfg.resolve ?? {}), ...(cfg.sameOriginHosts ?? [])].map((h) => new RegExp("^" + h.replace(/\./g, "\\.").replace(/\*/g, ".*") + "$", "i"));
const isSameOrigin = (u) => u.origin === base.origin || sameOriginRes.some((re) => re.test(u.hostname));
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

const normPath = (p) => idRes.reduce((s, re) => s.replace(re, ":id"), p);
const isIgnored = (p) => ignoreRe.some((re) => re.test(p));
const isApi = (p) => apiPrefixes.some((pre) => p.startsWith(pre));
const trunc = (s, n) => (s == null ? null : s.length > n ? s.slice(0, n) + `…(+${s.length - n})` : s);
const log = (...a) => console.error(...a);

// compact structural summary of a JSON value: {a: string, b: [3]{c: number}}
function schemaOf(v, depth = 0) {
  if (v === null) return "null";
  if (Array.isArray(v)) return `[${v.length}]` + (v.length && depth < 4 ? schemaOf(v[0], depth + 1) : "");
  if (typeof v === "object") {
    const keys = Object.keys(v);
    if (depth >= 4) return "{…}";
    return "{" + keys.slice(0, 14).map((k) => `${k}: ${schemaOf(v[k], depth + 1)}`).join(", ") + (keys.length > 14 ? `, …+${keys.length - 14}` : "") + "}";
  }
  return typeof v;
}
// fields that look like server-directed client behaviour (polling intervals etc.)
function hintsOf(v, depth = 0, out = []) {
  if (!v || typeof v !== "object" || depth > 2) return out;
  for (const [k, val] of Object.entries(v)) {
    if (/retry|interval|poll|wait|after|next|ttl|expire/i.test(k) && (typeof val === "number" || typeof val === "string")) out.push(`${k}=${val}`);
    if (val && typeof val === "object" && !Array.isArray(val)) hintsOf(val, depth + 1, out);
  }
  return out;
}

function attachRecorder(page) {
  const records = [];
  const t0 = Date.now();
  const onReq = (req) => {
    const u = new URL(req.url());
    const type = req.resourceType();
    if (u.origin !== base.origin) {
      // keep external calls (payment gateway etc.) but mark them
    }
    if (isIgnored(u.pathname) && !isApi(u.pathname)) return;
    if (!["xhr", "fetch", "eventsource", "document", "websocket", "other"].includes(type) && !isApi(u.pathname)) return;
    const rec = {
      t: Date.now() - t0,
      method: req.method(),
      origin: isSameOrigin(u) ? "" : u.origin,
      path: u.pathname,
      normPath: normPath(u.pathname),
      query: u.search ? Object.fromEntries(u.searchParams) : null,
      type,
      reqBody: trunc(req.postData(), 600),
      status: null,
      ms: null,
      resType: null,
      resBody: null,
      failed: null,
    };
    req.__rec = rec;
    records.push(rec);
  };
  const onRes = async (res) => {
    const req = res.request();
    const rec = req.__rec;
    if (!rec) return;
    rec.status = res.status();
    rec.resType = (res.headers()["content-type"] ?? "").split(";")[0];
    if (rec.resType.includes("json")) {
      try {
        const body = await res.text();
        rec.resBody = trunc(body, 800);
        try {
          const j = JSON.parse(body);
          rec.resSchema = schemaOf(j);
          rec.resHints = hintsOf(j);
          rec.resBytes = body.length;
        } catch {}
      } catch {}
    } else if (rec.resType.includes("event-stream")) {
      rec.resBody = "(SSE stream)";
    }
  };
  const onFail = (req) => {
    if (req.__rec) req.__rec.failed = req.__reason ?? req.failure()?.errorText ?? "failed";
  };
  const onDone = (req) => {
    const rec = req.__rec;
    if (!rec || rec.ms != null) return;
    try {
      const tm = req.timing();
      if (tm && tm.responseEnd > 0) rec.ms = Math.round(tm.responseEnd - Math.max(0, tm.requestStart >= 0 ? 0 : 0));
    } catch {}
  };
  // Cap concurrent calls per endpoint: a page that fires the same API 100x in a burst would fill the server's pool and stall
  // everything after it. The first maxInflightPerEndpoint go through untouched (native requests: cookies, timing, streaming
  // all intact); extras are aborted and recorded as "throttled". Slow calls are not aborted (Playwright cannot cancel a
  // continued request) but are recorded as "pending" at the end of the page/flow, and flows are cut by flowTimeoutMs.
  const inflight = new Map();
  const keyOf = (req) => `${req.method()} ${normPath(new URL(req.url()).pathname)}`;
  const routeHandler = async (route, req) => {
    const u = new URL(req.url());
    const type = req.resourceType();
    if (!isApi(u.pathname) || type === "eventsource" || type === "websocket" || type === "document") return route.continue();
    const k = keyOf(req);
    const n = inflight.get(k) ?? 0;
    if (n >= maxInflightPerEndpoint) { req.__reason = `throttled (>=${maxInflightPerEndpoint} in flight)`; return route.abort("blockedbyclient"); }
    inflight.set(k, n + 1); req.__counted = k;
    return route.continue();
  };
  const release = (req) => { if (req.__counted) { inflight.set(req.__counted, Math.max(0, (inflight.get(req.__counted) ?? 1) - 1)); req.__counted = null; } };
  page.route((url) => isApi(new URL(url).pathname), routeHandler);
  page.on("requestfinished", release);
  page.on("requestfailed", release);
  page.on("request", onReq);
  page.on("response", onRes);
  page.on("requestfailed", onFail);
  page.on("requestfinished", onDone);
  return {
    records,
    detach() {
      const now = Date.now() - t0;
      for (const r of records) if (r.status == null && !r.failed) r.failed = `pending >=${Math.round((now - r.t) / 1000)}s (no response before the page/flow ended)`;
      page.unroute((url) => isApi(new URL(url).pathname), routeHandler).catch(() => {});
      page.off("requestfinished", release);
      page.off("requestfailed", release);
      page.off("request", onReq);
      page.off("response", onRes);
      page.off("requestfailed", onFail);
      page.off("requestfinished", onDone);
    },
  };
}

// api(method, path, body): fetch from inside the page (cookies apply) with a timeout. Flows should use this instead of raw fetch.
function makeApi(page, timeoutMs) {
  return (method, path, body) =>
    page.evaluate(
      async ({ method, path, body, timeoutMs }) => {
        try {
          // retry a few times: the crawler throttles bursts of the same endpoint, and the page's own polling can take the slots
          for (let attempt = 0; ; attempt++) {
            try {
              const r = await fetch(path, { method, headers: { "content-type": "application/json" }, body: body ? JSON.stringify(body) : undefined, credentials: "include", signal: AbortSignal.timeout(timeoutMs) });
              let json = null;
              try { json = await r.json(); } catch {}
              return { status: r.status, json };
            } catch (e) {
              if (e && e.name === "TimeoutError") return { status: 0, json: null, error: `timeout >=${timeoutMs / 1000}s` };
              if (attempt >= 4) throw e;
              await new Promise((res) => setTimeout(res, 400 * (attempt + 1)));
            }
          }
        } catch (e) {
          return { status: 0, json: null, error: String(e) };
        }
      },
      { method, path, body, timeoutMs },
    );
}

async function extractPageInfo(page) {
  return page.evaluate((origin) => {
    const text = (el) => (el.textContent ?? "").trim().replace(/\s+/g, " ").slice(0, 80);
    const links = [...document.querySelectorAll("a[href]")]
      .map((a) => {
        try {
          const u = new URL(a.getAttribute("href"), location.href);
          return u.origin === origin ? { href: u.pathname + u.search, text: text(a) } : null;
        } catch {
          return null;
        }
      })
      .filter(Boolean);
    const forms = [...document.querySelectorAll("form")].map((f) => ({
      method: (f.getAttribute("method") ?? "GET").toUpperCase(),
      action: f.getAttribute("action"),
      fields: [...f.querySelectorAll("input,select,textarea")].map((i) => `${i.tagName.toLowerCase()}:${i.getAttribute("name") ?? i.id ?? "?"}${i.type ? `(${i.type})` : ""}`),
    }));
    const buttons = [...document.querySelectorAll("button,[role=button],input[type=submit]")].map(text).filter(Boolean);
    const headings = [...document.querySelectorAll("h1,h2,h3")].map((h) => `${h.tagName.toLowerCase()}: ${text(h)}`).slice(0, 20);
    const inputs = [...document.querySelectorAll("input,select,textarea")].map((i) => `${i.tagName.toLowerCase()}:${i.getAttribute("name") ?? i.id ?? i.placeholder ?? "?"}`).slice(0, 30);
    return { title: document.title, headings, links, forms, buttons: [...new Set(buttons)].slice(0, 30), inputs, bodyText: text(document.body).slice(0, 300) };
  }, base.origin);
}

function detectPolling(records) {
  const groups = new Map();
  for (const r of records) {
    const k = `${r.method} ${r.normPath}`;
    (groups.get(k) ?? groups.set(k, []).get(k)).push(r.t);
  }
  const out = [];
  for (const [k, ts] of groups) {
    if (ts.length < 3) continue;
    const gaps = ts.slice(1).map((t, i) => t - ts[i]).sort((a, b) => a - b);
    out.push({ endpoint: k, count: ts.length, medianGapMs: gaps[Math.floor(gaps.length / 2)], minGapMs: gaps[0] });
  }
  return out.sort((a, b) => b.count - a.count);
}

async function crawlPersona(browser, persona, idx) {
  const context = await browser.newContext({
    viewport: persona.viewport ?? cfg.viewport ?? { width: 1280, height: 800 },
    ignoreHTTPSErrors: true,
    userAgent: cfg.userAgent,
  });
  if (persona.cookies?.length) {
    await context.addCookies(persona.cookies.map((c) => ({ ...c, url: cfg.baseURL })));
  }
  if (persona.headers) await context.setExtraHTTPHeaders(persona.headers);
  // persona.login = { path: "/api/login", method: "POST", body: {...}, headers: {...} } — done in-page so Set-Cookie applies to this context
  if (persona.login?.path) {
    const page = await context.newPage();
    try {
      await page.goto(cfg.baseURL, { waitUntil: "domcontentloaded", timeout: navTimeout });
      const r = await page.evaluate(async ({ path, method, body, headers }) => {
        const res = await fetch(path, { method: method ?? "POST", headers: { "content-type": "application/json", ...(headers ?? {}) }, body: body ? JSON.stringify(body) : undefined, credentials: "include" });
        return { status: res.status, text: (await res.text()).slice(0, 200) };
      }, persona.login);
      log(`[${persona.name}] login ${persona.login.method ?? "POST"} ${persona.login.path} -> ${r.status}${r.status >= 300 ? " " + r.text : ""}`);
      persona.loginResult = r;
    } catch (e) {
      log(`[${persona.name}] login failed: ${String(e.message ?? e).split("\n")[0]}`);
    }
    await page.close();
  }

  const pages = [];
  const visited = new Set();
  const queue = [...(persona.seeds ?? ["/"])];
  let n = 0, inflight = 0;
  const t0 = Date.now();
  const crawlOne = async (target) => {
    const key = normPath(target.split("#")[0]);
    const page = await context.newPage();
    const rec = attachRecorder(page);
    const entry = { url: target, normPath: key, finalUrl: null, screenshot: null, error: null };
    const idx = n++;
    try {
      await page.goto(new URL(target, cfg.baseURL).toString(), { waitUntil: "domcontentloaded", timeout: navTimeout });
      await page.waitForLoadState("networkidle", { timeout: settleMs }).catch(() => {});
      await page.waitForTimeout(dwellMs);
      entry.finalUrl = new URL(page.url()).pathname + new URL(page.url()).search;
      Object.assign(entry, await extractPageInfo(page));
      const shot = `shots/${persona.name}-${String(idx).padStart(2, "0")}.png`;
      await page.screenshot({ path: path.join(outDir, shot), fullPage: false, scale: "css" });
      entry.screenshot = shot;
      for (const l of entry.links) {
        const p = l.href.split("#")[0];
        if (!isIgnored(p) && !isApi(p) && !visited.has(normPath(p)) && !queue.includes(p)) queue.push(p);
      }
    } catch (e) {
      entry.error = String(e.message ?? e).split("\n")[0];
    }
    rec.detach();
    entry.requests = rec.records;
    entry.polling = detectPolling(rec.records);
    pages.push(entry);
    log(`[${persona.name}] ${target} -> ${entry.finalUrl ?? "?"} (${rec.records.length} req${entry.error ? ", ERROR " + entry.error : ""}) +${((Date.now() - t0) / 1000).toFixed(1)}s`);
    await page.close();
  };
  const worker = async () => {
    for (;;) {
      if (pages.length + inflight >= maxPages) return;
      if (Date.now() - t0 > totalBudgetMs) { if (queue.length) { log(`[${persona.name}] crawl budget ${totalBudgetMs / 1000}s reached; ${queue.length} page(s) skipped`); queue.length = 0; } return; }
      const target = queue.shift();
      if (target == null) { if (inflight === 0) return; await sleep(150); continue; }
      const key = normPath(target.split("#")[0]);
      if (visited.has(key)) continue;
      visited.add(key);
      inflight++;
      try { await crawlOne(target); } finally { inflight--; }
    }
  };
  await Promise.all(Array.from({ length: concurrency }, worker));
  pages.sort((a, b) => a.screenshot?.localeCompare(b.screenshot ?? "") ?? 0);

  // Optional scripted flows: files exporting `default async (page, { persona, cfg }) => {}`
  const flows = [];
  for (const f of persona.flows ?? []) {
    const page = await context.newPage();
    const rec = attachRecorder(page);
    const entry = { name: f.name, script: f.script, error: null, steps: [] };
    try {
      const mod = await import(pathToFileURL(path.resolve(f.script)).href);
      const tFlow = Date.now();
      const step = async (label) => {
        const shot = `shots/${persona.name}-flow-${f.name}-${String(entry.steps.length).padStart(2, "0")}.png`;
        try {
          await page.waitForTimeout(f.stepShotDelayMs ?? 700);
          await page.screenshot({ path: path.join(outDir, shot), scale: "css" });
        } catch {}
        entry.steps.push({ t: rec.records.length, label, screenshot: shot });
        log(`  [${persona.name}/${f.name} +${((Date.now() - tFlow) / 1000).toFixed(1)}s] step: ${label}`);
      };
      let timer;
      await Promise.race([
        mod.default(page, { persona, cfg, step, api: makeApi(page, apiTimeoutMs) }),
        new Promise((_, rej) => { timer = setTimeout(() => rej(new Error(`flow timeout after ${(f.timeoutMs ?? flowTimeoutMs) / 1000}s at step ${entry.steps.length} (${entry.steps.at(-1)?.label ?? "start"}); the captured part is reported`)), f.timeoutMs ?? flowTimeoutMs); }),
      ]).finally(() => clearTimeout(timer));
      await step("end");
      await page.waitForTimeout(f.dwellMs ?? dwellMs);
      const shot = `shots/${persona.name}-flow-${f.name}.png`;
      await page.screenshot({ path: path.join(outDir, shot), scale: "css" });
      entry.screenshot = shot;
    } catch (e) {
      entry.error = String(e.message ?? e).split("\n")[0];
    }
    rec.detach();
    entry.requests = rec.records;
    entry.polling = detectPolling(rec.records);
    flows.push(entry);
    log(`[${persona.name}] flow ${f.name}: ${rec.records.length} req${entry.error ? ", ERROR " + entry.error : ""}`);
    await page.close();
  }
  await context.close();
  return { name: persona.name, pages, flows };
}

async function scanFrontendCode(browser, observedSegments) {
  const context = await browser.newContext({ ignoreHTTPSErrors: true });
  const refs = { scripts: [], endpoints: new Map(), setIntervalCount: 0, hasEventSource: false, hasWebSocket: false, intervalLiterals: [], timeoutLiterals: [] };
  const segs = new Set([...(cfg.apiSegments ?? []), ...observedSegments]);
  try {
    const html = await (await context.request.get(cfg.baseURL)).text();
    const srcs = [...html.matchAll(/(?:src|href)="([^"]+\.js[^"]*)"/g)].map((m) => m[1]);
    const seen = new Set();
    const queue = [...srcs];
    while (queue.length && seen.size < 40) {
      const s = queue.shift();
      const u = new URL(s, cfg.baseURL);
      if (u.origin !== base.origin || seen.has(u.pathname)) continue;
      seen.add(u.pathname);
      const res = await context.request.get(u.toString());
      if (!res.ok()) continue;
      const js = await res.text();
      refs.scripts.push({ path: u.pathname, bytes: js.length });
      // path-like string literals: "/api/..." or "/<known-segment>/..." (API base URL is often a variable)
      for (const m of js.matchAll(/["'`](\/[A-Za-z0-9_\-]+(?:\/[A-Za-z0-9_\-{}$.:]+)*)["'`]/g)) {
        let p = m[1];
        const first = p.split("/")[1];
        const api = apiPrefixes.some((pre) => p.startsWith(pre)) || segs.has(first);
        if (!api) continue;
        if (!apiPrefixes.some((pre) => p.startsWith(pre))) p = (apiPrefixes[0] ?? "/") + p.slice(1);
        p = p.replace(/\$\{[^}]*\}/g, ":id").replace(/\{[^}]*\}/g, ":id");
        refs.endpoints.set(p, (refs.endpoints.get(p) ?? 0) + 1);
      }
      refs.setIntervalCount += (js.match(/setInterval\(/g) ?? []).length;
      if (/new EventSource\(/.test(js)) refs.hasEventSource = true;
      if (/new WebSocket\(/.test(js)) refs.hasWebSocket = true;
      for (const m of js.matchAll(/setInterval\([^,]{1,120},\s*(\d{2,6})\)/g)) refs.intervalLiterals.push(Number(m[1]));
      for (const m of js.matchAll(/setTimeout\([^,]{1,120},\s*(\d{2,6})\)/g)) refs.timeoutLiterals.push(Number(m[1]));
      for (const m of js.matchAll(/import\(["']([^"']+\.js)["']\)/g)) queue.push(m[1]);
      for (const m of js.matchAll(/["'](\.?\/assets\/[^"']+\.js)["']/g)) queue.push(m[1]);
    }
  } catch (e) {
    refs.error = String(e.message ?? e);
  }
  await context.close();
  return { ...refs, endpoints: [...refs.endpoints].map(([p, c]) => ({ path: p, refs: c })).sort((a, b) => a.path.localeCompare(b.path)) };
}

// cfg.resolve = {"*.u.isucon.local": "18.183.49.79"} → no /etc/hosts needed (Chromium host-resolver-rules)
const resolverArgs = cfg.resolve && Object.keys(cfg.resolve).length ? [`--host-resolver-rules=${Object.entries(cfg.resolve).map(([h, ip]) => `MAP ${h} ${ip}`).join(", ")}`] : [];
const browser = await chromium.launch({ headless: cfg.headless ?? true, args: resolverArgs });
const tStart = Date.now();
// cfg.initialize = "/api/initialize": call it once before crawling. A host that never served a bench may have an empty DB
// (ISUCON13's svc2 returned 401 for the initial user until /initialize ran). Do NOT point this at the bench target mid-contest.
// cfg.initializeBody (default {}) is the JSON body: some apps read settings from it (ISUCON14: payment_server).
if (cfg.initialize) {
  const ctx = await browser.newContext({ ignoreHTTPSErrors: true });
  const pg = await ctx.newPage();
  const t = Date.now();
  try {
    await pg.goto(cfg.baseURL, { waitUntil: "domcontentloaded", timeout: navTimeout });
    const r = await pg.evaluate(async ({ path, body, ms }) => { try { const res = await fetch(path, { method: "POST", headers: { "content-type": "application/json" }, body, signal: AbortSignal.timeout(ms) }); return { status: res.status, text: (await res.text()).slice(0, 120) }; } catch (e) { return { status: 0, text: String(e) }; } }, { path: cfg.initialize, body: JSON.stringify(cfg.initializeBody ?? {}), ms: cfg.initializeTimeoutMs ?? 120000 });
    log(`initialize POST ${cfg.initialize} -> ${r.status} in ${((Date.now() - t) / 1000).toFixed(1)}s${r.status >= 300 || r.status === 0 ? " " + r.text : ""}`);
  } catch (e) { log(`initialize failed: ${String(e.message ?? e).split("\n")[0]}`); }
  await ctx.close();
}
const personas = await Promise.all((cfg.personas ?? [{ name: "guest", seeds: ["/"] }]).map((p, i) => crawlPersona(browser, p, i)));
log(`crawl finished in ${((Date.now() - tStart) / 1000).toFixed(1)}s`);
const observedSegments = new Set();
for (const p of personas) for (const pg of [...p.pages, ...p.flows]) for (const r of pg.requests) if (isApi(r.path)) observedSegments.add(r.path.replace(/^\/api\//, "").split("/")[0]);
const frontendRefs = await scanFrontendCode(browser, observedSegments);
// optional: OpenAPI spec for the "known endpoints" list
let openapi = null;
if (cfg.openapiPath && fs.existsSync(cfg.openapiPath)) {
  const txt = fs.readFileSync(cfg.openapiPath, "utf8");
  const ops = [];
  let cur = null;
  for (const line of txt.split("\n")) {
    const mp = line.match(/^  (\/[^:]+):\s*$/);
    if (mp) { cur = mp[1]; continue; }
    const mm = line.match(/^    (get|post|put|patch|delete):\s*$/);
    if (mm && cur) ops.push({ method: mm[1].toUpperCase(), path: ((cfg.openapiPrefix ?? "") + cur).replace(/\{[^}]+\}/g, ":id") });
  }
  openapi = { path: cfg.openapiPath, operations: ops };
}
await browser.close();

const data = { generatedAt: new Date().toISOString(), baseURL: cfg.baseURL, config: path.resolve(cfgPath), personas, frontendRefs, openapi };
fs.writeFileSync(path.join(outDir, "data.json"), JSON.stringify(data, null, 2));
fs.writeFileSync(path.join(outBase, "latest"), ts);
log(`done -> ${outDir}/data.json`);
console.log(outDir);
