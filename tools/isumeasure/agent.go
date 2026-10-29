package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type agentConfig struct {
	Contest       string
	Listen        string
	DataDir       string
	AccessLog     string
	SlowLog       string
	NginxErrLog   string
	MySQLErrLog   string
	ExpvarURL     string
	PprofURL      string
	StubURL       string
	MySQL         bool
	Repo          string
	EnvFile       string
	Units         string
	TriggerRe     string
	IgnoreRe      string
	IdleGap       time.Duration
	MinDuration   time.Duration
	MinReq        int     // auto-started runs with fewer requests in the first 30 s are discarded (someone just hit /initialize)
	DBTrigger     float64 // start a run when MySQL serves >= this many queries/s for 3 s (DB hosts see no HTTP); 0 = off
	MaxDuration   time.Duration
	PprofDelay    time.Duration
	PprofSeconds  int
	ProcessListEv time.Duration
	Rotate        bool
	KeepRuns      int
	MinFreeGB     float64
}

type runInfo struct {
	TS           string              `json:"ts"`
	Contest      string              `json:"contest"`
	Host         string              `json:"host"`
	Start        time.Time           `json:"start"`
	End          time.Time           `json:"end,omitempty"`
	DurationSec  float64             `json:"duration_sec"`
	Trigger      string              `json:"trigger"`
	ReqTotal     int                 `json:"req_total"`
	ErrTotal     int                 `json:"err_total"`
	AccessFrom   int64               `json:"access_log_from"`
	AccessTo     int64               `json:"access_log_to"`
	SlowFrom     int64               `json:"slow_log_from"`
	SlowTo       int64               `json:"slow_log_to"`
	Git          map[string]any      `json:"git,omitempty"`
	Services     []map[string]string `json:"services,omitempty"`
	Files        []string            `json:"files,omitempty"`
	Pprof        map[string]string   `json:"pprof,omitempty"`
	Manual       bool                `json:"manual"`
	Follow       bool                `json:"follow"` // started by the ops panel to shadow a run on another host (DB host etc.)
	EndReason    string              `json:"end_reason,omitempty"`
	NginxErrFrom int64               `json:"nginx_error_from"`
	MySQLErrFrom int64               `json:"mysql_error_from"`
	CPUs         int                 `json:"cpus,omitempty"` // runtime.NumCPU(): the report draws the CPU ceiling (cores × 100%)
	// end-detection diagnostics (why the run ended when it did; the panel and summary.md show one line from these)
	EndedAt    string   `json:"ended_at,omitempty"`     // = end, RFC3339
	LastReqAt  string   `json:"last_req_at,omitempty"`  // last counted (non-ignored) request during the run
	IdleSec    float64  `json:"idle_sec,omitempty"`     // end - last request (HTTP runs) or end - last DB activity (mysql: runs)
	PeakRPS    int      `json:"peak_rps,omitempty"`     // busiest second of the run
	PeakAt     string   `json:"peak_at,omitempty"`      // when it was
	TailReqs   []string `json:"tail_reqs,omitempty"`    // last requests seen ("hh:mm:ss.mmm METHOD uri status remote"): what kept the run open
	DBActiveAt string   `json:"db_active_at,omitempty"` // mysql: runs: last sample with meaningful MySQL load
	DBPeakQPS  float64  `json:"db_peak_qps,omitempty"`  // mysql: runs: peak questions/s
	DBStopQPS  float64  `json:"db_stop_qps,omitempty"`  // mysql: runs: below this the DB counts as idle (max(db-trigger/10, 5% of peak))
	StopBy     string   `json:"stop_by,omitempty"`      // /stop?reason=… (bench-done: the panel got the "bench finished" signal)
	Final      bool     `json:"final"`                  // true once the post-run work (log slices, journal, DB snapshots) is written: safe to collect
}

// tailReqN: how many of the last requests a run keeps for the end diagnostics.
const tailReqN = 8

// perSecond aggregates access-log events for one wall-clock second.
type perSecond struct {
	T      string  `json:"t"`
	Req    int     `json:"req"`
	Err5xx int     `json:"err_5xx"`
	Err4xx int     `json:"err_4xx"`
	SumRT  float64 `json:"reqtime_sum"`
	MaxRT  float64 `json:"reqtime_max"`
	P99RT  float64 `json:"reqtime_p99"`
	rts    []float64
}

type agent struct {
	cfg      agentConfig
	host     string
	trigger  *regexp.Regexp
	ignore   *regexp.Regexp
	tail     *tailer
	mu       sync.Mutex
	state    string // idle | running
	run      *runInfo
	runDir   string
	secs     map[string]*perSecond
	lastReq  time.Time
	reqCount int
	errCount int
	recent   []sysSample // ring of last 120 sys samples (always on)
	rps      []int       // ring of req/s for last 60s (always on)
	writers  map[string]*jsonlWriter
	prevSys  sysRaw
	prevProc map[int]procRaw
	prevT    time.Time
	topProcs []procSample
	prevMy   mysqlRaw
	haveMy   bool
	stopRun  chan struct{}
	ctx      context.Context
	cancel   context.CancelFunc
	started  time.Time
	lastErr  string
	pausedTo time.Time // while in the future, access-log triggers do not start runs ("not benchmarking now" from the panel)
	qpsHist  []float64 // last MySQL questions/s samples (db-trigger)
	qpsPeak  float64   // peak questions/s during the current run
	dbActive time.Time // last sample with meaningful MySQL load during a db-triggered run
	tailReqs []string  // ring of the last tailReqN requests of the current run (end diagnostics)
	peakRPS  int       // busiest second of the current run
	peakAt   time.Time
}

func runAgent(args []string) error {
	fs := flag.NewFlagSet("agent", flag.ContinueOnError)
	var c agentConfig
	fs.StringVar(&c.Contest, "contest", envOr("ISUMEASURE_CONTEST", "practice"), "contest name recorded in each run")
	fs.StringVar(&c.Listen, "listen", "127.0.0.1:19100", "status API address")
	fs.StringVar(&c.DataDir, "data", "/var/lib/isumeasure", "where runs are stored")
	fs.StringVar(&c.AccessLog, "access", "/var/log/nginx/access.log", "nginx access log (ltsv or combined)")
	fs.StringVar(&c.SlowLog, "slow", "/var/log/mysql/slow.log", "mysql slow log ('' to disable)")
	fs.StringVar(&c.NginxErrLog, "nginx-error", "/var/log/nginx/error.log", "nginx error log to slice per run ('' to disable)")
	fs.StringVar(&c.MySQLErrLog, "mysql-error", "/var/log/mysql/error.log", "mysql error log to slice per run ('' to disable)")
	fs.StringVar(&c.ExpvarURL, "expvar", "http://127.0.0.1:6060/debug/vars", "expvar URL ('' to disable)")
	fs.StringVar(&c.PprofURL, "pprof", "http://127.0.0.1:6060", "pprof base URL ('' to disable)")
	fs.StringVar(&c.StubURL, "stub", "", "nginx stub_status URL (e.g. http://127.0.0.1/nginx_status)")
	fs.BoolVar(&c.MySQL, "mysql", true, "sample SHOW GLOBAL STATUS via the mysql CLI")
	fs.StringVar(&c.Repo, "repo", "/home/isucon", "git repo for branch/commit info")
	fs.StringVar(&c.EnvFile, "env", "/home/isucon/env.sh", "env file summarized in status (secrets masked)")
	fs.StringVar(&c.Units, "units", "", "comma-separated systemd units to report (default: auto-discover)")
	fs.StringVar(&c.TriggerRe, "trigger", `^POST (/api)?/initialize`, "regex on 'METHOD uri' that starts a run")
	fs.StringVar(&c.IgnoreRe, "ignore", `/internal/`, "regex on 'METHOD uri' for requests that are not bench traffic (internal pollers); excluded from counts and idle detection")
	fs.DurationVar(&c.IdleGap, "idle", 10*time.Second, "end the run after this long without requests")
	fs.DurationVar(&c.MinDuration, "min", 20*time.Second, "minimum run length before idle detection applies")
	fs.Float64Var(&c.DBTrigger, "db-trigger", 100, "start a run when MySQL questions/s stays >= this for 3 samples (for the DB host, which sees no HTTP); ends when it drops; 0 = off")
	fs.IntVar(&c.MinReq, "min-req", 100, "auto-started runs with fewer requests in the first 30s are discarded (a lone /initialize is not a bench); 0 = keep all")
	fs.DurationVar(&c.MaxDuration, "max", 5*time.Minute, "maximum run length")
	fs.DurationVar(&c.PprofDelay, "pprof-delay", 20*time.Second, "capture pprof this long after the trigger")
	fs.IntVar(&c.PprofSeconds, "pprof-seconds", 30, "CPU/fgprof profile length")
	fs.DurationVar(&c.ProcessListEv, "processlist", 5*time.Second, "mysql processlist snapshot interval during a run")
	fs.BoolVar(&c.Rotate, "rotate", true, "after each run rotate the raw access/slow logs (nginx reopen, FLUSH SLOW LOGS) and delete the old files so they never grow unbounded")
	fs.IntVar(&c.KeepRuns, "keep", 3, "keep at most this many runs on the host (oldest are deleted; collected runs are deleted by the panel)")
	fs.Float64Var(&c.MinFreeGB, "min-free-gb", 1.0, "skip copying the slow log slice when free disk is below this")
	if err := fs.Parse(args); err != nil {
		return err
	}
	re, err := regexp.Compile(c.TriggerRe)
	if err != nil {
		return fmt.Errorf("bad -trigger: %w", err)
	}
	var ign *regexp.Regexp
	if c.IgnoreRe != "" {
		if ign, err = regexp.Compile(c.IgnoreRe); err != nil {
			return fmt.Errorf("bad -ignore: %w", err)
		}
	}
	host, _ := os.Hostname()
	if err := os.MkdirAll(filepath.Join(c.DataDir, "runs"), 0755); err != nil {
		return err
	}
	// close runs left open by a previous agent process
	if entries, err := os.ReadDir(filepath.Join(c.DataDir, "runs")); err == nil {
		for _, e := range entries {
			rp := filepath.Join(c.DataDir, "runs", e.Name(), "run.json")
			var ri map[string]any
			if b, err := os.ReadFile(rp); err == nil {
				json.Unmarshal(b, &ri)
			}
			if ri == nil || ri["end_reason"] == nil || ri["end_reason"] == "" {
				if ri == nil {
					ri = map[string]any{"ts": e.Name(), "host": host, "contest": c.Contest}
				}
				ri["end_reason"] = "aborted"
				writeJSON(rp, ri)
			}
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	a := &agent{cfg: c, host: host, trigger: re, ignore: ign, state: "idle", secs: map[string]*perSecond{}, writers: map[string]*jsonlWriter{}, ctx: ctx, cancel: cancel, started: time.Now()}
	log.Printf("isumeasure agent %s on %s contest=%s data=%s access=%s listen=%s", version, host, c.Contest, c.DataDir, c.AccessLog, c.Listen)

	// tail the access log
	a.tail = newTailer(c.AccessLog)
	stop := make(chan struct{})
	go a.tail.run(stop, a.onAccessLine)
	// always-on 1s sampler (sys to ring; everything to files while a run is active)
	go a.sampleLoop()
	// http API
	mux := http.NewServeMux()
	mux.HandleFunc("/status", a.handleStatus)
	mux.HandleFunc("/runs", a.handleRuns)
	mux.HandleFunc("/runs/", a.handleRunFile)
	mux.HandleFunc("/start", a.handleStart)
	mux.HandleFunc("/stop", a.handleStop)
	mux.HandleFunc("/pause", a.handlePause) // POST /pause?minutes=30 : ignore triggers for N minutes (0 = resume)
	mux.HandleFunc("/tail", a.handleTail)
	srv := &http.Server{Addr: c.Listen, Handler: mux}
	return srv.ListenAndServe()
}

func envOr(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

// ---- access log events ----

func (a *agent) onAccessLine(line string, offset int64) {
	ev := parseAccessLine(line, time.Now())
	if ev.method == "" {
		return
	}
	if a.ignore != nil && a.ignore.MatchString(ev.method+" "+ev.uri) {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.lastReq = time.Now()
	key := secKey(ev.at)
	ps := a.secs[key]
	if ps == nil {
		ps = &perSecond{T: key}
		a.secs[key] = ps
	}
	ps.Req++
	if ev.status >= 500 {
		ps.Err5xx++
	} else if ev.status >= 400 {
		ps.Err4xx++
	}
	ps.SumRT += ev.reqtime
	if ev.reqtime > ps.MaxRT {
		ps.MaxRT = ev.reqtime
	}
	ps.rts = append(ps.rts, ev.reqtime)
	if a.state == "running" {
		a.reqCount++
		if ev.status >= 500 {
			a.errCount++
		}
		a.tailReqs = append(a.tailReqs, fmt.Sprintf("%s %s %s %d %s", ev.at.Format("15:04:05.000"), ev.method, ev.uri, ev.status, ev.remote))
		if len(a.tailReqs) > tailReqN {
			a.tailReqs = a.tailReqs[len(a.tailReqs)-tailReqN:]
		}
	} else if a.trigger.MatchString(ev.method+" "+ev.uri) && time.Now().After(a.pausedTo) {
		a.startRunLocked(ev.method+" "+ev.uri, offset, false)
	}
}

// ---- run lifecycle ----

// runTS / secKey: run names and per-second keys are always UTC, whatever the host's TZ. The panel, runs:wait and the
// timeline sort names as strings and read them as UTC; the readers take zone-less keys in the run's start location
// (parseSec), and start is written in UTC too. 2026-10-28 (ISUCON11 AMI, host TZ JST): local names and keys shifted
// the load window 9 h, and a TZ=UTC workaround mid-contest mixed JST and UTC names (runs:wait and bench-done missed runs).
func runTS(t time.Time) string { return t.UTC().Format("20060102-150405") }

func secKey(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05") }

func (a *agent) startRunLocked(trigger string, accessOffset int64, manual bool) {
	a.startRunAs(trigger, accessOffset, manual, false, "")
}

// startRunAs: follow=true runs are started by the panel with the leader's ts; they never end on idle (no HTTP here) and are never discarded.
func (a *agent) startRunAs(trigger string, accessOffset int64, manual, follow bool, ts string) {
	if ts == "" {
		ts = runTS(time.Now())
	}
	dir := filepath.Join(a.cfg.DataDir, "runs", ts)
	if err := os.MkdirAll(dir, 0755); err != nil {
		a.lastErr = err.Error()
		return
	}
	a.run = &runInfo{TS: ts, Contest: a.cfg.Contest, Host: a.host, Start: time.Now().UTC(), Trigger: trigger, AccessFrom: accessOffset, SlowFrom: fileSize(a.cfg.SlowLog), NginxErrFrom: fileSize(a.cfg.NginxErrLog), MySQLErrFrom: fileSize(a.cfg.MySQLErrLog), Manual: manual, Follow: follow, CPUs: runtime.NumCPU()}
	a.runDir = dir
	a.state = "running"
	a.reqCount, a.errCount = 0, 0
	a.tailReqs, a.peakRPS, a.peakAt = nil, 0, time.Time{}
	a.secs = map[string]*perSecond{}
	a.rps = a.rps[:0]
	for _, n := range []string{"sys", "mysql", "app", "nginx", "http", "procs"} {
		if w, err := newJSONL(filepath.Join(dir, n+".jsonl")); err == nil {
			a.writers[n] = w
		}
	}
	a.stopRun = make(chan struct{})
	log.Printf("run %s started (trigger=%q manual=%v follow=%v)", ts, trigger, manual, follow)
	go a.runWatcher(a.stopRun, dir)
	if a.cfg.MySQL {
		go func() { // rows right after /initialize (the trigger line is logged when initialize has completed)
			if rc := rowCounts(a.ctx); rc != "" {
				os.WriteFile(filepath.Join(dir, "rows-start.txt"), []byte(rc), 0644)
			}
		}()
	}
}

// runWatcher ends the run on idle/max, captures pprof, snapshots processlist.
func (a *agent) runWatcher(stop chan struct{}, dir string) {
	start := time.Now()
	pprofDone := false
	probed := false
	plTick := time.NewTicker(a.cfg.ProcessListEv)
	defer plTick.Stop()
	check := time.NewTicker(1 * time.Second)
	defer check.Stop()
	plFile, _ := os.OpenFile(filepath.Join(dir, "processlist.txt"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if plFile != nil {
		defer plFile.Close()
	}
	for {
		select {
		case <-stop:
			return
		case <-plTick.C:
			if a.cfg.MySQL && plFile != nil {
				out := mysqlProcesslist(a.ctx)
				if strings.TrimSpace(out) != "" {
					fmt.Fprintf(plFile, "=== %s\n%s\n", time.Now().Format(time.RFC3339), out)
				}
			}
		case <-check.C:
			el := time.Since(start)
			if !pprofDone && el >= a.cfg.PprofDelay {
				pprofDone = true
				if a.cfg.PprofURL != "" {
					go a.capturePprof(dir)
				}
				if a.cfg.MySQL {
					go func() {
						os.WriteFile(filepath.Join(dir, "innodb-status-mid.txt"), []byte(mysqlQuery(a.ctx, "SHOW ENGINE INNODB STATUS\\G")), 0644)
					}()
				}
			}
			a.mu.Lock()
			idleFor := time.Since(a.lastReq)
			quiet := a.quietForLocked(a.cfg.IdleGap)
			follow, manual, dbRun, reqs := false, false, false, 0
			dbIdle := time.Since(a.dbActive)
			if a.run != nil {
				follow, manual, reqs = a.run.Follow, a.run.Manual, a.reqCount
				dbRun = strings.HasPrefix(a.run.Trigger, "mysql:")
			}
			a.mu.Unlock()
			reason := ""
			if el >= a.cfg.MaxDuration {
				reason = "max"
			} else if dbRun {
				// db-triggered run (no HTTP here): ends when MySQL load has been gone for IdleGap
				if el >= a.cfg.MinDuration && dbIdle >= a.cfg.IdleGap {
					reason = "idle"
				}
			} else if !follow && !manual && a.cfg.MinReq > 0 && !probed && el >= 30*time.Second {
				probed = true
				if reqs < a.cfg.MinReq {
					reason = "discarded"
				}
			} else if !follow && el >= a.cfg.MinDuration && (idleFor >= a.cfg.IdleGap || quiet) {
				reason = "idle"
			}
			// an auto-started HTTP run that ends (idle/max) with almost no traffic was a lone /initialize, not a bench
			if reason != "" && reason != "discarded" && !follow && !manual && !dbRun && a.cfg.MinReq > 0 && reqs < a.cfg.MinReq {
				reason = "discarded"
			}
			if reason != "" {
				a.mu.Lock()
				if a.state == "running" && a.stopRun == stop {
					a.endRunLocked(reason)
				}
				a.mu.Unlock()
				return
			}
		}
	}
}

// quietForLocked reports whether traffic over the last `gap` has stayed at or below 5% of the run's peak req/s
// (stray requests after a bench — health checks, pollers — must not keep a run open forever).
func (a *agent) quietForLocked(gap time.Duration) bool {
	n := int(gap.Seconds())
	if len(a.rps) < n+1 || a.run == nil {
		return false
	}
	peak := 0
	for _, v := range a.rps {
		if v > peak {
			peak = v
		}
	}
	if peak < 10 {
		return false // too little traffic to judge; fall back to the pure idle timer
	}
	limit := float64(peak) * 0.05
	for _, v := range a.rps[len(a.rps)-n:] {
		if float64(v) > limit {
			return false
		}
	}
	return true
}

func (a *agent) capturePprof(dir string) {
	secs := a.cfg.PprofSeconds
	files := map[string]string{"cpu": fmt.Sprintf("%s/debug/pprof/profile?seconds=%d", a.cfg.PprofURL, secs), "fgprof": fmt.Sprintf("%s/debug/fgprof?seconds=%d", a.cfg.PprofURL, secs), "heap": a.cfg.PprofURL + "/debug/pprof/heap", "goroutine": a.cfg.PprofURL + "/debug/pprof/goroutine?debug=1", "block": a.cfg.PprofURL + "/debug/pprof/block", "mutex": a.cfg.PprofURL + "/debug/pprof/mutex"}
	var wg sync.WaitGroup
	res := map[string]string{}
	var mu sync.Mutex
	for name, url := range files {
		wg.Add(1)
		go func(name, url string) {
			defer wg.Done()
			ext := ".pb.gz"
			if name == "goroutine" {
				ext = ".txt"
			}
			path := filepath.Join(dir, name+ext)
			t0 := time.Now()
			err := fetchToFile(a.ctx, url, path, time.Duration(secs+15)*time.Second)
			mu.Lock()
			if err != nil {
				// what was asked, how long it took and what came back: the report shows this as the reason
				msg := err.Error()
				if strings.Contains(msg, "deadline exceeded") || strings.Contains(msg, "Client.Timeout") {
					msg = fmt.Sprintf("timeout after %.0fs", time.Since(t0).Seconds())
				} else if strings.Contains(msg, "connection refused") {
					msg = "connection refused (nothing listens on the pprof address — is the app's pprof listener in this build?)"
				}
				res[name] = fmt.Sprintf("error: GET %s: %s (%.1fs)", url, msg, time.Since(t0).Seconds())
				if name == "fgprof" && strings.Contains(err.Error(), "404") {
					res[name] = "not enabled (optional: github.com/felixge/fgprof on /debug/fgprof)"
				}
				os.Remove(path)
			} else {
				res[name] = filepath.Base(path)
			}
			mu.Unlock()
		}(name, url)
	}
	wg.Wait()
	a.mu.Lock()
	if a.run != nil && a.runDir == dir {
		a.run.Pprof = res
	}
	a.mu.Unlock()
	log.Printf("pprof captured: %v", res)
}

func (a *agent) endRunLocked(reason string) {
	r := a.run
	dir := a.runDir
	if r == nil {
		return
	}
	close(a.stopRun)
	if reason == "discarded" {
		// not a bench (e.g. someone ran /initialize by hand): drop everything, do not rotate logs
		for _, w := range a.writers {
			w.Close()
		}
		a.writers = map[string]*jsonlWriter{}
		a.state, a.run, a.runDir = "idle", nil, ""
		os.RemoveAll(dir)
		log.Printf("run %s discarded: only %d requests in the first 30s (min-req %d)", r.TS, a.reqCount, a.cfg.MinReq)
		return
	}
	r.End = time.Now().UTC()
	r.DurationSec = round1(r.End.Sub(r.Start).Seconds())
	r.ReqTotal, r.ErrTotal = a.reqCount, a.errCount
	r.EndReason = reason
	a.fillEndDiagLocked(r)
	r.AccessTo = a.tail.Offset()
	r.SlowTo = fileSize(a.cfg.SlowLog)
	// flush per-second http series
	if w := a.writers["http"]; w != nil {
		keys := make([]string, 0, len(a.secs))
		for k := range a.secs {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			ps := a.secs[k]
			if len(ps.rts) > 0 {
				sort.Float64s(ps.rts)
				ps.P99RT = ps.rts[int(float64(len(ps.rts)-1)*0.99)]
			}
			w.Write(ps)
		}
	}
	for _, w := range a.writers {
		w.Close()
	}
	a.writers = map[string]*jsonlWriter{}
	a.state = "idle"
	a.run = nil
	a.runDir = ""
	// slice logs (outside the lock would be nicer, but keep it simple: these are a few MB)
	go func() {
		if n, err := copyRangeGz(a.cfg.AccessLog, filepath.Join(dir, "access.log.gz"), r.AccessFrom, r.AccessTo); err == nil {
			r.Files = append(r.Files, fmt.Sprintf("access.log.gz (%d bytes raw)", n))
		} else {
			r.Files = append(r.Files, "access.log.gz: "+err.Error())
		}
		if a.cfg.SlowLog != "" && r.SlowTo > r.SlowFrom {
			if free := readSys().diskFreeGB; free < a.cfg.MinFreeGB {
				r.Files = append(r.Files, fmt.Sprintf("slow.log: SKIPPED (disk free %.1f GB < %.1f GB)", free, a.cfg.MinFreeGB))
			} else if n, err := copyRangeGzSQL(a.cfg.SlowLog, filepath.Join(dir, "slow.log.gz"), r.SlowFrom, r.SlowTo); err == nil {
				r.Files = append(r.Files, fmt.Sprintf("slow.log.gz (%d bytes raw, literals > %d bytes collapsed, %d bytes gz)", n, maxSQLLiteral, fileSize(filepath.Join(dir, "slow.log.gz"))))
			} else {
				r.Files = append(r.Files, "slow.log.gz: "+err.Error())
			}
		}
		if a.cfg.Rotate {
			a.rotateRawLogs()
		}
		a.enforceRetention()
		// error logs for the run window, app journal, DB snapshots (all best-effort)
		if a.cfg.NginxErrLog != "" {
			if to := fileSize(a.cfg.NginxErrLog); to > r.NginxErrFrom {
				copyRange(a.cfg.NginxErrLog, filepath.Join(dir, "nginx-error.log"), r.NginxErrFrom, to)
				r.Files = append(r.Files, "nginx-error.log")
			}
		}
		if a.cfg.MySQLErrLog != "" {
			if to := fileSize(a.cfg.MySQLErrLog); to > r.MySQLErrFrom {
				copyRange(a.cfg.MySQLErrLog, filepath.Join(dir, "mysql-error.log"), r.MySQLErrFrom, to)
				r.Files = append(r.Files, "mysql-error.log")
			}
		}
		var appUnits []string
		for _, u := range a.units() {
			l := strings.ToLower(u)
			if strings.HasPrefix(l, "nginx") || strings.HasPrefix(l, "mysql") || strings.HasPrefix(l, "mariadb") || strings.HasPrefix(l, "isumeasure") {
				continue
			}
			appUnits = append(appUnits, u)
		}
		if len(appUnits) > 0 {
			args := []string{"--no-pager", "-o", "short-iso", "--since", r.Start.Add(-2 * time.Second).Format("2006-01-02 15:04:05"), "--until", r.End.Add(2 * time.Second).Format("2006-01-02 15:04:05")}
			for _, u := range appUnits {
				args = append(args, "-u", u)
			}
			ctx2, cancel := context.WithTimeout(a.ctx, 30*time.Second)
			if out, err := exec.CommandContext(ctx2, "journalctl", args...).Output(); err == nil {
				if len(out) > 20<<20 {
					out = out[len(out)-20<<20:]
				}
				os.WriteFile(filepath.Join(dir, "app.log"), out, 0644)
				r.Files = append(r.Files, fmt.Sprintf("app.log (%d bytes, units %s)", len(out), strings.Join(appUnits, ",")))
			}
			cancel()
		}
		// knobs' current values for the report's Tuning section (best-effort, no error if absent)
		if out, err := exec.CommandContext(a.ctx, "sh", "-c", "nginx -T 2>/dev/null | grep -E '^[[:space:]]*(worker_processes|worker_connections|worker_rlimit_nofile|keepalive|keepalive_requests|keepalive_timeout|proxy_http_version|http2|gzip|sendfile|open_file_cache|upstream|listen)' | sed 's/^[[:space:]]*//; s/;$//' | awk '{k=$1; $1=\"\"; sub(/^ /,\"\"); print k\"\\t\"$0}'").Output(); err == nil && len(out) > 0 {
			os.WriteFile(filepath.Join(dir, "nginx-conf.txt"), out, 0644)
			r.Files = append(r.Files, "nginx-conf.txt")
		}
		if len(appUnits) > 0 {
			var sb strings.Builder
			for _, u := range appUnits {
				if out, err := exec.CommandContext(a.ctx, "systemctl", "show", u, "-p", "Environment", "-p", "LimitNOFILE", "-p", "MainPID", "-p", "NRestarts").Output(); err == nil {
					for _, l := range strings.Split(string(out), "\n") {
						if strings.HasPrefix(l, "Environment=") { // GOGC=200 GOMEMLIMIT=... → one line each
							for _, kv := range strings.Fields(strings.TrimPrefix(l, "Environment=")) {
								sb.WriteString(kv + "\n")
							}
						} else if l != "" {
							sb.WriteString(l + "\n")
						}
					}
				}
			}
			os.WriteFile(filepath.Join(dir, "svc-env.txt"), []byte(sb.String()), 0644)
			r.Files = append(r.Files, "svc-env.txt")
		}
		if a.cfg.MySQL {
			os.WriteFile(filepath.Join(dir, "innodb-status.txt"), []byte(mysqlQuery(a.ctx, "SHOW ENGINE INNODB STATUS\\G")), 0644)
			cat := mysqlQuery(a.ctx, "SHOW GLOBAL VARIABLES WHERE Variable_name IN ('innodb_buffer_pool_size','max_connections','innodb_flush_log_at_trx_commit','sync_binlog','log_bin','slow_query_log','long_query_time','innodb_io_capacity','table_open_cache','thread_cache_size','innodb_log_file_size','tmp_table_size','max_heap_table_size','wait_timeout','version')") +
				"\n--- tables (rows are estimates)\n" + mysqlQuery(a.ctx, "SELECT table_schema, table_name, table_rows, ROUND(data_length/1048576,1) AS data_mb, ROUND(index_length/1048576,1) AS index_mb FROM information_schema.tables WHERE table_schema NOT IN ('mysql','sys','information_schema','performance_schema') ORDER BY table_schema, data_length DESC") +
				"\n--- indexes\n" + mysqlQuery(a.ctx, "SELECT table_name, index_name, non_unique, GROUP_CONCAT(column_name ORDER BY seq_in_index) AS columns FROM information_schema.statistics WHERE table_schema NOT IN ('mysql','sys','information_schema','performance_schema') GROUP BY table_name, index_name, non_unique ORDER BY table_name, index_name") +
				"\n--- columns\n" + mysqlQuery(a.ctx, "SELECT table_name, column_name FROM information_schema.columns WHERE table_schema NOT IN ('mysql','sys','information_schema','performance_schema') ORDER BY table_name, ordinal_position") +
				"\n--- global status (selected)\n" + mysqlQuery(a.ctx, "SHOW GLOBAL STATUS WHERE Variable_name IN ('Innodb_buffer_pool_read_requests','Innodb_buffer_pool_reads','Innodb_row_lock_waits','Innodb_row_lock_time_max','Innodb_deadlocks','Created_tmp_disk_tables','Select_full_join','Select_scan','Sort_merge_passes','Table_locks_waited','Threads_created','Max_used_connections','Aborted_connects','Connection_errors_max_connections')")
			os.WriteFile(filepath.Join(dir, "db-catalog.txt"), []byte(cat), 0644)
			r.Files = append(r.Files, "innodb-status.txt", "db-catalog.txt")
			if rc := rowCounts(a.ctx); rc != "" {
				os.WriteFile(filepath.Join(dir, "rows-end.txt"), []byte(rc), 0644)
				r.Files = append(r.Files, "rows-end.txt")
			}
			if _, err := os.Stat(filepath.Join(dir, "rows-start.txt")); err == nil {
				r.Files = append(r.Files, "rows-start.txt")
			}
		}
		if a.cfg.MySQL && fileSize(filepath.Join(dir, "slow.log.gz")) > 0 && collectExplain(a.ctx, dir) {
			r.Files = append(r.Files, "explain.txt")
		}
		r.Git = gitInfo(a.ctx, a.cfg.Repo)
		r.CPUs = runtime.NumCPU()
		r.Services = unitStates(a.ctx, a.units())
		r.Final = true
		writeJSON(filepath.Join(dir, "run.json"), r)
		log.Printf("run %s ended (%s): %d req, %d err, %.0fs", r.TS, reason, r.ReqTotal, r.ErrTotal, r.DurationSec)
	}()
	// also write a provisional run.json immediately
	writeJSON(filepath.Join(dir, "run.json"), r)
}

// rotateRawLogs swaps the raw access/slow logs for fresh files so they do not grow across the whole contest.
// nginx: rename + USR1 (reopen); mysql: rename + FLUSH SLOW LOGS. The renamed files are deleted (the run slices keep what matters).
func (a *agent) rotateRawLogs() {
	ctx, cancel := context.WithTimeout(a.ctx, 15*time.Second)
	defer cancel()
	if a.cfg.AccessLog != "" {
		old := a.cfg.AccessLog + ".rotated"
		if err := os.Rename(a.cfg.AccessLog, old); err == nil {
			if err := exec.CommandContext(ctx, "nginx", "-s", "reopen").Run(); err != nil {
				exec.CommandContext(ctx, "sh", "-c", "kill -USR1 $(cat /run/nginx.pid)").Run()
			}
			time.Sleep(500 * time.Millisecond)
			os.Remove(old)
			a.mu.Lock()
			a.tail.reopen(true)
			a.mu.Unlock()
		}
	}
	if a.cfg.SlowLog != "" && a.cfg.MySQL {
		old := a.cfg.SlowLog + ".rotated"
		if err := os.Rename(a.cfg.SlowLog, old); err == nil {
			if err := exec.CommandContext(ctx, "mysql", mysqlArgs("-e", "FLUSH SLOW LOGS")...).Run(); err != nil {
				os.Rename(old, a.cfg.SlowLog) // could not flush: put it back
			} else {
				os.Remove(old)
			}
		}
	}
	if a.cfg.NginxErrLog != "" {
		if st, err := os.Stat(a.cfg.NginxErrLog); err == nil && st.Size() > 200<<20 {
			os.Truncate(a.cfg.NginxErrLog, 0)
		}
	}
}

// enforceRetention keeps at most cfg.KeepRuns run directories on the host (oldest deleted first).
func (a *agent) enforceRetention() {
	if a.cfg.KeepRuns <= 0 {
		return
	}
	entries, err := os.ReadDir(filepath.Join(a.cfg.DataDir, "runs"))
	if err != nil {
		return
	}
	var dirs []string
	for _, e := range entries {
		if e.IsDir() {
			dirs = append(dirs, e.Name())
		}
	}
	sort.Strings(dirs)
	for len(dirs) > a.cfg.KeepRuns {
		os.RemoveAll(filepath.Join(a.cfg.DataDir, "runs", dirs[0]))
		log.Printf("retention: removed run %s", dirs[0])
		dirs = dirs[1:]
	}
}

func (a *agent) units() []string {
	if a.cfg.Units != "" {
		return strings.Split(a.cfg.Units, ",")
	}
	return discoverUnits(a.ctx)
}

// ---- 1-second sampler ----

func (a *agent) sampleLoop() {
	tick := time.NewTicker(1 * time.Second)
	defer tick.Stop()
	a.prevSys = readSys()
	a.prevProc = readProcs()
	a.prevT = time.Now()
	if a.cfg.MySQL {
		if m, err := readMySQL(a.ctx); err == nil {
			a.prevMy, a.haveMy = m, true
		}
	}
	for range tick.C {
		cur := readSys()
		s := diffSys(a.prevSys, cur)
		a.prevSys = cur
		curProc := readProcs()
		now := time.Now()
		procs := diffProcs(a.prevProc, curProc, now.Sub(a.prevT).Seconds(), 8)
		a.prevProc, a.prevT = curProc, now
		var my map[string]any
		if a.cfg.MySQL {
			if m, err := readMySQL(a.ctx); err == nil {
				if a.haveMy {
					my = diffMySQL(a.prevMy, m)
				}
				a.prevMy, a.haveMy = m, true
			} else {
				a.haveMy = false
			}
		}
		if my != nil {
			a.onMySQLSample(num(my["questions_per_s"]))
		}
		var app, stub map[string]any
		a.mu.Lock()
		running := a.state == "running"
		a.mu.Unlock()
		if running {
			if a.cfg.ExpvarURL != "" {
				app, _ = readExpvar(a.ctx, a.cfg.ExpvarURL)
			}
			if a.cfg.StubURL != "" {
				stub, _ = readStubStatus(a.ctx, a.cfg.StubURL)
			}
		}
		a.mu.Lock()
		a.topProcs = procs
		a.recent = append(a.recent, s)
		if len(a.recent) > 120 {
			a.recent = a.recent[1:]
		}
		sec := secKey(time.Now().Add(-1 * time.Second))
		n := 0
		if ps := a.secs[sec]; ps != nil {
			n = ps.Req
		}
		a.rps = append(a.rps, n)
		if running && n > a.peakRPS {
			a.peakRPS, a.peakAt = n, time.Now().Add(-1*time.Second)
		}
		if len(a.rps) > 600 {
			a.rps = a.rps[1:]
		}
		if !running {
			// keep the per-second map small while idle
			if len(a.secs) > 200 {
				a.secs = map[string]*perSecond{}
			}
		} else {
			a.writers["sys"].Write(s)
			a.writers["procs"].Write(map[string]any{"t": now.Format(time.RFC3339Nano), "procs": procs})
			if my != nil {
				a.writers["mysql"].Write(my)
			}
			if app != nil {
				a.writers["app"].Write(app)
			}
			if stub != nil {
				a.writers["nginx"].Write(stub)
			}
		}
		a.mu.Unlock()
	}
}

// ---- HTTP API ----

func (a *agent) status() map[string]any {
	a.mu.Lock()
	defer a.mu.Unlock()
	var last sysSample
	if len(a.recent) > 0 {
		last = a.recent[len(a.recent)-1]
	}
	rps := 0
	if len(a.rps) > 0 {
		for _, n := range a.rps[max(0, len(a.rps)-10):] {
			rps += n
		}
		rps = rps / min(10, len(a.rps))
	}
	st := map[string]any{
		"version": version, "host": a.host, "contest": a.cfg.Contest, "time": time.Now().Format(time.RFC3339), "uptime_sec": int(time.Since(a.started).Seconds()),
		"state": a.state, "req_per_s_10s": rps, "last_request_at": a.lastReq.Format(time.RFC3339),
		"paused_until": ternary(time.Now().Before(a.pausedTo), a.pausedTo.Format(time.RFC3339), ""),
		"mysql_qps":    lastQPS(a.qpsHist),
		"sys":          last, "top_procs": a.topProcs, "last_error": a.lastErr, "disk_free_gb": last.DiskFreeGB, "disk_warning": last.DiskFreeGB > 0 && last.DiskFreeGB < 2,
		"config": map[string]any{"access_log": a.cfg.AccessLog, "slow_log": a.cfg.SlowLog, "expvar": a.cfg.ExpvarURL, "pprof": a.cfg.PprofURL, "mysql": a.cfg.MySQL, "trigger": a.cfg.TriggerRe, "ignore": a.cfg.IgnoreRe},
	}
	if a.run != nil {
		st["run"] = map[string]any{"ts": a.run.TS, "start": a.run.Start, "elapsed_sec": int(time.Since(a.run.Start).Seconds()), "req_total": a.reqCount, "err_total": a.errCount, "trigger": a.run.Trigger, "manual": a.run.Manual, "follow": a.run.Follow}
	}
	return st
}

func (a *agent) handleStatus(w http.ResponseWriter, r *http.Request) {
	st := a.status()
	// slower, exec-based fields only when asked (?full=1) to keep the 1s poll cheap
	if r.URL.Query().Get("full") == "1" {
		st["services"] = unitStates(a.ctx, a.units())
		st["git"] = gitInfo(a.ctx, a.cfg.Repo)
		st["env"] = envSummary(a.cfg.EnvFile)
		st["runs"] = a.listRuns(5)
	}
	writeJSONResp(w, st)
}

func (a *agent) listRuns(limit int) []map[string]any {
	entries, _ := os.ReadDir(filepath.Join(a.cfg.DataDir, "runs"))
	var out []map[string]any
	for i := len(entries) - 1; i >= 0 && (limit <= 0 || len(out) < limit); i-- {
		e := entries[i]
		if !e.IsDir() {
			continue
		}
		var ri map[string]any
		if b, err := os.ReadFile(filepath.Join(a.cfg.DataDir, "runs", e.Name(), "run.json")); err == nil {
			json.Unmarshal(b, &ri)
		} else {
			ri = map[string]any{"ts": e.Name(), "state": "in-progress"}
		}
		files, _ := os.ReadDir(filepath.Join(a.cfg.DataDir, "runs", e.Name()))
		var names []string
		var total int64
		for _, f := range files {
			if fi, err := f.Info(); err == nil {
				names = append(names, f.Name())
				total += fi.Size()
			}
		}
		ri["files"] = names
		ri["bytes"] = total
		out = append(out, ri)
	}
	return out
}

func (a *agent) handleRuns(w http.ResponseWriter, r *http.Request) { writeJSONResp(w, a.listRuns(0)) }

func (a *agent) handleRunFile(w http.ResponseWriter, r *http.Request) {
	rel := strings.TrimPrefix(r.URL.Path, "/runs/")
	if rel == "" || strings.Contains(rel, "..") {
		http.NotFound(w, r)
		return
	}
	if r.Method == "DELETE" {
		dir := filepath.Join(a.cfg.DataDir, "runs", filepath.Clean(rel))
		if err := os.RemoveAll(dir); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		writeJSONResp(w, map[string]any{"deleted": rel})
		return
	}
	http.ServeFile(w, r, filepath.Join(a.cfg.DataDir, "runs", filepath.Clean(rel)))
}

func (a *agent) handleStart(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.state == "running" {
		http.Error(w, "already running", 409)
		return
	}
	// /start                    manual run (never discarded, ends on idle/max/stop)
	// /start?ts=<ts>&follow=1   shadow run started by the panel: same ts as the leader host, ends only on /stop or max
	if r.URL.Query().Get("follow") == "1" {
		a.startRunAs("follow:"+r.URL.Query().Get("leader"), a.tail.Offset(), false, true, r.URL.Query().Get("ts"))
	} else {
		a.startRunLocked("manual", a.tail.Offset(), true)
	}
	writeJSONResp(w, map[string]any{"started": a.run.TS, "follow": a.run.Follow})
}

func (a *agent) handleStop(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.state != "running" {
		http.Error(w, "not running", 409)
		return
	}
	ts := a.run.TS
	reason := ternary(a.run.Follow, "leader-ended", "manual")
	// ?reason=bench-done: the panel got the "bench finished" signal (button / task bench:done) and ends every host now
	if rq := r.URL.Query().Get("reason"); stopReasonRe.MatchString(rq) {
		reason = rq
		a.run.StopBy = rq
	}
	a.endRunLocked(reason)
	writeJSONResp(w, map[string]any{"stopped": ts})
}

var stopReasonRe = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)

// fillEndDiagLocked records why the run ended when it did: the last request, the idle time before the end, the busiest
// second, the last few requests (a long tail after the load window shows here: who kept the run open), and for
// db-triggered runs the last moment MySQL was busy.
func (a *agent) fillEndDiagLocked(r *runInfo) {
	r.EndedAt = r.End.Format(time.RFC3339Nano)
	if !a.lastReq.IsZero() && a.lastReq.After(r.Start) {
		r.LastReqAt = a.lastReq.Format(time.RFC3339Nano)
		r.IdleSec = round1(r.End.Sub(a.lastReq).Seconds())
	}
	if a.peakRPS > 0 {
		r.PeakRPS, r.PeakAt = a.peakRPS, a.peakAt.Format(time.RFC3339)
	}
	r.TailReqs = append([]string(nil), a.tailReqs...)
	if strings.HasPrefix(r.Trigger, "mysql:") {
		if !a.dbActive.IsZero() {
			r.DBActiveAt = a.dbActive.Format(time.RFC3339Nano)
			r.IdleSec = round1(r.End.Sub(a.dbActive).Seconds())
		}
		r.DBPeakQPS = round1(a.qpsPeak)
		r.DBStopQPS = round1(max(a.cfg.DBTrigger/10, 0.05*a.qpsPeak))
	}
}

// /tail returns the last N seconds of req/s and the sys ring (for live charts in the panel).
func (a *agent) handleTail(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	defer a.mu.Unlock()
	rps := a.rps
	if len(rps) > 60 {
		rps = rps[len(rps)-60:]
	}
	writeJSONResp(w, map[string]any{"rps": rps, "sys": a.recent})
}

func writeJSONResp(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetIndent("", " ")
	enc.Encode(v)
}

// ---- status subcommand (client) ----

func runStatus(args []string) error {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	addr := fs.String("addr", "127.0.0.1:19100", "agent address")
	full := fs.Bool("full", false, "include services/git/env/runs")
	start := fs.Bool("start", false, "start a manual run")
	stop := fs.Bool("stop", false, "stop the current run")
	if err := fs.Parse(args); err != nil {
		return err
	}
	path := "/status"
	if *full {
		path += "?full=1"
	}
	if *start {
		path = "/start"
	}
	if *stop {
		path = "/stop"
	}
	method := "GET"
	if *start || *stop {
		method = "POST"
	}
	req, _ := http.NewRequest(method, "http://"+*addr+path, nil)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	var buf strings.Builder
	b := make([]byte, 1<<20)
	for {
		n, err := res.Body.Read(b)
		buf.Write(b[:n])
		if err != nil {
			break
		}
	}
	fmt.Print(buf.String())
	if res.StatusCode >= 400 {
		return fmt.Errorf("http %d", res.StatusCode)
	}
	return nil
}

// handlePause: POST /pause?minutes=N — stop starting runs from the access log for N minutes (people poking the app,
// app-analysis, a lone /initialize). minutes=0 resumes. Manual and shadow runs are unaffected.
func (a *agent) handlePause(w http.ResponseWriter, r *http.Request) {
	mins, _ := strconv.Atoi(r.URL.Query().Get("minutes"))
	a.mu.Lock()
	if mins <= 0 {
		a.pausedTo = time.Time{}
	} else {
		a.pausedTo = time.Now().Add(time.Duration(mins) * time.Minute)
	}
	to := a.pausedTo
	a.mu.Unlock()
	log.Printf("trigger detection paused until %s (minutes=%d)", to.Format(time.RFC3339), mins)
	writeJSONResp(w, map[string]any{"paused_until": ternary(mins > 0, to.Format(time.RFC3339), "")})
}

// onMySQLSample implements the DB-side trigger: a host that serves no HTTP (the DB host after the split) still sees the
// bench as a surge of MySQL queries. 3 consecutive samples >= DBTrigger start a run ("mysql:qps>=N"); the run ends when
// load has been below max(DBTrigger/10, 5% of peak) for IdleGap. Paused/manual/follow states are respected.
func (a *agent) onMySQLSample(qps float64) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.qpsHist = append(a.qpsHist, qps)
	if len(a.qpsHist) > 5 {
		a.qpsHist = a.qpsHist[len(a.qpsHist)-5:]
	}
	now := time.Now()
	if a.state == "running" {
		if qps > a.qpsPeak {
			a.qpsPeak = qps
		}
		if qps >= max(a.cfg.DBTrigger/10, 0.05*a.qpsPeak) {
			a.dbActive = now
		}
		return
	}
	if a.cfg.DBTrigger <= 0 || !now.After(a.pausedTo) || len(a.qpsHist) < 3 {
		return
	}
	for _, v := range a.qpsHist[len(a.qpsHist)-3:] {
		if v < a.cfg.DBTrigger {
			return
		}
	}
	a.qpsPeak, a.dbActive = qps, now
	a.startRunAs(fmt.Sprintf("mysql:qps>=%.0f", a.cfg.DBTrigger), a.tail.Offset(), false, false, "")
}

func lastQPS(h []float64) float64 {
	if len(h) == 0 {
		return 0
	}
	return h[len(h)-1]
}
