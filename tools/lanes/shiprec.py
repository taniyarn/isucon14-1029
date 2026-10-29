#!/usr/bin/env python3
"""ship.sh's records after a merge: the journal line and the claims rows (ship.sh commits each file on its own).

  shiprec.py journal pr=<n> sha=<merge sha> branch=<head> title=<title> body_file=<file> [now=hh:mm] [file=docs/journal.md]
      Appends the PR body's journal line (a line `hh:mm | branch / PR | 何を | なぜ | 結果`, backticks / a list marker
      around it are fine; the one after a line mentioning "journal" wins) with the time, #<n>, `merge <sha>` and
      `OK: 盤 hh:mm` (merge OK on lane:web for that PR) / `OK: fast（事後レビュー）` ([fast] title) / `OK: タブ` filled in.
      No such line: `hh:mm | <branch> / #<n> | <title> | — | merge <sha>、OK: …`. Prints the line.
  shiprec.py claims branch=<head> [before=<sha>] [now=hh:mm] [file=docs/claims.md]
      Rows of the table whose branch is the PR's head branch, or (with before=) a local / origin branch whose tip is in
      HEAD but was not in <before> (the subagent branches of an integ/<ts> PR), and whose status is not merged /
      abandoned → status merged, updated now. Prints the branches it marked.
"""
import os
import re
import subprocess
import sys
from datetime import datetime

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import lanes  # noqa: E402

JLINE = re.compile(r"^\s*(?:[-*]\s+)?`?\s*((?:hh:mm|HH:MM|\d{1,2}:\d{2})\s*\|.*?)\s*`?\s*$")
JOURNAL_HEAD = "# journal\n\n時刻 | branch / PR | 何を | なぜ（report の行） | 結果（score）\n\n"
CLOSED = {"merged", "abandoned"}


def journal_line_in(body):
    """The journal line of a PR body ('' when none): ≥ 4 '|' (5 fields); after a line mentioning journal wins."""
    found, after_mark = [], False
    for ln in (body or "").splitlines():
        m = JLINE.match(ln)
        if m and m.group(1).count("|") >= 3:
            found.append((after_mark, m.group(1)))
        after_mark = "journal" in ln.lower() or (after_mark and not ln.strip())
    marked = [x for flag, x in found if flag]
    return (marked or [x for _, x in found] or [""])[0]


def ok_text(pr, title):
    try:
        b = lanes.board_ok(int(pr))
    except Exception:
        b = ""
    if b:
        return "OK: " + b
    if (title or "").lstrip().lower().startswith("[fast]"):
        return "OK: fast（事後レビュー）"
    return "OK: タブ"


def build_line(pr, sha, branch, title, body, now, ok):
    rec = f"merge {sha}、{ok}"
    src = journal_line_in(body)
    if not src:
        return f"{now} | {branch} / #{pr} | {(title or '').strip() or '-'} | — | {rec}"
    f = [x.strip() for x in src.split("|")]
    f += [""] * (5 - len(f))
    f[0] = now
    who = f[1]
    who = re.sub(r"#\s*<\s*n\s*>|<\s*n\s*>|#\s*N\b", f"#{pr}", who)
    if f"#{pr}" not in who:
        who = f"{who} / #{pr}" if who else f"{branch} / #{pr}"
    f[1] = who
    rest = " | ".join(x for x in f[4:] if x).strip()
    rest = re.sub(r"^[—-]$", "", rest)
    f4 = f"{rest}、{rec}" if rest else rec
    return " | ".join(f[:4] + [f4])


def append_journal(path, line):
    if os.path.exists(path):
        with open(path, "rb") as fh:
            data = fh.read()
        pre = b"" if not data or data.endswith(b"\n") else b"\n"
    else:
        os.makedirs(os.path.dirname(path) or ".", exist_ok=True)
        pre = JOURNAL_HEAD.encode()
    with open(path, "ab") as fh:
        fh.write(pre + line.encode() + b"\n")


def git(*args):
    r = subprocess.run(["git", *args], capture_output=True, text=True)
    return r.returncode, r.stdout.strip()


def in_this_ship(branch, before):
    """A branch whose tip is in HEAD but was not in <before>: it came in with this ship."""
    for ref in (f"refs/heads/{branch}", f"refs/remotes/origin/{branch}"):
        rc, tip = git("rev-parse", "-q", "--verify", ref + "^{commit}")
        if rc != 0 or not tip:
            continue
        if git("merge-base", "--is-ancestor", tip, "HEAD")[0] == 0 and git("merge-base", "--is-ancestor", tip, before)[0] != 0:
            return True
    return False


def mark_claims(path, head, now, before=""):
    try:
        with open(path) as fh:
            lines = fh.read().split("\n")
    except OSError:
        return []
    marked = []
    for i, ln in enumerate(lines):
        s = ln.strip()
        if not (s.startswith("|") and s.endswith("|")):
            continue
        cells = [c.strip() for c in s[1:-1].split("|")]
        if len(cells) < 6 or set(cells[0]) <= set("-: "):
            continue
        br = cells[0].strip("`").strip()
        if br in ("branch",) or cells[4].strip("`").strip().lower() in CLOSED:
            continue
        if br != head and not (before and in_this_ship(br, before)):
            continue
        cells[4], cells[5] = "merged", now
        lines[i] = "| " + " | ".join(cells) + " |"
        marked.append(br)
    if marked:
        with open(path, "w") as fh:
            fh.write("\n".join(lines))
    return marked


def main(argv):
    if not argv or argv[0] not in ("journal", "claims"):
        print(__doc__.strip(), file=sys.stderr)
        return 2
    a = lanes.parse_kv(argv[1:])
    now = (a.get("now") or "").strip() or datetime.now().strftime("%H:%M")
    if argv[0] == "journal":
        body = ""
        if a.get("body_file"):
            try:
                with open(a["body_file"]) as fh:
                    body = fh.read()
            except OSError:
                pass
        pr = a.get("pr", "").lstrip("#")
        line = build_line(pr, a.get("sha", "")[:7], a.get("branch", ""), a.get("title", ""), body, now,
                          ok_text(pr, a.get("title", "")))
        append_journal(a.get("file") or "docs/journal.md", line)
        print(line)
    else:
        print(" ".join(mark_claims(a.get("file") or "docs/claims.md", a.get("branch", "").strip(), now,
                                   a.get("before", "").strip())))
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
