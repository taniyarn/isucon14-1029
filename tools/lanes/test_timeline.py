# python3 -m unittest tools/lanes/test_timeline.py — the lane:timeline summarizer on synthetic events
import json
import os
import subprocess
import sys
import tempfile
import unittest
from datetime import datetime

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import timeline  # noqa: E402

T = int(datetime(2026, 10, 27, 0, 0, 0).timestamp())  # local midnight: hh:mm below are local


def at(m, s=0):
    return T + m * 60 + s


EVENTS = [
    {"t": at(10), "kind": "lane", "lane": "1", "frm": "idle", "to": "working"},
    {"t": at(10), "kind": "todo", "id": "t3", "tkind": "url", "msg": "URL"},
    {"t": at(16), "kind": "todo_done", "id": "t3", "tkind": "url", "decision": "done", "by": "web", "sec": 360},
    {"t": at(16), "kind": "lane", "lane": "2", "frm": "idle", "to": "working"},
    {"t": at(20), "kind": "lane", "lane": "2", "frm": "working", "to": "waiting-human"},
    {"t": at(30), "kind": "lane", "lane": "2", "frm": "waiting-human", "to": "working"},
    {"t": at(34), "kind": "bench", "frm": "none", "to": "ready", "sha": "0bf6235"},
    {"t": at(35), "kind": "bench", "frm": "ready", "to": "running", "src": "runs"},
    {"t": at(38), "kind": "bench_done", "score": "3238"},
    {"t": at(38), "kind": "bench", "frm": "running", "to": "collect", "src": "bench_done"},
    {"t": at(40), "kind": "run_scored", "ts": "x1", "score": "3238"},
    {"t": at(40), "kind": "bench", "frm": "collect", "to": "none", "src": "runs"},
    {"t": at(41), "kind": "todo", "id": "t9", "tkind": "review", "prs": "2 1"},
    {"t": at(44), "kind": "todo_done", "id": "t9", "tkind": "review", "decision": "ok", "by": "web", "sec": 180,
     "ok_prs": "2 1"},
    {"t": at(45), "kind": "ship.start", "prs": "2 1"},
    {"t": at(45, 5), "kind": "ship.step", "name": "sync", "sec": 5},
    {"t": at(45, 30), "kind": "ship.step", "name": "merge", "sec": 25},
    {"t": at(45, 40), "kind": "ship.step", "name": "record", "sec": 10},
    {"t": at(46), "kind": "deploy.host", "host": "10.0.0.3", "sec": 20, "ok": 1, "mode": "first"},
    {"t": at(46, 30), "kind": "deploy.host", "host": "10.0.0.1", "sec": 30, "ok": 1, "mode": "parallel"},
    {"t": at(46, 30), "kind": "deploy.end", "sec": 50, "ok": 1},
    {"t": at(46, 30), "kind": "ship.step", "name": "deploy", "sec": 50},
    {"t": at(47), "kind": "ship.step", "name": "smoke", "sec": 30},
    {"t": at(47), "kind": "todo", "id": "t12", "tkind": "bench"},
    {"t": at(47), "kind": "bench", "frm": "none", "to": "ready", "sha": "ec08737", "prs": "#2 #1"},
    {"t": at(47), "kind": "ship.step", "name": "todo", "sec": 1},
    {"t": at(47), "kind": "ship.end", "ok": 1, "sec": 120, "merged": "#2 #1", "mode": "deploy"},
    {"t": at(48), "kind": "todo_done", "id": "t12", "tkind": "bench", "decision": "done", "by": "web", "sec": 60},
    {"t": at(48), "kind": "bench", "frm": "ready", "to": "running"},
    {"t": at(50), "kind": "bench", "frm": "running", "to": "collect"},
    {"t": at(52), "kind": "run_scored", "ts": "x2", "score": "55000"},
    {"t": at(52), "kind": "bench", "frm": "collect", "to": "none"},
    {"t": at(53), "kind": "alert", "id": "a4", "level": "crit", "lane": "1", "msg": "smoke | 500"},
    {"t": at(60), "kind": "lane", "lane": "1", "frm": "working", "to": "idle"},
]


class TimelineTest(unittest.TestCase):
    def test_summary_sections(self):
        md = timeline.summarize(EVENTS, target=50000)
        self.assertTrue(md.startswith("# lane:timeline 00:10:00 → 01:00:00（50m00s）"), md[:80])
        for h in ("## 1. 時系列", "## 2. 節目", "## 3. ship", "## 4. 人の応答時間", "## 5. レーンの時間配分"):
            self.assertIn(h, md)
        # 1: key events only, pipes escaped so the table holds
        self.assertIn("| 00:38:00 | 🏁 人が「終わった」 score 3238 |", md)
        self.assertIn("smoke / 500", md)
        self.assertNotIn("| 00:45:05 |", md)  # ship sub-steps are in section 3, not the timeline
        # 2: URL → baseline: ready 18 min after the URL, ended at bench_done, score from run_scored
        self.assertIn("| URL 受信 | 00:16:00 |", md)
        self.assertIn("| baseline ベンチ | 00:34:00 | 00:35:00 | 00:38:00 | 3238 | 18m00s | 4m00s | - |", md)
        self.assertIn("| ベンチ 2 | 00:47:00 | 00:48:00 | 00:50:00 | 55000 | 31m00s | 3m00s | #2 #1 |", md)
        self.assertIn("**55000** で越えた 00:52:00（起点から 36m00s）", md)
        # 3: one ship with its steps and the hosts
        self.assertIn("| 00:45:00 | 2 1 | 2m00s | 5s | 25s | 10s | 50s | 30s | 1s | ok deploy | 10.0.0.3 20s (先), 10.0.0.1 30s |", md)
        # 4: human latency per kind
        self.assertIn("| review | 1 | 1 | 3m00s | 3m00s |", md)
        self.assertIn("| url | 1 | 1 | 6m00s | 6m00s |", md)
        self.assertIn("| bench | 1 | 1 | 1m00s | 1m00s |", md)
        # 5: lane 2 from 00:16 to 01:00 = 44 min: waiting-human 10 min
        row2 = next(x for x in md.splitlines() if x.startswith("| 2 インフラ"))
        self.assertIn("23%（10m00s）", row2)
        self.assertIn("77%（34m00s）", row2)  # working 4 + 30
        row1 = next(x for x in md.splitlines() if x.startswith("| 1 ハブ"))
        self.assertIn("100%（50m00s）", row1)

    def test_from_and_runs_and_no_events(self):
        md = timeline.summarize(EVENTS, frm="00:45", target=None)
        ms = md.split("## 2.")[1].split("## 3.")[0]
        self.assertNotIn("baseline ベンチ", ms)  # before FROM
        self.assertIn("| ベンチ 2 | 00:47:00 |", ms)  # numbered from the day's first bench
        self.assertIn("prize_target が無い", md)
        self.assertNotIn("00:38:00", md.split("## 2.")[0])
        self.assertIn("(イベントが無い", timeline.summarize([]))
        # a panel run (UTC ts) fills a bench without a run_scored event
        ev = [e for e in EVENTS if e["kind"] != "run_scored"]
        utc = datetime.utcfromtimestamp(at(36)).strftime("%Y%m%d-%H%M%S")
        md = timeline.summarize(ev, runs_rows=[{"ts": utc, "score": 4000}], target=3000)
        self.assertIn("| 4000 |", md)
        self.assertIn("**4000** で越えた 00:36:00", md)
        self.assertEqual(timeline.parse_from("23:50", at(30)), at(-10))  # across midnight

    def test_cli_reads_events_jsonl(self):
        d = tempfile.mkdtemp()
        with open(os.path.join(d, "events.jsonl"), "w") as f:
            for e in EVENTS:
                f.write(json.dumps(e, ensure_ascii=False) + "\n")
            f.write("not json\n")
        env = {k: v for k, v in os.environ.items() if k not in ("OPS_HOST", "SSH_KEY", "RUNS_JSON")}
        env["LANES_DIR"] = d
        r = subprocess.run([sys.executable, os.path.join(os.path.dirname(os.path.abspath(__file__)), "lanes.py"),
                            "timeline", "from=00:30"], capture_output=True, text=True, env=env)
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertIn("# lane:timeline 00:30:00", r.stdout)
        self.assertIn("## 3. ship", r.stdout)


def cc(m, s, sid, event, lane="1", **kw):
    return {"ts": int(at(m, s) * 1000), "t": at(m, s), "lane": lane, "sid": sid, "event": event, **kw}


CC = [
    cc(10, 0, "h", "SessionStart"),
    cc(10, 0, "h", "UserPromptSubmit"),
    cc(10, 5, "h", "PreToolUse", tool="Bash", tuid="u1", cmd="task hub:ship PRS=\"5 4\""),
    cc(10, 6, "h", "Notification", ntype="permission_prompt", msg="Claude needs your permission to use Bash"),
    cc(10, 46, "h", "PostToolUse", tool="Bash", tuid="u1"),  # 40 s in the permission prompt
    cc(10, 50, "h", "PreToolUse", tool="SendMessage", tuid="u2", to="lane2", head="[TASK] 2 setup"),
    cc(10, 51, "h", "PostToolUse", tool="SendMessage", tuid="u2"),
    cc(11, 0, "h", "Stop"),  # turn 1: 60 s
    cc(12, 0, "h", "UserPromptSubmit"),
    cc(12, 1, "h", "PreToolUse", tool="Agent", tuid="a1"),
    cc(12, 2, "h", "PostToolUse", tool="Agent", tuid="a1"),
    cc(12, 20, "h", "Stop"),  # turn 2: 20 s
    cc(15, 0, "h", "SubagentStop", tuid="a1", agent_id="ag1", agent_type="general-purpose"),  # 179 s after its Agent call
    cc(10, 30, "i", "SessionStart", lane="2"),
    cc(11, 10, "i", "UserPromptSubmit", lane="2", head="[TASK] 2 setup"),  # picked up 20 s after the send
    cc(11, 11, "i", "PreToolUse", lane="2", tool="Read", tuid="r1"),
    cc(11, 12, "i", "PostToolUse", lane="2", tool="Read", tuid="r1"),
    cc(13, 10, "i", "Stop", lane="2"),  # 120 s
]

OTEL = [{"resourceLogs": [{"resource": {"attributes": [{"key": "lane", "value": {"stringValue": "1"}}]},
                           "scopeLogs": [{"logRecords": [
                               {"timeUnixNano": str(int(at(10, 20) * 1e9)), "body": {"stringValue": "claude_code.api_request"},
                                "attributes": [{"key": "event.name", "value": {"stringValue": "api_request"}},
                                               {"key": "input_tokens", "value": {"stringValue": "1200"}},
                                               {"key": "output_tokens", "value": {"intValue": "300"}},
                                               {"key": "cost_usd", "value": {"doubleValue": 0.25}},
                                               {"key": "duration_ms", "value": {"stringValue": "4000"}}]},
                               {"body": {"stringValue": "claude_code.tool_result"}, "attributes": []}]}]}]}]


class ClaudeCodeSectionTest(unittest.TestCase):
    def test_cc_section(self):
        md = timeline.summarize([], cc=sorted(CC, key=lambda e: e["t"]), otel_rows=OTEL)
        sec = md.split("## 6. Claude Code")[1]
        row1 = next(x for x in sec.splitlines() if x.startswith("| 1 ハブ | 1 |"))
        self.assertEqual(row1, "| 1 ハブ | 1 | 2 | 40s | 1m00s | 1m00s | 1m20s | 3m40s | 1 / 40s / 40s | 1 回（中央値 2m59s、最大 2m59s） |")
        row2 = next(x for x in sec.splitlines() if x.startswith("| 2 インフラ | 1 |"))
        self.assertIn("| 1 | 2m00s |", row2)
        self.assertIn('| 1 ハブ | 00:10:05 | 41s | Bash | task hub:ship PRS="5 4" |', sec)
        self.assertIn("| 2 インフラ | 1 | 20s | 20s |", sec)  # [TASK] → lane 2's next prompt
        self.assertIn("| 1 ハブ | 1,200 | 300 | 0 | 0 | 0.25 | 1 | 4s |", sec)
        self.assertIn("（記録なし", timeline.summarize(EVENTS).split("## 6.")[1])
        self.assertNotIn("load1", sec)  # old logs without load1 / hook_ms: no load table

    def test_bash_load_buckets(self):
        def pair(i, t, d, load, hms):
            return [{"t": t, "event": "PreToolUse", "tool": "Bash", "tuid": f"b{i}", "load1": load, "hook_ms": hms},
                    {"t": t + d, "event": "PostToolUse", "tool": "Bash", "tuid": f"b{i}", "hook_ms": hms}]
        cc = pair(1, 10, 2, 1.5, 30) + pair(2, 20, 4, 2.0, 40) + pair(3, 30, 9, 9.5, 300)
        out = "\n".join(timeline.bash_load_lines(cc, 0, 100))
        self.assertIn("| < 4 | 2 | 3.0s |", out)
        self.assertIn("| ≥ 8 | 1 | 9.0s |", out)
        self.assertNotIn("| 4–8 |", out)
        self.assertIn("中央値 40 ms", out)
        self.assertEqual(timeline.bash_load_lines([{"t": 1, "event": "PreToolUse", "tool": "Bash", "tuid": "x"}], 0, 9), [])


class HookTest(unittest.TestCase):
    HOOK = os.path.join(os.path.dirname(os.path.abspath(__file__)), "cchook.py")

    def run_hook(self, data, d, lane="3"):
        return subprocess.run([sys.executable, self.HOOK], input=data, capture_output=True, text=True,
                              env=dict(os.environ, LANES_DIR=d, LANE=lane), timeout=10)

    def test_hook_appends_and_never_fails(self):
        d = tempfile.mkdtemp()
        samples = [
            {"session_id": "s", "transcript_path": "/x", "hook_event_name": "PreToolUse", "tool_name": "Bash",
             "tool_use_id": "t1", "tool_input": {"command": "task deploy BRANCH=main " + "x" * 200}},
            {"session_id": "s", "hook_event_name": "Notification", "message": "Claude needs your permission to use Bash",
             "notification_type": "permission_prompt"},
            {"session_id": "s", "hook_event_name": "UserPromptSubmit", "prompt": "hello [TASK] 3 fix TS=1\nmore"},
            {"session_id": "s", "hook_event_name": "UserPromptSubmit", "prompt": "secret prompt"},
            {"session_id": "s", "hook_event_name": "SubagentStop", "agent_id": "ag", "agent_type": "general-purpose"},
        ]
        for x in samples:
            r = self.run_hook(json.dumps(x), d)
            self.assertEqual((r.returncode, r.stdout), (0, ""))
        for junk in ("garbage{", "", "[1, 2]", '{"no": "event"}'):
            self.assertEqual(self.run_hook(junk, d).returncode, 0)
        self.assertEqual(subprocess.run([sys.executable, self.HOOK], input="{}", capture_output=True, text=True,
                                        env=dict(os.environ, LANES_DIR="/nonexistent/ro/x")).returncode, 0)
        with open(os.path.join(d, "cc-events.jsonl")) as f:
            got = [json.loads(x) for x in f]
        self.assertEqual(len(got), 5)
        self.assertEqual((got[0]["lane"], got[0]["event"], got[0]["tool"], got[0]["tuid"]), ("3", "PreToolUse", "Bash", "t1"))
        self.assertEqual(len(got[0]["cmd"]), 80)
        self.assertIsInstance(got[0]["hook_ms"], int)
        self.assertIsInstance(got[0]["load1"], float)
        self.assertNotIn("hook_ms", got[1])  # only Pre/PostToolUse carry the timing
        self.assertEqual(got[1]["ntype"], "permission_prompt")
        self.assertEqual(got[2]["head"], "[TASK] 3 fix TS=1")
        self.assertNotIn("head", got[3])  # ordinary prompts are not stored
        self.assertEqual((got[4]["agent_id"], got[4]["agent_type"]), ("ag", "general-purpose"))

    def test_settings_json_has_the_hooks(self):
        root = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
        with open(os.path.join(root, ".claude", "settings.json")) as f:
            st = json.load(f)
        self.assertTrue(st["permissions"]["allow"])
        for ev in ("PreToolUse", "PostToolUse", "UserPromptSubmit", "Notification", "Stop", "SubagentStop",
                   "SessionStart", "SessionEnd"):
            h = st["hooks"][ev][0]["hooks"][0]
            self.assertEqual(h["type"], "command")
            self.assertIn("tools/lanes/cchook.py", h["command"])
            self.assertLessEqual(h["timeout"], 10)
        self.assertEqual(st["hooks"]["PreToolUse"][0]["matcher"], "*")


class IdleBreakdownTest(unittest.TestCase):
    def test_idle_gaps_crossed_with_the_board(self):
        board = [
            {"t": at(0), "kind": "lane", "lane": "1", "frm": "idle", "to": "working", "step": "kickoff"},
            {"t": at(20), "kind": "lane_step", "lane": "1", "state": "working", "step": "hub:ship 5 4"},
            {"t": at(0), "kind": "lane", "lane": "3", "frm": "idle", "to": "working", "step": "app-map"},
            {"t": at(12), "kind": "lane", "lane": "3", "frm": "working", "to": "waiting", "step": "PR #1", "note": "[PR] #1 を送った"},
            {"t": at(16), "kind": "lane", "lane": "3", "frm": "waiting", "to": "idle", "step": "手が空いた"},
            {"t": at(30), "kind": "lane", "lane": "3", "frm": "idle", "to": "working", "step": "fix"},
            {"t": at(50), "kind": "run_scored", "ts": "x", "score": "60000"},
        ]
        ccx = [
            cc(1, 0, "s3", "UserPromptSubmit", lane="3"), cc(10, 0, "s3", "Stop", lane="3"),
            cc(25, 0, "s3", "UserPromptSubmit", lane="3"),  # 10:00 → 25:00 idle: working 2, waiting 4, idle 9 min
            cc(26, 0, "s3", "Stop", lane="3"),
            cc(26, 30, "s3", "Notification", lane="3", ntype="permission_prompt"),
            cc(28, 0, "s3", "UserPromptSubmit", lane="3"),  # permission in the gap: 人
            cc(29, 0, "s3", "Stop", lane="3"),
            cc(29, 10, "s3", "PreToolUse", lane="3", tool="Read", agent_id="sub"),  # subagent events are ignored
            cc(29, 20, "s3", "Stop", lane="3", agent_id="sub"),
            cc(29, 30, "s3", "UserPromptSubmit", lane="3"),  # 30 s
            cc(40, 0, "s3", "Stop", lane="3"), cc(55, 0, "s3", "UserPromptSubmit", lane="3"),  # 5 min before the prize
            cc(0, 0, "x", "Stop", lane="?"), cc(30, 0, "x", "UserPromptSubmit", lane="?"),  # no lane: ignored
        ]
        md = timeline.summarize(sorted(board, key=lambda e: e["t"]), cc=sorted(ccx, key=lambda e: e["t"]), target=50000)
        sec = md.split("## 7.")[1]
        # gaps: 10:00→25:00, 26:00→28:00 (permission → 人), 29:00→29:30 (board idle), 40:00→55:00 (board working)
        self.assertIn("（00:50:00）の idle: 3 アプリ **28 分**", sec)  # 15 + 2 + 0.5 + 10 (to 00:50)
        self.assertIn("| 3 アプリ | 32m30s | 2m00s | - | 4m00s | 9m30s | 17m00s | - |", sec)
        self.assertIn("盤の打ち忘れ（盤は working） 2m00s、他レーンを待っていた 4m00s、やることが無かった 9m00s", sec)
        self.assertIn("| 3 アプリ | 00:10:00 | 15m00s | working | app-map |", sec)
        self.assertIn("その時のハブ", sec)
        self.assertIn("| working: kickoff |", sec)
        self.assertIn("| 3 アプリ | 00:40:00 | 15m00s | working | fix |", sec)
        self.assertIn("| working: hub:ship 5 4 |", sec)

    def test_subagent_wait_is_not_forgot(self):
        board = [
            {"t": at(0), "kind": "lane", "lane": "3", "frm": "idle", "to": "working", "step": "parallel-fix"},
            {"t": at(20), "kind": "lane_step", "lane": "3", "state": "working", "step": "parallel-fix", "agents": 2},
            {"t": at(25), "kind": "lane_step", "lane": "3", "state": "working", "step": "integ", "agents": 0},
        ]
        ccx = [
            cc(1, 0, "s3", "UserPromptSubmit", lane="3"),
            cc(2, 0, "s3", "PreToolUse", lane="3", tool="Agent", tuid="u1"),
            cc(2, 1, "s3", "PostToolUse", lane="3", tool="Agent", tuid="u1"),  # background: returns at once
            cc(3, 0, "s3", "Stop", lane="3"),
            cc(4, 0, "s3", "SubagentStop", lane="3", agent_id="helper"),  # internal helper: no type, no tools
            cc(5, 0, "s3", "PreToolUse", lane="3", tool="Read", agent_id="real"),
            cc(8, 0, "s3", "SubagentStop", lane="3", agent_id="real", agent_type="general-purpose"),
            cc(10, 0, "s3", "UserPromptSubmit", lane="3"),  # 03:00 → 10:00: subagent 5 min, then working 2 min
            cc(11, 0, "s3", "Stop", lane="3"),
            cc(30, 0, "s3", "UserPromptSubmit", lane="3"),  # 11:00 → 30:00: AGENTS=2 from 20:00 to 25:00 on the board
        ]
        md = timeline.summarize(sorted(board, key=lambda e: e["t"]), cc=sorted(ccx, key=lambda e: e["t"]))
        sec = md.split("## 7.")[1]
        self.assertIn("| 3 アプリ | 26m00s | - | 10m00s | - | - | 16m00s | - |", sec)
        self.assertIn("| 3 アプリ | 1 | 2 |", md.split("## 6.")[1])
        self.assertIn("| 1 回", md.split("## 6.")[1])  # the helper's SubagentStop is not a subagent

    def test_band_sent_back_to_ready_is_still_one_bench(self):
        evs = [
            {"t": at(0), "kind": "bench", "frm": "none", "to": "ready", "sha": "3f82b7e", "prs": "#3"},
            {"t": at(1), "kind": "bench", "frm": "ready", "to": "running", "src": "runs"},
            {"t": at(1, 10), "kind": "bench", "frm": "running", "to": "ready", "sha": "3f82b7e", "prs": "#3"},
            {"t": at(2), "kind": "bench", "frm": "ready", "to": "none", "src": "runs"},
            {"t": at(2), "kind": "run_scored", "ts": "x", "score": "1459"},
            {"t": at(5), "kind": "bench", "frm": "none", "to": "ready", "sha": "33dab10", "prs": "#6"},
        ]
        cyc = timeline.bench_cycles(evs, [(at(2), 1459.0)])
        self.assertEqual(len(cyc), 2)
        self.assertEqual((cyc[0]["running"], cyc[0]["ended"], cyc[0]["score"]), (at(1), at(2), 1459.0))


if __name__ == "__main__":
    unittest.main()
