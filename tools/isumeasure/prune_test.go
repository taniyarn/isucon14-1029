package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
)

func TestPruneKeepsReports(t *testing.T) {
	data := t.TempDir()
	p := &panel{contest: "c", data: data, notes: map[string]map[string]any{}}
	run := filepath.Join(data, "c", "measurements", "20261020-161752")
	keep := []string{"summary.md", "report.md", "report.html", "report.json", "actions.json", "meta.json", "collect.json", "note.json", "bench.log", "flows.md", "flows.html",
		"svc1/run.json", "svc1/cpu.svg", "svc1/cpu-flame.html", "svc3/explain.txt", "svc3/db-catalog.txt", "svc3/innodb-status.txt", "svc3/rows-end.txt", "svc1/nginx-conf.txt", "svc1/svc-env.txt"}
	raw := []string{"svc1/access.log.gz", "svc3/slow.log.gz", "svc1/app.log", "svc1/nginx-error.log", "svc1/sys.jsonl", "svc1/http.jsonl", "svc1/cpu.pb.gz", "svc3/heap.pb.gz",
		"svc3/processlist.txt", "svc1/goroutine.txt", "export/requests.csv", "export/sql.csv"}
	for _, f := range append(append([]string{}, keep...), raw...) {
		os.MkdirAll(filepath.Dir(filepath.Join(run, f)), 0755)
		os.WriteFile(filepath.Join(run, f), []byte(strings.Repeat("x", 100)), 0644)
	}
	if _, total := rawFiles(run); total != int64(100*len(raw)) {
		t.Fatalf("raw size %d, want %d", total, 100*len(raw))
	}
	if runPruned(run) {
		t.Fatal("pruned before prune")
	}
	// agents: one keeps a copy of the run (ts 3 s off), one does not
	var mu sync.Mutex
	var deleted []string
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "GET" && r.URL.Path == "/runs":
			json.NewEncoder(w).Encode([]map[string]any{{"ts": "20261020-161755"}, {"ts": "20261020-150000"}})
		case r.Method == "DELETE":
			mu.Lock()
			deleted = append(deleted, r.URL.Path)
			mu.Unlock()
		}
	}))
	defer agent.Close()
	p.hosts = []panelHost{{Name: "svc1", Agent: agent.URL}}

	rec := httptest.NewRecorder()
	p.handlePrune(rec, httptest.NewRequest("POST", "/api/prune?ts=20261020-161752,20261020-000000", nil))
	var out map[string]any
	json.Unmarshal(rec.Body.Bytes(), &out)
	for _, f := range keep {
		if _, err := os.Stat(filepath.Join(run, f)); err != nil {
			t.Errorf("kept file deleted: %s", f)
		}
	}
	for _, f := range raw {
		if _, err := os.Stat(filepath.Join(run, f)); err == nil {
			t.Errorf("raw file left: %s", f)
		}
	}
	if _, err := os.Stat(filepath.Join(run, "export")); err == nil {
		t.Error("export dir left")
	}
	if !runPruned(run) {
		t.Error("not marked pruned")
	}
	r1, _ := out["20261020-161752"].(map[string]any)
	if r1 == nil || num(r1["freed_bytes"]) != float64(100*len(raw)) || !strings.HasPrefix(r1["svc1"].(string), "1 ") {
		t.Errorf("result: %v", out)
	}
	if r2, _ := out["20261020-000000"].(map[string]any); r2 == nil || r2["error"] == nil {
		t.Errorf("uncollected run must be refused: %v", out)
	}
	sort.Strings(deleted)
	if len(deleted) != 1 || deleted[0] != "/runs/20261020-161755" {
		t.Errorf("agent deletes: %v", deleted)
	}
	// a pruned run is never re-reported (it would rebuild the report from nothing)
	if err := p.reportProc(run, "", ""); err == nil || !strings.Contains(err.Error(), "ログ削除済み") {
		t.Errorf("reportProc on a pruned run: %v", err)
	}
	if b, _ := os.ReadFile(filepath.Join(run, "report.html")); len(b) != 100 {
		t.Error("report.html changed")
	}
	// runs list: pruned flag and raw size
	var g map[string]any
	for _, x := range p.runs(context.Background()) { // the stub agent still lists its copy: grouped with the ops run
		if x["ts"] == "20261020-161752" {
			g = x
		}
	}
	if g == nil || g["pruned"] != true || g["raw_bytes"].(int64) != 0 {
		t.Errorf("runs: %v", g)
	}
}

func TestPruneRequiresPost(t *testing.T) {
	p := &panel{contest: "c", data: t.TempDir()}
	rec := httptest.NewRecorder()
	p.handlePrune(rec, httptest.NewRequest("GET", "/api/prune?ts=x", nil))
	if rec.Code != 405 {
		t.Errorf("GET: %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	p.handlePrune(rec, httptest.NewRequest("POST", "/api/prune?ts=../x", nil))
	if rec.Code != 400 {
		t.Errorf("bad ts: %d", rec.Code)
	}
}
