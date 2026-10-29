package main

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// diagnose.go: turn the collected numbers into a verdict ("what is the bottleneck") and ranked next actions.

type loadWindow struct {
	fromT, toT time.Time
	secs       float64
	peakRps    float64
	avgRps     float64
	from       string // set when the window was borrowed from the busiest host (this host serves almost no HTTP)
}

// detectLoadWindow finds the span where req/s stays above 30% of the peak (skips initialize / integrity checks / tail).
func detectLoadWindow(h *hostRun) loadWindow {
	type pt struct {
		t   time.Time
		req float64
	}
	var pts []pt
	peak := 0.0
	for _, m := range h.HTTP {
		t, err := parseSec(str(m["t"]), h.Start.Location())
		if err != nil {
			continue
		}
		r := num(m["req"])
		pts = append(pts, pt{t, r})
		if r > peak {
			peak = r
		}
	}
	w := loadWindow{fromT: h.Start, toT: h.End, peakRps: peak}
	if len(pts) == 0 || peak == 0 {
		w.secs = h.End.Sub(h.Start).Seconds()
		return w
	}
	sort.Slice(pts, func(i, j int) bool { return pts[i].t.Before(pts[j].t) })
	first, last := -1, -1
	for i, p := range pts {
		if p.req >= 0.3*peak {
			if first < 0 {
				first = i
			}
			last = i
		}
	}
	if first < 0 {
		first, last = 0, len(pts)-1
	}
	w.fromT, w.toT = pts[first].t, pts[last].t.Add(time.Second)
	w.secs = w.toT.Sub(w.fromT).Seconds()
	sum := 0.0
	for _, p := range pts[first : last+1] {
		sum += p.req
	}
	w.avgRps = sum / float64(last-first+1)
	return w
}

// httpReqs: requests in a host's per-second series (what detectLoadWindow sees).
func httpReqs(h *hostRun) float64 {
	n := 0.0
	for _, m := range h.HTTP {
		n += num(m["req"])
	}
	return n
}

// fleetLoadWindows: one load window per host. A host that serves (almost) no HTTP — the DB host, a shadow app host
// behind svc1's nginx — takes the busiest host's window: its own access log holds only strays (an internet scanner
// hitting its nginx once), and a window built from one stray request is 1 s wide, which made the DB host's verdict
// "idle 100%" while mysqld ran at 190% (2026-10-27, run 20261027-120035).
func fleetLoadWindows(hosts []*hostRun) []loadWindow {
	ws := make([]loadWindow, len(hosts))
	bi, breq := -1, 0.0
	for i, h := range hosts {
		ws[i] = detectLoadWindow(h)
		if r := httpReqs(h); r > breq {
			bi, breq = i, r
		}
	}
	if bi < 0 || breq < 100 || ws[bi].secs < 5 {
		return ws
	}
	bw := ws[bi]
	for i, h := range hosts {
		if i == bi || httpReqs(h) >= 0.1*breq {
			continue
		}
		w := loadWindow{fromT: bw.fromT, toT: bw.toT, secs: bw.secs, from: hosts[bi].Name}
		sum := 0.0
		for _, m := range h.HTTP {
			t, err := parseSec(str(m["t"]), h.Start.Location())
			if err != nil || t.Before(w.fromT) || !t.Before(w.toT) {
				continue
			}
			r := num(m["req"])
			sum += r
			w.peakRps = math.Max(w.peakRps, r)
		}
		if w.secs > 0 {
			w.avgRps = sum / w.secs
		}
		ws[i] = w
	}
	return ws
}

// parseSec: a series timestamp. The agent writes http.jsonl "t" without a zone, in the host's local time; loc is the
// run's start location (run.json "start" carries the host's offset: +09:00 on JST hosts, Z on UTC ones), so both read
// right. Strings with a zone parse as they are.
func parseSec(s string, loc *time.Location) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t, nil
	}
	if loc == nil {
		loc = time.UTC
	}
	return time.ParseInLocation("2006-01-02T15:04:05", s, loc)
}

func inWindow(w loadWindow, s string) bool {
	t, err := parseSec(s, w.fromT.Location())
	if err != nil {
		return false
	}
	return !t.Before(w.fromT) && t.Before(w.toT)
}

type procAgg struct {
	name   string
	avgPct float64
}

// procBreakdown averages per-process-name CPU% (sum of pids with the same name) over the load window.
func procBreakdown(h *hostRun, w loadWindow) []procAgg {
	sums := map[string]float64{}
	n := 0
	for _, m := range h.Procs {
		if !inWindow(w, str(m["t"])) {
			continue
		}
		n++
		list, _ := m["procs"].([]any)
		for _, x := range list {
			pm, _ := x.(map[string]any)
			sums[str(pm["name"])] += num(pm["cpu_pct"])
		}
	}
	var out []procAgg
	for name, s := range sums {
		if n > 0 {
			out = append(out, procAgg{name, round1(s / float64(n))})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].avgPct > out[j].avgPct })
	return out
}

type winMetrics struct {
	cpuUser, cpuSys, cpuIdle, iowait, memUsed float64
	threadsRunning, lockWaits, lockMs, qps    float64
	p99ms, errRate, tcpTW                     float64
	n                                         int
}

func windowMetrics(h *hostRun, w loadWindow) winMetrics {
	var m winMetrics
	for _, s := range h.Sys {
		if !inWindow(w, str(s["t"])) {
			continue
		}
		m.n++
		m.cpuUser += num(s["cpu_user_pct"])
		m.cpuSys += num(s["cpu_sys_pct"])
		m.cpuIdle += num(s["cpu_idle_pct"])
		m.iowait += num(s["cpu_iowait_pct"])
		m.memUsed += num(s["mem_used_pct"])
		if v := num(s["tcp_timewait"]); v > m.tcpTW {
			m.tcpTW = v
		}
	}
	if m.n > 0 {
		f := float64(m.n)
		m.cpuUser, m.cpuSys, m.cpuIdle, m.iowait, m.memUsed = m.cpuUser/f, m.cpuSys/f, m.cpuIdle/f, m.iowait/f, m.memUsed/f
	}
	k := 0
	for _, s := range h.MySQL {
		if !inWindow(w, str(s["t"])) {
			continue
		}
		k++
		m.threadsRunning += num(s["threads_running"])
		m.lockWaits += num(s["innodb_row_lock_waits_per_s"])
		m.lockMs += num(s["innodb_row_lock_time_per_s"])
		m.qps += num(s["questions_per_s"])
	}
	if k > 0 {
		m.threadsRunning, m.lockWaits, m.lockMs, m.qps = m.threadsRunning/float64(k), m.lockWaits/float64(k), m.lockMs/float64(k), m.qps/float64(k)
	}
	req, errs := 0.0, 0.0
	var p99s []float64
	for _, s := range h.HTTP {
		if !inWindow(w, str(s["t"])) {
			continue
		}
		req += num(s["req"])
		errs += num(s["err_5xx"])
		p99s = append(p99s, num(s["reqtime_p99"])*1000)
	}
	if req > 0 {
		m.errRate = 100 * errs / req
	}
	sort.Float64s(p99s)
	m.p99ms = pct(p99s, .5) // median of per-second p99
	return m
}

type verdict struct {
	kind    string  // the bottleneck: db-cpu | app-cpu | cpu | io | lock | memory | wait | bench | idle | unknown
	title   string  // the bottleneck in one line (summary.md "### host: title" — retro / panel parse this line)
	fix     string  // what to fix before the bottleneck (5xx), "" when nothing jumps the queue
	near    string  // a second reading when the numbers sit on a threshold ("close call: …")
	cores   float64 // CPU cores of the host (run.json cpus, else estimated): the chart's ceiling = cores × 100%
	coresOK bool    // cores came from run.json (false = estimated)
	reasons []string
}

// hostCores: run.json "cpus" (agent ≥ this version), else estimated from the window: the processes' summed % of one
// core divided by the share of the box in use (100 − idle); at least 1.
func hostCores(h *hostRun, procs []procAgg, m winMetrics) (float64, bool) {
	if h != nil && h.Run != nil {
		if c := num(h.Run["cpus"]); c >= 1 {
			return c, true
		}
	}
	sum := 0.0
	for _, p := range procs {
		sum += p.avgPct
	}
	used := 100 - m.cpuIdle
	if sum <= 0 || used < 20 { // a nearly idle box: the ratio is noise, leave the ceiling unknown
		return 0, false
	}
	c := math.Round(sum / used)
	if c < 1 {
		c = 1
	}
	return c, false
}

// CPU-bound when the box has ≤ 15% idle (the rest goes to nginx, the agent, the kernel: a 2-core box at 12–15% idle
// with the app at 126% is CPU-bound — 2026-10-20 run 161752 was judged "wait" at 12.3% with the old 12% line);
// 15–25% idle is reported as a close call next to the other reading.
const cpuBoundIdle, cpuNearIdle = 15.0, 25.0

func diagnose(h *hostRun, w loadWindow, procs []procAgg, m winMetrics) verdict {
	var v verdict
	r := func(f string, a ...any) { v.reasons = append(v.reasons, fmt.Sprintf(f, a...)) }
	db, app, other := 0.0, 0.0, 0.0
	appName := ""
	for _, p := range procs {
		n := strings.ToLower(p.name)
		switch {
		case strings.Contains(n, "mysqld") || strings.Contains(n, "mariadbd") || strings.Contains(n, "postgres"):
			db += p.avgPct
		case n == "nginx" || n == "isumeasure" || n == "bench" || strings.HasPrefix(n, "kworker") || n == "sshd" || n == "systemd":
			other += p.avgPct
		default:
			app += p.avgPct
			if appName == "" {
				appName = p.name
			}
		}
	}
	used := m.cpuUser + m.cpuSys
	if m.n == 0 {
		v.kind, v.title = "unknown", "no system samples in the load window"
		return v
	}
	v.cores, v.coresOK = hostCores(h, procs, m)
	ceil := v.cores * 100
	of := func(x float64) string { // "153% of 200%", or just "153%" when the cores are unknown
		if ceil > 0 {
			return fmt.Sprintf("%.0f%% of %.0f%%", x, ceil)
		}
		return fmt.Sprintf("%.0f%%", x)
	}
	bench := 0.0
	for _, p := range procs {
		if strings.ToLower(p.name) == "bench" {
			bench += p.avgPct
		}
	}
	r("load window %.0fs%s: avg %.0f req/s (peak %.0f), median p99 %.0fms, 5xx %.1f%%", w.secs, ternary(w.from != "", " (= "+w.from+"'s; little HTTP here)", ""), w.avgRps, w.peakRps, m.p99ms, m.errRate)
	r("CPU user %.0f%% + sys %.0f%% (idle %.0f%%, iowait %.0f%%)", m.cpuUser, m.cpuSys, m.cpuIdle, m.iowait)
	if len(procs) > 0 {
		var parts []string
		for i, p := range procs {
			if i >= 5 {
				break
			}
			parts = append(parts, fmt.Sprintf("%s %.0f%%", p.name, p.avgPct))
		}
		r("CPU by process (%% of one core): %s%s", strings.Join(parts, ", "), ternary(ceil > 0, fmt.Sprintf(" — ceiling %.0f%% (%.0f cores%s)", ceil, v.cores, ternary(v.coresOK, "", ", estimated")), ""))
	}
	if m.qps > 0 {
		r("MySQL %.0f q/s, threads_running avg %.1f, row-lock waits %.2f/s (%.0f ms/s)", m.qps, m.threadsRunning, m.lockWaits, m.lockMs)
	}
	// a practice bench box (the bench process, no app traffic) is not a bottleneck of the app: say so and stop
	if bench >= 20 && w.avgRps < 1 && db < 20 {
		v.kind, v.title = "bench", "bench host (not judged): bench uses "+of(bench)+", no app requests here"
		return v
	}
	if w.avgRps < 1 && db < 5 && used < 20 { // a spare box (no app traffic, no DB work): nothing to judge
		v.kind, v.title = "idle", fmt.Sprintf("idle host (not judged): no app requests and no DB work here (CPU %.0f%% used)", used)
		return v
	}
	if m.errRate >= 1 { // 5xx jump the queue but are not the bottleneck: keep both
		v.fix = fmt.Sprintf("fix first: %.1f%% of requests are 5xx — read the Errors section before anything else", m.errRate)
	}
	dbHost := w.avgRps < 1 && db >= 5 // DB on its own box: no HTTP here, judge mysqld against the box
	switch {
	case m.cpuIdle <= cpuBoundIdle && len(procs) > 0 && db >= app*1.3:
		v.kind = "db-cpu"
		v.title = ternary(dbHost, "DB host CPU-bound: mysqld "+of(db)+" — fix the heaviest SQL (indexes, N+1) first",
			fmt.Sprintf("DB CPU-bound: mysqld %s (app %.0f%%) — fix the heaviest SQL (indexes, N+1) first", of(db), app))
	case m.cpuIdle <= cpuBoundIdle && len(procs) > 0 && app >= db*1.3:
		v.kind, v.title = "app-cpu", fmt.Sprintf("app CPU-bound: %s %s (mysqld %.0f%%) — profile the app (pprof), reduce per-request work", appName, of(app), db)
	case m.cpuIdle <= cpuBoundIdle:
		v.kind, v.title = "cpu", fmt.Sprintf("CPU saturated (%.0f%% used) with app and DB both busy — start with the top SQL, then profile", used)
	case m.iowait >= 15:
		v.kind, v.title = "io", fmt.Sprintf("IO-bound: iowait %.0f%% — fsync/binlog/slow log/disk writes; check innodb_flush settings and what writes", m.iowait)
	case m.lockWaits >= 1 || m.lockMs >= 200:
		v.kind, v.title = "lock", fmt.Sprintf("lock-bound: %.1f row-lock waits/s (%.0f ms/s) with idle CPU — shorten transactions, avoid FOR UPDATE hot rows", m.lockWaits, m.lockMs)
	case m.memUsed >= 90:
		v.kind, v.title = "memory", fmt.Sprintf("memory pressure: %.0f%% used — check buffer pool / app memory / swap", m.memUsed)
	default:
		v.kind, v.title = "wait", fmt.Sprintf("CPU is not the limit (idle %.0f%%): throughput is bounded by waiting — latency of the slowest endpoints, polling intervals, external calls, or the bench's concurrency", m.cpuIdle)
	}
	if dbHost && v.kind != "db-cpu" {
		v.title = "DB host: mysqld " + of(db) + " — " + v.title
	}
	// close calls: CPU nearly full (idle 15–25%) but judged something else, or judged CPU with lock waits as well
	switch {
	case v.kind != "db-cpu" && v.kind != "app-cpu" && v.kind != "cpu" && m.cpuIdle > cpuBoundIdle && m.cpuIdle <= cpuNearIdle && len(procs) > 0:
		who := ternary(db >= app, "mysqld "+of(db), appName+" "+of(app))
		v.near = fmt.Sprintf("close call: also near CPU-bound (idle %.0f%%, %s)", m.cpuIdle, who)
	case (v.kind == "db-cpu" || v.kind == "cpu") && (m.lockWaits >= 1 || m.lockMs >= 200):
		v.near = fmt.Sprintf("close call: also row-lock waits %.1f/s (%.0f ms/s)", m.lockWaits, m.lockMs)
	}
	return v
}

// ---------- next actions ----------

type action struct {
	priority string // P1 / P2 / P3
	title    string
	detail   string
	cmd      string
	where    string   // handler / file hint (from sqltag tags or the static code index) so an issue is self-contained
	sql      string   // normalized SQL the action is about (for file lookup), "" for non-SQL actions
	kind     string   // index | n1 | aggregate | endpoint | errors | polling | pprof | static | lock | io | wait | other
	group    string   // work unit for parallel implementation: "indexes", "file:<go file>", "endpoint:<key>", "ops"
	file     string   // Go file to edit when known
	also     []string // other hosts that produced the same action (deduped into this one)
	share    float64  // % of the time the action is about (request time, SQL time, app CPU profile); 0 = no measured share
	score    float64  // expected effect (score.go); actions are sorted by it
	why      string   // how the score was made ("35% of request time · bottleneck ×1.5 · score path ×1.5")
	pinned   bool     // a later rule set the priority on purpose (tiny retry_after …): scoring keeps at least that band
	callers  []string // CPU hot spot: main.* callers (score path check)
}

func withShare(share float64, a action) action {
	a.share = share
	return a
}

func mk(pri, title, detail, cmd, where string, sql ...string) action {
	a := action{priority: pri, title: title, detail: detail, cmd: cmd, where: where}
	if len(sql) > 0 {
		a.sql = sql[0]
	}
	return a
}

// kindOf classifies an action by its title (keeps the action literals short).
func kindOf(title string) string {
	switch {
	case strings.HasPrefix(title, "Add an index"):
		return "index"
	case strings.HasPrefix(title, "N+1"):
		return "n1"
	case strings.HasPrefix(title, "Heavy aggregate"):
		return "aggregate"
	case strings.HasPrefix(title, "Speed up"):
		return "endpoint"
	case strings.HasPrefix(title, "Fix 5xx"):
		return "errors"
	case strings.HasPrefix(title, "Polling"):
		return "polling"
	case strings.HasPrefix(title, "Query on "):
		return "slowidx"
	case strings.HasPrefix(title, "CPU hot spot"):
		return "hotspot"
	case strings.HasPrefix(title, "Enable pprof"):
		return "pprof"
	case strings.HasPrefix(title, "Serve static"):
		return "static"
	case strings.HasPrefix(title, "Reduce row-lock"):
		return "lock"
	case strings.HasPrefix(title, "Cut disk"):
		return "io"
	case strings.HasPrefix(title, "CPU is idle"):
		return "wait"
	}
	return "other"
}

var fromRe = regexp.MustCompile(`(?i)\b(?:FROM|UPDATE|INTO)\s+` + "`?(\\w+)`?")
var whereRe = regexp.MustCompile(`(?i)\bWHERE\b(.*?)(?:\bORDER BY\b|\bGROUP BY\b|\bLIMIT\b|\bFOR\b|$)`)
var condColRe = regexp.MustCompile("(?i)(?:`?(\\w+)`?\\.)?`?(\\w+)`?\\s*(?:=|<>|!=|<=|>=|<|>|\\bIN\\b|\\bIS\\b|\\bBETWEEN\\b|\\bLIKE\\b)")
var orderRe = regexp.MustCompile(`(?i)\bORDER BY\b(.*?)(?:\bLIMIT\b|\bFOR\b|$)`)
var orderColRe = regexp.MustCompile("(?:`?(\\w+)`?\\.)?`?(\\w+|\\?)`?")

var aliasRe = regexp.MustCompile("(?i)\\b(?:FROM|JOIN)\\s+`?(\\w+)`?(?:\\s+(?:AS\\s+)?`?(\\w+)`?)?")
var subqueryRe = regexp.MustCompile(`(?i)\(\s*SELECT\b`)
var aliasStop = map[string]bool{"WHERE": true, "JOIN": true, "LEFT": true, "RIGHT": true, "INNER": true, "OUTER": true, "CROSS": true, "ON": true, "USING": true, "ORDER": true, "GROUP": true, "LIMIT": true, "FOR": true, "SET": true, "STRAIGHT_JOIN": true, "NATURAL": true, "HAVING": true, "UNION": true, "FORCE": true, "USE": true, "IGNORE": true}

// stripSubqueries replaces every "(SELECT …)" (balanced) with "(?)": their columns are not the outer table's.
func stripSubqueries(q string) string {
	for {
		loc := subqueryRe.FindStringIndex(q)
		if loc == nil {
			return q
		}
		depth, end := 0, len(q)
		for i := loc[0]; i < len(q); i++ {
			if q[i] == '(' {
				depth++
			} else if q[i] == ')' {
				depth--
				if depth == 0 {
					end = i + 1
					break
				}
			}
		}
		q = q[:loc[0]] + "(?)" + q[end:]
	}
}

// indexSuggestion: the table of the outer FROM and the columns of WHERE (equality first, as written) + ORDER BY
// that belong to it. Columns qualified with another table's alias (JOINed tables) and anything inside a subquery
// are left out; colsOf (information_schema.columns / CREATE TABLE) drops whatever the table does not have.
func indexSuggestion(norm string, colsOf map[string]map[string]bool) (table string, cols []string) {
	outer := stripSubqueries(norm)
	m := fromRe.FindStringSubmatch(outer)
	if m == nil {
		return "", nil
	}
	table = m[1]
	mine := map[string]bool{strings.ToLower(table): true} // qualifiers that mean the main table
	for _, am := range aliasRe.FindAllStringSubmatch(outer, -1) {
		if strings.EqualFold(am[1], table) && am[2] != "" && !aliasStop[strings.ToUpper(am[2])] {
			mine[strings.ToLower(am[2])] = true
			break
		}
	}
	known := colsOf[strings.ToLower(table)]
	seen := map[string]bool{}
	take := func(qual, col string) {
		lc := strings.ToLower(col)
		if seen[lc] || (qual != "" && !mine[strings.ToLower(qual)]) || (len(known) > 0 && !known[lc]) {
			return
		}
		seen[lc] = true
		cols = append(cols, col)
	}
	if wm := whereRe.FindStringSubmatch(outer); wm != nil {
		for _, c := range condColRe.FindAllStringSubmatch(wm[1], -1) {
			col := c[2]
			if strings.EqualFold(col, "AND") || strings.EqualFold(col, "OR") || strings.EqualFold(col, "NOT") || strings.EqualFold(col, "NULL") {
				continue
			}
			take(c[1], col)
		}
	}
	if om := orderRe.FindStringSubmatch(outer); om != nil {
		for _, c := range orderColRe.FindAllStringSubmatch(om[1], -1) {
			col := c[2]
			if strings.EqualFold(col, "ASC") || strings.EqualFold(col, "DESC") || col == "?" {
				continue
			}
			take(c[1], col)
		}
	}
	return table, cols
}

// existingIndexes parses the agent's db-catalog.txt "--- indexes" section: table -> list of index column lists.
func existingIndexes(dir string) map[string][][]string {
	out := map[string][][]string{}
	b, err := os.ReadFile(filepath.Join(dir, "db-catalog.txt"))
	if err != nil {
		return out
	}
	in := false
	for _, ln := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(ln, "--- indexes") {
			in = true
			continue
		}
		if strings.HasPrefix(ln, "--- ") {
			in = false
		}
		if !in {
			continue
		}
		f := strings.Split(ln, "\t")
		if len(f) < 4 || f[0] == "table_name" {
			continue
		}
		out[strings.ToLower(f[0])] = append(out[strings.ToLower(f[0])], strings.Split(strings.ToLower(f[3]), ","))
	}
	return out
}

func suggestActions(v verdict, w loadWindow, m winMetrics, procs []procAgg, eps []*epStat, totalReq int, sqls []*sqlStat, totalSQL float64, sa sessionAnalysis, pprofOK bool, epHandler map[string]string, existing map[string][][]string, colsOf map[string]map[string]bool) []action {
	var acts []action
	apiReq := 0
	for _, e := range eps {
		if !strings.Contains(e.key, "static assets") {
			apiReq += e.count
		}
	}
	if m.errRate >= 1 {
		var bad []string
		for _, e := range eps {
			if e.c5 > 0 {
				bad = append(bad, fmt.Sprintf("%s (%d)", e.key, e.c5))
			}
		}
		acts = append(acts, mk("P1", fmt.Sprintf("Fix 5xx errors (%.1f%% of requests)", m.errRate), "Errors cost score and often hide a real bug (timeouts, connection limits, nil rows). Endpoints: "+strings.Join(bad, ", "), "", ""))
	}
	// index suggestions from the slow log
	seenIdx := map[string]bool{}
	for i, s := range sqls {
		if i >= 15 {
			break
		}
		share := 0.0
		if totalSQL > 0 {
			share = 100 * s.sum / totalSQL
		}
		ratio := 0.0
		if s.sent > 0 {
			ratio = float64(s.examined) / float64(s.sent)
		} else if s.count > 0 {
			ratio = float64(s.examined) / float64(s.count)
		}
		if share < 0.5 || s.count < 5 {
			continue
		}
		complex := regexp.MustCompile(`(?i)\(\s*SELECT|\bJOIN\b|\bOVER\s*\(|\bGROUP BY\b`).MatchString(s.norm)
		if complex && share >= 3 {
			acts = append(acts, withShare(share, mk(ternary(share >= 10, "P1", "P2"), fmt.Sprintf("Heavy aggregate query: %.0f%% of SQL time, avg %.0fms", share, 1000*s.sum/float64(s.count)), fmt.Sprintf("%d calls. Subquery/JOIN/window queries rarely get fast with an index alone: precompute the result (maintain a column or table on write), cache it within the allowed staleness, or move the computation into the app. Query: %s", s.count, trunc(s.norm, 200)), "", calledFrom(s), s.norm)))
			continue
		}
		if ratio >= 20 && strings.HasPrefix(strings.ToUpper(s.norm), "SELECT") {
			t, cols := indexSuggestion(s.norm, colsOf)
			if t != "" && len(cols) > 0 {
				// already indexed? (live catalog of the run) → the slowness is not a missing index
				lc := make([]string, len(cols))
				for i, c := range cols {
					lc[i] = strings.ToLower(c)
				}
				have := false
				for _, ex := range existing[strings.ToLower(t)] {
					if isPrefix(lc, ex) {
						have = true
						break
					}
				}
				if have {
					acts = append(acts, withShare(share, mk(ternary(share >= 10, "P1", "P2"), fmt.Sprintf("Query on %s is still slow with its index (%.0f%% of SQL time)", t, share), fmt.Sprintf("%d calls, examines ×%.0f more rows than it returns (avg %.1fms) although an index on (%s) exists: a range/LIKE/function on the indexed column, low selectivity, or a sort the index does not cover. Rewrite the query, add the ORDER BY column to the index, or precompute. Query: %s", s.count, ratio, 1000*s.sum/float64(s.count), strings.Join(cols, ", "), trunc(s.norm, 160)), "", calledFrom(s), s.norm)))
					continue
				}
				key := t + "(" + strings.Join(cols, ",") + ")"
				// prefix dedupe: (a) is covered by an existing (a,b) on the same table; (a,b) supersedes an existing (a)
				covered := false
				for i := range acts {
					if acts[i].kind != "" || !strings.HasPrefix(acts[i].title, "Add an index on "+t+"(") {
						continue
					}
					ec := strings.Split(strings.TrimSuffix(strings.TrimPrefix(acts[i].title, "Add an index on "+t+"("), ")"), ", ")
					if isPrefix(cols, ec) {
						covered = true
						break
					}
					if isPrefix(ec, cols) {
						acts = append(acts[:i], acts[i+1:]...)
						break
					}
				}
				if covered {
					continue
				}
				if !seenIdx[key] {
					seenIdx[key] = true
					acts = append(acts, withShare(share, mk(ternary(share >= 5, "P1", "P2"), fmt.Sprintf("Add an index on %s(%s)", t, strings.Join(cols, ", ")), fmt.Sprintf("%.0f%% of SQL time, %d calls, examines ×%.0f more rows than it returns (avg %.1fms). Query: %s", share, s.count, ratio, 1000*s.sum/float64(s.count), trunc(s.norm, 160)), fmt.Sprintf("ALTER TABLE %s ADD INDEX idx_%s_%s (%s);", t, t, strings.Join(cols, "_"), strings.Join(cols, ", ")), ternary(calledFrom(s) == "" && isPdnsTable(t), "PowerDNS backend table, not app code: the bench's DNS queries (name-not-found 'water torture' included) hit MySQL. Apply the DDL to the pdns database; check pdns cache settings (cache-ttl / negquery-cache-ttl / query-cache-ttl) against the manual before raising them; consider moving the pdns DB off the app DB host", calledFrom(s)), s.norm)))
				}
			}
		}
		if apiReq > 0 && float64(s.count)/float64(apiReq) >= 3 {
			acts = append(acts, withShare(share, mk("P2", fmt.Sprintf("N+1 suspect: %.1f executions per request", float64(s.count)/float64(apiReq)), fmt.Sprintf("%d calls for %d API requests, %.0f%% of SQL time. Batch it (IN (...)), JOIN it, or cache it. Query: %s", s.count, apiReq, share, trunc(s.norm, 160)), "", calledFrom(s), s.norm)))
		}
	}
	// heavy endpoints
	shown := 0
	for _, e := range eps {
		if isInternalEp(e.key) {
			continue // periodic internal jobs: their own section (call interval × work per call), not "Speed up"
		}
		if shown >= 8 || strings.Contains(e.key, "static assets") {
			break
		}
		shown++
		avg := e.sum / float64(e.count)
		share := 0.0
		if totalReq > 0 {
			var tot float64
			for _, x := range eps {
				if !isInternalEp(x.key) {
					tot += x.sum
				}
			}
			if tot > 0 {
				share = 100 * e.sum / tot
			}
		}
		if avg >= 0.5 || share >= 25 {
			acts = append(acts, withShare(share, mk(ternary(avg >= 1 || share >= 40, "P1", "P2"), fmt.Sprintf("Speed up %s (avg %.0fms, %.0f%% of request time)", e.key, avg*1000, share), fmt.Sprintf("%d calls, p99 %.0fms. Look at the SQL it runs (tables in the path name are a hint) and whether it can be precomputed or cached within the consistency window the manual allows.", e.count, pct(e.rts, .99)*1000), "", epHandler[e.key])))
		}
	}
	// polling
	np := 0
	for _, p := range sa.polling {
		if isInternalEp(p.ep) {
			continue
		}
		if np >= 3 {
			break
		}
		np++
		rps := float64(p.calls) / w.secs
		if rps >= 10 && p.medGap <= 5 { // a gap of seconds is a user action repeated, not polling (POST /api/icon, 19 s)
			pshare := 0.0
			var tot float64
			for _, x := range eps {
				if !isInternalEp(x.key) {
					tot += x.sum
				}
			}
			for _, x := range eps {
				if x.key == p.ep && tot > 0 {
					pshare = 100 * x.sum / tot
				}
			}
			acts = append(acts, withShare(pshare, mk("P2", fmt.Sprintf("Polling: %s at %.0f req/s (median gap %.0fms)", p.ep, rps, p.medGap*1000), "The client re-polls as soon as it may. If the API has a server-directed interval field, raise it; otherwise make the polled endpoint cheap (cache within the allowed staleness) or switch to SSE/long-polling if the manual allows; check how the interval affects scoring.", "", "")))
		}
	}
	// profiling
	app := 0.0
	for _, p := range procs {
		n := strings.ToLower(p.name)
		if !(strings.Contains(n, "mysqld") || strings.Contains(n, "mariadbd") || strings.Contains(n, "postgres") || n == "nginx" || n == "isumeasure" || n == "bench") {
			app += p.avgPct
		}
	}
	if !pprofOK && (app >= 40 || v.kind == "app-cpu" || v.kind == "cpu") {
		acts = append(acts, mk("P2", "Enable pprof in the app (no CPU profile was captured)", "Add net/http/pprof (and fgprof) on :6060; the agent captures CPU/fgprof automatically during the next bench.", "import _ \"net/http/pprof\"  //  go func(){ http.ListenAndServe(\":6060\", nil) }()", ""))
	}
	// static through the app
	for _, e := range eps {
		if strings.Contains(e.key, "static assets") && e.viaApp > 0 {
			acts = append(acts, mk("P3", fmt.Sprintf("Serve static files from nginx (%d of %d went through the app)", e.viaApp, e.count), "Add location blocks with root/alias + expires so the app never sees asset requests.", "", ""))
		}
	}
	if v.kind == "lock" {
		acts = append(acts, mk("P1", "Reduce row-lock contention", "Check the processlist section for statements stuck in 'Waiting for lock', shorten transactions, move FOR UPDATE/SHARE off hot rows, and avoid serial updates on the same row.", "", ""))
	}
	if v.kind == "io" {
		acts = append(acts, mk("P1", "Cut disk writes", "Disable the slow log and binlog for the final run, set innodb_flush_log_at_trx_commit=2 (zz-perf.cnf), batch the highest-frequency INSERTs, and check that logs are not on a tiny volume.", "", ""))
	}
	if v.kind == "wait" {
		acts = append(acts, mk("P2", "CPU is idle: find what the bench waits for", "Look at p99 per endpoint, polling intervals, external HTTP calls (payment etc.), DB locks, and DB connection pool limits (SetMaxOpenConns). Raising concurrency in the app/DB may also help.", "", ""))
	}
	// order: P1 first, keep insertion order otherwise
	// drop duplicates (same title; e.g. two hosts' metrics producing "CPU is idle" twice)
	seenTitle := map[string]bool{}
	uniq := acts[:0]
	for _, a := range acts {
		if seenTitle[a.title] {
			continue
		}
		seenTitle[a.title] = true
		uniq = append(uniq, a)
	}
	acts = uniq
	for i := range acts {
		acts[i].kind = kindOf(acts[i].title)
	}
	sort.SliceStable(acts, func(i, j int) bool { return acts[i].priority < acts[j].priority })
	return acts
}

func trunc(s string, n int) string {
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}

func isPrefix(short, long []string) bool {
	if len(short) > len(long) {
		return false
	}
	for i := range short {
		if short[i] != long[i] {
			return false
		}
	}
	return true
}
