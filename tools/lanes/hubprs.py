#!/usr/bin/env python3
"""task hub:prs — open PRs that no lane announced (a teammate's) get one review todo on the hub's board, once per PR.

A PR counts as a lane's when its author is the hub's own gh login AND it was announced: a [PR] #<n> line (Claude
lanes' SendMessage in cc-events.jsonl, Codex lanes' lane:msg on the board) or a docs/claims.md row (origin/main) for its
head branch. Everything else (another author, or an unannounced PR) is a human PR. A PR that already has a review
todo, or was offered before (board "hub_prs"), is not offered again.
Overlap is checked per FILE (not per function): the PR's files vs the other open PRs' files and the open claims rows'
"files / functions" column. Prints one line per open PR.
Env: GH_BIN (default gh; tests use a fake), LANES_DIR (the board), HUBPRS_CLAIMS (a claims.md to read instead of
git show origin/main:docs/claims.md).
"""
import json
import os
import re
import subprocess
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import lanes  # noqa: E402

GH = os.environ.get("GH_BIN") or "gh"
PR_RE = re.compile(r"\[PR\][^\n]*")


def gh(*args):
    out = subprocess.run([GH, *args], capture_output=True, text=True, timeout=30)
    if out.returncode != 0:
        raise lanes.LaneError("gh " + " ".join(args[:3]) + ": " + (out.stderr.strip() or "failed")[:200])
    return out.stdout


def open_prs():
    fields = "number,title,author,headRefName,files"
    try:
        rows = json.loads(gh("pr", "list", "--state", "open", "--limit", "100", "--json", fields) or "[]")
    except lanes.LaneError:  # an older gh without files in pr list
        rows = json.loads(gh("pr", "list", "--state", "open", "--limit", "100", "--json", fields[:-6]) or "[]")
    out = []
    for r in rows:
        out.append({"n": int(r["number"]), "title": str(r.get("title") or ""),
                    "author": str((r.get("author") or {}).get("login") or ""),
                    "branch": str(r.get("headRefName") or ""),
                    "files": [str(f.get("path")) for f in (r.get("files") or []) if f.get("path")]})
    return out


def me():
    try:
        return gh("api", "user", "-q", ".login").strip()
    except (lanes.LaneError, subprocess.TimeoutExpired, FileNotFoundError):
        return ""


def announced(board):
    """PR numbers named in a [PR] line: Claude lanes' SendMessage (cc-events.jsonl) and lane:msg on the board."""
    nums = set()
    texts = [x.get("msg") or "" for x in board.get("msgs", [])]
    path = os.path.join(lanes.lanes_dir(), "cc-events.jsonl")
    try:
        with open(path, errors="replace") as f:
            for line in f:
                if "[PR]" not in line:
                    continue
                try:
                    texts.append(str(json.loads(line).get("head") or ""))
                except ValueError:
                    pass
    except OSError:
        pass
    for x in texts:
        for m in PR_RE.findall(x):
            nums.update(int(n) for n in re.findall(r"#(\d+)", m))
    return nums


def claims_rows():
    """Rows of the claims table: [{branch, files, status}]."""
    p = os.environ.get("HUBPRS_CLAIMS")
    if p:
        text = open(p, errors="replace").read()
    else:
        subprocess.run(["git", "fetch", "-q", "origin", "main"], capture_output=True, timeout=30)
        r = subprocess.run(["git", "show", "origin/main:docs/claims.md"], capture_output=True, text=True)
        text = r.stdout if r.returncode == 0 else ""
    rows = []
    for line in text.splitlines():
        c = [x.strip() for x in line.strip().strip("|").split("|")]
        if len(c) < 5 or c[0] in ("branch", "") or set(c[0]) <= set("-: "):
            continue
        rows.append({"branch": c[0].strip("` "), "files": c[1], "status": c[4].strip("` ").lower()})
    return rows


def touches(text, path):
    return path in text or (os.path.basename(path) in text and len(os.path.basename(path)) > 4)


def main():
    prs = open_prs()
    board = lanes.read_board()
    login, said, claims = me(), announced(board), claims_rows()
    claimed = {r["branch"] for r in claims}
    offered = set(board.get("hub_prs") or [])
    on_board = {n for x in board["todos"] for n in lanes.todo_prs(x)}
    lines, made = [], 0
    for p in prs:
        n = p["n"]
        lane_pr = bool(login) and p["author"] == login and (n in said or p["branch"] in claimed)
        if n in on_board or n in offered:
            lines.append(f"#{n} on the board ({'lane' if lane_pr else 'human'}): {p['title'][:50]}")
            continue
        if lane_pr:
            lines.append(f"#{n} lane PR (announced, the hub reviews it from the [PR] line): {p['title'][:50]}")
            continue
        notes = []
        if p["branch"] not in claimed:
            notes.append("claims 無し")
        others = [f"#{o['n']}" for o in prs if o["n"] != n and set(o["files"]) & set(p["files"])]
        rows = [r["branch"] for r in claims if r["status"] == "open" and r["branch"] != p["branch"]
                and any(touches(r["files"], f) for f in p["files"])]
        if others or rows:
            notes.append("同じファイル: " + " ".join(others + rows) + "（ファイル単位。関数は PR の diff で確かめる）")
        if not p["files"]:
            notes.append("変更ファイル不明（gh が files を返さない）")
        who = p["author"] or "?"
        msg = (f"人の PR #{n} のレビュー（作者 {who}、{p['branch']}）: {p['title'][:80]}"
               + (" / " + " / ".join(notes) if notes else ""))
        r = lanes.mutate("hubpr", {"pr": str(n), "title": p["title"], "msg": msg,
                                   "why": "レーンの [PR] が無い PR（チームの人が出した）。task hub:prs が拾った",
                                   "after": "merge OK ならハブが README §7 の順で次の ship に載せる。要修正なら作者に Discord で"})
        if r == "already":
            lines.append(f"#{n} on the board (human): {p['title'][:50]}")
        else:
            made += 1
            lines.append(f"#{n} human PR → review todo {r}" + (f" ({'; '.join(notes)})" if notes else ""))
    lines.append(f"open PRs {len(prs)}, new review todos {made}" + ("" if login else " (gh api user failed: every PR treated as human)"))
    print("\n".join(lines))
    return 0


if __name__ == "__main__":
    try:
        sys.exit(main())
    except (lanes.LaneError, subprocess.TimeoutExpired, FileNotFoundError, ValueError) as e:
        print(f"!! hub:prs: {e}", file=sys.stderr)
        sys.exit(1)
