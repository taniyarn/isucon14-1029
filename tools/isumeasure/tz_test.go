package main

import (
	"testing"
	"time"
)

// Run names and per-second keys do not depend on the host's TZ (2026-10-28, ISUCON11 AMI with TZ=Asia/Tokyo).
func TestRunNamesUTCWhateverHostTZ(t *testing.T) {
	jstLoc, err := time.LoadLocation("Asia/Tokyo")
	if err != nil {
		t.Skip("no tzdata")
	}
	saved := time.Local
	defer func() { time.Local = saved }()
	at := time.Date(2026, 10, 28, 14, 32, 1, 0, time.UTC) // 23:32:01 JST
	for _, loc := range []*time.Location{jstLoc, time.UTC} {
		time.Local = loc
		local := at.In(time.Local)
		if got := runTS(local); got != "20261028-143201" {
			t.Fatalf("host TZ %s: runTS = %s", loc, got)
		}
		if got := secKey(local); got != "2026-10-28T14:32:01" {
			t.Fatalf("host TZ %s: secKey = %s", loc, got)
		}
	}
}

// The load window is found both for new runs (start and keys in UTC) and for runs recorded before the fix on a JST host
// (start +09:00, zone-less keys in JST): before, the JST keys read as UTC put the window 9 h off ("no system samples").
func TestLoadWindowAnyRunZone(t *testing.T) {
	jstLoc := time.FixedZone("+0900", 9*3600)
	for _, loc := range []*time.Location{time.UTC, jstLoc} {
		t0 := time.Date(2026, 10, 28, 14, 32, 1, 0, time.UTC).In(loc)
		h := &hostRun{Name: "svc1", Start: t0, End: t0.Add(70 * time.Second)}
		for i := 0; i < 70; i++ {
			r := 1000.0
			if i < 5 || i > 64 {
				r = 3
			}
			h.HTTP = append(h.HTTP, map[string]any{"t": t0.Add(time.Duration(i) * time.Second).Format("2006-01-02T15:04:05"), "req": r})
		}
		w := detectLoadWindow(h)
		if w.secs < 55 || w.fromT.Before(t0) || w.toT.After(t0.Add(70*time.Second)) {
			t.Fatalf("zone %s: window %v → %v (%.0f s), want inside the run", loc, w.fromT, w.toT, w.secs)
		}
	}
}

// bench-done without ts: the newest run with requests and no signal yet, by start instant (not by name).
func TestPickRecentRun(t *testing.T) {
	p := &panel{}
	run := func(ts string, start time.Time, req float64, signalled bool) map[string]any {
		g := map[string]any{"ts": ts, "hosts": map[string]any{"svc1": map[string]any{"start": start.Format(time.RFC3339Nano), "req_total": req}}}
		if signalled {
			g["note"] = map[string]any{"bench_done_at": start.Add(time.Minute).Format(time.RFC3339Nano)}
		}
		return g
	}
	base := time.Date(2026, 10, 28, 14, 32, 1, 0, time.UTC)
	// isucon11-1028 baseline: the bench's run, then an empty one 71 s later; the signal must go to the bench's run
	groups := []map[string]any{
		run("20261028-143312", base.Add(71*time.Second), 0, false),
		run("20261028-143201", base, 72842, false),
	}
	if got := p.pickRecentRun(groups, base.Add(95*time.Second)); got != "20261028-143201" {
		t.Fatalf("empty run must be skipped, got %s", got)
	}
	// a run that already got its signal belongs to the previous bench
	groups = []map[string]any{
		run("20261028-144000", base.Add(8*time.Minute), 50000, false),
		run("20261028-143201", base, 72842, true),
	}
	if got := p.pickRecentRun(groups, base.Add(9*time.Minute)); got != "20261028-144000" {
		t.Fatalf("got %s", got)
	}
	// names from a host in JST sort after newer UTC names: the start instant decides
	jstNamed := run("20261028-235206", base.Add(-40*time.Minute), 100185, true)
	utcNamed := run("20261028-150936", base.Add(37*time.Minute+35*time.Second), 344000, false)
	if got := p.pickRecentRun([]map[string]any{jstNamed, utcNamed}, base.Add(39*time.Minute)); got != "20261028-150936" {
		t.Fatalf("got %s", got)
	}
	// nothing in the last 10 min
	if got := p.pickRecentRun([]map[string]any{jstNamed}, base.Add(39*time.Minute)); got != "" {
		t.Fatalf("got %s", got)
	}
}
