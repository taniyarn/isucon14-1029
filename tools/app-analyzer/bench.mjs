// Bench overlay for the app-analysis report: joins one ops run (measurements/<ts>/report.json + actions.json, written by
// isumeasure report) with the repo state (gh issues / PRs, docs/app-annotations.json, the code's host-bound side effects)
// into model.bench / model.now / model.annotations / model.hosts. Used by report.mjs (--run) and refresh.mjs (no crawl).
// Everything here is cheap: a few JSON files, one `gh issue list`, one `gh pr list`, one regex pass over the Go files.
import fs from "node:fs";
import path from "node:path";
import { execFileSync } from "node:child_process";

const readJSON = (p) => { try { return JSON.parse(fs.readFileSync(p, "utf8")); } catch { return null; } };
export const paramKey = (k) => String(k ?? "").replace(/:\w+/g, ":id").replace(/\{[^}]+\}/g, ":id");
const pathOf = (k) => { const i = k.indexOf(" "); return i >= 0 ? k.slice(i + 1) : k; };

// run dir: explicit, or "latest" = newest measurements/<ts> that has a report.json
export function resolveRunDir(run, repoRoot) {
  if (!run) return null;
  if (run !== "latest") return path.resolve(run);
  const base = path.join(repoRoot, "measurements");
  try {
    const ds = fs.readdirSync(base).filter((d) => fs.existsSync(path.join(base, d, "report.json"))).sort();
    return ds.length ? path.join(base, ds[ds.length - 1]) : null;
  } catch { return null; }
}

const ghJSON = (args, cwd) => {
  try { return JSON.parse(execFileSync("gh", args, { cwd, stdio: ["ignore", "pipe", "ignore"], timeout: 8000 }).toString()); } catch { return null; }
};

// side effects that tie a handler to one host (files, process-local state, external commands) — T26 / IMPLEMENTATION-NOTES §4
const EFFECTS = [
  ["exec", /exec\.Command\(/],
  ["file write", /os\.(WriteFile|Create|OpenFile)\(|ioutil\.WriteFile\(/],
  ["memory cache", /sync\.Map\b|\bcmap\.|\w+Cache\.(Store|Load|Set|Get|Delete)\(|\bcache\[/],
  ["mutex", /\.(Lock|RLock)\(\)/],
  ["goroutine", /\bgo func\(|\bgo \w+\(/],
  ["external HTTP", /http\.(Get|Post|NewRequest)\(/],
];
export function scanHosts(codeDir) {
  if (!codeDir || !fs.existsSync(codeDir)) return [];
  const files = [];
  const walk = (d) => { for (const f of fs.readdirSync(d, { withFileTypes: true })) { if (f.isDirectory()) { if (!/node_modules|vendor|^\./.test(f.name)) walk(path.join(d, f.name)); } else if (f.name.endsWith(".go") && !f.name.endsWith("_test.go")) files.push(path.join(d, f.name)); } };
  walk(codeDir);
  const funcs = {}, routes = [];
  for (const f of files) {
    const src = fs.readFileSync(f, "utf8");
    for (const m of src.matchAll(/\.(GET|POST|PUT|DELETE|PATCH|Get|Post|Put|Delete|Patch)\("([^"]+)",\s*(\w+)/g)) routes.push({ key: `${m[1].toUpperCase()} ${paramKey(m[2])}`, fn: m[3] });
    for (const m of src.matchAll(/HandleFunc\("(\w+) ([^"]+)",\s*(\w+)/g)) routes.push({ key: `${m[1]} ${paramKey(m[2])}`, fn: m[3] });
    const decl = [...src.matchAll(/^func (?:\([^)]*\) )?(\w+)\(/gm)];
    decl.forEach((m, i) => { const end = i + 1 < decl.length ? decl[i + 1].index : src.length; funcs[m[1]] = { body: src.slice(m.index, end), file: path.relative(codeDir, f), line: src.slice(0, m.index).split("\n").length }; });
  }
  // one level of helpers: a handler inherits the effects of the functions it calls
  const effectsOf = (name, depth = 0) => {
    const fn = funcs[name]; if (!fn) return new Set();
    const s = new Set(EFFECTS.filter(([, re]) => re.test(fn.body)).map(([k]) => k));
    if (depth < 1) for (const m of fn.body.matchAll(/\b(\w+)\(/g)) if (m[1] !== name && funcs[m[1]]) for (const e of effectsOf(m[1], depth + 1)) s.add(e);
    return s;
  };
  return routes.map((r) => ({ key: r.key, handler: r.fn, file: funcs[r.fn]?.file, line: funcs[r.fn]?.line, effects: [...effectsOf(r.fn)] })).filter((r) => r.effects.length);
}

// endpoint → { table: "R" | "W" } from the SQL literals in each handler (+ one level of helpers), read from the current code
// (covers write endpoints the crawler never triggered, and follows the code as it changes during the contest)
export function tableUseFromCode(codeDir) {
  if (!codeDir || !fs.existsSync(codeDir)) return null;
  const files = [];
  const walk = (d) => { for (const f of fs.readdirSync(d, { withFileTypes: true })) { if (f.isDirectory()) { if (!/node_modules|vendor|^\./.test(f.name)) walk(path.join(d, f.name)); } else if (f.name.endsWith(".go") && !f.name.endsWith("_test.go")) files.push(path.join(d, f.name)); } };
  walk(codeDir);
  const funcs = {}, routes = [];
  for (const f of files) {
    const src = fs.readFileSync(f, "utf8");
    for (const m of src.matchAll(/\.(GET|POST|PUT|DELETE|PATCH|Get|Post|Put|Delete|Patch)\("([^"]+)",\s*(\w+)/g)) routes.push({ key: `${m[1].toUpperCase()} ${paramKey(m[2])}`, fn: m[3] });
    for (const m of src.matchAll(/HandleFunc\("(\w+) ([^"]+)",\s*(\w+)/g)) routes.push({ key: `${m[1]} ${paramKey(m[2])}`, fn: m[3] });
    const decl = [...src.matchAll(/^func (?:\([^)]*\) )?(\w+)\(/gm)];
    decl.forEach((m, i) => { funcs[m[1]] = src.slice(m.index, i + 1 < decl.length ? decl[i + 1].index : src.length); });
  }
  const sqlRe = /`([^`]*)`|"((?:[^"\\]|\\.)*)"/g;
  const use = (body, m) => {
    for (const x of body.matchAll(sqlRe)) {
      const q = (x[1] ?? x[2] ?? "").replace(/\s+/g, " ");
      if (!/^\s*\(?\s*(SELECT|INSERT|UPDATE|DELETE|REPLACE|WITH)\b/i.test(q)) continue;
      const w = q.match(/^\s*(?:INSERT\s+(?:IGNORE\s+)?INTO|REPLACE\s+INTO|UPDATE|DELETE\s+FROM)\s+`?(\w+)`?/i);
      if (w) m[w[1]] = "W";
      for (const r of q.matchAll(/\b(?:FROM|JOIN)\s+`?(\w+)`?/gi)) if (!m[r[1]]) m[r[1]] = "R";
    }
  };
  const out = {};
  for (const r of routes) {
    const body = funcs[r.fn]; if (!body) continue;
    const m = {}, seen = new Set([r.fn]);
    const visit = (b, depth) => {
      use(b, m);
      if (depth >= 2) return;
      for (const c of b.matchAll(/\b(\w+)\(/g)) if (!seen.has(c[1]) && funcs[c[1]]) { seen.add(c[1]); visit(funcs[c[1]], depth + 1); }
    };
    visit(body, 0);
    if (Object.keys(m).length) out[r.key] = m;
  }
  return out;
}

// indexes added during the contest (webapp/sql/indexes/*.sql: ALTER TABLE t ADD INDEX name (cols))
export function addedIndexes(repoRoot) {
  const dir = path.join(repoRoot, "webapp/sql/indexes");
  const out = {};
  try {
    for (const f of fs.readdirSync(dir).filter((x) => x.endsWith(".sql"))) {
      const src = fs.readFileSync(path.join(dir, f), "utf8");
      for (const m of src.matchAll(/ALTER\s+TABLE\s+`?(\w+)`?\s+ADD\s+(?:UNIQUE\s+)?(?:INDEX|KEY)\s+`?(\w+)`?\s*\(([^)]*)\)/gi)) (out[m[1]] ??= []).push(`${m[2]}(${m[3].replace(/[\s`]/g, "")})`);
      for (const m of src.matchAll(/CREATE\s+(?:UNIQUE\s+)?INDEX\s+`?(\w+)`?\s+ON\s+`?(\w+)`?\s*\(([^)]*)\)/gi)) (out[m[2]] ??= []).push(`${m[1]}(${m[3].replace(/[\s`]/g, "")})`);
    }
  } catch {}
  return out;
}

// endpoint → tables it reads / writes, from the crawl-time code scan (statements with SQL text)
export function tableUse(endpoints) {
  const out = {};
  for (const e of endpoints ?? []) {
    const m = {};
    for (const st of e.code?.statements ?? []) {
      const w = /^\s*\(?\s*(INSERT|UPDATE|DELETE|REPLACE)\b/i.test(st.sql);
      for (const t of st.tables ?? []) m[t] = m[t] === "W" || w ? "W" : "R";
    }
    if (Object.keys(m).length) out[`${e.method} ${paramKey(e.path)}`] = m;
  }
  return out;
}

// what to do about an action, in one line, and how it gets fixed: parallel-fix (mechanical: an index, SQL in a known
// file), design (needs an owner and a plan: polling, external commands, caching, structure), investigate (cause unknown)
const short = (q, n = 70) => { const x = String(q ?? "").replace(/\s+/g, " ").trim(); return x.length > n ? x.slice(0, n - 1) + "…" : x; };
export function fixOf(a) {
  const t = a.title ?? "", file = a.file ? a.file.split("/").pop() : "";
  const pf = (g) => ({ how: "parallel-fix", unit: g });
  if (a.kind === "index") return { todo: "index を追加: " + (t.match(/Add an index on (.*)/)?.[1] ?? t), ...pf("indexes") };
  if (a.kind === "n1") return { todo: `N+1 を IN / JOIN で 1 回にまとめる: ${short(a.sql)}`, ...(file ? pf("file:" + file) : { how: "investigate" }) };
  if (a.kind === "aggregate") return { todo: `重い集計 SQL を減らす（1 回にまとめる / 結果をキャッシュ / 集計列）: ${short(a.sql)}`, ...(file ? pf("file:" + file) : { how: "investigate" }) };
  if (a.kind === "endpoint") return { todo: `この endpoint の処理を軽くする${file ? `（${file}）` : ""}`, ...(file ? pf("file:" + file) : { how: "investigate" }) };
  if (a.kind === "other" && /still slow with its index/.test(t)) return { todo: `index があっても遅い SQL（列の順番 / カバリング index / 取得列を減らす）: ${short(a.sql)}`, ...(file ? pf("file:" + file) : { how: "investigate" }) };
  if (a.kind === "polling") return { todo: `ポーリングを軽くする（応答のキャッシュ / 304 / 間隔の指定）: ${t.replace(/^Polling: /, "").replace(/ at .*/, "")}`, how: "design" };
  if (a.kind === "lock") return { todo: "ロック待ちの原因の SQL を特定する（processlist / innodb-status）", how: "investigate" };
  if (a.kind === "errors") return { todo: "5xx を直す: " + t, how: "investigate" };
  if (/external command/.test(t)) return { todo: "外部コマンドの実行を SQL / ライブラリ呼び出しに置き換える: " + t.replace(/ runs an external command.*/, ""), how: "design" };
  return { todo: t, how: a.group?.startsWith("file:") ? "parallel-fix" : "design", unit: a.group };
}
// generic advice without a target belongs to the report's Next step, not to a task list
const GENERIC = (a) => a.kind === "wait" || a.kind === "pprof" || a.kind === "io" || /^CPU is idle/.test(a.title ?? "");

// every route registered in the code (the crawler only sees what the UI calls)
export function routesFromCode(codeDir) {
  if (!codeDir || !fs.existsSync(codeDir)) return [];
  const out = [], defs = {};
  const files = [];
  const walk0 = (d) => { for (const f of fs.readdirSync(d, { withFileTypes: true })) { if (f.isDirectory()) { if (!/node_modules|vendor|^\./.test(f.name)) walk0(path.join(d, f.name)); } else if (f.name.endsWith(".go") && !f.name.endsWith("_test.go")) files.push(path.join(d, f.name)); } };
  walk0(codeDir);
  // where each handler func is defined (the file to edit), not where the route is registered
  for (const fp of files) { const src = fs.readFileSync(fp, "utf8"); for (const m of src.matchAll(/^func (?:\([^)]*\) )?(\w+)\(/gm)) defs[m[1]] = { file: path.relative(codeDir, fp), line: src.slice(0, m.index).split("\n").length }; }
  const walk = (d) => { for (const f of fs.readdirSync(d, { withFileTypes: true })) { if (f.isDirectory()) { if (!/node_modules|vendor|^\./.test(f.name)) walk(path.join(d, f.name)); } else if (f.name.endsWith(".go") && !f.name.endsWith("_test.go")) {
    const src = fs.readFileSync(path.join(d, f.name), "utf8");
    const lineOf = (i) => src.slice(0, i).split("\n").length;
    for (const m of src.matchAll(/\.(GET|POST|PUT|DELETE|PATCH|Get|Post|Put|Delete|Patch)\("([^"]+)",\s*(\w+)/g)) out.push({ key: `${m[1].toUpperCase()} ${paramKey(m[2])}`, method: m[1].toUpperCase(), path: paramKey(m[2]), handler: m[3], file: path.relative(codeDir, path.join(d, f.name)), line: lineOf(m.index) });
    for (const m of src.matchAll(/HandleFunc\("(\w+) ([^"]+)",\s*(\w+)/g)) out.push({ key: `${m[1]} ${paramKey(m[2])}`, method: m[1], path: paramKey(m[2]), handler: m[3], file: path.relative(codeDir, path.join(d, f.name)), line: lineOf(m.index) });
  } } };
  walk(codeDir);
  for (const r of out) if (defs[r.handler]) { r.file = defs[r.handler].file; r.line = defs[r.handler].line; }
  return out;
}

export function loadBench({ runDir, repoRoot, codeDir, annotationsPath, opsReportBase }) {
  const annotations = readJSON(annotationsPath ?? path.join(repoRoot, "docs/app-annotations.json")) ?? { screens: {}, endpoints: {}, rules: [] };
  const hosts = scanHosts(codeDir);
  if (!runDir || !fs.existsSync(path.join(runDir, "report.json"))) return { bench: null, now: null, annotations, hosts };
  const meta = readJSON(path.join(runDir, "report.json")) ?? {};
  const actions = readJSON(path.join(runDir, "actions.json")) ?? [];
  const ts = path.basename(runDir);
  // previous run (sibling dir with a report.json): regressions
  let prev = null, prevTS = null;
  try {
    const ds = fs.readdirSync(path.dirname(runDir)).filter((d) => d < ts && d.length === ts.length && fs.existsSync(path.join(path.dirname(runDir), d, "report.json"))).sort();
    if (ds.length) { prevTS = ds[ds.length - 1]; prev = readJSON(path.join(path.dirname(runDir), prevTS, "report.json")); }
  } catch {}
  const E = meta.endpoints ?? { top: [] };
  const total = (E.top ?? []).reduce((n, x) => n + (x.sum_s ?? 0), 0) || 1;
  const prevBy = Object.fromEntries((prev?.endpoints?.top ?? []).map((x) => [paramKey(x.key), x]));
  const prevTotal = (prev?.endpoints?.top ?? []).reduce((n, x) => n + (x.sum_s ?? 0), 0) || 1;
  const endpoints = (E.top ?? []).map((x) => {
    const k = paramKey(x.key), p = prevBy[k];
    return { key: k, raw: x.key, count: x.count, share: x.sum_s / total, avgMs: x.avg_ms, p99Ms: x.p99_ms, prevAvgMs: p?.avg_ms ?? null, prevShare: p ? p.sum_s / prevTotal : null };
  });
  const sp = meta.score_post?.endpoint ? paramKey(meta.score_post.endpoint) : null;
  const loopSteps = new Set(((meta.flows?.loops ?? [])[0]?.steps ?? []).map(paramKey));
  const sqls = (meta.sqls ?? []).map((q) => ({ ...q, tags: Object.fromEntries(Object.entries(q.tags ?? {}).map(([k, v]) => [paramKey(k), v])) }));
  // status: issues created by `task actions:issues` ("[P1] <action title>") and parallel-fix PRs (branch fix/<ts>/<slug>)
  const issues = (ghJSON(["issue", "list", "--state", "all", "--limit", "300", "--json", "number,title,state,url"], repoRoot) ?? []);
  const prs = (ghJSON(["pr", "list", "--state", "all", "--limit", "100", "--json", "number,title,body,state,url,headRefName"], repoRoot) ?? []);
  // the newest issue / PR that names the item (action title or endpoint) in its title or body; PR branches fix/<ts>/<slug>
  const statusOf = (texts, slug) => {
    const hit = (s) => texts.some((t) => t && t.length > 6 && paramKey(s ?? "").includes(paramKey(t)));
    for (const is of issues) if (hit(is.title)) return { kind: "issue", number: is.number, state: is.state, url: is.url };
    for (const pr of prs) if (hit(pr.title) || hit(pr.body) || (slug && pr.headRefName.endsWith("/" + slug))) return { kind: "pr", number: pr.number, state: pr.state, url: pr.url };
    return null;
  };
  const opsLink = (anchor) => (opsReportBase ? `${opsReportBase.replace(/\/$/, "")}/${ts}/report.html${anchor ? "#" + anchor : ""}` : null);
  const relOf = (k) => (sp && k === sp ? "score" : loopSteps.has(k) ? "loop" : "");
  const rulesOf = (k) => (annotations.rules ?? []).filter((r) => (r.endpoints ?? []).some((x) => paramKey(x) === k));

  // ---- now: what to solve next ----
  const items = [];
  for (const ep of endpoints) {
    const k = ep.key;
    // actions about this endpoint: the title names it (method + path), or its SQL is called mostly from here
    const topTag = (q) => Object.entries(q.tags ?? {}).sort((a, b) => b[1] - a[1])[0]?.[0];
    const acts = actions.filter((a) => a.priority <= "P2" && !GENERIC(a) && (paramKey(a.title).includes(k) || (a.sql && sqls.some((q) => q.norm === a.sql && topTag(q) === k))));
    const epSql = sqls.filter((q) => q.tags[k]).sort((a, b) => (b.tags[k] ?? 0) - (a.tags[k] ?? 0)).slice(0, 3);
    const rel = relOf(k);
    const regression = ep.prevAvgMs != null && ep.avgMs > ep.prevAvgMs * 1.3 && ep.avgMs - ep.prevAvgMs > 2;
    const status = statusOf([k, pathOf(ep.raw), ...acts.map((a) => a.title)], acts.find((a) => a.slug)?.slug);
    const weight = rel === "score" ? 3 : rel === "loop" ? 2 : 1;
    items.push({
      type: "endpoint", key: k, title: k, share: ep.share, count: ep.count, avgMs: ep.avgMs, p99Ms: ep.p99Ms, prevAvgMs: ep.prevAvgMs, rel, regression,
      actions: acts.map((a) => ({ priority: a.priority, title: a.title, file: a.file, group: a.group, ...fixOf(a) })), sqls: epSql.map((q) => ({ norm: q.norm, n: q.tags[k], code: q.code, exact: q.exact, grep: q.grep })),
      status, rules: rulesOf(k).map((r) => r.text), purpose: annotations.endpoints?.[k]?.purpose ?? null,
      score: 100 * ep.share * weight + (regression ? 50 : 0) + acts.filter((a) => a.priority === "P1").length * 10,
      link: opsLink("ep-" + (E.host ?? "svc1") + "-" + k.toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/^-|-$/g, "")),
    });
  }
  // actions that are not about one endpoint (indexes, DNS, ops)
  for (const a of actions) {
    if (items.some((it) => it.actions?.some((x) => x.title === a.title))) continue;
    if (a.priority > "P2" || GENERIC(a)) continue;
    const f = fixOf(a);
    items.push({ type: "action", key: a.title, title: f.todo, detail: a.detail, priority: a.priority, group: a.group, file: a.file, status: statusOf([a.title], a.slug), score: a.priority === "P1" ? 30 : 12, actions: [{ priority: a.priority, title: a.title, ...f }], sqls: a.sql ? sqls.filter((q) => q.norm === a.sql).slice(0, 1).map((q) => ({ norm: q.norm, code: q.code, exact: q.exact, grep: q.grep })) : [], rules: [] });
  }
  // 5xx: errors stop a client's scenario (Flows drop-off "error"), so they go first. The app log's message says what to do.
  const routeFile = Object.fromEntries(routesFromCode(codeDir).map((r) => [r.key, r.file]));
  const errTodo = (msg) => {
    if (/deadlock/i.test(msg)) return { todo: "デッドロックを解消する（同じ行を複数の tx が逆順で更新している: DELETE→INSERT を 1 文の UPSERT に / 更新順をそろえる / tx を短く）", how: "parallel-fix" };
    if (/no rows in result set|not found/i.test(msg)) return { todo: "参照先の行が消えている（削除との競合）: 見つからない行の扱いを、元の実装の応答に合わせて直す（元でも 500 なら触らない）", how: "investigate" };
    if (/duplicate entry/i.test(msg)) return { todo: "一意制約の重複: 元の実装でも同じ応答か確認（ベンチが重複を送るなら正常な 4xx/5xx の可能性）", how: "investigate" };
    if (/too many connections|connection refused|bad connection/i.test(msg)) return { todo: "DB 接続の枯渇 / 切断: 接続プールと max_connections を見直す", how: "design" };
    return { todo: "5xx を直す: " + msg, how: "investigate" };
  };
  for (const er of meta.errors ?? []) {
    const k = paramKey(er.endpoint), rate = er.count ? er.c5 / er.count : 0;
    if (er.c5 < 10 && rate < 0.01) continue;
    const msgs = (er.messages ?? []).filter((m) => !/context canceled/i.test(m.message));
    if (!msgs.length) continue;
    const fixes = msgs.map((m) => ({ priority: "P1", title: `5xx ×${m.n}: ${m.message}`, ...errTodo(m.message), unit: routeFile[k] ? "file:" + routeFile[k].split("/").pop() : undefined }));
    const ex = items.find((it) => it.type === "endpoint" && it.key === k);
    const errInfo = { c5: er.c5, count: er.count, rate, messages: msgs };
    const boost = 100 + Math.min(300, er.c5); // more failed requests = more clients that gave up
    if (ex) { ex.errors = errInfo; ex.actions = [...fixes, ...ex.actions]; ex.score += boost; }
    else items.push({ type: "endpoint", key: k, title: k, share: endpoints.find((e) => e.key === k)?.share ?? 0, count: er.count, errors: errInfo, actions: fixes, sqls: [], rules: rulesOf(k).map((r) => r.text), purpose: annotations.endpoints?.[k]?.purpose ?? null, rel: relOf(k), score: boost, status: null });
  }

  // how each item gets fixed: parallel-fix if any of its actions is mechanical, else design, else investigate;
  // an endpoint with no action at all needs someone to look (technique-scan / deep-dive)
  for (const it of items) {
    const hows = (it.actions ?? []).map((a) => a.how);
    it.how = hows.includes("parallel-fix") ? "parallel-fix" : hows.includes("design") ? "design" : "investigate";
    it.units = [...new Set((it.actions ?? []).filter((a) => a.how === "parallel-fix" && a.unit).map((a) => a.unit))];
    if (!(it.actions ?? []).length && it.type === "endpoint") it.todo = it.regression ? "悪化の原因を探す（前の run との差分 / deep-dive）" : "重い理由を探す（report に個別の action なし: technique-scan のキャッシュ / 構成、deep-dive）";
  }
  // "resolved" is decided by the measurement, not by the tracker: an action of the previous run that this run no longer
  // reports, or an endpoint that was heavy and got light. A merged PR on something still heavy stays open (status shows the PR).
  const minor = (it) => it.type === "endpoint" && it.share < 0.03 && !it.regression && !it.actions.length && !it.errors;
  const open = items.filter((it) => !minor(it)).sort((a, b) => b.score - a.score);
  // a code line shared by many items (a fill* helper) is shown only on the first (highest) item that has it
  const seenLoc = new Set();
  for (const it of open) for (const q of it.sqls ?? []) { q.code = (q.code ?? []).filter((c) => { if (seenLoc.has(c.loc)) return false; seenLoc.add(c.loc); return true; }); }
  const resolved = [];
  const prevActions = prevTS ? readJSON(path.join(path.dirname(runDir), prevTS, "actions.json")) ?? [] : [];
  const nowTitles = new Set(actions.map((a) => paramKey(a.title).replace(/[\d.,]+/g, "#")));
  for (const a of prevActions) {
    if (a.priority > "P2" || nowTitles.has(paramKey(a.title).replace(/[\d.,]+/g, "#"))) continue;
    resolved.push({ type: "action", key: a.title, title: a.title, priority: a.priority, status: statusOf([a.title], a.slug), gone: true });
  }
  for (const ep of endpoints) if (ep.prevShare != null && ep.prevShare >= 0.05 && ep.share < ep.prevShare / 2) resolved.push({ type: "endpoint", key: ep.key, title: ep.key, share: ep.share, prevShare: ep.prevShare, status: statusOf([ep.key, ep.raw]), improved: true });
  const now = { open: open.slice(0, 7), more: open.length > 7 ? open.length - 7 : 0, resolved };

  // what is new in the flows compared with the previous run: roles, score loops, drop-off points (推測)
  const flows = meta.flows ? JSON.parse(JSON.stringify(meta.flows)) : null;
  if (flows && prev?.flows) {
    const sig = (l) => (l.steps ?? []).map(paramKey).join(">");
    const dsig = (d) => `${paramKey(d.ep)}|${d.status}`;
    const markAll = (cur, old) => {
      const L = new Set((old?.loops ?? []).map(sig)), D = new Set((old?.drops ?? []).map(dsig));
      const Dn = Object.fromEntries((old?.drops ?? []).map((d) => [dsig(d), d.n]));
      for (const l of cur.loops ?? []) if (!L.has(sig(l))) l.new = true;
      for (const d of cur.drops ?? []) { if (!D.has(dsig(d))) d.new = true; else if (d.n >= 2 * (Dn[dsig(d)] ?? 0) + 10) d.grew = Dn[dsig(d)]; }
    };
    markAll(flows, prev.flows);
    const oldRoles = Object.fromEntries((prev.flows.roles ?? []).map((r) => [r.name, r]));
    for (const r of flows.roles ?? []) { if (!oldRoles[r.name]) r.new = true; markAll(r, oldRoles[r.name] ?? {}); }
  }

  const bench = {
    ts, prevTS, host: E.host ?? null, scorePost: meta.score_post?.endpoint ?? null, scorePostCount: meta.score_post?.count ?? null,
    endpoints, sqls, actions, flows, nextStep: meta.next_step ?? [], growth: meta.growth ?? [], internal: meta.internal ?? [], benchShape: meta.bench_shape ?? null,
    flowsHtml: fs.existsSync(path.join(runDir, "flows.html")) ? path.join(runDir, "flows.html") : null,
    opsReport: opsLink(""), issues: issues.length, prs: prs.length,
  };
  return { bench, now, annotations, hosts };
}
