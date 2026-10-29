# python3 -m unittest tools/lanes/test_push.py — task push / task pr:create (push.sh) on a temp bare repo, fake gh.
import os
import subprocess
import tempfile
import unittest

HERE = os.path.dirname(os.path.abspath(__file__))
PUSH = os.path.join(HERE, "push.sh")


def sh(cwd, *a):
    r = subprocess.run(list(a), cwd=cwd, capture_output=True, text=True)
    if r.returncode != 0:
        raise AssertionError(f"{a}: {r.stderr}")
    return r.stdout.strip()


class PushTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.mkdtemp()
        self.origin = os.path.join(self.tmp, "origin.git")
        sh(self.tmp, "git", "init", "-q", "--bare", "-b", "main", self.origin)
        self.repo = os.path.join(self.tmp, "repo")
        sh(self.tmp, "git", "clone", "-q", self.origin, self.repo)
        for k, v in (("user.email", "t@example.com"), ("user.name", "t")):
            sh(self.repo, "git", "config", k, v)
        os.makedirs(os.path.join(self.repo, "docs"))
        for p in ("docs/claims.md", "docs/journal.md", "a.go"):
            with open(os.path.join(self.repo, p), "w") as f:
                f.write("x\n")
        sh(self.repo, "git", "add", "-A")
        sh(self.repo, "git", "commit", "-q", "-m", "init")
        sh(self.repo, "git", "push", "-q", "origin", "main")
        bin_ = os.path.join(self.tmp, "bin")
        os.makedirs(bin_)
        with open(os.path.join(bin_, "gh"), "w") as f:
            f.write('#!/bin/sh\necho "gh $* (in $PWD)" > "%s/gh.log"\necho https://github.com/x/y/pull/9\n' % self.tmp)
        os.chmod(os.path.join(bin_, "gh"), 0o755)
        self.env = dict(os.environ, PATH=bin_ + os.pathsep + os.environ["PATH"], USER_WORKING_DIR=self.repo)

    def run_push(self, mode="push", **kw):
        return subprocess.run(["bash", PUSH, mode], cwd=self.repo, capture_output=True, text=True, env=dict(self.env, **kw))

    def remote(self, ref):
        r = subprocess.run(["git", "--git-dir", self.origin, "rev-parse", "-q", "--verify", ref], capture_output=True, text=True)
        return r.stdout.strip()

    def commit(self, wt, path, msg):
        with open(os.path.join(wt, path), "a") as f:
            f.write(msg + "\n")
        sh(wt, "git", "commit", "-q", "-am", msg)
        return sh(wt, "git", "rev-parse", "HEAD")

    def test_branch_worktree_push_sets_upstream_then_plain_push(self):
        sh(self.repo, "git", "worktree", "add", "-q", "../wt-x", "-b", "fix/x", "origin/main")  # tracks origin/main
        wt = os.path.join(self.tmp, "wt-x")
        head = self.commit(wt, "a.go", "change")
        r = self.run_push(DIR="../wt-x")
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        self.assertIn("pushed fix/x", r.stdout)
        self.assertIn("upstream set", r.stdout)
        self.assertEqual(self.remote("refs/heads/fix/x"), head)
        self.assertEqual(sh(wt, "git", "rev-parse", "--abbrev-ref", "@{u}"), "origin/fix/x")
        self.assertNotEqual(self.remote("refs/heads/main"), head)
        head = self.commit(wt, "a.go", "again")
        r = self.run_push(DIR=wt)  # absolute DIR too
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        self.assertNotIn("upstream set", r.stdout)
        self.assertEqual(self.remote("refs/heads/fix/x"), head)

    def test_rejected_non_fast_forward_is_never_forced(self):
        sh(self.repo, "git", "worktree", "add", "-q", "../wt-y", "-b", "fix/y", "origin/main")
        wt = os.path.join(self.tmp, "wt-y")
        self.commit(wt, "a.go", "one")
        self.assertEqual(self.run_push(DIR=wt).returncode, 0)
        sh(wt, "git", "reset", "-q", "--hard", "HEAD~1")
        self.commit(wt, "a.go", "rewritten")
        r = self.run_push(DIR=wt)
        self.assertEqual(r.returncode, 1, r.stdout + r.stderr)
        self.assertIn("never --force", r.stdout)
        r = self.run_push(DIR=wt, ARGS="--force")
        self.assertEqual(r.returncode, 1)
        self.assertIn("no force push", r.stdout)

    def test_detached_claims_row_goes_to_main_code_does_not(self):
        sh(self.repo, "git", "worktree", "add", "-q", "--detach", "../wt-claims", "origin/main")
        wt = os.path.join(self.tmp, "wt-claims")
        # someone else pushes a journal line meanwhile: the push rebases once
        sh(self.repo, "git", "pull", "-q")
        self.commit(self.repo, "docs/journal.md", "hub line")
        sh(self.repo, "git", "push", "-q", "origin", "main")
        self.commit(wt, "docs/claims.md", "| fix/x | a.go | logic | lane3 | open | 01:00 |")
        r = self.run_push(DIR=wt)
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        self.assertIn("pushed to main: docs/claims.md", r.stdout)
        log = subprocess.run(["git", "--git-dir", self.origin, "log", "--format=%s", "-n", "2", "main"], capture_output=True, text=True).stdout.split("\n")
        self.assertEqual(log[:2], ["| fix/x | a.go | logic | lane3 | open | 01:00 |", "hub line"])
        # code on main: refused, nothing pushed
        before = self.remote("refs/heads/main")
        self.commit(wt, "a.go", "code on main")
        r = self.run_push(DIR=wt)
        self.assertEqual(r.returncode, 1, r.stdout + r.stderr)
        self.assertIn("refusing to push to main", r.stdout)
        self.assertEqual(self.remote("refs/heads/main"), before)

    def test_pr_create_from_the_worktree(self):
        sh(self.repo, "git", "worktree", "add", "-q", "../wt-z", "-b", "feat/z", "origin/main")
        wt = os.path.join(self.tmp, "wt-z")
        self.commit(wt, "a.go", "z")
        r = self.run_push("pr", DIR="../wt-z")
        self.assertEqual(r.returncode, 1)
        self.assertIn("task push DIR=../wt-z first", r.stdout)
        self.assertEqual(self.run_push(DIR="../wt-z").returncode, 0)
        with open(os.path.join(self.tmp, "body.md"), "w") as f:
            f.write("body\n")
        r = self.run_push("pr", DIR="../wt-z", TITLE="[fast] z", BODY_FILE="../body.md")
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        with open(os.path.join(self.tmp, "gh.log")) as f:
            log = f.read()
        self.assertIn("gh pr create --base main --head feat/z --title [fast] z --body-file %s/../body.md" % self.repo, log)
        self.assertRegex(log, r"\(in .*/wt-z\)")  # gh runs inside the worktree
        self.run_push("pr", DIR="../wt-z")
        with open(os.path.join(self.tmp, "gh.log")) as f:
            self.assertIn("--fill", f.read())

    def gh_view_fails(self):
        with open(os.path.join(self.tmp, "bin", "gh"), "w") as f:
            f.write('#!/bin/sh\necho "gh $*" >> "%s/gh.log"\n[ "$2" = view ] && exit 1\n'
                    'echo https://github.com/x/y/pull/9\n' % self.tmp)

    def test_pr_submit_commits_pushes_and_creates_in_one_call(self):
        self.gh_view_fails()
        sh(self.repo, "git", "worktree", "add", "-q", "../wt-s", "-b", "fix/s", "origin/main")
        wt = os.path.join(self.tmp, "wt-s")
        with open(os.path.join(wt, "a.go"), "a") as f:
            f.write("s\n")
        r = self.run_push("submit", DIR="../wt-s", TITLE="t", BODY="10:00 | fix/s | x | y | -")
        self.assertEqual(r.returncode, 1)
        self.assertIn("MSG=", r.stdout)  # dirty tree without a commit message
        self.assertEqual(sh(wt, "git", "diff", "--cached", "--name-only"), "")  # nothing staged behind the user's back
        r = self.run_push("submit", DIR="../wt-s", TITLE="[fast] s", BODY="10:00 | fix/s | x | y | -", MSG="Fix s")
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        self.assertIn("PR #9 https://github.com/x/y/pull/9", r.stdout)
        self.assertEqual(self.remote("refs/heads/fix/s"), sh(wt, "git", "rev-parse", "HEAD"))
        self.assertEqual(sh(wt, "git", "log", "-1", "--format=%s"), "Fix s")
        with open(os.path.join(self.tmp, "gh.log")) as f:
            self.assertIn("pr create --base main --head fix/s --title [fast] s --body 10:00 | fix/s", f.read())
        r = self.run_push("submit", DIR="../wt-s", TITLE="t")
        self.assertEqual(r.returncode, 1)
        self.assertIn("BODY", r.stdout)
        sh(self.repo, "git", "worktree", "add", "-q", "--detach", "../wt-d", "origin/main")
        r = self.run_push("submit", DIR="../wt-d", TITLE="t", BODY="b")
        self.assertEqual(r.returncode, 1)
        self.assertIn("needs a branch", r.stdout)

    def test_pr_submit_prefixes_fast_for_docs_or_new_index_files_only(self):
        self.gh_view_fails()
        for name, path, title, want in [("d", "docs/x.md", "docs: x", "[fast] docs: x"),
                                        ("i", "webapp/sql/indexes/1.sql", "idx", "[fast] idx"),
                                        ("g", "webapp/go/a.go", "fix", "fix"),
                                        ("f", "docs/y.md", "[fast] y", "[fast] y")]:
            sh(self.repo, "git", "worktree", "add", "-q", "../wt-" + name, "-b", "p/" + name, "origin/main")
            wt = os.path.join(self.tmp, "wt-" + name)
            os.makedirs(os.path.dirname(os.path.join(wt, path)), exist_ok=True)
            with open(os.path.join(wt, path), "a") as f:
                f.write(name + "\n")
            sh(wt, "git", "add", "-A")
            sh(wt, "git", "commit", "-q", "-m", name)
            open(os.path.join(self.tmp, "gh.log"), "w").close()
            r = self.run_push("submit", DIR="../wt-" + name, TITLE=title, BODY="b")
            self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
            with open(os.path.join(self.tmp, "gh.log")) as f:
                self.assertIn("--title " + want + " --body", f.read())

    def test_pr_submit_reuses_the_open_pr(self):
        sh(self.repo, "git", "worktree", "add", "-q", "../wt-o", "-b", "fix/o", "origin/main")
        wt = os.path.join(self.tmp, "wt-o")
        self.commit(wt, "a.go", "o")
        r = self.run_push("submit", DIR="../wt-o", TITLE="t", BODY="b")  # fake gh: pr view prints a url
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        self.assertIn("already open", r.stdout)

    def test_hub_log_appends_commits_one_file_and_pushes(self):
        with open(os.path.join(self.repo, "a.go"), "a") as f:
            f.write("dirty\n")  # the hub's other dirty file stays out of the commit
        other = os.path.join(self.tmp, "other")
        sh(self.tmp, "git", "clone", "-q", self.origin, other)
        for k, v in (("user.email", "t@example.com"), ("user.name", "t")):
            sh(other, "git", "config", k, v)
        self.commit(other, "docs/claims.md", "| row |")
        sh(other, "git", "push", "-q", "origin", "main")  # main moved: pull --ff-only first
        r = self.run_push("log", LINE="10:00 | #3 | x | y | +5%")
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        self.assertEqual(self.remote("refs/heads/main"), sh(self.repo, "git", "rev-parse", "HEAD"))
        self.assertEqual(sh(self.repo, "git", "show", "--name-only", "--format=%s", "HEAD").split("\n")[0],
                         "journal: 10:00 | #3 | x | y | +5%")
        self.assertEqual(sh(self.repo, "git", "show", "--name-only", "--format=", "HEAD"), "docs/journal.md")
        with open(os.path.join(self.repo, "docs/journal.md")) as f:
            self.assertTrue(f.read().endswith("x\n10:00 | #3 | x | y | +5%\n"))
        self.assertIn("a.go", sh(self.repo, "git", "status", "--porcelain"))
        r = self.run_push("log", LINE="| fix/x | a.go | logic | lane3 | open | 10:00 |", FILE="claims")
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        self.assertIn("pushed docs/claims.md", r.stdout)
        sh(self.repo, "git", "switch", "-q", "-c", "b")
        self.assertEqual(self.run_push("log", LINE="x").returncode, 1)  # main only


if __name__ == "__main__":
    unittest.main()
