# python3 -m unittest tools/lanes/test_ship.py — hub:ship (ship.sh) end to end on a temp bare repo with fake gh / task,
# and the parallel deploy script (tools/bootstrap/deploy-all.sh) with a stub host deploy. Never touches GitHub or hosts.
import json
import os
import subprocess
import sys
import tempfile
import time
import unittest

HERE = os.path.dirname(os.path.abspath(__file__))
ROOT = os.path.dirname(os.path.dirname(HERE))
sys.path.insert(0, HERE)
import lanes  # noqa: E402

FAKE_GH = r'''#!/usr/bin/env python3
# fake gh: pr merge <n> --merge (merges origin/<head> into origin/main from a work clone), pr view <n> --json …
import json, os, subprocess, sys
st = os.environ["FAKE_GH_DIR"]
prs = json.load(open(os.path.join(st, "prs.json")))
work = os.path.join(st, "work")
def g(*a):
    subprocess.run(["git", "-C", work, *a], check=True, capture_output=True)
a = sys.argv[1:]
if a[:2] == ["pr", "merge"]:
    n = a[2]
    with open(os.path.join(st, "merge-args"), "a") as f:
        f.write(" ".join(a) + "\n")
    if n not in prs:
        print("pull request %s is not mergeable" % n, file=sys.stderr); sys.exit(1)
    g("fetch", "-q", "origin"); g("checkout", "-q", "main"); g("reset", "-q", "--hard", "origin/main")
    g("merge", "-q", "--no-ff", "origin/" + prs[n]["head"], "-m", "Merge pull request #%s" % n)
    g("push", "-q", "origin", "main")
    sha = subprocess.run(["git", "-C", work, "rev-parse", "HEAD"], capture_output=True, text=True).stdout.strip()
    open(os.path.join(st, "merged-" + n), "w").write(sha)
elif a[:2] == ["pr", "diff"]:
    n = a[2]
    if n not in prs:
        sys.exit(1)
    g("fetch", "-q", "origin")
    print(subprocess.run(["git", "-C", work, "diff", "--name-only", "origin/main...origin/" + prs[n]["head"]],
                         capture_output=True, text=True).stdout, end="")
elif a[:2] == ["pr", "view"] and "headRefOid,commits" in a:  # ship.sh head_gate
    p = prs[a[2]]
    g("fetch", "-q", "origin")
    out = subprocess.run(["git", "-C", work, "log", "--format=%H %cI", "origin/main..origin/" + p["head"]],
                         capture_output=True, text=True).stdout.split("\n")
    cs = [{"oid": x.split()[0], "committedDate": x.split()[1]} for x in out if x.strip()]
    print(json.dumps({"headRefOid": cs[0]["oid"] if cs else "", "commits": cs[::-1]}))
elif a[:2] == ["pr", "view"]:
    n = a[2]
    p = prs[n]
    if p.get("race"):  # another lane pushes its claims row to main while the ship records
        g("fetch", "-q", "origin"); g("checkout", "-q", "main"); g("reset", "-q", "--hard", "origin/main")
        with open(os.path.join(work, "docs/claims.md"), "a") as f:
            f.write("| late/row | `x.go`: `f` | logic | lane3 | open | 01:00 |\n")
        g("commit", "-q", "-am", "claims: late/row open"); g("push", "-q", "origin", "main")
        p["race"] = False
        json.dump(prs, open(os.path.join(st, "prs.json"), "w"))
    sha = open(os.path.join(st, "merged-" + n)).read() if os.path.exists(os.path.join(st, "merged-" + n)) else ""
    print(json.dumps({"headRefName": p["head"], "title": p["title"], "body": p["body"], "mergeCommit": {"oid": sha},
                      "files": [{"path": x} for x in p.get("files", [])]}))
else:
    print("fake gh: " + " ".join(a), file=sys.stderr); sys.exit(1)
'''

FAKE_TASK = '''#!/bin/sh
echo "$@" >> "$FAKE_GH_DIR/task.log"
exit 0
'''

CLAIMS = """# claims

## 表

| branch | files / functions | layer | owner (session) | status | updated |
|---|---|---|---|---|---|
| feat/a | `webapp/a.go`: `A` | logic | lane3 | open | 00:40 |
| fix/T/x | `webapp/x.go`: `X` | SQL only | lane3 / subagent x | open | 00:41 |
| feat/old | `webapp/o.go` | logic | lane2 | merged | 00:10 |
| other | `webapp/z.go` | logic | lanec | open | 00:42 |
"""


def sh(cwd, *a):
    r = subprocess.run(list(a), cwd=cwd, capture_output=True, text=True)
    if r.returncode != 0:
        raise AssertionError(f"{a}: {r.stderr}")
    return r.stdout.strip()


class ShipTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.mkdtemp()
        self.st = os.path.join(self.tmp, "state")
        os.makedirs(self.st)
        origin = os.path.join(self.tmp, "origin.git")
        sh(self.tmp, "git", "init", "-q", "--bare", "-b", "main", origin)
        seed = os.path.join(self.tmp, "seed")
        sh(self.tmp, "git", "clone", "-q", origin, seed)
        for k, v in (("user.email", "t@example.com"), ("user.name", "t")):
            sh(seed, "git", "config", k, v)
        os.makedirs(os.path.join(seed, "docs"))
        os.makedirs(os.path.join(seed, "webapp"))
        with open(os.path.join(seed, "docs/journal.md"), "w") as f:
            f.write("# journal\n\n時刻 | branch / PR | 何を | なぜ（report の行） | 結果（score）\n\n00:40 | main | baseline | - | 3238\n")
        with open(os.path.join(seed, "docs/claims.md"), "w") as f:
            f.write(CLAIMS)
        with open(os.path.join(seed, "webapp/main.go"), "w") as f:
            f.write("package main\n")
        sh(seed, "git", "add", "-A")
        sh(seed, "git", "commit", "-q", "-m", "init")
        sh(seed, "git", "push", "-q", "origin", "main")

        def branch(name, path, text, base="main"):
            sh(seed, "git", "checkout", "-q", "-b", name, base)
            os.makedirs(os.path.dirname(os.path.join(seed, path)), exist_ok=True)
            with open(os.path.join(seed, path), "w") as f:
                f.write(text)
            sh(seed, "git", "add", "-A")
            sh(seed, "git", "commit", "-q", "-m", name)
            sh(seed, "git", "push", "-q", "origin", name)
            sh(seed, "git", "checkout", "-q", "main")
        branch("feat/a", "webapp/a.go", "package main // a\n")
        branch("fix/T/x", "webapp/x.go", "package main // x\n")
        sh(seed, "git", "checkout", "-q", "-b", "integ/T", "main")
        sh(seed, "git", "merge", "-q", "--no-ff", "fix/T/x", "-m", "integ")
        sh(seed, "git", "push", "-q", "origin", "integ/T")
        sh(seed, "git", "checkout", "-q", "main")
        branch("docs/b", "docs/plans/b.md", "plan\n")
        branch("other", "webapp/z.go", "package main // z (not shipped)\n")
        # an index PR on code that deadlocks with it (ISUCON14 coupons(code), cut down in test_indexcheck)
        from test_indexcheck import APP_HANDLERS
        sh(seed, "git", "checkout", "-q", "-b", "idx/c", "main")
        os.makedirs(os.path.join(seed, "webapp/go"))
        os.makedirs(os.path.join(seed, "webapp/sql/indexes"))
        with open(os.path.join(seed, "webapp/go/app_handlers.go"), "w") as f:
            f.write(APP_HANDLERS)
        sh(seed, "git", "add", "-A")
        sh(seed, "git", "commit", "-q", "-m", "handlers")
        sh(seed, "git", "push", "-q", "origin", "idx/c:main")  # the handlers are already on main
        with open(os.path.join(seed, "webapp/sql/indexes/c.sql"), "w") as f:
            f.write("ALTER TABLE coupons ADD INDEX idx_coupons_code (code);\n")
        sh(seed, "git", "add", "-A")
        sh(seed, "git", "commit", "-q", "-m", "index coupons(code)")
        sh(seed, "git", "push", "-q", "origin", "idx/c")
        sh(seed, "git", "checkout", "-q", "main")
        sh(self.tmp, "git", "clone", "-q", origin, os.path.join(self.st, "work"))
        for k, v in (("user.email", "w@example.com"), ("user.name", "w")):
            sh(os.path.join(self.st, "work"), "git", "config", k, v)
        self.hub = os.path.join(self.tmp, "hub")
        sh(self.tmp, "git", "clone", "-q", origin, self.hub)
        for k, v in (("user.email", "h@example.com"), ("user.name", "hub")):
            sh(self.hub, "git", "config", k, v)
        prs = {
            "1": {"head": "feat/a", "title": "feat a", "body": "本文\n\njournal 用の 1 行:\n`hh:mm | feat/a #<n> | A を速く | report P1 A 30% | `\n"},
            "3": {"head": "integ/T", "title": "parallel-fix T", "body": "no journal line here", "race": True},
            "5": {"head": "docs/b", "title": "[fast] docs b", "body": "- `hh:mm | docs/b #<n> | plan b | scan | docs のみ`",
                  "files": ["docs/app-map.md"]},
            "7": {"head": "idx/c", "title": "[fast] index coupons(code)", "body": "index for the coupons full scan"},
        }
        with open(os.path.join(self.st, "prs.json"), "w") as f:
            json.dump(prs, f)
        self.bin = os.path.join(self.tmp, "bin")
        os.makedirs(self.bin)
        for name, text in (("gh", FAKE_GH), ("task", FAKE_TASK)):
            with open(os.path.join(self.bin, name), "w") as f:
                f.write(text)
            os.chmod(os.path.join(self.bin, name), 0o755)
        self.board = os.path.join(self.tmp, "board")
        os.makedirs(self.board)
        self.env = dict(os.environ, PATH=self.bin + os.pathsep + os.environ["PATH"], FAKE_GH_DIR=self.st,
                        LANES_DIR=self.board, LANES_NO_GH="1", BENCH_CMD="./bench run", GIT_AUTHOR_NAME="hub",
                        GIT_AUTHOR_EMAIL="h@example.com", GIT_COMMITTER_NAME="hub", GIT_COMMITTER_EMAIL="h@example.com")
        os.environ["LANES_DIR"] = self.board

    def ship(self, prs):
        return subprocess.run(["bash", os.path.join(HERE, "ship.sh")], cwd=self.hub, capture_output=True, text=True,
                              env=dict(self.env, PRS=prs, INIT="1"))

    def origin_file(self, path):
        sh(self.hub, "git", "fetch", "-q", "origin")
        return sh(self.hub, "git", "show", f"origin/main:{path}")

    def test_ship_records_journal_and_claims_then_deploys(self):
        tid = lanes.mutate("todo", {"lane": "1", "msg": "review", "pr": "1 3"})
        lanes.decide(tid, "ok", held=[3])  # #1 OK on the board, #3 held there (OK given in the tab later)
        r = self.ship("1 99 3")
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        self.assertIn("merged: #1 #3  skipped: #99", r.stdout)
        journal = self.origin_file("docs/journal.md").splitlines()
        m1, m3 = (sh(self.st, "cat", "merged-" + n)[:7] for n in ("1", "3"))
        hm = lanes.hm(next(x for x in lanes.read_board()["todos"] if x["id"] == tid)["decided_at"])
        self.assertRegex(journal[-2], r"^\d\d:\d\d \| feat/a #1 \| A を速く \| report P1 A 30%% \| merge %s、OK: 盤 %s$" % (m1, hm))
        self.assertRegex(journal[-1], r"^\d\d:\d\d \| integ/T / #3 \| parallel-fix T \| — \| merge %s、OK: タブ$" % m3)
        claims = self.origin_file("docs/claims.md")
        self.assertRegex(claims, r"\| feat/a \| .* \| merged \| \d\d:\d\d \|")
        self.assertRegex(claims, r"\| fix/T/x \| .* \| merged \| \d\d:\d\d \|")  # the subagent branch of integ/T
        self.assertIn("| other | `webapp/z.go` | logic | lanec | open | 00:42 |", claims)  # not in this ship
        self.assertIn("| feat/old | `webapp/o.go` | logic | lane2 | merged | 00:10 |", claims)
        self.assertIn("| late/row |", claims)  # the other lane's push during the ship survived the rebase
        # one commit per file, journal-only and claims-only
        log = sh(self.hub, "git", "log", "--format=%s", "-n", "3", "origin/main").splitlines()
        self.assertEqual(log[0], "claims: merged feat/a fix/T/x")
        self.assertEqual(log[1], "journal: merged #1 #3")
        for i, f in ((0, "docs/claims.md"), (1, "docs/journal.md")):
            self.assertEqual(sh(self.hub, "git", "show", "--name-only", "--format=", f"origin/main~{i}"), f)
        self.assertEqual(sh(self.hub, "git", "rev-parse", "HEAD"), sh(self.hub, "git", "rev-parse", "origin/main"))
        with open(os.path.join(self.st, "task.log")) as f:
            self.assertEqual(f.read().splitlines(), ["deploy BRANCH=main", "smoke INIT=1"])
        b = lanes.read_board()
        self.assertEqual(b["bench"]["state"], "ready")
        self.assertEqual(b["bench"]["sha"], sh(self.hub, "git", "rev-parse", "--short", "HEAD"))
        bench_todo = b["todos"][-1]
        self.assertEqual((bench_todo["kind"], bench_todo["cmd"]), ("bench", "./bench run"))
        self.assertTrue(any(a["msg"].startswith("hub:ship: PR #99") for a in b["alerts"]))
        # roadmap: integ/T (not integ/<digits>) matches A4's auto globs in plan.json → done "PR #3"; feat/a nothing
        self.assertEqual((b["steps"]["A4"]["state"], b["steps"]["A4"]["note"]), ("done", "PR #3"))
        self.assertIn("roadmap: A4 done (PR #3)", r.stdout)
        with open(os.path.join(self.board, "events.jsonl")) as f:
            ev = [json.loads(x) for x in f]
        steps = [e["name"] for e in ev if e["kind"] == "ship.step"]
        self.assertEqual(steps, ["sync", "merge", "record", "deploy", "smoke", "todo"])
        end = next(e for e in ev if e["kind"] == "ship.end")
        self.assertEqual((end["ok"], end["merged"], end["mode"]), (1, "#1 #3", "deploy"))

        # docs-only ship: still recorded (fast title → OK: fast), no deploy / smoke / bench todo
        lanes.mutate("bench", {"state": "none"})
        r = self.ship("5")
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        self.assertIn("mode: docs-only", r.stdout)
        journal = self.origin_file("docs/journal.md").splitlines()
        self.assertRegex(journal[-1], r"^\d\d:\d\d \| docs/b #5 \| plan b \| scan \| docs のみ、merge \w{7}、OK: fast（事後レビュー）$")
        self.assertEqual(lanes.read_board()["steps"]["A2"]["note"], "PR #5")  # by file (docs/app-map.md), docs-only too
        with open(os.path.join(self.st, "task.log")) as f:
            self.assertEqual(f.read().splitlines()[-1], "ops:repo")
        self.assertEqual(sh(self.hub, "git", "show", "--name-only", "--format=", "origin/main"), "docs/journal.md")

    def test_refuses_with_local_commits_before_merging(self):
        with open(os.path.join(self.hub, "docs/journal.md"), "a") as f:
            f.write("01:00 | hand | x | y | z\n")
        sh(self.hub, "git", "commit", "-q", "-am", "journal by hand")
        r = self.ship("1")
        self.assertEqual(r.returncode, 1)
        self.assertFalse(os.path.exists(os.path.join(self.st, "merged-1")))

    def test_dirty_file_not_touched_by_the_merge_is_left_alone(self):
        with open(os.path.join(self.hub, "webapp/main.go"), "a") as f:  # hand edit; PR #1 only adds webapp/a.go
            f.write("// hub's edit\n")
        r = self.ship("1")
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        self.assertIn("uncommitted changes left alone, not touched by this ship: webapp/main.go", r.stdout)
        self.assertTrue(os.path.exists(os.path.join(self.st, "merged-1")))
        with open(os.path.join(self.hub, "webapp/main.go")) as f:
            self.assertIn("hub's edit", f.read())
        self.assertEqual(sh(self.hub, "git", "rev-parse", "HEAD"), sh(self.hub, "git", "rev-parse", "origin/main"))
        self.assertIn("journal: merged #1", sh(self.hub, "git", "log", "--format=%s", "-n", "2", "origin/main"))

    def test_dirty_file_the_pr_changes_stops_before_merging(self):
        sh(self.hub, "git", "fetch", "-q", "origin")
        sh(self.hub, "git", "checkout", "-q", "origin/feat/a", "--", "webapp/a.go")  # staged copy of the PR's new file
        with open(os.path.join(self.hub, "webapp/a.go"), "a") as f:
            f.write("// local\n")
        r = self.ship("1")
        self.assertEqual(r.returncode, 1, r.stdout + r.stderr)
        self.assertIn("also changed by origin/main or the PRs", r.stdout)
        self.assertFalse(os.path.exists(os.path.join(self.st, "merged-1")))
        self.assertTrue(any("webapp/a.go" in a["msg"] for a in lanes.read_board()["alerts"]))

    def test_dirty_file_changed_on_origin_main_stops(self):
        work = os.path.join(self.st, "work")
        with open(os.path.join(work, "webapp/main.go"), "a") as f:
            f.write("// pushed by a lane\n")
        sh(work, "git", "commit", "-q", "-am", "main.go on main")
        sh(work, "git", "push", "-q", "origin", "main")
        with open(os.path.join(self.hub, "webapp/main.go"), "a") as f:
            f.write("// hub\n")
        r = self.ship("5")
        self.assertEqual(r.returncode, 1, r.stdout + r.stderr)
        self.assertFalse(os.path.exists(os.path.join(self.st, "merged-5")))

    def test_dirty_journal_or_claims_stops(self):
        for path in ("docs/journal.md", "docs/claims.md"):
            with open(os.path.join(self.hub, path), "a") as f:
                f.write("hand edit\n")
            r = self.ship("1")
            self.assertEqual(r.returncode, 1, r.stdout + r.stderr)
            self.assertIn(path, r.stdout)
            self.assertFalse(os.path.exists(os.path.join(self.st, "merged-1")))
            with open(os.path.join(self.hub, path)) as f:
                self.assertTrue(f.read().endswith("hand edit\n"))  # never clobbered
            sh(self.hub, "git", "checkout", "-q", "--", path)

    def test_docs_only_ship_goes_while_a_bench_runs_and_keeps_the_band(self):
        lanes.mutate("bench", {"state": "running", "sha": "abc1234", "prs": "#9"})
        r = self.ship("1")  # code: refused, nothing merged
        self.assertEqual(r.returncode, 1, r.stdout + r.stderr)
        self.assertIn("Code ships wait", r.stdout)
        self.assertFalse(os.path.exists(os.path.join(self.st, "merged-1")))
        r = self.ship("5")  # docs only: no FORCE needed
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        self.assertIn("mode: docs-only", r.stdout)
        b = lanes.read_board()["bench"]
        self.assertEqual((b["state"], b["sha"], b["prs"]), ("running", "abc1234", "#9"))
        with open(os.path.join(self.board, "events.jsonl")) as f:
            self.assertFalse([e for e in map(json.loads, f) if e["kind"] == "bench" and e.get("src") == "ship"])


    def test_index_pr_with_deadlock_risk_is_held_unless_reviewed(self):
        r = self.ship("7 1")
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        self.assertIn("merged: #1  held: #7 (index:check)", r.stdout)
        self.assertIn("[index:check #7] !! coupons(code)", r.stdout)
        self.assertFalse(os.path.exists(os.path.join(self.st, "merged-7")))
        alert = next(a for a in lanes.read_board()["alerts"] if "#7" in a["msg"])
        self.assertIn("coupons(code)", alert["msg"])
        self.assertIn("index-check: reviewed", alert["impact"])
        self.assertIn("appPostUsers", alert["impact"])
        self.assertIn("index:check で保留: #7", lanes.read_board()["todos"][-1]["msg"])
        # the author fixed / judged it and says so in the PR body → it ships
        lanes.mutate("bench", {"state": "none"})
        with open(os.path.join(self.st, "prs.json")) as f:
            prs = json.load(f)
        prs["7"]["body"] += "\n\nindex-check: reviewed\n"
        with open(os.path.join(self.st, "prs.json"), "w") as f:
            json.dump(prs, f)
        r = self.ship("7")
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        self.assertIn("PR body says index-check: reviewed", r.stdout)
        self.assertTrue(os.path.exists(os.path.join(self.st, "merged-7")))

    def test_pr_whose_head_moved_after_the_ok_is_held(self):
        tid = lanes.mutate("todo", {"lane": "1", "msg": "review", "pr": "1"})
        lanes.decide(tid, "ok")
        time.sleep(1.1)  # committer dates have 1 s resolution: the fix lands strictly after the OK
        seed = os.path.join(self.st, "work")
        sh(seed, "git", "fetch", "-q", "origin")
        sh(seed, "git", "checkout", "-q", "-b", "fa", "origin/feat/a")
        with open(os.path.join(seed, "webapp/a.go"), "a") as f:
            f.write("// fix pushed after the OK\n")
        sh(seed, "git", "commit", "-q", "-am", "late fix")
        sh(seed, "git", "push", "-q", "origin", "fa:feat/a")
        r = self.ship("1 5")
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        self.assertIn("merged: #5  held: #1 (head changed after the OK)", r.stdout)
        self.assertFalse(os.path.exists(os.path.join(self.st, "merged-1")))
        self.assertTrue(any(a["msg"].startswith("hub:ship: #1 の head が merge OK") for a in lanes.read_board()["alerts"]))
        # the OK taken again in the tab → REOK lets it through, merged with the checked head
        r = subprocess.run(["bash", os.path.join(HERE, "ship.sh")], cwd=self.hub, capture_output=True, text=True,
                           env=dict(self.env, PRS="1", INIT="1", REOK="1"))
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        self.assertTrue(os.path.exists(os.path.join(self.st, "merged-1")))
        sh(self.hub, "git", "fetch", "-q", "origin")
        head = sh(self.hub, "git", "rev-parse", "origin/feat/a")
        with open(os.path.join(self.st, "merge-args")) as f:
            self.assertIn("pr merge 1 --merge --match-head-commit " + head, f.read())

    def test_head_unchanged_since_the_ok_merges(self):
        tid = lanes.mutate("todo", {"lane": "1", "msg": "review", "pr": "1"})
        time.sleep(1.1)
        lanes.decide(tid, "ok")
        r = self.ship("1")
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        self.assertIn("merged: #1  mode: deploy", r.stdout)

    def test_only_held_prs_fails_without_deploy(self):
        r = self.ship("7")
        self.assertEqual(r.returncode, 1, r.stdout + r.stderr)
        self.assertFalse(os.path.exists(os.path.join(self.st, "task.log")))

    def test_docs_pr_rides_along_with_code_in_one_deploy(self):
        r = self.ship("5 1")
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        self.assertIn("merged: #5 #1  mode: deploy", r.stdout)
        with open(os.path.join(self.st, "task.log")) as f:
            self.assertEqual(f.read().splitlines(), ["deploy BRANCH=main", "smoke INIT=1"])
        self.assertIn("#5 #1", lanes.read_board()["bench"]["prs"])

    def test_mixed_list_while_a_bench_runs_ships_docs_and_queues_code(self):
        lanes.mutate("bench", {"state": "running", "sha": "abc1234", "prs": "#9", "next": "#8"})
        r = self.ship("1 5")
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        self.assertIn("merged: #5  queued for after the bench: #1  mode: docs-only", r.stdout)
        self.assertFalse(os.path.exists(os.path.join(self.st, "merged-1")))
        b = lanes.read_board()["bench"]
        self.assertEqual((b["state"], b["sha"], b["next"]), ("running", "abc1234", "#8 #1"))

    def test_smoke_without_analyzer_config_is_a_minimal_probe(self):
        import http.server
        import threading

        class H(http.server.BaseHTTPRequestHandler):
            code = 200

            def do_GET(self):
                self.send_response(H.code)
                self.end_headers()
                self.wfile.write(b"ok")

            def log_message(self, *a):
                pass
        srv = http.server.HTTPServer(("127.0.0.1", 0), H)
        threading.Thread(target=srv.serve_forever, daemon=True).start()
        port = srv.server_address[1]
        smoke = os.path.join(ROOT, "tools/app-analyzer/smoke.mjs")
        env = dict(os.environ, SMOKE_HOST="127.0.0.1:%d" % port)
        try:
            r = subprocess.run(["node", smoke, os.path.join(self.tmp, "analyzer.config.json"), "--ip", "127.0.0.1", "--initialize"],
                               capture_output=True, text=True, env=env, cwd=self.tmp)
            self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
            self.assertIn("!! minimal smoke:", r.stdout)
            self.assertIn("initialize NOT run", r.stdout)
            self.assertIn("checked 1, failed 0", r.stdout)
            H.code = 502
            r = subprocess.run(["node", smoke, os.path.join(self.tmp, "analyzer.config.json"), "--ip", "127.0.0.1"],
                               capture_output=True, text=True, env=env, cwd=self.tmp)
            self.assertEqual(r.returncode, 1, r.stdout + r.stderr)
        finally:
            srv.shutdown()


class ShipRecTest(unittest.TestCase):
    def test_journal_line_parsing(self):
        import shiprec
        body = "x | y | z | w | v\n\n| table | row | a | b | c |\n\njournal 用の 1 行:\n\n`hh:mm | br #<n> | what | why | `\n"
        self.assertEqual(shiprec.journal_line_in(body), "hh:mm | br #<n> | what | why |")
        self.assertEqual(shiprec.journal_line_in("- 12:30 | br / PR | a | b | c"), "12:30 | br / PR | a | b | c")
        self.assertEqual(shiprec.journal_line_in("no line"), "")
        self.assertEqual(shiprec.build_line(7, "abc1234", "br", "t", body, "01:02", "OK: タブ"),
                         "01:02 | br #7 | what | why | merge abc1234、OK: タブ")
        self.assertEqual(shiprec.build_line(7, "abc1234", "br", "t", "`hh:mm | br | a | b | smoke 55/0`", "01:02", "OK: タブ"),
                         "01:02 | br / #7 | a | b | smoke 55/0、merge abc1234、OK: タブ")


class DeployAllTest(unittest.TestCase):
    def test_first_hosts_in_order_then_the_rest_in_parallel(self):
        tmp = tempfile.mkdtemp()
        stub = os.path.join(tmp, "host-deploy")
        with open(stub, "w") as f:
            f.write('#!/bin/sh\necho "start $1 $2"; echo "$1 $(date +%s)" >> "' + tmp + '/order"\n'
                    'case "$1" in 10.0.0.1) sleep 2;; *) sleep 1;; esac\n'
                    '[ "$1" = "$FAIL_HOST" ] && { echo boom; exit 3; }\necho "end $1"\n')
        os.chmod(stub, 0o755)
        env = dict(os.environ, HOSTS="10.0.0.1 10.0.0.2 10.0.0.3 10.0.0.4", DEPLOY_FIRST="db 2", BRANCH="feat",
                   DEPLOY_HOST_CMD=stub, DEPLOY_SSH="false", LANES_DIR=tmp, FAIL_HOST="none")
        script = os.path.join(ROOT, "tools/bootstrap/deploy-all.sh")
        t0 = time.time()
        r = subprocess.run(["bash", script], capture_output=True, text=True, env=env)
        took = time.time() - t0
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        with open(os.path.join(tmp, "order")) as f:
            order = [x.split()[0] for x in f.read().splitlines()]
        self.assertEqual(order[:2], ["10.0.0.3", "10.0.0.2"])  # DEPLOY_FIRST in its order
        self.assertEqual(sorted(order[2:]), ["10.0.0.1", "10.0.0.4"])
        self.assertLess(took, 5.5)  # sequential would be 1 + 1 + 2 + 1 = 5 s plus overhead; parallel ≈ 4 s
        self.assertIn("[10.0.0.1] start 10.0.0.1 feat", r.stdout)
        self.assertIn("[10.0.0.4] end 10.0.0.4", r.stdout)
        self.assertRegex(r.stdout, r"deploy took \d+s: 10\.0\.0\.3=\d+s 10\.0\.0\.2=\d+s 10\.0\.0\.1=\d+s 10\.0\.0\.4=\d+s")
        self.assertIn("could not read the previous commit on svc1", r.stdout)
        with open(os.path.join(tmp, "events.jsonl")) as f:
            ev = [json.loads(x) for x in f]
        self.assertEqual([(e["host"], e["mode"]) for e in ev if e["kind"] == "deploy.host"],
                         [("10.0.0.3", "first"), ("10.0.0.2", "first"), ("10.0.0.1", "parallel"), ("10.0.0.4", "parallel")])
        self.assertEqual(ev[-1]["kind"], "deploy.end")
        # a parallel host fails: the others finish, exit 1, names the host
        r = subprocess.run(["bash", script], capture_output=True, text=True, env=dict(env, FAIL_HOST="10.0.0.4"))
        self.assertEqual(r.returncode, 1)
        self.assertIn("[10.0.0.1] end 10.0.0.1", r.stdout)
        self.assertIn("!! deploy failed on: 10.0.0.4", r.stdout)
        # a first host fails: nothing else is deployed
        os.remove(os.path.join(tmp, "order"))
        r = subprocess.run(["bash", script], capture_output=True, text=True, env=dict(env, FAIL_HOST="10.0.0.3"))
        self.assertEqual(r.returncode, 1)
        with open(os.path.join(tmp, "order")) as f:
            self.assertEqual([x.split()[0] for x in f.read().splitlines()], ["10.0.0.3"])


if __name__ == "__main__":
    unittest.main()
