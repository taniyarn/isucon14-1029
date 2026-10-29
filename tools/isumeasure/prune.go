package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// prune.go: manual deletion of a collected run's raw data (the ops host has ~1 GB of disk and one run with
// long_query_time=0 can be 200 MB). Nothing here runs by itself: the human picks runs in the panel (or
// `task run:prune TS=…`). The reports stay.
//
// Keep-list (everything in the run directory except the raw files below):
//   - top level: summary.md, report.md, report.html, report.json, actions.json, meta.json, collect.json, note.json,
//     bench.log, flows.md/html — every top-level file is kept (all small, all the panel, timeline, retro and
//     issues read)
//   - per host: run.json, *.svg, *-flame.html (the report links them), and the small text snapshots
//     (explain.txt, db-catalog.txt, innodb-status*.txt, rows-*.txt, nginx-conf.txt, svc-env.txt)
// Deleted (raw): <run>/export/ (CSV), and per host *.log / *.log.gz (access, slow, app, nginx/mysql error logs),
// *.jsonl (1 s series), *.pb.gz (pprof), processlist.txt, goroutine.txt.
// After a prune the run is marked with pruned.json; re-report / re-collect are refused (they would rebuild the
// report from nothing) and run:pull / deepdive say the raw logs are gone.

const prunedMarker = "pruned.json"

// isRawPath: rel is relative to the run directory (slash separated).
func isRawPath(rel string) bool {
	parts := strings.Split(filepath.ToSlash(rel), "/")
	if parts[0] == "export" {
		return true
	}
	if len(parts) < 2 {
		return false // top-level files: reports, meta, notes
	}
	name := parts[len(parts)-1]
	switch {
	case strings.HasSuffix(name, ".log"), strings.HasSuffix(name, ".log.gz"), strings.HasSuffix(name, ".jsonl"),
		strings.HasSuffix(name, ".jsonl.gz"), strings.HasSuffix(name, ".pb.gz"), strings.HasSuffix(name, ".csv"):
		return true
	case name == "processlist.txt", name == "goroutine.txt":
		return true
	}
	return false
}

// rawFiles lists the raw files of a run directory and their total size.
func rawFiles(dir string) ([]string, int64) {
	var files []string
	var total int64
	filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(dir, path)
		if isRawPath(rel) {
			files = append(files, path)
			total += info.Size()
		}
		return nil
	})
	return files, total
}

// runPruned: marked by a prune, or an old automatic prune that removed the host directories entirely.
func runPruned(dir string) bool {
	if _, err := os.Stat(filepath.Join(dir, prunedMarker)); err == nil {
		return true
	}
	if _, err := os.Stat(filepath.Join(dir, "report.html")); err != nil {
		return false
	}
	m, _ := filepath.Glob(filepath.Join(dir, "*", "run.json"))
	m2, _ := filepath.Glob(filepath.Join(dir, "run.json"))
	return len(m)+len(m2) == 0
}

// pruneRunDir deletes the raw files of one collected run and writes pruned.json. Reports are never touched.
func pruneRunDir(dir string) (int, int64, error) {
	files, total := rawFiles(dir)
	n := 0
	var firstErr error
	for _, f := range files {
		if err := os.Remove(f); err == nil {
			n++
		} else if firstErr == nil {
			firstErr = err
		}
	}
	os.RemoveAll(filepath.Join(dir, "export")) // empty dirs left by the CSVs
	prev := map[string]any{}
	if b, err := os.ReadFile(filepath.Join(dir, prunedMarker)); err == nil {
		json.Unmarshal(b, &prev)
	}
	freed := total + int64(num(prev["freed_bytes"]))
	writeJSON(filepath.Join(dir, prunedMarker), map[string]any{"pruned_at": time.Now().Format(time.RFC3339), "freed_bytes": freed, "files": n + int(num(prev["files"]))})
	return n, total, firstErr
}

// handlePrune: POST /api/prune?ts=a,b (or a JSON body {"ts":["a","b"]}) deletes the raw data of those collected
// runs on ops and asks every agent to drop its copy (like /api/delete; the agents only keep raw data). Runs that
// were never collected are refused: their only copy is on the hosts.
func (p *panel) handlePrune(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "POST", 405)
		return
	}
	var tss []string
	for _, t := range strings.Split(r.URL.Query().Get("ts"), ",") {
		if t = strings.TrimSpace(t); t != "" {
			tss = append(tss, t)
		}
	}
	if len(tss) == 0 {
		var in struct {
			TS []string `json:"ts"`
		}
		if b, _ := io.ReadAll(io.LimitReader(r.Body, 1<<16)); len(b) > 0 {
			json.Unmarshal(b, &in)
		}
		tss = in.TS
	}
	if len(tss) == 0 {
		http.Error(w, "ts required (?ts=a,b or {\"ts\":[…]})", 400)
		return
	}
	for _, ts := range tss {
		if strings.ContainsAny(ts, "/\\.") {
			http.Error(w, "bad ts "+ts, 400)
			return
		}
	}
	// never delete under a report that is reading the files
	reportMu.Lock()
	defer reportMu.Unlock()
	out := map[string]any{}
	var freed int64
	for _, ts := range tss {
		dir := filepath.Join(p.contestDir("measurements"), ts)
		res := map[string]any{}
		out[ts] = res
		if _, err := os.Stat(filepath.Join(dir, "report.html")); err != nil {
			res["error"] = "not collected / no report on ops: nothing deleted (use delete to drop a run entirely)"
			continue
		}
		n, b, err := pruneRunDir(dir)
		freed += b
		res["files"], res["freed_bytes"] = n, b
		if err != nil {
			res["error"] = err.Error()
		}
		for _, h := range p.hosts {
			var rs []map[string]any
			if err := getJSON(r.Context(), h.Agent+"/runs", &rs, 5*time.Second); err != nil {
				res[h.Name] = "agent unreachable"
				continue
			}
			k := 0
			for _, run := range rs {
				if t, _ := run["ts"].(string); t != "" && within(ts, t, 20*time.Second) {
					req, _ := http.NewRequestWithContext(r.Context(), "DELETE", h.Agent+"/runs/"+t, nil)
					if resp, err := http.DefaultClient.Do(req); err == nil {
						resp.Body.Close()
						k++
					}
				}
			}
			res[h.Name] = fmt.Sprintf("%d copy(ies) deleted on host", k)
		}
	}
	out["freed_mb"] = fmt.Sprintf("%.1f", float64(freed)/1e6)
	writeJSONResp(w, out)
}
