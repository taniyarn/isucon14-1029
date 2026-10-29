#!/usr/bin/env python3
"""task lane:timeline [FROM=hh:mm] — where the time went, as Markdown for a retro.

Reads <board dir>/events.jsonl (appended by lanes.py on every board change, by ship.sh / the deploy script / runs:wait
through `lanes.py event`, and by lane:web's 「🏁 終わった」 on the bench todo) and prints:
  1. a chronological timeline of the key events
  2. milestones: URL received → baseline bench ready → each bench ready / running / ended / score → first score ≥ prize_target
  3. per ship: total and per sub-step durations (+ deploy per host)
  4. per todo kind: count, median and max human response time
  5. per lane: share of time in each state
  6. Claude Code per lane, from <board dir>/cc-events.jsonl (the cchook.py hook): turns (UserPromptSubmit → Stop),
     busy vs idle, slowest tool calls, permission waits (permission Notification → the session's next event),
     subagents, cross-session message → the receiver's pickup; with <board dir>/otel/logs.jsonl (task otel:up) also
     tokens / cost / API request duration per lane
  7. idle breakdown: each Claude Code idle gap (Stop → the session's next UserPromptSubmit) crossed with the lane's
     board state: 人を待っていた / サブエージェント待ち (the board's AGENTS > 0, or a subagent of that session started
     by Agent / Task without its SubagentStop yet) / 他レーンを待っていた / やることが無かった / 盤の打ち忘れ (board says
     working, no subagent) / 盤の記録なし; idle minutes per lane before the first score ≥ prize_target; every gap ≥ 3 min of lanes 2 / 3 / c
     with the board's STEP / NOTE and the hub's STEP at that time (to judge [TASK] delays)
Scores come from run_scored events and, when OPS_HOST / SSH_KEY are set (or RUNS_JSON for tests), from runs.py too.
FROM=hh:mm = the latest such time not after the last event (a practice that crosses midnight works).
"""
import json
import os
import statistics
import time
from datetime import datetime, timedelta, timezone

import lanes

KEY_KINDS = {"phase", "bench", "bench_done", "ship.start", "ship.end", "deploy.end", "todo", "todo_done", "alert",
             "resolve", "run_seen", "run_scored", "bench_done_task", "msg", "handoff", "note"}
SHIP_STEPS = ["sync", "merge", "record", "deploy", "smoke", "todo"]
STATES = lanes.STATES


def load_events(path=None):
    path = path or lanes.events_path()
    out = []
    try:
        with open(path) as f:
            for line in f:
                try:
                    e = json.loads(line)
                except ValueError:
                    continue
                if isinstance(e, dict) and isinstance(e.get("t"), (int, float)) and e.get("kind"):
                    out.append(e)
    except OSError:
        pass
    return sorted(out, key=lambda e: e["t"])  # stable: same-second events keep their order


def hm(t):
    return datetime.fromtimestamp(t).strftime("%H:%M:%S") if t else "-"


def dur(sec):
    if sec is None:
        return "-"
    sec = int(round(sec))
    if sec < 60:
        return f"{sec}s"
    if sec < 3600:
        return f"{sec // 60}m{sec % 60:02d}s"
    return f"{sec // 3600}h{sec % 3600 // 60:02d}m"


def parse_from(frm, last_t):
    """hh:mm → epoch: the latest such local time that is not after last_t."""
    if not frm:
        return None
    h, m = (int(x) for x in frm.strip().split(":")[:2])
    ref = datetime.fromtimestamp(last_t or time.time())
    cand = ref.replace(hour=h, minute=m, second=0, microsecond=0)
    if cand > ref:
        cand -= timedelta(days=1)
    return cand.timestamp()


def run_epoch(ts):
    """Panel run ts (YYYYmmdd-HHMMSS, UTC) → epoch; None when it does not parse."""
    try:
        return datetime.strptime(ts, "%Y%m%d-%H%M%S").replace(tzinfo=timezone.utc).timestamp()
    except (TypeError, ValueError):
        return None


def fetch_runs():
    """Scored runs from the ops panel through runs.py, [] when not configured or unreachable."""
    if not (os.environ.get("RUNS_JSON") or (os.environ.get("OPS_HOST") and os.environ.get("SSH_KEY"))):
        return []
    try:
        import runs
        return runs.fetch() or []
    except Exception:
        return []


def describe(e):
    k = e["kind"]
    if k == "phase":
        return f"phase → **{e.get('phase')}**"
    if k == "bench":
        return f"帯 {e.get('frm')} → **{e.get('to')}**" + (f"（main {e['sha']}）" if e.get("sha") else "") + \
            (f" {e['prs']}" if e.get("prs") else "") + (f" [{e['src']}]" if e.get("src") else "")
    if k == "bench_done":
        return "🏁 人が「終わった」" + (f"（{e['id']}）" if e.get("id") else "") + (f" score {e['score']}" if e.get("score") else "")
    if k == "note":
        return f"{lanes.NOTE_KINDS.get(e.get('nkind'), '📝')} lane {e.get('lane')} [{e.get('nkind')}] {e.get('msg', '')}" + \
            (f"（{e['ref']}）" if e.get("ref") else "")
    if k == "bench_done_task":
        return "task bench:done " + ("ok" if e.get("ok") else "失敗") + (f"（{dur(e.get('sec'))}）" if e.get("sec") else "") + \
            (f" {e['err']}" if e.get("err") else "")
    if k == "ship.start":
        return f"hub:ship 開始 PRS={e.get('prs', '')}"
    if k == "ship.end":
        return f"hub:ship {'完了' if e.get('ok') else '**失敗**'}（{dur(e.get('sec'))}）" + \
            (f" merged {e['merged']}" if e.get("merged") else "") + (f" mode {e['mode']}" if e.get("mode") else "")
    if k == "deploy.end":
        return f"deploy {'ok' if e.get('ok') else '**失敗**'}（{dur(e.get('sec'))}）"
    if k == "todo":
        return f"todo {e.get('id')} [{e.get('tkind')}] 作成: {e.get('msg', '')}"
    if k == "todo_done":
        d = e.get("decision", "done")
        extra = (f" ok {e['ok_prs']}" if e.get("ok_prs") else "") + (f" 保留 {e['held_prs']}" if e.get("held_prs") else "")
        return f"todo {e.get('id')} [{e.get('tkind')}] → {d}{extra}（人 {dur(e.get('sec'))}, {e.get('by', '')}）"
    if k == "alert":
        return f"🚨 alert {e.get('id')} {e.get('level')} lane {e.get('lane')}: {e.get('msg', '')}"
    if k == "resolve":
        return f"alert {e.get('id')} 解消（{dur(e.get('sec'))}）"
    if k == "msg":
        return f"✉ {e.get('id')} {e.get('frm')}→{e.get('to')}: {e.get('msg', '')}"
    if k == "msg_read":
        return f"✉ {e.get('id')} を lane {e.get('lane')} が読んだ（{dur(e.get('sec'))}）"
    if k == "handoff":
        return f"引き継ぎ lane {e.get('lane')} → {e.get('out')}（後継 {e.get('agent', '')}）"
    if k == "run_seen":
        return f"runs:wait が新しい run を検出 {e.get('ts')}（req {e.get('req', '?')}）"
    if k == "run_scored":
        return f"run {e.get('ts')} score **{e.get('score')}**"
    return k


def url_time(evs):
    """When the contest URL arrived: the human sent t3 (todo kind url), else roadmap step H1 → doing; None."""
    url_t = next((e["t"] for e in evs if e["kind"] == "todo_done" and e.get("tkind") == "url"), None)
    if url_t is None:
        url_t = next((e["t"] for e in evs if e["kind"] == "step" and e.get("id") == "H1" and e.get("state") == "doing"), None)
    return url_t


def bench_cycles(evs, scores):
    """One cycle per band → ready: ready / running / ended (collect, bench_done or none) / score. A → ready while the
    current cycle has not ended and names the same sha (or none) is the same bench (2026-10-27 20:54:42: the band was
    sent back to ready during a run), not a new row."""
    cycles = []
    for e in evs:
        if e["kind"] == "bench" and e.get("to") == "ready":
            if cycles and cycles[-1]["ended"] is None and (not e.get("sha") or e.get("sha") == cycles[-1]["sha"]):
                continue
            cycles.append({"ready": e["t"], "sha": e.get("sha", ""), "prs": e.get("prs", ""), "running": None,
                           "ended": None, "none": None, "score": None, "score_t": None})
            continue
        if not cycles:
            continue
        c = cycles[-1]
        if e["kind"] == "bench" and e.get("to") == "running" and c["running"] is None:
            c["running"] = e["t"]
        elif ((e["kind"] == "bench" and e.get("to") in ("collect", "none")) or e["kind"] == "bench_done") and c["ended"] is None:
            c["ended"] = e["t"]
        if e["kind"] == "bench" and e.get("to") == "none" and c["none"] is None:
            c["none"] = e["t"]
    for i, c in enumerate(cycles):
        end = cycles[i + 1]["ready"] if i + 1 < len(cycles) else float("inf")
        cand = [(t, sc) for t, sc in scores if c["ready"] <= t < end]
        if cand:
            c["score_t"], c["score"] = cand[0]
    return cycles


def collect_scores(evs, runs_rows):
    """[(t, score)] sorted: run_scored events (t = when the hub learned it) and panel runs (t = run start)."""
    out, seen = [], set()
    for e in evs:
        if e["kind"] == "run_scored":
            try:
                out.append((e["t"], float(e.get("score"))))
                seen.add(e.get("ts"))
            except (TypeError, ValueError):
                pass
    for r in runs_rows or []:
        if r.get("ts") in seen:
            continue
        t = run_epoch(r.get("ts"))
        try:
            sc = float(str(r.get("score")))
        except (TypeError, ValueError):
            continue
        if t:
            out.append((t, sc))
    return sorted(out)


def lane_shares(evs, start, end):
    """{lane: {state: seconds}} from lane events, clipped to [start, end]."""
    cur, out = {}, {}
    for e in evs:
        if e["kind"] != "lane":
            continue
        ln = e.get("lane")
        if ln not in cur:
            cur[ln] = (e.get("frm") or "idle", e["t"])  # before its first change: what it changed from
        st, since = cur[ln]
        a, b = max(since, start), min(e["t"], end)
        if b > a:
            out.setdefault(ln, {}).setdefault(st, 0)
            out[ln][st] += b - a
        cur[ln] = (e.get("to") or st, e["t"])
    for ln, (st, since) in cur.items():
        a = max(since, start)
        if end > a:
            out.setdefault(ln, {}).setdefault(st, 0)
            out[ln][st] += end - a
    return out


# ---------- 6. Claude Code (hooks → cc-events.jsonl, optional OTel logs) ----------

def load_jsonl(path):
    out = []
    try:
        with open(path) as f:
            for line in f:
                try:
                    e = json.loads(line)
                except ValueError:
                    continue
                if isinstance(e, dict):
                    out.append(e)
    except OSError:
        pass
    return out


def load_cc(path=None):
    path = path or os.path.join(lanes.lanes_dir(), "cc-events.jsonl")
    out = []
    for e in load_jsonl(path):
        if isinstance(e.get("ts"), (int, float)) and e.get("event"):
            out.append({**e, "t": e["ts"] / 1000.0})
    return sorted(out, key=lambda e: e["t"])


def pct(xs, p):
    xs = sorted(xs)
    if not xs:
        return None
    return xs[min(len(xs) - 1, int(round(p * (len(xs) - 1))))]


def is_permission(e):
    return e.get("event") == "Notification" and (e.get("ntype") == "permission_prompt" or "permission" in (e.get("msg") or "").lower())


def norm(s):
    return " ".join(str(s or "").split())[:30]


def cc_stats(cc, start, end):
    """Per lane: turns, busy, span, tools, permission waits, subagents; plus message gaps (send → receiver's prompt)."""
    by_sid, lane_of = {}, {}
    for e in cc:
        sid = e.get("sid") or "?"
        by_sid.setdefault(sid, []).append(e)
        if e.get("lane") and e["lane"] != "?":
            lane_of.setdefault(sid, e["lane"])
    st = {}
    worked = {e.get("agent_id") for e in cc if e.get("agent_id") and e["event"] != "SubagentStop"}
    for sid, xs in by_sid.items():
        ln = lane_of.get(sid, "?")
        L = st.setdefault(ln, {"turns": [], "busy": 0.0, "span": 0.0, "tools": [], "perm": [], "sub_n": 0, "sub": [],
                               "sessions": 0})
        L["sessions"] += 1
        win = [e for e in xs if start <= e["t"] <= end]
        if win:
            L["span"] += win[-1]["t"] - win[0]["t"]
        turn, pre, fifo, agent_pre = None, {}, {}, {}
        for i, e in enumerate(xs):
            ev = e["event"]
            if ev == "UserPromptSubmit":
                turn = e["t"]
            elif ev == "Stop" and turn is not None:
                if turn >= start and e["t"] <= end:
                    L["turns"].append(e["t"] - turn)
                    L["busy"] += e["t"] - turn
                turn = None
            elif ev == "PreToolUse":
                if e.get("tuid"):
                    pre[e["tuid"]] = e
                else:
                    fifo.setdefault(e.get("tool"), []).append(e)
                if e.get("tool") in ("Task", "Agent") and e.get("tuid"):
                    agent_pre[e["tuid"]] = e
            elif ev == "PostToolUse":
                p = pre.pop(e.get("tuid"), None) if e.get("tuid") else None
                if p is None and fifo.get(e.get("tool")):
                    p = fifo[e.get("tool")].pop(0)
                if p and p["t"] >= start and e["t"] <= end:
                    L["tools"].append((e["t"] - p["t"], p.get("tool", ""), p.get("cmd", ""), p["t"]))
            elif ev == "SubagentStop" and start <= e["t"] <= end and (e.get("agent_type") or e.get("agent_id") in worked):
                L["sub_n"] += 1
                p = agent_pre.pop(e.get("tuid"), None) if e.get("tuid") else None
                if p:
                    L["sub"].append(e["t"] - p["t"])
            if is_permission(e) and start <= e["t"] <= end:
                nxt = next((y for y in xs[i + 1:] if y["event"] in ("PreToolUse", "PostToolUse", "UserPromptSubmit", "Stop")), None)
                if nxt:
                    L["perm"].append(nxt["t"] - e["t"])
        if not L["sub"]:  # SubagentStop carries no tool_use_id: Agent call → its stop, matched in order
            L["sub"] += [b - a for a, b in subagent_spans(xs).get(sid, []) if start <= b <= end and b - a < SUBAGENT_CAP]
    gaps = []
    sends = [e for e in cc if e.get("event") == "PreToolUse" and e.get("tool") == "SendMessage" and e.get("head")
             and start <= e["t"] <= end]
    prompts = [e for e in cc if e.get("event") == "UserPromptSubmit" and e.get("head")]
    for snd in sends:
        h = norm(snd["head"])
        got = next((p for p in prompts if p["t"] >= snd["t"] and p.get("sid") != snd.get("sid")
                    and (norm(p["head"]).startswith(h) or h.startswith(norm(p["head"])))), None)
        if got:
            gaps.append((lane_of.get(got.get("sid"), "?"), got["t"] - snd["t"], h))
    return st, gaps


def otel_value(v):
    if not isinstance(v, dict):
        return None
    for k in ("stringValue", "intValue", "doubleValue", "boolValue"):
        if k in v:
            return v[k]
    return None


def otel_api_requests(rows, start, end):
    """{lane: {"in", "out", "cache_read", "cache_create", "cost", "dur": [ms]}} from the collector's logs.jsonl."""
    out = {}
    for row in rows:
        for rl in row.get("resourceLogs") or []:
            rattr = {a.get("key"): otel_value(a.get("value")) for a in (rl.get("resource") or {}).get("attributes") or []}
            ln = str(rattr.get("lane") or "?")
            for sl in rl.get("scopeLogs") or []:
                for rec in sl.get("logRecords") or []:
                    at = {a.get("key"): otel_value(a.get("value")) for a in rec.get("attributes") or []}
                    name = str(at.get("event.name") or otel_value(rec.get("body")) or "")
                    if not name.endswith("api_request"):
                        continue
                    try:
                        t = int(rec.get("timeUnixNano") or rec.get("observedTimeUnixNano") or 0) / 1e9
                    except (TypeError, ValueError):
                        t = 0
                    if t and not (start <= t <= end):
                        continue
                    L = out.setdefault(ln, {"in": 0, "out": 0, "cache_read": 0, "cache_create": 0, "cost": 0.0, "dur": []})

                    def num(k):
                        try:
                            return float(at.get(k) or 0)
                        except (TypeError, ValueError):
                            return 0.0
                    L["in"] += int(num("input_tokens"))
                    L["out"] += int(num("output_tokens"))
                    L["cache_read"] += int(num("cache_read_tokens"))
                    L["cache_create"] += int(num("cache_creation_tokens"))
                    L["cost"] += num("cost_usd")
                    if num("duration_ms"):
                        L["dur"].append(num("duration_ms"))
    return out


LOAD_BUCKETS = [(0, 4, "< 4"), (4, 8, "4–8"), (8, float("inf"), "≥ 8")]


def bash_load_lines(cc, start, end):
    """Bash time (Pre → Post) per load1 bucket of the PreToolUse line, and the hook's own hook_ms (cchook.py records
    both since 2026-10-29; older logs have neither → no lines). Separates "the laptop was loaded" from "the hook is slow"."""
    pre, rows, hooks = {}, [], []
    for e in cc:
        if e.get("event") not in ("PreToolUse", "PostToolUse") or not (start <= e["t"] <= end):
            continue
        if isinstance(e.get("hook_ms"), (int, float)):
            hooks.append(e["hook_ms"])
        if e.get("tool") != "Bash" or not e.get("tuid"):
            continue
        if e["event"] == "PreToolUse":
            pre[e["tuid"]] = e
        elif e["tuid"] in pre:
            p = pre.pop(e["tuid"])
            if isinstance(p.get("load1"), (int, float)):
                rows.append((p["load1"], e["t"] - p["t"]))
    if not rows and not hooks:
        return []
    lines = ["\nBash の時間と手元の負荷（PreToolUse 時点の 1 分 load average）:\n",
             "| load1 | 回数 | Bash 中央値 | p90 |", "|---|---|---|---|"]
    for lo, hi, label in LOAD_BUCKETS:
        xs = [d for ld, d in rows if lo <= ld < hi]
        if xs:
            lines.append(f"| {label} | {len(xs)} | {statistics.median(xs):.1f}s | {pct(xs, 0.9):.1f}s |")
    if hooks:
        lines.append(f"\nhook 自身の時間（hook_ms、Pre/PostToolUse {len(hooks)} 回）: 中央値 {statistics.median(hooks):.0f} ms、"
                     f"p90 {pct(hooks, 0.9):.0f} ms、最大 {max(hooks):.0f} ms")
    return lines


def cc_section(cc, start, end, otel_rows=None):
    lines = ["## 6. Claude Code（hooks の cc-events.jsonl）\n"]
    if not cc:
        lines.append("（記録なし: .claude/settings.json の hooks と、起動の 1 行の LANE=<lane> を確かめる）\n")
        return lines
    st, gaps = cc_stats(cc, start, end)
    lines.append("| レーン | セッション | ターン | 中央値 | p90 | 最大 | busy | idle | 許可待ち（回 / 計 / 最大） | サブエージェント |")
    lines.append("|---|---|---|---|---|---|---|---|---|---|")
    for ln in lanes.lane_order(st):
        L = st[ln]
        tu = L["turns"]
        sub = f"{L['sub_n']} 回" + (f"（中央値 {dur(statistics.median(L['sub']))}、最大 {dur(max(L['sub']))}）" if L["sub"] else "")
        perm = f"{len(L['perm'])} / {dur(sum(L['perm']))} / {dur(max(L['perm']))}" if L["perm"] else "0"
        lines.append(f"| {lanes.lane_label(ln)} | {L['sessions']} | {len(tu)} | {dur(statistics.median(tu)) if tu else '-'} | "
                     f"{dur(pct(tu, 0.9)) if tu else '-'} | {dur(max(tu)) if tu else '-'} | {dur(L['busy'])} | "
                     f"{dur(max(0, L['span'] - L['busy']))} | {perm} | {sub} |")
    lines.append("\n遅いツール呼び出し（レーンごとに上位 5）:\n")
    lines.append("| レーン | 時刻 | 秒 | ツール | コマンド（Bash の先頭 80 字） |")
    lines.append("|---|---|---|---|---|")
    for ln in lanes.lane_order(st):
        for sec, tool, cmd, t0 in sorted(st[ln]["tools"], reverse=True)[:5]:
            lines.append(f"| {lanes.lane_label(ln)} | {hm(t0)} | {dur(sec)} | {tool} | {cmd.replace('|', '/')} |")
    lines += bash_load_lines(cc, start, end)
    if gaps:
        lines.append("\nセッション間のメッセージ → 受け手の次の入力:\n")
        lines.append("| 受け手 | 件数 | 中央値 | 最大 |")
        lines.append("|---|---|---|---|")
        per = {}
        for ln, g, _ in gaps:
            per.setdefault(ln, []).append(g)
        for ln in lanes.lane_order(per):
            lines.append(f"| {lanes.lane_label(ln)} | {len(per[ln])} | {dur(statistics.median(per[ln]))} | {dur(max(per[ln]))} |")
    api = otel_api_requests(otel_rows or [], start, end)
    if api:
        lines.append("\nOTel（api_request）:\n")
        lines.append("| レーン | 入力 tok | 出力 tok | cache read | cache 作成 | cost USD | リクエスト | 中央値 |")
        lines.append("|---|---|---|---|---|---|---|---|")
        for ln in lanes.lane_order(api):
            L = api[ln]
            lines.append(f"| {lanes.lane_label(ln)} | {L['in']:,} | {L['out']:,} | {L['cache_read']:,} | {L['cache_create']:,} | "
                         f"{L['cost']:.2f} | {len(L['dur'])} | {dur(statistics.median(L['dur']) / 1000) if L['dur'] else '-'} |")
    lines.append("")
    return lines


# ---------- 7. idle breakdown (Claude Code idle × board state) ----------

IDLE_CATS = [("human", "人を待っていた"), ("agents", "サブエージェント待ち"), ("lane", "他レーンを待っていた"),
             ("none", "やることが無かった"), ("forgot", "盤の打ち忘れ（盤は working）"), ("unknown", "盤の記録なし")]
SUBAGENT_CAP = 1800  # an Agent call without its SubagentStop (failed / session ended) counts at most this long
STATE_CAT = {"waiting-human": "human", "alert": "human", "waiting": "lane", "idle": "none", "working": "forgot"}
LONG_GAP = 180


def idle_gaps(cc, start, end):
    """[(lane, sid, a, b, permission_seen)] — Stop → next UserPromptSubmit of the same main session (subagent events,
    i.e. with agent_id, and lane "?" are ignored), clipped to [start, end]."""
    by = {}
    for e in cc:
        if e.get("agent_id") or (e.get("lane") or "?") == "?":
            continue
        by.setdefault(e.get("sid") or "?", []).append(e)
    out = []
    for sid, xs in by.items():
        stop = None
        perm = False
        for e in xs:
            if e["event"] == "Stop":
                stop, perm = e["t"], False
            elif e["event"] == "Notification" and stop is not None and is_permission(e):
                perm = True
            elif e["event"] == "UserPromptSubmit" and stop is not None:
                a, b = max(stop, start), min(e["t"], end)
                if b > a:
                    out.append((e.get("lane"), sid, a, b, perm))
                stop = None
    return sorted(out, key=lambda g: g[2])


def board_timeline(evs):
    """{lane: [(t, state, step, note, agents)]} from lane / lane_step events (state after t; agents = the board's
    AGENTS, None on events written before it was logged)."""
    tl = {}
    for e in evs:
        if e["kind"] == "lane":
            tl.setdefault(e.get("lane"), []).append((e["t"], e.get("to"), e.get("step", ""), e.get("note", ""), e.get("agents")))
        elif e["kind"] == "lane_step":
            tl.setdefault(e.get("lane"), []).append((e["t"], e.get("state"), e.get("step", ""), e.get("note", ""), e.get("agents")))
    return tl


def subagent_spans(cc):
    """{sid: [(a, b)]} while a subagent of that session runs: each Agent / Task PreToolUse is matched to the session's
    next unmatched SubagentStop (FIFO); unmatched ones last SUBAGENT_CAP. Only stops of real subagents count: with an
    agent_type, or whose agent_id ran tools before (Claude Code also fires SubagentStop every ~30 s for internal helpers
    with neither — 2026-10-27: 40 of lane 3's 51)."""
    out = {}
    worked = {e.get("agent_id") for e in cc if e.get("agent_id") and e["event"] != "SubagentStop"}
    for sid in {e.get("sid") or "?" for e in cc}:
        xs = [e for e in cc if (e.get("sid") or "?") == sid]
        open_ = []
        spans = []
        for e in xs:
            if e["event"] == "PreToolUse" and e.get("tool") in ("Agent", "Task"):
                open_.append(e["t"])
            elif e["event"] == "SubagentStop" and open_ and (e.get("agent_type") or e.get("agent_id") in worked):
                spans.append((open_.pop(0), e["t"]))
        spans += [(a, a + SUBAGENT_CAP) for a in open_]
        if spans:
            out[sid] = sorted(spans)
    return out


def subagent_at(spans, t):
    return any(a <= t < b for a, b in spans or [])


def board_at(tl, lane, t):
    cur = None
    for x in tl.get(lane, []):
        if x[0] > t:
            break
        cur = x
    return cur


def split_gap(tl, lane, a, b, perm, spans=None):
    """{cat: seconds} for one idle gap; a permission prompt in the gap makes it all 人を待っていた. While a subagent runs
    (the board's AGENTS > 0 or one of the session's spans) the time is サブエージェント待ち unless the board says the
    lane waits on the human."""
    if perm:
        return {"human": b - a}
    cuts = [x[0] for x in tl.get(lane, [])] + [p for s in spans or [] for p in s]
    pts = [a] + sorted({p for p in cuts if a < p < b}) + [b]
    out = {}
    for x, y in zip(pts, pts[1:]):
        st = board_at(tl, lane, x)
        cat = STATE_CAT.get(st[1], "unknown") if st else "unknown"
        if cat != "human" and ((st and (st[4] or 0) > 0) or subagent_at(spans, x)):
            cat = "agents"
        out[cat] = out.get(cat, 0) + (y - x)
    return out


def idle_split(evs, cc, start, end, prize_t=None):
    """(gaps, per, before, tl, spans): per = {lane: {IDLE_CATS key: seconds}} over [start, end], before = {lane: idle
    seconds before prize_t} (practice.py reuses it)."""
    gaps = idle_gaps(cc, start, end)
    tl = board_timeline(evs)
    spans = subagent_spans(cc)
    per = {}
    before = {}
    for ln, sid, a, b, perm in gaps:
        P = per.setdefault(ln, {})
        for k, v in split_gap(tl, ln, a, b, perm, spans.get(sid)).items():
            P[k] = P.get(k, 0) + v
        if prize_t:
            before[ln] = before.get(ln, 0) + max(0, min(b, prize_t) - a)
    return gaps, per, before, tl, spans


def idle_section(evs, cc, start, end, prize_t=None):
    lines = ["## 7. 止まっていた時間の内訳（Claude Code の idle × 盤）\n"]
    gaps, per, before, tl, spans = idle_split(evs, cc, start, end, prize_t)
    if not gaps:
        lines.append("（記録なし: cc-events.jsonl に Stop → 次の入力が無い）\n")
        return lines
    if prize_t:
        lines.append(f"特別賞の目標を越えるまで（{hm(prize_t)}）の idle: "
                     + "、".join(f"{lanes.lane_label(ln)} **{before.get(ln, 0) / 60:.0f} 分**" for ln in lanes.lane_order(per)) + "\n")
    else:
        lines.append("特別賞の目標を越えた記録が無い: 全期間の idle: "
                     + "、".join(f"{lanes.lane_label(ln)} **{sum(per[ln].values()) / 60:.0f} 分**" for ln in lanes.lane_order(per)) + "\n")
    lines.append("| レーン | idle 計 | " + " | ".join(n for _, n in IDLE_CATS) + " |")
    lines.append("|---|---|" + "---|" * len(IDLE_CATS))
    for ln in lanes.lane_order(per):
        P = per[ln]
        lines.append(f"| {lanes.lane_label(ln)} | {dur(sum(P.values()))} | " + " | ".join(dur(P[k]) if P.get(k) else "-" for k, _ in IDLE_CATS) + " |")
    long = [g for g in gaps if g[0] in ("2", "3", "c") and g[3] - g[2] >= LONG_GAP]
    lines.append(f"\n{LONG_GAP // 60} 分以上止まっていた間（レーン 2 / 3 / c。その間にハブが [TASK] を渡せたかを人が判断する）:\n")
    if not long:
        lines.append("（なし）")
    else:
        lines.append("| レーン | 開始 | 長さ | 盤の状態 | 盤の STEP / NOTE | 内訳 | その時のハブ |")
        lines.append("|---|---|---|---|---|---|---|")
        for ln, sid, a, b, perm in long:
            st = board_at(tl, ln, a)
            hub = board_at(tl, "1", a)
            parts = split_gap(tl, ln, a, b, perm, spans.get(sid))
            lines.append(f"| {lanes.lane_label(ln)} | {hm(a)} | {dur(b - a)} | {(st[1] if st else '記録なし')}{'（許可確認あり）' if perm else ''} | "
                         + (f"{st[2]}" + (f" / {st[3]}" if st[3] else "") if st else "-").replace("|", "/") + " | "
                         + "、".join(f"{dict(IDLE_CATS)[k]} {dur(v)}" for k, v in parts.items()) + " | "
                         + (f"{hub[1]}: {hub[2]}".replace("|", "/") if hub else "-") + " |")
    lines.append("")
    return lines


def msg_section(evs):
    """Board messages (task lane:msg, the transport to / from Codex lanes): count and read latency per direction."""
    sent = [e for e in evs if e["kind"] == "msg"]
    if not sent:
        return []
    reads = {e.get("id"): e for e in evs if e["kind"] == "msg_read"}
    lines = ["## 5b. 盤のメッセージ（task lane:msg → lane:inbox / wait-msg）\n",
             "| 向き | 送った | 読んだ | 読むまで 中央値 | 最大 | 未読 |", "|---|---|---|---|---|---|"]
    pairs = {}
    for e in sent:
        pairs.setdefault(f"{e.get('frm')}→{e.get('to')}", []).append(e)
    for k in sorted(pairs):
        lat = sorted(int(reads[e["id"]].get("sec") or 0) for e in pairs[k] if e.get("id") in reads)
        med = lat[len(lat) // 2] if lat else None
        lines.append(f"| {k} | {len(pairs[k])} | {len(lat)} | {dur(med) if lat else '-'} | {dur(lat[-1]) if lat else '-'} | "
                     f"{len(pairs[k]) - len(lat)} |")
    lines.append("")
    return lines


def summarize(evs, frm=None, now=None, runs_rows=None, target=None, cc=None, otel_rows=None):
    lines = []
    cc = cc or []
    if not evs and not cc:
        return "# lane:timeline\n\n(イベントが無い: 盤の events.jsonl が空)\n"
    last = max(x[-1]["t"] for x in (evs, cc) if x)
    first = min(x[0]["t"] for x in (evs, cc) if x)
    start = parse_from(frm, last) if frm else first
    end = max(now or last, last) if now else last
    win = [e for e in evs if e["t"] >= start]
    lines.append(f"# lane:timeline {hm(start)} → {hm(end)}（{dur(end - start)}）\n")

    # 1. timeline
    lines.append("## 1. 時系列\n")
    lines.append("| 時刻 | 出来事 |")
    lines.append("|---|---|")
    for e in win:
        if e["kind"] in KEY_KINDS:
            lines.append(f"| {hm(e['t'])} | {describe(e).replace('|', '/')} |")
    lines.append("")

    # 2. milestones
    scores = collect_scores(evs, runs_rows)
    url_t = url_time(evs)
    base = url_t or start
    cycles = [(i, c) for i, c in enumerate(bench_cycles(evs, scores), 1) if c["ready"] >= start]  # numbered from the day's first
    lines.append("## 2. 節目\n")
    lines.append(f"起点 = {'URL 受信（t3 の送信）' if url_t else 'FROM / 最初のイベント'} {hm(base)}\n")
    lines.append("| 節目 | ready | running | ended | score | ready まで（起点から） | ready → ended | 載った PR |")
    lines.append("|---|---|---|---|---|---|---|---|")
    if url_t:
        lines.append(f"| URL 受信 | {hm(url_t)} | | | | 0s | | |")
    for i, c in cycles:
        name = "baseline ベンチ" if i == 1 else f"ベンチ {i}"
        sc = f"{c['score']:g}" if c["score"] is not None else "-"
        lines.append(f"| {name} | {hm(c['ready'])} | {hm(c['running'])} | {hm(c['ended'])} | {sc} | {dur(c['ready'] - base)} | "
                     f"{dur(c['ended'] - c['ready']) if c['ended'] else '-'} | {c['prs'] or '-'} |")
    hit = next(((t, sc) for t, sc in scores if sc >= target and t >= start), None) if target else None
    if target:
        lines.append("")
        lines.append(f"特別賞の目標 {target:,}: " + (f"**{hit[1]:g}** で越えた {hm(hit[0])}（起点から {dur(hit[0] - base)}）"
                                                    if hit else "まだ越えていない"))
    else:
        lines.append("\n特別賞の目標: alp.yml の prize_target が無い")
    lines.append("")

    # 3. ships
    lines.append("## 3. ship（hub:ship）\n")
    ships, curs = [], None
    for e in win:
        if e["kind"] == "ship.start":
            curs = {"t": e["t"], "prs": e.get("prs", ""), "steps": {}, "hosts": [], "end": None}
            ships.append(curs)
        elif curs and e["kind"] == "ship.step":
            curs["steps"][e.get("name")] = curs["steps"].get(e.get("name"), 0) + (e.get("sec") or 0)
        elif curs and e["kind"] == "deploy.host":
            curs["hosts"].append(f"{e.get('host')} {dur(e.get('sec'))}{'' if e.get('ok', 1) else ' 失敗'}"
                                 + (" (先)" if e.get("mode") == "first" else ""))
        elif curs and e["kind"] == "ship.end":
            curs["end"] = e
            curs = None
    if not ships:
        lines.append("（ship の記録なし）\n")
    else:
        lines.append("| 開始 | PRS | 合計 | " + " | ".join(SHIP_STEPS) + " | 結果 | deploy（ホストごと） |")
        lines.append("|---|---|---|" + "---|" * len(SHIP_STEPS) + "---|---|")
        for s in ships:
            e = s["end"] or {}
            res = ("ok" if e.get("ok") else "失敗") + (f" {e['mode']}" if e.get("mode") else "") if e else "（終わりの記録なし）"
            lines.append(f"| {hm(s['t'])} | {s['prs'] or '-'} | {dur(e.get('sec')) if e else '-'} | "
                         + " | ".join(dur(s["steps"][k]) if k in s["steps"] else "-" for k in SHIP_STEPS)
                         + f" | {res} | {', '.join(s['hosts']) or '-'} |")
        tot = [s["end"].get("sec") for s in ships if s["end"] and s["end"].get("sec") is not None]
        if tot:
            lines.append(f"\nship {len(tot)} 回: 中央値 {dur(statistics.median(tot))}、最大 {dur(max(tot))}")
    deps = [e for e in win if e["kind"] == "deploy.end"]
    if deps:
        lines.append(f"\ndeploy {len(deps)} 回（ship の外も含む）: 中央値 {dur(statistics.median(e.get('sec') or 0 for e in deps))}、"
                     f"最大 {dur(max(e.get('sec') or 0 for e in deps))}")
    lines.append("")

    # 4. human latency per todo kind
    lines.append("## 4. 人の応答時間（todo の種類ごと）\n")
    made, closed = {}, {}
    created_kind = {}
    for e in evs:
        if e["kind"] == "todo":
            created_kind[e.get("id")] = e.get("tkind", "other")
            if e["t"] >= start:
                made.setdefault(e.get("tkind", "other"), []).append(e.get("id"))
        elif e["kind"] == "todo_done" and e["t"] >= start:
            closed.setdefault(e.get("tkind") or created_kind.get(e.get("id"), "other"), []).append(e.get("sec") or 0)
    kinds = sorted(set(made) | set(closed))
    if not kinds:
        lines.append("（todo の記録なし）")
    else:
        lines.append("| 種類 | 作成 | 閉じた | 中央値 | 最大 |")
        lines.append("|---|---|---|---|---|")
        for k in kinds:
            xs = closed.get(k, [])
            lines.append(f"| {k} | {len(made.get(k, []))} | {len(xs)} | {dur(statistics.median(xs)) if xs else '-'} | "
                         f"{dur(max(xs)) if xs else '-'} |")
    lines.append("")

    # 5. lane time shares
    lines.append("## 5. レーンの時間配分\n")
    sh = lane_shares(evs, start, end)
    if not sh:
        lines.append("（レーンの status の記録なし）")
    else:
        lines.append("| レーン | " + " | ".join(STATES) + " | 計 |")
        lines.append("|---|" + "---|" * (len(STATES) + 1))
        for ln in lanes.lane_order(sh):
            tot = sum(sh[ln].values()) or 1
            lines.append(f"| {lanes.lane_label(ln)} | "
                         + " | ".join(f"{100 * sh[ln].get(s, 0) / tot:.0f}%（{dur(sh[ln].get(s, 0))}）" if sh[ln].get(s) else "-"
                                      for s in STATES) + f" | {dur(tot)} |")
    lines.append("")
    lines += msg_section(win)
    lines += cc_section(cc, start, end, otel_rows)
    lines += idle_section(evs, cc, start, end, hit[0] if hit else None)
    return "\n".join(lines)


def main(a):
    evs = load_events()
    otel = load_jsonl(os.path.join(lanes.lanes_dir(), "otel", "logs.jsonl"))
    return summarize(evs, frm=(a.get("from") or "").strip() or None, now=lanes.now(), runs_rows=fetch_runs(),
                     target=lanes.prize_target(), cc=load_cc(), otel_rows=otel)
