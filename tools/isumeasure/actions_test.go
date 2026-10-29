package main

import "testing"

func TestDedupeActions(t *testing.T) {
	idle := action{priority: "P2", title: "CPU is idle: find what the bench waits for", kind: "wait", group: "ops"}
	idx1 := action{priority: "P1", title: "Add an index on rides(chair_id, created_at)", kind: "index", group: "indexes", sql: "SELECT a"}
	idx2 := idx1
	idx2.sql = "SELECT b"
	lists := [][]action{{idle, idx1}, {idle, idx2}, {idle}}
	out := dedupeActions(lists, []string{"svc1", "svc2", "svc3"})
	n := 0
	for _, l := range out {
		n += len(l)
	}
	if n != 2 {
		t.Fatalf("want 2 actions after dedupe, got %d: %+v", n, out)
	}
	if got := out[0][0].also; len(got) != 2 || got[0] != "svc2" || got[1] != "svc3" {
		t.Errorf("also hosts = %v", got)
	}
}
