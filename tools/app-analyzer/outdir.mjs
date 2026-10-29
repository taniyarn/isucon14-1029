// Where crawl output (out/<ts>/, out/latest) lives. A relative cfg.outDir (default "out") is resolved against the config
// file's directory in the MAIN checkout of the repo (git rev-parse --git-common-dir's parent), not the worktree, so a
// crawl run from a subagent's worktree survives `task worktrees:clean`. An absolute outDir is used as is.
import fs from "node:fs";
import path from "node:path";
import { execFileSync } from "node:child_process";

const git = (cwd, args) => execFileSync("git", ["-C", cwd, ...args], { stdio: ["ignore", "pipe", "ignore"] }).toString().trim();

export function outRoot(cfg, cfgPath = "analyzer.config.json") {
  const o = cfg?.outDir ?? "out";
  if (path.isAbsolute(o)) return o;
  const cfgDir = path.dirname(path.resolve(cfgPath));
  try {
    const top = git(cfgDir, ["rev-parse", "--show-toplevel"]);
    const common = path.resolve(cfgDir, git(cfgDir, ["rev-parse", "--git-common-dir"]));
    const mainTop = path.basename(common) === ".git" ? path.dirname(common) : top;
    const dir = path.join(mainTop, path.relative(top, cfgDir));
    if (fs.existsSync(dir)) return path.resolve(dir, o);
  } catch {}
  return path.resolve(cfgDir, o);
}

// out/<latest> or null
export function latestOut(cfg, cfgPath) {
  const root = outRoot(cfg, cfgPath);
  try { return path.join(root, fs.readFileSync(path.join(root, "latest"), "utf8").trim()); } catch { return null; }
}
