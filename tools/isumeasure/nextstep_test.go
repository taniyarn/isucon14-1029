package main

import (
	"os"
	"testing"
	"time"
)

func at(hh, mm int) time.Time {
	loc, _ := time.LoadLocation("Asia/Tokyo")
	return time.Date(2026, 11, 1, hh, mm, 0, 0, loc)
}

func TestNextStepRegressionRevert(t *testing.T) {
	cs := computeNextStep(nextInput{ts: "x", t0: at(11, 0), score: 50, scoreOK: true, prevScore: 100, prevScoreOK: true, commit: "b", prevCommit: "a",
		hosts: []nextHost{{name: "svc1", cpuUsed: 80, verdict: "app-cpu"}}})
	if len(cs) == 0 || cs[0].name != "revert して切り分け" {
		t.Fatalf("got %+v", cs)
	}
}

func TestNextStepSameCommitNoise(t *testing.T) {
	cs := computeNextStep(nextInput{ts: "x", t0: at(11, 0), score: 50, scoreOK: true, prevScore: 100, prevScoreOK: true, commit: "a", prevCommit: "a",
		hosts: []nextHost{{name: "svc1", cpuUsed: 80, verdict: "app-cpu"}}})
	if len(cs) == 0 || cs[0].name != "再計測（変更なしでもう 1 回）" {
		t.Fatalf("got %+v", cs)
	}
}

func TestNextStepFinalPhase(t *testing.T) {
	cs := computeNextStep(nextInput{ts: "x", t0: at(16, 40), p1: 5, hosts: []nextHost{{name: "svc1", cpuUsed: 95, verdict: "cpu"}}})
	if len(cs) == 0 || cs[0].name != "失格回避フェーズ（再起動試験・計測 OFF）" {
		t.Fatalf("got %+v", cs)
	}
}

func TestNextStepTechniqueScanWhenDry(t *testing.T) {
	cs := computeNextStep(nextInput{ts: "x", t0: at(13, 0), p1: 0, p2: 1, recentScores: []float64{1000, 1010, 1020},
		hosts: []nextHost{{name: "svc1", cpuUsed: 95, verdict: "app-cpu"}}})
	if len(cs) == 0 || cs[0].name != "technique-scan" {
		t.Fatalf("got %+v", cs)
	}
}

func TestNextStepWaitBoundDeepDive(t *testing.T) {
	cs := computeNextStep(nextInput{ts: "x", t0: at(11, 0), p1: 1, benchKnown: true, fixedBench: false,
		hosts: []nextHost{{name: "svc1", cpuUsed: 30, verdict: "wait"}}})
	if len(cs) == 0 || cs[0].name != "deep-dive" {
		t.Fatalf("got %+v", cs)
	}
}

func TestNextStepPrizeWithinReach(t *testing.T) {
	cs := computeNextStep(nextInput{ts: "x", t0: at(11, 40), p1: 3, score: 42000, scoreOK: true, prizeTarget: 50000,
		hosts: []nextHost{{name: "svc1", cpuUsed: 95, verdict: "app-cpu"}}})
	if len(cs) == 0 || cs[0].skill != "score-sprint" {
		t.Fatalf("got %+v", cs)
	}
	cs = computeNextStep(nextInput{ts: "x", t0: at(11, 40), p1: 3, score: 20000, scoreOK: true, prizeTarget: 50000,
		hosts: []nextHost{{name: "svc1", cpuUsed: 95, verdict: "app-cpu"}}})
	for _, c := range cs {
		if c.skill == "score-sprint" {
			t.Fatalf("40%% of the target must not suggest the sprint: %+v", cs)
		}
	}
}

func TestPrizeTargetFromAlpYml(t *testing.T) {
	p := t.TempDir() + "/alp.yml"
	os.WriteFile(p, []byte("matching_groups:\n  - \"^/api/x$\"\nscore_post: \"POST /api/x\"\nprize_target: 50,000   # manual: special prize\n"), 0644)
	cfg, err := loadGroups(p)
	if err != nil || cfg.prizeTarget != 50000 || cfg.scorePost != "POST /api/x" {
		t.Fatalf("got %+v %v", cfg, err)
	}
}
