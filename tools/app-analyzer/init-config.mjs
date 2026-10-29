// Build analyzer.config.json mechanically (task analyzer:init), or from a bare template (legacy).
//   node init-config.mjs --auto [--root ../..] [--kickoff <root>/docs/kickoff.md] [--out analyzer.config.json] [--force] [--no-routes | --routes-file <json>]
//     HOSTS (env) = "svc1 svc2 svc3" public IPs; the crawl target (resolve) is the 2nd host (never the bench target svc1).
//     Reads the ```analyzer block at the end of docs/kickoff.md (kickoff phase 1: domain, personas, initializeBody),
//     the router / cookies of the Go code, the schema and openapi under webapp/. Paths in the config are relative to
//     <root>/tools/app-analyzer (where the analyzer runs), wherever --out points.
//   node init-config.mjs <baseURL> [out]   (legacy: template with placeholders)
import fs from "node:fs";
import path from "node:path";
import { execFileSync } from "node:child_process";
import { fileURLToPath } from "node:url";

const argv = process.argv.slice(2);
const flag = (k) => argv.includes(k);
const opt = (k, d) => (argv.includes(k) ? argv[argv.indexOf(k) + 1] : d);
const here = path.dirname(fileURLToPath(import.meta.url));

if (!flag("--auto")) legacy();
else auto();

function auto() {
  const root = path.resolve(opt("--root", path.join(here, "..", "..")));
  const anaDir = path.join(root, "tools", "app-analyzer"); // config paths are relative to this
  const rel = (p) => (p ? path.relative(anaDir, p) : "");
  const out = opt("--out", "analyzer.config.json");
  const force = flag("--force");
  const notes = [];
  const warn = (m) => { notes.push(m); console.error(`warn: ${m}`); };
  if (fs.existsSync(out) && !force) {
    console.error(`${out} already exists; not overwriting (FORCE=1 to rebuild it)`);
    process.exit(1);
  }

  // ---- kickoff.md block (kickoff phase 1) ----
  const kickoff = opt("--kickoff", path.join(root, "docs", "kickoff.md"));
  let ko = {};
  if (fs.existsSync(kickoff)) {
    const m = [...fs.readFileSync(kickoff, "utf8").matchAll(/```analyzer\s*\n([\s\S]*?)```/g)].pop();
    if (m) {
      try { ko = JSON.parse(m[1]); } catch (e) { warn(`${kickoff}: the \`\`\`analyzer block is not JSON (${e.message}); ignored`); }
    } else warn(`${kickoff}: no \`\`\`analyzer block (kickoff phase 1 writes it); personas get no tokens`);
  } else warn(`${kickoff} not found`);

  // ---- code: dir with go.mod under webapp/ (webapp/go first) ----
  const webapp = path.join(root, "webapp");
  const findDir = (d, test, depth = 0) => {
    if (depth > 3 || !fs.existsSync(d)) return null;
    if (test(d)) return d;
    const subs = fs.readdirSync(d, { withFileTypes: true }).filter((x) => x.isDirectory() && !/node_modules|vendor|^\./.test(x.name)).sort((a, b) => (a.name === "go" ? -1 : b.name === "go" ? 1 : a.name.localeCompare(b.name)));
    for (const f of subs) {
      const r = findDir(path.join(d, f.name), test, depth + 1);
      if (r) return r;
    }
    return null;
  };
  const codeDir = findDir(webapp, (d) => fs.existsSync(path.join(d, "go.mod")));
  if (!codeDir) warn("no go.mod under webapp/ (codeDir empty)");
  const goFiles = [];
  const walk = (d) => { for (const f of fs.readdirSync(d, { withFileTypes: true })) { const p = path.join(d, f.name); if (f.isDirectory()) { if (!/node_modules|vendor|^\./.test(f.name)) walk(p); } else if (f.name.endsWith(".go") && !f.name.endsWith("_test.go")) goFiles.push(p); } };
  if (codeDir) walk(codeDir);
  const src = Object.fromEntries(goFiles.map((f) => [f, fs.readFileSync(f, "utf8")]));

  // ---- schema / openapi ----
  const sqlDir = path.join(webapp, "sql");
  let schemaPath = "";
  if (fs.existsSync(sqlDir)) {
    const sqls = fs.readdirSync(sqlDir).filter((f) => f.endsWith(".sql")).sort();
    const f = sqls.find((x) => /schema/i.test(x)) ?? sqls.find((x) => /CREATE\s+TABLE/i.test(fs.readFileSync(path.join(sqlDir, x), "utf8")));
    if (f) schemaPath = path.join(sqlDir, f);
  }
  if (!schemaPath) warn("no schema SQL under webapp/sql (schemaPath empty)");
  const openapi = ["openapi.yaml", "openapi.yml", "openapi.json"].map((f) => path.join(webapp, f)).find((f) => fs.existsSync(f)) ?? "";

  // ---- routes: net/http "GET /x", chi .Get("/x"), echo .GET("/x") ----
  const routes = [];
  for (const [f, s] of Object.entries(src)) {
    s.split("\n").forEach((line, i) => {
      let m = line.match(/HandleFunc\(\s*"(?:(GET|POST|PUT|DELETE|PATCH) )?([^"]+)"\s*,\s*([\w.]+)/);
      if (m) return routes.push({ method: m[1] ?? "ANY", path: m[2], handler: m[3], line, file: f, i });
      m = line.match(/\.(Get|Post|Put|Delete|Patch|GET|POST|PUT|DELETE|PATCH)\(\s*"([^"]+)"\s*,\s*([\w.]+)/);
      if (m) routes.push({ method: m[1].toUpperCase(), path: m[2], handler: m[3], line, file: f, i });
    });
  }
  if (!routes.length) warn("no routes found in the Go code (initialize / groups / authCheck not set)");
  const initRoute = routes.find((r) => /\/initialize$/.test(r.path) && r.method !== "GET");
  const top = new Map();
  for (const r of routes) { const seg = r.path.split("/")[1]; if (seg) top.set(seg, (top.get(seg) ?? 0) + 1); }
  const apiSeg = top.has("api") ? "api" : [...top.entries()].sort((a, b) => b[1] - a[1])[0]?.[0] ?? "api";
  const groups = [...new Set(routes.map((r) => r.path.split("/")).filter((p) => p[1] === apiSeg && p.length >= 4 && !/[{:]/.test(p[2])).map((p) => p[2]))];

  // ---- cookie names from the code ----
  const cookies = new Set();
  for (const s of Object.values(src)) {
    for (const m of s.matchAll(/\.Cookie\(\s*"([^"]+)"/g)) cookies.add(m[1]);
    for (const m of s.matchAll(/(?:SetCookie|http\.Cookie|sessions\.)[\s\S]{0,200}?Name:\s*"([^"]+)"/g)) cookies.add(m[1]);
  }
  const cookieFor = (g) => [...cookies].find((c) => c === g || c.startsWith(`${g}_`) || c.endsWith(`_${g}`)) ?? [...cookies].find((c) => c.includes(g)) ?? null;

  // ---- authCheck: one authenticated GET per group (no path params; prefer auth-wrapped lines, handlers that read no query) ----
  const handlerBody = (h) => {
    const n = h.split(".").pop();
    for (const s of Object.values(src)) {
      const i = s.search(new RegExp(`\\nfunc (\\([^)]*\\) )?${n}\\(`));
      if (i >= 0) { const j = s.indexOf("\nfunc ", i + 1); return s.slice(i, j < 0 ? undefined : j); }
    }
    return "";
  };
  const authFor = (g) => {
    const c = routes.filter((r) => r.method === "GET" && r.path.startsWith(`/${apiSeg}/${g}/`) && !/[{:*]/.test(r.path));
    if (!c.length) return null;
    const score = (r) => (/auth/i.test(r.line) ? 0 : 2) + (/Query\(\)|QueryParam\(|FormValue\(/.test(handlerBody(r.handler)) ? 1 : 0);
    const best = c.slice().sort((a, b) => score(a) - score(b) || a.i - b.i)[0];
    return { method: "GET", path: best.path, expectStatus: 200 };
  };

  // ---- domain / baseURL / resolve (2nd of HOSTS) ----
  let domain = ko.domain ?? "";
  if (!domain) {
    const names = [];
    const walkN = (d) => { if (!fs.existsSync(d)) return; for (const f of fs.readdirSync(d, { withFileTypes: true })) { const p = path.join(d, f.name); if (f.isDirectory()) walkN(p); else { try { for (const m of fs.readFileSync(p, "utf8").matchAll(/server_name\s+([^;]+);/g)) names.push(...m[1].trim().split(/\s+/)); } catch {} } } };
    walkN(path.join(root, "etc", "nginx", "sites-available"));
    walkN(path.join(root, "etc", "nginx", "conf.d"));
    domain = names.find((n) => n !== "_" && n !== "localhost" && !n.startsWith("*")) ?? "";
    warn(domain ? `domain from nginx server_name (${domain}); no "domain" in the kickoff block` : "no domain (kickoff block or nginx server_name): baseURL is a placeholder");
  }
  const scheme = ko.scheme ?? "https";
  const baseURL = `${scheme}://${domain || "APP_HOST"}`;
  const hosts = (process.env.HOSTS ?? "").trim().split(/\s+/).filter(Boolean);
  const target = hosts[1] ?? hosts[0] ?? "SERVER_IP";
  if (!hosts[1]) warn(hosts[0] ? "HOSTS has one host: the crawl points at svc1 (the bench target)" : "HOSTS is empty: resolve = SERVER_IP placeholder");
  const resolve = domain ? { [domain]: target, ...(ko.subdomains ? { [`*.${domain}`]: target } : {}) } : {};

  // ---- seeds from the SPA bundle (npm run routes; best effort) ----
  let spa = [];
  if (opt("--routes-file")) spa = JSON.parse(fs.readFileSync(opt("--routes-file"), "utf8")); // saved npm run routes output (offline / tests)
  else if (!flag("--no-routes") && domain && hosts.length) {
    try {
      spa = JSON.parse(execFileSync("node", [path.join(here, "routes-from-bundle.mjs"), baseURL, "--ip", target], { encoding: "utf8", timeout: 20000, stdio: ["ignore", "pipe", "ignore"] }));
    } catch (e) { warn(`npm run routes failed (${(e.message ?? "").split("\n")[0]}): persona seeds = ["/"]`); }
  }
  spa = spa.filter((p) => typeof p === "string" && !/[:*]/.test(p));
  const used = new Set();
  const seedsFor = (keys) => {
    const s = spa.filter((p) => keys.some((k) => k && (p === `/${k}` || p.startsWith(`/${k}/`))) && !/\/(login|register|signin|signup)$/i.test(p));
    s.forEach((p) => used.add(p));
    return s;
  };

  // ---- personas (kickoff block) ----
  const kp = Array.isArray(ko.personas) ? ko.personas : [];
  const personas = [];
  const actorNames = Object.fromEntries(groups.map((g) => [g, g]));
  for (const p of kp) {
    const g = p.group ?? (groups.includes(p.name) ? p.name : groups.find((x) => p.cookie && cookieFor(x) === p.cookie)) ?? null;
    if (g) actorNames[g] = p.name;
    const per = { name: p.name };
    if (p.viewport) per.viewport = p.viewport;
    if (p.login) per.login = { method: "POST", ...p.login };
    const cname = p.cookie ?? (g ? cookieFor(g) : cookies.size === 1 ? [...cookies][0] : null);
    if (p.token) {
      if (cname) per.cookies = [{ name: cname, value: p.token }];
      else warn(`persona ${p.name}: token but no cookie name (set "cookie" in the kickoff block)`);
    }
    if (p.headers) per.headers = p.headers;
    const s = p.seeds ?? seedsFor([p.seedPrefix, g, p.name]);
    per.seeds = s.length ? s : ["/"];
    const ac = p.authCheck ?? (g ? authFor(g) : null);
    if (ac) per.authCheck = ac;
    if (p.vars) per.vars = p.vars;
    if (p.source) per["//source"] = p.source;
    personas.push(per);
  }
  if (!kp.some((p) => p.name === "guest")) personas.unshift({ name: "guest", seeds: ["/", ...spa.filter((p) => p !== "/" && !used.has(p))] });
  if (!kp.length) warn("no personas in the kickoff block: only guest (add tokens / logins to docs/kickoff.md, then FORCE=1)");

  const cfg = {
    "//": `generated by task analyzer:init${ko.source ? ` (kickoff block source: ${typeof ko.source === "string" ? ko.source : JSON.stringify(ko.source)})` : ""}. Crawl ${target} (2nd of HOSTS), never the bench target svc1. Cookies in the code: ${[...cookies].join(", ") || "none"}.${notes.length ? " Warnings: " + notes.join("; ") : ""}`,
    baseURL,
    resolve,
    name: domain.split(".")[0] || "app",
    publishDir: "../../docs/reports",
    outDir: "out",
    dwellMs: 5000,
    maxPages: 25,
    navTimeoutMs: 20000,
    headless: true,
    apiPrefixes: [`/${apiSeg}/`],
    initialize: ko.initialize ?? initRoute?.path ?? "",
    initializeBody: ko.initializeBody ?? {},
    openapiPath: rel(openapi),
    openapiPrefix: `/${apiSeg}`,
    codeDir: rel(codeDir),
    schemaPath: rel(schemaPath),
    frontendDir: "",
    actorNames,
    stateKeys: ["status", "state"],
    personas,
  };
  if (!cfg.initialize) delete cfg.initialize;
  fs.writeFileSync(out, JSON.stringify(cfg, null, 2) + "\n");
  console.log(`wrote ${out}: ${baseURL} -> ${target}, ${routes.length} routes, groups [${groups.join(", ")}], cookies [${[...cookies].join(", ")}], personas [${personas.map((p) => p.name).join(", ")}], ${spa.length} SPA routes${notes.length ? `, ${notes.length} warning(s)` : ""}`);
}

function legacy() {
  const baseURL = argv[0];
  const out = argv[1] ?? "analyzer.config.json";
  if (!baseURL) {
    console.error("usage: node init-config.mjs --auto [--root ../..] [--kickoff docs/kickoff.md] [--out f] [--force] [--no-routes]  |  node init-config.mjs <baseURL> [out]");
    process.exit(1);
  }
  if (fs.existsSync(out)) {
    console.error(`${out} already exists; not overwriting`);
    process.exit(1);
  }
  const host = new URL(baseURL).hostname;
  const cfg = {
    "//": "resolve: hostname pattern -> IP (replaces /etc/hosts; Chromium host-resolver-rules). personas: login (API call before crawling) or cookies/headers. seeds from the frontend routes. Remove what you don't need. Prefer task analyzer:init (fills this from the code + the kickoff block).",
    baseURL,
    initialize: "/api/initialize",
    "//initialize": "POSTed once before the crawl (fresh data; a host that never served a bench may have an empty DB). Remove when the target is the bench host mid-contest.",
    initializeBody: {},
    "//initializeBody": "JSON body of the initialize POST (crawl + task smoke INIT=1). Fill it when the handler reads one, e.g. ISUCON14 {\"payment_server\": \"http://<bench>:12346\"}; {} would blank that setting.",
    resolve: { [`*.${host.split(".").slice(1).join(".")}`]: "SERVER_IP", [host]: "SERVER_IP" },
    name: "app",
    publishDir: "../../docs/reports",
    outDir: "out",
    dwellMs: 4000,
    settleMs: 800,
    concurrency: 3,
    maxPages: 30,
    navTimeoutMs: 20000,
    headless: true,
    apiPrefixes: ["/api/"],
    openapiPath: "",
    codeDir: "",
    schemaPath: "",
    frontendDir: "",
    actorNames: { app: "user" },
    stateKeys: ["status", "state", "phase"],
    openapiPrefix: "/api",
    personas: [
      { name: "guest", seeds: ["/"] },
      {
        name: "user",
        viewport: { width: 390, height: 844 },
        login: { path: "/api/login", method: "POST", body: { username: "USER", password: "PASS" } },
        cookies: [],
        headers: {},
        seeds: ["/"],
        authCheck: { method: "GET", path: "/api/SOME_AUTHED_ENDPOINT", expectStatus: 200 },
        vars: {},
        flows: [],
      },
    ],
  };
  fs.writeFileSync(out, JSON.stringify(cfg, null, 2));
  console.log(`wrote ${out}. Next: edit personas, then npm run doctor -- ${out}`);
}
