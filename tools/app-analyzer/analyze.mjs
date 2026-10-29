// One shot: doctor -> crawl -> report -> publish (latest only) -> open.
// Usage: node analyze.mjs [config] [--no-open] [--skip-doctor] [--no-publish]
import { spawnSync } from "node:child_process";
import fs from "node:fs";
import path from "node:path";
import { latestOut } from "./outdir.mjs";
const args = process.argv.slice(2);
const cfgPath = args.find((a) => !a.startsWith("--")) ?? "analyzer.config.json";
const run = (cmd, a) => { const r = spawnSync(cmd, a, { stdio: "inherit" }); if (r.status !== 0) { console.error(`${cmd} ${a.join(" ")} failed`); process.exit(r.status ?? 1); } };
const t0 = Date.now();
if (!args.includes("--skip-doctor")) run("node", ["doctor.mjs", cfgPath]);
run("node", ["crawl.mjs", cfgPath]);
const cfg = JSON.parse(fs.readFileSync(cfgPath, "utf8"));
const dir = latestOut(cfg, cfgPath); // main checkout's out/ (outdir.mjs)
if (!dir) { console.error("crawl wrote no <outDir>/latest"); process.exit(1); }
const ts = path.basename(dir);
run("node", ["report.mjs", dir, cfgPath]);
let opened = `${dir}/report.html`;

// publish: write <publishDir>/latest.html + latest.md (only the latest is kept)
if (cfg.publishDir && !args.includes("--no-publish")) {
  const pub = path.resolve(path.dirname(cfgPath), cfg.publishDir);
  fs.mkdirSync(pub, { recursive: true });
  fs.copyFileSync(`${dir}/report.html`, path.join(pub, "latest.html"));
  fs.copyFileSync(`${dir}/report.md`, path.join(pub, "latest.md"));
  fs.writeFileSync(path.join(pub, "latest.txt"), `${cfg.name ?? new URL(cfg.baseURL).hostname} ${ts} ${dir}\n`);
  opened = path.join(pub, "latest.html");
  console.log(`published -> ${path.relative(process.cwd(), opened)} (+ latest.md). Older runs stay in ${path.dirname(dir)}.`);
}
console.log(`\ndone in ${((Date.now() - t0) / 1000).toFixed(1)}s -> ${opened}`);
if (!args.includes("--no-open") && process.platform === "darwin") spawnSync("open", [opened]);
