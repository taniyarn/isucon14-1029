package main

import (
	"testing"
	"time"
)

// 2026-10-27 run 20261027-120035: svc3 (DB host) logged one stray request (an internet scanner on its nginx), so its
// own load window was 1 s and the verdict "idle 100%" while mysqld ran at ~190%. Low-HTTP hosts take the busiest
// host's window.
func TestFleetLoadWindowsBorrowsBusiest(t *testing.T) {
	t0 := time.Date(2026, 10, 27, 12, 0, 35, 0, time.UTC)
	sec := func(i int) string { return t0.Add(time.Duration(i) * time.Second).Format("2006-01-02T15:04:05") }
	app := &hostRun{Name: "svc1", Start: t0, End: t0.Add(76 * time.Second)}
	for i := 0; i < 76; i++ {
		r := 1500.0
		if i < 10 || i > 67 { // initialize / tail
			r = 5
		}
		app.HTTP = append(app.HTTP, map[string]any{"t": sec(i), "req": r})
	}
	db := &hostRun{Name: "svc3", Start: t0.Add(2 * time.Second), End: t0.Add(78 * time.Second),
		HTTP: []map[string]any{{"t": sec(76), "req": 1.0}}} // one stray request near the end
	quiet := &hostRun{Name: "svc2", Start: t0.Add(2 * time.Second), End: t0.Add(78 * time.Second)} // no HTTP at all
	ws := fleetLoadWindows([]*hostRun{app, db, quiet})
	if ws[0].from != "" || ws[0].secs != 58 {
		t.Fatalf("busiest host keeps its own window: %+v", ws[0])
	}
	for i, w := range ws[1:] {
		if w.from != "svc1" || !w.fromT.Equal(ws[0].fromT) || !w.toT.Equal(ws[0].toT) || w.secs != 58 {
			t.Fatalf("host %d must borrow svc1's window, got %+v", i+1, w)
		}
		if w.avgRps >= 1 { // diagnose's DB-host rule needs avgRps < 1
			t.Fatalf("host %d avgRps %.2f", i+1, w.avgRps)
		}
	}
	// a second app host with real traffic (≥ 10% of the busiest) keeps its own window
	app2 := &hostRun{Name: "svc2", Start: t0, End: t0.Add(76 * time.Second)}
	for i := 20; i < 50; i++ {
		app2.HTTP = append(app2.HTTP, map[string]any{"t": sec(i), "req": 800.0})
	}
	ws = fleetLoadWindows([]*hostRun{app, app2})
	if ws[1].from != "" || ws[1].secs != 30 {
		t.Fatalf("busy second app host keeps its own window: %+v", ws[1])
	}
}
