package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"html"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime/metrics"
	"sort"
	"strconv"
	"strings"
	"time"
)

// report: turn a collected run (measurements/<ts>/<host>/... or a single run dir) into report.md + report.html.
// Everything is computed in-process (alp/slp-like aggregation, 10s timeline, sessions); pprof text uses `go tool pprof` when available.

type hostRun struct {
	Name   string
	Dir    string
	Run    map[string]any
	Start  time.Time
	End    time.Time
	HTTP   []map[string]any
	Sys    []map[string]any
	MySQL  []map[string]any
	App    []map[string]any
	Procs  []map[string]any
	Access []accessEvent
	// slow log: aggregated while streaming (the entries are never kept — a 400 MB slow log must not need 400 MB of RAM)
	SlowN    int
	SlowInit int // statements issued before /initialize answered (excluded from the aggregation)
	// the measurement agent's own statements (SHOW GLOBAL STATUS, processlist, row counts …): excluded, counted here
	SlowAgent  int
	SlowAgentT float64
	Sqls       []*sqlStat
	Tsql       float64
	ByTag      map[string][]*sqlStat
	PL         string
	Pprof      map[string]string
}

type slowEntry struct {
	at       time.Time
	qt, lt   float64
	sent     int
	examined int
	sql      string
	tag      string // from a leading /* handler */ comment (see tools/sqltag)
	loc      string // "file.go:line" that issued it (sqltag's " @ file:line" suffix), "" when absent
}

var tagRe = regexp.MustCompile(`^/\*\s*(.*?)\s*\*/\s*`)
var braceParamRe = regexp.MustCompile(`\{[^}]+\}`)

// splitTag splits a leading sqltag comment `/* GET /api/x @ file.go:12 */` into the endpoint tag, the source
// location and the statement. Either part of the comment may be missing.
func splitTag(q string) (tag, loc, sql string) {
	if !strings.HasPrefix(q, "/*") {
		return "", "", q
	}
	m := tagRe.FindStringSubmatch(q)
	if m == nil {
		return "", "", q
	}
	c := m[1]
	if i := strings.LastIndex(c, "@ "); i >= 0 && (i == 0 || c[i-1] == ' ') {
		c, loc = strings.TrimSpace(c[:i]), strings.TrimSpace(c[i+2:])
	}
	return braceParamRe.ReplaceAllString(c, ":id"), loc, strings.TrimSpace(q[len(m[0]):])
}

func runReport(args []string) error {
	fs := flag.NewFlagSet("report", flag.ContinueOnError)
	out := fs.String("out", "", "output dir (default: the input dir)")
	groups := fs.String("groups", "", "alp.yml with matching_groups (+ names); default: alp.yml next to / above -code, or ./alp.yml. Legacy one-regex-per-line files work too")
	score := fs.String("score", "", "score to print in the summary")
	note := fs.String("note", "", "note to print in the summary")
	code := fs.String("code", envOr("CODE_DIR", ""), "Go source dir of the app (maps SQL statements to handlers statically)")
	// accept the positional dir anywhere: move non-flag args to the end so flag.Parse sees all flags
	var flags, pos []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if strings.HasPrefix(a, "-") {
			flags = append(flags, a)
			if !strings.Contains(a, "=") && i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
				i++
				flags = append(flags, args[i])
			}
		} else {
			pos = append(pos, a)
		}
	}
	if err := fs.Parse(append(flags, pos...)); err != nil {
		return err
	}
	if fs.NArg() < 1 {
		return fmt.Errorf("usage: isumeasure report <dir> [-out DIR] [-groups FILE] [-score N] [-note TEXT]")
	}
	dir := fs.Arg(0)
	if *out == "" {
		*out = dir
	}
	if mb, _ := strconv.Atoi(os.Getenv("ISUMEASURE_MEM_CAP_MB")); mb > 0 {
		go memWatchdog(mb)
	}
	ro, err := buildReport(dir, *groups, *score, *note, *code)
	if err != nil {
		return err
	}
	os.MkdirAll(*out, 0755)
	if err := writeReportFiles(*out, ro); err != nil {
		return err
	}
	fmt.Printf("%s/report.md\n%s/summary.md\n%s/actions.json\n%s/report.html\n", *out, *out, *out, *out)
	return nil
}

func writeReportFiles(out string, ro reportOut) error {
	if err := os.WriteFile(filepath.Join(out, "report.md"), []byte(ro.md), 0644); err != nil {
		return err
	}
	os.WriteFile(filepath.Join(out, "summary.md"), []byte(ro.summary), 0644)
	writeJSON(filepath.Join(out, "actions.json"), ro.actions)
	if ro.meta != nil {
		writeJSON(filepath.Join(out, "report.json"), ro.meta)
	}
	return os.WriteFile(filepath.Join(out, "report.html"), []byte(ro.html), 0644)
}

// ---------- loading ----------

func loadHostRun(dir, name string) (*hostRun, error) { return loadHostRunSlow(dir, name, true) }

// loadHostRunSlow: withSlow=false leaves the slow log to the caller (buildReport reads it once, after it knows
// when /initialize finished — reading a 200 MB slow log twice was half of a report's time).
func loadHostRunSlow(dir, name string, withSlow bool) (*hostRun, error) {
	h := &hostRun{Name: name, Dir: dir, Pprof: map[string]string{}}
	b, err := os.ReadFile(filepath.Join(dir, "run.json"))
	if err != nil {
		return nil, err
	}
	json.Unmarshal(b, &h.Run)
	if s, ok := h.Run["start"].(string); ok {
		h.Start, _ = time.Parse(time.RFC3339Nano, s)
	}
	if s, ok := h.Run["end"].(string); ok {
		h.End, _ = time.Parse(time.RFC3339Nano, s)
	}
	if h.Name == "" {
		h.Name, _ = h.Run["host"].(string)
	}
	h.HTTP = readJSONL(filepath.Join(dir, "http.jsonl"))
	h.Sys = readJSONL(filepath.Join(dir, "sys.jsonl"))
	h.MySQL = readJSONL(filepath.Join(dir, "mysql.jsonl"))
	h.App = readJSONL(filepath.Join(dir, "app.jsonl"))
	h.Procs = readJSONL(filepath.Join(dir, "procs.jsonl"))
	if f, err := openMaybeGz(filepath.Join(dir, "access.log")); err == nil {
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 1<<20), 1<<20)
		for sc.Scan() {
			ev := parseAccessLine(sc.Text(), time.Time{})
			if ev.method != "" {
				h.Access = append(h.Access, ev)
			}
		}
		f.Close()
	}
	if withSlow {
		h.loadSlow(time.Time{}) // buildReport loads it itself with the time /initialize finished
	}
	if b, err := os.ReadFile(filepath.Join(dir, "processlist.txt")); err == nil {
		h.PL = string(b)
	}
	for _, n := range []string{"cpu", "fgprof", "heap", "block", "mutex"} {
		if _, err := os.Stat(filepath.Join(dir, n+".pb.gz")); err == nil {
			h.Pprof[n] = filepath.Join(dir, n+".pb.gz")
		}
	}
	if h.Start.IsZero() && len(h.Access) > 0 {
		h.Start = h.Access[0].at
	}
	if h.End.IsZero() && len(h.Access) > 0 {
		h.End = h.Access[len(h.Access)-1].at
	}
	return h, nil
}

// loadSlow aggregates the slow log, skipping statements issued before loadFrom (= when /initialize answered,
// taken from the app host's access log): the data load and zone import are not request traffic, and on a DB-only
// host they would otherwise dominate per-request counts and N+1 suspects.
func (h *hostRun) loadSlow(loadFrom time.Time) {
	sa := newSlowAgg()
	h.SlowInit, h.SlowAgent, h.SlowAgentT = 0, 0, 0
	walkSlowLog(filepath.Join(h.Dir, "slow.log"), func(e slowEntry) {
		if isAgentSQL(e.tag, e.sql) {
			h.SlowAgent++
			h.SlowAgentT += e.qt
			return
		}
		if !loadFrom.IsZero() && !e.at.IsZero() && e.at.Before(loadFrom) {
			h.SlowInit++
			return
		}
		sa.add(e)
	})
	h.SlowN, h.Tsql, h.Sqls, h.ByTag = sa.n, sa.total, sa.finishAll(), sa.finishByTag()
}

// initializeEnd: the latest time any host's access log saw the /initialize request complete (zero if none).
func initializeEnd(hosts []*hostRun) time.Time {
	var t time.Time
	for _, h := range hosts {
		for _, ev := range h.Access {
			if strings.Contains(ev.uri, "initialize") {
				if ev.at.After(t) {
					t = ev.at
				}
				break
			}
		}
	}
	return t
}

func readJSONL(path string) []map[string]any {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	var out []map[string]any
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		var m map[string]any
		if json.Unmarshal(sc.Bytes(), &m) == nil {
			out = append(out, m)
		}
	}
	return out
}

var slowTimeRe = regexp.MustCompile(`^# Time: (\S+)`)
var slowStatRe = regexp.MustCompile(`^# Query_time: ([\d.]+)\s+Lock_time: ([\d.]+)\s+Rows_sent: (\d+)\s+Rows_examined: (\d+)`)

// walkSlowLog streams the slow log, calling fn per statement (nothing is retained here).
func walkSlowLog(path string, fn func(slowEntry)) {
	raw, err := openMaybeGz(path)
	if err != nil {
		return
	}
	// slices shipped by older agents still carry blob literals: collapse them while streaming (sqllit.go)
	f := shortenLiteralsReader(raw)
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 4<<20), 4<<20)
	var cur slowEntry
	var sqlLines []string
	flush := func() {
		if len(sqlLines) > 0 {
			q := strings.TrimSpace(strings.Join(sqlLines, " "))
			if q != "" && !strings.HasPrefix(q, "# administrator command") && !(len(q) >= 13 && strings.EqualFold(q[:13], "SET TIMESTAMP")) {
				cur.tag, cur.loc, cur.sql = splitTag(q)
				fn(cur)
			}
		}
		sqlLines = sqlLines[:0]
	}
	timeFresh := false // a "# Time:" header opened the current entry (MySQL: one per entry)
	for sc.Scan() {
		line := sc.Text()
		// the prefix tests keep the header regexps off statement lines (most of the bytes)
		if strings.HasPrefix(line, "# Time: ") {
			if m := slowTimeRe.FindStringSubmatch(line); m != nil {
				flush()
				cur = slowEntry{}
				cur.at, _ = time.Parse(time.RFC3339Nano, m[1])
				timeFresh = true
				continue
			}
		}
		// MariaDB writes "# Time:" only when the second changes: every entry starts at "# User@Host:"
		// (without this, all statements of one second were joined into one entry: 531 of 674,896 in run 20261028-150019)
		if strings.HasPrefix(line, "# User@Host: ") {
			if !timeFresh {
				flush()
				cur = slowEntry{}
			}
			timeFresh = false
			continue
		}
		// MariaDB's "# Time: 261029  0:09:38" is not RFC3339: take the entry time from SET timestamp (epoch, zone-free)
		if strings.HasPrefix(line, "SET timestamp=") {
			if cur.at.IsZero() {
				if n, err := strconv.ParseInt(strings.TrimSuffix(strings.TrimPrefix(line, "SET timestamp="), ";"), 10, 64); err == nil {
					cur.at = time.Unix(n, 0)
				}
			}
			continue
		}
		if strings.HasPrefix(line, "# Query_time: ") {
			if m := slowStatRe.FindStringSubmatch(line); m != nil {
				cur.qt, _ = strconv.ParseFloat(m[1], 64)
				cur.lt, _ = strconv.ParseFloat(m[2], 64)
				cur.sent, _ = strconv.Atoi(m[3])
				cur.examined, _ = strconv.Atoi(m[4])
				continue
			}
		}
		if strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "SET timestamp=") || strings.HasPrefix(line, "use ") {
			continue
		}
		sqlLines = append(sqlLines, line)
	}
	flush()
}

// ---------- normalization ----------

var sqlStrRe = regexp.MustCompile(`'(?:[^'\\]|\\.)*'|"(?:[^"\\]|\\.)*"`)
var sqlNumRe = regexp.MustCompile(`\b\d+(?:\.\d+)?\b`)
var sqlInRe = regexp.MustCompile(`IN\s*\((?:\s*\?\s*,?)+\)`)
var sqlValuesRe = regexp.MustCompile(`VALUES\s*\((?:[^()]*)\)(?:\s*,\s*\([^()]*\))*`)
var wsRe = regexp.MustCompile(`\s+`)

func normSQL(q string) string {
	q = shortenSQLLiterals(q) // blobs: one byte scan instead of five regexps over 100 KB (same result: ?)
	q = sqlStrRe.ReplaceAllString(q, "?")
	q = sqlNumRe.ReplaceAllString(q, "?")
	q = sqlValuesRe.ReplaceAllString(q, "VALUES (...)")
	q = sqlInRe.ReplaceAllString(q, "IN (...)")
	q = wsRe.ReplaceAllString(q, " ")
	return strings.TrimSpace(q)
}

// ---------- aggregation ----------

type epStat struct {
	key               string
	count, c2, c4, c5 int
	c304              int // conditional GET answered with 304 (the bench skips body validation there)
	sum, min, max     float64
	rts               []float64
	bytes             int64
	perBucket         map[int]int
	perBucketSum      map[int]float64 // Σ request time per bucket (avg per 10 s)
	viaApp            int
	starts            []time.Time // request start times, kept only for periodic internal endpoints (call interval)
}

func aggregateAccess(evs []accessEvent, nz *normalizer, t0 time.Time, bucket float64) ([]*epStat, int) {
	m := map[string]*epStat{}
	total := 0
	for _, ev := range evs {
		k := ev.method + " " + nz.norm(ev.uri)
		if isStatic(ev.uri) {
			k = ev.method + " (static assets: /assets, /images, *.js|css|svg|png …)"
		}
		e := m[k]
		if e == nil {
			e = &epStat{key: k, min: math.MaxFloat64, perBucket: map[int]int{}, perBucketSum: map[int]float64{}}
			m[k] = e
		}
		e.count++
		total++
		if ev.status == 304 {
			e.c304++
		}
		switch {
		case ev.status >= 500:
			e.c5++
		case ev.status >= 400:
			e.c4++
		default:
			e.c2++
		}
		e.sum += ev.reqtime
		if ev.reqtime < e.min {
			e.min = ev.reqtime
		}
		if ev.reqtime > e.max {
			e.max = ev.reqtime
		}
		e.rts = append(e.rts, ev.reqtime)
		e.bytes += int64(ev.size)
		if ev.apptime > 0 {
			e.viaApp++
		}
		if !ev.at.IsZero() && !t0.IsZero() {
			e.perBucket[int(ev.at.Sub(t0).Seconds()/bucket)]++
			e.perBucketSum[int(ev.at.Sub(t0).Seconds()/bucket)] += ev.reqtime
		}
		if isInternalEp(k) && !ev.at.IsZero() {
			e.starts = append(e.starts, ev.at.Add(-time.Duration(ev.reqtime*float64(time.Second))))
		}
	}
	out := make([]*epStat, 0, len(m))
	for _, e := range m {
		sort.Float64s(e.rts)
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].sum > out[j].sum })
	return out, total
}

func pct(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	i := int(float64(len(sorted)-1) * p)
	return sorted[i]
}

type sqlStat struct {
	norm           string
	count          int
	sum, max, lock float64
	sent, examined int
	example        string
	qts            []float64
	tags           map[string]int // endpoint tag -> count (runtime tagging)
	locs           map[string]int // "file.go:line" -> count (sqltag caller location)
	handlers       []string       // static match from the code (fallback)
}

// slowAgg accumulates per-normalized-statement stats (and per sqltag) as the slow log streams by.
// Memory is proportional to the number of distinct statements, not to the number of log entries.
type slowAgg struct {
	n     int
	total float64
	all   map[string]*sqlStat
	byTag map[string]map[string]*sqlStat
}

func newSlowAgg() *slowAgg {
	return &slowAgg{all: map[string]*sqlStat{}, byTag: map[string]map[string]*sqlStat{}}
}

func addSlowStat(m map[string]*sqlStat, n string, e slowEntry) {
	s := m[n]
	if s == nil {
		s = &sqlStat{norm: n, example: e.sql, tags: map[string]int{}, locs: map[string]int{}}
		m[n] = s
	}
	if e.tag != "" {
		s.tags[e.tag]++
	}
	if e.loc != "" {
		s.locs[e.loc]++
	}
	s.count++
	s.sum += e.qt
	if e.qt > s.max {
		s.max = e.qt
	}
	s.lock += e.lt
	s.sent += e.sent
	s.examined += e.examined
	if len(s.qts) < 200000 { // percentiles; cap so a 10M-line log stays bounded
		s.qts = append(s.qts, e.qt)
	}
}

func (a *slowAgg) add(e slowEntry) {
	n := normSQL(e.sql)
	a.n++
	a.total += e.qt
	addSlowStat(a.all, n, e)
	if e.tag != "" {
		m := a.byTag[e.tag]
		if m == nil {
			m = map[string]*sqlStat{}
			a.byTag[e.tag] = m
		}
		addSlowStat(m, n, e)
	}
}

func finishSlowStats(m map[string]*sqlStat) []*sqlStat {
	out := make([]*sqlStat, 0, len(m))
	for _, s := range m {
		sort.Float64s(s.qts)
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].sum > out[j].sum })
	return out
}

func (a *slowAgg) finishAll() []*sqlStat { return finishSlowStats(a.all) }

// finishByTag: slow-log time per endpoint tag (only when sqltag is in use). Tags carry the router's parameter
// names (GET /api/user/:username/icon); endpoint keys use :id — keyed by the :id form so the two join.
func (a *slowAgg) finishByTag() map[string][]*sqlStat {
	out := map[string][]*sqlStat{}
	for tag, m := range a.byTag {
		k := routeParamRe.ReplaceAllString(tag, ":id")
		out[k] = append(out[k], finishSlowStats(m)...)
	}
	for k := range out {
		sort.Slice(out[k], func(i, j int) bool { return out[k][i].sum > out[k][j].sum })
	}
	return out
}

func calledFrom(q *sqlStat) string {
	if len(q.tags) > 0 {
		type kv struct {
			k string
			n int
		}
		var l []kv
		for k, n := range q.tags {
			l = append(l, kv{k, n})
		}
		sort.Slice(l, func(i, j int) bool { return l[i].n > l[j].n })
		var parts []string
		for i, x := range l {
			if i >= 3 {
				parts = append(parts, fmt.Sprintf("+%d", len(l)-3))
				break
			}
			parts = append(parts, fmt.Sprintf("%s ×%d", x.k, x.n))
		}
		return strings.Join(parts, ", ")
	}
	if len(q.handlers) > 0 {
		return "code: " + strings.Join(q.handlers, ", ")
	}
	return ""
}

// processlist snapshots: how often each normalized statement was seen running, and its longest observed time
type plStat struct {
	norm  string
	seen  int
	maxT  int
	state map[string]int
}

func aggregateProcesslist(txt string) []*plStat {
	m := map[string]*plStat{}
	for _, line := range strings.Split(txt, "\n") {
		if strings.HasPrefix(line, "===") || strings.HasPrefix(line, "id\t") || strings.TrimSpace(line) == "" {
			continue
		}
		fs := strings.Split(line, "\t")
		if len(fs) < 6 {
			continue
		}
		t, _ := strconv.Atoi(fs[3])
		if tag, _, q := splitTag(strings.ReplaceAll(fs[5], "\\n", " ")); isAgentSQL(tag, q) {
			continue // the agent's own snapshot / status queries
		}
		n := normSQL(strings.ReplaceAll(fs[5], "\\n", " "))
		s := m[n]
		if s == nil {
			s = &plStat{norm: n, state: map[string]int{}}
			m[n] = s
		}
		s.seen++
		if t > s.maxT {
			s.maxT = t
		}
		s.state[fs[4]]++
	}
	out := make([]*plStat, 0, len(m))
	for _, s := range m {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].seen > out[j].seen })
	return out
}

// ---------- timeline (10s grid) ----------

type bucketRow struct {
	idx  int
	vals map[string]float64 // metric -> value (avg over the bucket, or max for *_max)
	n    map[string]int
}

func buildTimeline(h *hostRun, t0 time.Time, bucket float64) map[int]*bucketRow {
	rows := map[int]*bucketRow{}
	get := func(i int) *bucketRow {
		r := rows[i]
		if r == nil {
			r = &bucketRow{idx: i, vals: map[string]float64{}, n: map[string]int{}}
			rows[i] = r
		}
		return r
	}
	add := func(i int, k string, v float64) { r := get(i); r.vals[k] += v; r.n[k]++ }
	setMax := func(i int, k string, v float64) {
		r := get(i)
		if v > r.vals[k] {
			r.vals[k] = v
		}
		r.n[k] = 1
	}
	parseT := func(s string) time.Time {
		t, _ := parseSec(s, h.Start.Location())
		return t
	}
	idx := func(t time.Time) int { return int(math.Floor(t.Sub(t0).Seconds() / bucket)) }
	for _, m := range h.HTTP {
		t := parseT(str(m["t"]))
		if t.IsZero() {
			continue
		}
		i := idx(t)
		add(i, "req_s", num(m["req"]))
		add(i, "err5xx_s", num(m["err_5xx"]))
		add(i, "err4xx_s", num(m["err_4xx"]))
		setMax(i, "p99_ms", num(m["reqtime_p99"])*1000)
		setMax(i, "max_ms", num(m["reqtime_max"])*1000)
		if r := num(m["req"]); r > 0 {
			add(i, "avg_ms", num(m["reqtime_sum"])/r*1000)
		}
	}
	for _, m := range h.Sys {
		i := idx(parseT(str(m["t"])))
		for _, k := range []string{"cpu_user_pct", "cpu_sys_pct", "cpu_iowait_pct", "load1", "mem_used_pct", "disk_util_pct", "disk_write_mb_s", "disk_read_mb_s", "net_rx_mb_s", "net_tx_mb_s", "ctxt_per_s"} {
			add(i, k, num(m[k]))
		}
		setMax(i, "tcp_inuse", num(m["tcp_inuse"]))
		setMax(i, "tcp_timewait", num(m["tcp_timewait"]))
	}
	for _, m := range h.MySQL {
		i := idx(parseT(str(m["t"])))
		for _, k := range []string{"questions_per_s", "com_select_per_s", "com_insert_per_s", "com_update_per_s", "com_delete_per_s", "com_commit_per_s", "innodb_row_lock_waits_per_s", "innodb_row_lock_time_per_s", "select_scan_per_s", "innodb_data_fsyncs_per_s", "created_tmp_disk_tables_per_s", "handler_read_rnd_next_per_s"} {
			add(i, k, num(m[k]))
		}
		setMax(i, "threads_running", num(m["threads_running"]))
		setMax(i, "threads_connected", num(m["threads_connected"]))
	}
	for _, m := range h.App {
		i := idx(parseT(str(m["t"])))
		for k, v := range m {
			if k == "t" {
				continue
			}
			if f, ok := v.(float64); ok {
				setMax(i, "app."+k, f)
			}
		}
	}
	for _, r := range rows {
		for k, n := range r.n {
			if n > 1 {
				r.vals[k] /= float64(n)
			}
		}
	}
	return rows
}

func num(v any) float64 {
	switch t := v.(type) {
	case float64:
		return t
	case int:
		return float64(t)
	case string:
		f, _ := strconv.ParseFloat(t, 64)
		return f
	}
	return 0
}
func str(v any) string {
	s, _ := v.(string)
	return s
}

// ---------- sessions (bench behaviour from the access log) ----------

type transStat struct {
	from, to string
	n        int
	gaps     []float64
	overlap  int // the next request started before the previous response ended (the client runs requests in parallel)
}

// fmtGap prints a think time with the precision the access log has (msec = 1 ms): the bench usually sends the
// next request within a few ms of the previous response, which "%.2fs" showed as 0.00 s.
func fmtGap(sec float64) string {
	switch {
	case sec < 0.0005:
		return "<1 ms"
	case sec < 1:
		return fmt.Sprintf("%.0f ms", sec*1000)
	default:
		return fmt.Sprintf("%.2f s", sec)
	}
}

type sessionAnalysis struct {
	usersPerBucket map[int]int
	roles          map[string]map[int]int // cookie name -> bucket -> active sessions
	roleSessions   map[string]int
	uas            map[string]int
	transitions    []*transStat
	polling        []pollStat
	sessions       int
}

type pollStat struct {
	ep       string
	sessions int
	medGap   float64
	calls    int
}

var staticRe = regexp.MustCompile(`(?i)^/(assets|static|images|img|css|js|fonts)/|\.(js|css|png|jpe?g|gif|svg|ico|woff2?|ttf|map|webp)$`)

func isStatic(uri string) bool {
	if i := strings.IndexByte(uri, '?'); i >= 0 {
		uri = uri[:i]
	}
	return staticRe.MatchString(uri)
}

func analyzeSessions(evs []accessEvent, nz *normalizer, t0 time.Time, bucket float64) sessionAnalysis {
	sa := sessionAnalysis{usersPerBucket: map[int]int{}, uas: map[string]int{}, roles: map[string]map[int]int{}, roleSessions: map[string]int{}}
	bySess := map[string][]accessEvent{}
	seenInBucket := map[int]map[string]bool{}
	roleSeen := map[string]map[string]bool{}
	for _, ev := range evs {
		sa.uas[ev.ua]++
		if ev.sess == "" || isStatic(ev.uri) {
			continue
		}
		role := ev.sess
		if i := strings.IndexByte(role, '='); i >= 0 {
			role = role[:i]
		}
		if roleSeen[role] == nil {
			roleSeen[role] = map[string]bool{}
			sa.roles[role] = map[int]int{}
		}
		roleSeen[role][ev.sess] = true
		bySess[ev.sess] = append(bySess[ev.sess], ev)
		i := int(ev.at.Sub(t0).Seconds() / bucket)
		if seenInBucket[i] == nil {
			seenInBucket[i] = map[string]bool{}
		}
		seenInBucket[i][ev.sess] = true
	}
	for i, s := range seenInBucket {
		sa.usersPerBucket[i] = len(s)
		for sess := range s {
			role := sess
			if j := strings.IndexByte(role, '='); j >= 0 {
				role = role[:j]
			}
			sa.roles[role][i]++
		}
	}
	for role, m := range roleSeen {
		sa.roleSessions[role] = len(m)
	}
	sa.sessions = len(bySess)
	tr := map[string]*transStat{}
	pollGaps := map[string][]float64{}
	pollSess := map[string]map[string]bool{}
	pollCalls := map[string]int{}
	start := func(ev accessEvent) time.Time { return ev.at.Add(-time.Duration(ev.reqtime * float64(time.Second))) }
	for sess, list := range bySess {
		sort.Slice(list, func(i, j int) bool { return start(list[i]).Before(start(list[j])) })
		lastByEp := map[string]time.Time{}
		for i := range list {
			ep := list[i].method + " " + nz.normQuiet(list[i].uri)
			if i > 0 {
				prev := list[i-1].method + " " + nz.normQuiet(list[i-1].uri)
				k := prev + "→" + ep
				t := tr[k]
				if t == nil {
					t = &transStat{from: prev, to: ep}
					tr[k] = t
				}
				t.n++
				// think time = next request start - previous response end (nginx stamps the completion time)
				gap := start(list[i]).Sub(list[i-1].at).Seconds()
				if gap < 0 {
					gap = 0 // concurrent requests within one session
					t.overlap++
				}
				t.gaps = append(t.gaps, gap)
			}
			if last, ok := lastByEp[ep]; ok {
				pollGaps[ep] = append(pollGaps[ep], start(list[i]).Sub(last).Seconds())
			}
			lastByEp[ep] = start(list[i])
			if pollSess[ep] == nil {
				pollSess[ep] = map[string]bool{}
			}
			pollSess[ep][sess] = true
			pollCalls[ep]++
		}
	}
	for _, t := range tr {
		sort.Float64s(t.gaps)
		sa.transitions = append(sa.transitions, t)
	}
	sort.Slice(sa.transitions, func(i, j int) bool { return sa.transitions[i].n > sa.transitions[j].n })
	for ep, gaps := range pollGaps {
		if len(gaps) < 20 {
			continue
		}
		sort.Float64s(gaps)
		sa.polling = append(sa.polling, pollStat{ep: ep, sessions: len(pollSess[ep]), medGap: gaps[len(gaps)/2], calls: pollCalls[ep]})
	}
	sort.Slice(sa.polling, func(i, j int) bool { return sa.polling[i].calls > sa.polling[j].calls })
	return sa
}

// ---------- pprof text (optional, needs the go toolchain) ----------

// pprofSVG writes <profile>.svg (call graph via graphviz) next to the profile and returns its path, "" when go/dot are missing.
func pprofSVG(path string) string {
	goBin, dot := "", ""
	for _, c := range []string{"go", "/usr/local/go/bin/go"} {
		if p, err := exec.LookPath(c); err == nil {
			goBin = p
			break
		}
	}
	if p, err := exec.LookPath("dot"); err == nil {
		dot = p
	}
	if goBin == "" || dot == "" {
		return ""
	}
	out := strings.TrimSuffix(strings.TrimSuffix(path, ".gz"), ".pb") + ".svg"
	if _, err := os.Stat(out); err == nil {
		return out
	}
	if b, err := exec.Command(goBin, "tool", "pprof", "-svg", "-nodecount=60", path).Output(); err == nil && len(b) > 0 {
		os.WriteFile(out, b, 0644)
		return out
	}
	return ""
}

func pprofTop(path string, nodes int) string {
	goBin := ""
	for _, c := range []string{"go", "/usr/local/go/bin/go", "/home/isucon/local/golang/bin/go"} {
		if p, err := exec.LookPath(c); err == nil {
			goBin = p
			break
		}
	}
	if goBin == "" {
		return "(go toolchain not available; open the profile with: go tool pprof -http=:8081 " + filepath.Base(path) + ")"
	}
	out, err := exec.Command(goBin, "tool", "pprof", "-top", "-nodecount="+strconv.Itoa(nodes), path).CombinedOutput()
	if err != nil {
		return "pprof failed: " + err.Error() + "\n" + string(out)
	}
	return string(out)
}

// ---------- rendering ----------

type series struct {
	name  string
	color string
	pts   []float64
}

var palette = []string{"#0f766e", "#2563eb", "#d97706", "#7c3aed", "#db2777", "#059669", "#dc2626", "#4b5563"}

func lineChart(title, unit string, ss []series, w, h int) string {
	if len(ss) == 0 {
		return ""
	}
	maxv := 0.0
	n := 0
	for _, s := range ss {
		for _, v := range s.pts {
			if v > maxv {
				maxv = v
			}
		}
		if len(s.pts) > n {
			n = len(s.pts)
		}
	}
	if maxv == 0 {
		maxv = 1
	}
	left, top, right, bottom := 48, 36, 10, 22 // top leaves room for the title line and a separate legend line
	pw, ph := float64(w-left-right), float64(h-top-bottom)
	var b strings.Builder
	fmt.Fprintf(&b, `<svg class="chart" width="%d" height="%d" viewBox="0 0 %d %d">`, w, h, w, h)
	fmt.Fprintf(&b, `<text x="%d" y="14" font-size="12" font-weight="600" fill="#1f2937">%s%s</text>`, left, html.EscapeString(title), ternary(unit != "", ` <tspan font-weight="400" fill="#6b7280">(`+html.EscapeString(unit)+`)</tspan>`, ""))
	for i := 0; i <= 4; i++ {
		y := float64(top) + ph*float64(i)/4
		v := maxv * float64(4-i) / 4
		fmt.Fprintf(&b, `<line x1="%d" y1="%.1f" x2="%d" y2="%.1f" stroke="#e5e7eb"/><text x="%d" y="%.1f" font-size="10" text-anchor="end" fill="#6b7280">%s</text>`, left, y, w-right, y, left-4, y+3, fmtNum(v))
	}
	for i := 0; i < n; i += max(1, n/6) {
		x := float64(left) + pw*float64(i)/float64(max(1, n-1))
		fmt.Fprintf(&b, `<text x="%.1f" y="%d" font-size="10" text-anchor="middle" fill="#6b7280">%ds</text>`, x, h-6, i*10)
	}
	for si, s := range ss {
		var pts []string
		for i, v := range s.pts {
			x := float64(left) + pw*float64(i)/float64(max(1, n-1))
			y := float64(top) + ph - ph*v/maxv
			pts = append(pts, fmt.Sprintf("%.1f,%.1f", x, y))
		}
		c := s.color
		if c == "" {
			c = palette[si%len(palette)]
		}
		fmt.Fprintf(&b, `<polyline fill="none" stroke="%s" stroke-width="1.6" points="%s"/>`, c, strings.Join(pts, " "))
	}
	// legend on its own line under the title; width follows the label length (≈6px per char) so many series do not overlap
	lx := left
	for si, s := range ss {
		c := s.color
		if c == "" {
			c = palette[si%len(palette)]
		}
		fmt.Fprintf(&b, `<rect x="%d" y="%d" width="10" height="3" fill="%s"/><text x="%d" y="%d" font-size="10" fill="#374151">%s</text>`, lx, 24, c, lx+14, 28, html.EscapeString(s.name))
		lx += 14 + 6*len(s.name) + 12
	}
	b.WriteString(`</svg>`)
	return b.String()
}

// cpuChart: CPU by process with what the Verdict needs to be read — the ceiling (cores × 100%) as a red dashed line,
// round ticks, the load window as a band, seconds since the run start on the x axis, each process's average in the legend.
func cpuChart(ss []series, avgs map[string]float64, ceil float64, b0, b1 int, bucket float64, w, h int) string {
	if len(ss) == 0 {
		return ""
	}
	maxv, n := ceil, 0
	for _, s := range ss {
		for _, v := range s.pts {
			maxv = math.Max(maxv, v)
		}
		n = max(n, len(s.pts))
	}
	step := niceStep(maxv / 4)
	top := math.Ceil(maxv/step) * step
	if top <= 0 {
		top, step = 100, 25
	}
	left, tp, right, bottom := 48, 40, 64, 34
	pw, ph := float64(w-left-right), float64(h-tp-bottom)
	X := func(i int) float64 { return float64(left) + pw*float64(i)/float64(max(1, n-1)) }
	Y := func(v float64) float64 { return float64(tp) + ph - ph*v/top }
	var b strings.Builder
	type leg struct {
		x, row int
		label  string
		c      string
	}
	var legs []leg
	lx, row := left, 0
	for si, s := range ss {
		c := s.color
		if c == "" {
			c = palette[si%len(palette)]
		}
		label := s.name
		if a, ok := avgs[s.name]; ok { // the same load-window average as the Verdict's "CPU by process" line
			label += fmt.Sprintf(" avg %.0f%%", a)
		}
		wd := 14 + 6*len(label) + 12
		if lx+wd > w-right && lx > left {
			lx, row = left, row+1
		}
		legs = append(legs, leg{lx, row, label, c})
		lx += wd
	}
	tp += 12 * row
	h += 12 * row
	ph = float64(h - tp - bottom)
	fmt.Fprintf(&b, `<svg class="chart i18n" width="%d" height="%d" viewBox="0 0 %d %d">`, w, h, w, h)
	fmt.Fprintf(&b, `<text x="%d" y="14" font-size="12" font-weight="600" fill="#1f2937">CPU by process (100%% = one core)</text>`, left)
	for _, l := range legs {
		fmt.Fprintf(&b, `<rect x="%d" y="%d" width="10" height="3" fill="%s"/><text x="%d" y="%d" font-size="10" fill="#374151">%s</text>`, l.x, 26+12*l.row, l.c, l.x+14, 30+12*l.row, html.EscapeString(l.label))
	}
	if b0 >= 0 && b1 > b0 {
		x0, x1 := X(max(0, b0)), X(min(n-1, b1))
		fmt.Fprintf(&b, `<rect x="%.1f" y="%d" width="%.1f" height="%.1f" fill="#fde68a" opacity=".35"/><text x="%.1f" y="%d" font-size="10" fill="#92400e">load window</text>`, x0, tp, math.Max(1, x1-x0), ph, x0+3, tp+11)
	}
	for v := 0.0; v <= top+1e-9; v += step {
		y := Y(v)
		fmt.Fprintf(&b, `<line x1="%d" y1="%.1f" x2="%d" y2="%.1f" stroke="#e5e7eb"/><text x="%d" y="%.1f" font-size="10" text-anchor="end" fill="#6b7280">%g%%</text>`, left, y, w-right, y, left-4, y+3, v)
	}
	if ceil > 0 {
		y := Y(ceil)
		fmt.Fprintf(&b, `<line x1="%d" y1="%.1f" x2="%d" y2="%.1f" stroke="#dc2626" stroke-width="1.5" stroke-dasharray="6 4"/><text x="%d" y="%.1f" font-size="10" fill="#dc2626">ceiling %g%%</text>`, left, y, w-right, y, w-right+4, y+3, ceil)
	}
	for i := 0; i < n; i += max(1, n/6) {
		fmt.Fprintf(&b, `<text x="%.1f" y="%d" font-size="10" text-anchor="middle" fill="#6b7280">%.0fs</text>`, X(i), h-20, float64(i)*bucket)
	}
	fmt.Fprintf(&b, `<text x="%.1f" y="%d" font-size="10" text-anchor="middle" fill="#6b7280">seconds since the run start</text>`, float64(left)+pw/2, h-6)
	for si, s := range ss {
		c := s.color
		if c == "" {
			c = palette[si%len(palette)]
		}
		var pts []string
		for i, v := range s.pts {
			pts = append(pts, fmt.Sprintf("%.1f,%.1f", X(i), Y(v)))
		}
		fmt.Fprintf(&b, `<polyline fill="none" stroke="%s" stroke-width="1.6" points="%s"/>`, c, strings.Join(pts, " "))
	}
	b.WriteString(`</svg>`)
	return b.String()
}

// niceStep rounds a raw tick step up to 1 / 2 / 2.5 / 5 × 10^k.
func niceStep(raw float64) float64 {
	if raw <= 0 {
		return 1
	}
	p := math.Pow(10, math.Floor(math.Log10(raw)))
	for _, m := range []float64{1, 2, 2.5, 5, 10} {
		if raw <= m*p {
			return m * p
		}
	}
	return 10 * p
}

func fmtNum(v float64) string {
	switch {
	case v >= 1e6:
		return fmt.Sprintf("%.1fM", v/1e6)
	case v >= 1e3:
		return fmt.Sprintf("%.1fk", v/1e3)
	case v >= 100:
		return fmt.Sprintf("%.0f", v)
	case v >= 10:
		return fmt.Sprintf("%.1f", v)
	}
	return fmt.Sprintf("%.2f", v)
}

func ms(v float64) string { return fmt.Sprintf("%.1f", v*1000) }

type reportOut struct {
	md, summary, html string
	actions           []map[string]any
	meta              map[string]any // report.json: bench shape, score_post count … (the panel's timeline reads it)
}

func buildReport(dir string, groupsFile string, score, note, codeDir string) (reportOut, error) {
	var ro reportOut
	var hosts []*hostRun
	if _, err := os.Stat(filepath.Join(dir, "run.json")); err == nil {
		h, err := loadHostRunSlow(dir, "", false)
		if err != nil {
			return ro, err
		}
		hosts = append(hosts, h)
	} else {
		entries, _ := os.ReadDir(dir)
		for _, e := range entries {
			if e.IsDir() {
				if h, err := loadHostRunSlow(filepath.Join(dir, e.Name()), e.Name(), false); err == nil {
					hosts = append(hosts, h)
				}
			}
		}
	}
	if len(hosts) == 0 {
		return ro, fmt.Errorf("no run.json found under %s", dir)
	}
	initEnd := initializeEnd(hosts) // zero when no /initialize was seen: then nothing is skipped
	for _, h := range hosts {
		h.loadSlow(initEnd)
	}
	var ci *codeIndex
	if codeDir != "" {
		ci = loadCodeIndex(codeDir)
	}
	// endpoint grouping: alp.yml (contest repo) → router (code index) → id substitution; one normalizer for everything
	gpath := findGroupsFile(groupsFile, codeDir)
	gcfg, gerr := loadGroups(gpath)
	if gerr != nil {
		return ro, gerr
	}
	nz := newNormalizer(gcfg.groups, gcfg.src, ci)
	// SQL -> code links: the code dir's git remote + the commit the bench ran (first host that recorded one)
	var cl *codeLinker
	for _, h := range hosts {
		if git, _ := h.Run["git"].(map[string]any); git != nil && str(git["commit"]) != "" {
			cl = newCodeLinker(codeDir, str(git["commit"]))
			break
		}
	}
	codeIdx := "code index: not configured (-code / CODE_DIR) — SQL is not mapped to code lines"
	if ci != nil {
		codeIdx = fmt.Sprintf("code index: %d funcs, %d SQL literals, %d routes (%s)", len(ci.fileOf), ci.nSQL, len(ci.routeOf), codeDir)
		if ci.nSQL == 0 {
			codeIdx = fmt.Sprintf("⚠ code index is empty (%s): SQL → code and router grouping are off. On the ops host: task ops:repo, then task ops:restart", codeDir)
		}
	}
	t0 := hosts[0].Start
	tEnd := hosts[0].End
	for _, h := range hosts {
		if !h.Start.IsZero() && h.Start.Before(t0) {
			t0 = h.Start
		}
		if h.End.After(tEnd) {
			tEnd = h.End
		}
	}
	bucket := 10.0
	nb := int(math.Ceil(tEnd.Sub(t0).Seconds()/bucket)) + 1
	if nb < 1 {
		nb = 1
	}
	ts := filepath.Base(dir)

	ro.meta = map[string]any{}
	var md, hb strings.Builder
	e := func(s string) string { return html.EscapeString(strings.ToValidUTF8(s, "\uFFFD")) } // raw log text can carry binary bytes (image INSERTs)
	// ---- summary ----
	fmt.Fprintf(&md, "# Bench report %s\n\n", ts)
	fmt.Fprintf(&md, "endpoint grouping: %s\n\n%s\n\n", nz.describe(), codeIdx)
	benchCommit := ""
	for _, h := range hosts {
		if git, _ := h.Run["git"].(map[string]any); git != nil && str(git["commit"]) != "" {
			benchCommit = str(git["commit"])
			break
		}
	}
	rh := readRepoHead(repoRootOf(codeDir), benchCommit)
	repoLine := rh.line(gpath, gcfg.scorePost)
	md.WriteString(repoLine + "\n\n")
	md.WriteString("app-analysis (screens → API → handler → tables): `/app/` on the panel\n\n")
	appPurpose := appPurposes(repoRootOf(codeDir))
	// epApp: the endpoint's purpose from docs/app-annotations.json as a tooltip, and a small link to the app-analysis
	// report's Endpoints tab (it has no per-endpoint anchor); both empty when the endpoint is not annotated.
	epApp := func(key string) (title, link string) {
		p := appPurpose[paramKey(key)]
		if p == "" {
			return "", ""
		}
		return ` title="` + e("app-map: "+p) + `"`, ` <a class="appl2" href="/app/#endpoints" target="_blank" title="` + e(p) + `">app-map ↗</a>`
	}
	if rh != nil {
		ro.meta["repo"] = map[string]any{"root": rh.root, "head": rh.sha, "subject": rh.subject, "committed_at": rh.at.Format(time.RFC3339), "bench_commit": benchCommit, "bench_commit_in_clone": rh.hasBench == 1, "alp_yml": gpath, "score_post": gcfg.scorePost}
	}
	if ci == nil && len(nz.groups) == 0 {
		md.WriteString("> ⚠ **No alp.yml and no code index**: endpoints are grouped by id substitution only, so usernames/slugs split one endpoint into many rows and the heaviest endpoint can be missing from the top 10. Commit `isumeasure groups -code webapp/go > alp.yml` (kickoff phase 2), or on ops run `task ops:repo`.\n\n")
	} else if ci == nil {
		md.WriteString("> note: no code index (CODE_DIR / -code): SQL → handler mapping and router-based grouping are off; alp.yml does the grouping.\n\n")
	}
	fmt.Fprintf(&md, "- window: %s → %s (%.0fs) · hosts: %d", t0.Format(time.RFC3339), tEnd.Format(time.RFC3339), tEnd.Sub(t0).Seconds(), len(hosts))
	if score != "" {
		fmt.Fprintf(&md, " · **score %s**", score)
	}
	if note != "" {
		fmt.Fprintf(&md, " · %s", note)
	}
	md.WriteString("\n\n| host | requests | 5xx | git | trigger | end | pprof | services |\n|---|---|---|---|---|---|---|---|\n")
	hb.WriteString(`<section><h2>Summary</h2>`)
	fmt.Fprintf(&hb, `<div class="muted">%s → %s (%.0fs) · %d host(s)%s%s</div>`, e(t0.Format(time.RFC3339)), e(tEnd.Format(time.RFC3339)), tEnd.Sub(t0).Seconds(), len(hosts), ternary(score != "", ` · <b>score `+e(score)+`</b>`, ""), ternary(note != "", " · "+e(note), ""))
	hb.WriteString(`<div class="tw"><table><tr><th>host</th><th>requests</th><th>5xx</th><th>git</th><th>trigger</th><th>end</th><th>pprof</th><th>services</th></tr>`)
	benchSignal := parseAnyTime(readBenchDone(dir)["at"]) // the "bench finished" signal (task bench:done), if any
	for _, h := range hosts {
		endCell := strings.TrimPrefix(endDiag(h.Run, benchSignal), "end: ") // e.g. "idle after 10 s idle; signal +35 s"
		git, _ := h.Run["git"].(map[string]any)
		g := ""
		if git != nil {
			g = fmt.Sprintf("%s@%s", str(git["branch"]), str(git["commit"]))
			if num(git["dirty_files"]) > 0 {
				g += fmt.Sprintf(" (dirty %d)", int(num(git["dirty_files"])))
			}
		}
		pp := []string{}
		if pm, ok := h.Run["pprof"].(map[string]any); ok {
			for k, v := range pm {
				if !strings.HasPrefix(str(v), "error") {
					pp = append(pp, k)
				}
			}
		}
		sort.Strings(pp)
		var svc []string
		if sl, ok := h.Run["services"].([]any); ok {
			for _, x := range sl {
				if m, ok := x.(map[string]any); ok && str(m["active"]) == "active" {
					svc = append(svc, strings.TrimSuffix(str(m["unit"]), ".service"))
				}
			}
		}
		fmt.Fprintf(&md, "| %s | %d | %d | %s | %s | %s | %s | %s |\n", h.Name, int(num(h.Run["req_total"])), int(num(h.Run["err_total"])), g, str(h.Run["trigger"]), endCell, strings.Join(pp, ","), strings.Join(svc, " "))
		fmt.Fprintf(&hb, `<tr><td><b>%s</b></td><td class="num">%d</td><td class="num %s">%d</td><td><code>%s</code></td><td><code>%s</code></td><td>%s</td><td>%s</td><td class="muted">%s</td></tr>`, e(h.Name), int(num(h.Run["req_total"])), lvl(num(h.Run["err_total"]), 1, 50), int(num(h.Run["err_total"])), e(g), e(str(h.Run["trigger"])), e(endCell), e(strings.Join(pp, ", ")), e(strings.Join(svc, " · ")))
	}
	hb.WriteString(`</table></div>`)
	pfails := pprofFailures(hosts)
	if len(pfails) > 0 {
		md.WriteString("\nprofiles not collected:\n\n")
		hb.WriteString(`<div class="muted" style="margin-top:6px"><b>profiles not collected</b><ul>`)
		for _, f := range pfails {
			fmt.Fprintf(&md, "- %s\n", f)
			hb.WriteString("<li>" + e(f.String()) + "</li>")
		}
		md.WriteString("\n")
		hb.WriteString(`</ul></div>`)
	}
	hb.WriteString(`</section>`)

	// ---- verdict, time breakdown, next actions (per host; the DB host usually tells the story) ----
	type hostDiag struct {
		h       *hostRun
		w       loadWindow
		procs   []procAgg
		m       winMetrics
		v       verdict
		eps     []*epStat
		total   int
		sqls    []*sqlStat
		tsql    float64
		byTag   map[string][]*sqlStat
		sqlFrom string // hosts whose slow log fed byTag when it is not this host (DB on another box)
		sa      sessionAnalysis
		acts    []action
		epH     map[string]string // endpoint key -> handler func (code index)
	}
	var diags []hostDiag
	wins := fleetLoadWindows(hosts)
	for hi, h := range hosts {
		d := hostDiag{h: h}
		d.w = wins[hi]
		d.procs = procBreakdown(h, d.w)
		d.m = windowMetrics(h, d.w)
		d.v = diagnose(h, d.w, d.procs, d.m)
		d.eps, d.total = aggregateAccess(h.Access, nz, t0, bucket)
		d.sqls, d.tsql = h.Sqls, h.Tsql
		if ci != nil {
			for _, q := range d.sqls {
				q.handlers = ci.handlersFor(q.norm)
			}
		}
		d.byTag = h.ByTag
		d.sa = analyzeSessions(h.Access, nz, t0, bucket)
		pprofOK := false
		if pm, ok := h.Run["pprof"].(map[string]any); ok {
			if c, ok := pm["cpu"].(string); ok && !strings.HasPrefix(c, "error") {
				pprofOK = true
			}
		}
		epHandler := map[string]string{}
		if ci != nil {
			byRoute := map[string]string{}
			for fn, route := range ci.routeOf {
				byRoute[paramKey(route)] = fn
			}
			for _, e := range d.eps {
				if fn, ok := byRoute[paramKey(e.key)]; ok {
					epHandler[e.key] = fn + "()" + ternary(len(ci.sqlOf[fn]) > 0, fmt.Sprintf(" (%d SQL)", len(ci.sqlOf[fn])), "")
				}
			}
		}
		// DNS years: PowerDNS's own SQL (records/domains/domainmetadata) can be most of an app host's mysqld — say so
		if d.tsql > 0 {
			pd, pdnsCPU := 0.0, 0.0
			for _, q := range d.sqls {
				if pdnsSQLRe.MatchString(q.norm) {
					pd += q.sum
				}
			}
			for _, p := range d.procs {
				if strings.HasPrefix(strings.ToLower(p.name), "pdns") {
					pdnsCPU += p.avgPct
				}
			}
			if pd/d.tsql >= 0.5 || pdnsCPU >= 10 {
				d.v.reasons = append(d.v.reasons, fmt.Sprintf("DNS: PowerDNS queries are %.0f%% of this host's SQL time and pdns_server uses %.0f%% of a core — the app's own DB is not the issue here; move pdns + its MySQL off the app host (TECHNIQUES T27), set a TTL / cache (T17); if that is not enough, replace PowerDNS with a minimal DNS in the app that ignores unknown names (T41)", 100*pd/d.tsql, pdnsCPU))
			}
		}
		d.epH = epHandler
		d.acts = suggestActions(d.v, d.w, d.m, d.procs, d.eps, d.total, d.sqls, d.tsql, d.sa, pprofOK, epHandler, existingIndexes(h.Dir), tableColumns(h.Dir, codeDir))
		// bench shape and what etc/ already holds change the priorities
		if bs := estimateBenchShape(h, d.w); bs.fixed {
			for i := range d.acts {
				if d.acts[i].kind == "polling" {
					d.acts[i].priority, d.acts[i].share = "P3", 0 // scored by the rule (P3), not by the endpoint's time
					d.acts[i].detail += " Bench shape is fixed concurrency (clients re-poll as soon as the response arrives, no server-set interval) — low priority."
				}
			}
		}
		attachExplain(d.acts, loadExplain(h.Dir)) // EXPLAIN collected by the agent at the end of the run
		for i := range d.acts {                   // a missing CPU profile: say why instead of "enable pprof" when the agent did try
			if d.acts[i].kind != "pprof" {
				continue
			}
			for _, f := range pprofFailures([]*hostRun{h}) {
				if f.profile == "cpu" || f.profile == "all" {
					d.acts[i].title = "CPU profile was not captured (" + trunc(f.reason, 80) + ")"
					d.acts[i].detail = "The agent tried and failed: " + f.String() + ". Fix that so the next bench has a CPU profile."
				}
			}
		}
		if ci != nil { // a server-set polling interval (retry_after_ms …): name it, and P1 when it is tiny
			applyPollFields(d.acts, ci, repoRootOf(codeDir))
			sort.SliceStable(d.acts, func(i, j int) bool { return d.acts[i].priority < d.acts[j].priority })
		}
		if codeDir != "" {
			if _, err := os.Stat(filepath.Join(codeDir, "..", "..", "etc", "mysql", "mysql.conf.d", "zz-perf.cnf")); err == nil {
				for i := range d.acts {
					if strings.HasPrefix(d.acts[i].title, "Cut disk writes") {
						d.acts[i].priority = "P3"
						d.acts[i].title += " (zz-perf.cnf is already in etc/ — only verify it is deployed on the DB host)"
					}
				}
			}
		}
		// handlers that shell out (exec.Command): a process per request is slow and does not scale to 2 app hosts
		if ci != nil {
			for _, e := range d.eps {
				fn := strings.TrimSuffix(strings.SplitN(epHandler[e.key], "(", 2)[0], "()")
				if fn != "" && ci.execs[fn] && e.count >= 5 {
					d.acts = append(d.acts, mk("P2", fmt.Sprintf("%s runs an external command per request", e.key), fmt.Sprintf("%d calls, avg %.0fms. The handler (or a helper) calls exec.Command: replace the command with a direct call/INSERT to what it does, or batch it, and remember a 2nd app host would run it too.", e.count, 1000*e.sum/float64(e.count)), "", fn+"()", ""))
				}
			}
		}
		if p, ok := h.Pprof["cpu"]; ok { // CPU hot spots from the profile (score.go): sha256 in fillUserResponse …
			d.acts = append(d.acts, hotspotActions(p)...)
		}
		diags = append(diags, d)
	}
	// DB on another host: the app host has the endpoints, the DB host has the slow log. sqltag joins them,
	// so give every host without its own tagged SQL the union of the other hosts' per-tag stats.
	{
		merged := map[string][]*sqlStat{}
		var from []string
		for _, d := range diags {
			if len(d.byTag) > 0 {
				from = append(from, d.h.Name)
				for k, v := range d.byTag {
					merged[k] = append(merged[k], v...)
				}
			}
		}
		for k := range merged {
			sort.Slice(merged[k], func(i, j int) bool { return merged[k][i].sum > merged[k][j].sum })
		}
		for i := range diags {
			if len(diags[i].byTag) == 0 && len(merged) > 0 {
				diags[i].byTag, diags[i].sqlFrom = merged, strings.Join(from, ", ")
			}
		}
	}
	// periodic internal endpoints (/api/internal/*): interval × work per call, as their own section + one action each
	var internals []internalStat
	for i := range diags {
		sts := internalStats(diags[i].h.Name, diags[i].eps, diags[i].byTag, diags[i].epH)
		internals = append(internals, sts...)
		for _, st := range sts {
			diags[i].acts = append(diags[i].acts, st.action())
		}
	}
	// work units + dedupe: the same action (group, title, query) from several hosts is listed once, on the first host
	for i := range diags {
		for j := range diags[i].acts {
			diags[i].acts[j].group, diags[i].acts[j].file = actionGroup(diags[i].acts[j], ci)
		}
	}
	{
		lists, names := make([][]action, len(diags)), make([]string, len(diags))
		for i := range diags {
			lists[i], names[i] = diags[i].acts, diags[i].h.Name
		}
		lists = dedupeActions(lists, names)
		for i := range diags {
			diags[i].acts = lists[i]
		}
	}
	// score every action by expected effect (score.go) and rank: 5xx first, then score
	{
		pathEps := map[string]bool{}
		if gcfg.scorePost != "" {
			pathEps[paramKey(gcfg.scorePost)] = true
			var best *hostDiag
			for i := range diags {
				if diags[i].sa.sessions > 0 && (best == nil || diags[i].total > best.total) {
					best = &diags[i]
				}
			}
			if best != nil {
				for _, l := range computeFlowsCached(best.h, nz, gcfg.scorePost).loops {
					for _, st := range l.steps {
						pathEps[paramKey(st)] = true
					}
				}
			}
		}
		pathSQLs, pathFns := map[string]bool{}, map[string]bool{}
		for _, d := range diags {
			for ep := range pathEps {
				for _, q := range d.byTag[ep] {
					pathSQLs[q.norm] = true
				}
			}
			for k, fn := range d.epH {
				if pathEps[paramKey(k)] {
					pathFns[strings.SplitN(fn, "(", 2)[0]] = true
				}
			}
		}
		busy := func(d hostDiag, isDB bool) float64 {
			if d.v.cores <= 0 {
				return 0
			}
			sum := 0.0
			for _, p := range d.procs {
				n := strings.ToLower(p.name)
				db := strings.Contains(n, "mysqld") || strings.Contains(n, "mariadbd") || strings.Contains(n, "postgres")
				other := n == "nginx" || n == "isumeasure" || n == "bench" || strings.HasPrefix(n, "kworker") || n == "sshd" || n == "systemd" || strings.HasPrefix(n, "pdns")
				if (isDB && db) || (!isDB && !db && !other) {
					sum += p.avgPct
				}
			}
			return sum / (d.v.cores * 100)
		}
		// the row-lock action and the 5xx it causes (deadlocks) are one problem: keep the 5xx item, note the locks there
		var errAct *action
		for i := range diags {
			for j := range diags[i].acts {
				if diags[i].acts[j].kind == "errors" && errAct == nil {
					errAct = &diags[i].acts[j]
				}
			}
		}
		for i := range diags {
			if errAct == nil {
				break
			}
			kept := diags[i].acts[:0]
			for _, a := range diags[i].acts {
				if a.kind == "lock" {
					errAct.detail += fmt.Sprintf(" Row-lock contention on %s (%.1f waits/s, %.0f ms/s) is the likely cause when the 5xx are deadlocks / lock wait timeouts: shorten the transactions, move FOR UPDATE off hot rows.", diags[i].h.Name, diags[i].m.lockWaits, diags[i].m.lockMs)
					continue
				}
				kept = append(kept, a)
			}
			diags[i].acts = kept
		}
		for i := range diags {
			scoreActions(diags[i].acts, scoreCtx{kind: diags[i].v.kind, dbBusy: busy(diags[i], true), appBusy: busy(diags[i], false), pathEps: pathEps, pathSQLs: pathSQLs, pathFns: pathFns})
		}
	}
	md.WriteString("\n## Verdict\n\n")
	hb.WriteString(`<section><h2>Verdict — where is the bottleneck?</h2><div class="vexplain"><p>Bottleneck = the narrowest point that stops the score from growing: a box that uses all its CPU, row-lock waits, disk writes — or, when none of these, waiting (slow API responses, the bench's concurrency).</p><p>Making something that is not the bottleneck faster does not raise the score. Look at the bottleneck box / resource first and fix the work that uses it.</p><p>The chart is the CPU each process used (100% = one core). A process that sticks to the red dashed line (cores × 100%) is the bottleneck.</p></div>`)
	for _, d := range diags {
		fmt.Fprintf(&md, "### %s: %s\n\n%s%s%s\n\n", d.h.Name, d.v.title, ternary(d.v.fix != "", "**"+d.v.fix+"**\n\n", ""), ternary(d.v.near != "", "_"+d.v.near+"_\n\n", ""), strings.Join(func() []string {
			var o []string
			for _, r := range d.v.reasons {
				o = append(o, "- "+r)
			}
			return o
		}(), "\n"))
		fmt.Fprintf(&hb, `<div class="verdict %s"><div class="vt"><span class="badge">%s</span> <span class="vlbl">bottleneck</span> %s</div>%s%s<ul>%s</ul></div>`, e(d.v.kind), e(d.h.Name), e(d.v.title),
			ternary(d.v.fix != "", `<div class="vfix"><span class="vlbl">fix first</span> `+e(d.v.fix)+`</div>`, ""),
			ternary(d.v.near != "", `<div class="vnear">`+e(d.v.near)+`</div>`, ""), func() string {
				var o string
				for _, r := range d.v.reasons {
					o += "<li>" + e(r) + "</li>"
				}
				return o
			}())
		if len(d.procs) > 0 {
			// CPU by process chart over the whole run
			names := []string{}
			for i, p := range d.procs {
				if i >= 5 {
					break
				}
				names = append(names, p.name)
			}
			var ss []series
			for _, name := range names {
				pts := make([]float64, nb)
				cnt := make([]int, nb)
				for _, m := range d.h.Procs {
					t, err := time.Parse(time.RFC3339Nano, str(m["t"]))
					if err != nil {
						continue
					}
					i := int(t.Sub(t0).Seconds() / bucket)
					if i < 0 || i >= nb {
						continue
					}
					list, _ := m["procs"].([]any)
					for _, x := range list {
						pm, _ := x.(map[string]any)
						if str(pm["name"]) == name {
							pts[i] += num(pm["cpu_pct"])
						}
					}
					cnt[i]++
				}
				for i := range pts {
					if cnt[i] > 0 {
						pts[i] /= float64(cnt[i])
					}
				}
				ss = append(ss, series{name: name, pts: pts})
			}
			b0, b1 := -1, -1
			if !d.w.fromT.IsZero() {
				b0, b1 = int(d.w.fromT.Sub(t0).Seconds()/bucket), int(d.w.toT.Sub(t0).Seconds()/bucket)
			}
			avgs := map[string]float64{}
			for _, p := range d.procs {
				avgs[p.name] = p.avgPct
			}
			hb.WriteString(`<div class="charts">` + cpuChart(ss, avgs, d.v.cores*100, b0, b1, bucket, 560, 200) + `</div>`)
		}
	}
	// ---- bench shape (推測): in-flight requests over the load window on the busiest host; fixed vs rising ----
	{
		var busiest *hostDiag
		for i := range diags {
			if busiest == nil || len(diags[i].h.Access) > len(busiest.h.Access) {
				busiest = &diags[i]
			}
		}
		if busiest != nil {
			if bs := estimateBenchShape(busiest.h, busiest.w); bs.secs >= 6 {
				var line string
				if bs.fixed {
					line = fmt.Sprintf("Bench shape (推測): **fixed concurrency** — ≈ %.0f requests in flight on %s through the load window (p10–p90 %.0f–%.0f, start %.0f → end %.0f, %d s). The bench waits for each response before sending the next, so shorter responses turn directly into more requests and score.", bs.med, busiest.h.Name, bs.p10, bs.p90, bs.first, bs.last, bs.secs)
				} else {
					line = fmt.Sprintf("Bench shape (推測): **rising load** — in-flight requests on %s went %.0f → %.0f (median %.0f, p10–p90 %.0f–%.0f, %d s). Throughput follows how many clients the bench adds; look for the moment p99 or errors stop the growth.", busiest.h.Name, bs.first, bs.last, bs.med, bs.p10, bs.p90, bs.secs)
				}
				md.WriteString(line + "\n\n")
				fmt.Fprintf(&hb, `<div class="muted" style="margin-top:6px">%s</div>`, strings.ReplaceAll(strings.ReplaceAll(e(line), "**", ""), "Bench shape", "<b>Bench shape</b>"))
				ro.meta["bench_shape"] = map[string]any{"host": busiest.h.Name, "fixed": bs.fixed, "inflight_med": bs.med, "p10": bs.p10, "p90": bs.p90, "start": bs.first, "end": bs.last, "secs": bs.secs}
			}
		}
		if gcfg.scorePost != "" {
			n := 0
			for _, d := range diags {
				for _, ep := range d.eps {
					if paramKey(ep.key) == paramKey(gcfg.scorePost) {
						n += ep.count
					}
				}
			}
			fmt.Fprintf(&md, "score_post: `%s` × %d (alp.yml; the panel's Score timeline shows score ÷ this)\n\n", gcfg.scorePost, n)
			fmt.Fprintf(&hb, `<div class="muted">score_post: <code>%s</code> × %d (alp.yml; the Score timeline shows score ÷ this)</div>`, e(gcfg.scorePost), n)
			ro.meta["score_post"] = map[string]any{"endpoint": gcfg.scorePost, "count": n}
		}
	}
	hb.WriteString(`</section>`)

	// ---- next step: which procedure to run next (rules over this report + run history + repo) ----
	{
		in := nextInput{ts: filepath.Base(dir), t0: t0, noCode: ci == nil, ciEmpty: ci != nil && ci.nSQL == 0, noGroups: gcfg.src == ""}
		order := make([]int, len(diags))
		for i := range order {
			order[i] = i
		}
		sort.SliceStable(order, func(a, b int) bool { return diags[order[a]].total > diags[order[b]].total })
		for _, i := range order {
			d := diags[i]
			nh := nextHost{name: d.h.Name, cpuUsed: 100 - d.m.cpuIdle, verdict: d.v.kind, req: d.total, errRate: d.m.errRate}
			if d.tsql > 0 {
				pd := 0.0
				for _, q := range d.sqls {
					if pdnsSQLRe.MatchString(q.norm) {
						pd += q.sum
					}
				}
				nh.pdnsShare = pd / d.tsql
			}
			for _, p := range d.procs {
				if strings.HasPrefix(strings.ToLower(p.name), "pdns") {
					nh.pdnsCPU += p.avgPct
				}
			}
			in.hosts = append(in.hosts, nh)
			for _, a := range d.acts {
				switch a.priority {
				case "P1":
					in.p1++
				case "P2":
					in.p2++
				}
			}
			if len(d.eps) > in.splitEps {
				in.splitEps = len(d.eps)
			}
		}
		if len(order) > 0 {
			for j, ep := range diags[order[0]].eps {
				if j >= 10 {
					break
				}
				if !strings.Contains(ep.key, "static assets") {
					in.topEps = append(in.topEps, paramKey(ep.key))
				}
			}
		}
		if bs, ok := ro.meta["bench_shape"].(map[string]any); ok {
			in.benchKnown = true
			in.fixedBench, _ = bs["fixed"].(bool)
		}
		in.score, in.scoreOK = scoreOf(score)
		hist := runHistory(dir)
		if !in.scoreOK {
			in.score, in.scoreOK = hist[in.ts]
		}
		if !in.scoreOK { // the bench's own result line, when the output was attached (task bench:log / the panel)
			if bs := readBenchLog(filepath.Join(dir, "bench.log"), nil); bs != nil && bs.score >= 0 {
				in.score, in.scoreOK = float64(bs.score), true
			}
		}
		var tss []string
		for ts := range hist {
			if ts < in.ts {
				tss = append(tss, ts)
			}
		}
		sort.Strings(tss)
		for _, ts := range tss {
			in.recentScores = append(in.recentScores, hist[ts])
		}
		if in.scoreOK {
			in.recentScores = append(in.recentScores, in.score)
		}
		if prevTS, _ := previousRunMeta(dir); prevTS != "" {
			in.prevTS = prevTS
			in.prevScore, in.prevScoreOK = hist[prevTS]
			in.prevCommit = runCommit(filepath.Join(filepath.Dir(dir), prevTS))
		}
		in.commit = runCommit(dir)
		in.prizeTarget = gcfg.prizeTarget
		in.repoRoot = repoRootOf(codeDir)
		cs := computeNextStep(in)
		if in.prizeTarget > 0 { // the special prize: how far this run is from it
			pl := fmt.Sprintf("Special prize target %.0f (alp.yml prize_target): ", in.prizeTarget)
			if in.scoreOK {
				pl += fmt.Sprintf("this run %.0f = %.0f%%", in.score, 100*in.score/in.prizeTarget)
				if in.score >= in.prizeTarget {
					pl += " — reached (check the leaderboard: only the first team gets it)"
				}
			} else {
				pl += "score of this run unknown"
			}
			md.WriteString(pl + "\n\n")
			hb.WriteString(`<div class="muted" style="margin:4px 0">` + e(pl) + `</div>`)
			ro.meta["prize"] = map[string]any{"target": in.prizeTarget, "score": in.score, "score_known": in.scoreOK}
		}
		renderNextStep(&md, &hb, cs)
		if !in.scoreOK {
			const noScore = "Score not recorded for this run: the rules that compare runs (revert after a drop, technique-scan when flat) cannot fire. Press 🏁 終わった on the bench todo of the lane board (or ■ ベンチ終わった on the panel) right after the bench (task bench:done SCORE=<n> [TS=<ts>] [OUTPUT_FILE=<bench output>]; the output's result line fills the score), or type it in the panel's Runs row."
			md.WriteString("> " + noScore + "\n\n")
			hb.WriteString(`<div class="muted" style="margin:-6px 0 10px">⚠ ` + e(noScore) + `</div>`)
		}
		var ns []map[string]any
		for i, c := range cs {
			if i >= 3 {
				break
			}
			ns = append(ns, map[string]any{"name": c.name, "skill": c.skill, "cmd": c.cmd, "score": c.score, "why": c.reasons})
		}
		ro.meta["next_step"] = ns
	}

	// ---- errors: 5xx by endpoint + the app log's error lines, condensed ----
	{
		type e5 struct {
			host, key string
			c5, count int
		}
		var errs []e5
		for _, d := range diags {
			for _, ep := range d.eps {
				if ep.c5 > 0 {
					errs = append(errs, e5{d.h.Name, ep.key, ep.c5, ep.count})
				}
			}
		}
		sort.Slice(errs, func(i, j int) bool { return errs[i].c5 > errs[j].c5 })
		type hostKinds struct {
			host  string
			kinds []appErrKind
			total int
		}
		var logs []hostKinds
		for _, d := range diags {
			if ks, n := appErrorKinds(filepath.Join(d.h.Dir, "app.log")); n > 0 {
				logs = append(logs, hostKinds{d.h.Name, ks, n})
			}
		}
		var dls []deadlock
		for _, d := range diags {
			dls = append(dls, hostDeadlocks(d.h.Name, d.h.Dir)...)
		}
		dlFor := func(key string) string {
			for _, dl := range dls {
				if dl.matches(key) {
					return "deadlock: " + dl.summary()
				}
			}
			return ""
		}
		// report.json: 5xx per endpoint with the app's messages for that route (app-analysis "Now" lists them first)
		var em []map[string]any
		for _, x := range errs {
			var msgs []map[string]any
			for _, l := range logs {
				for _, k := range l.kinds {
					path := x.key
					if i := strings.IndexByte(path, ' '); i >= 0 {
						path = path[i+1:] // the log has the route without the method
					}
					if k.endpoint != "" && paramKey(k.endpoint) == paramKey(path) && len(msgs) < 3 {
						msgs = append(msgs, map[string]any{"message": k.message, "n": k.count})
					}
				}
			}
			row := map[string]any{"host": x.host, "endpoint": x.key, "c5": x.c5, "count": x.count, "messages": msgs}
			if dl := dlFor(x.key); dl != "" {
				row["deadlock"] = dl
			}
			em = append(em, row)
		}
		if em != nil {
			ro.meta["errors"] = em
		}
		if len(errs) > 0 || len(logs) > 0 || len(dls) > 0 {
			md.WriteString("## Errors (5xx by endpoint; app log error lines, de-duplicated)\n\n")
			hb.WriteString(`<section><h2>Errors <span class="muted">(5xx by endpoint · app log error lines, de-duplicated)</span></h2>`)
			if len(errs) > 0 {
				md.WriteString("| host | endpoint | 5xx | of requests | cause (deadlock: the two statements, sqltag) |\n|---|---|---|---|---|\n")
				hb.WriteString(`<div class="tw"><table><tr><th>host</th><th>endpoint</th><th>5xx</th><th>of requests</th><th>cause</th></tr>`)
				for i, x := range errs {
					if i >= 10 {
						break
					}
					cause := dlFor(x.key)
					fmt.Fprintf(&md, "| %s | `%s` | %d | %d | %s |\n", x.host, x.key, x.c5, x.count, strings.ReplaceAll(cause, "|", "\\|"))
					fmt.Fprintf(&hb, `<tr><td>%s</td><td><code>%s</code></td><td class="num bad">%d</td><td class="num">%d</td><td class="muted">%s</td></tr>`, e(x.host), e(x.key), x.c5, x.count, e(strings.ReplaceAll(cause, "`", "")))
				}
				md.WriteString("\n")
				hb.WriteString(`</table></div>`)
			}
			for _, l := range logs {
				fmt.Fprintf(&md, "%s app.log: %d error lines → %d kinds (numbers masked as N)\n\n| count | endpoint | message |\n|---|---|---|\n", l.host, l.total, len(l.kinds))
				fmt.Fprintf(&hb, `<div class="muted">%s app.log: %d error lines → %d kinds (numbers masked as N)</div><div class="tw"><table><tr><th>count</th><th>endpoint</th><th>message</th></tr>`, e(l.host), l.total, len(l.kinds))
				for i, k := range l.kinds {
					if i >= 8 {
						break
					}
					fmt.Fprintf(&md, "| %d | `%s` | %s |\n", k.count, k.endpoint, strings.ReplaceAll(trunc(k.message, 200), "|", "\\|"))
					fmt.Fprintf(&hb, `<tr><td class="num">%d</td><td><code>%s</code></td><td>%s</td></tr>`, k.count, e(k.endpoint), e(k.message))
				}
				md.WriteString("\n")
				hb.WriteString(`</table></div>`)
			}
			renderDeadlocks(&md, &hb, dls)
			hb.WriteString(`</section>`)
		}
	}

	renderInternal(&md, &hb, internals)
	{ // per 10 s on the busiest host: whole-window averages hide a throughput that collapses mid-run
		var busiest *hostDiag
		for i := range diags {
			if busiest == nil || len(diags[i].h.Access) > len(busiest.h.Access) {
				busiest = &diags[i]
			}
		}
		if busiest != nil {
			renderPer10s(&md, &hb, busiest.h.Name, busiest.eps, busiest.w, t0, bucket, gcfg.scorePost)
		}
	}
	if len(internals) > 0 {
		var im []map[string]any
		for _, st := range internals {
			im = append(im, map[string]any{"host": st.host, "endpoint": st.key, "calls": st.calls, "median_gap_ms": 1000 * st.medGap, "p90_gap_ms": 1000 * st.p90Gap, "avg_ms": st.avgMs, "p99_ms": st.p99Ms, "writes_per_call": st.writesPerCallSum})
		}
		ro.meta["internal"] = im
	}

	// ---- vs previous run: the busiest host's top endpoints against the previous run's report.json ----
	{
		var busiest *hostDiag
		for i := range diags {
			if busiest == nil || len(diags[i].h.Access) > len(busiest.h.Access) {
				busiest = &diags[i]
			}
		}
		if busiest != nil {
			var eps []map[string]any
			for i, ep := range busiest.eps {
				if i >= 30 {
					break
				}
				eps = append(eps, map[string]any{"key": ep.key, "count": ep.count, "avg_ms": 1000 * ep.sum / float64(ep.count), "p99_ms": 1000 * pct(ep.rts, .99), "sum_s": ep.sum})
			}
			ro.meta["endpoints"] = map[string]any{"host": busiest.h.Name, "req": busiest.total, "secs": busiest.w.secs, "top": eps}
			if prevTS, prev := previousRunMeta(dir); prev != nil {
				pm, _ := prev["endpoints"].(map[string]any)
				prevEps := map[string]map[string]any{}
				if pm != nil {
					if top, ok := pm["top"].([]any); ok {
						for _, x := range top {
							if m, ok := x.(map[string]any); ok {
								prevEps[str(m["key"])] = m
							}
						}
					}
				}
				if len(prevEps) > 0 {
					prevReq, prevSecs := num(pm["req"]), num(pm["secs"])
					rps, prevRps := 0.0, 0.0
					if busiest.w.secs > 0 {
						rps = float64(busiest.total) / busiest.w.secs
					}
					if prevSecs > 0 {
						prevRps = prevReq / prevSecs
					}
					fmt.Fprintf(&md, "## vs previous run %s (%s · %.0f → %.0f req/s)\n\n| endpoint | count | Δ count | avg ms | Δ avg | p99 ms | Δ p99 |\n|---|---|---|---|---|---|---|\n", prevTS, busiest.h.Name, prevRps, rps)
					fmt.Fprintf(&hb, `<section><h2>vs previous run %s <span class="muted">(%s · %.0f → %.0f req/s)</span></h2><div class="tw"><table><tr><th>endpoint</th><th>count</th><th>Δ count</th><th>avg ms</th><th>Δ avg</th><th>p99 ms</th><th>Δ p99</th></tr>`, e(prevTS), e(busiest.h.Name), prevRps, rps)
					for i, ep := range busiest.eps {
						if i >= 10 {
							break
						}
						avg, p99 := 1000*ep.sum/float64(ep.count), 1000*pct(ep.rts, .99)
						dc, da, dp := "new", "", ""
						dcCls, daCls, dpCls := "hi", "", ""
						if p := prevEps[ep.key]; p != nil {
							pc := num(p["count"])
							if pc > 0 {
								dc = fmt.Sprintf("%+.0f%%", 100*(float64(ep.count)-pc)/pc)
							}
							da = fmt.Sprintf("%+.0f", avg-num(p["avg_ms"]))
							dp = fmt.Sprintf("%+.0f", p99-num(p["p99_ms"]))
							dcCls = ternary(float64(ep.count) < pc*0.9, "bad", ternary(float64(ep.count) > pc*1.1, "hi", ""))
							daCls = ternary(avg > num(p["avg_ms"])*1.2, "bad", ternary(avg < num(p["avg_ms"])*0.8, "hi", ""))
							dpCls = ternary(p99 > num(p["p99_ms"])*1.2, "bad", ternary(p99 < num(p["p99_ms"])*0.8, "hi", ""))
						}
						fmt.Fprintf(&md, "| `%s` | %d | %s | %.0f | %s | %.0f | %s |\n", ep.key, ep.count, dc, avg, da, p99, dp)
						fmt.Fprintf(&hb, `<tr><td><code>%s</code></td><td class="num">%d</td><td class="num %s">%s</td><td class="num">%.0f</td><td class="num %s">%s</td><td class="num">%.0f</td><td class="num %s">%s</td></tr>`, e(ep.key), ep.count, dcCls, dc, avg, daCls, da, p99, dpCls, dp)
					}
					md.WriteString("\nMore requests at the same share = progress (the share stays, the count grows). Fewer requests with a higher p99 = regression.\n\n")
					hb.WriteString(`</table></div><div class="muted">more requests at the same share = progress; fewer requests with a higher p99 = regression</div></section>`)
				}
			}
		}
	}

	// ---- tuning: knobs with evidence (fixed rules, no search) — the `tuning` skill applies them one per bench at 15:00 ----
	{
		type hostKnobs struct {
			host string
			ks   []knob
		}
		var all []hostKnobs
		acts := 0
		for _, d := range diags {
			top := ""
			if p, ok := d.h.Pprof["cpu"]; ok {
				top = pprofTop(p, 60)
			}
			ks := tuningAdvice(d.h, d.w, d.procs, top)
			var shown []knob
			for _, k := range ks {
				if k.level >= 1 {
					shown = append(shown, k)
				}
				if k.level >= 2 {
					acts++
				}
			}
			if len(shown) > 0 {
				all = append(all, hostKnobs{d.h.Name, shown})
			}
		}
		md.WriteString("## Tuning (knobs with evidence; when=now: a fast-lane etc/ PR right after this run · when=later: one per bench at 15:00 with the tuning skill · never after the freeze)\n\n")
		hb.WriteString(`<section><h2>Tuning <span class="muted">(knobs with evidence · fixed rules, no search · now = fast lane right away · later = one per bench at 15:00)</span></h2>`)
		if len(all) == 0 {
			md.WriteString("No knob shows a signal in this run (buffer pool hit, connections, temp tables, GC share, pool waits, worker_connections, TIME_WAIT, listen overflows all within range). Keep the free-wins defaults.\n\n")
			hb.WriteString(`<div class="muted">no knob shows a signal in this run — keep the free-wins defaults</div></section>`)
		} else {
			md.WriteString("| host | area | knob | current | signal | suggestion | act | when |\n|---|---|---|---|---|---|---|---|\n")
			hb.WriteString(`<div class="tw"><table><tr><th>host</th><th>area</th><th>knob</th><th>current</th><th>signal</th><th>suggestion</th><th>act</th><th>when</th></tr>`)
			for _, hk := range all {
				for _, k := range hk.ks {
					lvl := ternary(k.level >= 2, "●", "○")
					when := ternary(k.when == "now", "now (fast lane)", "later (15:00 tuning)")
					fmt.Fprintf(&md, "| %s | %s | `%s` | %s | %s | %s | %s | %s |\n", hk.host, k.area, k.name, strings.ReplaceAll(k.current, "|", "/"), strings.ReplaceAll(k.signal, "|", "/"), strings.ReplaceAll(k.suggest, "|", "/"), lvl, when)
					fmt.Fprintf(&hb, `<tr><td>%s</td><td>%s</td><td><code>%s</code></td><td>%s</td><td class="%s">%s</td><td>%s</td><td class="num %s">%s</td><td><span class="%s">%s</span></td></tr>`, e(hk.host), e(k.area), e(k.name), e(k.current), ternary(k.level >= 2, "bad", "hi"), e(k.signal), e(k.suggest), ternary(k.level >= 2, "bad", "hi"), lvl, ternary(k.when == "now", "bad", "muted"), e(when))
				}
			}
			md.WriteString("\n● = act (one change, one bench, keep only if the score holds and the signal drops) · ○ = worth a look · when=now ● = the hub hands it to lane 2 as a [fast] etc/ PR for the next bench · when=later = the 15:00 tuning window\n\n")
			hb.WriteString(`</table></div><div class="muted">● act: one change → one bench → keep only if the score holds and the signal drops · ○ worth a look</div><div class="muted">now ●: a limit — the hub hands it to lane 2 as a [fast] etc/ PR for the next bench · later: interacts or smaller than the noise — one per bench at 15:00</div></section>`)
		}
		ro.meta["tuning"] = map[string]any{"act": acts}
	}

	// ---- bench output: <run>/bench.log pasted from the portal or uploaded by `task bench:log`, condensed ----
	if bs := readBenchLog(filepath.Join(dir, "bench.log"), parseBenchPatterns(gcfg.benchPatterns)); bs != nil && bs.total > 0 {
		md.WriteString(bs.markdown())
		hb.WriteString(`<section><h2>Bench output <span class="muted">(bench.log, de-duplicated)</span></h2>`)
		for _, r := range bs.result {
			fmt.Fprintf(&hb, `<div><b>%s</b></div>`, e(r))
		}
		fmt.Fprintf(&hb, `<div class="muted">%d lines → %d kinds · %d lines matched a known pattern (alp.yml bench_patterns + built-ins)</div><div class="tw"><table><tr><th>count</th><th>message</th><th>meaning</th></tr>`, bs.total, bs.kinds, bs.flagged)
		for i, l := range bs.lines {
			if i >= 20 {
				fmt.Fprintf(&hb, `<tr><td class="num">…</td><td class="muted">%d more kinds</td><td></td></tr>`, len(bs.lines)-20)
				break
			}
			fmt.Fprintf(&hb, `<tr><td class="num">%d</td><td>%s</td><td class="%s">%s</td></tr>`, l.count, e(l.text), ternary(strings.Contains(l.meaning, "クリティカル") || strings.Contains(l.meaning, "整合性"), "bad", ternary(l.meaning != "", "hi", "muted")), e(l.meaning))
		}
		hb.WriteString(`</table></div></section>`)
		top := []string{}
		for i, l := range bs.lines {
			if i >= 3 {
				break
			}
			top = append(top, fmt.Sprintf("%d× %s", l.count, trunc(l.text, 60)))
		}
		ro.meta["bench_log"] = map[string]any{"lines": bs.total, "kinds": bs.kinds, "flagged": bs.flagged, "result": bs.result, "top": top, "score": bs.score, "pass": bs.pass, "dns_ok": bs.dnsOK, "dns_fail": bs.dnsFail}
	}

	// ---- unmatched paths: stage-3-only families (alp.yml candidates) + a warning when one endpoint is split many ways ----
	fams := nz.unmatched(5)
	maxEps := 0
	for _, d := range diags {
		if len(d.eps) > maxEps {
			maxEps = len(d.eps)
		}
	}
	if len(fams) > 0 || maxEps > 200 {
		md.WriteString("## Unmatched paths (alp.yml candidates)\n\n")
		hb.WriteString(`<section><h2>Unmatched paths <span class="muted">(alp.yml candidates)</span></h2>`)
		if maxEps > 200 || (len(fams) > 0 && fams[0].distinct >= 20) {
			ex, exN := "-", 0
			if len(fams) > 0 {
				ex, exN = fams[0].pattern, fams[0].distinct
			}
			w := fmt.Sprintf("⚠ one endpoint is split into many rows (%d endpoint kinds; e.g. %s seen as %d different paths). The endpoint tables and \"Where the time goes\" under-count it. Add the rule below to alp.yml and re-report.", maxEps, ex, exN)
			fmt.Fprintf(&md, "**%s**\n\n", w)
			fmt.Fprintf(&hb, `<div class="verdict wait"><div class="vt">%s</div></div>`, e(w))
		}
		md.WriteString("Paths that no alp.yml rule and no route matched (only id-looking segments were collapsed), grouped by the segment that varies. `*` = the varying segment.\n\n| family | distinct paths | requests | examples | add to alp.yml |\n|---|---|---|---|---|\n")
		hb.WriteString(`<div class="muted">paths that no alp.yml rule and no route matched (only id-looking segments were collapsed), grouped by the segment that varies</div><div class="tw"><table><tr><th>family</th><th>distinct paths</th><th>requests</th><th>examples</th><th>add to alp.yml (matching_groups)</th></tr>`)
		for i, f := range fams {
			if i >= 10 {
				break
			}
			fmt.Fprintf(&md, "| `%s` | %d | %d | %s | `%s` |\n", f.pattern, f.distinct, f.requests, strings.Join(f.examples, ", "), f.regex)
			fmt.Fprintf(&hb, `<tr><td><code>%s</code></td><td class="num %s">%d</td><td class="num">%d</td><td class="muted">%s</td><td><code>- "%s"</code></td></tr>`, e(f.pattern), lvl(float64(f.distinct), 10, 20), f.distinct, f.requests, e(strings.Join(f.examples, ", ")), e(f.regex))
		}
		md.WriteString("\n")
		hb.WriteString(`</table></div></section>`)
	}

	// ---- data growth: rows right after /initialize vs after the bench (per host; the DB host is the one that matters) ----
	{
		type rowHost struct {
			rowDelta
			host string
			dur  float64
		}
		byTable := map[string]rowHost{} // per table: the host whose count moved most (DNS DB on svc1, app DB on svc3, …)
		for _, h := range hosts {
			rs, re := parseRows(filepath.Join(h.Dir, "rows-start.txt")), parseRows(filepath.Join(h.Dir, "rows-end.txt"))
			if len(rs) == 0 || len(re) == 0 {
				continue
			}
			for t, e := range re {
				st := rs[t]
				cur, ok := byTable[t]
				d := e - st
				if d < 0 {
					d = -d
				}
				curD := cur.end - cur.start
				if curD < 0 {
					curD = -curD
				}
				if !ok || d > curD || (d == curD && e > cur.end) {
					byTable[t] = rowHost{rowDelta{t, st, e}, h.Name, num(h.Run["duration_sec"])}
				}
			}
		}
		if len(byTable) > 0 {
			var best []rowHost
			for _, r := range byTable {
				best = append(best, r)
			}
			sort.Slice(best, func(i, j int) bool { return best[i].end-best[i].start > best[j].end-best[j].start })
			md.WriteString("## Data growth (rows right after /initialize → after the bench; per table, the host whose DB changed)\n\n| table | host | after init | after bench | Δ | Δ/s |\n|---|---|---|---|---|---|\n")
			hb.WriteString(`<section><h2>Data growth <span class="muted">· rows right after /initialize → after the bench</span></h2><div class="muted">which tables the bench writes to (per table, the host whose DB changed — a DNS DB may live on the app host), and the baseline for the data-retention check (rows must never go down after a reboot)</div><div class="tw"><table class="sortable"><tr><th>table</th><th>host</th><th>after init</th><th>after bench</th><th>Δ</th><th>Δ/s</th></tr>`)
			var growth []map[string]any
			for i, d := range best {
				per := 0.0
				if d.dur > 0 {
					per = float64(d.end-d.start) / d.dur
				}
				growth = append(growth, map[string]any{"table": d.table, "host": d.host, "start": d.start, "end": d.end, "per_s": per})
				if i >= 25 {
					continue
				}
				md.WriteString(fmt.Sprintf("| %s | %s | %d | %d | %+d | %.1f |\n", d.table, d.host, d.start, d.end, d.end-d.start, per))
				fmt.Fprintf(&hb, `<tr><td><code>%s</code></td><td>%s</td><td class="num">%d</td><td class="num">%d</td><td class="num %s">%+d</td><td class="num">%.1f</td></tr>`, e(d.table), e(d.host), d.start, d.end, ternary(d.end-d.start < 0, "bad", ""), d.end-d.start, per)
			}
			md.WriteString("\n")
			hb.WriteString(`</table></div></section>`)
			ro.meta["growth"] = growth // app-analysis refresh: rows and Δ/s per table
		}
	}
	md.WriteString("## Where the time goes\n\n")
	hb.WriteString(`<section><h2>Where the time goes</h2><div class="muted">the top 5 endpoints of each host and the heaviest SQL each one issues · click a row for every statement, the code that issued it, its 5xx and the steps around it (the full tables are under "Per host")</div>`)
	// API requests of all app hosts: the denominator of "per request" on a DB-only host (it serves no API itself)
	totalAPI := 0
	for _, d := range diags {
		for _, x := range d.eps {
			if !strings.Contains(x.key, "static assets") {
				totalAPI += x.count
			}
		}
	}
	for _, d := range diags {
		var tot float64
		for _, x := range d.eps {
			tot += x.sum
		}
		tagged := len(d.byTag) > 0
		fmt.Fprintf(&md, "### %s · endpoints by total time\n\n| endpoint | share | count | avg ms | p99 ms | SQL (from sqltag) |\n|---|---|---|---|---|---|\n", d.h.Name)
		for i, ep := range d.eps {
			if i >= 10 {
				break
			}
			share := 0.0
			if tot > 0 {
				share = 100 * ep.sum / tot
			}
			sqlInfo := ""
			if tagged {
				if qs, ok := d.byTag[paramKey(ep.key)]; ok {
					var tsum float64
					for _, q := range qs {
						tsum += q.sum
					}
					var parts []string
					for j, q := range qs {
						if j >= 3 {
							break
						}
						parts = append(parts, fmt.Sprintf("%s (%d×, %.1fs)", trunc(q.norm, 70), q.count, q.sum))
					}
					sqlInfo = fmt.Sprintf("%.1fs SQL (%.0f%% of endpoint time): %s", tsum, ternary2(ep.sum > 0, 100*tsum/ep.sum, 0), strings.Join(parts, "; "))
				}
			}
			fmt.Fprintf(&md, "| `%s` | %.0f%% | %d | %.0f | %.0f | %s |\n", ep.key, share, ep.count, 1000*ep.sum/float64(ep.count), 1000*pct(ep.rts, .99), strings.ReplaceAll(sqlInfo, "|", "\\|"))
		}
		apiReq := 0
		for _, x := range d.eps {
			if !strings.Contains(x.key, "static assets") {
				apiReq += x.count
			}
		}
		perDen, perNote := apiReq, "executions per API request"
		if apiReq == 0 && totalAPI > 0 { // DB-only host: its SQL comes from the app hosts' requests
			perDen, perNote = totalAPI, "executions per API request of all app hosts (this host serves no API)"
		}
		if d.h.SlowAgent > 0 {
			fmt.Fprintf(&md, "\n%d statements of the measurement agent (SHOW GLOBAL STATUS, processlist, row counts …; %.1fs) excluded.\n", d.h.SlowAgent, d.h.SlowAgentT)
		}
		fmt.Fprintf(&md, "\n### %s · SQL by total time (per request = %s)\n\n| query | share | count | per request | avg ms | examined/sent | called from | code |\n|---|---|---|---|---|---|---|---|\n", d.h.Name, perNote)
		for i, q := range d.sqls {
			if i >= 10 {
				break
			}
			share := 0.0
			if d.tsql > 0 {
				share = 100 * q.sum / d.tsql
			}
			perReq := 0.0
			if perDen > 0 {
				perReq = float64(q.count) / float64(perDen)
			}
			ratio := "-"
			if q.sent > 0 {
				ratio = fmt.Sprintf("×%.0f", float64(q.examined)/float64(q.sent))
			}
			qc := codeOf(q, ci)
			fmt.Fprintf(&md, "| `%s` | %.0f%% | %d | %.2f | %.1f | %s | %s | %s |\n", strings.ReplaceAll(trunc(q.norm, 120), "|", "\\|"), share, q.count, perReq, 1000*q.sum/float64(q.count), ratio, calledFrom(q), strings.ReplaceAll(qc.md(q.norm), "|", "\\|"))
		}
		if len(d.sqls) == 0 {
			md.WriteString("| (no app statements in this host's slow log — the DB runs elsewhere, or the slow log is off) | | | | | | | |\n")
		}
		md.WriteString("\n")

		// HTML: a short summary; the detail lives in the per-host tables (rows expand)
		if len(d.eps) == 0 && len(d.sqls) == 0 {
			fmt.Fprintf(&hb, `<div class="hvy-none"><b>%s</b> <span class="muted">· no app requests and no SQL in this run (bench machine or idle host)</span></div>`, e(d.h.Name))
			continue
		}
		fmt.Fprintf(&hb, `<h3>%s</h3><ol class="hvy">`, e(d.h.Name))
		if len(d.eps) > 0 {
			for i, ep := range d.eps {
				if i >= 5 {
					break
				}
				share := ternary2(tot > 0, 100*ep.sum/tot, 0)
				avg, p99 := 1000*ep.sum/float64(ep.count), 1000*pct(ep.rts, .99)
				top := ""
				if qs, ok := d.byTag[paramKey(ep.key)]; ok && len(qs) > 0 {
					q := qs[0]
					top = fmt.Sprintf(`<div class="hvq"><span class="muted">heaviest SQL</span> <code class="clamp" title="click to show it all">%s</code> <span class="muted">%d× · %.1fs</span></div>`, e(q.norm), q.count, q.sum)
				}
				appT, appL := epApp(ep.key)
				fmt.Fprintf(&hb, `<li><a href="#%s" class="epl"><code%s>%s</code></a>%s <span class="num %s">%.0f%%</span> <span class="muted">of request time · %d× · avg</span> <span class="%s">%.0f ms</span> <span class="muted">· p99</span> <span class="%s">%.0f ms</span>%s%s</li>`,
					epAnchor(d.h.Name, ep.key), appT, e(ep.key), appL, lvl(share, 20, 40), share, ep.count, lvl(avg, 100, 500), avg, lvl(p99, 300, 1000), p99, ternary(ep.c5 > 0, fmt.Sprintf(` <span class="bad">5xx %d</span>`, ep.c5), ""), top)
			}
		} else { // DB-only host: its heaviest statements
			for i, q := range d.sqls {
				if i >= 5 {
					break
				}
				share := ternary2(d.tsql > 0, 100*q.sum/d.tsql, 0)
				perReq := ternary2(perDen > 0, float64(q.count)/float64(perDen), 0)
				fmt.Fprintf(&hb, `<li><a href="#%s" class="epl">SQL</a> <code class="clamp" title="click to show it all">%s</code> <span class="num %s">%.0f%%</span> <span class="muted">of SQL time · %d× · per request</span> <span class="%s">%.2f</span> <span class="muted">· called from %s</span></li>`,
					sqlAnchor(d.h.Name, q.norm), e(q.norm), lvl(share, 20, 40), share, q.count, lvl(perReq, 1.5, 3), perReq, e(calledFrom(q)))
			}
		}
		hb.WriteString(`</ol>`)
	}
	hb.WriteString(`</section>`)

	// ---- flows: the bench's clients as step sequences (score loops, transitions, drop-off) on the busiest host ----
	{
		var best *hostDiag
		for i := range diags {
			if diags[i].sa.sessions > 0 && (best == nil || diags[i].total > best.total) {
				best = &diags[i]
			}
		}
		if best != nil {
			fr := computeFlowsCached(best.h, nz, gcfg.scorePost)
			renderFlows(&md, &hb, best.h.Name, fr)
			ro.meta["flows"] = flowsMeta(fr)
		}
	}

	// ---- report.json: top SQL per host with where it is in the code (app-analysis refresh shows it per endpoint) ----
	{
		var sq []map[string]any
		for _, d := range diags {
			for i, q := range d.sqls {
				if i >= 15 {
					break
				}
				qc := codeOf(q, ci)
				var locs []map[string]any
				for j, l := range qc.locs {
					if j >= 5 {
						break
					}
					m := map[string]any{"loc": l, "url": cl.url(l, qc.exact)}
					if qc.exact {
						m["n"] = qc.count[j]
					}
					locs = append(locs, m)
				}
				share := 0.0
				if d.tsql > 0 {
					share = q.sum / d.tsql
				}
				sq = append(sq, map[string]any{"host": d.h.Name, "norm": q.norm, "count": q.count, "sum_s": q.sum, "share": share, "tags": q.tags, "code": locs, "exact": qc.exact, "grep": grepFrag(q.norm)})
			}
		}
		ro.meta["sqls"] = sq
	}

	md.WriteString("## Next actions (ranked, auto-suggested — verify against the manual before acting)\n\n")
	hb.WriteString(`<section><h2>Next actions <span class="muted">(ranked, auto-suggested — verify against the manual before acting)</span></h2>`)
	for _, d := range diags {
		if len(hosts) > 1 {
			fmt.Fprintf(&md, "### %s\n\n", d.h.Name)
			fmt.Fprintf(&hb, `<h3>%s</h3>`, e(d.h.Name))
		}
		if len(d.acts) == 0 {
			md.WriteString("- nothing stood out; look at the detail sections\n\n")
			hb.WriteString(`<div class="muted">nothing stood out; look at the detail sections</div>`)
		}
		hb.WriteString(`<ol class="actions">`)
		for _, a := range d.acts {
			also := ""
			if len(a.also) > 0 {
				also = " (also " + strings.Join(a.also, ", ") + ")"
			}
			est := ternary(a.kind == "errors", "first (5xx)", fmt.Sprintf("%.1f = %s", a.score, a.why))
			fmt.Fprintf(&md, "- **[%s] %s**%s (est. %s) — %s%s%s\n", a.priority, a.title, also, est, a.detail, ternary(a.where != "", " Where: "+a.where+".", ""), ternary(a.cmd != "", "\n  ```\n  "+a.cmd+"\n  ```", ""))
			fmt.Fprintf(&hb, `<li class="%s"><span class="badge p">%s</span> <b>%s</b>%s <span class="est">est. %s</span><div class="muted">%s%s</div>%s</li>`, a.priority, a.priority, e(a.title), ternary(also != "", ` <span class="muted">`+e(also)+`</span>`, ""), e(est), e(a.detail), ternary(a.where != "", ` <span class="where">where: `+e(a.where)+`</span>`, ""), ternary(a.cmd != "", `<pre class="cmd">`+e(a.cmd)+`</pre>`, ""))
		}
		hb.WriteString(`</ol>`)
		md.WriteString("\n")
	}
	hb.WriteString(`</section>`)

	ro.summary = addTOCMarkdown(md.String())
	for _, d := range diags {
		for _, a := range d.acts {
			m := map[string]any{"host": d.h.Name, "priority": a.priority, "title": a.title, "detail": a.detail, "cmd": a.cmd, "where": a.where, "kind": a.kind, "group": a.group, "slug": slugOf(a.group), "file": a.file, "sql": a.sql, "score": math.Round(a.score*10) / 10, "score_why": a.why}
			if len(a.also) > 0 {
				m["also_hosts"] = a.also
			}
			ro.actions = append(ro.actions, m)
		}
	}

	// ---- timeline ----
	md.WriteString("\n")
	hb.WriteString(`<section><h2>Timeline <span class="muted">(all hosts · 10s buckets)</span></h2>`)
	type tl struct {
		h    *hostRun
		rows map[int]*bucketRow
	}
	var tls []tl
	for _, h := range hosts {
		tls = append(tls, tl{h, buildTimeline(h, t0, bucket)})
	}
	val := func(t tl, i int, k string) float64 {
		if r := t.rows[i]; r != nil {
			return r.vals[k]
		}
		return 0
	}
	mk := func(k string) []series {
		var ss []series
		for _, t := range tls {
			pts := make([]float64, nb)
			for i := 0; i < nb; i++ {
				pts[i] = val(t, i, k)
			}
			ss = append(ss, series{name: t.h.Name, pts: pts})
		}
		return ss
	}
	hb.WriteString(`<div class="charts">`)
	hb.WriteString(lineChart("requests / s", "req/s", mk("req_s"), 440, 160))
	hb.WriteString(lineChart("p99 latency", "ms", mk("p99_ms"), 440, 160))
	hb.WriteString(lineChart("CPU user %", "%", mk("cpu_user_pct"), 440, 160))
	hb.WriteString(lineChart("CPU sys / iowait %", "%", append(mk("cpu_sys_pct"), func() []series {
		s := mk("cpu_iowait_pct")
		for i := range s {
			s[i].name += " iowait"
			s[i].color = palette[(i+4)%len(palette)]
		}
		return s
	}()...), 440, 160))
	hb.WriteString(lineChart("MySQL questions / s", "q/s", mk("questions_per_s"), 440, 160))
	hb.WriteString(lineChart("MySQL threads_running (max)", "", mk("threads_running"), 440, 160))
	hb.WriteString(lineChart("row lock waits / s", "", mk("innodb_row_lock_waits_per_s"), 440, 160))
	hb.WriteString(lineChart("5xx / s", "", mk("err5xx_s"), 440, 160))
	hb.WriteString(lineChart("memory used %", "%", mk("mem_used_pct"), 440, 160))
	hb.WriteString(lineChart("disk write MB/s", "MB/s", mk("disk_write_mb_s"), 440, 160))
	hb.WriteString(lineChart("TCP in use (max)", "", mk("tcp_inuse"), 440, 160))
	hb.WriteString(lineChart("context switches / s", "", mk("ctxt_per_s"), 440, 160))
	hb.WriteString(`</div></section>`)
	cols := []struct{ k, label string }{{"req_s", "req/s"}, {"err5xx_s", "5xx/s"}, {"avg_ms", "avg ms"}, {"p99_ms", "p99 ms"}, {"cpu_user_pct", "cpu usr%"}, {"cpu_sys_pct", "cpu sys%"}, {"cpu_iowait_pct", "iowait%"}, {"load1", "load1"}, {"mem_used_pct", "mem%"}, {"disk_util_pct", "disk util%"}, {"net_rx_mb_s", "net rx MB/s"}, {"net_tx_mb_s", "net tx MB/s"}, {"tcp_inuse", "tcp"}, {"questions_per_s", "mysql q/s"}, {"com_select_per_s", "select/s"}, {"com_insert_per_s", "insert/s"}, {"com_update_per_s", "update/s"}, {"threads_running", "thr run"}, {"innodb_row_lock_waits_per_s", "lock w/s"}, {"innodb_row_lock_time_per_s", "lock ms/s"}, {"select_scan_per_s", "scan/s"}, {"innodb_data_fsyncs_per_s", "fsync/s"}}
	for _, t := range tls {
		fmt.Fprintf(&md, "## %s · timeline (10s buckets; avg per bucket, p99/max/threads as bucket max)\n\n| t |", t.h.Name)
		for _, c := range cols {
			md.WriteString(" " + c.label + " |")
		}
		md.WriteString("\n|---|" + strings.Repeat("---|", len(cols)) + "\n")
		fmt.Fprintf(&hb, `<section><h2>%s · Timeline <span class="muted">(10s buckets)</span></h2><div class="tw"><table class="tl"><tr><th>t</th>`, e(t.h.Name))
		for _, c := range cols {
			fmt.Fprintf(&hb, `<th>%s</th>`, e(c.label))
		}
		hb.WriteString(`</tr>`)
		for i := 0; i < nb; i++ {
			fmt.Fprintf(&md, "| %d |", i*10)
			fmt.Fprintf(&hb, `<tr><td class="num">%d</td>`, i*10)
			for _, c := range cols {
				v := val(t, i, c.k)
				fmt.Fprintf(&md, " %s |", fmtNum(v))
				fmt.Fprintf(&hb, `<td class="num">%s</td>`, fmtNum(v))
			}
			md.WriteString("\n")
			hb.WriteString(`</tr>`)
		}
		md.WriteString("\n")
		hb.WriteString(`</table></div>`)
		// app metrics if any
		if len(t.h.App) > 0 {
			keys := []string{}
			for k := range t.h.App[len(t.h.App)-1] {
				if k != "t" {
					keys = append(keys, k)
				}
			}
			sort.Strings(keys)
			fmt.Fprintf(&md, "app metrics (last sample): %s\n\n", func() string {
				var p []string
				for _, k := range keys {
					p = append(p, fmt.Sprintf("%s=%s", k, fmtNum(num(t.h.App[len(t.h.App)-1][k]))))
				}
				return strings.Join(p, ", ")
			}())
		}
		hb.WriteString(`</section>`)
	}

	// ---- per host: endpoints, sessions, sql, processlist, pprof ----
	// expansion of an endpoint row: what it runs (sqltag), where in the code, its 5xx and the steps around it
	var allDL []deadlock
	appKinds := map[string][]appErrKind{}
	for _, d := range diags {
		allDL = append(allDL, hostDeadlocks(d.h.Name, d.h.Dir)...)
		if ks, n := appErrorKinds(filepath.Join(d.h.Dir, "app.log")); n > 0 {
			appKinds[d.h.Name] = ks
		}
	}
	epDetail := func(d hostDiag, ep *epStat) string {
		var b strings.Builder
		if fn := d.epH[ep.key]; fn != "" {
			fmt.Fprintf(&b, `<div class="muted">handler: <code>%s</code></div>`, e(fn))
		}
		if qs, ok := d.byTag[paramKey(ep.key)]; ok && len(qs) > 0 {
			var tsum float64
			for _, q := range qs {
				tsum += q.sum
			}
			fmt.Fprintf(&b, `<div class="muted">SQL it issued (sqltag%s): %.1fs = %.0f%% of its request time</div><table><tr><th>query</th><th>count</th><th>per request</th><th>sum s</th><th>avg ms</th><th>examined/sent</th><th>code</th></tr>`, ternary(d.sqlFrom != "", ", slow log of "+e(d.sqlFrom), ""), tsum, ternary2(ep.sum > 0, 100*tsum/ep.sum, 0))
			for j, q := range qs {
				if j >= 12 {
					break
				}
				per := float64(q.count) / float64(ep.count)
				ratio, r := "-", 0.0
				if q.sent > 0 {
					r = float64(q.examined) / float64(q.sent)
					ratio = fmt.Sprintf("×%.0f", r)
				}
				fmt.Fprintf(&b, `<tr><td><code class="clamp" title="click to show it all">%s</code></td><td class="num">%d</td><td class="num %s">%.2f</td><td class="num">%.2f</td><td class="num %s">%.1f</td><td class="num %s">%s</td><td>%s</td></tr>`, e(q.norm), q.count, lvl(per, 1.5, 3), per, q.sum, lvl(1000*q.sum/float64(q.count), 5, 50), 1000*q.sum/float64(q.count), lvl(r, 100, 1000), ratio, codeOf(q, ci).html(q.norm, cl))
			}
			b.WriteString(`</table>`)
		} else {
			b.WriteString(`<div class="muted">no SQL tagged with this endpoint (no measurement patch, or it runs no SQL)</div>`)
		}
		if ep.c5 > 0 {
			path := ep.key
			if i := strings.IndexByte(path, ' '); i >= 0 {
				path = path[i+1:]
			}
			fmt.Fprintf(&b, `<div class="bad">5xx: %d of %d</div><ul class="ex">`, ep.c5, ep.count)
			n := 0
			for _, ks := range appKinds {
				for _, k := range ks {
					if k.endpoint != "" && paramKey(k.endpoint) == paramKey(path) && n < 4 {
						fmt.Fprintf(&b, `<li><code>%s</code> <span class="muted">×%d</span></li>`, e(trunc(k.message, 300)), k.count)
						n++
					}
				}
			}
			for _, dl := range allDL {
				if dl.matches(ep.key) {
					fmt.Fprintf(&b, `<li>deadlock: <code>%s</code></li>`, e(dl.summary()))
					n++
					break
				}
			}
			if n == 0 {
				b.WriteString(`<li class="muted">no matching line in the app log</li>`)
			}
			b.WriteString(`</ul>`)
		}
		var before, after []*transStat
		for _, t := range d.sa.transitions {
			if t.to == ep.key && t.from != ep.key && len(before) < 3 {
				before = append(before, t)
			}
			if t.from == ep.key && t.to != ep.key && len(after) < 3 {
				after = append(after, t)
			}
		}
		if len(before) > 0 || len(after) > 0 {
			step := func(ts []*transStat, other func(*transStat) string) string {
				var p []string
				for _, t := range ts {
					par := ""
					if t.n > 0 && t.overlap*20 >= t.n { // ≥ 5% of these transitions overlapped
						par = fmt.Sprintf(" · parallel %.0f%%", 100*float64(t.overlap)/float64(t.n))
					}
					p = append(p, fmt.Sprintf(`<code>%s</code> <span class="muted">%d× gap %s%s</span>`, e(other(t)), t.n, e(fmtGap(pct(t.gaps, .5))), par))
				}
				if len(p) == 0 {
					return `<span class="muted">-</span>`
				}
				return strings.Join(p, " · ")
			}
			fmt.Fprintf(&b, `<div class="muted">steps around it in the bench's sessions (gap = median time from the previous response to the next request; ~0 ms = the client sends the next request right away)</div><div>before: %s</div><div>after: %s</div>`, step(before, func(t *transStat) string { return t.from }), step(after, func(t *transStat) string { return t.to }))
		}
		return b.String()
	}
	for _, d := range diags {
		h := d.h
		eps, total := d.eps, d.total
		if total > 0 && len(eps) > 0 { // empty tables (a DB-only host) are not rendered
			fmt.Fprintf(&md, "## %s · endpoints (alp-style, sorted by total time; %d requests)\n\n| method path | count | 2xx/4xx/5xx | sum s | avg ms | p50 | p90 | p99 | max | bytes avg |\n|---|---|---|---|---|---|---|---|---|---|\n", h.Name, total)
			fmt.Fprintf(&hb, `<section><h2>%s · Endpoints <span class="muted">(%d requests, sorted by total time)</span></h2><div class="tw"><table><tr><th>method path</th><th>count</th><th>2xx/4xx/5xx</th><th>304%%</th><th>sum s</th><th>avg ms</th><th>p50</th><th>p90</th><th>p99</th><th>max</th><th>bytes avg</th><th>req/s over time</th></tr>`, e(h.Name), total)
			for i, ep := range eps {
				if i >= 40 {
					break
				}
				avg := ep.sum / float64(ep.count)
				fmt.Fprintf(&md, "| `%s` | %d | %d/%d/%d | %.0f%% | %.2f | %s | %s | %s | %s | %s | %d |\n", ep.key, ep.count, ep.c2, ep.c4, ep.c5, 100*float64(ep.c304)/float64(ep.count), ep.sum, ms(avg), ms(pct(ep.rts, .5)), ms(pct(ep.rts, .9)), ms(pct(ep.rts, .99)), ms(ep.max), ep.bytes/int64(ep.count))
				pts := make([]float64, nb)
				for bi, c := range ep.perBucket {
					if bi >= 0 && bi < nb {
						pts[bi] = float64(c) / bucket
					}
				}
				appT, _ := epApp(ep.key)
				fmt.Fprintf(&hb, `<tr id="%s" class="exp" title="click: SQL, code, 5xx, steps around it"><td><span class="tw-ar">▸</span> <code%s>%s</code></td><td class="num">%d</td><td class="num">%d/%d/<span class="%s">%d</span></td><td class="num">%.0f%%</td><td class="num">%.2f</td><td class="num %s">%s</td><td class="num">%s</td><td class="num">%s</td><td class="num %s">%s</td><td class="num %s">%s</td><td class="num">%d</td><td>%s</td></tr>`, epAnchor(h.Name, ep.key), appT, e(ep.key), ep.count, ep.c2, ep.c4, ternary(ep.c5 > 0, "bad", ""), ep.c5, 100*float64(ep.c304)/float64(ep.count), ep.sum, lvl(avg, .1, .5), ms(avg), ms(pct(ep.rts, .5)), ms(pct(ep.rts, .9)), lvl(pct(ep.rts, .99), .3, 1), ms(pct(ep.rts, .99)), lvl(ep.max, 1, 3), ms(ep.max), ep.bytes/int64(ep.count), sparkline(pts, 140, 22))
				fmt.Fprintf(&hb, `<tr class="epx" hidden><td colspan="12">%s</td></tr>`, epDetail(d, ep))
			}
			md.WriteString("\n")
			hb.WriteString(`</table></div></section>`)
		}

		// sessions
		sa := d.sa
		if sa.sessions > 0 {
			roleNames := []string{}
			for r := range sa.roles {
				roleNames = append(roleNames, r)
			}
			sort.Strings(roleNames)
			var rs []series
			fmt.Fprintf(&md, "## %s · bench behaviour (from session cookies; %d sessions, static assets excluded)\n\n", h.Name, sa.sessions)
			for _, r := range roleNames {
				pts := make([]float64, nb)
				for i := 0; i < nb; i++ {
					pts[i] = float64(sa.roles[r][i])
				}
				rs = append(rs, series{name: fmt.Sprintf("%s (%d)", r, sa.roleSessions[r]), pts: pts})
				fmt.Fprintf(&md, "- active `%s` sessions per 10s (%d total): %s\n", r, sa.roleSessions[r], strings.Trim(fmt.Sprint(intsOf(pts)), "[]"))
			}
			md.WriteString("\n")
			fmt.Fprintf(&hb, `<section><h2>%s · Bench behaviour <span class="muted">(%d sessions seen in cookies; static assets excluded)</span></h2><div class="charts">%s</div>`, e(h.Name), sa.sessions, lineChart("active sessions per 10s, by cookie (role)", "sessions", rs, 440, 160))
			md.WriteString("### polling (same endpoint repeated within a session)\n\n| endpoint | calls | sessions | median gap s |\n|---|---|---|---|\n")
			hb.WriteString(`<h3>Polling (same endpoint repeated within a session)</h3><div class="tw"><table><tr><th>endpoint</th><th>calls</th><th>sessions</th><th>median gap s</th></tr>`)
			for i, p := range sa.polling {
				if i >= 10 {
					break
				}
				fmt.Fprintf(&md, "| `%s` | %d | %d | %.3f |\n", p.ep, p.calls, p.sessions, p.medGap)
				fmt.Fprintf(&hb, `<tr><td><code>%s</code></td><td class="num">%d</td><td class="num">%d</td><td class="num">%.3f</td></tr>`, e(p.ep), p.calls, p.sessions, p.medGap)
			}
			hb.WriteString(`</table></div>`)
			md.WriteString("\n### transitions (previous → next request within a session; gap = think time after the previous response)\n\n| from | to | count | median gap s | p90 gap s |\n|---|---|---|---|---|\n")
			hb.WriteString(`<h3>Transitions (previous → next request within a session; gap = think time after the previous response)</h3><div class="tw"><table><tr><th>from</th><th>to</th><th>count</th><th>median gap s</th><th>p90 gap s</th></tr>`)
			for i, t := range sa.transitions {
				if i >= 25 {
					break
				}
				fmt.Fprintf(&md, "| `%s` | `%s` | %d | %.3f | %.3f |\n", t.from, t.to, t.n, pct(t.gaps, .5), pct(t.gaps, .9))
				fmt.Fprintf(&hb, `<tr><td><code>%s</code></td><td><code>%s</code></td><td class="num">%d</td><td class="num">%.3f</td><td class="num">%.3f</td></tr>`, e(t.from), e(t.to), t.n, pct(t.gaps, .5), pct(t.gaps, .9))
			}
			hb.WriteString(`</table></div>`)
			uas := []string{}
			for ua, n := range sa.uas {
				uas = append(uas, fmt.Sprintf("%s×%d", ua, n))
			}
			sort.Strings(uas)
			fmt.Fprintf(&md, "\nuser agents: %s\n\n", strings.Join(uas, ", "))
			fmt.Fprintf(&hb, `<div class="muted">user agents: %s</div></section>`, e(strings.Join(uas, ", ")))
		}

		// slow queries
		if h.SlowInit > 0 {
			fmt.Fprintf(&md, "(%d statements during /initialize were excluded from %s's SQL table)\n\n", h.SlowInit, h.Name)
		}
		if h.SlowAgent > 0 {
			fmt.Fprintf(&md, "(%d statements of the measurement agent itself (SHOW GLOBAL STATUS, processlist, row counts …; %.1fs) were excluded from %s's SQL tables, N+1 suspects and actions)\n\n", h.SlowAgent, h.SlowAgentT, h.Name)
		}
		if h.SlowN > 0 && len(d.sqls) > 0 {
			sqls, total := d.sqls, d.tsql
			fmt.Fprintf(&md, "## %s · slow queries (slp-style; %d statements, %.1fs total, sorted by total time)\n\n| query | count | sum s | avg ms | max ms | rows examined/sent | lock ms |\n|---|---|---|---|---|---|---|\n", h.Name, h.SlowN, total)
			fmt.Fprintf(&hb, `<section><h2>%s · Slow queries <span class="muted">(%d statements, %.1fs total, sorted by total time)</span></h2><div class="tw"><table><tr><th>query</th><th>count</th><th>sum s</th><th>avg ms</th><th>max ms</th><th>rows examined / sent</th><th>lock ms</th></tr>`, e(h.Name), h.SlowN, total)
			for i, s := range sqls {
				if i >= 40 {
					break
				}
				q := s.norm
				if len(q) > 220 {
					q = q[:220] + "…"
				}
				ratio := ""
				if s.sent > 0 {
					ratio = fmt.Sprintf(" (×%.0f)", float64(s.examined)/float64(s.sent))
				}
				fmt.Fprintf(&md, "| `%s` | %d | %.2f | %s | %s | %d/%d%s | %s |\n", strings.ReplaceAll(q, "|", "\\|"), s.count, s.sum, ms(s.sum/float64(s.count)), ms(s.max), s.examined, s.sent, ratio, ms(s.lock))
				sRatio := 0.0
				if s.sent > 0 {
					sRatio = float64(s.examined) / float64(s.sent)
				}
				fmt.Fprintf(&hb, `<tr id="%s"><td><code class="clamp" title="%s">%s</code></td><td class="num">%d</td><td class="num">%.2f</td><td class="num %s">%s</td><td class="num %s">%s</td><td class="num %s">%d / %d%s</td><td class="num %s">%s</td></tr>`, sqlAnchor(h.Name, s.norm), e(trunc(s.example, 400)), e(q), s.count, s.sum, lvl(s.sum/float64(s.count), .005, .05), ms(s.sum/float64(s.count)), lvl(s.max, .1, 1), ms(s.max), lvl(sRatio, 100, 1000), s.examined, s.sent, e(ratio), lvl(s.lock, .01, .1), ms(s.lock))
			}
			md.WriteString("\n")
			hb.WriteString(`</table></div></section>`)
		}

		// processlist
		if pls := aggregateProcesslist(h.PL); h.PL != "" && len(pls) > 0 {
			fmt.Fprintf(&md, "## %s · statements seen running (processlist snapshots every 5s)\n\n| statement | seen | max time s | states |\n|---|---|---|---|\n", h.Name)
			fmt.Fprintf(&hb, `<section><h2>%s · Statements seen running <span class="muted">(processlist snapshots every 5s)</span></h2><div class="tw"><table><tr><th>statement</th><th>seen</th><th>max time s</th><th>states</th></tr>`, e(h.Name))
			for i, p := range pls {
				if i >= 15 {
					break
				}
				q := p.norm
				if len(q) > 200 {
					q = q[:200] + "…"
				}
				st := []string{}
				for k, n := range p.state {
					st = append(st, fmt.Sprintf("%s×%d", k, n))
				}
				sort.Strings(st)
				fmt.Fprintf(&md, "| `%s` | %d | %d | %s |\n", strings.ReplaceAll(q, "|", "\\|"), p.seen, p.maxT, strings.Join(st, ", "))
				fmt.Fprintf(&hb, `<tr><td><code>%s</code></td><td class="num">%d</td><td class="num">%d</td><td class="muted">%s</td></tr>`, e(q), p.seen, p.maxT, e(strings.Join(st, ", ")))
			}
			md.WriteString("\n")
			hb.WriteString(`</table></div></section>`)
		}

		// pprof
		// a profile with no samples ("… of 0 total") is not rendered; a host with none left gets no pprof section
		profTop := map[string]string{}
		for _, n := range []string{"cpu", "fgprof", "heap", "block", "mutex"} {
			if p, ok := h.Pprof[n]; ok {
				if top := pprofTop(p, 40); !strings.Contains(top, " of 0 total") {
					profTop[n] = top
				}
			}
		}
		if len(profTop) > 0 {
			fmt.Fprintf(&hb, `<section><h2>%s · pprof</h2>`, e(h.Name))
			for _, n := range []string{"cpu", "fgprof", "heap", "block", "mutex"} {
				top, ok := profTop[n]
				if !ok {
					continue
				}
				p := h.Pprof[n]
				svg := pprofSVG(p)
				flame, hline := "", ""
				if n == "cpu" { // per-handler CPU (access log + code index) in the flame page and one line here
					var hs []flameHandler
					flame, hs = pprofFlameHTMLCtx(p, flameTitle(h.Name, n, p), &flameCtx{access: h.Access, nz: nz, ci: ci})
					hline = flameHandlersLine(hs)
				} else {
					flame = pprofFlameHTML(p, flameTitle(h.Name, n, p))
				}
				fmt.Fprintf(&md, "## %s · pprof %s (top 40)\n\n```\n%s\n```\n\n", h.Name, n, top)
				rel := h.Name + "/" + filepath.Base(p)
				links := `<a href="` + e(rel) + `" download>download .pb.gz</a> · local: <code>go tool pprof -http=:8081 ` + e(filepath.Base(p)) + `</code>`
				if svg != "" {
					links = `<a href="` + e(h.Name+"/"+filepath.Base(svg)) + `" target="_blank">call graph (svg)</a> · ` + links
				}
				if flame != "" {
					links = `<a href="` + e(h.Name+"/"+filepath.Base(flame)) + `" target="_blank"><b>flame graph</b></a> · ` + links
				}
				topHTML := e(top)
				if flame != "" { // function names link to the flame page with that function selected (summed over every caller)
					topHTML = pprofTopWithLinks(top, h.Name+"/"+filepath.Base(flame))
				}
				if hline != "" {
					links += `<br>` + e(hline)
					fmt.Fprintf(&md, "%s: %s\n\n", h.Name, hline)
				}
				fmt.Fprintf(&hb, `<details %s><summary>%s</summary><div class="muted">%s</div><pre>%s</pre></details>`, ternary(n == "cpu", "open", ""), e(n), links, topHTML)
			}
			hb.WriteString(`</section>`)
		} else {
			fmt.Fprintf(&md, "(%s: no pprof profiles with samples in this run)\n\n", h.Name)
		}
	}

	ro.md = addTOCMarkdown(md.String())
	body, tocNav := addTOCHTML(hb.String())
	htm := `<!DOCTYPE html><html lang="ja"><head><meta charset="utf-8"><title>Bench report ` + e(ts) + `</title><style>` + reportCSS + `</style></head><body><header>` + langToggleHTML + `<h1>Bench report ` + e(ts) + `</h1><div class="sub">generated ` + e(time.Now().Format(time.RFC3339)) + ` · isumeasure ` + version + ` · endpoint grouping: ` + e(nz.describe()) + ` · ` + e(codeIdx) + `</div><div class="sub">` + e(strings.NewReplacer("`", "", "**", "", "\n\n> ", " — ").Replace(repoLine)) + `</div><div class="sub"><a class="appl" href="/app/" target="_blank">App analysis: screens → API → handler → tables →</a></div><div class="sub"><span class="hi" style="color:#fcd34d">amber</span> = worth a look · <span class="bad" style="color:#fca5a5">red</span> = fix first (endpoint avg ≥ 100 / 500 ms, p99 ≥ 300 / 1000 ms, share ≥ 20 / 40 %; SQL avg ≥ 5 / 50 ms, per request ≥ 1.5 / 3, rows examined/sent ≥ 100 / 1000, lock ≥ 10 / 100 ms)</div></header><main>` + tocNav + body + `</main><script>` + sortJS + per10sJS + expandJS + `</script><script>var I18N=` + jaCatalogJSON() + `;` + langJS + `</script></body></html>`
	ro.html = htm
	return ro, nil
}

func ternary2(c bool, a, b float64) float64 {
	if c {
		return a
	}
	return b
}

func intsOf(f []float64) []int {
	out := make([]int, len(f))
	for i, v := range f {
		out[i] = int(v)
	}
	return out
}

func ternary(c bool, a, b string) string {
	if c {
		return a
	}
	return b
}

func sparkline(pts []float64, w, h int) string {
	if len(pts) == 0 {
		return ""
	}
	maxv := 0.0
	for _, v := range pts {
		if v > maxv {
			maxv = v
		}
	}
	if maxv == 0 {
		maxv = 1
	}
	var p []string
	for i, v := range pts {
		x := float64(w) * float64(i) / float64(max(1, len(pts)-1))
		y := float64(h) - float64(h)*v/maxv
		p = append(p, fmt.Sprintf("%.1f,%.1f", x, y))
	}
	return fmt.Sprintf(`<svg class="spark" width="%d" height="%d"><polyline fill="none" stroke="#0f766e" stroke-width="1.3" points="%s"/></svg>`, w, h, strings.Join(p, " "))
}

const sortJS = `
// click a header to sort (numeric-aware: 1.2k, 3M, ×12, 12/3/0, 45%). Click again to flip.
document.querySelectorAll("table").forEach((table) => {
  const head = table.rows[0]; if (!head) return;
  [...head.cells].forEach((th, ci) => {
    th.style.cursor = "pointer"; th.title = "click to sort";
    th.addEventListener("click", () => {
      const rows = [...table.rows].slice(1);
      const val = (r) => { const t = (r.cells[ci]?.textContent || "").trim(); const m = t.match(/^[-+]?[\d.,]+/); if (!m) return t.toLowerCase(); let v = parseFloat(m[0].replace(/,/g, "")); if (/k\b/.test(t.slice(m[0].length, m[0].length + 1))) v *= 1e3; if (/M\b/.test(t.slice(m[0].length, m[0].length + 1))) v *= 1e6; return v; };
      const dir = th.dataset.dir === "asc" ? "desc" : "asc"; [...head.cells].forEach((c) => delete c.dataset.dir); th.dataset.dir = dir;
      rows.sort((a, b) => { const x = val(a), y = val(b); if (typeof x === "number" && typeof y === "number") return dir === "asc" ? x - y : y - x; return dir === "asc" ? String(x).localeCompare(String(y)) : String(y).localeCompare(String(x)); });
      rows.forEach((r) => table.appendChild(r));
      [...head.cells].forEach((c) => c.classList.remove("asc", "desc")); th.classList.add(dir);
    });
  });
});
`

// expandJS: endpoint rows open their detail row; long SQL opens on click; a #ep-… link opens and scrolls to its row.
const expandJS = `
document.addEventListener("click",function(ev){
 var c=ev.target.closest("code.clamp");if(c){c.classList.toggle("open");ev.stopPropagation();return}
 if(ev.target.closest("a"))return;
 var r=ev.target.closest("tr.exp");if(!r)return;
 var x=r.nextElementSibling;if(x&&x.classList.contains("epx")){x.hidden=!x.hidden;r.classList.toggle("open",!x.hidden)}
});
function openHash(){var id=decodeURIComponent(location.hash.slice(1));if(!id)return;var r=document.getElementById(id);
 if(r&&r.classList.contains("exp")){var x=r.nextElementSibling;if(x&&x.hidden){x.hidden=false;r.classList.add("open")}r.scrollIntoView({block:"center"})}}
window.addEventListener("hashchange",openHash);openHash();
`

const reportCSS = `
:root{--ink:#1f2937;--muted:#6b7280;--accent:#0f766e;--line:#e5e7eb}
*{box-sizing:border-box}body{font-family:-apple-system,"Hiragino Sans","Noto Sans JP",sans-serif;margin:0;background:#fafaf9;color:var(--ink);font-size:13px;line-height:1.5}
header{background:#0f766e;color:#fff;padding:12px 24px}header h1{margin:0;font-size:18px}header .sub{color:#d1fae5;font-size:12px}header a.appl{color:#fff;font-weight:600}a.appl2{font-size:11px;color:#7c3aed;text-decoration:none;margin-left:4px}
main{max-width:1500px;margin:0 auto;padding:14px 24px}section{background:#fff;border:1px solid var(--line);border-radius:10px;padding:14px 18px;margin-bottom:14px}
h2{font-size:16px;margin:0 0 8px;border-left:4px solid var(--accent);padding-left:8px}h3{font-size:14px;margin:12px 0 6px}
table{border-collapse:collapse;width:100%;font-size:12px}th,td{border:1px solid var(--line);padding:3px 7px;vertical-align:top;text-align:left}th{background:#f1f5f4;white-space:nowrap}td.num{text-align:right;white-space:nowrap;font-variant-numeric:tabular-nums}
table.tl td,table.tl th{padding:2px 5px;font-size:11px}
.tw{overflow-x:auto;max-width:100%}.muted{color:var(--muted);font-size:12px}.bad{color:#b91c1c;font-weight:600}.hi{color:#b45309;font-weight:600}td.bad{background:#fef2f2}td.hi{background:#fffbeb}
code{font-family:ui-monospace,Menlo,monospace;font-size:11.5px;background:#f3f4f6;padding:1px 4px;border-radius:3px;word-break:break-all}
pre{background:#111827;color:#e5e7eb;padding:8px;border-radius:6px;overflow:auto;font-size:11.5px;max-height:480px}
.charts{display:grid;grid-template-columns:repeat(auto-fill,minmax(440px,1fr));gap:8px}svg.chart{background:#fff;border:1px solid var(--line);border-radius:6px}.spark{vertical-align:middle}
details{margin:6px 0}summary{cursor:pointer;font-weight:600}
th.asc::after{content:" ▲"}th.desc::after{content:" ▼"}
.est{font-size:11px;color:#475569;background:#f1f5f9;border-radius:4px;padding:1px 6px;margin-left:4px}.verdict{border-left:5px solid #94a3b8;background:#f8fafc;padding:8px 12px;border-radius:0 8px 8px 0;margin:6px 0}.vlbl{display:inline-block;font-size:11px;font-weight:700;padding:1px 6px;border-radius:4px;background:#e2e8f0;color:#334155;margin-right:4px}.vfix{margin:4px 0;color:#b91c1c;font-weight:600}.vfix .vlbl{background:#fee2e2;color:#b91c1c}.vnear{margin:2px 0;color:#92400e;font-size:13px}.vexplain{background:#f1f5f9;border-radius:8px;padding:6px 12px;margin:6px 0 10px;font-size:13px;color:#334155}.vexplain p{margin:3px 0}.verdict .vt{font-size:15px;font-weight:700}.verdict ul{margin:4px 0 0;padding-left:18px;font-size:12px;color:#374151}
.verdict.db-cpu,.verdict.app-cpu,.verdict.cpu,.verdict.errors,.verdict.lock,.verdict.io{border-color:#dc2626;background:#fef2f2}.verdict.wait{border-color:#d97706;background:#fffbeb}
.badge{display:inline-block;font-size:11px;padding:0 6px;border-radius:999px;background:#e0f2f1;color:#0f766e}.badge.p{background:#111827;color:#fff}
.two{display:grid;grid-template-columns:1fr;gap:12px}
ol.hvy{margin:4px 0 10px;padding-left:22px}ol.hvy li{margin:4px 0}ol.hvy .hvq{margin:2px 0 0 4px;font-size:12px;display:flex;gap:8px;align-items:baseline}ol.hvy .hvq code{flex:1;min-width:0}ol.hvy .hvq>span{white-space:nowrap}a.epl{text-decoration:none}.hvy-none{margin:6px 0}
code.clamp{display:-webkit-box;-webkit-line-clamp:2;-webkit-box-orient:vertical;overflow:hidden;cursor:pointer;white-space:normal;word-break:break-word}code.clamp.open{-webkit-line-clamp:unset;display:block}
tr.exp{cursor:pointer}tr.exp:hover td{background:#f0fdfa}tr.exp.open td{background:#ecfdf5}.tw-ar{display:inline-block;color:#0f766e;transition:transform .15s}tr.exp.open .tw-ar{transform:rotate(90deg)}
tr.epx>td{background:#f8fafc;padding:8px 12px}tr.epx table{margin:4px 0 8px}tr.epx ul.ex{margin:2px 0 6px;padding-left:18px}
.bar{display:inline-block;height:8px;background:#99f6e4;vertical-align:middle;margin-right:4px;max-width:60px}
ol.actions{padding-left:18px}ol.actions li{margin:6px 0;padding:6px 10px;border-left:4px solid #94a3b8;background:#f8fafc;border-radius:0 6px 6px 0}ol.actions li.P1{border-color:#dc2626;background:#fef2f2}ol.actions li.P2{border-color:#d97706;background:#fffbeb}pre.cmd{max-height:none;margin:4px 0 0;font-size:11.5px}.where{color:#0f766e}
nav.toc{background:#fff;border:1px solid var(--line);border-radius:10px;padding:8px 14px;margin-bottom:14px;font-size:12px;line-height:1.9}nav.toc a{color:#0f766e;text-decoration:none;white-space:nowrap}nav.toc a:hover{text-decoration:underline}nav.toc .sep{color:#d1d5db}
.lang{float:right;display:flex;gap:4px}.lang button{font-size:12px;padding:3px 10px;border:1px solid #94a3b8;border-radius:6px;background:transparent;color:inherit;cursor:pointer;opacity:.7}.lang button.on{opacity:1;font-weight:700;background:#0f766e;border-color:#0f766e;color:#fff}
nav.toc .th{margin-left:18px}nav.toc .hn{font-weight:600;color:#111827}.hostgrp{margin:16px 0 6px 6px;font-size:13px;font-weight:700;color:#0f766e}.p10ctl{font-size:12px;margin:4px 0 8px;line-height:2}.p10ctl label{margin-right:10px;white-space:nowrap;cursor:pointer}.p10ctl .sw{display:inline-block;width:10px;height:10px;border-radius:2px;margin:0 4px 0 2px;vertical-align:middle}.p10ctl button{font-size:11px;padding:1px 8px}.p10 .ct{font-size:12px;font-weight:600;margin:4px 0}
.flowslink{margin:6px 0 14px;font-size:13px}.flowslink a{color:#0f766e}
nav.toc .tg{margin:2px 0}nav.toc .gt{font-weight:600;color:#111827}nav.toc .gn,.grp .gn{color:#0f766e;font-weight:700}
.grp{margin:22px 0 8px;padding:4px 2px;border-bottom:2px solid #0f766e;font-size:15px;font-weight:700}.grp .muted{font-weight:400;font-size:12px}
.totop{font-size:11px;margin:-4px 0 6px}.totop a{color:var(--muted);text-decoration:none}.totop a:hover{color:#0f766e}
`

var pdnsTables = map[string]bool{"records": true, "domains": true, "domainmetadata": true, "comments": true, "cryptokeys": true, "tsigkeys": true, "supermasters": true}

func isPdnsTable(t string) bool { return pdnsTables[strings.ToLower(strings.TrimSpace(t))] }

// slugOf turns a work-unit group into a branch-safe name: file:x.go -> file-x, endpoint:GET /api/a/:id -> endpoint-get-api-a-id
func slugOf(group string) string {
	var b strings.Builder
	last := byte('-')
	for i := 0; i < len(group); i++ {
		c := group[i]
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9':
			b.WriteByte(c)
			last = c
		case c >= 'A' && c <= 'Z':
			b.WriteByte(c + 32)
			last = c
		default:
			if last != '-' {
				b.WriteByte('-')
				last = '-'
			}
		}
	}
	return strings.Trim(strings.TrimSuffix(b.String(), "-go"), "-")
}

// parseRows reads a rows-*.txt ("db.table count" per line, "#" comments) into table -> count.
func parseRows(path string) map[string]int64 {
	out := map[string]int64{}
	b, err := os.ReadFile(path)
	if err != nil {
		return out
	}
	for _, ln := range strings.Split(string(b), "\n") {
		f := strings.Fields(ln)
		if len(f) != 2 || strings.HasPrefix(ln, "#") {
			continue
		}
		if n, err := strconv.ParseInt(f[1], 10, 64); err == nil {
			out[f[0]] = n
		}
	}
	return out
}

type rowDelta struct {
	table      string
	start, end int64
}

func sumDelta(ds []rowDelta) int64 {
	var t int64
	for _, d := range ds {
		if d.end-d.start > 0 {
			t += d.end - d.start
		}
	}
	return t
}

// memWatchdog aborts this process (exit 3, clear message) when the Go heap passes capMB. The panel runs
// `report` as a child with this set, so an oversized run fails the report instead of OOM-killing the panel.
func memWatchdog(capMB int) {
	samples := []metrics.Sample{{Name: "/memory/classes/total:bytes"}, {Name: "/memory/classes/heap/released:bytes"}}
	for {
		time.Sleep(200 * time.Millisecond)
		metrics.Read(samples)
		if samples[0].Value.Kind() != metrics.KindUint64 {
			return
		}
		used := samples[0].Value.Uint64() - samples[1].Value.Uint64()
		if used > uint64(capMB)<<20 {
			fmt.Fprintf(os.Stderr, "report: memory over %d MB (REPORT_MEM_MB) — the run's logs are too large for this ops host; aborting\n", capMB)
			os.Exit(3)
		}
	}
}

// lvl picks the cell class for a threshold pair: "" below warn, "hi" (amber) from warn, "bad" (red) from bad.
func lvl(v, warn, bad float64) string {
	switch {
	case v >= bad:
		return "bad"
	case v >= warn:
		return "hi"
	}
	return ""
}

// benchShape: in-flight requests ≈ Σ request time of the requests completing in each second (Little's law:
// L = λ·W), over the load window. Flat → the bench runs a fixed number of clients that wait for each response.
type benchShape struct {
	fixed                      bool
	med, p10, p90, first, last float64
	secs                       int
}

func estimateBenchShape(h *hostRun, w loadWindow) benchShape {
	if w.fromT.IsZero() || !w.toT.After(w.fromT) {
		return benchShape{}
	}
	per := map[int]float64{}
	for _, ev := range h.Access {
		if ev.at.Before(w.fromT) || ev.at.After(w.toT) {
			continue
		}
		per[int(ev.at.Sub(w.fromT).Seconds())] += ev.reqtime
	}
	if len(per) < 6 {
		return benchShape{}
	}
	secs := make([]int, 0, len(per))
	for k := range per {
		secs = append(secs, k)
	}
	sort.Ints(secs)
	vals := make([]float64, len(secs))
	for i, k := range secs {
		vals[i] = per[k]
	}
	third := len(vals) / 3
	med := func(v []float64) float64 {
		c := append([]float64{}, v...)
		sort.Float64s(c)
		return c[len(c)/2]
	}
	sorted := append([]float64{}, vals...)
	sort.Float64s(sorted)
	bs := benchShape{secs: len(vals), med: med(vals), p10: pct(sorted, .1), p90: pct(sorted, .9), first: med(vals[:third]), last: med(vals[len(vals)-third:])}
	bs.fixed = bs.med > 0 && bs.last <= 1.3*bs.first && bs.last >= bs.first/1.3 && (bs.p90-bs.p10) <= 0.6*bs.med
	return bs
}

var pdnsSQLRe = regexp.MustCompile(`(?i)\b(records|domains|domainmetadata|cryptokeys|tsigkeys)\b`)
var appErrRe = regexp.MustCompile(`(?i)\b(error|panic|fatal|level=error|"level":"error")\b|\s5\d\d\s|エラー`)

// previousRunMeta finds the newest sibling run before this one that has a report.json (the panel keeps them).
func previousRunMeta(dir string) (string, map[string]any) {
	parent, me := filepath.Dir(dir), filepath.Base(dir)
	entries, err := os.ReadDir(parent)
	if err != nil {
		return "", nil
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() && e.Name() < me && len(e.Name()) == len(me) {
			names = append(names, e.Name())
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(names)))
	for _, n := range names {
		if b, err := os.ReadFile(filepath.Join(parent, n, "report.json")); err == nil {
			var m map[string]any
			if json.Unmarshal(b, &m) == nil && m["endpoints"] != nil {
				return n, m
			}
		}
	}
	return "", nil
}
