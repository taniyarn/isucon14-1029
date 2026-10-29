package main

import (
	"strings"
	"testing"
)

// Golden ranking from practice run 20261020-161752 (ISUCON13, svc1 = app host, app CPU-bound, 5xx 1.4%): the numbers
// below are that report's shares. Before scoring, reaction (35% of request time, on the score loop) sat at P2 under
// moderate (12%), and the sha256 hot spot (35% of the app's CPU) was not suggested at all.
func TestScoreActionsGolden161752(t *testing.T) {
	acts := []action{
		{priority: "P1", title: "Fix 5xx errors (1.4% of requests)", kind: "errors"},
		{priority: "P1", title: "Speed up POST /api/livestream/:livestream_id/moderate (avg 1124ms, 12% of request time)", kind: "endpoint", share: 12},
		{priority: "P2", title: "Speed up GET /api/livestream/:livestream_id/reaction (avg 154ms, 35% of request time)", kind: "endpoint", share: 35},
		{priority: "P3", title: "Polling: GET /api/user/:username/icon at 316 req/s (median gap 7ms)", kind: "polling"},
	}
	acts = append(acts, hotspotActionsFrom([]hotFunc{
		{name: "crypto/sha256.block", self: 34.7, cum: 34.7, callers: []string{"fillUserResponse"}, callerPct: []float64{34.7}},
		{name: "main.fillReactionResponse", self: 0.5, cum: 41, callers: []string{"getReactionsHandler"}, callerPct: []float64{41}},
		{name: "runtime.mallocgc", self: 12, cum: 15},
		{name: "main.getReactionsHandler", self: 1, cum: 60},
	})...)
	scoreActions(acts, scoreCtx{
		kind: "app-cpu", appBusy: 0.56,
		pathEps: map[string]bool{paramKey("GET /api/livestream/:livestream_id/reaction"): true, paramKey("GET /api/user/:username/icon"): true},
		pathFns: map[string]bool{"getReactionsHandler": true},
	})
	var got []string
	for _, a := range acts {
		got = append(got, a.priority+" "+strings.SplitN(a.title, " (", 2)[0])
	}
	want := []string{
		"P1 Fix 5xx errors",
		"P1 Speed up GET /api/livestream/:livestream_id/reaction",
		"P1 CPU hot spot: main.fillReactionResponse",
		"P1 CPU hot spot: crypto/sha256.block",
		"P1 Speed up POST /api/livestream/:livestream_id/moderate",
		"P3 Polling: GET /api/user/:username/icon at 316 req/s",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("ranking:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if !strings.Contains(acts[1].why, "bottleneck ×1.5") || !strings.Contains(acts[1].why, "score path ×1.5") {
		t.Fatalf("why = %q", acts[1].why)
	}
}

// Baseline 20261020-160320 (svc1 = app + DB, DB CPU-bound): the index on livestream_tags (32% of SQL) stays first,
// and statistics (6 calls, 7% of request time) no longer outranks livecomment (12%, on the score loop).
func TestScoreActionsGolden160320(t *testing.T) {
	acts := []action{
		{priority: "P1", title: "Speed up GET /api/user/:username/statistics (avg 11683ms, 7% of request time)", kind: "endpoint", share: 7},
		{priority: "P2", title: "Speed up GET /api/livestream/:livestream_id/livecomment (avg 616ms, 12% of request time)", kind: "endpoint", share: 12},
		{priority: "P1", title: "Add an index on livestream_tags(livestream_id)", kind: "index", share: 32, sql: "SELECT * FROM livestream_tags WHERE livestream_id = ?"},
		{priority: "P1", title: "Add an index on icons(user_id)", kind: "index", share: 10},
	}
	scoreActions(acts, scoreCtx{kind: "db-cpu", dbBusy: 0.70, pathEps: map[string]bool{paramKey("GET /api/livestream/:livestream_id/livecomment"): true}})
	if !strings.HasPrefix(acts[0].title, "Add an index on livestream_tags") {
		t.Fatalf("first = %s", acts[0].title)
	}
	pos := map[string]int{}
	for i, a := range acts {
		pos[strings.SplitN(a.title, " (", 2)[0]] = i
	}
	if pos["Speed up GET /api/livestream/:livestream_id/livecomment"] > pos["Speed up GET /api/user/:username/statistics"] {
		t.Fatalf("livecomment should rank above statistics: %v", pos)
	}
}

func TestPinnedKeepsBand(t *testing.T) {
	acts := []action{{priority: "P1", pinned: true, title: "Polling: GET /api/x — retry_after_ms = 30 ms", kind: "polling"}}
	scoreActions(acts, scoreCtx{kind: "db-cpu"})
	if acts[0].priority != "P1" {
		t.Fatalf("pinned P1 lost: %s", acts[0].priority)
	}
}

func TestHotFuncsCallers(t *testing.T) {
	// all → main.getReactionsHandler → main.fillUserResponse → crypto/sha256.block (60) ; all → runtime.gcBgMarkWorker (40)
	root := &flameNode{Name: "all", Value: 100}
	h := root.child("main.getReactionsHandler")
	h.Value = 60
	f := h.child("main.fillUserResponse")
	f.Value = 60
	s := f.child("crypto/sha256.block")
	s.Value = 60
	g := root.child("runtime.gcBgMarkWorker")
	g.Value = 40
	acts := hotspotActionsFrom(hotFuncs(root))
	if len(acts) == 0 || !strings.Contains(acts[0].title, "crypto/sha256.block") || acts[0].where != "fillUserResponse()" {
		t.Fatalf("got %+v", acts)
	}
	for _, a := range acts {
		if strings.Contains(a.title, "runtime.") || strings.Contains(a.title, "Handler") || strings.Contains(a.title, "fillUserResponse") {
			t.Fatalf("plumbing / handler / already-named caller suggested: %s", a.title)
		}
	}
}
