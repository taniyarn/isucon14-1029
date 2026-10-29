// Refresh the published app-analysis report with a bench run, without crawling:
//   node refresh.mjs [analyzer.config.json] --run <measurements/<ts> dir | latest>
//   node refresh.mjs [analyzer.config.json] [--run none]   no run: re-read docs/app-annotations.json, the host-bound code and
//     endpoint x table only (before the first bench, or after editing annotations). The bench overlay already in latest.html
//     is kept (recomputed when that run is still under measurements/).
// Reads <publishDir>/latest.html (its embedded model from the last crawl), replaces the bench overlay
// (bench.mjs: ops report.json + actions.json, gh issues/PRs, docs/app-annotations.json, host-bound code), and writes
// latest.html + the "Now" block of latest.md back. Takes about a second; run it after every bench (task app:refresh TS=).
import fs from "node:fs";
import path from "node:path";
import { execFileSync } from "node:child_process";
import { loadBench, resolveRunDir, tableUse, tableUseFromCode, addedIndexes, routesFromCode } from "./bench.mjs";
import { renderHtml, extractModel, screenWeights, withNow, relLink } from "./html.mjs";

const args = process.argv.slice(2);
const cfgPath = path.resolve(args.find((a) => a.endsWith(".json")) ?? "analyzer.config.json");
const runArg = args.includes("--run") ? args[args.indexOf("--run") + 1] : "none";
if (!runArg) { console.error("usage: node refresh.mjs [analyzer.config.json] [--run <measurements/<ts> dir | latest | none>]"); process.exit(2); }
const noRun = runArg === "none";
const cfg = JSON.parse(fs.readFileSync(cfgPath, "utf8"));
const cfgDir = path.dirname(cfgPath);
const pub = path.resolve(cfgDir, cfg.publishDir ?? "../../docs/reports");
const codeDir = cfg.codeDir ? path.resolve(cfgDir, cfg.codeDir) : null;
const repoRoot = cfg.repoRoot ? path.resolve(cfgDir, cfg.repoRoot) : (() => { try { return execFileSync("git", ["-C", cfgDir, "rev-parse", "--show-toplevel"]).toString().trim(); } catch { return path.resolve(cfgDir, "../.."); } })();
const t0 = Date.now();
const htmlPath = path.join(pub, "latest.html");
if (!fs.existsSync(htmlPath)) { console.error(`${htmlPath} not found: run the analyzer once (task analyze) to publish it`); process.exit(1); }
const model = extractModel(fs.readFileSync(htmlPath, "utf8"));
let runDir = null;
if (noRun) { // the run already shown, if it is still local
  const prevTs = model.bench?.ts;
  if (prevTs && fs.existsSync(path.join(repoRoot, "measurements", prevTs, "report.json"))) runDir = path.join(repoRoot, "measurements", prevTs);
} else {
  runDir = resolveRunDir(runArg === "latest" ? "latest" : path.resolve(runArg), repoRoot);
  if (!runDir) { console.error("no run with a report.json under measurements/ (task run:pull TS=… first, or TS=none for annotations only)"); process.exit(1); }
}
const loaded = loadBench({ runDir, repoRoot, codeDir, annotationsPath: cfg.annotations ? path.resolve(cfgDir, cfg.annotations) : undefined, opsReportBase: cfg.opsReportBase });
const { annotations, hosts } = loaded;
let { bench, now } = loaded;
if (bench) bench.flowsHtml = relLink(pub, bench.flowsHtml);
else if (noRun) { bench = model.bench ?? null; now = model.now ?? null; } // keep the overlay already in the page
Object.assign(model, { bench, now, annotations, hosts, tableUse: tableUseFromCode(codeDir) ?? tableUse(model.endpoints), addedIndexes: addedIndexes(repoRoot), routes: routesFromCode(codeDir), refreshedAt: new Date().toISOString() });
model.screenWeights = screenWeights(model);
fs.writeFileSync(htmlPath, renderHtml(model));
const mdPath = path.join(pub, "latest.md");
if (fs.existsSync(mdPath)) fs.writeFileSync(mdPath, withNow(fs.readFileSync(mdPath, "utf8"), model));
const runNote = runDir ? `run ${bench?.ts}` : bench ? `no run (kept the overlay of run ${bench.ts}; not under measurements/)` : "no run (annotations + code only)";
console.log(`refreshed ${path.relative(process.cwd(), htmlPath)} with ${runNote}: ${now?.open.length ?? 0} open, ${now?.resolved.length ?? 0} resolved, ${hosts.length} host-bound endpoints (${Date.now() - t0} ms)`);
