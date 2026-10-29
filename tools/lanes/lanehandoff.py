#!/usr/bin/env python3
"""lane:handoff — one page of a lane's state, so a new session (Claude or Codex) can take the lane over mid-contest.

  lanes.py handoff lane=3 [out=docs/handoff-lane3.md]

Collects: the lane's board row (STATE / STEP / NOTE / checklist), the open todos and alerts it created, its unread and
recent board messages, the last [TASK] it received on the board, its open docs/claims.md rows (origin/main, else the
local file), its open PRs (gh pr list --author @me, filtered by the lane's branch globs in plan.json "auto" and its claims
branches), its worktrees (git worktree list → dirty? ahead of origin/main?), the lane → session name table the Claude
hooks saw ([HELLO] lines, SendMessage recipients in cc-events.jsonl), the lane file's row for the last [TASK] (next
action), and the start line + command for the successor. Writes the Markdown to out and prints the path + the start line.
LANES_NO_GH=1 skips gh; LANES_NO_GIT=1 skips git (tests).
"""
import json
import os
import re
import subprocess

import lanes

CLAIM_OPEN = ("open", "draft", "wip")


def sh(args, cwd=None, timeout=15):
    try:
        r = subprocess.run(args, cwd=cwd, capture_output=True, text=True, timeout=timeout)
        return r.stdout if r.returncode == 0 else ""
    except (OSError, subprocess.TimeoutExpired):
        return ""


def claims_rows(text):
    """Rows of the claims table: dicts branch / files / layer / owner / status / updated."""
    rows, head = [], None
    for line in (text or "").splitlines():
        if not line.startswith("|"):
            head = None if not line.strip() else head
            continue
        cells = [c.strip() for c in line.strip().strip("|").split("|")]
        if cells and cells[0] == "branch":
            head = [c.split()[0].lower() if c else "" for c in cells]
            continue
        if head is None or set("".join(cells)) <= set("-: "):
            continue
        rows.append({head[i] if i < len(head) else str(i): c for i, c in enumerate(cells)})
    return rows


def lane_owner_re(lane):
    return re.compile(r"(?:lane|レーン)\s*" + re.escape(lane) + r"(?![0-9a-z])", re.I)


def lane_claims(lane, text):
    r = lane_owner_re(lane)
    return [x for x in claims_rows(text) if r.search(x.get("owner", "")) and x.get("status", "").lower() in CLAIM_OPEN]


def branch_of(row):
    return row.get("branch", "").strip("` ").split("（")[0].split(" ")[0]


def lane_globs(lane):
    """The branch rules (plan.json "auto") of the lane's steps."""
    return [x["auto"] for x in lanes.load_plan() if x["lane"] == lane and x.get("auto")]


def is_lane_branch(lane, branch, claim_branches):
    if not branch:
        return False
    if branch in claim_branches:
        return True
    return any(lanes.auto_match({"branches": r.get("branches"), "not": r.get("not")}, branch, []) for r in lane_globs(lane))


def worktrees():
    """[{path, branch, dirty, ahead}] from git worktree list --porcelain."""
    if os.environ.get("LANES_NO_GIT") == "1":
        return []
    out, cur = [], {}
    for line in sh(["git", "worktree", "list", "--porcelain"]).splitlines() + [""]:
        if not line.strip():
            if cur.get("path"):
                out.append(cur)
            cur = {}
        elif line.startswith("worktree "):
            cur["path"] = line[9:]
        elif line.startswith("branch "):
            cur["branch"] = line[7:].replace("refs/heads/", "", 1)
        elif line.strip() == "detached":
            cur["branch"] = "(detached)"
    for w in out:
        if os.path.isdir(w["path"]):
            w["dirty"] = len([x for x in sh(["git", "-C", w["path"], "status", "--porcelain"]).splitlines() if x.strip()])
            n = sh(["git", "-C", w["path"], "rev-list", "--count", "origin/main..HEAD"]).strip()
            w["ahead"] = int(n) if n.isdigit() else None
    return out


def open_prs():
    if os.environ.get("LANES_NO_GH") == "1":
        return None
    out = sh(["gh", "pr", "list", "--author", "@me", "--state", "open", "--limit", "100",
              "--json", "number,title,headRefName,url,isDraft"], timeout=20)
    try:
        return json.loads(out) if out.strip() else []
    except ValueError:
        return []


def claims_text():
    if os.environ.get("LANES_NO_GIT") != "1":
        t = sh(["git", "show", "origin/main:docs/claims.md"])
        if t:
            return t, "origin/main"
    try:
        with open(os.path.join(lanes.repo_toplevel() or ".", "docs", "claims.md")) as f:
            return f.read(), "local"
    except OSError:
        return "", "none"


def lane_file_text(lane):
    try:
        with open(os.path.join(lanes.repo_toplevel() or ".", lanes.LANE_FILES.get(lane, ""))) as f:
            return f.read()
    except (OSError, TypeError):
        return ""


def session_names(cc_path=None):
    """{lane: name} from the Claude hooks' cc-events.jsonl: [HELLO] <lane> <name> prompts the hub received, and the
    recipient of each lane's SendMessage to the hub (the hub's name). Codex sessions have no names (board messages)."""
    path = cc_path or os.path.join(lanes.lanes_dir(), "cc-events.jsonl")
    names = {}
    try:
        with open(path) as f:
            for line in f:
                try:
                    e = json.loads(line)
                except ValueError:
                    continue
                m = re.match(r"\[HELLO\]\s+([0-9a-z]+)\s+(\S+)", e.get("head") or "")
                if m and e.get("event") in ("UserPromptSubmit", "PreToolUse"):
                    names[m.group(1)] = m.group(2)
                if e.get("event") == "PreToolUse" and e.get("tool") == "SendMessage" and e.get("lane") in ("2", "3", "c") \
                        and e.get("to") and (e.get("head") or "").startswith("["):
                    names["1"] = e["to"]  # a lane's message goes to the hub only (hub and spoke)
    except OSError:
        pass
    return names


def next_action(lane, task_msg, text):
    """The lane file's trigger row for the last [TASK] (its やること cell), else ''."""
    m = re.match(r"\[TASK\]\s+(\S+)\s+(\S+)", task_msg or "")
    if not m or not text:
        return ""
    job = m.group(2)
    want = f"[TASK] {lane} {job}"
    for line in text.splitlines():
        if not line.startswith("|"):
            continue
        cells = [c.strip() for c in line.strip().strip("|").split("|")]
        if len(cells) >= 2 and want in cells[0].replace("`", ""):
            return lanes.clean(cells[1], 600)
    return ""


def start_line(lane, out, hub_name=""):
    name = lanes.LANE_NAMES.get(lane, lane)
    lf = lanes.LANE_FILES.get(lane, "docs/lanes/README.md")
    if lanes.lane_agent(lane) == "codex":
        return (f"あなたはレーン {lane}（{name}）を前のセッションから引き継ぐ。AGENTS.md（「Codex で動くとき」）→ docs/lanes/README.md"
                f"（§12）→ {lf} → {out} の順に読んで、続きをやって。最初に task lane:inbox LANE={lane} と task lane:board を見て、"
                f"task lane:status LANE={lane} で盤を更新。合図は task lane:msg。")
    hub = "" if lane == "1" else f"レーン 1 の名前は {hub_name or '<ハブの名前>'}。"
    first = ("最初に ListAgents で自分の名前を出し、2 / 3 / c に [HELLO] 1 <新しい名前> を送り直す（Codex のレーンには task lane:msg）。"
             if lane == "1" else "最初に [HELLO] を送り直して（ハブが Codex なら task lane:msg）、")
    return (f"あなたはレーン {lane}（{name}）を前のセッションから引き継ぐ。CLAUDE.md → docs/lanes/README.md（§12）→ {lf} → {out} "
            f"の順に読んで、続きをやって。{hub}{first}task lane:inbox LANE={lane} と task lane:board も見る。")


def render(lane, b, t, out, claims=("", "none"), prs=None, wts=None, lane_text="", names=None, notes=None):
    names = names or {}
    l = b["lanes"].get(lane) or {}
    agent = lanes.lane_agent(lane)
    L = [f"# 引き継ぎ: レーン {lane}（{lanes.LANE_NAMES.get(lane, lane)}） {lanes.hm(t)}", "",
         f"agent（sessions.json）: **{agent}** · 盤: phase {b.get('phase')} · 作成: `task lane:handoff LANE={lane}`", ""]
    L += ["## 盤の行", ""]
    if l:
        L.append(f"- STATE **{l.get('state')}**（{lanes.age(l.get('since'), t)}）· STEP: {l.get('step') or '-'}"
                 + (f" · NOTE: {l['note']}" if l.get("note") else "") + (f" · 🤖×{l['agents']}" if l.get("agents") else ""))
        for tk in lanes.lane_tasks(b, lane):
            items = " ".join(("✅" if x["state"] in ("done", "skip") else "🔵" if x["state"] == "doing" else "⬜") + x["t"]
                             for x in tk["items"])
            L.append(f"- step {tk['id']} {tk['title']} {tk['done_n']}/{tk['total']} {items}".rstrip())
    else:
        L.append("- （盤にこのレーンの行が無い）")
    L += ["", "## 自分が出した open の todo / alert", ""]
    tds = [x for x in b.get("todos", []) if x.get("lane") == lane and not x.get("done")]
    als = [x for x in b.get("alerts", []) if x.get("lane") == lane and not x.get("resolved")]
    L += [f"- todo {x['id']}: {x['msg']}" + (f" — cmd `{x['cmd']}`" if x.get("cmd") else "") for x in tds]
    L += [f"- alert {x['id']} [{x['level']}]: {x['msg']}（直ったら task lane:resolve ID={x['id']}）" for x in als]
    if not tds and not als:
        L.append("- なし")
    msgs = [x for x in b.get("msgs", []) if lane in (x["to"], x["from"])]
    unread = [x for x in msgs if x["to"] == lane and not x.get("read_at")]
    task = next((x for x in reversed(msgs) if x["to"] == lane and x["msg"].startswith("[TASK]")), None)
    L += ["", "## メッセージ（盤）", "",
          f"- 未読 {len(unread)} 件（後継は最初に `task lane:inbox LANE={lane}`）"]
    L += [f"- `{lanes.msg_line(x, t)}`" for x in msgs[-8:]]
    L += ["", f"最後に受けた `[TASK]`（盤）: " + (f"`{task['msg']}`（{lanes.hm(task['t'])}）" if task else
                                                "盤には無い（SendMessage で来た分は前のセッションの画面で見る）")]
    if notes is not None:  # the lane's own conclusions (task lane:note, lane:web 「📜 経緯」): what the successor builds on
        L += ["", "## 自分が書いた経緯（lane:note、新しい順）", ""]
        L += [f"- {lanes.hm(e['t'])} [{e.get('nkind', '')}] {e.get('msg', '')}" + (f"（{e['ref']}）" if e.get("ref") else "")
              + "".join(f"\n  - {x}" for x in (e.get("body") if isinstance(e.get("body"), list) else []))
              for e in notes[::-1][:8]] or ["- なし"]
    if names:
        L += ["", "## セッション名（Claude の hooks が見たもの）", "", "| lane | session |", "|---|---|"]
        L += [f"| {k} | {names[k]} |" for k in lanes.lane_order(names)]
        L.append("")
        L.append("Codex のレーンには名前が無い（合図は `task lane:msg`）。")
    rows = lane_claims(lane, claims[0])
    L += ["", f"## claims（open、{claims[1]}）", ""]
    L += [f"- `{branch_of(x)}` — {x.get('files', '')}（{x.get('layer', '')}, {x.get('status', '')} {x.get('updated', '')}）"
          for x in rows] or ["- なし"]
    cb = {branch_of(x) for x in rows}
    L += ["", "## open の PR（gh pr list --author @me、このレーンのブランチ）", ""]
    if prs is None:
        L.append("- （gh を呼ばなかった: LANES_NO_GH=1）")
    else:
        mine = [p for p in prs if is_lane_branch(lane, p.get("headRefName", ""), cb)]
        L += [f"- #{p['number']} {p.get('title', '')}（`{p.get('headRefName', '')}`{'、draft' if p.get('isDraft') else ''}）"
              for p in mine] or ["- なし"]
    L += ["", "## worktree", ""]
    wl = [w for w in (wts or []) if is_lane_branch(lane, w.get("branch", ""), cb)]
    others = [w for w in (wts or []) if w not in wl and os.path.basename(w["path"]).startswith("wt-")]
    for w in wl:
        L.append(f"- `{w['path']}`（`{w.get('branch')}`）未 commit {w.get('dirty', '?')} · origin/main より先 "
                 f"{w.get('ahead') if w.get('ahead') is not None else '?'} commit")
    if others:
        L.append("- 持ち主が分からない wt-*: " + ", ".join(f"`{os.path.basename(w['path'])}`（{w.get('branch')}, 未 commit "
                                                    f"{w.get('dirty', '?')}）" for w in others))
    if not wl and not others:
        L.append("- なし")
    na = next_action(lane, task["msg"] if task else "", lane_text)
    L += ["", "## 次にやること", "",
          (f"レーンのファイルの `{task['msg'].split(' TS=')[0]}` の行: {na}" if na else
           "（レーンのファイルから決められない: STEP と最後の [TASK] から判断し、無ければ「手が空いたら」の上から）")]
    sl = start_line(lane, out, names.get("1", ""))
    L += ["", "## 後継の起動（前のセッションを閉じてから）", "", "起動の 1 行:", "", "```", sl, "```", "",
          f"コマンド（{agent}、repo のルートで）:", "", "```", lanes.session_cmd(lane, sl), "```", ""]
    return "\n".join(L), sl


def main(a):
    lane = (a.get("lane") or "").strip().lower()
    if lane not in lanes.LANE_FILES:
        raise lanes.LaneError("lane= 1 | 2 | 3 | c")
    out = (a.get("out") or "").strip() or f"docs/handoff-lane{lane}.md"
    b, t = lanes.read_board(), lanes.now()
    md, sl = render(lane, b, t, out, claims=claims_text(), prs=open_prs(), wts=worktrees(), lane_text=lane_file_text(lane),
                    names=session_names(),
                    notes=[e for e in lanes.read_events() if e["kind"] == "note" and e.get("lane") == lane])
    d = os.path.dirname(out)
    if d:
        os.makedirs(d, exist_ok=True)
    with open(out, "w") as f:
        f.write(md)
    lanes.record_event("handoff", lane=lane, out=out, agent=lanes.lane_agent(lane))
    return f"wrote {out}\n起動の 1 行: {sl}"
