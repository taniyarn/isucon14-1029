# python3 -m unittest tools/lanes/test_hubprs.py — task hub:prs (hubprs.py) with a fake gh; never touches GitHub.
import json
import os
import subprocess
import sys
import tempfile
import unittest

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, HERE)
os.environ["LANES_NO_GH"] = "1"
import lanes  # noqa: E402

FAKE_GH = r'''#!/usr/bin/env python3
import json, os, sys
a = sys.argv[1:]
if a[:2] == ["api", "user"]:
    print("hubuser")
elif a[:2] == ["pr", "list"]:
    print(open(os.environ["FAKE_PRS"]).read())
else:
    sys.exit(1)
'''

CLAIMS = """## 表

| branch | files / functions | layer | owner (session) | status | updated |
|---|---|---|---|---|---|
| fix/lane3 | webapp/go/main.go: getTrend | logic | lane 3 | open | 10:00 |
| fix/old | webapp/go/isu.go: x | logic | lane 3 | merged | 09:00 |
"""


class HubPrsTest(unittest.TestCase):
    def setUp(self):
        self.dir = tempfile.mkdtemp()
        os.environ["LANES_DIR"] = self.dir
        gh = os.path.join(self.dir, "gh")
        with open(gh, "w") as f:
            f.write(FAKE_GH)
        os.chmod(gh, 0o755)
        self.prs = os.path.join(self.dir, "prs.json")
        claims = os.path.join(self.dir, "claims.md")
        with open(claims, "w") as f:
            f.write(CLAIMS)
        self.env = dict(os.environ, GH_BIN=gh, FAKE_PRS=self.prs, HUBPRS_CLAIMS=claims, LANES_DIR=self.dir)

    def write_prs(self, rows):
        with open(self.prs, "w") as f:
            json.dump([{"number": n, "title": t, "author": {"login": a}, "headRefName": br,
                        "files": [{"path": x} for x in fs]} for n, t, a, br, fs in rows], f)

    def run_it(self):
        return subprocess.run([sys.executable, os.path.join(HERE, "hubprs.py")], capture_output=True, text=True,
                              env=self.env)

    def reviews(self):
        return [x for x in lanes.read_board()["todos"] if x.get("kind") == "review"]

    def test_human_pr_once_lane_pr_never(self):
        # lane 3's PR #5 was announced with SendMessage (cc-events) and has a claims row; #7 is a teammate's
        with open(os.path.join(self.dir, "cc-events.jsonl"), "w") as f:
            f.write(json.dumps({"event": "PreToolUse", "tool": "SendMessage", "lane": "3", "head": "[PR] #5 ready integ"}) + "\n")
        self.write_prs([(5, "integ", "hubuser", "fix/lane3", ["webapp/go/main.go"]),
                        (7, "trend cache", "shota3506", "shota/trend", ["webapp/go/main.go", "etc/nginx/x.conf"])])
        r = self.run_it()
        self.assertEqual(r.returncode, 0, r.stderr)
        rv = self.reviews()
        self.assertEqual([x["pr"] for x in rv], [7])
        self.assertIn("shota3506", rv[0]["msg"])
        self.assertIn("claims 無し", rv[0]["msg"])
        self.assertIn("#5", rv[0]["msg"])          # same file as the open PR #5
        self.assertIn("fix/lane3", rv[0]["msg"])   # and as the open claims row
        self.assertEqual(rv[0]["pr_titles"], {"7": "trend cache"})
        self.assertIn("#5 lane PR", r.stdout)
        # second run: nothing new
        r = self.run_it()
        self.assertEqual(len(self.reviews()), 1)
        self.assertIn("new review todos 0", r.stdout)

    def test_unannounced_own_pr_and_existing_todo(self):
        # the hub's own login but no [PR] / claims row → offered; a PR that already has a review todo → not again
        lanes.mutate("todo", {"lane": "1", "kind": "review", "pr": "9", "msg": "lane review"})
        self.write_prs([(8, "by hand", "hubuser", "hand/x", ["docs/a.md"]),
                        (9, "lane", "hubuser", "fix/y", ["docs/b.md"])])
        r = self.run_it()
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertEqual(sorted(x["pr"] for x in self.reviews()), [8, 9])
        self.assertNotIn("同じファイル", next(x for x in self.reviews() if x["pr"] == 8)["msg"])

    def test_board_msg_counts_as_announced(self):
        lanes.mutate("msg", {"from": "2", "to": "1", "msg": "[PR] #11 ready tune"})
        self.write_prs([(11, "tune", "hubuser", "tune/a", ["etc/mysql/a.cnf"])])
        self.run_it()
        self.assertEqual(self.reviews(), [])

    def test_op_once(self):
        self.assertTrue(lanes.mutate("hubpr", {"pr": "3", "msg": "m"}).startswith("t"))
        self.assertEqual(lanes.mutate("hubpr", {"pr": "3", "msg": "m"}), "already")


if __name__ == "__main__":
    unittest.main()
