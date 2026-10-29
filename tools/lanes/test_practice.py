# python3 -m unittest tools/lanes/test_practice.py — practice-log records (practice.py) and the HTML report on synthetic data
import json
import os
import subprocess
import sys
import tempfile
import unittest
from datetime import datetime

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import practice  # noqa: E402
import practice_report  # noqa: E402
from test_timeline import EVENTS, at, T  # noqa: E402  (URL 00:16, baseline ready 00:34, 55000 at 00:52, ship 120 s)


def cc(m, s, sid, event, lane="1", **kw):
    return {"ts": int(at(m, s) * 1000), "t": at(m, s), "lane": lane, "sid": sid, "event": event, **kw}


CC = sorted([
    cc(16, 0, "h", "UserPromptSubmit"),
    cc(16, 30, "h", "Stop"),
    cc(20, 0, "h", "UserPromptSubmit"),  # idle 00:16:30 → 00:20 (lane 1 working on the board: unmarked)
    cc(20, 40, "h", "Stop"),
    cc(17, 0, "i", "UserPromptSubmit", lane="2"),
    cc(17, 10, "i", "PreToolUse", lane="2", tool="SendMessage", tuid="s1", head="[PR] #2 ready setup / #1 ready docs (#9 の上)"),
    cc(17, 10, "i", "PostToolUse", lane="2", tool="SendMessage", tuid="s1", head="[PR] #2 ready setup / #1 ready docs (#9 の上)"),
    cc(17, 20, "i", "Notification", lane="2", ntype="permission_prompt"),
    cc(17, 50, "i", "PreToolUse", lane="2", tool="Bash", tuid="b1"),  # 30 s permission wait
    cc(17, 55, "i", "PostToolUse", lane="2", tool="Bash", tuid="b1"),
    cc(18, 0, "i", "Stop", lane="2"),
    cc(30, 0, "i", "UserPromptSubmit", lane="2"),  # idle 00:18 → 00:30: waiting-human 00:20–00:30 on the board
    cc(31, 0, "i", "Stop", lane="2"),
    cc(40, 0, "k", "UserPromptSubmit", lane="c"),
    cc(40, 5, "k", "PreToolUse", lane="c", tool="SendMessage", tuid="s2", head="[DONE] c review #2 ng (1 fix: keep the ORDER BY)"),
    cc(40, 6, "k", "PreToolUse", lane="c", tool="SendMessage", tuid="s3", head="[DONE] c review #5 ok"),
    cc(41, 0, "k", "Stop", lane="c"),
], key=lambda e: e["t"])


def otel(lane, minute, cost):
    ns = str(int(at(minute) * 1e9))
    return {"resourceLogs": [{"resource": {"attributes": [{"key": "lane", "value": {"stringValue": lane}}]},
                              "scopeLogs": [{"logRecords": [{"timeUnixNano": ns, "attributes": [
                                  {"key": "event.name", "value": {"stringValue": "api_request"}},
                                  {"key": "cost_usd", "value": {"doubleValue": cost}},
                                  {"key": "duration_ms", "value": {"intValue": "1000"}}]}]}]}]}


OTEL = [otel("1", 20, 1.5), otel("2", 30, 2.0), otel("1", 55, 4.0), otel("1", 5, 9.0)]  # 00:05 is before the URL

JOURNAL = """# journal

時刻 | branch / PR | 何を | なぜ | 結果
00:34 | main 0bf6235 | baseline | — | run 20261026-153400 score=3238
00:45 | integ/setup / #2 | setup | — | merge ec08737、OK: 盤
00:45 | app-map / #1 | docs | — | merge ec08737
00:50 | early/x / #3 | 先読み（レーン c） | — | merge 1111111
00:51 | tune-dns-ttl / #4 | TTL | — | merge 2222222。7 回目 run 155900 score=1000
00:52 | revert-dns-ttl / #5 | revert | — | merge 3333333、人のベンチは DNS 不通で失敗
00:53 | fix/a / #6 | x | — | revert なし
"""

CLAIMS = """| branch | files | layer | owner (session) | status | updated |
|---|---|---|---|---|---|
| setup/db-split | deploy.sh | config | lane2 / subagent A | merged | 00:45 |
| tune/dns-ttl | zone | config | lane 2 | merged | 00:51 |
| revert/dns-ttl | zone | config | laneC | open | 00:52 |
"""


class PracticeRecordTest(unittest.TestCase):
    def rec(self, **kw):
        return practice.compute(EVENTS, CC, OTEL, [], JOURNAL, CLAIMS, 50000,
                                meta={"slug": "t", "models": {"1": {"model": "Opus", "fast": True}, "c": {"model": "Fable", "fast": False}},
                                      "notes_changed": "x"}, **kw)

    def test_board_record(self):
        r = self.rec()
        self.assertEqual(r["schema_version"], 1)
        self.assertEqual(r["meta"]["prize_target"], 50000)
        self.assertEqual(r["meta"]["model_config"], "1 Opus+fast / c Fable")
        self.assertEqual(r["meta"]["notes"], {"changed": "x", "hypothesis": "", "observations": ""})
        self.assertEqual(r["north_star"], 36.0)  # URL 00:16 → run_scored 55000 at 00:52
        s = r["speed"]
        self.assertEqual(s["url_to_baseline_ready_min"], 18.0)
        self.assertEqual(s["benches_to_prize"], 2)  # 3238, then 55000
        self.assertEqual(s["loop_median_min"], 13.0)  # ready 00:34 → 00:47
        self.assertEqual((s["ship_median_s"], s["ship_max_s"], s["deploy_median_s"]), (120, 120, 50))
        o = r["outcome"]
        self.assertEqual((o["best_score"], o["best_ratio"], o["score_at_60min_ratio"]), (55000, 1.1, 1.1))
        self.assertEqual(o["fails"], 1)  # the journal's 「ベンチは DNS 不通で失敗」 (not 「revert なし」)
        self.assertEqual(o["reverts"], 1)
        self.assertEqual(o["new_5xx_alerts"], 1)  # a4 "smoke | 500"
        self.assertEqual(o["reboot_test"], "not_run")
        self.assertEqual(r["safety"]["crit_alerts"], 1)
        self.assertEqual(r["safety"]["review_catches"], 1)  # #2 ng; #5 ok
        h = r["human"]
        self.assertEqual(h["todos"], 3)
        self.assertEqual(h["response_median_s"]["review"], 180)
        self.assertEqual(h["decisions"], {"choices": 0, "picks": 0, "pr_reviews": 1})
        self.assertEqual(h["wait_before_prize_min"], 4.0)  # t9 00:41–00:44 + t12 00:47–00:48 (t3 closed at the URL)

    def test_agents_and_costs(self):
        r = self.rec()
        a = r["agents"]
        self.assertEqual(a["1"]["cost_usd"], 5.5)  # 1.5 + 4.0, not the 9.0 before the URL
        self.assertEqual(r["agents_total"]["cost_to_prize_usd"], 3.5)  # 1.5 (lane 1, 00:20) + 2.0 (lane 2, 00:30)
        self.assertEqual(a["2"]["permission_wait_s"], 30)
        self.assertEqual(a["2"]["turns"], 2)
        self.assertEqual(a["2"]["idle_min_before_prize"], 12.0)
        self.assertEqual(a["2"]["idle_breakdown"]["human"], 10.0)
        self.assertEqual(a["2"]["idle_breakdown"]["unmarked"], 2.0)  # working on the board 00:18–00:20
        self.assertEqual(a["1"]["alerts_raised"], 1)
        self.assertIsNone(a["3"]["cost_usd"])
        # merged = ship.end #2 #1; [PR] from lane 2 names both (#9 inside the parentheses is not a PR of it)
        self.assertEqual(r["pr_lanes"], {"1": "2", "2": "2"})
        self.assertEqual(a["2"]["merged_prs"], 2)
        self.assertEqual(r["agents_total"]["merged_prs"], 2)
        self.assertEqual(r["agents_total"]["merged_per_usd"], round(2 / 7.5, 2))

    def test_journal_only(self):
        """No board (the first practices): runs and PRs from the journal, URL time given."""
        r = practice.compute([], [], [], [], JOURNAL, CLAIMS, 3000, url_at=datetime(2026, 10, 27, 0, 22).timestamp(),
                             meta={"date": "2026-10-27a"})
        self.assertEqual(r["outcome"]["runs"], 2)  # the full ts and the short "run 155900" on the same day
        self.assertEqual(r["outcome"]["best_score"], 3238)
        self.assertEqual(r["agents_total"]["merged_prs"], 5)  # #2 #1 #3 #4 #5 (not #6: no merge)
        self.assertEqual(r["pr_lanes"]["3"], "c")  # (レーン c) in the line
        self.assertEqual(r["pr_lanes"]["2"], "2")  # integ/setup → every setup/* claims row is lane 2
        self.assertEqual(r["pr_lanes"]["4"], "2")  # tune-dns-ttl = tune/dns-ttl
        self.assertEqual(r["pr_lanes"]["5"], "c")
        self.assertEqual(r["pr_lanes"]["1"], "?")
        self.assertEqual(r["agents_total"]["merged_unattributed"], 1)
        self.assertIsNone(r["agents"]["1"]["turns"])
        self.assertIsNone(r["human"]["todos"])
        self.assertIsNone(r["safety"]["review_catches"])
        self.assertIsNone(r["speed"]["ship_median_s"])

    def test_empty(self):
        r = practice.compute([], [], [], [], "", "", None)
        self.assertIsNone(r["north_star"])
        self.assertIsNone(r["outcome"]["best_score"])
        json.dumps(r)

    def test_parsers(self):
        rows = practice.parse_runs_list("task: [runs] …\n20261027-114454  score=790  req=14681 err=0  report  git=84033e7  verdict: x\n"
                                        "20261027-115000  score=-  req=10 err=0  no report\n")
        self.assertEqual(rows, [{"ts": "20261027-114454", "score": "790", "req": 14681, "err": 0, "git": {"commit": "84033e7"}}])
        self.assertEqual(practice.parse_models("1=Opus 5.5+fast,c=Fable 5.1"),
                         {"1": {"model": "Opus 5.5", "fast": True}, "c": {"model": "Fable 5.1", "fast": False}})
        self.assertEqual(practice.union_seconds([(0, 10), (5, 20), (30, 40)], 0, 35), 25)

    def test_cli_record_writes_the_file(self):
        d = tempfile.mkdtemp()
        board = os.path.join(d, "board")
        os.makedirs(board)
        with open(os.path.join(board, "events.jsonl"), "w") as f:
            for e in EVENTS:
                f.write(json.dumps(e, ensure_ascii=False) + "\n")
        kit = os.path.join(d, "kit")
        os.makedirs(kit)
        env = {k: v for k, v in os.environ.items() if k not in ("OPS_HOST", "SSH_KEY", "RUNS_JSON", "KIT_DIR")}
        env.update(LANES_DIR=board, PRIZE_TARGET="50000")
        here = os.path.dirname(os.path.abspath(__file__))
        r = subprocess.run([sys.executable, os.path.join(here, "practice.py"), "record", "slug=t", "out=" + kit,
                            "notes_changed=a b", "reboot=pass"], capture_output=True, text=True, env=env, cwd=d)
        self.assertEqual(r.returncode, 0, r.stderr)
        files = os.listdir(os.path.join(kit, "practice-log"))
        self.assertEqual(len(files), 1, files)
        self.assertTrue(files[0].endswith("-t.json"))
        with open(os.path.join(kit, "practice-log", files[0])) as f:
            rec = json.load(f)
        self.assertEqual(rec["outcome"]["reboot_test"], "pass")
        self.assertEqual(rec["meta"]["notes"]["changed"], "a b")
        self.assertEqual(rec["north_star"], 36.0)
        # without OUT / KIT_DIR it prints the JSON and where to put it
        r = subprocess.run([sys.executable, os.path.join(here, "practice.py"), "record", "slug=t"], capture_output=True,
                           text=True, env=env, cwd=d)
        self.assertIn('"schema_version": 1', r.stdout)
        self.assertIn("practice-log/", r.stdout.splitlines()[-1])


class PracticeReportTest(unittest.TestCase):
    def test_render_0_1_n(self):
        a = PracticeRecordTest().rec()
        a["meta"]["date"] = "2026-10-27a"
        b = json.loads(json.dumps(a))
        b["meta"]["date"], b["north_star"], b["agents_total"]["cost_to_prize_usd"] = "2026-10-28", None, None
        b["meta"]["model_config"] = "all Sonnet"
        empty = practice.compute([], [], [], [], "", "", None, meta={"date": "2026-10-29", "slug": "z"})
        for recs in ([], [a], [b, a], [a, b, empty]):
            out = practice_report.render(recs)
            self.assertTrue(out.startswith("<!doctype html>"))
            self.assertNotIn("http://", out.replace('http://www.w3.org', ''))  # nothing external
            self.assertNotIn("https://", out)
            self.assertEqual(out.count("<svg"), out.count("</svg>"))
        out = practice_report.render([b, a])
        self.assertLess(out.index("2026-10-27a"), out.index("2026-10-28"))  # sorted by date
        self.assertIn("未達", out)  # b never crossed
        self.assertIn("点が無い練習", out)
        self.assertIn("all Sonnet", out)


if __name__ == "__main__":
    unittest.main()
