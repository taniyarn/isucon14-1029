package main

import (
	"testing"
	"time"
)

func TestFmtGapKeepsMillisecondPrecision(t *testing.T) {
	cases := map[float64]string{0: "<1 ms", 0.0004: "<1 ms", 0.001: "1 ms", 0.0137: "14 ms", 0.85: "850 ms", 1.234: "1.23 s"}
	for in, want := range cases {
		if got := fmtGap(in); got != want {
			t.Errorf("fmtGap(%v) = %q, want %q", in, got, want)
		}
	}
}

// The access log carries msec (1 ms); a bench that sends the next request 3 ms after the response must show 3 ms,
// not 0.00 s, and requests that overlap within one session are counted as parallel, not as a 0 gap only.
func TestSessionGapsUseMsecAndCountOverlap(t *testing.T) {
	t0 := time.Unix(1792513072, 0)
	at := func(ms int) time.Time { return t0.Add(time.Duration(ms) * time.Millisecond) }
	evs := []accessEvent{
		// uid=1: A ends at 100 ms, B starts at 103 ms (reqtime 0.010 → ends 113), A again starts 116 ms
		{at: at(100), method: "GET", uri: "/a", reqtime: 0.050, sess: "uid=1"},
		{at: at(113), method: "GET", uri: "/b", reqtime: 0.010, sess: "uid=1"},
		{at: at(126), method: "GET", uri: "/a", reqtime: 0.010, sess: "uid=1"},
		// uid=2: A and B in parallel (B starts before A ends)
		{at: at(200), method: "GET", uri: "/a", reqtime: 0.050, sess: "uid=2"},
		{at: at(190), method: "GET", uri: "/b", reqtime: 0.030, sess: "uid=2"},
	}
	sa := analyzeSessions(evs, newNormalizer(nil, "", nil), t0, 10)
	by := map[string]*transStat{}
	for _, tr := range sa.transitions {
		by[tr.from+"→"+tr.to] = tr
	}
	ab := by["GET /a→GET /b"]
	if ab == nil || ab.n != 2 {
		t.Fatalf("a→b transitions = %+v, want 2", ab)
	}
	if ab.overlap != 1 {
		t.Errorf("a→b overlap = %d, want 1 (uid=2 ran them in parallel)", ab.overlap)
	}
	if g := ab.gaps[len(ab.gaps)-1]; g < 0.0025 || g > 0.0035 {
		t.Errorf("a→b largest gap = %v s, want ≈ 0.003 (3 ms from msec timestamps)", g)
	}
	if got := fmtGap(ab.gaps[len(ab.gaps)-1]); got != "3 ms" {
		t.Errorf("shown as %q, want 3 ms", got)
	}
}
