# python3 -m unittest discover -s tools/lanes -p 'test_*.py'
# Codex lanes: board messages (lane:msg / inbox / wait-msg), the per-lane agent in sessions.json, lane:handoff, and the
# generated AGENTS.md (the kit's own AGENTS.md is checked in test_lanes.py StartsTest).
import json
import os
import shutil
import subprocess
import sys
import tempfile
import threading
import time
import unittest

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
os.environ["LANES_NO_GH"] = "1"
os.environ.setdefault("LANES_ENV_FILE", os.path.join(tempfile.mkdtemp(), ".env"))
for _k in ("PRACTICE", "HOST_LOGIN", "LANES_OTEL"):
    os.environ.pop(_k, None)
import lanes  # noqa: E402
import lanehandoff  # noqa: E402
import timeline  # noqa: E402

HERE = os.path.dirname(os.path.abspath(__file__))
SCRIPT = os.path.join(HERE, "lanes.py")
ROOT = os.path.dirname(os.path.dirname(HERE))
AGENTS_MD = os.path.join(ROOT, "tools", "codex", "agents_md.py")


def events(d):
    with open(os.path.join(d, "events.jsonl")) as f:
        return [json.loads(x) for x in f if x.strip()]


class MsgTest(unittest.TestCase):
    def setUp(self):
        self.dir = tempfile.mkdtemp()
        os.environ["LANES_DIR"] = self.dir

    def cli(self, *args, **kw):
        return subprocess.run([sys.executable, SCRIPT, *args], capture_output=True, text=True, env=dict(os.environ), **kw)

    def test_send_inbox_marks_read_and_logs_latency(self):
        r = self.cli("msg", "from=1", "to=3", "msg=[TASK] 3 fix TS=20261031-113002")
        self.assertEqual((r.returncode, r.stdout.strip()), (0, "m1"), r.stderr)
        self.assertEqual(self.cli("msg", "from=3", "to=1", "msg=[PR] #21 ready integ-113002").stdout.strip(), "m2")
        b = lanes.read_board()
        self.assertEqual(len(lanes.unread_msgs(b, "3")), 1)
        lanes.mutate("status", {"lane": "3", "state": "working", "step": "x"})
        self.assertIn("✉1", lanes.render(lanes.read_board(), lanes.now()))
        out = self.cli("inbox", "lane=3").stdout
        self.assertIn("m1", out)
        self.assertIn("1→3 [TASK] 3 fix TS=20261031-113002", out)
        self.assertIn("(lane 3: 未読のメッセージは無い)", self.cli("inbox", "lane=3").stdout)
        hist = self.cli("inbox", "lane=3", "all=1").stdout
        self.assertIn("m1", hist)
        self.assertIn("m2", hist)
        self.assertIn("既読", hist)
        self.assertIn("未読", hist)  # m2 went to the hub, still unread there
        kinds = [e["kind"] for e in events(self.dir)]
        self.assertEqual(kinds.count("msg"), 2)
        rd = [e for e in events(self.dir) if e["kind"] == "msg_read"]
        self.assertEqual([(e["id"], e["lane"], e["frm"], e["tag"]) for e in rd], [("m1", "3", "1", "TASK")])
        self.assertIsInstance(rd[0]["sec"], int)

    def test_rules(self):
        b, t = lanes.empty_board(), 1000
        for a in [{"from": "2", "to": "3", "msg": "[TASK] x"},        # lane to lane
                  {"from": "1", "to": "1", "msg": "[TASK] x"},
                  {"from": "1", "to": "3", "msg": "fix please"},      # no tag
                  {"from": "", "to": "3", "msg": "[TASK] x"}]:
            with self.assertRaises(lanes.LaneError, msg=a):
                lanes.apply(b, "msg", a, t)
        self.assertEqual(lanes.apply(b, "msg", {"from": "C", "to": "1", "msg": "[HELLO] c codex\nmore"}, t), "m1")
        self.assertEqual(b["msgs"][0]["msg"], "[HELLO] c codex more")
        self.assertEqual(b["msgs"][0]["from"], "c")
        for i in range(lanes.MAX_MSGS + 5):
            lanes.apply(b, "msg", {"from": "1", "to": "2", "msg": f"[TASK] 2 n{i}"}, t)
        self.assertEqual(len(b["msgs"]), lanes.MAX_MSGS)
        self.assertEqual(b["next_msg"], lanes.MAX_MSGS + 6)  # ids never reused

    def test_wait_msg_wakes_on_a_message_and_times_out(self):
        r = self.cli("wait-msg", "lane=2", "timeout=0")
        self.assertEqual(r.returncode, 2)
        p = subprocess.Popen([sys.executable, SCRIPT, "wait-msg", "lane=2", "timeout=30"], stdout=subprocess.PIPE,
                             text=True, env=dict(os.environ))
        time.sleep(0.5)
        self.cli("msg", "from=1", "to=2", "msg=[TASK] 2 setup")
        out, _ = p.communicate(timeout=20)
        self.assertEqual(p.returncode, 0)
        self.assertIn("[TASK] 2 setup", out)
        self.assertEqual(lanes.unread_msgs(lanes.read_board(), "2"), [])

    def test_board_json_carries_agent_and_card_messages(self):
        lanes.mutate("status", {"lane": "2", "state": "working", "step": "setup"})
        for i in range(6):
            lanes.mutate("msg", {"from": "1", "to": "2", "msg": f"[TASK] 2 n{i}"})
        d = lanes.board_json(lanes.read_board(), lanes.now())
        l2 = d["lanes"][0]
        self.assertEqual((l2["agent"], l2["unread"], len(l2["msgs"])), ("claude", 6, lanes.CARD_MSGS))
        self.assertEqual(l2["msgs"][-1]["msg"], "[TASK] 2 n5")
        self.assertFalse(l2["msgs"][-1]["read"])

    def test_timeline_section(self):
        lanes.mutate("msg", {"from": "1", "to": "c", "msg": "[TASK] c deep-dive TS=1"})
        lanes.mutate("msg", {"from": "1", "to": "c", "msg": "[TASK] c design x"})
        lanes.mutate("inbox", {"lane": "c"})
        md = "\n".join(timeline.msg_section(events(self.dir)))
        self.assertIn("盤のメッセージ", md)
        self.assertIn("| 1→c | 2 | 2 |", md)
        self.assertIn("✉ m1 1→c", timeline.describe(events(self.dir)[0]))


class AgentTest(unittest.TestCase):
    def setUp(self):
        self.dir = tempfile.mkdtemp()
        os.environ["LANES_DIR"] = self.dir
        with open(os.path.join(HERE, "sessions.json")) as f:
            self.cfg = json.load(f)
        self.sj = os.path.join(self.dir, "sessions.json")
        os.environ["LANES_SESSIONS"] = self.sj

    def tearDown(self):
        os.environ.pop("LANES_SESSIONS", None)

    def write(self, **agents):
        cfg = json.loads(json.dumps(self.cfg))
        for k, v in agents.items():
            cfg["lanes"][k]["agent"] = v
        with open(self.sj, "w") as f:
            json.dump(cfg, f, ensure_ascii=False)

    def test_kit_sessions_json_is_valid_and_all_claude(self):
        os.environ["LANES_SESSIONS"] = os.path.join(HERE, "sessions.json")
        self.assertEqual(lanes.agents_text(), "1=claude 2=claude 3=claude c=claude")
        self.assertTrue(lanes.start_todo("1")["cmd"].startswith("LANE=1 claude --model claude-opus-5-5 '"))

    def test_codex_lane_start_command(self):
        self.write(**{"3": "codex"})
        self.assertEqual(lanes.lane_agent("3"), "codex")
        self.assertEqual(lanes.codex_lanes(), ["3"])
        r = subprocess.run([sys.executable, SCRIPT, "agent", "lane=3"], capture_output=True, text=True, env=dict(os.environ))
        self.assertEqual(r.stdout.strip(), "codex")
        r = subprocess.run([sys.executable, SCRIPT, "agent"], capture_output=True, text=True, env=dict(os.environ))
        self.assertIn("1=claude 2=claude 3=codex c=claude", r.stdout)
        self.assertIn("lane:wait-msg LANE=1", r.stdout)
        st = lanes.start_todo("3", "hub-7")
        self.assertTrue(st["cmd"].startswith(
            "LANE=3 codex --model gpt-6.1-sol --approve-for-me -c sandbox_workspace_write.network_access=true "
            "--add-dir \"$PWD/..\" 'あなたはレーン 3（アプリ）。"), st["cmd"])
        self.assertIn("task lane:msg FROM=3", st["cmd"])
        self.assertIn("task lane:inbox LANE=3", st["cmd"])
        self.assertIn("Codex GPT-6.1-Sol", st["msg"])
        self.assertNotIn("/fast", st["msg"])
        os.environ["LANES_OTEL"] = "1"
        try:  # the OTel env is Claude Code's: never on a codex line
            self.assertNotIn("OTEL", lanes.start_todo("3", "h")["cmd"])
            self.assertIn("OTEL", lanes.start_todo("2", "h")["cmd"])
        finally:
            os.environ.pop("LANES_OTEL")
        lanes.mutate("starts", {"hub": "hub-7"})
        t3 = next(x for x in lanes.read_board()["todos"] if x.get("start_lane") == "3")
        self.assertTrue(lanes.copy_text(t3).startswith("LANE=3 codex"))
        self.assertIn("lane:msg", t3["why"])
        lanes.mutate("status", {"lane": "3", "state": "working", "step": "fix"})
        self.assertIn("[codex]", lanes.render(lanes.read_board(), lanes.now()))
        self.assertEqual(lanes.board_json(lanes.read_board(), lanes.now())["lanes"][0]["agent"], "codex")

    def test_codex_hub(self):
        self.write(**{"1": "codex"})
        t1 = lanes.start_todo("1")
        self.assertTrue(t1["cmd"].startswith("LANE=1 codex --model gpt-6.1-sol --approve-for-me"), t1["cmd"])
        self.assertIn("lane-1", t1["cmd"])          # start_codex: no ListAgents
        self.assertNotIn("ListAgents で自分の名前を出して", t1["cmd"])
        c2 = lanes.start_todo("2", "lane-1")["cmd"]  # a Claude lane under a Codex hub: lane:msg out, wait-msg in
        self.assertTrue(c2.startswith("LANE=2 claude"))
        self.assertIn("task lane:msg FROM=2 TO=1", c2)
        self.assertIn("task lane:wait-msg LANE=2", c2)

    def test_template_override(self):
        cfg = json.loads(json.dumps(self.cfg))
        cfg["lanes"]["c"]["agent"] = "codex"
        cfg["lanes"]["c"]["codex_model"] = "gpt-x"
        cfg["templates"]["codex"] = "codex{model} --sandbox workspace-write -a never {start}"
        with open(self.sj, "w") as f:
            json.dump(cfg, f, ensure_ascii=False)
        self.assertTrue(lanes.start_todo("c", "h")["cmd"].startswith("LANE=c codex --model gpt-x --sandbox workspace-write -a never '"))


class HandoffTest(unittest.TestCase):
    CLAIMS = """# claims

| branch | files / functions | layer | owner (session) | status | updated |
|---|---|---|---|---|---|
| integ/113002 | `webapp/go/livestream_handler.go`: `getLivestreamsHandler` | logic | lane3 / subagent | open | 11:40 |
| setup/db | `etc/mysql` | config | lane2 | open | 10:40 |
| fix/old | `x.go` | logic | lane3 | merged | 10:00 |
"""
    LANE3 = """## トリガー → やること → 出すもの

| トリガー | やること | 出すもの |
|---|---|---|
| `[TASK] 3 app-map` | app-analysis skill | PR |
| `[TASK] 3 fix TS=<ts>` | parallel-fix skill（`docs/plans/<ts>.md`） | `integ/<ts>` の PR |
"""

    def setUp(self):
        self.dir = tempfile.mkdtemp()
        os.environ["LANES_DIR"] = self.dir
        os.environ["LANES_NO_GIT"] = "1"

    def tearDown(self):
        os.environ.pop("LANES_NO_GIT", None)

    def board(self):
        lanes.mutate("status", {"lane": "3", "state": "working", "step": "parallel-fix 113002（3 グループ）", "note": "2/3"})
        lanes.mutate("todo", {"lane": "3", "msg": "! task hosts:setup", "cmd": "task hosts:setup"})
        lanes.mutate("alert", {"lane": "3", "level": "warn", "msg": "claims conflict"})
        lanes.mutate("todo", {"lane": "2", "msg": "not mine"})
        lanes.mutate("msg", {"from": "1", "to": "3", "msg": "[TASK] 3 fix TS=20261031-113002"})
        lanes.mutate("msg", {"from": "1", "to": "3", "msg": "[RUN] 20261031-113002 score=31200"})
        return lanes.read_board()

    def test_render(self):
        b = self.board()
        prs = [{"number": 21, "title": "integ 113002", "headRefName": "integ/113002", "isDraft": False},
               {"number": 22, "title": "setup", "headRefName": "setup/db"},
               {"number": 23, "title": "impl", "headRefName": "impl/ridecache"}]
        wts = [{"path": "/r/wt-integ-113002", "branch": "integ/113002", "dirty": 2, "ahead": 3},
               {"path": "/r/wt-zzz", "branch": "zzz", "dirty": 0, "ahead": 0}]
        md, sl = lanehandoff.render("3", b, lanes.now(), "docs/handoff-lane3.md", claims=(self.CLAIMS, "origin/main"),
                                    prs=prs, wts=wts, lane_text=self.LANE3,
                                    names={"1": "brave-otter-17", "3": "bold-heron-11"})
        self.assertIn("STATE **alert**", md)  # the alert it raised
        self.assertNotIn("自分が書いた経緯", md)  # no notes passed: no section
        notes = [{"t": 1, "kind": "note", "lane": "3", "nkind": "fix", "msg": "N+1 → IN", "ref": "PR #21", "body": ["a", "b"]}]
        md2, _ = lanehandoff.render("3", b, lanes.now(), "docs/handoff-lane3.md", claims=(self.CLAIMS, "origin/main"),
                                    prs=prs, wts=wts, lane_text=self.LANE3, notes=notes)
        self.assertIn("[fix] N+1 → IN（PR #21）\n  - a\n  - b", md2)
        self.assertIn("parallel-fix 113002", md)
        self.assertIn("todo t1: ! task hosts:setup", md)
        self.assertIn("alert a2 [warn]: claims conflict", md)
        self.assertNotIn("not mine", md)
        self.assertIn("未読 2 件", md)
        self.assertIn("最後に受けた `[TASK]`（盤）: `[TASK] 3 fix TS=20261031-113002`", md)
        self.assertIn("`integ/113002`", md)
        self.assertNotIn("setup/db", md.split("## open の PR")[0].split("## claims")[1])  # lane 2's claim
        self.assertNotIn("fix/old", md)  # merged
        prsec = md.split("## open の PR")[1].split("## worktree")[0]
        self.assertIn("#21", prsec)
        self.assertIn("#23", prsec)      # impl/* is a lane 3 branch in plan.json
        self.assertNotIn("#22", prsec)
        self.assertIn("/r/wt-integ-113002`（`integ/113002`）未 commit 2 · origin/main より先 3", md)
        self.assertIn("持ち主が分からない wt-*: `wt-zzz`", md)
        self.assertIn("parallel-fix skill", md.split("## 次にやること")[1])
        self.assertIn("| 1 | brave-otter-17 |", md)
        self.assertIn("レーン 1 の名前は brave-otter-17", sl)
        self.assertIn("docs/handoff-lane3.md", sl)
        self.assertIn("LANE=3 claude --model", md)
        self.assertEqual(len(lanes.unread_msgs(lanes.read_board(), "3")), 2)  # writing the page does not read the inbox

    def test_codex_successor_and_cli(self):
        self.board()
        sj = os.path.join(self.dir, "sessions.json")
        with open(os.path.join(HERE, "sessions.json")) as f:
            cfg = json.load(f)
        cfg["lanes"]["3"]["agent"] = "codex"
        with open(sj, "w") as f:
            json.dump(cfg, f, ensure_ascii=False)
        out = os.path.join(self.dir, "docs", "handoff-lane3.md")
        env = dict(os.environ, LANES_SESSIONS=sj)
        r = subprocess.run([sys.executable, SCRIPT, "handoff", "lane=3", f"out={out}"], capture_output=True, text=True, env=env)
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertIn(f"wrote {out}", r.stdout)
        with open(out) as f:
            md = f.read()
        self.assertIn("agent（sessions.json）: **codex**", md)
        self.assertIn("AGENTS.md（「Codex で動くとき」）", md)
        self.assertIn("LANE=3 codex --model", md)
        self.assertIn("（gh を呼ばなかった: LANES_NO_GH=1）", md)
        self.assertIn("handoff", [e["kind"] for e in events(self.dir)])
        r = subprocess.run([sys.executable, SCRIPT, "handoff", "lane=9"], capture_output=True, text=True, env=env)
        self.assertEqual(r.returncode, 1)

    def test_session_names_from_hooks(self):
        p = os.path.join(self.dir, "cc-events.jsonl")
        with open(p, "w") as f:
            for e in [{"lane": "1", "event": "UserPromptSubmit", "head": "[HELLO] 2 calm-falcon-08"},
                      {"lane": "3", "event": "PreToolUse", "tool": "SendMessage", "to": "brave-otter-17", "head": "[PR] #4 ready"},
                      {"lane": "1", "event": "UserPromptSubmit", "head": "[HELLO] 2 calm-falcon-09"}]:
                f.write(json.dumps(e) + "\n")
            f.write("junk\n")
        self.assertEqual(lanehandoff.session_names(p), {"2": "calm-falcon-09", "1": "brave-otter-17"})


class AgentsMdTest(unittest.TestCase):
    def test_generation_and_stale_check(self):
        root = tempfile.mkdtemp()
        os.makedirs(os.path.join(root, "tools", "codex"))
        os.makedirs(os.path.join(root, "docs"))
        os.makedirs(os.path.join(root, "tools", "kick"))
        os.makedirs(os.path.join(root, ".claude", "skills"))
        with open(os.path.join(root, "tools", "kick", "SKILL.md"), "w") as f:
            f.write("---\nname: kick\ndescription: 最初に読む手順。二文目は要らない。\n---\n")
        os.symlink("../../tools/kick", os.path.join(root, ".claude", "skills", "kickoff"))
        shutil.copy(os.path.join(ROOT, "tools", "codex", "AGENTS.codex.md"), os.path.join(root, "tools", "codex"))
        with open(os.path.join(root, "docs", "NOTES.md"), "w") as f:
            f.write("# notes\nrule 1\n")
        with open(os.path.join(root, "CLAUDE.md"), "w") as f:
            f.write("# CLAUDE\n\n@docs/NOTES.md\n\nend\n")
        env = dict(os.environ, ROOT=root)
        run = lambda *a: subprocess.run([sys.executable, AGENTS_MD, *a], capture_output=True, text=True, env=env)
        self.assertEqual(run("--check").returncode, 1)  # missing
        self.assertEqual(run().returncode, 0)
        with open(os.path.join(root, "AGENTS.md")) as f:
            md = f.read()
        self.assertIn("GENERATED by `task agents:md`", md)
        self.assertIn("<!-- inlined from docs/NOTES.md -->\n# notes\nrule 1", md)  # small: inlined
        self.assertIn("| `kickoff` | `tools/kick/SKILL.md` | 最初に読む手順。 |", md)
        self.assertIn("Codex で動くとき（Claude Code との違い）", md)
        self.assertEqual(run("--check").returncode, 0)
        with open(os.path.join(root, "CLAUDE.md"), "a") as f:
            f.write("- Servers: svc1 = 1.2.3.4\n")  # the kickoff header changed
        self.assertEqual(run("--check").returncode, 1)
        with open(os.path.join(root, "docs", "NOTES.md"), "w") as f:
            f.write("x" * 40000)  # too big for Codex's project doc limit: read-first line instead
        run()
        with open(os.path.join(root, "AGENTS.md")) as f:
            md = f.read()
        self.assertIn("**Read first: `docs/NOTES.md`**", md)
        self.assertNotIn("x" * 100, md)


if __name__ == "__main__":
    unittest.main()
