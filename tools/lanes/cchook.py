#!/usr/bin/env python3
"""Claude Code hook (.claude/settings.json → every event): one line per hook call into <board dir>/cc-events.jsonl, for
the "Claude Code" section of task lane:timeline. Never blocks or fails the session: no network, stdin capped, every
error swallowed, always exit 0. Prints nothing, except one case: the hub (LANE=1) about to Edit / Write a file of the
main checkout outside its own files (HUB_FILES) gets a non-blocking warning — PreToolUse JSON
hookSpecificOutput.additionalContext (Claude sees it next to the tool result) + systemMessage (the human sees it); no
permissionDecision, so the edit goes on as usual. 2026-10-27: the hub's hand edit in the main checkout blocked a ship.

Board state (LANE set, main session only, not subagents): UserPromptSubmit → working, SendMessage starting with [PR] /
[DONE] → waiting, Stop → idle (lanes.py op hook: never over alert; Stop never over a hand-set waiting / waiting-human;
the STEP text stays). 2026-10-28 ISUCON14: `task lane:status` was the most run command (111 calls, ~37 of them right
after a SendMessage), each one a model round trip; a lane now runs it only to change STEP or to say waiting-human.

Line: {"ts": <ms>, "lane": $LANE or "?", "sid", "event", "tool", "tuid", "agent_id", "agent_type", "ntype", "msg",
"cmd" (Bash: first 80 chars of the command), "to" / "head" (SendMessage: recipient, first 60 chars of the message),
"head" (UserPromptSubmit: the [TAG] line of a cross-session message, 60 chars; other prompts are not stored),
"hook_ms" / "load1" (Pre/PostToolUse: this hook's own time from its first line to the write, and the 1-min load
average; 2026-10-29 ISUCON11: Bash median 2.4 → 5.4 s while Read stayed 0.1 s, cause unknown)}.
Board dir: LANES_DIR, else <git common dir>/lanes (as lanes.py). The start todos put LANE=<1|2|3|c> before claude.
"""
import time

T0 = time.monotonic()

import json
import os
import re
import subprocess
import sys

HUB_FILES = re.compile(r"^(docs/(journal|claims|kickoff)\.md|docs/manual/.+|docs/plans/.+|CLAUDE\.md|AGENTS\.md|alp\.yml|deploy\.sh"
                       r"|tools/app-analyzer/analyzer\.config\.json)$")
EDIT_TOOLS = ("Edit", "Write", "MultiEdit", "NotebookEdit")
TAG = re.compile(r"\[(?:HELLO|TASK|PHASE|PR|DONE|RUN|MERGED|PICK|DECIDED|NEED-OK|ALERT|STOP)\][^\n]{0,80}")


_COMMON = None


def git_common(cwd=None):
    global _COMMON
    if _COMMON is None:
        cwd = cwd or os.environ.get("CLAUDE_PROJECT_DIR") or None
        _COMMON = subprocess.run(["git", "rev-parse", "--path-format=absolute", "--git-common-dir"], cwd=cwd,
                                 capture_output=True, text=True, timeout=2).stdout.strip()
    return _COMMON


def main_checkout_rel(path, common):
    """path relative to the main checkout (the worktree that owns <common>), or None when it is elsewhere (another
    worktree, even one nested inside the main checkout, or outside the repo)."""
    if not path or not common or os.path.basename(common.rstrip("/")) != ".git":
        return None
    root = os.path.realpath(os.path.dirname(common.rstrip("/")))
    p = os.path.realpath(path if os.path.isabs(path) else os.path.join(os.getcwd(), path))
    if os.path.commonpath([root, p]) != root or p == root:
        return None
    d = os.path.dirname(p)
    while d != root and len(d) > len(root):
        if os.path.exists(os.path.join(d, ".git")):  # a nested worktree / repo (.claude/worktrees/<x>)
            return None
        d = os.path.dirname(d)
    return os.path.relpath(p, root)


def hub_warning(h):
    """The warning text for the hub editing someone else's file in the main checkout, else ''."""
    if os.environ.get("LANE") != "1" or h.get("hook_event_name") != "PreToolUse" or h.get("tool_name") not in EDIT_TOOLS:
        return ""
    ti = h.get("tool_input") if isinstance(h.get("tool_input"), dict) else {}
    path = ti.get("file_path") or ti.get("notebook_path") or ""
    rel = main_checkout_rel(path, git_common(h.get("cwd") or None))
    if rel is None or HUB_FILES.match(rel.replace(os.sep, "/")):
        return ""
    return (f"[lanes] ハブが main の checkout の {rel} を編集しようとしている。ハブが main で書いてよいのは "
            "docs/journal.md docs/claims.md docs/kickoff.md docs/manual/ docs/plans/ CLAUDE.md alp.yml deploy.sh "
            "analyzer.config.json だけ。それ以外は worktree（git worktree add ../wt-<name> -b <name>）で書いて PR にするか、"
            "持ち主のレーンに [TASK] で渡す（未 commit の変更が hub:ship を止めることがある）。")


def board_dir():
    d = os.environ.get("LANES_DIR")
    if not d:
        common = git_common()
        if not common:
            return ""
        d = os.path.join(common, "lanes")
    os.makedirs(d, exist_ok=True)
    return d


def short(v, n):
    v = " ".join(str(v or "").split())
    return v[:n]


def hook_state(h):
    """The board state this event moves the lane to, or ''."""
    if not os.environ.get("LANE") or h.get("agent_id"):
        return ""
    ev = h.get("hook_event_name")
    if ev == "UserPromptSubmit":
        return "working"
    if ev == "Stop":
        return "idle"
    if ev == "PreToolUse" and h.get("tool_name") == "SendMessage":
        ti = h.get("tool_input") if isinstance(h.get("tool_input"), dict) else {}
        text = str(ti.get("message") or ti.get("content") or ti.get("text") or "").lstrip()
        if re.match(r"\[(PR|DONE)\]", text):
            return "waiting"
    return ""


def set_board_state(d, to):
    sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
    os.environ["LANES_DIR"] = d
    import lanes
    lanes.mutate("hook", {"lane": os.environ["LANE"], "to": to})


def sent_message(h):
    """The [PR] / [DONE] / [PICK] line a lane session sends with SendMessage (plan.json done_on), or ''."""
    if not os.environ.get("LANE") or h.get("agent_id") or h.get("hook_event_name") != "PreToolUse" \
            or h.get("tool_name") != "SendMessage":
        return ""
    ti = h.get("tool_input") if isinstance(h.get("tool_input"), dict) else {}
    text = str(ti.get("message") or ti.get("content") or ti.get("text") or "").lstrip()
    return text[:400] if re.match(r"\[(PR|DONE|PICK)\]", text) else ""


def set_board_steps(d, text):
    sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
    os.environ["LANES_DIR"] = d
    import lanes
    lanes.mutate("msgstep", {"lane": os.environ["LANE"], "msg": text})


def record(h):
    ev = h.get("hook_event_name") or ""
    ti = h.get("tool_input") if isinstance(h.get("tool_input"), dict) else {}
    out = {"ts": int(time.time() * 1000), "lane": os.environ.get("LANE") or "?", "sid": h.get("session_id", ""),
           "event": ev, "tool": h.get("tool_name", ""), "tuid": h.get("tool_use_id", ""),
           "agent_id": h.get("agent_id", ""), "agent_type": h.get("agent_type", "")}
    if ev == "Notification":
        out["ntype"] = short(h.get("notification_type"), 40)
        out["msg"] = short(h.get("message"), 160)
    if out["tool"] == "Bash" and ev == "PreToolUse":
        out["cmd"] = short(ti.get("command"), 80)
    if out["tool"] == "SendMessage" and ev == "PreToolUse":
        out["to"] = short(ti.get("to") or ti.get("recipient") or ti.get("name"), 60)
        out["head"] = short(ti.get("message") or ti.get("content") or ti.get("text"), 60)
    if ev == "UserPromptSubmit":
        m = TAG.search(str(h.get("prompt") or "")[:2000])
        if m:
            out["head"] = short(m.group(0), 60)
    return {k: v for k, v in out.items() if v not in ("", None)}


def main():
    try:
        raw = sys.stdin.read(1 << 20)
        h = json.loads(raw) if raw.strip() else {}
        if not isinstance(h, dict) or not h.get("hook_event_name"):
            return
        line = record(h)
        try:
            warn = hub_warning(h)
        except Exception:
            warn = ""
        if warn:
            line["warn"] = "hub-main-edit"
            sys.stdout.write(json.dumps({"systemMessage": warn, "hookSpecificOutput": {
                "hookEventName": "PreToolUse", "additionalContext": warn}}, ensure_ascii=False))
            sys.stdout.flush()
        d = board_dir()
        if not d:
            return
        if line.get("event") in ("PreToolUse", "PostToolUse"):
            try:
                line["load1"] = round(os.getloadavg()[0], 2)
            except Exception:
                pass
            line["hook_ms"] = int((time.monotonic() - T0) * 1000)
        with open(os.path.join(d, "cc-events.jsonl"), "a") as f:
            f.write(json.dumps(line, ensure_ascii=False) + "\n")
        to = hook_state(h)
        if to:
            set_board_state(d, to)
        text = sent_message(h)
        if text:
            set_board_steps(d, text)
    except Exception:
        pass


if __name__ == "__main__":
    main()
    sys.exit(0)
