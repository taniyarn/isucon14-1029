package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// 2026-10-27: after 「ベンチ終わった」 the pipeline creates measurements/<ts>/ (note.json, bench-done.json) right away
// and meta.json only after collect; /api/timeline rows had no req then, and runs:wait logged "req 0" for every run.
func TestTimelineRowBeforeMetaHasAgentTotals(t *testing.T) {
	agent := func(runs []map[string]any) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/runs" {
				json.NewEncoder(w).Encode(runs)
				return
			}
			http.NotFound(w, r)
		}))
	}
	a1 := agent([]map[string]any{{"ts": "20261027-120827", "req_total": 155866, "err_total": 2, "duration_sec": 90, "end_reason": "idle", "git": map[string]any{"commit": "6778806"}}})
	defer a1.Close()
	a3 := agent([]map[string]any{{"ts": "20261027-120829", "req_total": 0, "duration_sec": 87.9, "follow": true, "end_reason": "bench-done"}})
	defer a3.Close()
	data := t.TempDir()
	p := &panel{contest: "c", data: data, notes: map[string]map[string]any{}, pipes: map[string]*benchPipe{},
		hosts: []panelHost{{Name: "svc1", Agent: a1.URL}, {Name: "svc3", Agent: a3.URL}}}
	pending := filepath.Join(p.contestDir("measurements"), "20261027-120827")
	os.MkdirAll(pending, 0755)
	os.WriteFile(filepath.Join(pending, "bench-done.json"), []byte(`{"ts":"20261027-120827"}`), 0644)
	done := filepath.Join(p.contestDir("measurements"), "20261027-120035")
	os.MkdirAll(done, 0755)
	os.WriteFile(filepath.Join(done, "meta.json"), []byte(`{"hosts":[{"host":"svc1","req_total":100670}]}`), 0644)

	rec := httptest.NewRecorder()
	p.handleTimeline(rec, httptest.NewRequest("GET", "/api/timeline", nil))
	var rows []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &rows); err != nil {
		t.Fatal(err)
	}
	got := map[string]map[string]any{}
	for _, r := range rows {
		got[str(r["ts"])] = r
	}
	if r := got["20261027-120827"]; num(r["req"]) != 155866 || num(r["err"]) != 2 || num(r["dur"]) != 90 || r["pending"] != true || r["git"] == nil {
		t.Fatalf("pending row: %v", r)
	}
	if r := got["20261027-120035"]; num(r["req"]) != 100670 || r["pending"] != nil {
		t.Fatalf("collected row: %v", r)
	}
}
