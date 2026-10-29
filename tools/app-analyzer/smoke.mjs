// Smoke test / response diff without a browser. Replays the API calls the crawler observed (out/<latest>/data.json)
// against one host (smoke) or two hosts (compare main vs a branch), using the personas' login from analyzer.config.json.
//   node smoke.mjs analyzer.config.json --ip <svc1 ip> [--initialize] [--dns]
//   node smoke.mjs analyzer.config.json --ip <svc1 ip> --compare <svc2 ip>      # same requests to both, diff JSON bodies
// Exit code 1 when: initialize != 200, initialize's "language" != the expected implementation, login != 2xx, any 5xx,
// or (compare) any body differs. Last line: "checked N, failed M".
// Expected language: --lang <x>, else cfg.expectLanguage, else the first word of LANGS (.env), else "go" ("" = don't check).
// Catches another language's unit answering on the app port (smoke used to pass with isuride-node holding :8080).
import fs from "node:fs";
import https from "node:https";
import http from "node:http";
import path from "node:path";
import { execSync } from "node:child_process";
import { latestOut } from "./outdir.mjs";

const args = process.argv.slice(2);
const cfgPath = args.find((a) => !a.startsWith("--")) ?? "analyzer.config.json";
const opt = (k) => (args.includes(k) ? args[args.indexOf(k) + 1] : null);
// No config yet (kickoff phase 2 writes it; 2026-10-28 hub:ship ran smoke before it existed and died on ENOENT, which
// stopped the ship's bench todo): minimal probe instead — GET / and the app answers without 5xx. initialize is skipped
// (its body is unknown without the config: ISUCON14's {} would blank payment_server); the bench initializes anyway.
let cfg, minimal = "";
try {
  cfg = JSON.parse(fs.readFileSync(cfgPath, "utf8"));
  if (!cfg.baseURL || /SERVER_IP|example\.(com|org)|<[^>]*>/.test(cfg.baseURL)) minimal = `${cfgPath} has no real baseURL (${cfg.baseURL ?? "missing"})`;
} catch (e) {
  minimal = e.code === "ENOENT" ? `${cfgPath} does not exist yet (kickoff phase 2 writes it)` : `${cfgPath} unreadable: ${e.message}`;
  cfg = {};
}
if (minimal) {
  const host = process.env.SMOKE_HOST || "";
  cfg = { baseURL: `http://${host || opt("--ip") || "127.0.0.1"}/`, personas: [{ name: "guest" }], smokePaths: ["/"], minimal: true };
  console.log(`!! minimal smoke: ${minimal} — only GET / (expect < 500)${args.includes("--initialize") ? "; initialize NOT run (body unknown)" : ""}. Full smoke once the config is on main`);
}
const base = new URL(cfg.baseURL);
const ipA = opt("--ip") ?? Object.values(cfg.resolve ?? {})[0];
const ipB = opt("--compare");
const doInit = args.includes("--initialize") && !cfg.minimal;
const doDns = args.includes("--dns") && !cfg.minimal;
const maxCalls = Number(opt("--max") ?? 30);
const expectLang = opt("--lang") ?? cfg.expectLanguage ?? ((process.env.LANGS ?? "").trim().split(/\s+/)[0] || "go");
const langNorm = (l) => String(l ?? "").toLowerCase().replace(/^golang$/, "go").replace(/^(nodejs|node\.js|typescript|ts|js)$/, "node");
if (!ipA) { console.error("need --ip (or resolve in the config)"); process.exit(2); }

function request(ip, method, path, body, cookie, timeoutMs = 20000) {
  return new Promise((resolve) => {
    const url = new URL(path, base);
    const mod = url.protocol === "https:" ? https : http;
    const data = body ? JSON.stringify(body) : null;
    const headers = { Host: url.hostname, Accept: "application/json" };
    if (data) { headers["Content-Type"] = "application/json"; headers["Content-Length"] = Buffer.byteLength(data); }
    if (cookie) headers.Cookie = cookie;
    const t = Date.now();
    const req = mod.request({ host: ip, port: url.port || (url.protocol === "https:" ? 443 : 80), method, path: url.pathname + url.search, headers, servername: url.hostname, rejectUnauthorized: false, timeout: timeoutMs }, (res) => {
      const chunks = [];
      res.on("data", (c) => chunks.push(c));
      res.on("end", () => resolve({ status: res.statusCode, ms: Date.now() - t, body: Buffer.concat(chunks).toString("utf8"), setCookie: res.headers["set-cookie"] ?? [] }));
    });
    req.on("error", (e) => resolve({ status: 0, ms: Date.now() - t, body: String(e), setCookie: [] }));
    req.on("timeout", () => req.destroy(new Error("timeout")));
    if (data) req.write(data);
    req.end();
  });
}
const cookieJar = (setCookie, prev = "") => { const m = new Map(prev.split("; ").filter(Boolean).map((kv) => kv.split("=", 2))); for (const sc of setCookie) { const [kv] = sc.split(";"); const [k, v] = kv.split("=", 2); m.set(k, v); } return [...m].map(([k, v]) => `${k}=${v}`).join("; "); };

// requests to replay: observed GETs per persona (first sample path per endpoint), plus cfg.smokePaths
let dataJson = null;
if (!cfg.minimal) try { dataJson = JSON.parse(fs.readFileSync(path.join(latestOut(cfg, cfgPath), "data.json"), "utf8")); } catch {}
const plan = [];
for (const p of cfg.personas ?? [{ name: "guest" }]) {
  const seen = new Set();
  const calls = [];
  const src = dataJson?.personas?.find((x) => x.name === p.name);
  for (const pg of [...(src?.pages ?? []), ...(src?.flows ?? [])]) for (const r of pg.requests ?? []) {
    if (r.method !== "GET" || !r.path.startsWith((cfg.apiPrefixes ?? ["/api/"])[0]) || r.origin) continue;
    if (seen.has(r.normPath) || calls.length >= maxCalls) continue;
    seen.add(r.normPath); calls.push(r.path + (r.query ? "?" + new URLSearchParams(r.query).toString() : ""));
  }
  for (const sp of cfg.smokePaths ?? []) if (!calls.includes(sp)) calls.push(sp);
  plan.push({ persona: p, calls });
}

let bad = 0, checked = 0;
const tally = (ok) => { checked++; if (!ok) bad++; return ok; };
const norm = (s) => { try { return JSON.stringify(JSON.parse(s), (k, v) => (/(_at|At|time|Time|timestamp|expires)$/.test(k) ? "<t>" : v)); } catch { return s; } };
async function runOn(ip, label) {
  const out = { label, results: [] };
  if (doInit) { const r = await request(ip, "POST", cfg.initialize ?? "/api/initialize", cfg.initializeBody ?? {}, null, (cfg.initializeTimeoutMs ?? 120000)); console.log(`[${label}] initialize -> ${r.status} in ${(r.ms / 1000).toFixed(1)}s`); tally(r.status === 200);
    if (expectLang && r.status === 200) {
      let lang; try { lang = JSON.parse(r.body).language; } catch {}
      if (lang === undefined) console.log(`[${label}] initialize: no "language" in the response (${r.body.slice(0, 60).replace(/\n/g, " ")}) - implementation not checked`);
      else if (tally(langNorm(lang) === langNorm(expectLang))) console.log(`[${label}] implementation: language=${lang} (expected ${expectLang}) ok`);
      else console.log(`[${label}] !! implementation: language=${lang}, expected ${expectLang} - another language's unit holds the app port? (task host:diag: listening)`);
    }
  }
  for (const { persona, calls } of plan) {
    let cookie = "";
    if (persona.login?.path) { const r = await request(ip, persona.login.method ?? "POST", persona.login.path, persona.login.body, null); cookie = cookieJar(r.setCookie); console.log(`[${label}] ${persona.name}: login -> ${r.status} (${r.ms}ms)`); tally(r.status < 300); }
    else if (persona.cookies?.length) cookie = persona.cookies.map((c) => `${c.name}=${c.value}`).join("; ");
    for (const path of calls) {
      const r = await request(ip, "GET", path, null, cookie);
      out.results.push({ persona: persona.name, path, status: r.status, ms: r.ms, body: r.body });
      tally(!(r.status >= 500 || r.status === 0));
      if (!ipB) console.log(`[${label}] ${persona.name}: GET ${path} -> ${r.status} ${r.ms}ms${r.status >= 400 ? " " + r.body.slice(0, 80).replace(/\n/g, " ") : ""}`);
    }
    // cfg.smoke: explicit requests incl. writes, e.g. [{"persona":"viewer","method":"POST","path":"/api/register","body":{...},"expect":201}]
    for (const sm of (cfg.smoke ?? []).filter((x) => !x.persona || x.persona === persona.name)) {
      const body = typeof sm.body === "string" ? JSON.parse(sm.body.replace(/\$\{rand\}/g, String(Date.now() % 1000000))) : sm.body;
      const r = await request(ip, sm.method ?? "GET", sm.path, body, cookie);
      const want = sm.expect ?? null;
      const okStatus = want ? r.status === want : r.status < 400;
      out.results.push({ persona: persona.name, path: `${sm.method ?? "GET"} ${sm.path}`, status: r.status, ms: r.ms, body: r.body });
      tally(okStatus);
      if (!ipB) console.log(`[${label}] ${persona.name}: ${sm.method ?? "GET"} ${sm.path} -> ${r.status} ${r.ms}ms${okStatus ? "" : " !! expected " + (want ?? "<400") + " " + r.body.slice(0, 100).replace(/\n/g, " ")}`);
    }
  }
  return out;
}
if (doDns) {
  for (const ip of [ipA, ipB].filter(Boolean)) {
    try { const o = execSync(`dig +short +time=2 +tries=1 @${ip} ${base.hostname}`, { encoding: "utf8" }).trim(); console.log(`[${ip}] dig ${base.hostname} -> ${o || "(no answer)"}`); tally(!!o); } catch (e) { console.log(`[${ip}] dig failed: ${String(e.message).split("\n")[0]}`); tally(false); }
  }
}
const A = await runOn(ipA, "A " + ipA);
if (ipB) {
  const B = await runOn(ipB, "B " + ipB);
  let diffs = 0;
  for (let i = 0; i < A.results.length; i++) {
    const a = A.results[i], b = B.results[i];
    const same = a.status === b.status && norm(a.body) === norm(b.body);
    if (!tally(same)) { diffs++; console.log(`DIFF ${a.persona} GET ${a.path}: A ${a.status} ${a.ms}ms vs B ${b.status} ${b.ms}ms`); if (a.status === b.status) console.log(`   A: ${a.body.slice(0, 160).replace(/\n/g, " ")}\n   B: ${b.body.slice(0, 160).replace(/\n/g, " ")}`); }
    else console.log(`same ${a.persona} GET ${a.path}: ${a.status} A ${a.ms}ms / B ${b.ms}ms`);
  }
  console.log(`${diffs} differing response(s) of ${A.results.length} (timestamps ignored)`);
  if (A.results.length === 0) { console.log("nothing was compared: no observed GETs in <outDir>/<latest>/data.json and no smokePaths/smoke entries in the config — run the analyzer (or add smokePaths) first"); tally(false); }
}
console.log(`${bad ? "!! " : ""}checked ${checked}, failed ${bad}`);
process.exit(bad ? 1 : 0);
