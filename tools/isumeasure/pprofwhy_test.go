package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestPprofFailures(t *testing.T) {
	hs := []*hostRun{
		{Name: "svc1", Run: map[string]any{"pprof": map[string]any{"cpu": "error: GET http://x/debug/pprof/profile: http 500: Could not enable CPU profiling: cpu profiling already in use (0.1s)", "heap": "heap.pb.gz"}}},
		{Name: "svc2", Run: map[string]any{"trigger": "POST /api/initialize", "duration_sec": 12.0}},
	}
	fs := pprofFailures(hs)
	if len(fs) != 2 || fs[0].profile != "cpu" || !strings.Contains(fs[0].String(), "only one at a time") || fs[1].profile != "all" {
		t.Errorf("%+v", fs)
	}
}

func TestFetchToFileKeepsBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Could not enable CPU profiling: cpu profiling already in use", 500)
	}))
	defer srv.Close()
	err := fetchToFile(context.Background(), srv.URL, filepath.Join(t.TempDir(), "cpu.pb.gz"), 5e9)
	if err == nil || !strings.Contains(err.Error(), "already in use") {
		t.Errorf("err = %v", err)
	}
}
