package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBenchLogISUCON13(t *testing.T) {
	dir := t.TempDir()
	out := `2026-10-26T16:28:54.684Z	info	staff-logger	bench/bench.go:329	DNSAttacker並列数: 13
2026-10-26T16:28:54.684Z	info	staff-logger	bench/bench.go:330	名前解決成功数: 36007
2026-10-26T16:28:54.684Z	info	staff-logger	bench/bench.go:331	名前解決失敗数: 31
2026-10-26T16:28:54.684Z	info	staff-logger	bench/bench.go:335	スコア: 127667
{"pass":true,"score":127667,"messages":[],"language":"golang","resolved_count":36007}
`
	p := filepath.Join(dir, "bench.log")
	os.WriteFile(p, []byte(out), 0644)
	bs := readBenchLog(p, nil)
	if bs.score != 127667 || bs.pass != "true" || bs.dnsOK != 36007 || bs.dnsFail != 31 {
		t.Fatalf("got score=%d pass=%q ok=%d fail=%d", bs.score, bs.pass, bs.dnsOK, bs.dnsFail)
	}
	if md := bs.markdown(); !strings.Contains(md, "名前解決 成功 36007 · 失敗 31") {
		t.Fatalf("markdown lacks the DNS line:\n%s", md)
	}
	// スコア alone (no JSON line) is enough
	os.WriteFile(p, []byte("2026-10-26T16:28:54Z info x スコア: 1,234\n"), 0644)
	if bs := readBenchLog(p, nil); bs.score != 1234 {
		t.Fatalf("スコア line: got %d", bs.score)
	}
}

func TestEndDiag(t *testing.T) {
	end := time.Date(2026, 10, 26, 16, 25, 19, 0, time.UTC)
	run := map[string]any{"end_reason": "idle", "trigger": "POST /api/initialize", "idle_sec": 12.0, "ended_at": end.Format(time.RFC3339Nano)}
	if got := endDiag(run, end.Add(35*time.Second)); got != "end: idle after 12 s idle; signal +35 s" {
		t.Fatalf("got %q", got)
	}
	run = map[string]any{"end_reason": "idle", "trigger": "mysql:qps>=100", "idle_sec": 10.0, "db_stop_qps": 400.0, "db_peak_qps": 8000.0, "end": end.Format(time.RFC3339)}
	if got := endDiag(run, time.Time{}); got != "end: idle after 10 s without DB load (< 400 q/s, peak 8000)" {
		t.Fatalf("got %q", got)
	}
	run = map[string]any{"end_reason": "bench-done", "idle_sec": 2.0, "ended_at": end.Format(time.RFC3339Nano)}
	if got := endDiag(run, end); got != "end: bench-done (last request 2 s before); signal +0 s" {
		t.Fatalf("got %q", got)
	}
	if got := endDiag(map[string]any{}, time.Time{}); got != "end: running" {
		t.Fatalf("got %q", got)
	}
}
