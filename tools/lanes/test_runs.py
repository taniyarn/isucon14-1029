import io, json, os, sys, tempfile, unittest
from contextlib import redirect_stdout
sys.path.insert(0, os.path.dirname(__file__))
import runs

ROWS = [{"ts": "20261020-160320", "req": 3416, "err": 2, "git": {"commit": "18b8cc2"}, "verdict": "DB CPU-bound", "next": "parallel-fix", "score": 3113, "note": "baseline"},
        {"ts": "20261020-161752", "req": 36433, "err": 501, "git": {"commit": "4edcc0c"}, "verdict": "errors first", "next": "deep-dive", "score": ""}]


class RunsTest(unittest.TestCase):
    def setUp(self):
        f = tempfile.NamedTemporaryFile("w", suffix=".json", delete=False)
        json.dump(ROWS, f); f.close()
        os.environ["RUNS_JSON"] = f.name
        os.environ["LANES_DIR"] = tempfile.mkdtemp()  # wait moves the bench band: never the real board
        os.environ.pop("RUNS_LIVE_JSON", None)
        os.environ.pop("LANES_PLAN", None)

    def run_main(self, *argv):
        buf = io.StringIO()
        with redirect_stdout(buf):
            code = runs.main(list(argv))
        return code, buf.getvalue()

    def test_list(self):
        code, out = self.run_main("list")
        self.assertEqual(code, 0)
        self.assertIn("score=3113", out)
        self.assertIn("score=-", out)  # empty score is not a score
        self.assertIn("git=4edcc0c", out)

    def test_wait_returns_scored_run(self):
        code, out = self.run_main("wait", "after=20261020-150000", "timeout=0")
        self.assertEqual(code, 0)
        self.assertIn("NEW RUN  20261020-160320", out)
        self.assertIn("next (hub): task app:refresh TS=20261020-160320", out)
        import lanes  # the scored run ticks the roadmap's B1 (shipped plan.json)
        self.assertEqual(lanes.read_board()["steps"]["B1"]["note"], "20261020-160320 score 3113")

    def test_wait_ignores_unscored_and_times_out(self):
        code, out = self.run_main("wait", "after=20261020-160320", "timeout=0")
        self.assertEqual(code, 2)
        self.assertIn("waiting: 20261020-161752", out)

    def test_wait_default_after_is_the_newest_scored_run(self):
        # the unscored 161752 (a bench already running when the wait starts) is waited for, not skipped
        code, out = self.run_main("wait", "timeout=0")
        self.assertEqual(code, 2)
        self.assertIn("after=20261020-160320 (default: the newest scored run)", out)
        self.assertIn("waiting: 20261020-161752", out)

    def test_wait_moves_the_bench_band(self):
        import lanes
        lanes.mutate("bench", {"state": "ready", "sha": "abc1234", "prs": "7 8"})
        code, _ = self.run_main("wait", "after=20261020-150000", "timeout=0")
        self.assertEqual((code, lanes.read_board()["bench"]["state"]), (0, "none"))
        self.assertEqual(lanes.read_board()["bench"]["prs"], "#7 #8")  # kept for the record
        code, _ = self.run_main("wait", "after=20261020-160320", "timeout=0")  # unscored new run, no live data
        self.assertEqual((code, lanes.read_board()["bench"]["state"]), (2, "running"))
        live = [{"ts": "20261020-170000", "hosts": {"svc1": {"end_reason": "", "req_total": 10}}}]
        f = tempfile.NamedTemporaryFile("w", suffix=".json", delete=False)
        json.dump(live, f); f.close()
        os.environ["RUNS_LIVE_JSON"] = f.name
        lanes.mutate("bench", {"state": "none"})
        self.run_main("wait", "after=20261020-161752", "timeout=0")
        self.assertEqual(lanes.read_board()["bench"]["state"], "running")
        self.assertEqual(runs.live_state([{"ts": "20261020-170000", "hosts": {"svc1": {"end_reason": "ended", "req_total": 30000}}}],
                                         "20261020-161752"), "collect")
        self.assertEqual(runs.live_state([{"ts": "20261020-170000", "hosts": {"svc1": {"end_reason": "ended", "req_total": 40}}}],
                                         "20261020-161752"), "")  # a smoke is not a bench

    def test_band_never_regresses(self):
        import lanes
        lanes.mutate("bench", {"state": "ready", "sha": "3f82b7e", "prs": "3 2"})
        lanes.mutate("bench", {"state": "running", "src": "runs"})
        # 2026-10-27 20:54:42: the hub re-sent STATE=ready with NEXT after the bench had started
        out = lanes.mutate("bench", {"state": "ready", "sha": "3f82b7e", "prs": "#3 #2", "next": "6", "src": "lane:bench"})
        b = lanes.read_board()["bench"]
        self.assertEqual((b["state"], b["next"]), ("running", "#6"))
        self.assertTrue(out.startswith("kept running"))
        lanes.mutate("bench", {"state": "ready", "next": "7"})  # no sha: still running
        self.assertEqual(lanes.read_board()["bench"]["state"], "running")
        lanes.mutate("bench", {"next": "8"})  # fields only
        self.assertEqual((lanes.read_board()["bench"]["state"], lanes.read_board()["bench"]["next"]), ("running", "#8"))
        lanes.mutate("bench", {"state": "ready", "sha": "1111111"})  # a new ship (FORCE=1): a new bench
        self.assertEqual(lanes.read_board()["bench"]["state"], "ready")
        # runs:wait: collect never goes back to running (a host flickering without end_reason)
        lanes.mutate("bench", {"state": "collect"})
        live = [{"ts": "20261020-170000", "hosts": {"svc1": {"end_reason": "", "req_total": 10}}}]
        f = tempfile.NamedTemporaryFile("w", suffix=".json", delete=False)
        json.dump(live, f); f.close()
        os.environ["RUNS_LIVE_JSON"] = f.name
        self.run_main("wait", "after=20261020-161752", "timeout=0")
        self.assertEqual(lanes.read_board()["bench"]["state"], "collect")


if __name__ == "__main__":
    unittest.main()
