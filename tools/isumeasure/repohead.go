package main

import (
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// repohead.go: the report is built from the ops host's clone of the contest repo (code index, alp.yml). A clone
// whose pull silently failed (2026-10-18: an untracked ISUCON13 tree blocked it) builds reports with last
// contest's alp.yml — so the report says which commit it read, and whether the bench's commit is in it.

type repoHead struct {
	root, sha, subject string
	at                 time.Time
	benchCommit        string
	hasBench           int // 1 = the bench's commit is an ancestor of HEAD, 0 = not, -1 = unknown commit (clone older than the bench)
}

func gitOut(root string, args ...string) (string, error) {
	b, err := exec.Command("git", append([]string{"-c", "safe.directory=*", "-C", root}, args...)...).Output()
	return strings.TrimSpace(string(b)), err
}

func readRepoHead(root, benchCommit string) *repoHead {
	if root == "" {
		return nil
	}
	out, err := gitOut(root, "log", "-1", "--format=%h%x09%cI%x09%s")
	if err != nil || out == "" {
		return nil
	}
	f := strings.SplitN(out, "\t", 3)
	if len(f) < 3 {
		return nil
	}
	rh := &repoHead{root: root, sha: f[0], subject: f[2], benchCommit: benchCommit, hasBench: 1}
	rh.at, _ = time.Parse(time.RFC3339, f[1])
	if benchCommit != "" {
		cmd := exec.Command("git", "-c", "safe.directory=*", "-C", root, "merge-base", "--is-ancestor", benchCommit, "HEAD")
		if err := cmd.Run(); err != nil {
			if ee, ok := err.(*exec.ExitError); ok && ee.ExitCode() == 1 {
				rh.hasBench = 0
			} else {
				rh.hasBench = -1
			}
		}
	}
	return rh
}

// line: one sentence for the top of the report (markdown; "" when there is nothing to say).
func (rh *repoHead) line(alpPath, scorePost string) string {
	sp := "score_post: " + ternary(scorePost != "", "`"+scorePost+"`", "**not set** (the panel's score ÷ count and the flows' score loops are off)")
	alp := ternary(alpPath != "", "alp.yml: `"+alpPath+"`", "alp.yml: **none found**")
	if rh == nil {
		return "repo: not a git clone (or -code not set) · " + alp + " · " + sp
	}
	s := fmt.Sprintf("repo `%s` HEAD **%s** %q (%s) · %s · %s", rh.root, rh.sha, trunc(rh.subject, 70), rh.at.Format("01-02 15:04"), alp, sp)
	switch rh.hasBench {
	case 0:
		s += fmt.Sprintf("\n\n> ⚠ the bench ran %s, which is not in this clone's history: the clone is on another branch or stale — on ops: `task ops:repo`, then re-report", rh.benchCommit)
	case -1:
		s += fmt.Sprintf("\n\n> ⚠ the bench ran %s, which this clone does not have: the clone is stale (its pull is failing?) — the code index and alp.yml above may be from an older state or another contest. On ops: `task ops:repo`, then re-report", rh.benchCommit)
	}
	return s
}
