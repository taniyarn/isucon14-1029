package main

// "Bench finished" signal: the human presses a button (lane board → `task bench:done`, or the panel's header button)
// right after the bench prints its score. The panel then
//   1. ends the run on every host now (agent /stop?reason=bench-done) instead of waiting for the idle detection,
//   2. stores the bench output (optional request body) as <run>/bench.log and reads score / pass / 名前解決成功数 from it,
//   3. collects → reports (one report at a time, queued) → fills the score, in the background.
// Calling it twice is harmless: a finished pipeline is not re-run unless a new score or bench output came with the call.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type benchPipe struct {
	running bool
	again   bool   // new score / bench output arrived while running: report once more at the end
	state   string // waiting for agents | collecting | report | done | report failed: …
	at      time.Time
}

// endDiagKeys: run.json fields the agent writes to explain when / why a run ended (copied into meta.json).
var endDiagKeys = []string{"ended_at", "last_req_at", "idle_sec", "peak_rps", "peak_at", "tail_reqs", "db_active_at", "db_peak_qps", "db_stop_qps", "stop_by"}

var benchTSParamRe = regexp.MustCompile(`^\d{8}-\d{6}$`)

func readBenchDone(dir string) map[string]any {
	b, err := os.ReadFile(filepath.Join(dir, "bench-done.json"))
	if err != nil {
		return nil
	}
	var m map[string]any
	if json.Unmarshal(b, &m) != nil {
		return nil
	}
	return m
}

func parseAnyTime(v any) time.Time {
	s := str(v)
	if s == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}
	}
	return t
}

// endDiag is the one-line explanation of a host's run end: "end: idle after 10 s idle; signal +35 s".
// signal = when the "bench finished" signal reached the panel (zero = none); +N s = N s after the run had ended.
func endDiag(run map[string]any, signal time.Time) string {
	reason := str(run["end_reason"])
	if reason == "" {
		reason = "running"
	}
	out := "end: " + reason
	if idle := num(run["idle_sec"]); idle > 0 {
		switch {
		case strings.HasPrefix(str(run["trigger"]), "mysql:"):
			out += fmt.Sprintf(" after %.0f s without DB load (< %.0f q/s, peak %.0f)", idle, num(run["db_stop_qps"]), num(run["db_peak_qps"]))
		case reason == "idle" || reason == "max":
			out += fmt.Sprintf(" after %.0f s idle", idle)
		default:
			out += fmt.Sprintf(" (last request %.0f s before)", idle)
		}
	}
	end := parseAnyTime(run["ended_at"])
	if end.IsZero() {
		end = parseAnyTime(run["end"])
	}
	if !signal.IsZero() && !end.IsZero() {
		out += fmt.Sprintf("; signal %+d s", int(math.Round(signal.Sub(end).Seconds())))
	}
	return out
}

// handleBenchDone: POST /api/bench-done[?ts=<ts>&score=<n>] with an optional body (the bench's output).
// Without ts: the run running now, else the newest run that started within the last 10 min.
func (p *panel) handleBenchDone(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "POST", 405)
		return
	}
	now := time.Now()
	body, err := io.ReadAll(io.LimitReader(r.Body, 64<<20))
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	q := r.URL.Query()
	ts := q.Get("ts")
	if ts != "" && !benchTSParamRe.MatchString(ts) {
		http.Error(w, "ts must look like 20261026-162403", 400)
		return
	}
	scoreArg := strings.ReplaceAll(strings.TrimSpace(q.Get("score")), ",", "")
	var scoreVal float64
	if scoreArg != "" {
		if scoreVal, err = strconv.ParseFloat(scoreArg, 64); err != nil {
			http.Error(w, "score must be a number", 400)
			return
		}
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	fl := p.fleet(ctx, false)
	cancel()
	if ts == "" {
		ts = p.resolveBenchTS(r.Context(), fl, now)
		if ts == "" {
			http.Error(w, "no run is running and none started in the last 10 min: pass ts=<ts> (task bench:done TS=…)", 404)
			return
		}
	}
	res := map[string]any{"ts": ts, "stopped": p.stopForBench(r.Context(), fl, ts)}
	dir := filepath.Join(p.contestDir("measurements"), ts)
	os.MkdirAll(dir, 0755)

	newInfo := false
	var bs *benchSummary
	if len(strings.TrimSpace(string(body))) > 0 {
		if err := os.WriteFile(filepath.Join(dir, "bench.log"), body, 0644); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		bs = readBenchLog(filepath.Join(dir, "bench.log"), nil)
		res["bench_log_bytes"] = len(body)
		newInfo = true
	}
	p.mu.Lock()
	n := p.notes[ts]
	if n == nil {
		n = map[string]any{}
	}
	if str(n["bench_done_at"]) == "" {
		n["bench_done_at"] = now.Format(time.RFC3339Nano)
	}
	n["bench_done_last"] = now.Format(time.RFC3339Nano)
	n["bench_done_n"] = num(n["bench_done_n"]) + 1
	cur := fmt.Sprint(n["score"])
	autoScore := n["score"] == nil || cur == "" || cur == "<nil>" || str(n["score_src"]) == "bench.log" // a typed or SCORE= score wins over the output
	if scoreArg != "" {
		if cur != scoreArg && cur != strconv.FormatFloat(scoreVal, 'f', -1, 64) {
			newInfo = true
		}
		n["score"], n["score_src"] = scoreVal, "bench-done"
	} else if bs != nil && bs.score >= 0 && autoScore {
		n["score"], n["score_src"] = float64(bs.score), "bench.log"
	}
	if bs != nil {
		if bs.pass != "" {
			n["pass"] = bs.pass
		}
		if bs.dnsOK >= 0 {
			n["dns_ok"] = bs.dnsOK
		}
		if bs.dnsFail >= 0 {
			n["dns_fail"] = bs.dnsFail
		}
	}
	n["updated_at"] = now.Format(time.RFC3339)
	p.notes[ts] = n
	p.saveNotes()
	writeJSON(filepath.Join(dir, "note.json"), n)
	signalAt := str(n["bench_done_at"])
	res["score"], res["pass"], res["dns_ok"], res["dns_fail"], res["signal_at"] = n["score"], n["pass"], n["dns_ok"], n["dns_fail"], signalAt
	// bench-done.json: when the signal arrived (first call) and what came with it; the report's run table reads it
	bd := readBenchDone(dir)
	if bd == nil {
		bd = map[string]any{"ts": ts, "at": signalAt, "stopped": res["stopped"]}
	}
	bd["calls"], bd["last"] = n["bench_done_n"], now.Format(time.RFC3339Nano)
	for _, k := range []string{"score", "score_src", "pass", "dns_ok", "dns_fail"} {
		if v, ok := n[k]; ok {
			bd[k] = v
		}
	}
	writeJSON(filepath.Join(dir, "bench-done.json"), bd)

	// the background pipeline (collect → report → score): once per ts, again only when something new came in
	pp := p.pipes[ts]
	_, cerr := os.Stat(filepath.Join(dir, "collect.json"))
	_, rerr := os.Stat(filepath.Join(dir, "report.html"))
	done := cerr == nil && rerr == nil
	switch {
	case pp != nil && pp.running:
		if newInfo {
			pp.again = true
		}
		res["pipeline"] = "already running (" + pp.state + ")" + ternary(newInfo, "; will re-report with the new score / output", "")
	case done && !newInfo:
		res["pipeline"] = "already done (collected + report); nothing new"
	default:
		pp = &benchPipe{running: true, state: "waiting for agents", at: now}
		p.pipes[ts] = pp
		res["pipeline"] = ternary(done, "re-report started (new score / output)", "started: waiting for the agents to finish → collect → report")
		go p.runBenchPipeline(ts, pp)
	}
	p.mu.Unlock()
	log.Printf("bench-done %s: %v", ts, res)
	writeJSONResp(w, res)
}

// resolveBenchTS: the run running now (the leader's ts, as coordinate picks it), else the newest run that started in the
// last 10 min with requests and no signal yet (pickRecentRun; on the hosts or already collected).
func (p *panel) resolveBenchTS(ctx context.Context, fl []map[string]any, now time.Time) string {
	lead, anyTS := "", ""
	for _, st := range fl {
		run, ok := st["run"].(map[string]any)
		if !ok {
			continue
		}
		t := str(run["ts"])
		if t == "" {
			continue
		}
		if f, _ := run["follow"].(bool); !f && (lead == "" || t < lead) {
			lead = t
		}
		if anyTS == "" || t < anyTS {
			anyTS = t
		}
	}
	if lead != "" {
		return lead
	}
	if anyTS != "" {
		return anyTS
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	return p.pickRecentRun(p.runs(ctx), now)
}

// pickRecentRun: among the runs that started in the last 10 min (by their start instant, not by name), the newest one
// that has requests and no "bench finished" signal yet. 2026-10-28: an empty run (req 0) started right after the
// baseline ended and got the signal, so the baseline itself was left without a score; and with JST and UTC names
// mixed, the newest name was not the newest run. Falls back to the newest recent run when every one is empty or signalled.
func (p *panel) pickRecentRun(groups []map[string]any, now time.Time) string {
	type cand struct {
		ts       string
		start    time.Time
		req      float64
		signaled bool
	}
	var cs []cand
	for _, g := range groups {
		c := cand{ts: str(g["ts"])}
		if hs, ok := g["hosts"].(map[string]any); ok {
			for _, x := range hs {
				hm, _ := x.(map[string]any)
				if t := parseAnyTime(hm["start"]); !t.IsZero() && (c.start.IsZero() || t.Before(c.start)) {
					c.start = t
				}
				c.req += num(hm["req_total"])
			}
		}
		if c.start.IsZero() {
			if t, err := time.Parse("20060102-150405", c.ts); err == nil { // run names are UTC (agent runTS)
				c.start = t
			}
		}
		if n, ok := g["note"].(map[string]any); ok && str(n["bench_done_at"]) != "" {
			c.signaled = true
		}
		if !c.start.IsZero() && now.Sub(c.start) <= 10*time.Minute && now.Sub(c.start) > -time.Minute {
			cs = append(cs, c)
		}
	}
	if len(cs) == 0 {
		return ""
	}
	sort.SliceStable(cs, func(i, j int) bool { return cs[i].start.After(cs[j].start) })
	for _, c := range cs {
		if c.req > 0 && !c.signaled {
			return c.ts
		}
	}
	return cs[0].ts
}

// stopForBench ends the run of ts on every host that is still running it (leader, shadows, a db-triggered run).
func (p *panel) stopForBench(ctx context.Context, fl []map[string]any, ts string) map[string]any {
	out := map[string]any{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	for i, st := range fl {
		if i >= len(p.hosts) {
			break
		}
		h := p.hosts[i]
		if st["error"] != nil {
			out[h.Name] = "unreachable"
			continue
		}
		run, ok := st["run"].(map[string]any)
		if !ok || str(st["state"]) != "running" {
			out[h.Name] = "not running (already ended)"
			continue
		}
		if t := str(run["ts"]); !within(t, ts, 20*time.Second) {
			if f, _ := run["follow"].(bool); !f {
				out[h.Name] = "running another run " + t + " (left alone)"
				continue
			}
		}
		wg.Add(1)
		go func(h panelHost) {
			defer wg.Done()
			msg := "stopped"
			if err := postShort(ctx, h.Agent+"/stop?reason=bench-done", 3*time.Second); err != nil {
				msg = "stop failed: " + err.Error()
			}
			mu.Lock()
			out[h.Name] = msg
			mu.Unlock()
		}(h)
	}
	wg.Wait()
	return out
}

func postShort(ctx context.Context, url string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "POST", url, nil)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	io.Copy(io.Discard, res.Body)
	res.Body.Close()
	if res.StatusCode >= 400 && res.StatusCode != 409 { // 409 = not running any more
		return &httpErr{code: res.StatusCode}
	}
	return nil
}

func (p *panel) setPipe(pp *benchPipe, state string) {
	p.mu.Lock()
	pp.state = state
	p.mu.Unlock()
}

// runBenchPipeline: wait until every host has written its final run.json, collect (once), report (queued behind any
// other report), then fill the score from the bench output if nobody gave one.
func (p *panel) runBenchPipeline(ts string, pp *benchPipe) {
	dir := filepath.Join(p.contestDir("measurements"), ts)
	ctx := context.Background()
	for {
		if _, err := os.Stat(filepath.Join(dir, "collect.json")); err != nil {
			p.setPipe(pp, "waiting for agents")
			p.waitRunsFinal(ctx, ts, 150*time.Second)
			p.setPipe(pp, "collecting")
			res := p.collectRun(ctx, ts)
			log.Printf("bench-done %s: collected %v", ts, res)
		}
		p.setPipe(pp, "report")
		score, note := p.noteScoreNote(ts)
		err := p.reportProcWait(dir, score, note, true)
		if err == nil {
			p.scoreFromReport(ts) // only when no score was given or typed
		}
		p.mu.Lock()
		if pp.again {
			pp.again = false
			p.mu.Unlock()
			continue
		}
		pp.running = false
		pp.state = ternary(err == nil, "done", "report failed: "+fmt.Sprint(err))
		p.mu.Unlock()
		log.Printf("bench-done %s: %s", ts, pp.state)
		return
	}
}

// waitRunsFinal polls the hosts until run ts is finalised everywhere (agent ≥ 0.3.2 writes final:true; older agents:
// the final run.json is the one with services), stopping it again if a host is still running it.
func (p *panel) waitRunsFinal(ctx context.Context, ts string, limit time.Duration) {
	t0 := time.Now()
	restopped := false
	for time.Since(t0) < limit {
		pending := []string{}
		for _, h := range p.hosts {
			var rs []map[string]any
			if err := getJSON(ctx, h.Agent+"/runs", &rs, 4*time.Second); err != nil {
				continue // unreachable: collect reports it
			}
			for _, run := range rs {
				if t := str(run["ts"]); t == "" || !within(ts, t, 20*time.Second) {
					continue
				}
				fin, hasFin := run["final"].(bool)
				switch {
				case str(run["end_reason"]) == "":
					pending = append(pending, h.Name+" running")
					if !restopped && time.Since(t0) > 20*time.Second {
						postShort(ctx, h.Agent+"/stop?reason=bench-done", 3*time.Second)
					}
				case hasFin && !fin:
					pending = append(pending, h.Name+" finalising")
				case !hasFin && run["services"] == nil && time.Since(t0) < 60*time.Second:
					pending = append(pending, h.Name+" finalising (old agent)")
				}
			}
		}
		if time.Since(t0) > 20*time.Second {
			restopped = true
		}
		if len(pending) == 0 {
			log.Printf("bench-done %s: agents final after %.0fs", ts, time.Since(t0).Seconds())
			return
		}
		sort.Strings(pending)
		if int(time.Since(t0).Seconds())%20 < 2 {
			log.Printf("bench-done %s: waiting for %s", ts, strings.Join(pending, ", "))
		}
		time.Sleep(2 * time.Second)
	}
	log.Printf("bench-done %s: agents not final after %s; collecting what is there", ts, limit)
}

// scoreFromReport fills the note's score from report.json's bench_log (the bench's result line) when nobody typed one.
func (p *panel) scoreFromReport(ts string) (float64, bool) {
	dir := filepath.Join(p.contestDir("measurements"), ts)
	b, err := os.ReadFile(filepath.Join(dir, "report.json"))
	if err != nil {
		return 0, false
	}
	var m map[string]any
	if json.Unmarshal(b, &m) != nil {
		return 0, false
	}
	bl, ok := m["bench_log"].(map[string]any)
	if !ok || num(bl["score"]) < 0 {
		return 0, false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	n := p.notes[ts]
	if n == nil {
		n = map[string]any{}
	}
	if n["score"] != nil && fmt.Sprint(n["score"]) != "" && fmt.Sprint(n["score"]) != "<nil>" {
		return 0, false
	}
	n["score"], n["score_src"], n["updated_at"] = num(bl["score"]), "bench.log", time.Now().Format(time.RFC3339)
	p.notes[ts] = n
	p.saveNotes()
	writeJSON(filepath.Join(dir, "note.json"), n)
	return num(bl["score"]), true
}

// pipeState for the Runs list ("auto: report…").
func (p *panel) pipeStateLocked(ts string) string {
	if pp := p.pipes[ts]; pp != nil {
		return pp.state
	}
	return ""
}
