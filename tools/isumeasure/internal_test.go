package main

import (
	"testing"
	"time"
)

func TestInternalStats(t *testing.T) {
	t0 := time.Now()
	e := &epStat{key: "GET /api/internal/matching", count: 4, sum: 0.004, rts: []float64{.001, .001, .001, .001}}
	for i := 0; i < 4; i++ {
		e.starts = append(e.starts, t0.Add(time.Duration(i)*500*time.Millisecond))
	}
	byTag := map[string][]*sqlStat{"GET /api/internal/matching": {{norm: "UPDATE rides SET chair_id = ? WHERE id = ?", count: 4}, {norm: "SELECT * FROM chairs", count: 4}}}
	sts := internalStats("svc1", []*epStat{e, {key: "GET /api/app/rides", count: 10}}, byTag, nil)
	if len(sts) != 1 {
		t.Fatalf("want 1 internal endpoint, got %d", len(sts))
	}
	st := sts[0]
	if st.medGap < .49 || st.medGap > .51 || st.writesPerCallSum != 1 {
		t.Errorf("gap %.3f writes/call %.2f", st.medGap, st.writesPerCallSum)
	}
	if a := st.action(); a.kind != "internal" || a.priority != "P2" {
		t.Errorf("action %+v", a)
	}
	if !isInternalEp("POST /api/internal/x") || isInternalEp("GET /api/app/internal") {
		t.Error("isInternalEp")
	}
}
