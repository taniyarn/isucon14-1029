// Preflight checks before a crawl. Usage: node doctor.mjs [analyzer.config.json]
import fs from "node:fs";
import dns from "node:dns/promises";
import path from "node:path";

const cfgPath = process.argv[2] ?? "analyzer.config.json";
const results = [];
const ok = (name, detail = "") => results.push({ ok: true, name, detail });
const ng = (name, detail = "") => results.push({ ok: false, name, detail });

let cfg = null;
try {
  cfg = JSON.parse(fs.readFileSync(cfgPath, "utf8"));
  ok("config", cfgPath);
} catch (e) {
  ng("config", `${cfgPath}: ${e.message}`);
}

if (cfg) {
  // DNS / hosts — or cfg.resolve (host-resolver-rules inside the browser; no /etc/hosts needed)
  let host = null;
  const resolverArgs = cfg.resolve && Object.keys(cfg.resolve).length ? [`--host-resolver-rules=${Object.entries(cfg.resolve).map(([h, ip]) => `MAP ${h} ${ip}`).join(", ")}`] : [];
  if (resolverArgs.length) ok("dns", `resolve rules: ${Object.entries(cfg.resolve).map(([h, ip]) => `${h} -> ${ip}`).join(", ")}`);
  else {
    try {
      host = new URL(cfg.baseURL).hostname;
      const a = await dns.lookup(host);
      ok("dns", `${host} -> ${a.address}${/^(10\.|172\.(1[6-9]|2\d|3[01])\.|192\.168\.|127\.)/.test(a.address) ? " (private: check this is intended)" : ""}`);
    } catch (e) {
      ng("dns", `${host}: ${e.message} (add it to /etc/hosts, or set "resolve" in the config)`);
    }
  }

  // browser + reachability + auth, all through playwright so TLS errors are ignored like the crawl does
  let chromium = null;
  try {
    ({ chromium } = await import("playwright"));
  } catch (e) {
    ng("playwright", `not installed: run npm run setup (${e.message})`);
  }
  if (chromium) {
    let browser = null;
    try {
      browser = await chromium.launch({ args: resolverArgs });
      ok("browser", "chromium launches");
    } catch (e) {
      ng("browser", `cannot launch: run npx playwright install chromium chromium-headless-shell (${e.message.split("\n")[0]})`);
    }
    if (browser) {
      // reachability through a real page (goes through the resolver rules; ctx.request would not)
      {
        const c = await browser.newContext({ ignoreHTTPSErrors: true });
        const page = await c.newPage();
        try {
          const r = await page.goto(cfg.baseURL, { waitUntil: "domcontentloaded", timeout: 15000 });
          const st = r?.status() ?? 0;
          (st && st < 400 ? ok : ng)("reachable", `${cfg.baseURL} -> ${st} (${(await page.content()).length} bytes)`);
        } catch (e) {
          ng("reachable", `${cfg.baseURL}: ${e.message.split("\n")[0]} (security group? hosts/resolve? https?)`);
        }
        await c.close();
      }
      const inPage = (page, path, method = "GET", body) => page.evaluate(async ({ path, method, body }) => {
        const res = await fetch(path, { method, headers: body ? { "content-type": "application/json" } : {}, body: body ? JSON.stringify(body) : undefined, credentials: "include" });
        return { status: res.status, text: (await res.text()).slice(0, 160) };
      }, { path, method, body });
      for (const p of cfg.personas ?? []) {
        const c = await browser.newContext({ ignoreHTTPSErrors: true, extraHTTPHeaders: p.headers ?? {} });
        if (p.cookies?.length) await c.addCookies(p.cookies.map((k) => ({ ...k, url: cfg.baseURL })));
        const page = await c.newPage();
        try {
          await page.goto(cfg.baseURL, { waitUntil: "domcontentloaded", timeout: 15000 });
          if (p.login?.path) {
            const r = await inPage(page, p.login.path, p.login.method ?? "POST", p.login.body);
            (r.status < 300 ? ok : ng)(`login:${p.name}`, `${p.login.method ?? "POST"} ${p.login.path} -> ${r.status}${r.status >= 300 ? " " + r.text + (r.status === 401 && !cfg.initialize ? "  (a host that never ran a bench may have an empty DB: set \"initialize\": \"/api/initialize\" in the config)" : "") : ""}`);
          }
          if (p.authCheck) {
            const r = await inPage(page, p.authCheck.path, p.authCheck.method ?? "GET");
            const want = p.authCheck.expectStatus ?? 200;
            (r.status === want ? ok : ng)(`auth:${p.name}`, `${p.authCheck.method ?? "GET"} ${p.authCheck.path} -> ${r.status} (expected ${want})`);
          } else {
            await page.goto(new URL((p.seeds ?? ["/"])[0], cfg.baseURL).toString(), { waitUntil: "domcontentloaded", timeout: 15000 });
            await page.waitForTimeout(800);
            const final = new URL(page.url()).pathname;
            const redirectedToAuth = /login|register|signin|auth/i.test(final) && !/login|register|signin|auth/i.test((p.seeds ?? ["/"])[0]);
            (redirectedToAuth ? ng : ok)(`auth:${p.name}`, `${(p.seeds ?? ["/"])[0]} -> ${final}${redirectedToAuth ? " (looks like a login redirect: token invalid?)" : ""}`);
          }
        } catch (e) {
          ng(`auth:${p.name}`, e.message.split("\n")[0]);
        }
        for (const f of p.flows ?? []) (fs.existsSync(f.script) ? ok : ng)(`flow:${p.name}/${f.name}`, f.script);
        await c.close();
      }
      await browser.close();
    }
  }
  if (cfg.openapiPath) (fs.existsSync(cfg.openapiPath) ? ok : ng)("openapi", cfg.openapiPath);
  else ok("openapi", "(not configured; unobserved-ops diff will be skipped)");
  (fs.existsSync(path.join("node_modules", "playwright")) ? ok : ng)("deps", "node_modules/playwright");
}

const w = Math.max(...results.map((r) => r.name.length));
for (const r of results) console.log(`${r.ok ? "OK " : "NG "} ${r.name.padEnd(w)}  ${r.detail}`);
const bad = results.filter((r) => !r.ok).length;
console.log(bad ? `\n${bad} check(s) failed` : "\nall checks passed");
process.exit(bad ? 1 : 0);
