// report.html = CSS + window.MODEL + report-app.js in one file. Shared by report.mjs (after a crawl) and refresh.mjs
// (re-renders the published latest.html with a new bench overlay, no crawl).
import fs from "node:fs";
import path from "node:path";
import { paramKey } from "./bench.mjs";

const esc = (s) => String(s ?? "").replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;");

export function renderHtml(model) {
  const appJs = fs.readFileSync(new URL("./report-app.js", import.meta.url), "utf8");
  const css = fs.readFileSync(new URL("./report-app.css", import.meta.url), "utf8");
  const nScreens = (model.screens ?? []).filter((s) => s.kind === "screen").length;
  const bench = model.bench ? ` · bench run ${esc(model.bench.ts)}` : "";
  return `<!DOCTYPE html><html lang="ja"><head><meta charset="utf-8"><title>App analysis ${esc(model.baseURL)}</title><style>${css}</style></head><body>
<header><h1>App analysis</h1><div class="sub">${esc(model.baseURL)} · crawl ${esc(model.generatedAt)} · ${nScreens} screens · ${(model.endpoints ?? []).length} endpoints${bench}</div><div class="lang"><button data-lang="ja">日本語</button><button data-lang="en">English</button></div></header>
<nav></nav><main><div id="content"></div></main>
<script>window.MODEL = ${JSON.stringify(model).replace(/<\/script/g, "<\\/script")};</script>
<script>${appJs}</script>
</body></html>`;
}

export function extractModel(html) {
  const m = html.match(/<script>window\.MODEL = ([\s\S]*?);<\/script>\n<script>/);
  if (!m) throw new Error("no window.MODEL in the published report: not crawled yet (template placeholder) or not re-rendered. Run the app-analysis crawl once (npm run analyze), then refresh");
  return JSON.parse(m[1].replace(/<\\\/script/g, "</script"));
}

// per screen path: share of the bench time spent in the endpoints that screen calls, and whether it calls score_post
export function screenWeights(model) {
  const B = model.bench;
  if (!B) return {};
  const share = Object.fromEntries(B.endpoints.map((e) => [e.key, e.share]));
  const sp = B.scorePost ? paramKey(B.scorePost) : null;
  const out = {};
  for (const s of model.screens ?? []) {
    const keys = new Set((s.requests ?? []).filter((r) => r.normPath && (r.type !== "document" || r.path?.startsWith(model.apiPrefix ?? "/api/"))).map((r) => `${r.method} ${paramKey(r.normPath)}`));
    const w = [...keys].reduce((n, k) => n + (share[k] ?? 0), 0);
    const p = s.path || s.label;
    const cur = out[p] ?? { share: 0, score: false, endpoints: [] };
    out[p] = { share: Math.max(cur.share, w), score: cur.score || (sp != null && keys.has(sp)), endpoints: [...new Set([...cur.endpoints, ...[...keys].filter((k) => share[k])])], calls: [...new Set([...(cur.calls ?? []), ...keys])] };
  }
  return out;
}

// the "now" list as markdown, for Claude (latest.md gets it between markers)
export function nowMarkdown(model) {
  const B = model.bench, N = model.now;
  if (!B || !N) return "";
  const pct = (x) => (x == null ? "" : `${(100 * x).toFixed(0)}%`);
  let md = `<!-- bench:start -->\n## Now — what to solve next (bench run ${B.ts}${B.prevTS ? `, vs ${B.prevTS}` : ""}; refreshed ${new Date().toISOString()})\n\n`;
  if (B.nextStep?.length) md += `Next step (report rules):\n${B.nextStep.map((c, i) => `${i + 1}. ${c.name} — \`${c.cmd}\``).join("\n")}\n\n`;
  const HOW = { "parallel-fix": "parallel-fix", design: "design (assign an owner)", investigate: "investigate (deep-dive)" };
  md += `| # | what | pts | share | p99 ms | score | fix | to do | rules |\n|---|---|---|---|---|---|---|---|---|\n`;
  N.open.forEach((it, i) => {
    const todo = [it.regression ? `regression ${Math.round(it.prevAvgMs)}→${Math.round(it.avgMs)} ms` : "", it.todo ?? "", ...(it.actions ?? []).map((a) => a.todo)].filter(Boolean).join("; ");
    md += `| ${i + 1} | \`${it.type === "endpoint" ? it.title : it.priority}\` | ${Math.round(it.score ?? 0)} | ${pct(it.share)} | ${it.p99Ms != null ? Math.round(it.p99Ms) : ""} | ${it.rel || ""} | ${HOW[it.how] ?? it.how}${it.units?.length ? " " + it.units.join(", ") : ""} | ${todo.replace(/\|/g, "\\|")} | ${(it.rules ?? []).join("; ").replace(/\|/g, "\\|")} |\n`;
  });
  if (N.more) md += `\n(+${N.more} more open items)\n`;
  if (N.resolved.length) md += `\nResolved: ${N.resolved.map((r) => r.title + (r.improved ? ` (share ${pct(r.prevShare)}→${pct(r.share)})` : "")).join(", ")}\n`;
  return md + `<!-- bench:end -->\n`;
}

export function withNow(md, model) {
  const block = nowMarkdown(model);
  const stripped = md.replace(/<!-- bench:start -->[\s\S]*?<!-- bench:end -->\n?/, "");
  if (!block) return stripped;
  const i = stripped.indexOf("\n## ");
  return i >= 0 ? stripped.slice(0, i + 1) + block + "\n" + stripped.slice(i + 1) : stripped + "\n" + block;
}

export function relLink(fromDir, absPath) {
  return absPath ? path.relative(fromDir, absPath) : null;
}
