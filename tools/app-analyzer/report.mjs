// Build report.md (for Claude) and report.html (single-file interactive app) from a crawl output dir.
// Usage: node report.mjs [crawl dir] [config.json] [--no-inline]   (default dir: <outDir>/<latest>, see outdir.mjs)
import fs from "node:fs";
import path from "node:path";
import { execSync } from "node:child_process";
import { loadBench, resolveRunDir, tableUse, tableUseFromCode, addedIndexes, routesFromCode } from "./bench.mjs";
import { renderHtml, screenWeights, withNow, relLink } from "./html.mjs";
import { latestOut } from "./outdir.mjs";

const argv = process.argv.slice(2);
const argDir = argv.find((a, i) => !a.startsWith("--") && argv[i - 1] !== "--run" && !a.endsWith(".json"));
const argCfg = argv.find((a) => a.endsWith(".json")) ?? "analyzer.config.json";
const dir = argDir ?? latestOut(fs.existsSync(argCfg) ? JSON.parse(fs.readFileSync(argCfg, "utf8")) : {}, argCfg);
if (!dir) { console.error("no crawl output: run the crawl first (out/latest not found; see outDir in README)"); process.exit(1); }
const data = JSON.parse(fs.readFileSync(path.join(dir, "data.json"), "utf8"));
const cfgFile = data.config && fs.existsSync(data.config) ? JSON.parse(fs.readFileSync(data.config, "utf8")) : {};
const apiPrefix = (cfgFile.apiPrefixes ?? ["/api/"])[0];
const actorNames = cfgFile.actorNames ?? {};

// ---- route-table normalization: /api/user/mtanaka1/icon -> /api/user/:id/icon when the app's router has that pattern ----
// (the crawler only knows numeric/ULID-looking ids; usernames and slugs need the router)
const routePatterns = [];
if (cfgFile.codeDir && fs.existsSync(cfgFile.codeDir)) {
  const walk = (d) => { for (const f of fs.readdirSync(d, { withFileTypes: true })) { if (f.isDirectory()) { if (!/node_modules|vendor|\.git/.test(f.name)) walk(path.join(d, f.name)); } else if (f.name.endsWith(".go") && !f.name.endsWith("_test.go")) { const src = fs.readFileSync(path.join(d, f.name), "utf8"); for (const m of src.matchAll(/HandleFunc\("(\w+) ([^"]+)"/g)) routePatterns.push(m[2]); for (const m of src.matchAll(/\.(?:Get|Post|Put|Delete|Patch|GET|POST|PUT|DELETE|PATCH)\("([^"]+)",/g)) routePatterns.push(m[1]); } } };
  walk(cfgFile.codeDir);
}
const routeSegs = [...new Set(routePatterns)].map((p) => p.split("/").map((seg) => (/^(:\w+|\{[^}]+\}|\*)$/.test(seg) ? ":id" : seg)));
const normByRoutes = (p) => {
  const segs = p.split("?")[0].split("/");
  let best = null;
  for (const rs of routeSegs) {
    if (rs.length !== segs.length) continue;
    let ok = true, lit = 0;
    for (let i = 0; i < rs.length; i++) { if (rs[i] === ":id") continue; if (rs[i] !== segs[i]) { ok = false; break; } lit++; }
    if (ok && (!best || lit > best.lit)) best = { lit, path: rs.join("/") };
  }
  return best?.path ?? null;
};
const detectPolling = (records) => {
  const groups = new Map();
  for (const r of records) { const k = `${r.method} ${r.normPath}`; (groups.get(k) ?? groups.set(k, []).get(k)).push(r.t); }
  const out = [];
  for (const [k, ts] of groups) { if (ts.length < 3) continue; const gaps = ts.slice(1).map((t, i) => t - ts[i]).sort((a, b) => a - b); out.push({ endpoint: k, count: ts.length, medianGapMs: gaps[Math.floor(gaps.length / 2)], minGapMs: gaps[0] }); }
  return out.sort((a, b) => b.count - a.count);
};
if (routeSegs.length) for (const p of data.personas) for (const pg of [...p.pages, ...(p.flows ?? [])]) { for (const r of pg.requests ?? []) { const n = normByRoutes(r.path); if (n) r.normPath = n; } pg.polling = detectPolling(pg.requests ?? []); }

const esc = (s) => String(s ?? "").replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;");
const code = (s) => (s == null || s === "" ? "" : "`" + String(s).replace(/`/g, "'") + "`");
const fmtQuery = (q) => (q ? Object.entries(q).map(([k, v]) => `${k}=${v}`).join("&") : "");
const median = (a) => (a.length ? [...a].sort((x, y) => x - y)[Math.floor(a.length / 2)] : null);
const isApiReq = (r) => r.type !== "document" || r.path.startsWith(apiPrefix);
const epKey = (r) => `${r.method} ${r.origin || ""}${r.normPath}`;
const actorOf = (p) => {
  const seg = p.startsWith(apiPrefix) ? p.slice(apiPrefix.length).split("/")[0] : null;
  return seg ? actorNames[seg] ?? seg : "-";
};

// ---- screens (routes + flows) ----
const screens = [];
for (const p of data.personas) {
  for (const pg of p.pages) screens.push({ persona: p.name, kind: "screen", label: pg.finalUrl ?? pg.url, ...pg });
  for (const fl of p.flows ?? []) screens.push({ persona: p.name, kind: "flow", label: `flow:${fl.name}`, url: `flow:${fl.name}`, ...fl });
}
screens.forEach((s, i) => (s.id = String(i)));
{
  const seen = new Set();
  for (const s of screens.filter((x) => x.kind === "screen")) {
    const k = `${s.persona}|${(s.finalUrl ?? s.url).split("?")[0]}`;
    if (seen.has(k)) s.revisit = true;
    seen.add(k);
  }
}

// ---- error classification: crawl artefacts vs app-origin ----
const errClass = (r, persona) => {
  if (!(r.failed || (r.status ?? 0) >= 400)) return null;
  const msg = r.resBody ?? "";
  if (r.failed || r.status == null) return "artefact:aborted";
  if ((r.status === 401 || r.status === 403) && (persona === "guest" || /cookie|session|unauthori|token|login|forbidden/i.test(msg))) return "artefact:auth";
  if (r.status === 409 || /duplicate|already exist|conflict/i.test(msg)) return "artefact:rerun";
  return "app";
};
for (const s of screens) for (const r of s.requests ?? []) r.errClass = errClass(r, s.persona);

// ---- polling ----
const repeatedAll = screens.flatMap((s) => (s.polling ?? []).map((p) => ({ screen: `${s.persona}:${s.label}`, screenId: s.id, ...p }))).sort((a, b) => a.medianGapMs - b.medianGapMs);
const pollingAll = repeatedAll.filter((p) => p.endpoint.startsWith("GET "));   // polling = repeated reads
const repeatedWrites = repeatedAll.filter((p) => !p.endpoint.startsWith("GET "));
const pollEps = new Set(pollingAll.map((p) => p.endpoint));

// ---- endpoints ----
const endpoints = new Map();
let totalApiReqs = 0;
for (const s of screens) {
  for (const r of s.requests ?? []) {
    if (!isApiReq(r)) continue;
    totalApiReqs++;
    const key = epKey(r);
    const e = endpoints.get(key) ?? endpoints.set(key, { method: r.method, path: `${r.origin || ""}${r.normPath}`, count: 0, screens: new Set(), screenIds: new Set(), personas: new Set(), ms: [], bytes: [], statuses: new Map(), samples: new Map(), sampleQuery: null, hints: new Set(), errors: { app: 0, artefact: 0, samples: [] } }).get(key);
    e.count++;
    e.screens.add(`${s.persona}:${s.label}`);
    e.screenIds.add(s.id);
    e.personas.add(s.persona);
    if (r.ms != null) e.ms.push(r.ms);
    if (r.resBytes != null) e.bytes.push(r.resBytes);
    const st = r.status ?? (r.failed ? "failed" : "pending");
    e.statuses.set(st, (e.statuses.get(st) ?? 0) + 1);
    if (!e.samples.has(st) || (!e.samples.get(st).res && r.resBody)) e.samples.set(st, { req: r.reqBody, res: r.resBody, schema: r.resSchema, query: r.query ? fmtQuery(r.query) : null });
    if (!e.sampleQuery && r.query) e.sampleQuery = fmtQuery(r.query);
    for (const hh of r.resHints ?? []) e.hints.add(hh);
    if (r.errClass) {
      if (r.errClass === "app") e.errors.app++; else e.errors.artefact++;
      if (e.errors.samples.length < 3) e.errors.samples.push({ cls: r.errClass, status: st, msg: (r.resBody ?? "").slice(0, 160), screen: `${s.persona}:${s.label}` });
    }
  }
}
const roleOf = (e) => {
  const p = e.path;
  if (p.includes("/internal/")) return "internal";
  if (pollEps.has(`${e.method} ${e.path}`)) return "polling";
  if (/notification|notify|\/events?$/.test(p)) return "notification";
  if (e.method !== "GET") {
    if (/initialize/.test(p)) return "initialize";
    if (/status$/.test(p)) return "state transition";
    if (/coordinate|location|position/.test(p)) return "position report";
    if (/evaluation|review|rating/.test(p)) return "evaluate";
    if (/users$|owners$|chairs$|register|signup|login/.test(p)) return "register/login";
    if (/estimate|preview|calc/.test(p)) return "calc (read)";
    return "write";
  }
  if (e.sampleQuery && /since|until|from|to|start|end|date/.test(e.sampleQuery)) return "aggregate (range)";
  if (/sales|stats|summary|ranking|report/.test(p)) return "aggregate";
  if (e.sampleQuery || /nearby|search|list/.test(p)) return "list/search";
  return "read";
};
const epList = [...endpoints.values()].map((e) => {
  const sorted = [...e.statuses].sort((a, b) => b[1] - a[1]);
  const pref = sorted.find(([st]) => typeof st === "number" && st < 400) ?? sorted[0];
  const sample = e.samples.get(pref?.[0]) ?? {};
  return { ...e, actor: actorOf(e.path), kind: e.method === "GET" ? "read" : "write", role: roleOf(e), medianMs: median(e.ms), medianBytes: median(e.bytes), sampleStatus: pref?.[0], sampleReq: sample.req ?? null, sampleRes: sample.res ?? null, sampleSchema: sample.schema ?? null, allSamples: [...e.samples].map(([st, v]) => ({ status: st, ...v })) };
}).sort((a, b) => b.count - a.count);
const observedKeys = new Set(epList.map((e) => `${e.method} ${e.path}`));

// ---- OpenAPI / frontend refs diff ----
const screenPaths = new Set(screens.filter((s) => s.kind === "screen").map((s) => (s.finalUrl ?? s.url ?? "").split("?")[0]));
const noteKeyOf = (o) => (o.path.includes("/internal/") ? "internal" : /initialize/.test(o.path) ? "initialize" : o.method !== "GET" && /users$|owners$|chairs$|register|signup/.test(o.path) ? "register" : o.method !== "GET" ? "write" : "read");
const annotateOp = (o) => {
  const p = o.path;
  if (p.includes("/internal/")) return "internal path, never called by the frontend → likely a server-side batch / cron / systemd timer. Find who calls it, how often and how many items per call.";
  if (/initialize/.test(p)) return "called once by the benchmarker before each run (reset data). Has a time limit; keep it fast if you add precomputation.";
  if (o.method !== "GET" && /users$|owners$|chairs$|register|signup/.test(p)) return "registration; probably called by the benchmarker during setup / user growth.";
  if (o.method !== "GET") return "write API not reached by the crawl; exercise it with a flow if it matters.";
  return "not reached by the crawl (behind a click or a state the crawl did not produce).";
};
const opKeys = new Set();
const unobservedOps = (data.openapi?.operations ?? []).filter((o) => { const k = `${o.method} ${o.path}`; if (opKeys.has(k)) return false; opKeys.add(k); return !observedKeys.has(k); }).map((o) => ({ ...o, actor: actorOf(o.path), note: annotateOp(o), noteKey: noteKeyOf(o) }));
const unobserved = (data.frontendRefs?.endpoints ?? []).filter((e) => ![...observedKeys].some((k) => k.endsWith(" " + e.path))).filter((e) => !screenPaths.has(e.path.replace(new RegExp("^" + apiPrefix.replace(/\/$/, "")), "")));

// ---- storyboards: state timeline + transitions ----
const stateKeys = cfgFile.stateKeys ?? ["status", "state", "phase"];
const findState = (obj, depth = 0) => {
  if (!obj || typeof obj !== "object" || depth > 4) return null;
  for (const k of stateKeys) if (typeof obj[k] === "string") return { key: k, value: obj[k] };
  for (const v of Object.values(obj)) { const r = findState(v, depth + 1); if (r) return r; }
  return null;
};
const storyboards = screens.filter((s) => s.kind === "flow" && (s.steps ?? []).length).map((s) => {
  const all = s.requests ?? [];
  const steps = s.steps;
  const parts = steps.map((st, i) => {
    const end = i + 1 < steps.length ? steps[i + 1].t : all.length;
    const calls = new Map();
    for (const r of all.slice(st.t, end)) {
      if (!isApiReq(r)) continue;
      const k = `${r.method} ${r.normPath} → ${r.status ?? r.failed ?? "?"}`;
      const c = calls.get(k) ?? calls.set(k, { n: 0, polling: pollEps.has(`${r.method} ${r.normPath}`), cls: r.errClass }).get(k);
      c.n++;
    }
    const tStart = all[st.t]?.t ?? (i === 0 ? 0 : all[all.length - 1]?.t ?? 0);
    const tEnd = all[end - 1]?.t ?? tStart;
    return { label: st.label, screenshot: st.screenshot, tStart, tEnd, calls: [...calls].map(([k, c]) => ({ text: c.n > 1 ? `${k} ×${c.n}` : k, polling: c.polling, cls: c.cls })) };
  });
  const states = [];
  for (const r of all) {
    if (!r.resBody || !r.resBody.startsWith("{")) continue;
    let j = null;
    try { j = JSON.parse(r.resBody.replace(/…\(\+\d+\)$/, "")); } catch { continue; }
    const st = findState(j);
    if (!st) continue;
    const last = states[states.length - 1];
    if (last && last.value === st.value && last.key === st.key) last.to = r.t;
    else states.push({ key: st.key, value: st.value, from: r.t, to: r.t, endpoint: `${r.method} ${r.normPath}`, actor: actorOf(r.normPath) });
  }
  for (let i = 0; i < states.length - 1; i++) states[i].to = states[i + 1].from;
  const transitions = [];
  for (let i = 1; i < states.length; i++) {
    const t = states[i].from;
    const cands = all.filter((r) => isApiReq(r) && r.t <= t && t - r.t <= 5000 && !pollEps.has(`${r.method} ${r.normPath}`) && !r.errClass);
    const write = [...cands].reverse().find((r) => r.method !== "GET");
    const trig = write ?? [...cands].reverse()[0];
    transitions.push({ from: states[i - 1].value, to: states[i].value, trigger: trig ? `${trig.method} ${trig.normPath}` : null, actor: trig ? actorOf(trig.normPath) : "server", observedVia: states[i].endpoint, t });
  }
  parts.forEach((p) => { p.stateChanges = states.filter((st) => st.from > p.tStart && st.from <= p.tEnd).map((st) => st.value); });
  return { screenId: s.id, persona: s.persona, label: s.label, parts, states, transitions, stateKeys, tEnd: all[all.length - 1]?.t ?? 0, actors: [...new Set(all.filter(isApiReq).map((r) => actorOf(r.normPath)).filter((a) => a !== "-"))] };
});

// ---- navigation ----
const navEdges = new Map();
const nodeOf = (p) => p.split("?")[0];
for (const s of screens.filter((x) => x.kind === "screen")) {
  const from = nodeOf(s.finalUrl ?? s.url);
  for (const l of s.links ?? []) {
    const to = nodeOf(l.href);
    if (to === from) continue;
    const k = `${from}|${to}`;
    const e = navEdges.get(k) ?? navEdges.set(k, { from, to, labels: new Set(), persona: s.persona }).get(k);
    if (l.text) e.labels.add(l.text);
  }
}
const redirects = screens.filter((x) => x.kind === "screen" && x.finalUrl && nodeOf(x.url) !== nodeOf(x.finalUrl)).map((x) => ({ from: nodeOf(x.url), to: nodeOf(x.finalUrl), persona: x.persona }));
const nodeMap = new Map();
for (const s of screens.filter((x) => x.kind === "screen")) {
  const p = nodeOf(s.finalUrl ?? s.url);
  if (!p) continue;
  const nd = nodeMap.get(p) ?? nodeMap.set(p, { path: p, personas: [], screenIds: [], actors: new Set() }).get(p);
  if (!nd.personas.includes(s.persona)) nd.personas.push(s.persona);
  nd.screenIds.push(s.id);
  for (const r of (s.requests ?? []).filter(isApiReq)) nd.actors.add(actorOf(r.normPath));
}
const navModel = {
  nodes: [...nodeMap.values()].map((nd) => ({ ...nd, actors: [...nd.actors].filter((a) => a !== "-") })),
  edges: [...[...navEdges.values()].map((e) => ({ from: e.from, to: e.to, label: [...e.labels][0] ?? "", type: "link" })), ...redirects.map((r) => ({ from: r.from, to: r.to, label: `redirect (${r.persona})`, type: "redirect" }))],
};

// ---- optional: schema (indexes) ----
let schema = null;
{
  let schemaPath = cfgFile.schemaPath;
  if (!schemaPath && cfgFile.codeDir) {
    const cand = [path.join(cfgFile.codeDir, "..", "sql"), path.join(cfgFile.codeDir, "sql")].filter((d) => fs.existsSync(d));
    for (const d of cand) { const f = fs.readdirSync(d).find((x) => /schema.*\.sql$/i.test(x)); if (f) { schemaPath = path.join(d, f); break; } }
  }
  if (schemaPath && fs.existsSync(schemaPath)) {
    schema = { path: schemaPath, tables: {} };
    const sql = fs.readFileSync(schemaPath, "utf8");
    for (const m of sql.matchAll(/CREATE TABLE\s+(?:IF NOT EXISTS\s+)?`?(\w+)`?\s*\(([\s\S]*?)\n\s*\)/gi)) {
      const t = { columns: [], indexes: [] };
      for (const raw of m[2].split("\n")) {
        const ln = raw.trim().replace(/,$/, "");
        if (!ln || ln.startsWith("--")) continue;
        let im;
        if ((im = ln.match(/^PRIMARY KEY\s*\(([^)]+)\)/i))) t.indexes.push({ name: "PRIMARY", cols: im[1].split(",").map((c) => c.trim().replace(/`/g, "")) });
        else if ((im = ln.match(/^(?:UNIQUE(?:\s+KEY|\s+INDEX)?|KEY|INDEX)\s*`?(\w*)`?\s*\(([^)]+)\)/i))) t.indexes.push({ name: im[1] || "idx", cols: im[2].split(",").map((c) => c.trim().replace(/`/g, "").replace(/\(\d+\)$/, "")) });
        else if ((im = ln.match(/^`?(\w+)`?\s+([A-Za-z]+(?:\([^)]*\))?)/))) if (!/^(CONSTRAINT|FOREIGN|CHECK|PRIMARY|UNIQUE|KEY|INDEX)$/i.test(im[1])) {
          t.columns.push({ name: im[1], type: im[2].toUpperCase(), nullable: !/NOT NULL/i.test(ln), auto: /AUTO_INCREMENT/i.test(ln) });
          if (/\bPRIMARY KEY\b/i.test(ln)) t.indexes.push({ name: "PRIMARY", cols: [im[1]] });        // inline: `id BIGINT ... PRIMARY KEY`
          else if (/\bUNIQUE\b/i.test(ln)) t.indexes.push({ name: "uniq_" + im[1], cols: [im[1]] }); // inline UNIQUE
        }
      }
      schema.tables[m[1]] = t;
    }
  }
}

// ---- optional: frontend route meta (cfg.frontendDir) ----
const routeMeta = {};
if (cfgFile.frontendDir && fs.existsSync(cfgFile.frontendDir)) {
  const files = [];
  const walk = (d) => { for (const f of fs.readdirSync(d, { withFileTypes: true })) { if (f.isDirectory()) { if (!/node_modules|\.git|build|dist|public/.test(f.name)) walk(path.join(d, f.name)); } else if (/\.(tsx?|jsx?|vue|svelte)$/.test(f.name)) files.push(path.join(d, f.name)); } };
  walk(cfgFile.frontendDir);
  const routeOf = (file) => {
    let rel = path.relative(cfgFile.frontendDir, file).replace(/\\/g, "/");
    const m = rel.match(/(?:^|\/)(routes|pages|app)\/(.*)$/);
    if (!m) return null;
    let r = m[2].replace(/\/(route|page|index)\.(tsx?|jsx?|vue|svelte)$/, "").replace(/\.(tsx?|jsx?|vue|svelte)$/, "");
    if (m[1] === "routes") { // Remix flat routes
      r = r.replace(/_\./g, ".").split(".").filter((seg) => seg && seg !== "_index" && !seg.startsWith("_")).join("/");
    } else {
      r = r.split("/").filter((seg) => seg && seg !== "index" && !/^\(.*\)$/.test(seg)).map((seg) => seg.replace(/^\[.*\]$/, ":id")).join("/");
    }
    return "/" + r;
  };
  for (const f of files) {
    const route = routeOf(f);
    if (route == null) continue;
    const src = fs.readFileSync(f, "utf8");
    const title = src.match(/title:\s*["'`]([^"'`]+)["'`]/)?.[1] ?? null;
    const desc = src.match(/name:\s*["']description["'],\s*content:\s*["'`]([^"'`]+)["'`]/)?.[1] ?? src.match(/description:\s*["'`]([^"'`]+)["'`]/)?.[1] ?? null;
    if (title || desc) routeMeta[route] = { file: path.relative(cfgFile.frontendDir, f), title, desc };
  }
}

// ---- optional: map endpoints to Go handlers / SQL / tables (cfg.codeDir) ----
let codeMap = null, codeStats = null;
if (cfgFile.codeDir && fs.existsSync(cfgFile.codeDir)) {
  const files = [];
  const walk = (d) => { for (const f of fs.readdirSync(d, { withFileTypes: true })) { if (f.isDirectory()) { if (!/node_modules|vendor|\.git/.test(f.name)) walk(path.join(d, f.name)); } else if (f.name.endsWith(".go") && !f.name.endsWith("_test.go")) files.push(path.join(d, f.name)); } };
  walk(cfgFile.codeDir);
  const funcs = new Map();
  const routes = [];
  for (const f of files) {
    const src = fs.readFileSync(f, "utf8");
    src.split("\n").forEach((ln, i) => {
      let m;
      if ((m = ln.match(/HandleFunc\("(\w+) ([^"]+)",\s*(\w+)\)/))) routes.push({ method: m[1], path: m[2], fn: m[3], file: f, line: i + 1 });
      else if ((m = ln.match(/\.(Get|Post|Put|Delete|Patch)\("([^"]+)",\s*(\w+)\)/))) routes.push({ method: m[1].toUpperCase(), path: m[2], fn: m[3], file: f, line: i + 1 }); // chi
      else if ((m = ln.match(/\.(GET|POST|PUT|DELETE|PATCH)\("([^"]+)",\s*(\w+)[,)]/))) routes.push({ method: m[1], path: m[2], fn: m[3], file: f, line: i + 1 }); // echo / gin
    });
    const fnRe = /^func (?:\([^)]*\) )?(\w+)\(/gm;
    let m;
    const starts = [];
    while ((m = fnRe.exec(src))) starts.push({ name: m[1], idx: m.index });
    starts.forEach((st, i) => {
      const body = src.slice(st.idx, starts[i + 1]?.idx ?? src.length);
      funcs.set(st.name, { name: st.name, file: f, line: src.slice(0, st.idx).split("\n").length, body });
    });
  }
  {
    const lines = (f) => fs.readFileSync(f, "utf8").split("\n").length;
    const sqlN = (f) => (fs.readFileSync(f, "utf8").match(/["`]\s*\(?\s*(SELECT|INSERT|UPDATE|DELETE|REPLACE)\b/gi) ?? []).length;
    codeStats = { dir: cfgFile.codeDir, files: files.length, lines: files.reduce((n, f) => n + lines(f), 0), funcs: funcs.size, routes: routes.length, sqlStatements: files.reduce((n, f) => n + sqlN(f), 0), biggest: files.map((f) => ({ file: path.relative(cfgFile.codeDir, f), lines: lines(f), sql: sqlN(f) })).sort((a, b) => b.lines - a.lines).slice(0, 8) };
  }
  const whereCols = (sql) => {
    const m = sql.match(/\bWHERE\b([\s\S]*?)(?:\bORDER BY\b|\bGROUP BY\b|\bLIMIT\b|\bFOR\b|\)|$)/i);
    if (!m) return [];
    return [...m[1].matchAll(/`?(?:\w+\.)?(\w+)`?\s*(?:=|<>|!=|<=|>=|<|>|\bIN\b|\bIS\b|\bLIKE\b|\bBETWEEN\b)/gi)].map((x) => x[1]).filter((c) => !/^(AND|OR|NOT|NULL|TRUE|FALSE)$/i.test(c));
  };
  const orderCols = (sql) => [...(sql.match(/\bORDER BY\b([\s\S]*?)(?:\bLIMIT\b|\bFOR\b|\)|$)/i)?.[1] ?? "").matchAll(/`?(?:\w+\.)?(\w+)`?/g)].map((x) => x[1]).filter((c) => !/^(ASC|DESC|RAND)$/i.test(c));
  const analyzeSql = (sql, inLoop) => {
    const st = { sql: sql.replace(/\s+/g, " ").trim(), inLoop, tables: [], flags: [] };
    for (const tm of sql.matchAll(/\b(?:FROM|JOIN|INTO|UPDATE)\s+`?(\w+)`?/gi)) if (!/^(SELECT|DUAL|VALUES)$/i.test(tm[1]) && !st.tables.includes(tm[1])) st.tables.push(tm[1]);
    const kind = (sql.match(/^\s*\(?\s*(SELECT|INSERT|UPDATE|DELETE)/i)?.[1] ?? "").toUpperCase();
    st.kind = kind;
    if (inLoop) st.flags.push("loop");
    if (kind === "SELECT" && /SELECT\s+\*/i.test(sql)) st.flags.push("selectStar");
    if (kind === "SELECT" && /\bFROM\b/i.test(sql) && !/\bWHERE\b/i.test(sql) && !/\bJOIN\b/i.test(sql)) st.flags.push("noWhere");
    if (/ORDER BY RAND\(\)/i.test(sql)) st.flags.push("rand");
    if (/\bOVER\s*\(/i.test(sql)) st.flags.push("window");
    if ((sql.match(/\(\s*SELECT/gi) ?? []).length) st.flags.push("subquery");
    if (/FOR UPDATE|FOR SHARE|LOCK IN SHARE/i.test(sql)) st.flags.push("lock");
    if (/ORDER BY[\s\S]*LIMIT 1\b/i.test(sql)) st.flags.push("latestRow");
    if (/COUNT\s*\(/i.test(sql)) st.flags.push("count");
    const wc = [...new Set(whereCols(sql))], oc = [...new Set(orderCols(sql))];
    st.whereCols = wc; st.orderCols = oc;
    const t = schema?.tables[st.tables[0]];
    if (t && kind !== "INSERT") {
      if (wc.length && !t.indexes.some((ix) => wc.includes(ix.cols[0]))) st.flags.push("noIndexWhere");
      if (oc.length && !t.indexes.some((ix) => ix.cols.includes(oc[0]))) st.flags.push("noIndexOrder");
    }
    return st;
  };
  const analyze = (fn, depth = 0, seen = new Set()) => {
    const out = { statements: [], tables: new Set(), sql: 0, loopSql: false, locks: false, external: false, helpers: [], tx: false, sleep: false, retry: false, sortInApp: false, loc: 0, branches: 0, loops: 0 };
    const f = funcs.get(fn);
    if (!f || seen.has(fn) || depth > 1) return out;
    seen.add(fn);
    const lines = f.body.split("\n");
    out.loc = lines.filter((l) => l.trim() && !l.trim().startsWith("//")).length;
    const loopDepth = [];
    const stack = [];
    lines.forEach((ln) => {
      if (/^\s*$/.test(ln)) { loopDepth.push(stack.length); return; }
      const ind = ln.match(/^\s*/)[0].length;
      while (stack.length && stack[stack.length - 1] >= ind) stack.pop();
      if (/^\s*for\b/.test(ln)) { stack.push(ind); out.loops++; }
      loopDepth.push(stack.length);
      if (/^\s*(if|else if|switch|case)\b/.test(ln)) out.branches++;
    });
    for (const m of f.body.matchAll(/`([^`]*)`|"((?:[^"\\]|\\.)*)"/g)) {
      const lit = m[1] ?? m[2];
      if (!/^\s*\(?\s*(SELECT|INSERT|UPDATE|DELETE)\b/i.test(lit)) continue;
      const line = f.body.slice(0, m.index).split("\n").length - 1;
      const st = analyzeSql(lit, loopDepth[line] > 0);
      if (depth > 0) st.via = fn;
      out.statements.push(st);
      out.sql++;
      if (st.inLoop) out.loopSql = true;
      if (st.flags.includes("lock")) out.locks = true;
      st.tables.forEach((t) => out.tables.add(t));
    }
    if (/\.Beginx?\(|BeginTx\(/.test(f.body)) out.tx = true;
    if (/time\.Sleep/.test(f.body)) out.sleep = true;
    if (/retry/i.test(f.body) && /for\s*\{/.test(f.body)) out.retry = true;
    if (/\bsort\.(Slice|Sort|Strings|Ints)/.test(f.body)) out.sortInApp = true;
    if (/http\.(Post|Get|NewRequest)|\.Do\(|client\.(Get|Post)/.test(f.body)) out.external = true;
    for (const cm of f.body.matchAll(/\b([a-zA-Z_]\w*)\(/g)) {
      const name = cm[1];
      if (name !== fn && funcs.has(name) && !seen.has(name)) {
        const sub = analyze(name, depth + 1, seen);
        if (sub.sql || sub.external || sub.sleep) {
          out.helpers.push(name);
          out.statements.push(...sub.statements.map((st) => ({ ...st, via: st.via ?? name, inLoop: st.inLoop || false })));
          sub.tables.forEach((t) => out.tables.add(t));
          out.sql += sub.sql; out.loopSql ||= sub.loopSql; out.locks ||= sub.locks; out.external ||= sub.external; out.sleep ||= sub.sleep; out.retry ||= sub.retry; out.sortInApp ||= sub.sortInApp; out.loc += sub.loc; out.branches += sub.branches;
          // a helper called from inside a loop makes its SQL loop SQL too
        }
      }
    }
    // helper calls inside loops → mark their statements as loop
    const helperCallsInLoop = new Set();
    lines.forEach((ln, i) => { if (loopDepth[i] > 0) for (const cm of ln.matchAll(/\b([a-zA-Z_]\w*)\(/g)) if (out.helpers.includes(cm[1])) helperCallsInLoop.add(cm[1]); });
    for (const st of out.statements) if (st.via && helperCallsInLoop.has(st.via) && !st.inLoop) { st.inLoop = true; st.flags.unshift("loop"); out.loopSql = true; }
    return out;
  };
  const norm = (p) => p.replace(/\{[^}]+\}/g, ":id").replace(/:\w+/g, ":id");
  const info = (r) => {
    const a = analyze(r.fn);
    const fdef = funcs.get(r.fn);
    const flags = [];
    if (a.loopSql) flags.push("loop");
    if (a.tx) flags.push("tx");
    if (a.locks) flags.push("lock");
    if (a.external) flags.push("external");
    if (a.retry) flags.push("retry");
    if (a.sleep) flags.push("sleep");
    if (a.sortInApp) flags.push("sortInApp");
    for (const fl of ["rand", "window", "noWhere", "selectStar", "subquery", "noIndexWhere", "noIndexOrder", "latestRow"]) if (a.statements.some((st) => st.flags.includes(fl))) flags.push(fl);
    return { handler: r.fn, file: path.relative(cfgFile.codeDir, fdef?.file ?? r.file), line: fdef?.line ?? r.line, tables: [...a.tables], sql: a.sql, loopSql: a.loopSql, locks: a.locks, external: a.external, helpers: a.helpers, tx: a.tx, sleep: a.sleep, retry: a.retry, sortInApp: a.sortInApp, loc: a.loc, branches: a.branches, loops: a.loops, statements: a.statements, flags };
  };
  codeMap = {};
  for (const e of epList) {
    const r = routes.find((rt) => rt.method === e.method && norm(rt.path) === e.path) ?? routes.find((rt) => rt.method === e.method && e.path.endsWith(norm(rt.path)));
    if (r) codeMap[`${e.method} ${e.path}`] = info(r);
  }
  for (const o of unobservedOps) {
    const r = routes.find((rt) => rt.method === o.method && norm(rt.path) === o.path);
    if (r) o.code = info(r);
  }
}
for (const e of epList) e.code = codeMap?.[`${e.method} ${e.path}`] ?? null;

// ---- system overview: components, web routing, storage, code size (from the repo = /home/isucon mirror: etc/, env.sh, docs/server-info-*.txt) ----
const system = { root: null, components: [], web: [], storage: [], notes: [], env: [], code: codeStats, schema: schema ? { path: schema.path, tables: Object.keys(schema.tables).length, indexes: Object.values(schema.tables).reduce((n, t) => n + t.indexes.length, 0), columns: Object.values(schema.tables).reduce((n, t) => n + t.columns.length, 0) } : null };
{
  const root = cfgFile.systemRoot ?? (cfgFile.codeDir ? path.join(cfgFile.codeDir, "..", "..") : null);
  if (root && fs.existsSync(path.join(root, "etc"))) {
    system.root = root;
    const etc = path.join(root, "etc");
    const ud = path.join(etc, "systemd", "system");
    if (fs.existsSync(ud)) for (const f of fs.readdirSync(ud).filter((x) => x.endsWith(".service")).sort()) {
      const src = fs.readFileSync(path.join(ud, f), "utf8");
      const g = (k) => (src.match(new RegExp(`^${k}=(.*)$`, "m"))?.[1] ?? "").trim();
      const deps = [...new Set(`${g("Requires")} ${g("After")}`.split(/\s+/).filter((x) => x && !/syslog|network|\.target$/.test(x)))].join(" ");
      system.components.push({ unit: f, exec: g("ExecStart"), dir: g("WorkingDirectory"), deps, envFile: g("EnvironmentFile"), type: g("Type") });
    }
    const nd = path.join(etc, "nginx");
    if (fs.existsSync(nd)) {
      const confs = [];
      const walkN = (d) => { for (const f of fs.readdirSync(d, { withFileTypes: true })) { const p2 = path.join(d, f.name); if (f.isDirectory()) { if (!/tls|modules|snippets/.test(f.name)) walkN(p2); } else if (/\.conf$|sites-enabled/.test(p2) && !/sites-available/.test(p2)) confs.push(p2); } };
      walkN(nd);
      // sites-enabled entries are often absolute symlinks (/etc/nginx/sites-available/x) that do not exist in the repo copy:
      // fall back to ../sites-available/<same name> (or the link target's name), else skip with a warning
      const readConf = (f) => {
        if (fs.existsSync(f)) return fs.readFileSync(f, "utf8");
        let target = "";
        try { target = fs.readlinkSync(f); } catch {}
        for (const alt of [path.join(path.dirname(f), "..", "sites-available", path.basename(f)), target && path.join(path.dirname(f), "..", "sites-available", path.basename(target))].filter(Boolean)) if (fs.existsSync(alt)) return fs.readFileSync(alt, "utf8");
        console.warn(`warn: ${f} -> ${target || "?"} not found locally (no sites-available copy); skipped`);
        return null;
      };
      for (const f of confs) {
        const raw = readConf(f);
        if (raw == null) continue;
        const src = raw.replace(/#[^\n]*/g, "");
        let idx = 0;
        while ((idx = src.indexOf("server", idx)) >= 0) {
          const open = src.indexOf("{", idx);
          if (open < 0 || !/server\s*$/.test(src.slice(idx, open))) { idx += 6; continue; }
          let depth = 0, end = open;
          for (let i = open; i < src.length; i++) { if (src[i] === "{") depth++; else if (src[i] === "}") { depth--; if (!depth) { end = i; break; } } }
          const blk = src.slice(open + 1, end);
          const names = [...blk.matchAll(/server_name\s+([^;]+);/g)].flatMap((m) => m[1].trim().split(/\s+/));
          const listens = [...blk.matchAll(/listen\s+([^;]+);/g)].map((m) => m[1].trim());
          const root2 = blk.match(/\broot\s+([^;]+);/)?.[1]?.trim() ?? "";
          const locs = [...blk.matchAll(/location\s+([^{]+)\{([^}]*)\}/g)].map((m) => { const pp = m[2].match(/proxy_pass\s+([^;]+);/)?.[1]; const tf = m[2].match(/try_files\s+([^;]+);/)?.[1]; return `${m[1].trim()} → ${pp ? "proxy " + pp.trim() : tf ? "static (" + tf.trim() + ")" : "static"}`; });
          if (names.length || listens.length) system.web.push({ file: path.relative(etc, f), serverName: names.join(" ") || "_", listen: listens.join(", "), root: root2, locations: locs });
          idx = end;
        }
      }
    }
    const has = (d) => fs.existsSync(path.join(etc, d));
    if (has("mysql/mysql.conf.d")) system.storage.push({ kind: "MySQL", detail: "etc/mysql/mysql.conf.d" });
    else if (has("mysql/mariadb.conf.d")) system.storage.push({ kind: "MariaDB", detail: "etc/mysql/mariadb.conf.d" });
    if (has("postgresql")) system.storage.push({ kind: "PostgreSQL", detail: "etc/postgresql" });
    if (has("redis")) system.storage.push({ kind: "Redis", detail: "etc/redis" });
    if (has("valkey")) system.storage.push({ kind: "Valkey", detail: "etc/valkey" });
    if (has("memcached.conf")) system.storage.push({ kind: "memcached", detail: "etc/memcached.conf" });
    if (has("powerdns")) {
      let txt = "";
      const pd = path.join(etc, "powerdns");
      for (const f of ["pdns.conf", ...(fs.existsSync(path.join(pd, "pdns.d")) ? fs.readdirSync(path.join(pd, "pdns.d")).map((x) => "pdns.d/" + x) : [])]) if (fs.existsSync(path.join(pd, f))) txt += fs.readFileSync(path.join(pd, f), "utf8") + "\n";
      const launch = txt.match(/^launch=(.*)$/m)?.[1] ?? "?";
      const extra = [...txt.matchAll(/^(gmysql-dbname|gmysql-host|gsqlite3-database|gpgsql-dbname|cache-ttl|query-cache-ttl|negquery-cache-ttl)=(.*)$/gm)].map((m) => `${m[1]}=${m[2]}`).join(" ");
      system.storage.push({ kind: "PowerDNS", detail: `launch=${launch} ${extra}`.trim(), note: /gmysql/.test(launch) ? "DNS queries hit MySQL" : /gsqlite3/.test(launch) ? "DNS backend is a SQLite file" : "" });
    }
    const dd = path.join(root, "docs");
    if (fs.existsSync(dd)) for (const f of fs.readdirSync(dd).filter((x) => /^server-info-.*\.txt$/.test(x))) for (const ln of fs.readFileSync(path.join(dd, f), "utf8").split("\n")) if (/^(detected|NOTE)/.test(ln)) system.notes.push(ln.trim());
    const walkDb = (d, depth) => { if (depth > 3 || !fs.existsSync(d)) return; for (const f of fs.readdirSync(d, { withFileTypes: true })) { const p2 = path.join(d, f.name); if (f.isDirectory()) { if (!/node_modules|vendor|\.git/.test(f.name)) walkDb(p2, depth + 1); } else if (/\.(db|sqlite|sqlite3)$/.test(f.name)) system.storage.push({ kind: "SQLite file", detail: path.relative(root, p2) + ` (${fs.statSync(p2).size} B)` }); } };
    walkDb(path.join(root, "webapp"), 0);
    const envf = path.join(root, "env.sh");
    if (fs.existsSync(envf)) system.env = fs.readFileSync(envf, "utf8").split("\n").map((l) => l.trim()).filter((l) => l && !l.startsWith("#")).map((l) => l.replace(/^export\s+/, "").split("=")[0]);
  }
}

// ---- screen purpose (title / meta / headings / inputs / buttons / API roles) ----
const roleByKey = {};
for (const s of screens) {
  const meta = routeMeta[(s.finalUrl ?? s.url ?? "").split("?")[0]] ?? null;
  const apis = [];
  const seen = new Set();
  for (const r of (s.requests ?? []).filter(isApiReq)) { const k = epKey(r); if (seen.has(k)) continue; seen.add(k); const e = epList.find((x) => `${x.method} ${x.path}` === k); if (e) apis.push({ key: k, role: e.role, kind: e.kind }); }
  s.purpose = { desc: meta?.desc ?? null, metaTitle: meta?.title ?? null, h1: (s.headings ?? []).find((x) => x.startsWith("h1:"))?.slice(3).trim() ?? null, h2: (s.headings ?? []).filter((x) => x.startsWith("h2:")).map((x) => x.slice(3).trim()).slice(0, 3), inputs: (s.inputs ?? []).map((x) => x.replace(/^\w+:/, "")).slice(0, 8), buttons: (s.buttons ?? []).slice(0, 6), apis };
}

// ---- service summary + findings ----
const actors = {};
for (const e of epList) {
  const a = actors[e.actor] ?? (actors[e.actor] = { actor: e.actor, endpoints: 0, reads: 0, writes: 0, requests: 0, polling: 0, screens: new Set(), paths: new Set() });
  a.endpoints++; a[e.kind === "read" ? "reads" : "writes"]++; a.requests += e.count; if (e.role === "polling") a.polling++;
  for (const sid of e.screenIds) a.screens.add(sid);
  e.path.replace(apiPrefix, "").split("/").slice(1).forEach((seg) => { if (seg && seg !== ":id") a.paths.add(seg); });
}
const actorList = Object.values(actors).map((a) => ({ ...a, screens: [...a.screens].map((id) => screens[+id]).filter((s) => s.kind === "screen").map((s) => s.label), resources: [...a.paths].slice(0, 12) })).sort((a, b) => b.requests - a.requests);
const findings = [];
const pollEpList = epList.filter((e) => e.role === "polling");
const pollCount = pollEpList.reduce((n, e) => n + e.count, 0);
if (pollCount) {
  const hints = [...new Set(pollEpList.flatMap((e) => [...e.hints]))];
  findings.push({ key: "polling", params: { n: pollEpList.length, pct: Math.round((100 * pollCount) / Math.max(1, totalApiReqs)), list: pollEpList.map((e) => `${e.method} ${e.path} (~${median(pollingAll.filter((p) => p.endpoint === `${e.method} ${e.path}`).map((p) => p.medianGapMs))}ms gap, median ${e.medianMs ?? "?"}ms)`).join("; "), hints: hints.join(", "), sse: !!data.frontendRefs?.hasEventSource }, level: "high", title: `${pollEpList.length} polling endpoint(s) = ${Math.round((100 * pollCount) / Math.max(1, totalApiReqs))}% of observed requests`, detail: `${pollEpList.map((e) => `${e.method} ${e.path} (~${median(pollingAll.filter((p) => p.endpoint === `${e.method} ${e.path}`).map((p) => p.medianGapMs))}ms gap, median ${e.medianMs ?? "?"}ms)`).join("; ")}${hints.length ? `. Server-directed interval field(s) in the response: ${hints.join(", ")} → raising the value or switching to SSE changes client behaviour.` : ""}${data.frontendRefs?.hasEventSource ? " The frontend bundle already contains EventSource code." : ""}`, view: "endpoints", arg: "polling" });
}
const slow = epList.filter((e) => e.medianMs != null && e.medianMs >= 300).sort((a, b) => b.medianMs - a.medianMs);
if (slow.length) findings.push({ perf: true, key: "slow", params: { n: slow.length, list: slow.map((e) => `${e.method} ${e.path} ${e.medianMs}ms [${e.role}${e.code ? `; ${e.code.handler} → ${e.code.tables.join(", ")}${e.code.loopSql ? "; SQL in loop" : ""}` : ""}]`).join("; ") }, level: "high", title: `${slow.length} endpoint(s) slow even without load (≥300ms)`, detail: slow.map((e) => `${e.method} ${e.path} ${e.medianMs}ms [${e.role}${e.code ? `; ${e.code.handler} → ${e.code.tables.join(", ")}${e.code.loopSql ? "; SQL in loop" : ""}` : ""}]`).join("; "), view: "endpoints", arg: "slow" });
const internals = unobservedOps.filter((o) => o.path.includes("/internal/"));
if (internals.length) findings.push({ key: "internal", params: { n: internals.length, list: internals.map((o) => `${o.method} ${o.path}${o.code ? ` (${o.code.handler} → ${o.code.tables.join(", ")})` : ""}`).join("; ") }, level: "high", title: `${internals.length} internal endpoint(s) never called by the UI — server-side batch candidates`, detail: internals.map((o) => `${o.method} ${o.path}${o.code ? ` (${o.code.handler} → ${o.code.tables.join(", ")})` : ""}`).join("; ") + ". Who calls it, how often, how many items per call? This often bounds throughput.", view: "scan" });
const loopEps = epList.filter((e) => e.code?.loopSql);
if (loopEps.length) findings.push({ perf: true, key: "loop", params: { n: loopEps.length, list: loopEps.map((e) => `${e.method} ${e.path} → ${e.code.handler} (${e.code.sql} SQL; ${e.code.tables.join(", ")})`).join("; ") }, level: "high", title: `${loopEps.length} handler(s) run SQL inside a loop (N+1 suspects)`, detail: loopEps.map((e) => `${e.method} ${e.path} → ${e.code.handler} (${e.code.sql} SQL; ${e.code.tables.join(", ")})`).join("; "), view: "endpoints", arg: "loop" });
const writes = epList.filter((e) => e.kind === "write" && e.role !== "initialize").sort((a, b) => b.count - a.count);
if (repeatedWrites.length) findings.push({ key: "repeatedWrites", params: { list: [...new Set(repeatedWrites.map((p) => p.endpoint))].join(", "), detail: repeatedWrites.map((p) => `${p.endpoint} every ~${p.medianGapMs}ms in ${p.screen}`).join("; ") }, level: "info", title: `Repeated write(s) from clients: ${[...new Set(repeatedWrites.map((p) => p.endpoint))].join(", ")}`, detail: repeatedWrites.map((p) => `${p.endpoint} every ~${p.medianGapMs}ms in ${p.screen}`).join("; ") + ". Under the bench this is the main write stream; consider batching / async writes.", view: "endpoints", arg: "write" });
if (writes.length) findings.push({ key: "writeHot", params: { ep: `${writes[0].method} ${writes[0].path}`, count: writes[0].count, ms: writes[0].medianMs ?? "?", role: writes[0].role, code: writes[0].code ? `${writes[0].code.handler} → ${writes[0].code.tables.join(", ")}${writes[0].code.locks ? "; row locks" : ""}` : "", others: writes.slice(1, 5).map((e) => `${e.method} ${e.path}`).join(", ") }, level: "info", title: `Write hot path: ${writes[0].method} ${writes[0].path} (${writes[0].count} calls, median ${writes[0].medianMs ?? "?"}ms)`, detail: `${writes[0].role}${writes[0].code ? `; ${writes[0].code.handler} → ${writes[0].code.tables.join(", ")}${writes[0].code.locks ? "; row locks" : ""}` : ""}. Other writes: ${writes.slice(1, 5).map((e) => `${e.method} ${e.path}`).join(", ")}`, view: "endpoints", arg: "write" });
for (const sb of storyboards.filter((x) => x.states.length > 1)) findings.push({ key: "stateMachine", params: { label: sb.label, seq: [...new Set(sb.states.map((s) => s.value))].join(" → "), transitions: sb.transitions.map((t) => `${t.from}→${t.to}: ${t.trigger ?? "(server-side)"}${t.trigger ? ` [${t.actor}]` : ""}`).join("; ") }, level: "info", title: `State machine observed in ${sb.label}: ${[...new Set(sb.states.map((s) => s.value))].join(" → ")}`, detail: sb.transitions.map((t) => `${t.from}→${t.to}: ${t.trigger ?? "no client call (server-side)"}${t.trigger ? ` [${t.actor}]` : ""}`).join("; "), view: "storyboard" });
const external = epList.filter((e) => e.code?.external);
if (external.length) findings.push({ key: "external", params: { n: external.length, list: external.map((e) => `${e.method} ${e.path} → ${e.code.handler}`).join("; ") }, level: "info", title: `${external.length} handler(s) call an external HTTP service`, detail: external.map((e) => `${e.method} ${e.path} → ${e.code.handler}`).join("; ") + ". Latency and retries there are outside DB tuning.", view: "endpoints", arg: "external" });
const appErr = epList.reduce((n, e) => n + e.errors.app, 0), artErr = epList.reduce((n, e) => n + e.errors.artefact, 0);
const leaks = epList.filter((e) => e.errors.samples.some((s) => /Error \d{4}|SQL|syntax|duplicate entry/i.test(s.msg)));
findings.push({ key: "errors", params: { app: appErr, art: artErr, leaks: leaks.map((e) => `${e.method} ${e.path}`).join(", ") }, level: appErr ? "high" : "info", title: `${appErr} app-origin error(s); ${artErr} crawl artefact(s) (missing cookie, re-run duplicates, aborted)`, detail: leaks.length ? `DB error text leaks to clients on: ${leaks.map((e) => `${e.method} ${e.path}`).join(", ")} — handlers are thin; expect little validation around writes.` : "", view: "endpoints", arg: "errors" });

// ---- data model: tables, inferred relations (<x>_id -> x / xs), row counts after a bench (task db:snapshot → docs/retention/latest.txt) ----
const dataModel = { tables: [], relations: [], rowsSource: null };
if (schema) {
  const names = Object.keys(schema.tables);
  const findTable = (base) => names.find((n) => n === base) ?? names.find((n) => n === base + "s") ?? names.find((n) => n === base + "es") ?? names.find((n) => base.endsWith("y") && n === base.slice(0, -1) + "ies") ?? null;
  let rows = {};
  const rp = cfgFile.rowCountsPath ?? path.join(cfgFile.codeDir ?? ".", "..", "..", "docs", "retention", "latest.txt"); // <repo>/docs/retention/latest.txt (task db:snapshot)
  if (fs.existsSync(rp)) {
    const txt = fs.readFileSync(rp, "utf8");
    const head = txt.split("\n")[0];
    dataModel.rowsSource = path.relative(process.cwd(), rp) + (head.startsWith("#") ? " (" + head.replace(/^#\s*/, "") + ")" : "");
    for (const ln of txt.split("\n")) { const m = ln.trim().match(/^(?:\w+\.)?(\w+)\s+(\d+)$/); if (m) rows[m[1]] = Number(m[2]); }
  }
  for (const [t, v] of Object.entries(schema.tables)) {
    const cols = v.columns.map((c) => (typeof c === "string" ? { name: c, type: "" } : c));
    const pk = v.indexes.find((ix) => ix.name === "PRIMARY")?.cols ?? [];
    dataModel.tables.push({ name: t, columns: cols.map((c) => ({ ...c, pk: pk.includes(c.name), indexed: v.indexes.some((ix) => ix.name !== "PRIMARY" && ix.cols[0] === c.name) })), indexes: v.indexes.map((ix) => `${ix.name}(${ix.cols.join(",")})`), rows: rows[t] ?? null });
    for (const c of cols) {
      const m = c.name.match(/^(\w+)_id$/);
      if (!m) continue;
      const target = findTable(m[1]);
      if (target && target !== t) dataModel.relations.push({ from: t, col: c.name, to: target });
    }
  }
}

// ---- report.md ----
const codeRev = (() => { try { return execSync("git rev-parse --short HEAD", { stdio: ["ignore", "pipe", "ignore"] }).toString().trim(); } catch { return "?"; } })();
let md = `# App analysis report\n\n- crawl: ${data.generatedAt} (screens/API calls are from this crawl)\n- code read: ${codeRev} at ${new Date().toISOString()} (handler → tables, data model)\n- baseURL: ${data.baseURL}\n- screens: ${screens.filter((s) => s.kind === "screen").length}, flows: ${screens.filter((s) => s.kind === "flow").length}, distinct endpoints observed: ${epList.length}, API requests recorded: ${totalApiReqs}\n\n`;
md += `> The benchmarker calls the API directly. Request counts here reflect the browser UI; use the access log for bench behaviour.\n\n`;
if (system.root || system.code) {
  md += `## System (from the repo mirror of /home/isucon: etc/, env.sh, server-info)\n\n`;
  if (system.components.length) { md += `| unit | ExecStart | WorkingDirectory | depends on |\n|---|---|---|---|\n`; for (const c of system.components) md += `| ${code(c.unit)} | ${code(c.exec)} | ${code(c.dir)} | ${c.deps} |\n`; md += `\n`; }
  if (system.web.length) { md += `| nginx server_name | listen | root | locations |\n|---|---|---|---|\n`; for (const w of system.web) md += `| ${code(w.serverName)} | ${w.listen} | ${code(w.root)} | ${w.locations.map(code).join("<br>")} |\n`; md += `\n`; }
  if (system.storage.length) md += `Storage: ${system.storage.map((st) => `**${st.kind}** ${st.detail}${st.note ? " — " + st.note : ""}`).join("; ")}\n\n`;
  if (system.notes.length) md += `server-info: ${system.notes.join("; ")}\n\n`;
  if (system.code) md += `Code (${system.code.dir}): ${system.code.files} Go files, ${system.code.lines} lines, ${system.code.funcs} funcs, ${system.code.routes} routes, ${system.code.sqlStatements} SQL literals${system.schema ? `; schema ${system.schema.tables} tables, ${system.schema.columns} columns, ${system.schema.indexes} secondary indexes` : ""}. Biggest files: ${system.code.biggest.slice(0, 5).map((b) => `${b.file} (${b.lines} lines, ${b.sql} SQL)`).join(", ")}\n\n`;
  if (system.env.length) md += `env.sh keys: ${system.env.map(code).join(" ")}\n\n`;
}
md += `## Service summary (auto)\n\n| actor | screens | endpoints (read/write) | polling | resources seen in paths |\n|---|---|---|---|---|\n`;
for (const a of actorList) md += `| ${a.actor} | ${a.screens.map(code).join(" ") || "-"} | ${a.endpoints} (${a.reads}/${a.writes}) | ${a.polling} | ${a.resources.join(", ")} |\n`;
md += `\n## Top findings (behaviour)\n\n${findings.filter((f) => !f.perf).map((f) => `- **[${f.level}] ${f.title}** ${f.detail}`).join("\n")}\n\n`;
if (findings.some((f) => f.perf)) md += `## Performance hints — code snapshot at crawl time (the bench report is the source of truth; these go stale after the first fix round)\n\n${findings.filter((f) => f.perf).map((f) => `- **[${f.level}] ${f.title}** ${f.detail}`).join("\n")}\n\n`;
if (dataModel.tables.length) {
  md += `## Data model (${schema.path})${dataModel.rowsSource ? ` — rows from ${path.relative(process.cwd(), dataModel.rowsSource)}` : " — rows: run \`task db:snapshot LABEL=latest\` after a bench to fill in"}\n\n| table | rows | columns | indexes | refers to |\n|---|---|---|---|---|\n`;
  for (const t of dataModel.tables) md += `| ${t.name} | ${t.rows ?? ""} | ${t.columns.map((c) => c.name + (c.pk ? " (PK)" : c.indexed ? " (idx)" : "")).join(", ")} | ${t.indexes.join(", ") || "-"} | ${dataModel.relations.filter((r) => r.from === t.name).map((r) => `${r.col} → ${r.to}`).join(", ")} |\n`;
  md += `\n`;
}
for (const sb of storyboards.filter((x) => x.states.length > 1)) {
  md += `## State transitions (${sb.label})\n\n| from | to | trigger (last non-polling client call ≤5s before) | actor | observed via |\n|---|---|---|---|---|\n`;
  for (const t of sb.transitions) md += `| ${t.from} | ${t.to} | ${code(t.trigger ?? "(none: server-side)")} | ${t.actor} | ${code(t.observedVia)} |\n`;
  md += `\n`;
}
md += `## Endpoint summary (all personas)\n\n| actor | role | method | path | count | median ms | statuses | handler → tables | screens | sample query | response schema |\n|---|---|---|---|---|---|---|---|---|---|---|\n`;
for (const e of [...epList].sort((a, b) => a.actor.localeCompare(b.actor) || b.count - a.count)) {
  md += `| ${e.actor} | ${e.role} | ${e.method} | ${code(e.path)} | ${e.count} | ${e.medianMs ?? ""} | ${[...e.statuses].map(([s, nn]) => `${s}×${nn}`).join(" ")} | ${e.code ? `${code(e.code.handler)} → ${e.code.tables.join(", ")}${e.code.locks ? " (locks)" : ""}${e.code.external ? " (external call)" : ""}` : ""} | ${[...e.screens].slice(0, 3).map(code).join("<br>")}${e.screens.size > 3 ? ` +${e.screens.size - 3}` : ""} | ${code(e.sampleQuery)} | ${code(e.sampleSchema?.slice(0, 300))} |\n`;
}
if (codeMap) {
  md += `\n## Handler details (which SQL each endpoint runs — from ${cfgFile.codeDir}; performance judgements belong to the bench report)\n\n`;
  for (const e of [...epList, ...unobservedOps.filter((o) => o.code).map((o) => ({ method: o.method, path: o.path, code: o.code }))]) {
    if (!e.code) continue;
    const c = e.code;
    const beh = (fl) => (fl ?? []).filter((f) => ["external", "exec", "retry", "sleep", "lock", "tx"].includes(f));
    md += `### ${e.method} ${e.path} → ${c.handler} (${c.file}:${c.line}${c.helpers.length ? `; helpers ${c.helpers.join(", ")}` : ""})\n\nbehaviour: ${beh(c.flags).join(", ") || "-"}\n\n`;
    for (const st of c.statements) md += `- ${beh(st.flags).length ? `[${beh(st.flags).join(",")}] ` : ""}${code(st.sql.slice(0, 220))}${st.via ? ` (via ${st.via})` : ""}\n`;
    md += `\n`;
  }
  if (schema) md += `### Schema indexes\n\n${Object.entries(schema.tables).map(([t, v]) => `- ${t}: ${v.indexes.map((ix) => `${ix.name}(${ix.cols.join(",")})`).join(", ") || "(none)"}`).join("\n")}\n\n`;
}
md += `\n## Screens\n\n| persona | screen | title | headings | buttons / inputs | #req | polling | error |\n|---|---|---|---|---|---|---|---|\n`;
for (const s of screens) {
  const apiReq = (s.requests ?? []).filter(isApiReq).length;
  md += `| ${s.persona} | ${code(s.label)}${s.revisit ? " (revisit)" : ""} | ${esc(s.title ?? "")}${s.purpose?.desc ? ` — ${esc(s.purpose.desc)}` : ""} | ${(s.headings ?? []).slice(0, 4).map(esc).join("<br>")} | ${[...(s.buttons ?? []).slice(0, 6), ...(s.inputs ?? []).slice(0, 6)].map(code).join(" ")} | ${apiReq} | ${(s.polling ?? []).map((p) => `${p.endpoint} ×${p.count} (~${p.medianGapMs}ms)`).join("<br>")} | ${esc(s.error ?? "")} |\n`;
}
if (storyboards.length) {
  md += `\n## Storyboard (flows: step → screen state → API calls in that step)\n\n`;
  for (const sb of storyboards) {
    md += `### ${sb.persona}: ${sb.label} (actors: ${sb.actors.join(", ")})\n\n| # | step | state changes | main API calls | polling |\n|---|---|---|---|---|\n`;
    sb.parts.forEach((p, i) => { md += `| ${i} | ${esc(p.label)} | ${p.stateChanges.join(" → ")} | ${p.calls.filter((c) => !c.polling).map((c) => code(c.text)).join("<br>")} | ${p.calls.filter((c) => c.polling).map((c) => code(c.text)).join("<br>")} |\n`; });
    md += `\n`;
  }
}
md += `## Navigation graph\n\n\`\`\`mermaid\ngraph LR\n`;
const nid = (p) => "n" + p.replace(/[^a-zA-Z0-9]/g, "_");
const mlabel = (t) => (t.replace(/[^\p{L}\p{N} _\-]/gu, " ").replace(/\s+/g, " ").trim().slice(0, 24) || "link");
const nodes = new Set();
for (const e of navModel.edges) { nodes.add(e.from); nodes.add(e.to); }
for (const nname of nodes) md += `  ${nid(nname)}["${nname}"]\n`;
for (const e of navModel.edges) md += `  ${nid(e.from)} ${e.type === "redirect" ? "-.->" : "-->"}|${mlabel(e.label)}| ${nid(e.to)}\n`;
md += `\`\`\`\n\n### In-page actions (buttons / forms per screen, candidates for flows)\n\n`;
for (const s of screens.filter((x) => x.kind === "screen" && ((x.buttons ?? []).length || (x.forms ?? []).length))) md += `- ${code(`${s.persona}:${s.label}`)}: buttons ${(s.buttons ?? []).slice(0, 10).map(code).join(" ")}${(s.forms ?? []).length ? `; forms ${s.forms.map((f) => code(`${f.method} ${f.action ?? ""} [${f.fields.join(",")}]`)).join(" ")}` : ""}\n`;
md += `\n## Polling / repeated calls\n\n`;
md += repeatedAll.length ? `| screen | endpoint | count | median gap ms | min gap ms |\n|---|---|---|---|---|\n` + repeatedAll.map((p) => `| ${p.screen} | ${code(p.endpoint)} | ${p.count} | ${p.medianGapMs} | ${p.minGapMs} |\n`).join("") : `(none detected)\n`;
const fr = data.frontendRefs ?? {};
md += `\n## Frontend code scan\n\n- scripts scanned: ${(fr.scripts ?? []).length} (${(fr.scripts ?? []).reduce((a, s) => a + s.bytes, 0)} bytes)\n- setInterval calls: ${fr.setIntervalCount ?? 0}${fr.intervalLiterals?.length ? ` (literal ms: ${fr.intervalLiterals.join(", ")})` : ""}; setTimeout literal ms: ${(fr.timeoutLiterals ?? []).join(", ") || "-"}\n- EventSource: ${fr.hasEventSource ? "yes" : "no"}, WebSocket: ${fr.hasWebSocket ? "yes" : "no"}\n- endpoints referenced in code: ${(fr.endpoints ?? []).length}\n`;
if (unobserved.length) md += `\n### Referenced in frontend code but NOT observed by the crawl\n\n${unobserved.map((e) => `- ${code(e.path)} (refs ${e.refs})`).join("\n")}\n`;
if (data.openapi) md += `\n## OpenAPI (${data.openapi.path}): ${data.openapi.operations.length} operations, ${unobservedOps.length} NOT observed\n\n${unobservedOps.map((o) => `- ${o.method} ${code(o.path)} — ${o.note}${o.code ? ` (${o.code.handler} → ${o.code.tables.join(", ")})` : ""}`).join("\n")}\n`;
md += `\n## Requests per screen\n\n`;
for (const s of screens) {
  const reqs = (s.requests ?? []).filter(isApiReq);
  if (!reqs.length) continue;
  md += `### ${s.persona}: ${s.label}\n\n| t(ms) | method | path | query | status | ms | request body | response (sample) |\n|---|---|---|---|---|---|---|---|\n`;
  const seen = new Map();
  const stepsAt = new Map((s.steps ?? []).map((st) => [st.t, st.label]));
  const all = s.requests ?? [];
  for (const r of reqs) {
    const idx = all.indexOf(r);
    if (stepsAt.has(idx)) md += `| — | **step** | ${esc(stepsAt.get(idx))} |  |  |  |  |  |\n`;
    const k = `${r.method} ${r.normPath}`;
    const nn = (seen.get(k) ?? 0) + 1;
    seen.set(k, nn);
    if (nn > 3) continue;
    md += `| ${r.t} | ${r.method} | ${code((r.origin || "") + r.path)} | ${code(fmtQuery(r.query))} | ${r.status ?? r.failed ?? ""} | ${r.ms ?? ""} | ${code(r.reqBody)} | ${code(r.resBody?.slice(0, 200))} |\n`;
  }
  const collapsed = [...seen].filter(([, nn]) => nn > 3);
  if (collapsed.length) md += `\n(collapsed repeats: ${collapsed.map(([k, nn]) => `${k} ×${nn}`).join(", ")})\n`;
  md += `\n`;
}
fs.writeFileSync(path.join(dir, "report.md"), md);

// ---- report.html ----
const inlineImages = !process.argv.includes("--no-inline");
const imgSrc = (rel) => { if (!rel) return null; if (!inlineImages) return rel; try { return "data:image/png;base64," + fs.readFileSync(path.join(dir, rel)).toString("base64"); } catch { return rel; } };
const model = {
  baseURL: data.baseURL, generatedAt: data.generatedAt, apiPrefix, totalApiReqs,
  screens: screens.map((s) => ({ ...s, path: (s.finalUrl ?? s.url ?? "").split("?")[0], screenshot: imgSrc(s.screenshot), steps: (s.steps ?? []).map((st) => ({ ...st, screenshot: imgSrc(st.screenshot) })), actors: [...new Set((s.requests ?? []).filter(isApiReq).map((r) => actorOf(r.normPath)).filter((a) => a !== "-"))] })),
  endpoints: epList.map((e) => ({ key: `${e.method} ${e.path}`, method: e.method, path: e.path, actor: e.actor, kind: e.kind, role: e.role, count: e.count, personas: [...e.personas], screens: [...e.screens], screenIds: [...e.screenIds], statuses: Object.fromEntries([...e.statuses]), medianMs: e.medianMs, medianBytes: e.medianBytes, sampleQuery: e.sampleQuery, sampleStatus: e.sampleStatus, sampleReq: e.sampleReq, sampleRes: e.sampleRes, sampleSchema: e.sampleSchema, allSamples: e.allSamples, hints: [...e.hints], errors: e.errors, code: e.code })),
  actors: actorList,
  system,
  findings,
  polling: pollingAll,
  repeated: repeatedAll,
  nav: navModel,
  storyboards: storyboards.map((sb) => ({ ...sb, parts: sb.parts.map((p) => ({ ...p, screenshot: imgSrc(p.screenshot) })) })),
  scan: fr,
  openapi: data.openapi ? { path: data.openapi.path, operations: data.openapi.operations, unobserved: unobservedOps } : null,
  unobservedRefs: unobserved,
  codeDir: cfgFile.codeDir ?? null,
  frontendDir: cfgFile.frontendDir ?? null,
  schema: schema ? { path: schema.path, tables: Object.fromEntries(Object.entries(schema.tables).map(([k, v]) => [k, { columns: v.columns.length, indexes: v.indexes.map((ix) => `${ix.name}(${ix.cols.join(",")})`) }])) } : null,
  dataModel,
};
// bench overlay (--run <measurements/<ts> | latest>): what to solve now, from an ops run + issues/PRs + annotations
{
  const cfgDir = data.config ? path.dirname(path.resolve(data.config)) : process.cwd();
  const repoRoot = cfgFile.repoRoot ? path.resolve(cfgDir, cfgFile.repoRoot) : (() => { try { return execSync("git rev-parse --show-toplevel", { cwd: cfgDir, stdio: ["ignore", "pipe", "ignore"] }).toString().trim(); } catch { return path.resolve(cfgDir, "../.."); } })();
  const ai = process.argv.indexOf("--run");
  const runDir = ai > 0 ? resolveRunDir(process.argv[ai + 1] === "latest" ? "latest" : path.resolve(process.argv[ai + 1]), repoRoot) : null;
  const pubDir = cfgFile.publishDir ? path.resolve(cfgDir, cfgFile.publishDir) : dir;
  const { bench, now, annotations, hosts } = loadBench({ runDir, repoRoot, codeDir: cfgFile.codeDir ? path.resolve(cfgDir, cfgFile.codeDir) : null, annotationsPath: cfgFile.annotations ? path.resolve(cfgDir, cfgFile.annotations) : undefined, opsReportBase: cfgFile.opsReportBase });
  if (bench) bench.flowsHtml = relLink(pubDir, bench.flowsHtml);
  Object.assign(model, { bench, now, annotations, hosts, tableUse: tableUseFromCode(cfgFile.codeDir ? path.resolve(cfgDir, cfgFile.codeDir) : null) ?? tableUse(model.endpoints), addedIndexes: addedIndexes(repoRoot), routes: routesFromCode(cfgFile.codeDir ? path.resolve(cfgDir, cfgFile.codeDir) : null), refreshedAt: new Date().toISOString() });
  model.screenWeights = screenWeights(model);
  if (bench) fs.writeFileSync(path.join(dir, "report.md"), withNow(fs.readFileSync(path.join(dir, "report.md"), "utf8"), model));
}
const html = renderHtml(model);
fs.writeFileSync(path.join(dir, "report.html"), html);
console.log(`${path.join(dir, "report.md")}\n${path.join(dir, "report.html")} (${(html.length / 1e6).toFixed(1)} MB${inlineImages ? ", images inlined" : ""})`);
// --publish: refresh <publishDir>/latest.* (same as analyze.mjs does after a crawl)
if (process.argv.includes("--publish") && cfgFile.publishDir) {
  const pub = path.resolve(path.dirname(data.config), cfgFile.publishDir);
  fs.mkdirSync(pub, { recursive: true });
  fs.copyFileSync(path.join(dir, "report.html"), path.join(pub, "latest.html"));
  fs.copyFileSync(path.join(dir, "report.md"), path.join(pub, "latest.md"));
  fs.writeFileSync(path.join(pub, "latest.txt"), `${cfgFile.name ?? new URL(data.baseURL).hostname} ${path.basename(dir)} ${dir} code=${codeRev}\n`);
  console.log(`published -> ${path.relative(process.cwd(), path.join(pub, "latest.html"))} (+ latest.md)`);
}
