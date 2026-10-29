package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"html"
	"io"
	"log"
	"math"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// panel: the shared control panel. Talks to each host's agent through the tunnel container
// (http://tunnel:1910N -> host N 127.0.0.1:19100). Stores collected runs under data/<contest>/measurements/<ts>/<host>/.

type panelHost struct {
	Name  string `json:"name"`
	Addr  string `json:"addr"`  // ssh target (for display)
	Agent string `json:"agent"` // http base of the agent (via tunnel)
}

type panel struct {
	contest string
	codeDir string
	hosts   []panelHost
	data    string
	mu      sync.Mutex
	notes   map[string]map[string]any // ts -> {score, note, branch...} persisted in data/<contest>/runs.json
	pipes   map[string]*benchPipe     // ts -> auto collect → report started by the "bench finished" signal (benchdone.go)
}

func runPanel(args []string) error {
	fs := flag.NewFlagSet("panel", flag.ContinueOnError)
	envFile := fs.String("env", envOr("ISUCON_ENV", "/opt/isucon-ops/isucon.env"), "isucon.env with SVC1..SVC9, CONTEST")
	data := fs.String("data", envOr("OPS_DATA", "/opt/isucon-ops/data"), "data dir")
	listen := fs.String("listen", envOr("PANEL_LISTEN", ":8080"), "listen address")
	agentBase := fs.String("agent-base", envOr("AGENT_BASE", "http://tunnel:1910"), "agent base URL; host index N is appended (-> http://tunnel:1910N)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	env := envSummary(*envFile)
	p := &panel{contest: env["CONTEST"], data: *data, notes: map[string]map[string]any{}, pipes: map[string]*benchPipe{}, codeDir: envOr("CODE_DIR", "")}
	if p.contest == "" {
		p.contest = "practice"
	}
	for i := 1; i <= 9; i++ {
		if ip := env["SVC"+strconv.Itoa(i)]; ip != "" {
			p.hosts = append(p.hosts, panelHost{Name: "svc" + strconv.Itoa(i), Addr: ip, Agent: *agentBase + strconv.Itoa(i)})
		}
	}
	if len(p.hosts) == 0 {
		log.Printf("no SVC1..SVC9 in %s; panel starts empty", *envFile)
	}
	os.MkdirAll(p.contestDir("measurements"), 0755)
	p.loadNotes()
	mux := http.NewServeMux()
	mux.HandleFunc("/", p.handleIndex)
	mux.HandleFunc("/api/fleet", p.handleFleet)
	mux.HandleFunc("/api/runs", p.handleRuns)
	mux.HandleFunc("/api/collect", p.handleCollect)
	mux.HandleFunc("/api/delete", p.handleDelete) // POST ?ts= : drop a run everywhere (ops dir + every host's copy within 20 s + its note)
	mux.HandleFunc("/api/prune", p.handlePrune)   // POST ?ts=a,b : delete the raw logs of collected runs (reports stay; prune.go)
	mux.HandleFunc("/api/note", p.handleNote)
	mux.HandleFunc("/api/benchlog", p.handleBenchLog) // GET ?ts= → text; POST ?ts= (body = the portal's messages / contestant log) → <run>/bench.log + re-report
	mux.HandleFunc("/api/report", p.handleReport)
	mux.HandleFunc("/api/bench-done", p.handleBenchDone) // POST [?ts=&score=] [body = bench output]: end the run on every host now, then collect → report → score in the background
	mux.HandleFunc("/api/timeline", p.handleTimeline)
	mux.HandleFunc("/api/retro", p.handleRetro)
	mux.HandleFunc("/api/pause", p.handlePause) // POST ?minutes=N (0 = resume) → every agent
	mux.HandleFunc("/api/agent/", p.handleAgentProxy)
	mux.HandleFunc("/app/", handleAppReport)    // REPO_DIR/docs/reports (app-analysis latest.html), read-only; ?ref=<branch> = origin/<branch> of the clone
	mux.HandleFunc("/api/app", handleAppStatus) // app-analysis report on main (crawl time, commit) + unmerged branches carrying a newer one
	mux.Handle("/data/", http.StripPrefix("/data/", http.FileServer(http.Dir(p.data))))
	log.Printf("isumeasure panel %s contest=%s hosts=%d data=%s listen=%s", version, p.contest, len(p.hosts), p.data, *listen)
	go p.coordinate()
	return http.ListenAndServe(*listen, mux)
}

func (p *panel) contestDir(sub string) string { return filepath.Join(p.data, p.contest, sub) }

func (p *panel) loadNotes() {
	b, err := os.ReadFile(p.contestDir("runs.json"))
	if err == nil {
		json.Unmarshal(b, &p.notes)
	}
}

func (p *panel) saveNotes() { writeJSON(p.contestDir("runs.json"), p.notes) }

func getJSON(ctx context.Context, url string, v any, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return &httpErr{code: res.StatusCode}
	}
	return json.NewDecoder(res.Body).Decode(v)
}

// fleet: status of every host (parallel)
func (p *panel) fleet(ctx context.Context, full bool) []map[string]any {
	out := make([]map[string]any, len(p.hosts))
	var wg sync.WaitGroup
	for i, h := range p.hosts {
		wg.Add(1)
		go func(i int, h panelHost) {
			defer wg.Done()
			var st map[string]any
			url := h.Agent + "/status"
			if full {
				url += "?full=1"
			}
			err := getJSON(ctx, url, &st, 4*time.Second)
			if err != nil {
				st = map[string]any{"error": err.Error()}
			}
			st["name"] = h.Name
			st["addr"] = h.Addr
			out[i] = st
		}(i, h)
	}
	wg.Wait()
	return out
}

func (p *panel) handleFleet(w http.ResponseWriter, r *http.Request) {
	writeJSONResp(w, map[string]any{"contest": p.contest, "time": time.Now().Format(time.RFC3339), "ops_disk_free_gb": round2(readSys().diskFreeGB), "hosts": p.fleet(r.Context(), r.URL.Query().Get("full") == "1")})
}

// runs: merge per-host run lists by ts (a bench produces one run per host within a few seconds)
func (p *panel) runs(ctx context.Context) []map[string]any {
	type hr struct {
		host string
		runs []map[string]any
	}
	results := make([]hr, len(p.hosts))
	var wg sync.WaitGroup
	for i, h := range p.hosts {
		wg.Add(1)
		go func(i int, h panelHost) {
			defer wg.Done()
			var rs []map[string]any
			getJSON(ctx, h.Agent+"/runs", &rs, 5*time.Second)
			results[i] = hr{h.Name, rs}
		}(i, h)
	}
	wg.Wait()
	// group: key = ts truncated to the minute + sequence; simpler: exact ts match first, else nearest within 20s
	groups := map[string]map[string]any{}
	keys := []string{}
	for _, r := range results {
		for _, run := range r.runs {
			ts, _ := run["ts"].(string)
			if ts == "" {
				continue
			}
			key := ""
			for _, k := range keys {
				if within(k, ts, 20*time.Second) {
					key = k
					break
				}
			}
			if key == "" {
				key = ts
				keys = append(keys, key)
				groups[key] = map[string]any{"ts": key, "hosts": map[string]any{}}
			}
			groups[key]["hosts"].(map[string]any)[r.host] = run
		}
	}
	// runs already collected on ops (and usually deleted on the hosts) must stay visible: add them from meta.json
	collected := p.collectedSet()
	for k := range collected {
		dup := ""
		for _, key := range keys {
			if within(k, key, 20*time.Second) {
				dup = key
				break
			}
		}
		if dup != "" {
			if dup != k { // host key differs slightly from the ops dir name: use the ops name so links resolve
				groups[k] = groups[dup]
				groups[k]["ts"] = k
				delete(groups, dup)
				for i := range keys {
					if keys[i] == dup {
						keys[i] = k
					}
				}
			}
			continue
		}
		g := map[string]any{"ts": k, "hosts": map[string]any{}, "ops_only": true}
		if b, err := os.ReadFile(filepath.Join(p.contestDir("measurements"), k, "meta.json")); err == nil {
			var meta map[string]any
			if json.Unmarshal(b, &meta) == nil {
				if hs, ok := meta["hosts"].([]any); ok {
					for _, x := range hs {
						hm, _ := x.(map[string]any)
						name, _ := hm["host"].(string)
						hr := map[string]any{"req_total": hm["req_total"], "err_total": hm["err_total"], "duration_sec": hm["duration_sec"], "git": hm["git"], "end_reason": "collected", "pprof": hm["pprof"], "trigger": hm["trigger"], "follow": hm["follow"], "start": hm["start"], "end": hm["end"]}
						if er := str(hm["end_reason"]); er != "" {
							hr["end_reason"], hr["collected"] = er, true
						}
						for _, k := range endDiagKeys {
							if v, ok := hm[k]; ok {
								hr[k] = v
							}
						}
						g["hosts"].(map[string]any)[name] = hr
					}
				}
			}
		}
		groups[k] = g
		keys = append(keys, k)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(keys)))
	var out []map[string]any
	p.mu.Lock()
	for _, k := range keys {
		g := groups[k]
		g["collected"] = collected[k]
		if _, err := os.Stat(filepath.Join(p.contestDir("measurements"), k, "report.html")); err == nil {
			g["report"] = true
		}
		if collected[k] {
			rd := filepath.Join(p.contestDir("measurements"), k)
			_, g["raw_bytes"] = rawFiles(rd)
			g["pruned"] = runPruned(rd)
		}
		if b, err := os.ReadFile(filepath.Join(p.contestDir("measurements"), k, "report-error.txt")); err == nil {
			g["report_error"] = strings.SplitN(string(b), "\n", 2)[0]
		}
		var signal time.Time
		if n := p.notes[k]; n != nil {
			g["note"] = n
			signal = parseAnyTime(n["bench_done_at"])
		}
		if hs, ok := g["hosts"].(map[string]any); ok {
			for _, x := range hs {
				if hr, ok := x.(map[string]any); ok && str(hr["end_reason"]) != "" && str(hr["end_reason"]) != "collected" {
					hr["end_diag"] = endDiag(hr, signal)
				}
			}
		}
		if st := p.pipeStateLocked(k); st != "" {
			g["auto"] = st
		}
		out = append(out, g)
	}
	p.mu.Unlock()
	return out
}

func within(a, b string, d time.Duration) bool {
	ta, e1 := time.Parse("20060102-150405", a)
	tb, e2 := time.Parse("20060102-150405", b)
	if e1 != nil || e2 != nil {
		return a == b
	}
	diff := ta.Sub(tb)
	if diff < 0 {
		diff = -diff
	}
	return diff <= d
}

func (p *panel) collectedSet() map[string]bool {
	m := map[string]bool{}
	entries, _ := os.ReadDir(p.contestDir("measurements"))
	for _, e := range entries {
		if e.IsDir() {
			m[e.Name()] = true
		}
	}
	return m
}

func (p *panel) handleRuns(w http.ResponseWriter, r *http.Request) {
	writeJSONResp(w, p.runs(r.Context()))
}

// collect: download every file of run <ts> from every host into measurements/<ts>/<host>/
func (p *panel) handleCollect(w http.ResponseWriter, r *http.Request) {
	ts := r.URL.Query().Get("ts")
	if ts == "" || strings.Contains(ts, "..") {
		http.Error(w, "ts required", 400)
		return
	}
	result := p.collectRun(r.Context(), ts)
	score, note := p.noteScoreNote(ts)
	if err := p.reportProc(filepath.Join(p.contestDir("measurements"), ts), score, note); err == nil {
		result["report"] = "ok"
	} else {
		result["report"] = err.Error()
	}
	writeJSONResp(w, result)
}

// noteScoreNote: the score / note typed in the panel (or filled from the bench output) for the report's header.
func (p *panel) noteScoreNote(ts string) (string, string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	n := p.notes[ts]
	if n == nil {
		return "", ""
	}
	score, note := fmt.Sprint(n["score"]), fmt.Sprint(n["note"])
	if score == "<nil>" {
		score = ""
	}
	if note == "<nil>" {
		note = ""
	}
	return score, note
}

// collectRun downloads run <ts> from every host (and deletes it there), writes collect.json and meta.json.
func (p *panel) collectRun(ctx context.Context, ts string) map[string]any {
	result := map[string]any{"ts": ts}
	var deletes [][2]string // host name, agent URL: deleted only after meta.json is written (the timeline reads the
	// agents' run list until then; deleting first made /api/timeline show req 0 for a collected-but-not-meta run)
	for _, h := range p.hosts {
		var rs []map[string]any
		if err := getJSON(ctx, h.Agent+"/runs", &rs, 5*time.Second); err != nil {
			result[h.Name] = "agent unreachable: " + err.Error()
			continue
		}
		// several runs may sit within 20 s of ts on one host (own run + a shadow): prefer the non-shadow one, then the busiest
		var match map[string]any
		score := func(r map[string]any) float64 {
			f, _ := r["follow"].(bool)
			base := 1e9
			if f {
				base = 0
			}
			return base + num(r["req_total"])
		}
		for _, run := range rs {
			if t, _ := run["ts"].(string); t != "" && within(ts, t, 20*time.Second) {
				if match == nil || score(run) > score(match) {
					match = run
				}
			}
		}
		if match == nil {
			result[h.Name] = "no run"
			continue
		}
		hostTS, _ := match["ts"].(string)
		dst := filepath.Join(p.contestDir("measurements"), ts, h.Name)
		os.MkdirAll(dst, 0755)
		files, _ := match["files"].([]any)
		n, bytes := 0, int64(0)
		for _, f := range files {
			name, _ := f.(string)
			if name == "" {
				continue
			}
			ctx2, cancel := context.WithTimeout(ctx, 120*time.Second)
			req, _ := http.NewRequestWithContext(ctx2, "GET", h.Agent+"/runs/"+hostTS+"/"+name, nil)
			res, err := http.DefaultClient.Do(req)
			if err == nil && res.StatusCode == 200 {
				if err := writeFileFromReader(filepath.Join(dst, name), res.Body); err == nil {
					n++
					bytes += fileSize(filepath.Join(dst, name))
				}
				res.Body.Close()
			} else if err == nil {
				res.Body.Close()
			}
			cancel()
		}
		result[h.Name] = fmt.Sprintf("%d files, %.1f MB", n, float64(bytes)/1e6)
		if n > 0 && n == len(files) && !keepOnHost() {
			deletes = append(deletes, [2]string{h.Name, h.Agent + "/runs/" + hostTS})
		}
	}
	// meta.json survives raw-data pruning: per-host run.json essentials for the timeline / retrospective
	var metaHosts []map[string]any
	for _, h := range p.hosts {
		if b, err := os.ReadFile(filepath.Join(p.contestDir("measurements"), ts, h.Name, "run.json")); err == nil {
			var ri map[string]any
			if json.Unmarshal(b, &ri) == nil {
				mh := map[string]any{"host": h.Name, "req_total": ri["req_total"], "err_total": ri["err_total"], "duration_sec": ri["duration_sec"], "start": ri["start"], "end": ri["end"], "git": ri["git"], "services": ri["services"], "end_reason": ri["end_reason"], "pprof": ri["pprof"], "trigger": ri["trigger"], "follow": ri["follow"]}
				for _, k := range endDiagKeys { // end-detection diagnostics (agent ≥ 0.3.2)
					if v, ok := ri[k]; ok {
						mh[k] = v
					}
				}
				metaHosts = append(metaHosts, mh)
			}
		}
	}
	meta := map[string]any{"ts": ts, "contest": p.contest, "hosts": metaHosts}
	if bd := readBenchDone(filepath.Join(p.contestDir("measurements"), ts)); bd != nil {
		meta["bench_done"] = bd
	}
	writeJSON(filepath.Join(p.contestDir("measurements"), ts, "meta.json"), meta)
	for _, d := range deletes {
		req, _ := http.NewRequestWithContext(ctx, "DELETE", d[1], nil)
		if res, err := http.DefaultClient.Do(req); err == nil {
			res.Body.Close()
			result[d[0]] = result[d[0]].(string) + " (deleted on host)"
		}
	}
	p.pruneRaw()
	writeJSON(filepath.Join(p.contestDir("measurements"), ts, "collect.json"), result)
	return result
}

// liveRunTotals: req / err / duration / git of run ts summed over the hosts' agents (one run per host within 20 s,
// the non-shadow busiest one, as collectRun picks it). For timeline rows whose meta.json is not written yet: the
// bench-done pipeline creates measurements/<ts>/ at the signal, a minute before collect writes meta.json, and a row
// without req made runs:wait report "req 0" for every bench-done run (2026-10-27).
func (p *panel) liveRunTotals(ctx context.Context, tss []string) map[string]map[string]any {
	out := map[string]map[string]any{}
	if len(tss) == 0 {
		return out
	}
	lists := make([][]map[string]any, len(p.hosts))
	var wg sync.WaitGroup
	for i, h := range p.hosts {
		wg.Add(1)
		go func(i int, h panelHost) {
			defer wg.Done()
			getJSON(ctx, h.Agent+"/runs", &lists[i], 3*time.Second)
		}(i, h)
	}
	wg.Wait()
	for _, ts := range tss {
		tot := map[string]any{}
		req, errs, dur, found := 0.0, 0.0, 0.0, false
		for i, h := range p.hosts {
			var best map[string]any
			score := func(r map[string]any) float64 {
				if f, _ := r["follow"].(bool); f {
					return num(r["req_total"])
				}
				return 1e9 + num(r["req_total"])
			}
			for _, r := range lists[i] {
				if t := str(r["ts"]); t != "" && within(ts, t, 20*time.Second) && (best == nil || score(r) > score(best)) {
					best = r
				}
			}
			if best == nil { // already deleted on the host: the downloaded run.json
				if b, err := os.ReadFile(filepath.Join(p.contestDir("measurements"), ts, h.Name, "run.json")); err == nil {
					json.Unmarshal(b, &best)
				}
			}
			if best == nil {
				continue
			}
			found = true
			req += num(best["req_total"])
			errs += num(best["err_total"])
			dur = math.Max(dur, num(best["duration_sec"]))
			if g, ok := best["git"].(map[string]any); ok && tot["git"] == nil {
				tot["git"] = g
			}
		}
		if found {
			tot["req"], tot["err"], tot["dur"] = req, errs, dur
			out[ts] = tot
		}
	}
	return out
}

var reportMu sync.Mutex

// reportProc builds report.md/html for a run in a child process (`isumeasure report`), one at a time.
// A report that needs more memory than the ops host has (huge slow logs: long_query_time=0 on a busy DB host)
// then kills the child, not the panel: Fleet, collect and the other runs keep working, and the Runs list shows
// the error from report-error.txt. REPORT_MEM_MB (default 1024) caps the child (GOMEMLIMIT at 3/4 of it).
func (p *panel) reportProc(dir, score, note string) error {
	return p.reportProcWait(dir, score, note, false)
}

// reportProcWait: wait=true queues behind a running report (the automatic pipeline) instead of failing.
func (p *panel) reportProcWait(dir, score, note string, wait bool) error {
	if runPruned(dir) {
		return fmt.Errorf("raw logs of this run were deleted (ログ削除済み): the report is kept as it is and cannot be rebuilt")
	}
	if wait {
		reportMu.Lock()
	} else if !reportMu.TryLock() {
		return fmt.Errorf("another report is still running — wait for it (one at a time)")
	}
	defer reportMu.Unlock()
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	args := []string{"report", "-out", dir}
	if p.codeDir != "" {
		args = append(args, "-code", p.codeDir)
	}
	// endpoint groups: alp.yml from the contest repo (task ops:repo); data/<contest>/alp-groups.txt is the legacy fallback
	if g := filepath.Join(envOr("REPO_DIR", "/opt/isucon-ops/repo"), "alp.yml"); fileSize(g) > 0 {
		args = append(args, "-groups", g)
	} else if g := filepath.Join(p.data, p.contest, "alp-groups.txt"); fileSize(g) > 0 {
		args = append(args, "-groups", g)
	}
	if score != "" {
		args = append(args, "-score", score)
	}
	if note != "" {
		args = append(args, "-note", note)
	}
	args = append(args, dir)
	mb, _ := strconv.Atoi(envOr("REPORT_MEM_MB", "1024"))
	if mb < 256 {
		mb = 256
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe, args...)
	cmd.Env = append(os.Environ(), fmt.Sprintf("GOMEMLIMIT=%dMiB", mb*3/4), fmt.Sprintf("ISUMEASURE_MEM_CAP_MB=%d", mb))
	t0 := time.Now()
	out, err := cmd.CombinedOutput()
	errPath := filepath.Join(dir, "report-error.txt")
	if err != nil {
		msg := err.Error()
		if strings.Contains(msg, "signal: killed") {
			msg = fmt.Sprintf("report killed after %.0fs (out of memory on the ops host? raise REPORT_MEM_MB / the instance size, or trim the run's logs)", time.Since(t0).Seconds())
		} else if ctx.Err() != nil {
			msg = "report timed out after 15 min"
		}
		tail := string(out)
		if len(tail) > 2000 {
			tail = tail[len(tail)-2000:]
		}
		os.WriteFile(errPath, []byte(msg+"\n\n"+tail), 0644)
		log.Printf("report %s failed: %s\n%s", filepath.Base(dir), msg, tail)
		if lines := strings.Split(strings.TrimSpace(tail), "\n"); !strings.Contains(msg, "killed") && ctx.Err() == nil && len(lines) > 0 && lines[len(lines)-1] != "" {
			msg = lines[len(lines)-1] // the child's last line says what went wrong (e.g. "report: memory over 1024 MB …")
		}
		return fmt.Errorf("%s", msg)
	}
	os.Remove(errPath)
	log.Printf("report %s built in %.1fs", filepath.Base(dir), time.Since(t0).Seconds())
	return nil
}

// report: build report.md/html for a collected run (re-runs on demand, e.g. after a score/note edit)
func (p *panel) handleReport(w http.ResponseWriter, r *http.Request) {
	ts := r.URL.Query().Get("ts")
	dir := filepath.Join(p.contestDir("measurements"), ts)
	if ts == "" || strings.Contains(ts, "..") {
		http.Error(w, "ts required", 400)
		return
	}
	if _, err := os.Stat(dir); err != nil {
		http.Error(w, "not collected yet", 404)
		return
	}
	score, note := p.noteScoreNote(ts)
	if err := p.reportProc(dir, score, note); err != nil {
		code := 500
		if strings.Contains(err.Error(), "another report") || runPruned(dir) {
			code = 409
		}
		http.Error(w, err.Error(), code)
		return
	}
	writeJSONResp(w, map[string]any{"ts": ts, "report": "/data/" + p.contest + "/measurements/" + ts + "/report.html"})
}

// timeline: one row per collected run (score/note from runs.json, git/req from meta.json, verdict from summary.md)
func (p *panel) handleTimeline(w http.ResponseWriter, r *http.Request) {
	entries, _ := os.ReadDir(p.contestDir("measurements"))
	var noMeta []string // runs being collected (bench-done pipeline): totals from the agents until meta.json exists
	for _, e := range entries {
		if _, err := os.Stat(filepath.Join(p.contestDir("measurements"), e.Name(), "meta.json")); e.IsDir() && err != nil {
			noMeta = append(noMeta, e.Name())
		}
	}
	live := p.liveRunTotals(r.Context(), noMeta)
	p.mu.Lock()
	defer p.mu.Unlock()
	var rows []map[string]any
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		base := filepath.Join(p.contestDir("measurements"), e.Name())
		row := map[string]any{"ts": e.Name()}
		if lt := live[e.Name()]; lt != nil {
			for k, v := range lt {
				row[k] = v
			}
			row["pending"] = true // not collected yet: req / err / dur are the agents' numbers
		}
		if b, err := os.ReadFile(filepath.Join(base, "meta.json")); err == nil {
			var meta map[string]any
			json.Unmarshal(b, &meta)
			req, errs, dur := 0.0, 0.0, 0.0
			if hs, ok := meta["hosts"].([]any); ok {
				for _, x := range hs {
					hm, _ := x.(map[string]any)
					req += num(hm["req_total"])
					errs += num(hm["err_total"])
					if d := num(hm["duration_sec"]); d > dur {
						dur = d
					}
					if g, ok := hm["git"].(map[string]any); ok && row["git"] == nil {
						row["git"] = g
					}
				}
			}
			row["req"], row["err"], row["dur"] = req, errs, dur
			if hs, ok := meta["hosts"].([]any); ok { // end diagnostics of the busiest non-shadow host
				var best map[string]any
				for _, x := range hs {
					hm, _ := x.(map[string]any)
					if f, _ := hm["follow"].(bool); f || hm == nil {
						continue
					}
					if best == nil || num(hm["req_total"]) > num(best["req_total"]) {
						best = hm
					}
				}
				if best != nil && str(best["end_reason"]) != "" {
					var sig time.Time
					if bd, ok := meta["bench_done"].(map[string]any); ok {
						sig = parseAnyTime(bd["at"])
					} else if n := p.notes[e.Name()]; n != nil {
						sig = parseAnyTime(n["bench_done_at"])
					}
					row["end"] = str(best["host"]) + " " + endDiag(best, sig)
				}
			}
		}
		if b, err := os.ReadFile(filepath.Join(base, "summary.md")); err == nil {
			if m := verdictRe.FindStringSubmatch(string(b)); m != nil {
				row["verdict"] = m[1]
			}
		}
		if b, err := os.ReadFile(filepath.Join(base, "report.json")); err == nil { // written by the report: score_post count, bench shape
			var m map[string]any
			if json.Unmarshal(b, &m) == nil {
				if ns, ok := m["next_step"].([]any); ok && len(ns) > 0 { // report's Next step #1 (rules); the summary has the reasons
					if c, ok := ns[0].(map[string]any); ok {
						row["next"] = c["name"]
					}
				}
				if sp, ok := m["score_post"].(map[string]any); ok {
					row["score_post"], row["score_post_ep"] = sp["count"], sp["endpoint"]
				}
				if bl, ok := m["bench_log"].(map[string]any); ok {
					if top, ok := bl["top"].([]any); ok && len(top) > 0 {
						parts := []string{}
						for _, t := range top {
							parts = append(parts, fmt.Sprint(t))
						}
						row["bench_top"] = strings.Join(parts, " / ")
					}
				}
				if bs, ok := m["bench_shape"].(map[string]any); ok {
					row["shape"] = ternary(bs["fixed"] == true, fmt.Sprintf("fixed ≈%.0f", num(bs["inflight_med"])), fmt.Sprintf("rising %.0f→%.0f", num(bs["start"]), num(bs["end"])))
				}
			}
		}
		if n := p.notes[e.Name()]; n != nil {
			row["score"], row["note"], row["bench"] = n["score"], n["note"], n["bench"]
			for _, k := range []string{"pass", "dns_ok", "dns_fail", "bench_done_at", "score_src"} {
				if v, ok := n[k]; ok {
					row[k] = v
				}
			}
		}
		if st := p.pipeStateLocked(e.Name()); st != "" {
			row["auto"] = st
		}
		rows = append(rows, row)
	}
	writeJSONResp(w, rows)
}

// retro: generate retro.md/html for the contest (served under /data/<contest>/)
func (p *panel) handleRetro(w http.ResponseWriter, r *http.Request) {
	if err := runRetro([]string{p.contestDir(""), "-out", p.contestDir(""), "-repo", envOr("REPO_DIR", "")}); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	writeJSONResp(w, map[string]any{"retro": "/data/" + p.contest + "/retro.html", "md": "/data/" + p.contest + "/retro.md"})
}

func keepOnHost() bool { return os.Getenv("KEEP_ON_HOST") == "1" }

// pruneRaw keeps raw files (logs, jsonl, pprof) only for the newest RAW_KEEP runs; reports/summaries/notes stay
// (same keep-list as the manual prune, prune.go).
func (p *panel) pruneRaw() {
	keep := 20
	if v, err := strconv.Atoi(os.Getenv("RAW_KEEP")); err == nil && v > 0 {
		keep = v
	}
	entries, _ := os.ReadDir(p.contestDir("measurements"))
	var dirs []string
	for _, e := range entries {
		if e.IsDir() {
			dirs = append(dirs, e.Name())
		}
	}
	sort.Strings(dirs)
	for i := 0; i < len(dirs)-keep; i++ {
		base := filepath.Join(p.contestDir("measurements"), dirs[i])
		if _, total := rawFiles(base); total > 0 {
			pruneRunDir(base)
		}
	}
}

// benchlog: the organisers' bench output for a run, pasted by a human (portal) or uploaded by `task bench:log`.
// Stored verbatim as <run>/bench.log (survives raw pruning); the report condenses it to a "Bench output" section.
func (p *panel) handleBenchLog(w http.ResponseWriter, r *http.Request) {
	ts := r.URL.Query().Get("ts")
	if ts == "" || strings.Contains(ts, "..") {
		http.Error(w, "ts required", 400)
		return
	}
	dir := filepath.Join(p.contestDir("measurements"), ts)
	path := filepath.Join(dir, "bench.log")
	if r.Method == "GET" {
		b, err := os.ReadFile(path)
		if err != nil {
			http.Error(w, "", 404)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Write(b)
		return
	}
	if r.Method != "POST" {
		http.Error(w, "GET or POST", 405)
		return
	}
	if _, err := os.Stat(dir); err != nil {
		os.MkdirAll(dir, 0755) // the bench can be pasted before collect; collect fills the host dirs later
	}
	b, err := io.ReadAll(io.LimitReader(r.Body, 64<<20))
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	if err := os.WriteFile(path, b, 0644); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	res := map[string]any{"ts": ts, "bytes": len(b)}
	if _, err := os.Stat(filepath.Join(dir, "collect.json")); err == nil {
		if err := p.reportProc(dir, "", ""); err != nil {
			res["report"] = err.Error()
		} else {
			res["report"] = "ok"
			// the bench's result line carries the score: fill the note when nobody typed one
			if sc, ok := p.scoreFromReport(ts); ok {
				res["score"] = sc
			}
		}
	} else {
		res["report"] = "saved; the report is built at collect"
	}
	writeJSONResp(w, res)
}

func (p *panel) handleNote(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "POST", 405)
		return
	}
	var in map[string]any
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&in); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	ts, _ := in["ts"].(string)
	if ts == "" {
		http.Error(w, "ts required", 400)
		return
	}
	p.mu.Lock()
	n := p.notes[ts]
	if n == nil {
		n = map[string]any{}
	}
	for k, v := range in {
		if k != "ts" {
			n[k] = v
		}
	}
	n["updated_at"] = time.Now().Format(time.RFC3339)
	p.notes[ts] = n
	p.saveNotes()
	if _, err := os.Stat(filepath.Join(p.contestDir("measurements"), ts)); err == nil {
		writeJSON(filepath.Join(p.contestDir("measurements"), ts, "note.json"), n)
	}
	p.mu.Unlock()
	writeJSONResp(w, n)
}

// /api/agent/<host>/<path> proxies GET/POST to an agent (used for start/stop and file preview)
func (p *panel) handleAgentProxy(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/agent/")
	parts := strings.SplitN(rest, "/", 2)
	if len(parts) < 2 {
		http.NotFound(w, r)
		return
	}
	var h *panelHost
	for i := range p.hosts {
		if p.hosts[i].Name == parts[0] {
			h = &p.hosts[i]
		}
	}
	if h == nil {
		http.NotFound(w, r)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, r.Method, h.Agent+"/"+parts[1]+"?"+r.URL.RawQuery, r.Body)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		http.Error(w, err.Error(), 502)
		return
	}
	defer res.Body.Close()
	w.Header().Set("Content-Type", res.Header.Get("Content-Type"))
	w.WriteHeader(res.StatusCode)
	io.Copy(w, res.Body)
}

// ---- UI (single page, server-rendered shell + small JS) ----

func (p *panel) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	hostsJSON, _ := json.Marshal(p.hosts)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, panelHTML, html.EscapeString(p.contest), version, string(hostsJSON))
}

const panelHTML = `<!DOCTYPE html><html lang="ja"><head><meta charset="utf-8"><title>isucon ops · %[1]s</title>
<style>code.copy{cursor:pointer}code.copy:hover{background:#d1fae5}button.danger{color:#b91c1c}body.paused header{background:#92400e}.pausedhost{outline:2px dashed #d97706}
:root{--ink:#1f2937;--muted:#6b7280;--accent:#0f766e;--line:#e5e7eb}
*{box-sizing:border-box}body{font-family:-apple-system,"Hiragino Sans",sans-serif;margin:0;background:#fafaf9;color:var(--ink);font-size:14px}
header{background:#0f766e;color:#fff;padding:10px 20px;display:flex;gap:14px;align-items:baseline}header h1{margin:0;font-size:17px}header .m{color:#d1fae5;font-size:12px}
main{max-width:1600px;margin:0 auto;padding:14px 20px}section{background:#fff;border:1px solid var(--line);border-radius:10px;padding:14px 18px;margin-bottom:14px}
h2{font-size:16px;margin:0 0 8px;border-left:4px solid var(--accent);padding-left:8px}
table{border-collapse:collapse;width:100%%;font-size:13px}th,td{border:1px solid var(--line);padding:4px 8px;text-align:left;vertical-align:top}th{background:#f1f5f4;white-space:nowrap}td.num{text-align:right;white-space:nowrap}
.ok{color:#059669}.bad{color:#b91c1c;font-weight:600}.warn{color:#b45309}.muted{color:var(--muted);font-size:12px}
.badge{display:inline-block;font-size:11px;padding:0 6px;border-radius:999px;background:#e0f2f1;color:#0f766e}.badge.run{background:#fee2e2;color:#991b1b;animation:blink 1s infinite}@keyframes blink{50%%{opacity:.5}}
button{border:1px solid var(--line);background:#fff;border-radius:6px;padding:3px 10px;cursor:pointer;font-size:12px}button:hover{border-color:var(--accent)}input{border:1px solid var(--line);border-radius:6px;padding:3px 6px;font-size:12px}
code{font-family:ui-monospace,Menlo,monospace;font-size:12px;background:#f3f4f6;padding:1px 4px;border-radius:3px}
.spark{display:inline-block;vertical-align:middle;background:#f3f4f6;border-radius:3px}.spk{display:inline-block;margin-right:10px;white-space:nowrap}.spk b{display:inline-block;min-width:3.2em;text-align:right}
.appcard{display:flex;gap:16px;align-items:center;flex-wrap:wrap;border-left:6px solid #7c3aed}.appcard h2{border:0;padding:0;margin:0}
a.appbtn{display:inline-block;background:#7c3aed;color:#fff;font-weight:600;padding:7px 16px;border-radius:8px;text-decoration:none;font-size:14px}a.appbtn:hover{background:#6d28d9}a.appbtn.br{background:#fff;color:#6d28d9;border:1px solid #7c3aed;font-weight:500;padding:4px 12px;font-size:13px}
.appst{font-size:13px;line-height:1.7}.appst .none{color:#b45309;font-weight:600}
#benchbar{position:sticky;top:0;z-index:10;display:flex;gap:18px;align-items:center;padding:10px 20px;background:#e5e7eb;color:#374151;border-bottom:1px solid #d1d5db;font-size:15px}
#benchbar .bbtext{flex:1}#benchbar code{background:rgba(255,255,255,.25);color:inherit}#benchbar.running a,#benchbar.ended a{color:#fde047}#benchbar .bbtext b{font-size:17px}
#benchbar button.bigdone{font-size:15px;font-weight:700;padding:8px 18px;border-radius:10px;border:2px solid #6b7280;background:#fff;color:#111827}
#benchbar.running{background:#b91c1c;color:#fff;border-color:#7f1d1d}#benchbar.ended{background:#d97706;color:#fff;border-color:#92400e}#benchbar.signalled{background:#065f46;color:#fff}
#benchbar.running button.bigdone,#benchbar.ended button.bigdone{font-size:26px;padding:14px 34px;border:3px solid #111827;background:#fde047;color:#111827;box-shadow:0 3px 0 #111827}
#benchbar.running button.bigdone:hover,#benchbar.ended button.bigdone:hover{background:#facc15}
input.score{font-size:20px;width:8.5em;padding:4px 8px;font-weight:700;text-align:right}input.score.need{background:#fef9c3;border:2px solid #d97706}
button.fail{font-size:14px;font-weight:700;padding:5px 12px;color:#b91c1c;border:2px solid #b91c1c}button.fail.on{background:#fff;color:#6b7280;border-color:#9ca3af;font-weight:500}
.failtag{display:inline-block;background:#b91c1c;color:#fff;font-weight:700;padding:1px 8px;border-radius:6px}
a.primary,button.primary{display:inline-block;font-size:15px;font-weight:700;padding:6px 16px;border-radius:8px;background:#0f766e;color:#fff;border:0;text-decoration:none;margin:2px 0}a.primary:hover,button.primary:hover{background:#115e59}
.fallback{font-size:11px;color:var(--muted)}td.more{white-space:nowrap;text-align:right}td.more details summary{cursor:pointer;list-style:none;font-size:18px;padding:0 6px;color:var(--muted)}
#pagenav{display:flex;gap:6px;white-space:nowrap;font-size:13px}#pagenav a{color:inherit;text-decoration:none;padding:2px 9px;border:1px solid rgba(0,0,0,.18);border-radius:6px;background:rgba(255,255,255,.55)}#benchbar.running #pagenav a,#benchbar.ended #pagenav a,#benchbar.signalled #pagenav a{color:#111827}
section{scroll-margin-top:64px}tr[id^="run-"]{scroll-margin-top:72px}#runs td{vertical-align:top}#runs th,#runs td{padding:6px 6px}#runs td.sc{min-width:15em}#runs td.sc .scrow{display:flex;gap:4px;align-items:center;flex-wrap:wrap}#runs td.sc .scstack{display:flex;flex-direction:column;gap:4px}#runs td.sc input.nt,#runs td.sc textarea{width:100%%;box-sizing:border-box}#runs td.sc textarea{font-size:12px}#runs td.act{min-width:9em}#runs tr.old{display:none}#runs table.showold tr.old{display:table-row}#runs tr.oldtoggle td{background:#f9fafb;cursor:pointer;color:var(--muted);font-size:13px;padding:8px}#runs tr.oldtoggle td:hover{color:var(--accent)}td.more details[open]{white-space:normal;text-align:left}td.more details div{display:flex;flex-direction:column;gap:3px;align-items:flex-start;margin-top:3px}#runs .tw{overflow-x:auto}#runs input.score{font-size:17px;width:5.6em;padding:2px 6px}#runs button.fail{font-size:12px;padding:2px 8px}#runs a.primary,#runs button.primary{font-size:13px;padding:3px 10px}td.ts{white-space:nowrap}
.hb{display:inline-block;font-size:11px;padding:0 5px;border-radius:4px;background:#ecfdf5;color:#065f46;margin:1px 3px 1px 0;white-space:nowrap}.hb.run{background:#fef3c7;color:#92400e}.hb.shadow{background:#f3f4f6;color:#6b7280}
td.hostcell{min-width:7em;max-width:16em}td.hostcell details summary{cursor:pointer;list-style:none}td.hostcell details summary::-webkit-details-marker{display:none}td.hostcell details[open] summary{margin-bottom:3px}
code.git{display:inline-block;max-width:8em;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;vertical-align:middle}
details.tl>summary{cursor:pointer;color:var(--muted);font-size:12px;margin:6px 0}
.hosts{display:grid;grid-template-columns:repeat(auto-fit,minmax(300px,1fr));gap:10px}.host{border:1px solid var(--line);border-radius:8px;padding:10px}.host h3{margin:0 0 6px;font-size:14px}.kv{display:grid;grid-template-columns:auto 1fr;gap:2px 10px;font-size:12px}.kv b{color:var(--muted);font-weight:500}
</style></head><body>
<header><h1>isucon ops</h1><span class="m">contest: <b>%[1]s</b> · panel %[2]s · <a href="/app/" target="_blank" style="color:#fff">app-analysis report</a> · <span id="clock"></span></span><span style="margin-left:auto"></span><span id="pausebox"><button id="pausebtn" onclick="pause(30)" title="access-log triggers are ignored on every host for 30 min (auto-resume); manual/shadow runs still work">⏸ not benching now (30 min)</button> <button onclick="pause(0)">▶ resume detection</button> <span id="pausest" class="m"></span></span></header>
<div id="benchbar"><span class="bbtext" id="bbtext">…</span><button class="bigdone" id="bigdone" onclick="benchDone()" title="the bench finished: end the run on every host now, then collect → report automatically (same as task bench:done). Type the score in the Runs table afterwards.">■ ベンチ終わった</button><nav id="pagenav"><a href="#sec-app">App</a><a href="#sec-fleet">Fleet</a><a href="#sec-runs">Runs</a><a href="#sec-timeline">Timeline</a></nav></div>
<main>
<section id="sec-app" class="appcard"><h2>App analysis</h2><span class="muted">画面 → API → ハンドラ → テーブル（app-map）</span><div id="appst" class="appst">…</div></section>
<section id="sec-fleet"><h2>Fleet</h2><div id="fleet" class="hosts"></div></section>
<section id="sec-runs"><h2>Runs <button onclick="loadRuns(true)" title="re-render now (drops text typed but not saved)">refresh</button></h2><div id="runs"></div></section>
<section id="sec-timeline"><h2>Score timeline <button onclick="loadTimeline()">refresh</button> <button onclick="retro()">build retrospective</button></h2><div id="timeline"></div></section>
</main>
<script>
const HOSTS = %[3]s;
const $ = (s) => document.querySelector(s);
const esc = (s) => String(s ?? "").replace(/&/g,"&amp;").replace(/</g,"&lt;");
const escA = (s) => esc(s).replace(/"/g,"&quot;");
// spark: polyline of arr on a 0..max scale (max defaults to the series max); faint = optional second series drawn underneath
const spark = (arr, w=120, h=24, max, color="#0f766e", faint) => { if(!arr||!arr.length) return ""; const m = max ?? Math.max(1, ...arr); const line = (a) => a.map((v,i)=>((i/(a.length-1||1))*w).toFixed(1)+","+(h-(Math.min(v,m)/m)*h).toFixed(1)).join(" "); return '<svg class="spark" width="'+w+'" height="'+h+'">'+(faint&&faint.length?'<polyline fill="none" stroke="#d97706" stroke-opacity=".45" stroke-width="1" points="'+line(faint)+'"/>':'')+'<polyline fill="none" stroke="'+color+'" stroke-width="1.5" points="'+line(arr)+'"/></svg>'; };
// the host's last 120 s from /tail: cpu = user+sys (faint orange = iowait), mem = used %%, both on a fixed 0-100 scale
function sysSparks(t){
  const sys = (t&&t.sys)||[]; if(!sys.length) return '<span class="muted">no samples yet</span>';
  const cpu = sys.map(x=>(x.cpu_user_pct||0)+(x.cpu_sys_pct||0)), io = sys.map(x=>x.cpu_iowait_pct||0), mem = sys.map(x=>x.mem_used_pct||0);
  const last = (a) => Math.round(a[a.length-1]);
  return '<span class="spk" title="cpu user+sys %%, last '+sys.length+' s (faint orange = iowait %%, now '+last(io)+'%%)">cpu '+spark(cpu,120,22,100,"#0f766e",io)+' <b>'+last(cpu)+'%%</b></span><span class="spk" title="memory used %%, last '+sys.length+' s">mem '+spark(mem,120,22,100,"#7c3aed")+' <b>'+last(mem)+'%%</b></span>';
}
async function loadFleet(){
  const r = await fetch("/api/fleet?full=1").then(r=>r.json()).catch(()=>null);
  if(!r) return;
  $("#clock").textContent = r.time + " · ops disk free " + r.ops_disk_free_gb + " GB";
  { const pu = r.hosts.map(h=>h.paused_until).filter(Boolean); const st = $("#pausest"); if(pu.length){ const until = new Date(pu.sort()[pu.length-1]); st.innerHTML = '<b style="color:#b45309">detection paused until '+String(until.getHours()).padStart(2,"0")+":"+String(until.getMinutes()).padStart(2,"0")+' on '+pu.length+'/'+r.hosts.length+' hosts</b>'; document.body.classList.add("paused"); } else { st.textContent = ""; document.body.classList.remove("paused"); } }
  { const nowRunning = r.hosts.some(h=>h.run); if (RUNNING !== null && nowRunning !== RUNNING) { loadRuns(); loadTimeline(); } RUNNING = nowRunning;
    const lead = r.hosts.filter(h=>h.run && !h.run.follow && !h.run.manual).map(h=>h.run).sort((a,b)=>String(a.ts).localeCompare(String(b.ts)))[0] || r.hosts.map(h=>h.run).filter(Boolean)[0] || null;
    LIVE = lead; renderBar(); }
  const el = $("#fleet"); el.innerHTML = "";
  for (const h of r.hosts) {
    const d = document.createElement("div"); d.className = "host" + (h.paused_until ? " pausedhost" : "");
    if (h.error) { d.innerHTML = '<h3>'+esc(h.name)+' <span class="bad">unreachable</span></h3><div class="muted">'+esc(h.addr)+' · '+esc(h.error)+'</div>'; el.append(d); continue; }
    const s = h.sys||{}; const g = h.git||{}; const run = h.run;
    const svc = (h.services||[]).map(x=>'<span class="'+(x.active==="active"?"ok":"bad")+'">'+esc(x.unit.replace(".service",""))+'</span>'+(x.enabled!=="enabled"?'<span class="warn">(disabled)</span>':'')).join(" · ");
    d.innerHTML = '<h3>'+esc(h.name)+' <span class="muted">'+esc(h.addr)+'</span> '+(run?'<span class="badge run">BENCH '+run.elapsed_sec+'s · '+run.req_total+' req</span>':'<span class="badge">idle</span>')+'</h3>'
      + '<div class="kv"><b>git</b><span><code>'+esc(g.branch)+'</code> '+esc(g.commit)+' <span class="muted">'+esc(g.subject)+(g.dirty_files?' · dirty '+g.dirty_files:'')+'</span></span>'
      + '<b>services</b><span>'+svc+'</span>'
      + '<b>load/cpu</b><span>'+s.load1+' · user '+s.cpu_user_pct+'%% sys '+s.cpu_sys_pct+'%% iowait '+s.cpu_iowait_pct+'%%</span>'
      + '<b>mem/disk</b><span>'+s.mem_used_pct+'%% used · '+s.mem_avail_mb+' MB free · disk <span class="'+(h.disk_warning?'bad':'')+'">'+s.disk_free_gb+' GB free'+(h.disk_warning?' LOW':'')+'</span></span>'
      + '<b>cpu · mem</b><span id="sp-'+h.name+'"></span>'
      + '<b>req/s</b><span>'+h.req_per_s_10s+' <span class="muted">(10 s avg)</span></span>'
      + '<b>env</b><span class="muted">'+esc(Object.entries(h.env||{}).filter(([k])=>/HOST|PORT|NAME|INTERVAL/.test(k)).map(([k,v])=>k+"="+v).join(" "))+'</span>'
      + '<b>agent</b><span class="muted">'+esc(h.version)+(h.version && h.version < "%[2]s" ? ' <span class="bad">outdated → task host:agent HOST='+esc(h.addr)+'</span>' : '')+' up '+h.uptime_sec+'s · '+(h.last_error?'<span class="bad">'+esc(h.last_error)+'</span>':'ok')+'</span></div>'
      + '<div style="margin-top:6px"><button onclick="agent(\''+h.name+'\',\'start\')">manual start</button> <button onclick="agent(\''+h.name+'\',\'stop\')">stop</button></div>';
    el.append(d);
    fetch("/api/agent/"+h.name+"/tail").then(r=>r.json()).then(t=>{ const e=document.getElementById("sp-"+h.name); if(e) e.innerHTML = sysSparks(t); }).catch(()=>{});
  }
}
const hm = (iso) => { if(!iso) return ""; const d = new Date(iso); return isNaN(d) ? "" : String(d.getHours()).padStart(2,"0")+":"+String(d.getMinutes()).padStart(2,"0"); };
const appWhen = (x) => (x.generated_at ? hm(x.generated_at)+' の巡回' : '巡回時刻不明') + (x.refreshed_at && x.refreshed_at > (x.generated_at||"") ? ' · '+hm(x.refreshed_at)+' に数字を更新' : '') + (x.sha ? ' · commit <code>'+esc(x.sha)+'</code>（'+esc(x.subject)+'）'+(x.commit_at?' '+hm(x.commit_at):'') : '');
async function loadApp(){
  const r = await fetch("/api/app").then(r=>r.json()).catch(()=>null);
  const el = $("#appst"); if(!r){ el.textContent = "status unavailable"; return; }
  let h = "";
  if (r.main) h += '<a class="appbtn" href="/app/" target="_blank">App analysis を開く →</a> <span>'+appWhen(r.main)+'</span>';
  else h += '<span class="none">main にはまだ無い</span> <span class="muted">（app-analysis 実行中か PR の merge 待ち。merge 後 1 分で ops の clone に入る）</span>';
  for (const b of (r.branches||[])) h += '<div><a class="appbtn br" href="'+esc(b.url)+'" target="_blank">ブランチ '+esc(b.ref)+' にある（merge 前）→ 見る</a> <span class="muted">'+appWhen(b)+'</span></div>';
  if (r.error) h += ' <span class="muted">'+esc(r.error)+'</span>';
  el.innerHTML = h;
}
let RUNNING = null;
let LIVE = null, LASTRUNS = [], BBMSG = "";
const tsDate = (ts) => new Date(Date.parse(String(ts).replace(/(\d{4})(\d{2})(\d{2})-(\d{2})(\d{2})(\d{2})/,"$1-$2-$3T$4:$5:$6Z")));
// the sticky bar: huge 「ベンチ終わった」 while a run is going or just ended without the signal; after the signal, where the pipeline is
function renderBar(){
  const bar = $("#benchbar"), tx = $("#bbtext"); if(!bar) return;
  const newest = (LASTRUNS||[]).find(g=>!g.ops_only) || (LASTRUNS||[])[0];
  const signalled = (g) => g && ((g.note||{}).bench_done_at || g.auto);
  let cls = "", h = "";
  if (LIVE) {
    cls = "running";
    h = '<b>ベンチ実行中</b> run <code>'+esc(LIVE.ts)+'</code> · '+(LIVE.elapsed_sec||0)+' s · '+(LIVE.req_total||0)+' req — ベンチがスコアを出したら押す（スコアは後で Runs の欄に入れる）';
  } else if (newest && !signalled(newest) && !newest.report && (Date.now() - tsDate(newest.ts)) < 15*60*1000) {
    cls = "ended";
    h = '<b>run '+esc(newest.ts)+' が終わった</b>（自動検出）— ベンチのスコアが出たら押す → collect → report が自動';
  } else if (newest && signalled(newest) && (Date.now() - tsDate(newest.ts)) < 30*60*1000) {
    cls = "signalled";
    const sc = (newest.note||{}).score;
    h = '<b>受け付けた</b> run <code>'+esc(newest.ts)+'</code>'+(newest.auto?' · 自動: '+esc(newest.auto):(newest.report?' · report あり':''))+(isFail(newest.note) ? ' · <span class="failtag">FAIL</span>' : sc!=null && sc!=="" ? ' · score '+esc(sc) : ' · <a href="#run-'+esc(newest.ts)+'" style="color:#fde047;font-weight:700">スコアを入力 ↓</a>');
  } else {
    h = '<span class="muted">ベンチは走っていない（/initialize を受けると run が始まる）</span>';
  }
  if (BBMSG) h += '<div style="font-size:12px;margin-top:2px">'+esc(BBMSG)+'</div>';
  bar.className = cls; tx.innerHTML = h;
}
async function benchDone(){
  const btn = $("#bigdone"); if (btn) btn.disabled = true;
  BBMSG = "送信中…"; renderBar();
  try {
    const r = await fetch("/api/bench-done", {method:"POST"});
    const t = await r.text(); let m = t; try { const j = JSON.parse(t); m = "run "+j.ts+" · stopped: "+Object.entries(j.stopped||{}).map(([k,v])=>k+"="+v).join(", ")+" · "+j.pipeline; } catch(e){}
    BBMSG = (r.ok ? "" : "失敗: ") + m;
  } catch(e) { BBMSG = "失敗: "+e; }
  if (btn) btn.disabled = false;
  renderBar(); loadRuns(); loadFleet();
}
const isFail = (n) => n && (n.pass === "false" || n.pass === false);
async function saveScore(ts, el){
  const v = el.value.trim().replace(/,/g,""); const n = (LASTRUNS.find(g=>g.ts===ts)||{}).note||{};
  const d = {score: v, score_src: v ? "typed" : ""};
  if (isFail(n) && parseFloat(v) > 0) d.pass = "";
  el.defaultValue = el.value; await note(ts, d); el.classList.remove("need"); el.blur(); loadRuns();
}
async function toggleFail(ts){
  const n = (LASTRUNS.find(g=>g.ts===ts)||{}).note||{};
  if (isFail(n)) await note(ts, {pass: "", score: "", score_src: "", note: String(n.note||"").replace(/^fail( · )?/,"")});
  else await note(ts, {pass: "false", score: 0, score_src: "fail", note: n.note ? (String(n.note).startsWith("fail") ? n.note : "fail · "+n.note) : "fail"});
  loadRuns();
}
async function agent(host, action){ const r = await fetch("/api/agent/"+host+"/"+action, {method:"POST"}); alert(await r.text()); loadFleet(); }
// a field in the Runs table is being edited (focused, or typed but not saved yet): re-rendering would drop it
function runsEditing(){
  const el = $("#runs"); if(!el) return false;
  return [...el.querySelectorAll("input, textarea")].some(f => f.type!=="checkbox" && (f===document.activeElement || f.value!==f.defaultValue));
}
let RUNS_PENDING = false;
async function loadRuns(force){
  const runs = await fetch("/api/runs").then(r=>r.json()).catch(()=>[]);
  LASTRUNS = runs || []; renderBar();
  const el = $("#runs");
  if (force!==true && runsEditing()) { RUNS_PENDING = true; return; }
  RUNS_PENDING = false;
  if(!runs||!runs.length){ el.innerHTML = '<div class="muted">no runs yet. A run starts automatically when the agent sees POST /initialize.</div>'; return; }
  let h = '<div style="margin-bottom:6px"><button class="danger" id="prunebtn" onclick="pruneSelected()" title="選んだ run の生ログ（slow/access log, jsonl, pprof, export CSV）を ops と各ホストから消す。report / summary / メモは残る">ログを削除（レポートは残す）</button> <span id="prunest" class="muted"></span></div>'
    + '<div class="tw"><table><tr><th><input type="checkbox" title="select all runs that still have raw logs" onchange="selAll(this.checked)"></th><th>ts</th><th title="hover a badge for the end reason / diagnostics; click to expand">hosts</th><th>req</th><th>err</th><th>dur</th><th>git</th><th title="raw logs on ops (what ログを削除 frees)">raw</th><th title="score change from the previous scored run (FAIL runs are skipped)">Δ</th><th>score / note</th><th>report</th><th title="re-report / re-collect / bench log / delete">⋯</th></tr>';
  const delta = runDeltas(runs); // over all runs, so the first old run still has its Δ
  runs.forEach((g, i) => {
    if (i === RUNS_SHOWN) h += '<tr class="oldtoggle" onclick="toggleOld(this)"><td colspan="12">'+oldLabel(runs.length-RUNS_SHOWN)+'</td></tr>';
    const hs = Object.entries(g.hosts);
    const req = hs.reduce((n,[,r])=>n+(r.req_total||0),0), err = hs.reduce((n,[,r])=>n+(r.err_total||0),0);
    const dur = Math.max(...hs.map(([,r])=>r.duration_sec||0));
    const git = [...new Set(hs.map(([,r])=>r.git?(r.git.branch+"@"+r.git.commit):"?"))].join(", ");
    const files = hostsCell(g.ts, hs);
    const note = g.note||{};
    const prunable = g.collected && g.report && !g.pruned && (g.raw_bytes||0) > 0;
    const sel = '<td>'+(prunable?'<input type="checkbox" class="prunesel" data-ts="'+g.ts+'" data-bytes="'+(g.raw_bytes||0)+'"'+(SEL.has(g.ts)?' checked':'')+' onchange="selOne(this)">':'')+'</td>';
    const raw = '<td class="num">'+(g.collected?(g.pruned?'<span class="badge" title="raw logs deleted: re-report, deep-dive and task run:pull of raw logs are not possible; the report stays">ログ削除済み</span>'+((g.raw_bytes||0)>0?' '+mb(g.raw_bytes):''):mb(g.raw_bytes||0)):'<span class="muted">on hosts</span>')+'</td>';
    const d = delta[g.ts];
    h += '<tr id="run-'+g.ts+'"'+(i>=RUNS_SHOWN?' class="old"':'')+'>'+sel+'<td class="ts"><code class="copy" title="click to copy" onclick="copyTs(this,\''+g.ts+'\')">'+g.ts+'</code> <span class="muted" title="local time">'+hhmm(g.ts)+'</span></td><td class="hostcell">'+files+'</td><td class="num">'+req+'</td><td class="num">'+err+'</td><td class="num">'+dur+'s</td><td><code class="git" title="'+escA(git)+'">'+esc(git)+'</code></td>'+raw
      + '<td class="num '+(d<0?"bad":d>0?"ok":"")+'">'+(d==null?"":(d>0?"+":"")+Math.round(d))+'</td>'
      + '<td class="sc"><div class="scstack"><div class="scrow">'+scoreCell(g)+'</div><input class="nt" placeholder="note (what changed)" value="'+escA(note.note??"")+'" onchange="this.defaultValue=this.value;note(\''+g.ts+'\',{note:this.value})"><textarea rows="1" title="bench output: score line, fail reason, error categories (paste from the portal; click to expand)" placeholder="bench" onfocus="this.rows=8" onblur="this.rows=1" onchange="this.defaultValue=this.value;note(\''+g.ts+'\',{bench:this.value})">'+esc(note.bench??"")+'</textarea></div></td>'
      + '<td class="act">'+actionsCell(g)+'<div id="st-'+g.ts+'" style="white-space:normal"></div></td>'
      + '<td class="more"><details'+(MOPEN.has(g.ts)?" open":"")+' ontoggle="this.open?MOPEN.add(\''+g.ts+'\'):MOPEN.delete(\''+g.ts+'\')"><summary title="re-report / re-collect / bench log / delete">⋯</summary><div>'+secondaryActions(g).join("")+'<button class="danger" onclick="delRun(\''+g.ts+'\')" title="remove this run on ops and on every host (mis-detection, test)">delete run…</button></div></details></td></tr>';
  });
  el.innerHTML = h + '</table></div>';
  if (OLDOPEN) { const t = el.querySelector("table"); if (t) t.classList.add("showold"); }
  selShow();
  // the bench-finished pipeline is moving: follow it every 5 s until done
  if (!AUTOT && runs.some(g=>g.auto && g.auto!=="done" && !String(g.auto).startsWith("report failed"))) AUTOT = setTimeout(()=>{ AUTOT = null; loadRuns(); loadTimeline(); }, 5000);
}
let AUTOT = null;
// only the newest RUNS_SHOWN runs are visible; the rest sit behind a toggle row whose state survives the 30 s re-render
const RUNS_SHOWN = 5; let OLDOPEN = false;
const oldLabel = (n) => (OLDOPEN ? "▾ 古い run を隠す（" : "▸ 古い run（")+n+" 件）";
function toggleOld(tr){ OLDOPEN = !OLDOPEN; const t = tr.closest("table"); t.classList.toggle("showold", OLDOPEN); tr.cells[0].textContent = oldLabel(t.querySelectorAll("tr.old").length); }
// a link to #run-<ts> (Timeline) that points at a hidden old run opens the old runs first
function showRunAnchor(){ const m = location.hash.match(/^#run-(.+)$/); if (!m) return; const tr = document.getElementById("run-"+m[1]); if (tr && tr.classList.contains("old") && !OLDOPEN) { const tg = tr.closest("table").querySelector("tr.oldtoggle"); if (tg) toggleOld(tg); tr.scrollIntoView(); } }
window.addEventListener("hashchange", showRunAnchor);
// Δ per run: score minus the previous (older) scored run; FAIL and unscored runs get none and are not a baseline
function runDeltas(runs){
  const out = {}; let prev = null;
  for (const g of [...runs].reverse()) { const n = g.note||{}; const s = parseFloat(n.score); if (isFail(n) || isNaN(s)) continue; if (prev != null) out[g.ts] = s - prev; prev = s; }
  return out;
}
// hosts: one line of badges (svc1 ✓ · svc2 ● running · grey = shadow); the details are in each badge's tooltip and in the expandable cell
const HOPEN = new Set(), MOPEN = new Set(); // expanded host cells / ⋯ menus survive the 30 s re-render
function hostTip(n, r){
  const pp = r.pprof ? Object.values(r.pprof).filter(v=>!String(v).startsWith("error")).length : 0;
  return n+": "+(r.end_reason ? r.end_reason+(r.collected?" (collected)":"") : "running")+(r.follow?" (shadow)":"")+(r.pprof?" · pprof "+pp:"")
    + (r.end_diag ? "\n"+r.end_diag : "") + (r.end_diag ? "\n"+((r.tail_reqs||[]).join("\n")||"no request tail recorded") : "");
}
function hostsCell(ts, hs){
  if (!hs.length) return '<span class="muted">(collected; raw data pruned)</span>';
  const badges = hs.map(([n,r])=>'<span class="hb'+(!r.end_reason?" run":r.follow?" shadow":"")+'" title="'+escA(hostTip(n,r))+'">'+esc(n)+" "+(!r.end_reason?"●":"✓")+'</span>').join("");
  const body = hs.map(([n,r])=>esc(hostTip(n,r)).replace(/\n/g,"<br>")).join('<hr style="border:0;border-top:1px solid #e5e7eb;margin:2px 0">');
  return '<details'+(HOPEN.has(ts)?" open":"")+' ontoggle="this.open?HOPEN.add(\''+ts+'\'):HOPEN.delete(\''+ts+'\')"><summary>'+badges+'</summary><div class="muted">'+body+'</div></details>';
}
// a refresh skipped while editing runs once the field is left (after its onchange save has been sent)
$("#runs").addEventListener("focusout", () => { if (RUNS_PENDING) setTimeout(() => { if (RUNS_PENDING) loadRuns(); }, 400); });
const SEL = new Set();
const runBase = (ts) => '/data/'+encodeURIComponent("%[1]s")+'/measurements/'+ts;
// score: a big input (Enter saves) + FAIL; the end signal (■ ベンチ終わった) carries no score any more
function scoreCell(g){
  const n = g.note||{}, fail = isFail(n), has = n.score!=null && n.score!=="";
  const need = !has && !fail && (n.bench_done_at || g.auto || g.report);
  const hint = n.score_src==="bench.log" ? '<span class="muted" title="read from the pasted bench output">（ベンチ出力から）</span>' : '';
  return (fail ? '<span class="failtag">FAIL</span> ' : '<input class="score'+(need?' need':'')+'" inputmode="numeric" placeholder="'+(need?'score → Enter':'score')+'" value="'+esc(has?n.score:"")+'" onkeydown="if(event.key===\'Enter\'){saveScore(\''+g.ts+'\',this)}" onchange="saveScore(\''+g.ts+'\',this)"> ')
    + '<button class="fail'+(fail?' on':'')+'" onclick="toggleFail(\''+g.ts+'\')" title="'+(fail?'FAIL を取り消す':'ベンチが FAIL: score 0 / pass=false / note に fail を記録')+'">'+(fail?'FAIL を取消':'FAIL')+'</button>'+hint;
}
// report / collect: the report link is the primary action; collect / report are fallbacks (bench-done does both automatically)
function actionsCell(g){
  const b = runBase(g.ts); let h = "";
  const autoBusy = g.auto && g.auto!=="done" && !String(g.auto).startsWith("report failed");
  const running = Object.values(g.hosts||{}).some(r=>r && !r.end_reason);
  if (running && !autoBusy) h += '<span class="muted">running — ■ ベンチ終わった で collect → report が自動</span>';
  else if (g.report) h += '<a class="primary" href="'+b+'/report.html" target="_blank">report を開く</a> <a href="'+b+'/summary.md" target="_blank">summary</a>';
  else if (autoBusy) h += '<span class="badge run" style="font-size:13px" title="started by the bench-finished signal">自動: '+esc(g.auto)+'</span>';
  else if (g.collected && !g.pruned) h += '<button class="primary" onclick="report(\''+g.ts+'\')" title="自動で出来ていない時だけ（■ ベンチ終わった で自動）">report を作る</button>';
  else if (!g.ops_only && !g.pruned) h += '<button class="primary" onclick="collect(\''+g.ts+'\')" title="手動の予備: 普通は ■ ベンチ終わった で collect → report が自動">collect</button>';
  if (g.auto && !autoBusy) h += ' <span class="badge" title="started by the bench-finished signal (task bench:done / ■ button)">auto: '+esc(g.auto)+'</span>';
  if (g.report_error) h += '<div style="color:#c00">report failed: '+esc(g.report_error)+'</div>';
  return h;
}
// the row's ⋯ menu: fallbacks that keep the row one line high (re-report, re-collect, bench log); delete is added after them
function secondaryActions(g){
  const sec = [];
  if (g.report) sec.push('<a href="'+runBase(g.ts)+'/">files</a>');
  if (g.collected && g.report && !g.pruned) sec.push('<button onclick="report(\''+g.ts+'\')">re-report</button>');
  if (g.collected && !g.pruned && g.report) sec.push('<button onclick="collect(\''+g.ts+'\')">re-collect</button>');
  if (g.pruned) sec.push('<span class="muted" title="ログ削除済み: the raw logs were deleted: the report cannot be rebuilt, deep-dive / task run:pull get the reports only">(re-report 不可)</span>');
  sec.push('<button onclick="benchLog(\''+g.ts+'\')" title="paste the portal\'s bench messages (score, warnings, errors); the report condenses them">bench log</button>');
  return sec;
}
const mb = (b) => (b/1e6).toFixed(1)+" MB";
function selOne(cb){ if(cb.checked) SEL.add(cb.dataset.ts); else SEL.delete(cb.dataset.ts); selShow(); }
function selAll(on){ document.querySelectorAll("input.prunesel").forEach(cb=>{ cb.checked = on; if(on) SEL.add(cb.dataset.ts); else SEL.delete(cb.dataset.ts); }); selShow(); }
function selBytes(){ let n = 0; document.querySelectorAll("input.prunesel").forEach(cb=>{ if(cb.checked) n += Number(cb.dataset.bytes)||0; }); return n; }
function selShow(){ const st = $("#prunest"); if(!st) return; const ts = [...document.querySelectorAll("input.prunesel")].filter(cb=>cb.checked).map(cb=>cb.dataset.ts); st.textContent = ts.length ? ts.length+" run(s) selected · "+mb(selBytes())+" to free" : "select runs (checkbox) to delete their raw logs"; }
async function pruneSelected(){
  const ts = [...document.querySelectorAll("input.prunesel")].filter(cb=>cb.checked).map(cb=>cb.dataset.ts);
  if(!ts.length){ alert("ログを削除する run をチェックしてください"); return; }
  if(!confirm("生ログを削除します（レポートは残す）\n\n"+ts.join("\n")+"\n\n合計 "+mb(selBytes())+" を解放（ops と各ホストのコピー）。\n削除後は re-report / deep-dive / run:pull の生ログは使えません。元に戻せません。")) return;
  $("#prunest").textContent = "deleting…";
  const r = await fetch("/api/prune?ts="+encodeURIComponent(ts.join(",")), {method:"POST"}).then(r=>r.json()).catch(e=>({error:String(e)}));
  ts.forEach(t=>SEL.delete(t));
  alert(Object.entries(r).map(([k,v])=>k+": "+(typeof v==="object"?Object.entries(v).map(([a,b])=>a+"="+b).join(", "):v)).join("\n"));
  loadRuns(); loadFleet();
}
const hhmm = (ts) => { const d = new Date(Date.parse(ts.replace(/(\d{4})(\d{2})(\d{2})-(\d{2})(\d{2})(\d{2})/,"$1-$2-$3T$4:$5:$6Z"))); return isNaN(d) ? ts.slice(9,13) : String(d.getHours()).padStart(2,"0")+":"+String(d.getMinutes()).padStart(2,"0"); };
async function loadTimeline(){
  const rows = await fetch("/api/timeline").then(r=>r.json()).catch(()=>[]);
  const el = $("#timeline"); if(!rows||!rows.length){ el.innerHTML='<div class="muted">collect a run to start the timeline</div>'; return; }
  const scored = rows.filter(r=>r.score!=null && r.score!=="" && !isNaN(parseFloat(r.score)));
  const max = Math.max(1, ...scored.map(r=>parseFloat(r.score)));
  const W=900,H=150,L=50,T=12,B=24; const t0=rows[0].ts, parse=(ts)=>Date.parse(ts.replace(/(\d{4})(\d{2})(\d{2})-(\d{2})(\d{2})(\d{2})/,"$1-$2-$3T$4:$5:$6Z")); const x0=parse(rows[0].ts), x1=parse(rows[rows.length-1].ts)||x0+1;
  const X=(ts)=>L+(W-L-10)*((parse(ts)-x0)/Math.max(1,x1-x0)); const Y=(s)=>T+(H-T-B)-(H-T-B)*s/max;
  let svg='<svg width="'+W+'" height="'+H+'" style="background:#fff;border:1px solid #e5e7eb;border-radius:6px">';
  for(let i=0;i<=4;i++){const y=T+(H-T-B)*i/4; svg+='<line x1="'+L+'" y1="'+y+'" x2="'+(W-10)+'" y2="'+y+'" stroke="#e5e7eb"/><text x="'+(L-4)+'" y="'+(y+3)+'" font-size="10" text-anchor="end" fill="#6b7280">'+Math.round(max*(4-i)/4)+'</text>';}
  const pts=scored.map(r=>X(r.ts).toFixed(1)+","+Y(parseFloat(r.score)).toFixed(1)).join(" ");
  if(scored.length>1) svg+='<polyline fill="none" stroke="#0f766e" stroke-width="1.5" points="'+pts+'"/>';
  for(const r of rows){ const x=X(r.ts); const g=r.git||{}; const tip=esc(r.ts)+((r.pass==="false"||r.pass===false)?" FAIL":(r.score?" score "+esc(r.score):" (no score)"))+(g.commit?" · "+esc(g.branch)+"@"+esc(g.commit)+" "+esc(g.subject):"")+(r.note?" · "+esc(r.note):"")+(r.verdict?"\n"+esc(r.verdict):"")+(r.bench_top?"\nbench: "+esc(r.bench_top):"");
    if(r.score && !isNaN(parseFloat(r.score))) svg+='<circle cx="'+x+'" cy="'+Y(parseFloat(r.score))+'" r="'+((r.pass==="false"||r.pass===false)?6:4)+'" fill="'+((r.err>0||r.pass==="false"||r.pass===false)?"#dc2626":"#0f766e")+'"><title>'+tip+'</title></circle>'; else svg+='<line x1="'+x+'" y1="'+T+'" x2="'+x+'" y2="'+(H-B)+'" stroke="#d1d5db" stroke-dasharray="2 3"><title>'+tip+'</title></line>';
    svg+='<text x="'+x+'" y="'+(H-8)+'" font-size="9" text-anchor="middle" fill="#6b7280">'+hhmm(r.ts)+'</text>'; }
  svg+='</svg>';
  // the per-run list is Runs (commit / req / 5xx / note / report links live there); this table only adds what Runs lacks.
  // Newest first like Runs; collapsed once there are more than 8 runs (the choice is kept across the 60 s refresh)
  let prev=null; const drows=[];
  for(const r of rows){ const s=parseFloat(r.score); const fail=(r.pass==="false"||r.pass===false); const d=(prev!=null&&!isNaN(s)&&!fail)?(s-prev):null; if(!isNaN(s)&&!fail) prev=s; drows.push([r,s,d,fail]); }
  let tbl='<table><tr><th>time</th><th>score</th><th>Δ</th><th title="score ÷ count of the score_post endpoint in alp.yml">score/POST</th><th>verdict / next</th><th title="in-flight requests over the load window (report)">bench</th><th>bench output</th></tr>';
  for(const [r,s,d,fail] of drows.reverse()){
    tbl+='<tr><td class="ts"><a href="#run-'+r.ts+'" title="'+r.ts+' UTC — jump to the run in Runs">'+hhmm(r.ts)+'</a></td><td class="num">'+(fail?'<span class="failtag">FAIL</span>':'<b>'+esc(r.score??"")+'</b>')+'</td><td class="num '+(d<0?"bad":"")+'">'+(d==null?"":(d>0?"+":"")+Math.round(d))+'</td><td class="num" title="'+escA(r.score_post_ep||"")+' × '+(r.score_post||"")+'">'+((r.score_post&&!isNaN(s))?(s/r.score_post).toFixed(2):"")+'</td><td class="muted">'+esc(r.verdict||"")+(r.next?' <b>next:</b> '+esc(r.next):'')+'</td><td class="muted">'+esc(r.shape||"")+'</td><td class="muted">'+esc(r.bench_top||"")+'</td></tr>'; }
  const open = TL_OPEN ?? rows.length <= 8;
  el.innerHTML = svg + '<details class="tl"'+(open?" open":"")+' ontoggle="TL_OPEN=this.open"><summary>per-run analysis ('+rows.length+' runs): score/POST · verdict · next · bench shape — commit / req / note / report are in Runs</summary>' + tbl + '</table></details><div class="muted">times are your local time (agent records UTC). dashed = no score entered yet (type it in the Runs table). Hover a point for commit / note / verdict.</div>';
}
let TL_OPEN = null;
async function benchLog(ts){
  const st = document.getElementById("st-"+ts); if(!st) return;
  let cur = ""; try { const r = await fetch("/api/benchlog?ts="+ts); if(r.ok) cur = await r.text(); } catch(e){}
  st.innerHTML = '<div class="muted">paste the bench output from the portal (score line + messages). Saved as bench.log; the report gets a "Bench output" section.</div><textarea id="bl-'+ts+'" rows="8" style="width:100%%;font-family:ui-monospace,Menlo,monospace;font-size:11px"></textarea><div><button onclick="benchLogSave(\''+ts+'\')">save + re-report</button> <button onclick="document.getElementById(\'st-'+ts+'\').innerHTML=\'\'">cancel</button></div>';
  document.getElementById("bl-"+ts).value = cur;
}
async function benchLogSave(ts){
  const ta = document.getElementById("bl-"+ts); const st = document.getElementById("st-"+ts);
  st.innerHTML = '<span class="muted">saving…</span>';
  const r = await fetch("/api/benchlog?ts="+ts, {method:"POST", body: ta.value}).then(r=>r.json()).catch(e=>({report:String(e)}));
  st.innerHTML = '<span class="muted">bench.log '+(r.bytes||0)+' bytes · report: '+esc(String(r.report||""))+'</span>';
  loadRuns(); loadTimeline();
}
async function copyTs(el, ts){ try { await navigator.clipboard.writeText(ts); const t = el.textContent; el.textContent = "copied"; setTimeout(()=>{ el.textContent = t; }, 900); } catch(e){ prompt("copy:", ts); } }
async function delRun(ts){ const typed = prompt("run "+ts+" を ops と全ホストから削除します（元に戻せません）。\n確認のため ts をそのまま入力:", ""); if (typed === null) return; if (typed.trim() !== ts) { alert("ts が一致しないので削除しません"); return; } const r = await fetch("/api/delete?ts="+ts, {method:"POST"}).then(r=>r.json()).catch(e=>({error:String(e)})); alert(Object.entries(r).map(([k,v])=>k+": "+v).join("\n")); loadRuns(); loadTimeline(); }
async function pause(min){ const r = await fetch("/api/pause?minutes="+min, {method:"POST"}).then(r=>r.json()); loadFleet(); if(min>0) alert("run detection paused for "+min+" min on: "+Object.keys(r).join(", ")+"\n(auto-resumes; press ▶ to resume earlier)"); }
async function retro(){ const r = await fetch("/api/retro").then(r=>r.json()); if(r.retro) window.open(r.retro,"_blank"); else alert(JSON.stringify(r)); }
async function collect(ts){
  const st = document.getElementById("st-"+ts); if(st) st.innerHTML = '<span class="muted">collecting…</span>';
  const r = await fetch("/api/collect?ts="+ts).then(r=>r.json()).catch(e=>({error:String(e)}));
  const msg = Object.entries(r).filter(([k])=>k!=="ts").map(([k,v])=>k+": "+esc(String(v))).join(" · ");
  await loadRuns(); loadTimeline();
  const st2 = document.getElementById("st-"+ts); if(st2) st2.innerHTML = '<span class="muted">'+msg+'</span>';
}
async function report(ts){ const st=document.getElementById("st-"+ts); if(st) st.textContent="building report…"; const res = await fetch("/api/report?ts="+ts); if(!res.ok){ if(st) st.textContent="report failed: "+await res.text(); loadRuns(); return; } const r = await res.json(); if(st) st.textContent=""; if(r.report) window.open(r.report, "_blank"); loadRuns(); }
async function note(ts, data){ await fetch("/api/note",{method:"POST",headers:{"content-type":"application/json"},body:JSON.stringify({ts,...data})}); loadTimeline(); }
loadApp(); loadFleet(); loadRuns(); loadTimeline(); setInterval(loadApp, 30000); setInterval(loadFleet, 5000); setInterval(loadRuns, 30000); setInterval(loadTimeline, 60000);
</script></body></html>`

// coordinate makes a bench one run across the fleet: when a host starts a run from the access log (the bench target),
// every other idle host is told to start a shadow run with the same ts (the DB host sees no HTTP, so it would never
// start on its own); when the leader ends, the shadows are stopped; when the leader discarded its run (not a bench),
// the shadows are deleted. Polls every 3 s.
func (p *panel) coordinate() {
	if len(p.hosts) < 2 {
		return
	}
	post := func(ctx context.Context, url string) error {
		ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		req, _ := http.NewRequestWithContext(ctx, "POST", url, nil)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			return err
		}
		res.Body.Close()
		return nil
	}
	lastLeader := ""
	warnedOld := map[string]bool{}
	recentRun := map[string]string{} // host -> ts of the run last seen running there (so a host whose own run just ended is not shadowed for the same bench)
	for {
		time.Sleep(3 * time.Second)
		ctx := context.Background()
		fl := p.fleet(ctx, false)
		leaderTS, leaderHost := "", ""
		type hs struct {
			h      panelHost
			state  string
			ts     string
			follow bool
			err    bool
		}
		var hosts []hs
		for i, st := range fl {
			x := hs{h: p.hosts[i]}
			if st["error"] != nil {
				x.err = true
			}
			x.state, _ = st["state"].(string)
			manual := false
			if run, ok := st["run"].(map[string]any); ok {
				x.ts, _ = run["ts"].(string)
				x.follow, _ = run["follow"].(bool)
				manual, _ = run["manual"].(bool)
			}
			// an agent older than 0.2.0 turns /start?follow=1 into a manual run → never shadow it, never treat it as a leader
			// (otherwise the fleet loops: manual run → "leader" → shadows → idle → repeat)
			if v, _ := st["version"].(string); v < "0.2.0" {
				x.err = true
				if !warnedOld[x.h.Name] {
					warnedOld[x.h.Name] = true
					log.Printf("coordinate: agent on %s is %s (< 0.2.0): no shadow runs until `task host:agent HOST=%s`", x.h.Name, v, x.h.Addr)
				}
			}
			if x.state == "running" && x.ts != "" {
				recentRun[x.h.Name] = x.ts
			}
			hosts = append(hosts, x)
			// leader = a run started from the access log or from MySQL load (db-trigger). Manual and shadow runs never lead.
			if x.state == "running" && !x.follow && !manual && !x.err && (leaderTS == "" || x.ts < leaderTS) {
				leaderTS, leaderHost = x.ts, x.h.Name
			}
		}
		if leaderTS != "" {
			if leaderTS != lastLeader {
				log.Printf("coordinate: run %s on %s is the leader; shadows on the other hosts", leaderTS, leaderHost)
				lastLeader = leaderTS
			}
			for _, x := range hosts {
				if x.err || x.state == "running" || within(recentRun[x.h.Name], leaderTS, 20*time.Second) {
					continue
				}
				if err := post(ctx, x.h.Agent+"/start?follow=1&ts="+leaderTS+"&leader="+leaderHost); err != nil {
					log.Printf("coordinate: start shadow on %s: %v", x.h.Name, err)
				}
			}
			continue
		}
		// no leader: stop shadows; if the leader's run no longer exists (discarded), delete the shadows too
		for _, x := range hosts {
			if x.err || x.state != "running" || !x.follow {
				continue
			}
			post(ctx, x.h.Agent+"/stop")
			if lastLeader != "" && !p.runExists(ctx, lastLeader) {
				req, _ := http.NewRequestWithContext(ctx, "DELETE", x.h.Agent+"/runs/"+x.ts, nil)
				if res, err := http.DefaultClient.Do(req); err == nil {
					res.Body.Close()
					log.Printf("coordinate: leader run %s was discarded; deleted shadow on %s", lastLeader, x.h.Name)
				}
			}
		}
		if lastLeader != "" {
			lastLeader = ""
		}
	}
}

// runExists reports whether any host still has run ts (within 20 s) in its run list.
func (p *panel) runExists(ctx context.Context, ts string) bool {
	for _, h := range p.hosts {
		var rs []map[string]any
		if getJSON(ctx, h.Agent+"/runs", &rs, 4*time.Second) != nil {
			continue
		}
		for _, r := range rs {
			if t, _ := r["ts"].(string); t != "" && within(t, ts, 20*time.Second) {
				if f, _ := r["follow"].(bool); !f {
					return true
				}
			}
		}
	}
	return false
}

// handlePause relays "not benchmarking now" to every agent: triggers are ignored for N minutes (auto-resume so a real
// bench is never missed because someone forgot). minutes=0 resumes immediately.
func (p *panel) handlePause(w http.ResponseWriter, r *http.Request) {
	mins := r.URL.Query().Get("minutes")
	if mins == "" {
		mins = "30"
	}
	out := map[string]any{}
	for _, h := range p.hosts {
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		req, _ := http.NewRequestWithContext(ctx, "POST", h.Agent+"/pause?minutes="+mins, nil)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			out[h.Name] = "error: " + err.Error()
		} else {
			var v map[string]any
			json.NewDecoder(res.Body).Decode(&v)
			res.Body.Close()
			out[h.Name] = v["paused_until"]
		}
		cancel()
	}
	writeJSONResp(w, out)
}

// handleDelete removes a (mis-detected or unwanted) run: the collected directory on ops, the run on every host whose
// ts is within 20 s, and the score/note entry.
func (p *panel) handleDelete(w http.ResponseWriter, r *http.Request) {
	ts := r.URL.Query().Get("ts")
	if ts == "" || strings.ContainsAny(ts, "/\\.") {
		http.Error(w, "ts required", 400)
		return
	}
	result := map[string]any{"ts": ts}
	for _, h := range p.hosts {
		var rs []map[string]any
		if err := getJSON(r.Context(), h.Agent+"/runs", &rs, 5*time.Second); err != nil {
			result[h.Name] = "agent unreachable"
			continue
		}
		n := 0
		for _, run := range rs {
			if t, _ := run["ts"].(string); t != "" && within(ts, t, 20*time.Second) {
				req, _ := http.NewRequestWithContext(r.Context(), "DELETE", h.Agent+"/runs/"+t, nil)
				if res, err := http.DefaultClient.Do(req); err == nil {
					res.Body.Close()
					n++
				}
			}
		}
		result[h.Name] = fmt.Sprintf("%d run(s) deleted on host", n)
	}
	dir := filepath.Join(p.contestDir("measurements"), ts)
	if _, err := os.Stat(dir); err == nil {
		os.RemoveAll(dir)
		result["ops"] = "deleted " + dir
	}
	p.mu.Lock()
	delete(p.notes, ts)
	p.saveNotes()
	p.mu.Unlock()
	writeJSONResp(w, result)
}
