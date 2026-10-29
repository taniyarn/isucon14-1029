# python3 -m unittest tools/lanes/test_lanes.py   (or: cd tools/lanes && python3 -m unittest)
import json
import os
import subprocess
import sys
import tempfile
import unittest
from concurrent.futures import ThreadPoolExecutor

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
os.environ["LANES_NO_GH"] = "1"  # review todos made through the CLI never call the real gh
os.environ["LANES_ENV_FILE"] = os.path.join(tempfile.mkdtemp(), ".env")  # never the repo's .env (tests that need one set it)
for _k in ("PRACTICE", "HOST_LOGIN", "LANES_OTEL"):
    os.environ.pop(_k, None)
import lanes  # noqa: E402

SCRIPT = os.path.join(os.path.dirname(os.path.abspath(__file__)), "lanes.py")


class LanesTest(unittest.TestCase):
    def setUp(self):
        self.dir = tempfile.mkdtemp()
        os.environ["LANES_DIR"] = self.dir

    def run_cli(self, *args):
        return subprocess.run([sys.executable, SCRIPT, *args], capture_output=True, text=True, env=dict(os.environ))

    def test_baseline_once(self):
        b, t = lanes.empty_board(), 1000
        b["todos"].append({"id": "t3", "lane": "1", "msg": "url", "kind": "url", "done": True, "values": ["a", "b", "svc2 から bench"]})
        self.assertEqual(lanes.apply(b, "baseline", {"sha": "abc1234"}, t), "t1")
        td = b["todos"][-1]
        self.assertEqual((td["kind"], td["lane"]), ("bench", "1"))
        self.assertIn("main abc1234", td["msg"])
        self.assertTrue(td["msg"].endswith("svc2 から bench"))
        self.assertIn("この todo の「🏁 終わった」", td["after"])  # the button is on the bench todo (no band on lane:web)
        self.assertEqual((b["bench"]["state"], b["bench"]["sha"], b["bench"]["prs"]), ("ready", "abc1234", "baseline"))
        self.assertEqual(b["msgs"][-1]["msg"], "[DONE] 2 baseline-ready t1 main abc1234")
        self.assertEqual(lanes.apply(b, "baseline", {"sha": "abc1234"}, t), "skip (already offered)")
        self.assertEqual(len([x for x in b["todos"] if x.get("kind") == "bench"]), 1)
        b2 = lanes.empty_board()
        lanes.apply(b2, "bench", {"state": "ready", "sha": "x"}, t)
        lanes.apply(b2, "bench", {"state": "none"}, t)
        self.assertEqual(lanes.apply(b2, "baseline", {"sha": "y"}, t), "skip (the band has moved before)")
        b3 = lanes.empty_board()
        lanes.apply(b3, "phase", {"phase": "loop"}, t)
        self.assertEqual(lanes.apply(b3, "baseline", {"sha": "y"}, t), "skip (phase is loop)")
        self.assertEqual(lanes.apply(lanes.empty_board(), "baseline", {}, t), "skip (sha= required)")

    def test_ops(self):
        b, t = lanes.empty_board(), 1000
        lanes.apply(b, "status", {"lane": "1", "state": "working", "step": " bench \n"}, t)
        self.assertEqual(b["lanes"]["1"]["step"], "bench")
        self.assertEqual(lanes.apply(b, "alert", {"lane": "1", "level": "crit", "msg": "down", "impact": "0 pt"}, t), "a1")
        self.assertEqual(b["lanes"]["1"]["state"], "alert")
        self.assertEqual(lanes.apply(b, "alert", {"lane": "1", "msg": "again"}, t), "a2")
        self.assertEqual(lanes.apply(b, "todo", {"lane": "c", "msg": "bench", "why": "PR 3", "after": "tell 1"}, t), "t3")
        lanes.apply(b, "resolve", {"id": "a1"}, t)
        self.assertEqual(b["lanes"]["1"]["state"], "alert")  # a2 still open
        lanes.apply(b, "resolve", {"id": "a2"}, t)
        self.assertEqual(b["lanes"]["1"]["state"], "working")
        lanes.apply(b, "done", {"id": "t3"}, t)
        self.assertTrue(b["todos"][0]["done"])
        lanes.apply(b, "phase", {"phase": "loop"}, t)
        self.assertEqual(b["phase"], "loop")
        for op, a in [("status", {"lane": "1", "state": "sleep"}), ("status", {"lane": "BAD LANE", "state": "idle"}),
                      ("alert", {"lane": "1", "level": "info", "msg": "x"}), ("alert", {"lane": "1", "msg": " "}),
                      ("resolve", {"id": "a9"}), ("done", {"id": "t9"}), ("phase", {"phase": "lunch"}), ("nope", {})]:
            with self.assertRaises(lanes.LaneError, msg=(op, a)):
                lanes.apply(b, op, a, t)

    def test_limits(self):
        b, t = lanes.empty_board(), 1000
        lanes.apply(b, "status", {"lane": "1", "state": "working", "step": "あ" * 2000, "note": "a\x00b\tc"}, t)
        self.assertEqual(len(b["lanes"]["1"]["step"]), lanes.MAX_SHORT)
        self.assertEqual(b["lanes"]["1"]["note"], "ab c")
        for i in range(lanes.MAX_ITEMS + 20):
            aid = lanes.apply(b, "alert", {"lane": "1", "msg": "m"}, t)
            if i < 10:
                lanes.apply(b, "resolve", {"id": aid}, t)
        self.assertEqual(len(b["alerts"]), lanes.MAX_ITEMS)
        self.assertFalse(any(x["resolved"] for x in b["alerts"][:5]))
        for i in range(lanes.MAX_LANES + 2):
            try:
                lanes.apply(b, "status", {"lane": "l" + chr(97 + i), "state": "idle"}, t)
            except lanes.LaneError:
                pass
        self.assertLessEqual(len(b["lanes"]), lanes.MAX_LANES)
        for i in range(50):
            lanes.apply(b, "todo", {"lane": "1", "msg": "x"}, t)
        self.assertLessEqual(len(lanes.render(b, t, color=True).split("\n")), lanes.BOARD_ROWS)

    def test_cli_and_quoting(self):
        tricky = "it's \"q\" $(echo pwned) 日本語"
        r = self.run_cli("alert", "lane=2", "level=warn", "msg=" + tricky)
        self.assertEqual((r.returncode, r.stdout), (0, "a1\n"))
        r = self.run_cli("phase", "phase=x")
        self.assertEqual(r.returncode, 1)
        self.assertIn("!! lane: phase must be", r.stderr)
        out = self.run_cli("board").stdout
        self.assertIn(tricky, out)
        self.assertIn("lane 2 インフラ", out)

    def test_concurrent_writes(self):
        with ThreadPoolExecutor(12) as ex:
            ids = list(ex.map(lambda i: self.run_cli("todo", "lane=c", f"msg=m{i}").stdout.strip(), range(30)))
        self.assertEqual(len(set(ids)), 30)
        with open(os.path.join(self.dir, "board.json")) as f:
            self.assertEqual(len(json.load(f)["todos"]), 30)

    def test_names_cmd_copy(self):
        b, t = lanes.empty_board(), 1000
        for k in ("c", "3", "1", "2", "zz"):
            lanes.apply(b, "status", {"lane": k, "state": "idle", "step": "s"}, t)
        self.assertEqual(lanes.lane_order(b["lanes"]), ["1", "2", "3", "c", "zz"])
        self.assertEqual([lanes.lane_label(k) for k in ("1", "2", "3", "c", "zz")],
                         ["1 ハブ", "2 インフラ", "3 アプリ", "c スカウト", "zz"])
        tid = lanes.apply(b, "todo", {"lane": "1", "msg": "bench", "cmd": "cd ~/bench && ./bench run"}, t)
        t2 = lanes.apply(b, "todo", {"lane": "2", "msg": "edit .env"}, t)
        td = {x["id"]: x for x in b["todos"]}
        self.assertEqual(lanes.copy_text(td[tid]), "cd ~/bench && ./bench run")
        self.assertEqual(lanes.copy_text(td[t2]), "edit .env")
        out = lanes.render(b, t)
        self.assertIn("cmd: cd ~/bench && ./bench run", out)
        self.assertIn("lane 3 アプリ", out)

    def test_board_json_and_watch_frame(self):
        b, t = lanes.empty_board(), 10_000
        lanes.apply(b, "status", {"lane": "2", "state": "waiting-human", "step": "許可待ち: push"}, t - 400)
        aid = lanes.apply(b, "alert", {"lane": "3", "level": "crit", "msg": "smoke 500"}, t - 60)
        lanes.apply(b, "todo", {"lane": "1", "msg": "ベンチ", "cmd": "./bench run"}, t)
        d = lanes.board_json(b, t)
        self.assertTrue(next(x for x in d["lanes"] if x["id"] == "2")["stuck"])
        self.assertEqual(d["todos"][0]["copy"], "./bench run")
        self.assertIn(f"task lane:resolve ID={aid}", d["alerts"][0]["hint"])
        self.assertIn("3 アプリ", d["alerts"][0]["hint"])
        for width in (40, 120):
            lines = lanes.paint(lanes.watch_frame(b, t, width), width, color=False)
            self.assertTrue(all(sum(lanes.cell_width(c) for c in x) <= width for x in lines), width)
        text = "\n".join(lanes.paint(lanes.watch_frame(b, t, 200), 200, color=False))
        for want in ("🙋 2 インフラ", "[1]", "$ ./bench run", "直ったら: task lane:resolve", "⚠ 5 分超"):
            self.assertIn(want, text)

    def test_web(self):
        import threading
        import urllib.error
        import urllib.request
        import lanes_web
        tid = self.run_cli("todo", "lane=1", "msg=bench", "cmd=./bench run").stdout.strip()
        aid = self.run_cli("alert", "lane=c", "msg=x").stdout.strip()
        srv = lanes_web.make_server(0)
        threading.Thread(target=srv.serve_forever, daemon=True).start()
        base = f"http://127.0.0.1:{srv.server_address[1]}"
        try:
            self.assertIn("ISUCON lanes", urllib.request.urlopen(base + "/").read().decode())
            d = json.loads(urllib.request.urlopen(base + "/board.json").read())
            self.assertEqual(d["todos"][0]["copy"], "./bench run")

            def post(path, hdr=True):
                req = urllib.request.Request(base + path, method="POST", headers={"X-Lanes": "1"} if hdr else {})
                return urllib.request.urlopen(req).status
            with self.assertRaises(urllib.error.HTTPError):
                post(f"/done?id={tid}", hdr=False)
            self.assertEqual(post(f"/done?id={tid}"), 200)
            self.assertEqual(post(f"/resolve?id={aid}"), 200)
            with self.assertRaises(urllib.error.HTTPError):
                post("/done?id=t99")
            d = json.loads(urllib.request.urlopen(base + "/board.json").read())
            self.assertEqual((d["todos"], d["alerts"]), ([], []))
        finally:
            srv.shutdown()
            srv.server_close()

    def test_web_long_poll_and_sound_toggle(self):
        import threading
        import time as _t
        import urllib.request
        import lanes_web
        self.run_cli("todo", "lane=1", "msg=first")
        srv = lanes_web.make_server(0)
        threading.Thread(target=srv.serve_forever, daemon=True).start()
        base = f"http://127.0.0.1:{srv.server_address[1]}"
        old = lanes_web.LONGPOLL_S
        try:
            page = urllib.request.urlopen(base + "/").read().decode()
            self.assertIn('id="sound"', page)  # 🔊 toggle next to 🔔, remembered in localStorage, WebAudio beep
            self.assertIn('localStorage.getItem("lanes.sound")', page)
            self.assertIn("createOscillator", page)
            self.assertIn("?wait=", page)
            r = urllib.request.urlopen(base + "/board.json")
            ver = r.headers["X-Board-Version"]
            self.assertTrue(ver)
            got = {}

            def wait():
                t0 = _t.time()
                d = json.loads(urllib.request.urlopen(base + "/board.json?wait=" + ver).read())
                got.update(sec=_t.time() - t0, msgs=[x["msg"] for x in d["todos"]])
            th = threading.Thread(target=wait)
            th.start()
            _t.sleep(0.6)
            self.assertNotIn("sec", got)  # held: nothing changed yet
            t1 = _t.time()
            lanes.mutate("todo", {"lane": "1", "msg": "second"})
            th.join(5)
            self.assertIn("second", got["msgs"])
            self.assertLess(_t.time() - t1, 0.5)  # answered right after the change, not at the next poll
            lanes_web.LONGPOLL_S = 0.3  # nothing changes: answers anyway after the limit
            ver = urllib.request.urlopen(base + "/board.json").headers["X-Board-Version"]
            t0 = _t.time()
            urllib.request.urlopen(base + "/board.json?wait=" + ver).read()
            self.assertGreaterEqual(_t.time() - t0, 0.25)
        finally:
            lanes_web.LONGPOLL_S = old
            srv.shutdown()
            srv.server_close()

    def test_pr_todo_decision_bench_and_token(self):
        import threading
        import urllib.error
        import urllib.request
        import lanes_web
        tid = lanes.mutate("todo", {"lane": "1", "msg": "review", "pr": "#5"})
        plain = lanes.mutate("todo", {"lane": "1", "msg": "hosts"})
        with self.assertRaises(lanes.LaneError):
            lanes.mutate("decide", {"id": tid, "decision": "ok"})  # not a CLI op: only lane:web decides
        with self.assertRaises(lanes.LaneError):
            lanes.mutate("todo", {"lane": "1", "msg": "x", "pr": "abc"})
        lanes.mutate("bench", {"state": "ready", "sha": "abc1234", "prs": "7,8", "next": "9"})
        out = lanes.render(lanes.read_board(), lanes.now())
        self.assertTrue(out.splitlines()[0].startswith("bench: ready"), out)
        self.assertIn("main abc1234 (#7 #8)", out)
        lanes.mutate("bench", {"state": "running", "next": "-"})
        bn = lanes.read_board()["bench"]
        self.assertEqual((bn["state"], bn["sha"], bn.get("next")), ("running", "abc1234", None))
        srv = lanes_web.make_server(0, token="tok")
        threading.Thread(target=srv.serve_forever, daemon=True).start()
        base = f"http://127.0.0.1:{srv.server_address[1]}"

        def post(path, token=None):
            h = {"X-Lanes": "1", **({"X-Lanes-Token": token} if token else {})}
            return urllib.request.urlopen(urllib.request.Request(base + path, method="POST", headers=h)).status
        try:
            self.assertIn('"token": "tok"', urllib.request.urlopen(base + "/").read().decode())
            for path, tok in ((f"/decide?id={tid}&decision=ok", None), (f"/decide?id={tid}&decision=ok", "bad"),
                              (f"/done?id={tid}", None), (f"/decide?id={plain}&decision=ok", "tok")):
                with self.assertRaises(urllib.error.HTTPError, msg=path):
                    post(path, tok)
            self.assertEqual(post(f"/decide?id={tid}&decision=changes", "tok"), 200)
            self.assertEqual(post(f"/done?id={plain}"), 200)
        finally:
            srv.shutdown()
            srv.server_close()
        td = next(x for x in lanes.read_board()["todos"] if x["id"] == tid)
        self.assertEqual((td["done"], td["decision"], td["decided_by"], td["pr"]), (True, "changes", "web", 5))
        self.assertEqual(lanes.wait_todo({"id": tid, "timeout": "0"}, poll=0), 0)

    def test_choices_todo_decided_only_on_web(self):
        import threading
        import urllib.error
        import urllib.parse
        import urllib.request
        import lanes_web
        tid = self.run_cli("todo", "lane=1", "msg=Q1: 早期の手を入れる?", "choices=OK| やめる |0 のまま|OK").stdout.strip()
        td = next(x for x in lanes.read_board()["todos"] if x["id"] == tid)
        self.assertEqual(td["choices"], ["OK", "やめる", "0 のまま"])  # trimmed, duplicates dropped
        for a in ({"pr": "5", "choices": "a|b"}, {"choices": "a|done"}, {"choices": "|".join("abcdefg")}):
            with self.assertRaises(lanes.LaneError, msg=a):
                lanes.mutate("todo", {"lane": "1", "msg": "x", **a})
        self.assertEqual(self.run_cli("decide", f"id={tid}", "decision=OK").returncode, 1)  # no CLI path to a decision
        self.assertIn("選択: OK / やめる / 0 のまま", self.run_cli("board").stdout)
        frame = "\n".join("".join(t for _, t in segs) for segs in lanes.watch_frame(lanes.read_board(), lanes.now()))
        self.assertIn("選択: OK / やめる / 0 のまま", frame)
        srv = lanes_web.make_server(0, token="tok")
        threading.Thread(target=srv.serve_forever, daemon=True).start()
        base = f"http://127.0.0.1:{srv.server_address[1]}"

        def post(path, token=None):
            h = {"X-Lanes": "1", **({"X-Lanes-Token": token} if token else {})}
            return urllib.request.urlopen(urllib.request.Request(base + path, method="POST", headers=h)).status
        q = lambda c: f"/decide?id={tid}&decision=" + urllib.parse.quote(c)  # noqa: E731
        try:
            page = urllib.request.urlopen(base + "/").read().decode()
            self.assertIn('class="tid"', page)
            self.assertIn("x.choices.map", page)
            self.assertIn("Notification.requestPermission", page)  # W1
            self.assertIn("🔔 通知を有効にする", page)
            d = json.loads(urllib.request.urlopen(base + "/board.json").read())
            self.assertEqual(d["todos"][0]["choices"], ["OK", "やめる", "0 のまま"])
            for path, tok in ((q("0 のまま"), None), (q("0 のまま"), "bad"), (q("ok"), "tok"), (q("changes"), "tok"),
                              (f"/done?id={tid}", None)):
                with self.assertRaises(urllib.error.HTTPError, msg=path):
                    post(path, tok)
            self.assertEqual(post(q("0 のまま"), "tok"), 200)
            with self.assertRaises(urllib.error.HTTPError):
                post(q("OK"), "tok")  # already closed
            d = json.loads(urllib.request.urlopen(base + "/board.json").read())
            self.assertEqual((d["todos"], d["decisions"][0]["id"], d["decisions"][0]["decision"], d["decisions"][0]["pr"]),
                             ([], tid, "0 のまま", None))
        finally:
            srv.shutdown()
            srv.server_close()
        r = self.run_cli("wait", f"id={tid}", "timeout=0")
        self.assertEqual(r.returncode, 0)
        self.assertIn("decision=0 のまま\n", r.stdout)
        self.assertIn(f"{tid} → 0 のまま", self.run_cli("board").stdout)

    def test_advance_and_phase_move_hub_steps(self):
        plan = tempfile.NamedTemporaryFile("w", suffix=".json", delete=False)
        json.dump({"steps": [{"id": i, "lane": "1"} for i in ("H3", "H4", "H5")]}, plan)
        plan.close()
        os.environ["LANES_PLAN"] = plan.name
        try:
            lanes.mutate("phase", {"phase": "baseline"})
            self.assertEqual(lanes.read_board()["steps"]["H3"]["state"], "doing")
            self.assertEqual(lanes.mutate("advance", {"ids": "H4 H5", "note": "ship #7"}), "H4")
            self.assertEqual(lanes.mutate("advance", {"ids": "H4 H5"}), "H5")
            self.assertEqual(lanes.mutate("advance", {"ids": "H4 H5"}), "")
            self.assertEqual(lanes.read_board()["steps"]["H4"]["note"], "ship #7")
        finally:
            del os.environ["LANES_PLAN"]


PLAN = [
    {"id": "I1", "lane": "2", "title": "a", "deps": [], "stage": 0, "how": ""},
    {"id": "I3", "lane": "2", "title": "b", "deps": ["I1"], "stage": 1, "how": ""},
    {"id": "B1", "lane": "bench", "title": "c", "deps": ["I3"], "stage": 2, "how": ""},
    {"id": "A2", "lane": "3", "title": "d", "deps": ["I3"], "stage": 2, "how": ""},
    {"id": "B2", "lane": "bench", "title": "e", "deps": ["B1"], "stage": 3, "how": ""},
    {"id": "P1", "lane": "bench", "title": "f", "deps": ["B2"], "stage": 4, "how": ""},
]


class RoadmapTest(unittest.TestCase):
    def setUp(self):
        self.dir = tempfile.mkdtemp()
        os.environ["LANES_DIR"] = self.dir
        self.plan = os.path.join(self.dir, "plan.json")
        with open(self.plan, "w") as f:
            json.dump({"steps": PLAN}, f)
        os.environ["LANES_PLAN"] = self.plan

    def tearDown(self):
        os.environ.pop("LANES_PLAN", None)

    def states(self, rm):
        return {x["id"]: x["state"] for x in rm["steps"]}

    def test_step_op_and_ready_blocked(self):
        b, t = lanes.empty_board(), 1000
        st = self.states(lanes.roadmap(b))
        self.assertEqual((st["I1"], st["I3"], st["A2"]), ("ready", "blocked", "blocked"))
        lanes.apply(b, "step", {"id": "i1", "state": "done", "note": "ok"}, t)
        lanes.apply(b, "step", {"id": "I3", "state": "doing"}, t)
        st = self.states(lanes.roadmap(b))
        self.assertEqual((st["I1"], st["I3"], st["A2"]), ("done", "doing", "blocked"))
        lanes.apply(b, "step", {"id": "I3", "state": "skip", "note": ""}, t)
        self.assertEqual(self.states(lanes.roadmap(b))["A2"], "ready")
        self.assertEqual(b["steps"]["I3"].get("note"), None)
        lanes.apply(b, "step", {"id": "I1", "state": "done", "note": ""}, t)
        self.assertEqual(b["steps"]["I1"]["note"], "ok")  # empty NOTE keeps the old one
        with self.assertRaises(lanes.LaneError):
            lanes.apply(b, "step", {"id": "ZZ", "state": "done"}, t)
        with self.assertRaises(lanes.LaneError):
            lanes.apply(b, "step", {"id": "I1", "state": "finished"}, t)
        lanes.apply(b, "step", {"id": "I1", "state": "todo"}, t)
        self.assertNotIn("I1", b["steps"])
        self.assertIn("roadmap: done", lanes.render(b, t))

    def test_bench_auto_and_prize(self):
        b = lanes.empty_board()
        runs = [{"ts": "20261031-101500", "score": 3113, "req": 3400}, {"ts": "20261031-100000", "score": "", "req": 2},
                {"ts": "20261031-103000", "score": 52000, "req": 9000}]
        rm = lanes.roadmap(b, runs=runs, target=50000)
        st = self.states(rm)
        self.assertEqual((st["B1"], st["B2"], st["P1"]), ("done", "done", "done"))
        self.assertIn("20261031-101500", next(x for x in rm["steps"] if x["id"] == "B1")["note"])
        self.assertEqual((rm["score"]["latest"], rm["score"]["best"], rm["score"]["pct"]), (52000, 52000, 104))
        rm = lanes.roadmap(b, runs=runs[:2], target=50000)
        st = self.states(rm)
        self.assertEqual((st["B1"], st["B2"], st["P1"]), ("done", "ready", "blocked"))
        lanes.apply(b, "step", {"id": "B2", "state": "skip"}, 1)  # manual state wins over automatic
        self.assertEqual(self.states(lanes.roadmap(b, runs=runs))["B2"], "skip")

    def test_critical_path_and_layout(self):
        import lanes_web
        rm = lanes.roadmap(lanes.empty_board())
        self.assertEqual(set(rm["critical"]), {"P1", "B2", "B1", "I3", "I1"})
        self.assertFalse(next(x for x in rm["steps"] if x["id"] == "A2")["critical"])
        lay = lanes_web.layout(rm)
        self.assertEqual([r["lane"] for r in lay["rows"]], ["2", "3", "bench"])
        self.assertTrue(all(sid in lay["pos"] for sid in ("I1", "A2", "P1")))
        crit = [e for e in lay["edges"] if e["critical"]]
        self.assertIn({"from": "B2", "to": "P1"}, [{"from": e["from"], "to": e["to"]} for e in crit])
        self.assertNotIn("A2", [e["to"] for e in crit])

    def test_web_payload_with_runs_json(self):
        import lanes_web
        rj = os.path.join(self.dir, "runs.json")
        with open(rj, "w") as f:
            json.dump([{"ts": "20261031-101500", "score": 3113, "req": 3400}], f)
        os.environ["RUNS_JSON"] = rj
        try:
            lanes_web.RUNS.refresh()
            d = lanes_web.payload()
        finally:
            os.environ.pop("RUNS_JSON", None)
        st = {x["id"]: x["state"] for x in d["roadmap"]["steps"]}
        self.assertEqual(st["B1"], "done")
        self.assertIn("layout", d["roadmap"])



# Merged PR heads / files of the 2026-10-28 practice (gh pr list -R ytakaya/isucon14-1028 --state merged) → the step
# the shipped plan.json marks done on merge (None = loop work, no roadmap step).
PRACTICE_1028 = [
    (1, "scan/early", ["docs/technique-scan-early.md"], None),
    (2, "docs/config-options", ["docs/plans/config-options.md"], None),
    (3, "fix/20261027-155039/ops", ["etc/mysql/mysql.conf.d/zz-perf.cnf"], "I5"),
    (4, "early/payment-client", ["webapp/go/payment_gateway.go"], "C2"),
    (5, "integ/setup", ["deploy.sh", "webapp/go/main.go", "etc/nginx/nginx.conf"], "I4"),
    (6, "integ/early", ["webapp/go/app_handlers.go", "webapp/sql/indexes/early-indexes.sql"], None),
    (7, "docs/infra-plans", ["docs/plans/infra-tuning.md"], None),
    (8, "setup/app-map", ["docs/app-map.md"], "A2"),
    (9, "design/early-sse", ["docs/plans/early-sse.md"], None),  # C2 already done by #4
    (10, "infra/matcher-reset-failed", ["deploy.sh"], None),
    (11, "docs/app-next", ["docs/plans/app-next.md"], None),
    (13, "scan/20261027-160106", ["docs/plans/20261027-160106-techniques.md",
                                  "docs/technique-scan-20261027-160106.md"], "C3"),
    (14, "integ/160106", ["webapp/go/app_handlers.go", "webapp/sql/indexes/20261027-160106-indexes.sql"], "A3"),
    (16, "docs/app-report", ["docs/app-annotations.json", "docs/reports/latest.md"], None),
    (17, "design/w1-ride-status", ["docs/plans/w1-ride-status.md"], None),
    (18, "fix/drop-coupons-used-by", ["webapp/sql/indexes/20261027-160106-indexes.sql"], None),
    (19, "integ/w12", ["webapp/go/app_handlers.go", "webapp/sql/4-ride-status.sql"], "A4"),
    (20, "deepdive/20261027-161011", ["docs/deepdive/20261027-161011.md"], None),
    (22, "fix/coupons-deadlock", ["webapp/go/app_handlers.go"], None),
    (23, "fix/chair-positions-gap", ["webapp/go/chair_handlers.go"], None),
]


class AutoStepTest(unittest.TestCase):
    def setUp(self):
        self.dir = tempfile.mkdtemp()
        os.environ["LANES_DIR"] = self.dir
        os.environ.pop("LANES_PLAN", None)  # the shipped plan.json

    def test_matcher_globs(self):
        r = lanes.auto_rule({"branches": ["integ/*"], "not": ["integ/setup*"], "files": ["docs/app-map.md"]})
        self.assertTrue(lanes.auto_match(r, "integ/w12", []))
        self.assertFalse(lanes.auto_match(r, "integ/setup", []))
        self.assertTrue(lanes.auto_match(r, "integ/setup", ["docs/app-map.md"]))  # a file still matches
        self.assertFalse(lanes.auto_match(lanes.auto_rule(None), "integ/w12", ["docs/app-map.md"]))
        for br in ("integ-160106", "integ/20261028-101500", "fix/20261028-101500/indexes"):
            self.assertEqual(lanes.auto_steps(lanes.load_plan(), br, []), ["A3"], br)
        for br in ("fix-14-coupons", "coupons-deadlock", "coordinate-deadlock", "matcher-reset-failed",
                   "docs/w1-design", "docs/deepdive", "docs/infra-tuning"):
            self.assertEqual(lanes.auto_steps(lanes.load_plan(), br, []), [], br)

    def test_each_practice_pr_matches_at_most_one_step(self):
        plan = lanes.load_plan()
        for n, br, files, _ in PRACTICE_1028:
            self.assertLessEqual(len(lanes.auto_steps(plan, br, files)), 1, (n, br))

    def test_ship_order_of_the_1028_practice_marks_the_lane_steps(self):
        lanes.mutate("step", {"id": "A2", "state": "doing"})
        got = {}
        for n, br, files, want in PRACTICE_1028:
            out = lanes.mutate("autostep", {"pr": str(n), "branch": br, "files": " ".join(files)})
            got[n] = out or None
            self.assertEqual(got[n], want, (n, br))
        st = lanes.read_board()["steps"]
        self.assertEqual(st["A2"]["state"], "done")
        self.assertEqual(st["A2"]["note"], "PR #8")
        self.assertEqual(st["A4"]["note"], "PR #19")
        # done / skip are never touched again
        lanes.mutate("step", {"id": "H6", "state": "skip", "note": "solo"})
        self.assertEqual(lanes.mutate("autostep", {"pr": "30", "branch": "docs/handoff", "files": "docs/handoff.md"}), "")
        self.assertEqual(lanes.read_board()["steps"]["H6"]["state"], "skip")

    def test_cli(self):
        r = subprocess.run([sys.executable, os.path.join(os.path.dirname(os.path.abspath(__file__)), "lanes.py"),
                            "autostep", "pr=8", "branch=setup/app-map", "files=docs/app-map.md"],
                           capture_output=True, text=True, env=os.environ)
        self.assertEqual(r.stdout.strip(), "A2")
        r = subprocess.run([sys.executable, os.path.join(os.path.dirname(os.path.abspath(__file__)), "lanes.py"),
                            "autostep", "pr=9", "branch=nothing"], capture_output=True, text=True, env=os.environ)
        self.assertEqual(r.stdout.strip(), "ok")


class SoftDepsTest(unittest.TestCase):
    def test_soft_dep_draws_dotted_edge_and_never_blocks(self):
        import lanes_web
        plan = [{"id": "X1", "lane": "1", "title": "a", "deps": [], "soft": [], "stage": 0, "how": ""},
                {"id": "X2", "lane": "c", "title": "b", "deps": [], "soft": [], "stage": 0, "how": ""},
                {"id": "P1", "lane": "bench", "title": "c", "deps": ["X1"], "soft": ["X2"], "stage": 1, "how": ""}]
        b = lanes.empty_board()
        b["steps"] = {"X1": {"state": "done"}}
        rm = lanes.roadmap(b, plan=plan, runs=[], target=None)
        st = {x["id"]: x["state"] for x in rm["steps"]}
        self.assertEqual(st["P1"], "ready")  # X2 is still todo but only soft
        self.assertNotIn("X2", rm["critical"])
        lay = lanes_web.layout(rm) if hasattr(lanes_web, "layout") else None
        if lay:
            soft = [e for e in lay["edges"] if e.get("soft")]
            self.assertEqual([(e["from"], e["to"]) for e in soft], [("X2", "P1")])



class WaitTodoTest(unittest.TestCase):
    def test_wait_returns_when_done_and_times_out_when_open(self):
        d = tempfile.mkdtemp()
        os.environ["LANES_DIR"] = d
        tid = lanes.mutate("todo", {"lane": "2", "msg": "hosts", "cmd": "sudo sh -c 'echo x >> /etc/hosts'"})
        self.assertEqual(lanes.wait_todo({"id": tid, "timeout": "0"}, poll=0), 2)
        lanes.mutate("done", {"id": tid})
        self.assertEqual(lanes.wait_todo({"id": tid, "timeout": "0"}, poll=0), 0)



class SeedTest(unittest.TestCase):
    def test_init_seeds_three_todos_once_and_not_on_a_used_board(self):
        os.environ["LANES_DIR"] = tempfile.mkdtemp()
        lanes.mutate("init", {})
        lanes.mutate("init", {})
        todos = lanes.read_board()["todos"]
        self.assertEqual(len(todos), 3)
        self.assertEqual([x["id"] for x in todos], ["t1", "t2", "t3"])  # the lane files name t2 (HOSTS) / t3 (URLs)
        self.assertIn("HOSTS", todos[1]["msg"])
        self.assertEqual((todos[1]["envkey"], len(todos[1]["inputs"])), ("HOSTS", 3))
        self.assertEqual(todos[2]["inputs"], ["当日マニュアル URL", "アプリマニュアル URL", "ベンチ（本番: ポータル / 練習: セットアップ済み、実行するマシンと --webapp の IP）"])
        self.assertNotIn("envkey", todos[2])
        os.environ["LANES_DIR"] = tempfile.mkdtemp()
        lanes.mutate("status", {"lane": "1", "state": "idle", "step": "x"})
        lanes.mutate("init", {})
        self.assertEqual(lanes.read_board()["todos"], [])



class InputTodoTest(unittest.TestCase):
    def setUp(self):
        self.dir = tempfile.mkdtemp()
        os.environ["LANES_DIR"] = self.dir
        self.repo = tempfile.mkdtemp()  # a temp "repo root": never the real .env
        self.env = os.path.join(self.repo, ".env")
        os.environ["LANES_ENV_FILE"] = self.env

    def tearDown(self):
        os.environ.pop("LANES_ENV_FILE", None)

    def run_cli(self, *args):
        return subprocess.run([sys.executable, SCRIPT, *args], capture_output=True, text=True, env=dict(os.environ))

    def serve(self):
        import threading
        import lanes_web
        srv = lanes_web.make_server(0, token="tok")
        threading.Thread(target=srv.serve_forever, daemon=True).start()
        self.addCleanup(srv.server_close)
        self.addCleanup(srv.shutdown)
        return f"http://127.0.0.1:{srv.server_address[1]}"

    def post(self, base, path, values=None, token="tok"):
        import urllib.parse
        import urllib.request
        h = {"X-Lanes": "1", **({"X-Lanes-Token": token} if token else {})}
        body = urllib.parse.urlencode([("v", v) for v in values]).encode() if values is not None else None
        return urllib.request.urlopen(urllib.request.Request(base + path, data=body, method="POST", headers=h)).status

    def test_input_todo_written_only_on_web_and_waited(self):
        import urllib.error
        import urllib.request
        tid = self.run_cli("todo", "lane=1", "msg=マニュアル", "input=当日 URL| アプリ URL |ベンチ").stdout.strip()
        td = next(x for x in lanes.read_board()["todos"] if x["id"] == tid)
        self.assertEqual(td["inputs"], ["当日 URL", "アプリ URL", "ベンチ"])
        for a in ({"input": "a", "pr": "5"}, {"input": "a", "choices": "x|y"}, {"envkey": "HOSTS"},
                  {"input": "a", "envkey": "SSH_KEY"}, {"input": "a|b", "envkey": "BENCH_CMD"},
                  {"input": "|".join("abcdefg")}):
            with self.assertRaises(lanes.LaneError, msg=a):
                lanes.mutate("todo", {"lane": "1", "msg": "x", **a})
        for op in ("input", "submit", "submit_input"):  # no CLI path to an answer
            self.assertEqual(self.run_cli(op, f"id={tid}", "v=https://x").returncode, 1, op)
        self.assertIn("入力: 当日 URL / アプリ URL / ベンチ", self.run_cli("board").stdout)
        frame = "\n".join("".join(t for _, t in segs) for segs in lanes.watch_frame(lanes.read_board(), lanes.now()))
        self.assertIn("入力: 当日 URL / アプリ URL / ベンチ", frame)
        base = self.serve()
        page = urllib.request.urlopen(base + "/").read().decode()
        self.assertIn("data-inp", page)
        self.assertEqual(json.loads(urllib.request.urlopen(base + "/board.json").read())["todos"][0]["inputs"],
                         ["当日 URL", "アプリ URL", "ベンチ"])
        bad = [(["https://a"], None), (["https://a"], "bad"), (["a\nb"], "tok"), (["a\x07"], "tok"),
               (["x" * 501], "tok"), (["", " ", ""], "tok"), (["a", "b", "c", "d"], "tok")]
        for vals, tok in bad:
            with self.assertRaises(urllib.error.HTTPError, msg=(vals, tok)):
                self.post(base, f"/input?id={tid}", vals, tok)
        with self.assertRaises(urllib.error.HTTPError):
            self.post(base, f"/done?id={tid}")  # ✅ is not an answer
        self.assertFalse(next(x for x in lanes.read_board()["todos"] if x["id"] == tid)["done"])
        url = "https://example.com/manual?a=1&b=" + "あ" * 10
        self.assertEqual(self.post(base, f"/input?id={tid}", [f"  {url} ", "https://app", "ポータル"]), 200)
        with self.assertRaises(urllib.error.HTTPError):
            self.post(base, f"/input?id={tid}", ["again"])  # already closed
        r = self.run_cli("wait", f"id={tid}", "timeout=0")
        self.assertEqual(r.returncode, 0)
        self.assertEqual(r.stdout.splitlines()[1:], [f"input.1={url}", "input.2=https://app", "input.3=ポータル",
                                                     "decision=done"])
        out = self.run_cli("board").stdout
        self.assertIn(f"{tid} → 入力済み", out)
        self.assertNotIn("https://app", out)  # the board never echoes the answers
        self.assertNotIn("https://app", json.dumps(json.loads(urllib.request.urlopen(base + "/board.json").read())))
        self.assertFalse(os.path.exists(self.env))  # no envkey: .env untouched

    def test_input_todo_closed_by_cli_prints_input_none(self):
        tid = lanes.mutate("todo", {"lane": "1", "msg": "x", "input": "a"})
        lanes.mutate("done", {"id": tid})
        r = self.run_cli("wait", f"id={tid}", "timeout=0")
        self.assertEqual(r.stdout.splitlines()[1:], ["input=none", "decision=done"])

    def test_envkey_hosts_rewrites_only_that_line(self):
        orig = (b"# team .env\nOPS_HOST=203.0.113.9\nHOSTS=192.0.2.1 192.0.2.2\r\nSSH_KEY=~/.ssh/k  # secret\n"
                b"BENCH_CMD=ssh -t u@h 'cd ~/bench && ./bench run'\nexport CONTEST=isucon13-old\n# HOSTS=commented\nLAST=1")
        with open(self.env, "wb") as f:
            f.write(orig)
        os.chmod(self.env, 0o640)
        tid = lanes.mutate("todo", {"lane": "2", "msg": "HOSTS", "input": "svc1|svc2|svc3", "envkey": "hosts"})
        for vals in (["10.0.0.300"], ["10.0.0.1", "bad_host!"], ["", ""], ["10.0.0.1 $(id)"]):
            with self.assertRaises(lanes.LaneError, msg=vals):
                lanes.submit_input(tid, vals)
        with open(self.env, "rb") as f:
            self.assertEqual(f.read(), orig)  # rejected answers write nothing
        lanes.submit_input(tid, [" 10.0.0.1", "10.0.0.2", "isu3.example.com"])
        with open(self.env, "rb") as f:
            got = f.read()
        want = orig.replace(b"HOSTS=192.0.2.1 192.0.2.2\r\n", b"HOSTS=10.0.0.1 10.0.0.2 isu3.example.com\r\n")
        self.assertEqual(got, want)
        self.assertEqual(os.stat(self.env).st_mode & 0o777, 0o640)
        self.assertEqual([x for x in os.listdir(self.repo) if x != ".env"], [])  # no temp file left
        r = self.run_cli("wait", f"id={tid}", "timeout=0")
        self.assertEqual(r.stdout.splitlines()[1:], ["input.1=10.0.0.1", "input.2=10.0.0.2", "input.3=isu3.example.com",
                                                     "env.HOSTS=10.0.0.1 10.0.0.2 isu3.example.com", "decision=done"])
        # other keys: the value is not kept on the board and lane:wait says only "set"
        t_cmd = lanes.mutate("todo", {"lane": "1", "msg": "bench", "input": "コマンド", "envkey": "BENCH_CMD"})
        cmd = "ssh -t -i ~/.ssh/k ubuntu@10.0.0.2 'sudo -u isucon -i bash -c \"cd ~/bench && ./bench run\"'"
        lanes.submit_input(t_cmd, [cmd])
        t_ctx = lanes.mutate("todo", {"lane": "1", "msg": "contest", "input": "名前", "envkey": "CONTEST"})
        lanes.submit_input(t_ctx, ["isucon13-1026"])
        t_gh = lanes.mutate("todo", {"lane": "1", "msg": "project", "input": "番号", "envkey": "GH_PROJECT"})
        lanes.submit_input(t_gh, ["ytakaya/5"])
        with open(self.env, "rb") as f:
            got = f.read().split(b"\n")
        self.assertEqual(got[4], b"BENCH_CMD=" + cmd.encode())
        self.assertEqual(got[5], b"export CONTEST=isucon13-1026")  # export kept
        self.assertEqual(got[6:], [b"# HOSTS=commented", b"LAST=1", b"GH_PROJECT=ytakaya/5", b""])  # appended
        self.assertEqual(got[3], b"SSH_KEY=~/.ssh/k  # secret")
        board = json.dumps(lanes.read_board(), ensure_ascii=False)
        self.assertNotIn("ubuntu@10.0.0.2", board)
        r = self.run_cli("wait", f"id={t_cmd}", "timeout=0")
        self.assertEqual(r.stdout.splitlines()[1:], ["env.BENCH_CMD=set", "decision=done"])
        self.assertIn(f"{t_cmd} → 入力済み（.env BENCH_CMD）", self.run_cli("board").stdout)
        # quoting / validation / allow-list
        t_x = lanes.mutate("todo", {"lane": "1", "msg": "b", "input": "c", "envkey": "BENCH_CMD"})
        lanes.submit_input(t_x, ["./bench run --target https://x #1 $HOME"])
        with open(self.env, "rb") as f:
            self.assertIn(b"BENCH_CMD='./bench run --target https://x #1 $HOME'\n", f.read())
        t_y = lanes.mutate("todo", {"lane": "1", "msg": "b", "input": "c", "envkey": "BENCH_CMD"})
        with self.assertRaises(lanes.LaneError):
            lanes.submit_input(t_y, ["echo '$X'"])  # ' together with $: not writable safely
        t_l = lanes.mutate("todo", {"lane": "1", "msg": "login", "input": "user", "envkey": "HOST_LOGIN"})
        with self.assertRaises(lanes.LaneError):
            lanes.submit_input(t_l, ["ubuntu; rm -rf ~"])
        with self.assertRaises(lanes.LaneError):
            lanes.write_env(self.env, "SSH_KEY", "x")
        with self.assertRaises(lanes.LaneError):
            lanes.write_env(self.env, "PATH", "x")

    def test_seeded_hosts_todo_through_the_web_writes_env(self):
        import urllib.error
        lanes.mutate("init", {})
        base = self.serve()
        with self.assertRaises(urllib.error.HTTPError):
            self.post(base, "/input?id=t2", ["10.0.0.1", "10.0.0.2", "10.0.0.3"], token=None)
        self.assertFalse(os.path.exists(self.env))
        self.assertEqual(self.post(base, "/input?id=t2", ["10.0.0.1", "10.0.0.2", "10.0.0.3"]), 200)
        with open(self.env, "rb") as f:
            self.assertEqual(f.read(), b"HOSTS=10.0.0.1 10.0.0.2 10.0.0.3\n")  # created
        self.assertEqual(os.stat(self.env).st_mode & 0o777, 0o600)


class AgentsEtaTest(unittest.TestCase):
    def test_agents_and_eta_on_status(self):
        os.environ["LANES_DIR"] = tempfile.mkdtemp()
        lanes.mutate("status", {"lane": "3", "state": "working", "step": "fix", "agents": "4"})
        b = lanes.read_board()
        d = lanes.board_json(b, lanes.now())
        l = next(x for x in d["lanes"] if x["id"] == "3")
        self.assertEqual(l["agents"], 4)
        self.assertIn("🤖4", lanes.render(b, lanes.now()))
        lanes.mutate("status", {"lane": "3", "state": "idle", "step": "done"})
        l = lanes.read_board()["lanes"]["3"]
        self.assertEqual(l["agents"], 0)

    def test_items_checklist_progress(self):
        os.environ["LANES_DIR"] = tempfile.mkdtemp()
        lanes.mutate("items", {"id": "A3", "items": "indexes|livecomment|stats"})
        lanes.mutate("item", {"id": "A3", "n": "1", "state": "done"})
        lanes.mutate("item", {"id": "A3", "n": "2", "state": "doing"})
        b = lanes.read_board()
        tk = lanes.lane_tasks(b, "3")
        self.assertEqual((tk[0]["id"], tk[0]["done_n"], tk[0]["total"]), ("A3", 1, 3))
        self.assertEqual(b["steps"]["A3"]["state"], "doing")
        with self.assertRaises(lanes.LaneError):
            lanes.mutate("item", {"id": "A3", "n": "9"})


def serve_web(token="tok"):
    import threading
    import lanes_web
    srv = lanes_web.make_server(0, token=token)
    threading.Thread(target=srv.serve_forever, daemon=True).start()
    return srv, f"http://127.0.0.1:{srv.server_address[1]}"


def web_post(base, path, token=None, body=None):
    import urllib.request
    h = {"X-Lanes": "1", **({"X-Lanes-Token": token} if token else {})}
    if body is not None:
        h["Content-Type"] = "application/x-www-form-urlencoded"
    req = urllib.request.Request(base + path, method="POST", headers=h, data=body.encode() if body is not None else None)
    with urllib.request.urlopen(req) as r:
        return r.status


def read_events(d):
    with open(os.path.join(d, "events.jsonl")) as f:
        return [json.loads(x) for x in f]


class MultiPrTodoTest(unittest.TestCase):
    def setUp(self):
        self.dir = tempfile.mkdtemp()
        os.environ["LANES_DIR"] = self.dir

    def test_one_review_todo_for_several_prs(self):
        import urllib.error
        import urllib.request
        tid = lanes.mutate("todo", {"lane": "1", "msg": "ベンチ 3 のレビュー", "pr": "7 #8,9 7",
                                    "titles": {"7": "indexes", "9": "reaction N+1"}})
        td = lanes.read_board()["todos"][0]
        self.assertEqual((td["pr"], td["prs"], td["pr_titles"]), (7, [7, 8, 9], {"7": "indexes", "9": "reaction N+1"}))
        self.assertEqual(lanes.todo_kind(td), "review")
        for bad in ("7 x", "1.5"):
            with self.assertRaises(lanes.LaneError):
                lanes.mutate("todo", {"lane": "1", "msg": "x", "pr": bad})
        single = lanes.mutate("todo", {"lane": "1", "msg": "one", "pr": "5"})
        self.assertIn("[PR #7 #8 #9]", lanes.render(lanes.read_board(), lanes.now()))
        d = lanes.board_json(lanes.read_board(), lanes.now())
        self.assertEqual([(p["n"], p["title"]) for p in d["todos"][0]["pr_list"]], [(7, "indexes"), (8, ""), (9, "reaction N+1")])
        srv, base = serve_web()
        try:
            page = urllib.request.urlopen(base + "/").read().decode()
            for want in ("まとめて merge OK", "要修正（タブで番号）", "data-prsel", "held="):
                self.assertIn(want, page)
            for path, tok in ((f"/decide?id={tid}&decision=ok&held=8", None),  # no token: only the page decides
                              (f"/decide?id={tid}&decision=ok&held=8", "bad"),
                              (f"/decide?id={tid}&decision=ok&held=12", "tok"),  # not in the todo
                              (f"/decide?id={tid}&decision=ok&held=7,8,9", "tok")):  # all held = 要修正
                with self.assertRaises(urllib.error.HTTPError, msg=path):
                    web_post(base, path, tok)
            self.assertEqual(web_post(base, f"/decide?id={tid}&decision=ok&held=8", "tok"), 200)
            self.assertEqual(web_post(base, f"/decide?id={single}&decision=changes", "tok"), 200)
        finally:
            srv.shutdown()
            srv.server_close()
        td = next(x for x in lanes.read_board()["todos"] if x["id"] == tid)
        self.assertEqual((td["decision"], td["ok_prs"], td["held_prs"]), ("ok", [7, 9], [8]))
        r = subprocess.run([sys.executable, SCRIPT, "wait", f"id={tid}", "timeout=0"], capture_output=True, text=True)
        self.assertTrue(r.stdout.endswith("ok_prs=7 9\nheld_prs=8\ndecision=ok\n"), r.stdout)
        r = subprocess.run([sys.executable, SCRIPT, "wait", f"id={single}", "timeout=0"], capture_output=True, text=True)
        self.assertTrue(r.stdout.endswith("ok_prs=\nheld_prs=5\ndecision=changes\n"), r.stdout)
        self.assertEqual(lanes.board_ok(7)[:2], "盤 ")
        self.assertEqual((lanes.board_ok(8), lanes.board_ok(5), lanes.board_ok(99)), ("", "", ""))
        self.assertIn("ok（保留 #8）", lanes.render(lanes.read_board(), lanes.now()))
        done = [e for e in read_events(self.dir) if e["kind"] == "todo_done"]
        self.assertEqual([(e["id"], e["decision"], e.get("ok_prs"), e.get("held_prs"), e["tkind"]) for e in done],
                         [(tid, "ok", "7 9", "8", "review"), (single, "changes", None, "5", "review")])

    def test_cli_review_todo_fetches_titles_from_gh(self):
        bindir = tempfile.mkdtemp()
        with open(os.path.join(bindir, "gh"), "w") as f:
            f.write('#!/bin/sh\necho \'[{"number": 7, "title": "idx"}, {"number": 9, "title": "n+1"}, {"number": 3, "title": "x"}]\'\n')
        os.chmod(os.path.join(bindir, "gh"), 0o755)
        env = dict(os.environ, PATH=bindir + os.pathsep + os.environ["PATH"], LANES_NO_GH="0")
        r = subprocess.run([sys.executable, SCRIPT, "todo", "lane=1", "msg=review", "pr=7 8 9"], capture_output=True,
                           text=True, env=env)
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertEqual(lanes.read_board()["todos"][0]["pr_titles"], {"7": "idx", "9": "n+1"})


class StartsTest(unittest.TestCase):
    def setUp(self):
        os.environ["LANES_DIR"] = self.dir = tempfile.mkdtemp()

    def test_seed_and_starts_carry_the_model_of_each_lane(self):
        cfg = lanes.load_sessions()
        self.assertEqual({k: cfg[k]["model"] for k in ("1", "2", "3", "c")},
                         {"1": "claude-opus-5-5", "2": "claude-opus-5-5", "3": "claude-opus-5-5", "c": "claude-fable-5-1"})
        lanes.mutate("init", {})
        t1 = lanes.read_board()["todos"][0]
        self.assertTrue(t1["cmd"].startswith("LANE=1 claude --model claude-opus-5-5 'あなたはレーン 1（ハブ）。"), t1["cmd"])
        self.assertIn("/fast", t1["msg"])
        self.assertEqual((t1["kind"], t1["start_lane"]), ("start", "1"))
        r = subprocess.run([sys.executable, SCRIPT, "starts", "hub=hub-7"], capture_output=True, text=True)
        self.assertEqual(r.stdout.strip(), "t4 t5 t6")
        todos = {x["start_lane"]: x for x in lanes.read_board()["todos"] if x.get("start_lane")}
        self.assertEqual(todos["c"]["cmd"], "LANE=c claude --model claude-fable-5-1 'あなたはレーン c（スカウト）。docs/lanes/README.md と "
                         "docs/lanes/c-scout.md を読んで従って。レーン 1 の名前は hub-7。最初に [HELLO] を送って。'")
        self.assertIn("2-infra.md", todos["2"]["cmd"])
        self.assertNotIn("/fast", todos["2"]["msg"])
        import shlex
        self.assertEqual(shlex.split(todos["3"]["cmd"])[:4], ["LANE=3", "claude", "--model", "claude-opus-5-5"])  # one line
        self.assertEqual(subprocess.run([sys.executable, SCRIPT, "starts", "hub=x"], capture_output=True, text=True).stdout.strip(),
                         "already")  # still open: not stacked
        lanes.mutate("done", {"id": todos["2"]["id"]})
        self.assertEqual(lanes.mutate("starts", {"hub": "hub-8"}), "t7")
        with self.assertRaises(lanes.LaneError):
            lanes.mutate("starts", {"hub": " "})
        sj = tempfile.NamedTemporaryFile("w", suffix=".json", delete=False)
        json.dump({"lanes": {"2": {"model": "claude-sonnet-x", "start": "lane 2 of {hub}"}}}, sj)
        sj.close()
        os.environ["LANES_SESSIONS"] = sj.name
        try:
            self.assertEqual(lanes.start_todo("2", "h")["cmd"], "LANE=2 claude --model claude-sonnet-x 'lane 2 of h'")
            os.environ["LANES_OTEL"] = "1"  # .env LANES_OTEL=1: the OTel env goes in front, still one line
            cmd = lanes.start_todo("2", "h")["cmd"]
            self.assertTrue(cmd.startswith("LANE=2 CLAUDE_CODE_ENABLE_TELEMETRY=1 OTEL_METRICS_EXPORTER=otlp"), cmd)
            self.assertIn("OTEL_RESOURCE_ATTRIBUTES=lane=2 claude --model", cmd)
            self.assertNotIn("\n", cmd)
        finally:
            del os.environ["LANES_SESSIONS"]
            os.environ.pop("LANES_OTEL", None)


    def test_agents_md_is_current(self):
        # AGENTS.md (Codex) is generated from CLAUDE.md + tools/codex/AGENTS.codex.md: `task agents:md CHECK=1`
        tool = os.path.join(os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__)))), "tools", "codex",
                            "agents_md.py")
        r = subprocess.run([sys.executable, tool, "--check"], capture_output=True, text=True)
        self.assertEqual(r.returncode, 0, "AGENTS.md is stale: run `task agents:md` and commit it\n" + r.stderr)


class EventsTest(unittest.TestCase):
    def setUp(self):
        os.environ["LANES_DIR"] = self.dir = tempfile.mkdtemp()

    def test_board_changes_and_outside_steps_are_logged(self):
        lanes.mutate("status", {"lane": "2", "state": "working", "step": "peek"})
        lanes.mutate("status", {"lane": "2", "state": "working", "step": "peek 2"})  # same state: a lane_step only
        lanes.mutate("status", {"lane": "2", "state": "waiting-human", "step": "push"})
        aid = lanes.mutate("alert", {"lane": "2", "msg": "x"})
        lanes.mutate("resolve", {"id": aid})
        tid = lanes.mutate("todo", {"lane": "1", "msg": "bench", "kind": "bench"})
        with self.assertRaises(lanes.LaneError):
            lanes.mutate("todo", {"lane": "1", "msg": "x", "kind": "Bad Kind"})
        lanes.mutate("done", {"id": tid})
        lanes.mutate("bench", {"state": "ready", "sha": "abc", "prs": "7"})
        lanes.mutate("phase", {"phase": "loop"})
        r = subprocess.run([sys.executable, SCRIPT, "event", "kind=ship.step", "name=deploy", "sec=42", "ok=1"],
                           capture_output=True, text=True)
        self.assertEqual(r.returncode, 0, r.stderr)
        ev = read_events(self.dir)
        kinds = [e["kind"] for e in ev]
        self.assertEqual(kinds, ["lane", "lane_step", "lane", "alert", "lane", "resolve", "lane", "todo", "todo_done", "bench", "phase",
                                 "ship.step"])
        ev = [e for e in ev if e["kind"] != "lane_step"]
        self.assertEqual([(e["frm"], e["to"]) for e in ev if e["kind"] == "lane"],
                         [("idle", "working"), ("working", "waiting-human"), ("waiting-human", "alert"), ("alert", "waiting-human")])
        self.assertEqual(ev[7]["tkind"], "bench")
        self.assertEqual((ev[-1]["name"], ev[-1]["sec"], ev[-1]["ok"]), ("deploy", 42, 1))
        self.assertEqual((ev[8]["frm"], ev[8]["to"], ev[8]["sha"], ev[8]["prs"]), ("none", "ready", "abc", "#7"))


class BenchDoneTest(unittest.TestCase):
    def setUp(self):
        os.environ["LANES_DIR"] = self.dir = tempfile.mkdtemp()
        self.bindir = tempfile.mkdtemp()
        self.calls = os.path.join(self.bindir, "calls")

    def stub_task(self, with_bench_done=True):
        p = os.path.join(self.bindir, "task")
        with open(p, "w") as f:
            f.write("#!/bin/sh\n"
                    'if [ "$1" = --list ]; then echo "* lane:web:   x"; %s exit 0; fi\n'
                    'echo "$@" >> "%s"; echo bench-done-ran\n' % ('echo "* bench:done:   collect";' if with_bench_done else "", self.calls))
        os.chmod(p, 0o755)
        os.environ["LANES_TASK"] = p

    def tearDown(self):
        os.environ.pop("LANES_TASK", None)

    def wait_note(self, want):
        import time
        for _ in range(100):
            note = lanes.read_board()["bench"].get("done_note", "")
            if want in note:
                return note
            time.sleep(0.05)
        self.fail(f"band note never said {want!r}: {lanes.read_board()['bench']}")

    def test_bench_done_button_moves_band_logs_and_runs_task(self):
        import urllib.error
        import urllib.parse
        import urllib.request
        self.stub_task()
        srv, base = serve_web()
        body = urllib.parse.urlencode({"score": "95,591", "output": "score: 95591\n名前解決成功数: 36007"})
        try:
            page = urllib.request.urlopen(base + "/").read().decode()
            self.assertIn("終わった", page)
            self.assertNotIn('id="bench"', page)  # the band is no longer drawn (the ops panel shows bench state)
            other = lanes.mutate("todo", {"lane": "1", "msg": "x"})
            tid = lanes.mutate("todo", {"lane": "1", "msg": "ベンチ: main abc1234", "kind": "bench"})
            lanes.mutate("bench", {"state": "running", "sha": "abc1234", "prs": "7 8"})
            for path, tok, b in (("/bench-done", "tok", body),  # no id: the button lives on the bench todo
                                 (f"/bench-done?id={other}", "tok", body),  # not a bench todo
                                 (f"/bench-done?id={tid}", None, body), (f"/bench-done?id={tid}", "bad", body),
                                 (f"/bench-done?id={tid}", "tok", "score=12x")):
                with self.assertRaises(urllib.error.HTTPError, msg=(path, tok, b)):
                    web_post(base, path, tok, b)
            self.assertEqual(web_post(base, f"/bench-done?id={tid}", "tok", body), 200)
            bn = lanes.read_board()["bench"]
            self.assertEqual((bn["state"], bn["done_score"]), ("collect", "95591"))
            td = next(x for x in lanes.read_board()["todos"] if x["id"] == tid)
            self.assertEqual((td["done"], td["decision"], td["decided_by"], td["done_score"]), (True, "done", "web", "95591"))
            with self.assertRaises(urllib.error.HTTPError):  # twice: already closed
                web_post(base, f"/bench-done?id={tid}", "tok", body)
            note = self.wait_note("ok")
            self.assertIn("bench-done-", note)
            d = json.loads(urllib.request.urlopen(base + "/board.json").read())
            self.assertTrue(d["bench"]["done_hm"])
        finally:
            srv.shutdown()
            srv.server_close()
        with open(self.calls) as f:
            args = f.read().split()
        self.assertEqual(args[:2], ["bench:done", "SCORE=95591"])
        out = args[2].split("=", 1)[1]
        self.assertTrue(out.startswith(self.dir), out)
        with open(out) as f:
            self.assertEqual(f.read(), "score: 95591\n名前解決成功数: 36007\n")
        ev = read_events(self.dir)
        bd = next(e for e in ev if e["kind"] == "bench_done")
        self.assertEqual((bd["score"], bd["frm"], bd["id"]), ("95591", "running", tid))
        self.assertTrue(any(e["kind"] == "todo_done" and e["id"] == tid and e["tkind"] == "bench" for e in ev))
        self.assertTrue(any(e["kind"] == "bench" and e["to"] == "collect" and e.get("src") == "bench_done" for e in ev))
        self.assertTrue(any(e["kind"] == "bench_done_task" and e["ok"] == 1 for e in ev))
        lanes.mutate("bench", {"state": "ready", "sha": "def5678"})  # the next ship (new sha): the old 「終わった」 goes
        self.assertNotIn("done_at", lanes.read_board()["bench"])

    def test_bench_todo_done_runs_bench_done_in_background(self):
        """「🏁 終わった」 on the bench todo: todo closed, lane:wait prints decision=done (and score), and
        `task bench:done` is started (subprocess mocked) even when the band was already moved by hand."""
        import io
        from contextlib import redirect_stdout
        from unittest import mock
        import lanes_web
        tid = lanes.mutate("todo", {"lane": "1", "msg": "ベンチ", "kind": "bench"})
        lanes.mutate("bench", {"state": "none"})
        proc = mock.Mock()
        proc.wait.return_value = 0
        with mock.patch.object(lanes_web, "has_bench_done", return_value=True), \
                mock.patch.object(lanes_web.subprocess, "Popen", return_value=proc) as popen:
            lanes_web.bench_done("1234", "", tid).join(5)
        args = popen.call_args[0][0]
        self.assertEqual(args[1:], ["bench:done", "SCORE=1234"])
        self.assertEqual(lanes.read_board()["bench"]["state"], "none")  # band not ready / running: left as it was
        out = io.StringIO()
        with redirect_stdout(out):
            self.assertEqual(lanes.wait_todo({"id": tid, "timeout": "1"}), 0)
        self.assertIn("score=1234\ndecision=done", out.getvalue())
        ev = read_events(self.dir)
        self.assertTrue(any(e["kind"] == "bench_done_task" and e["ok"] == 1 for e in ev))

    def test_without_bench_done_task_the_band_still_moves_and_says_so(self):
        self.stub_task(with_bench_done=False)
        lanes.mutate("bench", {"state": "ready"})
        import lanes_web
        lanes_web.bench_done("", "").join(5)
        self.assertEqual(lanes.read_board()["bench"]["state"], "collect")
        self.assertIn("bench:done が無い", self.wait_note("無い"))
        self.assertFalse(os.path.exists(self.calls))

    def test_runs_wait_keeps_collect_after_bench_done(self):
        import io
        from contextlib import redirect_stdout
        import runs
        rows = tempfile.NamedTemporaryFile("w", suffix=".json", delete=False)
        json.dump([{"ts": "20261020-160320", "req": 3000, "score": 3113, "verdict": "x"}], rows)
        rows.close()
        live = tempfile.NamedTemporaryFile("w", suffix=".json", delete=False)
        json.dump([{"ts": "20261020-170000", "hosts": {"svc1": {"end_reason": "", "req_total": 10}}}], live)
        live.close()
        os.environ.update(RUNS_JSON=rows.name, RUNS_LIVE_JSON=live.name)
        try:
            lanes.mutate("bench", {"state": "running"})
            lanes.bench_done("", "")
            with redirect_stdout(io.StringIO()):
                self.assertEqual(runs.main(["wait", "after=20261020-160320", "timeout=0"]), 2)
            self.assertEqual(lanes.read_board()["bench"]["state"], "collect")  # not back to running
            with redirect_stdout(io.StringIO()):
                self.assertEqual(runs.main(["wait", "after=20261020-150000", "timeout=0"]), 0)
            self.assertEqual(lanes.read_board()["bench"]["state"], "none")  # the scored report is in
            ev = read_events(self.dir)
            self.assertTrue(any(e["kind"] == "run_scored" and e["score"] == "3113" for e in ev))
        finally:
            os.environ.pop("RUNS_JSON")
            os.environ.pop("RUNS_LIVE_JSON")


class PracticeOtelTodoTest(unittest.TestCase):
    def setUp(self):
        os.environ["LANES_DIR"] = tempfile.mkdtemp()
        self.env = os.path.join(tempfile.mkdtemp(), ".env")
        os.environ["LANES_ENV_FILE"] = self.env

    def tearDown(self):
        os.environ["LANES_ENV_FILE"] = os.path.join(tempfile.mkdtemp(), ".env")

    def test_practice_board_gets_t0_and_start_commands_follow_env(self):
        with open(self.env, "w") as f:
            f.write("HOSTS=1.2.3.4\nHOST_LOGIN=ubuntu\n")
        lanes.mutate("init", {})
        todos = lanes.read_board()["todos"]
        self.assertEqual([x["id"] for x in todos], ["t0", "t1", "t2", "t3"])  # t1-t3 keep their ids
        self.assertEqual((todos[0]["cmd"], todos[0]["kind"]), ("task otel:up", "otel"))
        self.assertIn("LANES_OTEL=1", todos[0]["msg"])
        t1 = todos[1]
        self.assertTrue(lanes.copy_text(t1).startswith("LANE=1 claude"))
        with open(self.env, "a") as f:
            f.write("LANES_OTEL=1\n")  # added after the board was seeded: the next show / copy picks it up
        self.assertTrue(lanes.copy_text(t1).startswith("LANE=1 CLAUDE_CODE_ENABLE_TELEMETRY=1"), lanes.copy_text(t1))
        d = lanes.board_json(lanes.read_board(), lanes.now())
        self.assertIn("OTEL_RESOURCE_ATTRIBUTES=lane=1", d["todos"][1]["cmd"])
        lanes.mutate("starts", {"hub": "h"})
        st = next(x for x in lanes.read_board()["todos"] if x.get("start_lane") == "c")
        self.assertIn("OTEL_RESOURCE_ATTRIBUTES=lane=c claude --model claude-fable-5-1", lanes.copy_text(st))
        self.assertIn("レーン 1 の名前は h", lanes.copy_text(st))

    def test_contest_board_has_no_t0(self):
        with open(self.env, "w") as f:
            f.write("HOST_LOGIN=isucon\nPRACTICE=0\n")
        lanes.mutate("init", {})
        self.assertEqual([x["id"] for x in lanes.read_board()["todos"]], ["t1", "t2", "t3"])
        os.environ["LANES_DIR"] = tempfile.mkdtemp()
        with open(self.env, "w") as f:
            f.write("PRACTICE='1'\n")
        lanes.mutate("init", {})
        self.assertEqual(lanes.read_board()["todos"][0]["id"], "t0")

    def test_step_changes_are_logged_with_note(self):
        lanes.mutate("status", {"lane": "3", "state": "waiting", "step": "PR #1", "note": "[PR] #1"})
        lanes.mutate("status", {"lane": "3", "state": "waiting", "step": "PR #2", "note": "x"})
        lanes.mutate("status", {"lane": "3", "state": "waiting", "step": "PR #2", "note": "x"})  # nothing changed
        with open(os.path.join(os.environ["LANES_DIR"], "events.jsonl")) as f:
            ev = [json.loads(x) for x in f]
        self.assertEqual([(e["kind"], e.get("step"), e.get("note")) for e in ev],
                         [("lane", "PR #1", "[PR] #1"), ("lane_step", "PR #2", "x")])


class ChoicePickNudgeTest(unittest.TestCase):
    def setUp(self):
        self.dir = tempfile.mkdtemp()
        os.environ["LANES_DIR"] = self.dir

    def cli(self, *args):
        return subprocess.run([sys.executable, SCRIPT, *args], capture_output=True, text=True, env=dict(os.environ))

    def web(self):
        import threading
        import lanes_web
        srv = lanes_web.make_server(0, token="tok")
        threading.Thread(target=srv.serve_forever, daemon=True).start()
        self.addCleanup(srv.server_close)
        self.addCleanup(srv.shutdown)
        return f"http://127.0.0.1:{srv.server_address[1]}"

    def post(self, url):
        import urllib.request
        return urllib.request.urlopen(urllib.request.Request(url, method="POST", headers={"X-Lanes": "1", "X-Lanes-Token": "tok"})).status

    def test_choices_with_descriptions(self):
        import urllib.parse
        import urllib.request
        tid = self.cli("todo", "lane=1", "msg=構成を決める", "why=DB CPU 律速",
                       "choices=案1 DB→svc3::DB を svc3 に出す。追加 0 行|案2 そのまま::何もしない|やめる").stdout.strip()
        td = next(x for x in lanes.read_board()["todos"] if x["id"] == tid)
        self.assertEqual(td["choices"], ["案1 DB→svc3", "案2 そのまま", "やめる"])
        self.assertEqual(td["choice_desc"], {"案1 DB→svc3": "DB を svc3 に出す。追加 0 行", "案2 そのまま": "何もしない"})
        self.assertIn("選択: 案1 DB→svc3 / 案2 そのまま / やめる", self.cli("board").stdout)
        base = self.web()
        page = urllib.request.urlopen(base + "/").read().decode()
        self.assertIn("推奨", page)
        self.assertIn("choice_desc", page)
        d = json.loads(urllib.request.urlopen(base + "/board.json").read())
        self.assertEqual(d["todos"][0]["choice_desc"]["案1 DB→svc3"], "DB を svc3 に出す。追加 0 行")
        self.assertEqual(self.post(base + f"/decide?id={tid}&decision=" + urllib.parse.quote("案1 DB→svc3")), 200)
        self.assertIn("decision=案1 DB→svc3\n", self.cli("wait", f"id={tid}", "timeout=0").stdout)

    def test_picks_partial_approval_on_web_only(self):
        import urllib.error
        import urllib.request
        tid = self.cli("todo", "lane=1", "msg=先読みの手を作ってよいか",
                       "picks=U1 retry_after 30→500 ms::通知 4 か所の定数|U2 matching を全件最近傍::1 関数、50 行以内|U3 決済::冪等キー").stdout.strip()
        td = next(x for x in lanes.read_board()["todos"] if x["id"] == tid)
        self.assertEqual([p["key"] for p in td["picks"]], ["U1", "U2", "U3"])
        self.assertEqual(td["picks"][1], {"key": "U2", "label": "U2 matching を全件最近傍", "desc": "1 関数、50 行以内"})
        for a in ({"picks": "U1 a|U2 b", "pr": "5"}, {"picks": "U1 a|U2 b", "choices": "x|y"}, {"picks": "U1 a", "input": "x"},
                  {"picks": "U1 a|U1 b"}):
            with self.assertRaises(lanes.LaneError, msg=a):
                lanes.mutate("todo", {"lane": "1", "msg": "x", **a})
        self.assertEqual(self.cli("decide", f"id={tid}", "decision=ok").returncode, 1)  # no CLI path
        with self.assertRaises(lanes.LaneError):
            lanes.web_done(tid)
        self.assertIn("選ぶ: U1 retry_after 30→500 ms", self.cli("board").stdout)
        base = self.web()
        page = urllib.request.urlopen(base + "/").read().decode()
        self.assertIn("選んだものを OK", page)
        self.assertIn("全部やめる", page)
        for q in (f"/decide?id={tid}&decision=ok&dropped=U9", f"/decide?id={tid}&decision=changes"):
            with self.assertRaises(urllib.error.HTTPError, msg=q):
                self.post(base + q)
        self.assertEqual(self.post(base + f"/decide?id={tid}&decision=ok&dropped=U2"), 200)
        out = self.cli("wait", f"id={tid}", "timeout=0").stdout
        self.assertIn("picked=U1 U3\ndropped=U2\n", out)
        self.assertTrue(out.endswith("decision=ok\n"))
        tid2 = self.cli("todo", "lane=1", "msg=y", "picks=A1 a|A2 b").stdout.strip()
        self.assertEqual(self.post(base + f"/decide?id={tid2}&decision=none"), 200)
        out = self.cli("wait", f"id={tid2}", "timeout=0").stdout
        self.assertIn("picked=\ndropped=A1 A2\n", out)
        self.assertTrue(out.endswith("decision=none\n"))
        tid3 = self.cli("todo", "lane=1", "msg=z", "picks=B1 a|B2 b").stdout.strip()
        self.assertEqual(self.post(base + f"/decide?id={tid3}&decision=ok&dropped=B1,B2"), 200)  # all dropped = none
        self.assertTrue(self.cli("wait", f"id={tid3}", "timeout=0").stdout.endswith("decision=none\n"))

    def test_idle_lane_nudge_for_the_hub(self):
        b, t = lanes.empty_board(), 10_000
        b["phase"] = "loop"
        lanes.apply(b, "status", {"lane": "1", "state": "working", "step": "ship"}, t - 600)
        lanes.apply(b, "status", {"lane": "2", "state": "waiting", "step": "PR #3 merge 待ち", "note": "#6 は NEXT"}, t - 420)
        lanes.apply(b, "status", {"lane": "3", "state": "idle", "step": "手が空いた"}, t - 100)  # < 3 min
        lanes.apply(b, "status", {"lane": "c", "state": "waiting", "step": "sub", "agents": "2"}, t - 600)  # subagents run
        out = lanes.render(b, t)
        self.assertIn("!! レーン 2: waiting 7m（#6 は NEXT）→ 手が空いたら から [TASK]", out)
        self.assertNotIn("レーン 3:", out)
        self.assertNotIn("レーン c:", out)
        self.assertEqual([n["lane"] for n in lanes.board_json(b, t)["nudges"]], ["2"])
        frame = "\n".join("".join(x for _, x in segs) for segs in lanes.watch_frame(b, t))
        self.assertIn("!! レーン 2: waiting 7m", frame)
        b["phase"] = "freeze"
        self.assertNotIn("!! レーン", lanes.render(b, t))


class CcHookTest(unittest.TestCase):
    """cchook.py: never fails, and warns (not blocks) the hub editing another lane's file in the main checkout."""
    HOOK = os.path.join(os.path.dirname(os.path.abspath(__file__)), "cchook.py")

    def setUp(self):
        self.tmp = os.path.realpath(tempfile.mkdtemp())
        self.repo = os.path.join(self.tmp, "repo")
        os.makedirs(self.repo)
        for c in (["git", "init", "-q", "-b", "main"], ["git", "commit", "-q", "--allow-empty", "-m", "i"]):
            subprocess.run(c, cwd=self.repo, check=True, capture_output=True,
                           env=dict(os.environ, GIT_AUTHOR_NAME="t", GIT_AUTHOR_EMAIL="t@e", GIT_COMMITTER_NAME="t",
                                    GIT_COMMITTER_EMAIL="t@e"))
        subprocess.run(["git", "worktree", "add", "-q", os.path.join(self.tmp, "wt-x"), "-b", "x"], cwd=self.repo,
                       check=True, capture_output=True)
        subprocess.run(["git", "worktree", "add", "-q", os.path.join(self.repo, ".claude/worktrees/y"), "-b", "y"],
                       cwd=self.repo, check=True, capture_output=True)
        self.board = os.path.join(self.tmp, "board")

    def hook(self, payload, lane="1", raw=None):
        env = dict(os.environ, LANE=lane, LANES_DIR=self.board, CLAUDE_PROJECT_DIR=self.repo)
        r = subprocess.run([sys.executable, self.HOOK], input=raw if raw is not None else json.dumps(payload),
                           capture_output=True, text=True, env=env, cwd=self.repo, timeout=10)
        self.assertEqual(r.returncode, 0, r.stderr)
        return r.stdout

    def edit(self, path, tool="Edit"):
        return {"hook_event_name": "PreToolUse", "tool_name": tool, "session_id": "s", "tool_use_id": "u",
                "cwd": self.repo, "tool_input": {"file_path": path, "old_string": "a", "new_string": "b"}}

    def test_warns_hub_on_other_files_in_the_main_checkout_only(self):
        out = self.hook(self.edit(os.path.join(self.repo, "webapp/go/main.go")))
        j = json.loads(out)
        self.assertIn("webapp/go/main.go", j["hookSpecificOutput"]["additionalContext"])
        self.assertEqual(j["hookSpecificOutput"]["hookEventName"], "PreToolUse")
        self.assertNotIn("permissionDecision", j["hookSpecificOutput"])  # a warning, never a block
        self.assertIn("worktree", j["systemMessage"])
        for ok in ("docs/journal.md", "docs/claims.md", "docs/kickoff.md", "docs/manual/day.md", "docs/plans/x/y.md",
                   "CLAUDE.md", "alp.yml", "deploy.sh", "tools/app-analyzer/analyzer.config.json"):
            self.assertEqual(self.hook(self.edit(os.path.join(self.repo, ok), "Write")), "", ok)
        for elsewhere in (os.path.join(self.tmp, "wt-x/webapp/a.go"), os.path.join(self.repo, ".claude/worktrees/y/a.go"),
                          "/etc/hosts"):
            self.assertEqual(self.hook(self.edit(elsewhere)), "", elsewhere)
        self.assertEqual(self.hook(self.edit(os.path.join(self.repo, "webapp/go/main.go")), lane="3"), "")
        self.assertEqual(self.hook({**self.edit(os.path.join(self.repo, "x.go")), "hook_event_name": "PostToolUse"}), "")
        with open(os.path.join(self.board, "cc-events.jsonl")) as f:
            ev = [json.loads(x) for x in f]
        self.assertEqual(ev[0].get("warn"), "hub-main-edit")

    def test_never_breaks_on_junk(self):
        for raw in ("", "not json", "[]", '{"hook_event_name": "PreToolUse", "tool_name": "Edit", "tool_input": 5}',
                    '{"hook_event_name": "PreToolUse", "tool_name": "Write", "tool_input": {"file_path": null}}'):
            self.assertEqual(self.hook(None, raw=raw), "", raw)

    def test_board_state_follows_the_session(self):
        def state():
            with open(os.path.join(self.board, "board.json")) as f:
                return json.load(f)["lanes"]["3"]["state"]

        def msg(text, **kw):
            return {"hook_event_name": "PreToolUse", "tool_name": "SendMessage", "tool_input": {"to": "h", "message": text},
                    **kw}

        prompt, stop = {"hook_event_name": "UserPromptSubmit", "prompt": "[TASK] x"}, {"hook_event_name": "Stop"}
        self.hook(prompt, lane="3")
        self.assertEqual(state(), "working")
        self.hook(msg("hello"), lane="3")
        self.assertEqual(state(), "working")
        self.hook(msg("[DONE] 3 x", agent_id="sub"), lane="3")  # a subagent's message never moves the lane
        self.assertEqual(state(), "working")
        self.hook(msg("[PR] 3 #12"), lane="3")
        self.assertEqual(state(), "waiting")
        self.hook(stop, lane="3")
        self.assertEqual(state(), "waiting")  # Stop never over waiting
        self.hook(prompt, lane="3")
        self.hook(stop, lane="3")
        self.assertEqual(state(), "idle")
        os.environ["LANES_DIR"] = self.board
        lanes.mutate("status", {"lane": "3", "state": "working", "step": "s", "agents": "2"})
        self.hook(stop, lane="3")
        self.assertEqual(state(), "working")  # subagents still running
        lanes.mutate("alert", {"lane": "3", "msg": "m"})
        self.hook(prompt, lane="3")
        self.assertEqual(state(), "alert")
        with open(os.path.join(self.board, "board.json")) as f:
            self.assertEqual(json.load(f)["lanes"]["3"]["step"], "s")  # the STEP text stays

    def test_sent_message_marks_done_on_steps(self):
        def steps():
            with open(os.path.join(self.board, "board.json")) as f:
                return json.load(f)["steps"]

        def msg(text, **kw):
            return {"hook_event_name": "PreToolUse", "tool_name": "SendMessage", "tool_input": {"to": "h", "message": text},
                    **kw}

        env_plan = os.environ.pop("LANES_PLAN", None)  # the shipped plan.json
        try:
            self.hook(msg("[DONE] c manual 型 3 / 6"), lane="c")
            self.assertEqual((steps()["C1"]["state"], steps()["C1"]["by"]), ("done", "msg"))
            self.hook(msg("[PICK] docs/technique-scan-early.md units=3 ask=1"), lane="c")
            self.assertEqual(steps()["C2"]["state"], "done")
            self.hook(msg("[DONE] servers", agent_id="sub"), lane="2")  # a subagent's message never marks
            self.assertNotIn("I3", steps())
            self.hook(msg("[DONE] servers"), lane="2")
            self.assertEqual(steps()["I3"]["note"], "[DONE] servers")
        finally:
            if env_plan is not None:
                os.environ["LANES_PLAN"] = env_plan




class StepMarkTest(unittest.TestCase):
    """2026-10-28: I1–I3, A1, C1, B1–B3 stayed todo (only merges and panel scores ticked steps); A2 was done by a
    docs/plans/app-early.md merge."""

    def setUp(self):
        self.dir = tempfile.mkdtemp()
        os.environ["LANES_DIR"] = self.dir
        os.environ.pop("LANES_PLAN", None)  # the shipped plan.json

    def steps(self):
        return lanes.read_board()["steps"]

    def test_msgstep_marks_the_lane_step_only(self):
        self.assertEqual(lanes.mutate("msgstep", {"lane": "c", "msg": "[DONE] c manual 型は 3（マッチング）と 6"}), "C1")
        st = self.steps()["C1"]
        self.assertEqual((st["state"], st["by"]), ("done", "msg"))
        self.assertEqual(st["note"], "[DONE] c manual 型は 3（マッチング）と 6"[:40])
        self.assertEqual(lanes.mutate("msgstep", {"lane": "3", "msg": "[DONE] c manual x"}), "")  # not lane 3's step
        self.assertEqual(lanes.mutate("msgstep", {"lane": "c", "msg": "[DONE] c technique-scan docs/plans/x.md"}), "C3")
        self.assertEqual(lanes.mutate("msgstep", {"lane": "3", "msg": "[DONE] 3 crawl docs/reports/latest.md"}), "A1")
        self.assertEqual(lanes.mutate("msgstep", {"lane": "2", "msg": "[DONE] servers"}), "I3")
        self.assertEqual(lanes.mutate("msgstep", {"lane": "2", "msg": "[DONE] servers"}), "")  # already done
        lanes.mutate("step", {"id": "C2", "state": "skip"})
        self.assertEqual(lanes.mutate("msgstep", {"lane": "c", "msg": "[PICK] docs/technique-scan-early.md units=1"}), "")
        self.assertEqual(self.steps()["C2"]["state"], "skip")  # a manual skip is kept

    def test_lane_msg_marks_steps_too(self):
        lanes.mutate("msg", {"from": "c", "to": "1", "msg": "[DONE] c manual 型 3"})
        self.assertEqual(self.steps()["C1"]["state"], "done")
        lanes.mutate("msg", {"from": "1", "to": "c", "msg": "[DONE] c manual 型 3"})  # the sender's lane counts
        self.assertNotIn("A1", self.steps())

    def test_a2_not_done_by_app_early(self):
        plan = lanes.load_plan()
        self.assertEqual(lanes.auto_steps(plan, "docs/app-early", ["docs/plans/app-early.md"]), [])
        self.assertEqual(lanes.auto_steps(plan, "x", ["docs/app-map.md"]), ["A2"])

    def test_bench_end_marks_b1_then_b2(self):
        b = lanes.empty_board()
        self.assertEqual(lanes.bench_step(b, 1000, ts="20261028-101500", score="3113"), "B1")
        self.assertEqual(b["steps"]["B1"]["note"], "20261028-101500 score 3113")
        self.assertEqual(lanes.bench_step(b, 1010, ts="20261028-101500", score="3113"), "")  # the same run twice
        # the next bench: 「終わった」 first (no ts), then its run → one step, not two
        self.assertEqual(lanes.bench_step(b, 5000, score="4200", button=True), "B2")
        self.assertEqual(lanes.bench_step(b, 5100, ts="20261028-110000", score="4210"), "B2")
        self.assertEqual(b["steps"]["B2"]["note"], "20261028-110000 score 4210")
        self.assertNotIn("B3", b["steps"])
        # ts order is not used: an older-looking ts still ticks the next step
        self.assertEqual(lanes.bench_step(b, 9000, ts="20261028-090000", score="5000"), "B3")
        # a late 「終わった」 after runs:wait already reset the band: joins B3
        self.assertEqual(lanes.bench_step(b, 9100, button=True, advance=False), "")
        self.assertNotIn("B4", b["steps"])

    def test_bench_done_and_benchstep_ops(self):
        lanes.mutate("bench", {"state": "ready", "sha": "abc"})
        lanes.bench_done(score="3113")
        self.assertEqual(self.steps()["B1"]["state"], "done")
        self.assertEqual(lanes.mutate("benchstep", {"ts": "20261028-101500", "score": "3113"}), "B1")  # same bench
        self.assertNotIn("B2", self.steps())
        self.assertEqual(lanes.mutate("benchstep", {"ts": "20261028-120000", "score": "9000"}), "B2")
        self.assertEqual(self.steps()["B2"]["note"], "20261028-120000 score 9000")

    def test_auto_done_wins_over_manual_doing(self):
        b = lanes.empty_board()
        lanes.apply(b, "step", {"id": "B1", "state": "doing"}, 1)
        rm = lanes.roadmap(b, runs=[{"ts": "20261028-101500", "score": 3113, "req": 3400}])
        self.assertEqual({x["id"]: x["state"] for x in rm["steps"]}["B1"], "done")
        lanes.apply(b, "step", {"id": "B1", "state": "skip"}, 2)  # a manual skip still wins
        rm = lanes.roadmap(b, runs=[{"ts": "20261028-101500", "score": 3113, "req": 3400}])
        self.assertEqual({x["id"]: x["state"] for x in rm["steps"]}["B1"], "skip")


class NoteStoryTest(unittest.TestCase):
    def setUp(self):
        os.environ["LANES_DIR"] = self.dir = tempfile.mkdtemp()

    def run_cli(self, *args):
        return subprocess.run([sys.executable, SCRIPT, *args], capture_output=True, text=True, env=dict(os.environ))

    def test_note_cli_appends_an_event(self):
        r = self.run_cli("note", "lane=3", "kind=bottleneck", "msg=livecomment の N+1 が DB CPU の 6 割", "ref=PR #7")
        self.assertEqual((r.returncode, r.stdout.strip()), (0, "ok"), r.stderr)
        ev = [e for e in read_events(self.dir) if e["kind"] == "note"]
        self.assertEqual(len(ev), 1)
        self.assertEqual((ev[0]["lane"], ev[0]["nkind"], ev[0]["ref"]), ("3", "bottleneck", "PR #7"))
        self.assertEqual(ev[0]["msg"], "livecomment の N+1 が DB CPU の 6 割")
        for bad in (["lane=3", "kind=memo", "msg=x"], ["lane=3", "kind=fix", "msg= "], ["kind=fix", "msg=x"]):
            r = self.run_cli("note", *bad)
            self.assertEqual(r.returncode, 1, bad)
        r = self.run_cli("note", "lane=1", "kind=app", "msg=" + "あ" * 300)
        self.assertEqual(r.returncode, 0)
        self.assertEqual(len([e for e in read_events(self.dir) if e["kind"] == "note"][-1]["msg"]), lanes.MAX_NOTE)

    def test_note_body_is_stored_and_shown(self):
        r = self.run_cli("note", "lane=c", "kind=scan", "msg=early-scan: 3 採用 / 1 見送り",
                         "body=U1 DNS TTL 採用\\nU2 pdns cache 採用\nQ1 見送り: 効果が測れない")
        self.assertEqual(r.returncode, 0, r.stderr)
        ev = [e for e in read_events(self.dir) if e["kind"] == "note"][-1]
        self.assertEqual(ev["body"], ["U1 DNS TTL 採用", "U2 pdns cache 採用", "Q1 見送り: 効果が測れない"])
        row = lanes.story_rows(lanes.story())[0]
        self.assertEqual((row["icon"], row["body"][2]), ("🔎", "Q1 見送り: 効果が測れない"))
        self.assertTrue(row["key"].startswith("r"))
        self.assertEqual(self.run_cli("note", "lane=c", "kind=scan", "msg=x", "body=" + "\\n".join("abcdef")).returncode, 1)
        r = self.run_cli("note", "lane=1", "kind=app", "msg=y", "body=" + "あ" * 300)
        self.assertEqual(r.returncode, 0)
        self.assertEqual(len([e for e in read_events(self.dir) if e["kind"] == "note"][-1]["body"][0]), lanes.MAX_BODY_LINE)

    def test_story_groups_per_bench_and_highlights(self):
        ev = lambda t, kind, **kw: {"t": t, "kind": kind, **kw}
        evs = [ev(100, "note", lane="1", nkind="app", msg="ISUPipe", body=["score = 投げ銭"]),
               ev(110, "note", lane="3", nkind="fix", msg="index 13 本", ref="PR #1"),
               ev(120, "run_scored", ts="r1", score="3371"),
               ev(130, "note", lane="1", nkind="bottleneck", msg="pdns が SQL の 96%"),
               ev(140, "todo", id="t5", lane="1", tkind="question", msg="どちらにする"),
               ev(150, "todo_done", id="t5", lane="1", tkind="question", decision="A案", by="web"),
               ev(160, "bench_done", score="44101", sha="abc"),  # 「終わった」 then runs:wait's score: one header
               ev(170, "note", lane="2", nkind="fix", msg="DB 分離"),
               ev(180, "run_scored", ts="r2", score="44101"),
               ev(180, "run_scored", ts="r2", score="44101"),  # logged twice: once
               ev(190, "note", lane="3", nkind="fix", msg="icon 304"),
               ev(195, "note", lane="2", nkind="fix", msg="app ×2"),
               ev(196, "alert", id="a1", lane="1", level="crit", msg="5xx")]
        gs = lanes.story(evs)
        self.assertEqual([(g.get("current", False), g["bench"], g["score"]) for g in gs],
                         [(True, 3, ""), (False, 2, "44101"), (False, 1, "3371")])
        self.assertEqual([x["text"] for x in gs[1]["items"]], ["DB 分離", "A案 — どちらにする", "pdns が SQL の 96%"])
        self.assertEqual([x["kind"] for x in gs[0]["items"]], ["incident", "fix", "fix"])
        self.assertEqual(gs[1]["ts"], "r2")
        self.assertEqual(len(lanes.story_rows(lanes.story(evs, n=3))), 3)  # the newest 3 rows only
        b = lanes.empty_board()
        lanes.apply(b, "todo", {"lane": "1", "msg": "ベンチ 7 のレビュー", "pr": "21"}, 1)
        lanes.apply(b, "todo", {"lane": "1", "msg": "500 をどうする", "choices": "やらない|直す"}, 1)
        lanes.apply(b, "todo", {"lane": "1", "msg": "sudo する"}, 1)
        h = lanes.highlights(b, gs, runs=[{"ts": "r2", "score": 62374, "verdict": "DB CPU bound"}], target=50000, evs=evs,
                             docs={"score": [], "forbid": []})
        self.assertEqual((h["app"]["text"], h["app"]["body"]), ("ISUPipe", []))  # a score line goes to 🧮 (kickoff only)
        self.assertEqual(h["bottleneck"]["text"], "pdns が SQL の 96%")
        self.assertEqual([x["text"] for x in h["fix"]], ["app ×2", "icon 304", "DB 分離"])
        self.assertEqual([(x["id"], x["kind"]) for x in h["decisions"]], [("t1", "review"), ("t2", "question")])
        sc = h["score"]
        self.assertEqual((sc["latest"], sc["bench"], sc["verdict"], sc["best"], sc["pct"]), ("44101", 2, "DB CPU bound", 62374, 125))

    def test_app_slot_keeps_the_kickoff_note_and_splits_score_and_forbid(self):
        ev = lambda t, kind, **kw: {"t": t, "kind": kind, **kw}
        evs = [ev(100, "note", lane="3", nkind="app", msg="early app-map", ref="docs/app-map.md"),
               ev(110, "note", lane="1", nkind="app", msg="ISUCONDITION: ISU の condition", ref="docs/kickoff.md",
                  body=["スコア = 1000 + condition", "構成: 3 台", "減点: 誤り −1", "禁止: env-checker", "猶予: 1 秒",
                        ]),
               ev(200, "note", lane="3", nkind="app", msg="app-map: Go 1 ファイル", ref="docs/app-map.md")]
        h = lanes.highlights(lanes.empty_board(), lanes.story(evs), evs=evs, docs={"score": ["docs"], "forbid": ["docs"]},
                             target=50000)
        self.assertEqual((h["app"]["text"], h["app"]["body"], h["app"]["map"]),
                         ("ISUCONDITION: ISU の condition", ["構成: 3 台"], "app-map: Go 1 ファイル"))
        self.assertEqual(h["calc"], {"lines": ["スコア = 1000 + condition", "減点: 誤り −1", "猶予: 1 秒"],
                                     "src": "kickoff note", "target": 50000})
        self.assertEqual(h["forbid"], {"lines": ["禁止: env-checker"], "src": "kickoff note"})
        evs2 = evs[:1]  # no kickoff note: the oldest app note, 🧮 / ⛔ from docs/kickoff.md
        h = lanes.highlights(lanes.empty_board(), lanes.story(evs2), evs=evs2, docs={"score": ["s"], "forbid": ["f"]})
        self.assertEqual((h["app"]["text"], h["app"]["map"], h["calc"]["lines"], h["calc"]["src"], h["forbid"]["lines"]),
                         ("early app-map", None, ["s"], "docs/kickoff.md", ["f"]))
        h = lanes.highlights(lanes.empty_board(), [], evs=[], docs={"score": [], "forbid": []})
        self.assertEqual((h["app"], h["calc"]["lines"], h["forbid"]["lines"], h["bottleneck"]), (None, [], [], None))

    def test_brief_docs_reads_kickoff_md(self):
        top = tempfile.mkdtemp()
        os.makedirs(os.path.join(top, "docs"))
        path = os.path.join(top, "docs", "kickoff.md")
        with open(path, "w") as f:
            f.write("# kickoff\n\n| 節 | 内容 |\n|---|---|\n| アクセス | x |\n\n## スコア\n\n- 互換性チェックで **1,000** 点\n"
                    "- 減点: 誤り −1\n- 猶予: 1 秒\n- 負荷走行 60 秒\n\n## 禁止・必須\n\n- 変更禁止: `env-checker`\n"
                    "- 最終スコア = 再起動後\n- 終了後は触らない\n")
        self.assertEqual(lanes.brief_docs(top), {"score": ["互換性チェックで 1,000 点", "減点: 誤り −1", "猶予: 1 秒"],
                                                 "forbid": ["変更禁止: env-checker", "最終スコア = 再起動後"]})
        with open(path, "w") as f:  # the table row wins over the section
            f.write("| 節 | 内容 |\n|---|---|\n| スコア | 成功 1 点、失敗 −1 |\n| 禁止・必須 | SG を変えない |\n\n## スコア\n\n- x\n")
        os.utime(path, (1, 1))
        self.assertEqual(lanes.brief_docs(top), {"score": ["成功 1 点、失敗 −1"], "forbid": ["SG を変えない"]})
        self.assertEqual(lanes.brief_docs(tempfile.mkdtemp()), {"score": [], "forbid": []})

    def test_bottleneck_slot_is_marked_stale_after_a_newer_bench(self):
        ev = lambda t, kind, **kw: {"t": t, "kind": kind, **kw}
        evs = [ev(100, "run_scored", ts="r1", score="1000"),
               ev(110, "note", lane="1", nkind="bottleneck", msg="trend 35%", body=["avg 859ms", "x"]),
               ev(120, "run_scored", ts="r2", score="1500")]
        h = lanes.highlights(lanes.empty_board(), lanes.story(evs), evs=evs, docs={"score": [], "forbid": []},
                             runs=[{"ts": "r2", "score": 1500, "verdict": "DB CPU"}])
        self.assertEqual((h["bottleneck"]["text"], h["bottleneck"]["line"], h["bottleneck"]["stale"], h["verdict"]),
                         ("trend 35%", "avg 859ms", "ベンチ 2 より前", "DB CPU"))
        self.assertEqual((h["score"]["latest"], h["score"]["prev"], h["score"]["delta_pct"]), ("1500", "1000", 50.0))
        evs.append(ev(130, "note", lane="c", nkind="bottleneck", msg="DB 律速"))
        h = lanes.highlights(lanes.empty_board(), lanes.story(evs), evs=evs, docs={"score": [], "forbid": []})
        self.assertEqual((h["bottleneck"]["text"], h["bottleneck"]["stale"]), ("DB 律速", ""))

    def test_story_cards_group_prs_and_decisions_per_bench(self):
        ev = lambda t, kind, **kw: {"t": t, "kind": kind, **kw}
        evs = [ev(10, "note", lane="1", nkind="app", msg="ISUCONDITION", ref="docs/kickoff.md"),
               ev(20, "todo", id="t3", lane="1", tkind="question", msg="構成は"),
               ev(25, "todo_done", id="t3", lane="1", tkind="question", decision="DB → svc3", by="web"),
               ev(30, "alert", id="a1", lane="2", level="warn", msg="warn は出さない"),
               ev(40, "bench_done", score="1641", prs="baseline", id="t9"),
               ev(50, "note", lane="3", nkind="fix", msg="index 2 本", ref="PR #6"),
               ev(60, "ship.end", ok=1, merged="#3 #6", mode="deploy"),
               ev(61, "ship.end", ok=1, merged="#7", mode="docs-only"),
               ev(70, "bench_done", score="30418", prs="#3 #6", id="t24"),
               ev(80, "note", lane="1", nkind="bottleneck", msg="ベンチ 2: trend 35%"),
               ev(85, "msg", id="m2", frm="3", to="1", tag="DONE", msg="[DONE] 3 effect r9 integ=効いた"),
               ev(90, "todo", id="t38", lane="1", tkind="question", msg="どうする"),
               ev(95, "todo_done", id="t37", lane="1", tkind="review", decision="ok", by="web", ok_prs="20"),
               ev(96, "todo_done", id="t38", lane="1", tkind="question", decision="#20 で前に進む", by="web"),
               ev(97, "alert", id="a2", lane="1", level="crit", msg="score −47%"),
               ev(98, "ship.end", ok=1, merged="#20", mode="deploy")]
        cards = lanes.story_cards(evs=evs, cc=[{"t": 45, "lane": "2", "text": "[DONE] 2 effect x=効かない"}])
        self.assertEqual([(c["bench"], c["score"], c["delta_pct"]) for c in cards],
                         [(2, "30418", 1753.6), (1, "1641", None), (0, "", None)])
        lines = lambda c: [(x["k"], x["text"]) for x in c["lines"]]
        self.assertEqual(lines(cards[0]), [("pr", "#3 #6（docs #7）"), ("bottleneck", "ベンチ 2: trend 35%"),
                                           ("effect", "[DONE] 3 effect r9 integ=効いた"),
                                           ("decision", "t38 #20 で前に進む"), ("incident", "score −47%"),
                                           ])  # 5 lines max: the 「次」 line (#20) does not fit
        self.assertEqual(lines(cards[1]), [("effect", "[DONE] 2 effect x=効かない")])
        self.assertEqual(lines(cards[2]), [("app", "ISUCONDITION"), ("decision", "t3 DB → svc3")])
        self.assertTrue(cards[2]["prep"])
        evs = [e for e in evs[:-3] if e["kind"] != "msg"]
        cards = lanes.story_cards(evs=evs, cc=[])  # no effect line: the fix notes of the bench's PRs
        self.assertIn(("fix", "index 2 本"), lines(cards[0]))
        self.assertIn(("decision", "t37 merge OK #20"), lines(cards[0]))  # only a review answer: shown

    def test_web_serves_story_and_highlights(self):
        lanes.mutate("note", {"lane": "1", "kind": "app", "msg": "ISUPipe", "body": "a\\nb"})
        lanes.record_event("run_scored", ts="20261028-010000", score="55000")
        srv, base = serve_web()
        try:
            import urllib.request
            d = json.loads(urllib.request.urlopen(base + "/board.json").read())
            self.assertEqual(d["story"][1]["items"][0]["body"], ["a", "b"])
            self.assertEqual(d["highlights"]["score"]["latest"], "55000")
            self.assertEqual(d["highlights"]["app"]["text"], "ISUPipe")
            self.assertEqual([c["bench"] for c in d["cards"]], [1, 0])
            self.assertIn("scan", d["note_kinds"])
            page = urllib.request.urlopen(base + "/").read().decode()
            self.assertIn("📜 経緯", page)
            self.assertIn("いまの要点", page)
            self.assertIn("既読にする", page)
            self.assertIn("すべて表示", page)
            self.assertIn("🧮 スコア計算", page)
            self.assertIn(json.dumps(self.dir)[1:-1], page)  # the board dir keys the page's last-read time (per board)
        finally:
            srv.shutdown()
            srv.server_close()

if __name__ == "__main__":
    unittest.main()
