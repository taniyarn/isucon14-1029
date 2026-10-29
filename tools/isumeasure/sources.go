package main

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ---- MySQL: SHOW GLOBAL STATUS via the mysql CLI (root over the unix socket; works with auth_socket) ----

var mysqlCounters = []string{"Questions", "Com_select", "Com_insert", "Com_update", "Com_delete", "Com_commit", "Com_rollback", "Innodb_row_lock_waits", "Innodb_row_lock_time", "Innodb_data_fsyncs", "Innodb_data_reads", "Innodb_buffer_pool_reads", "Created_tmp_disk_tables", "Created_tmp_tables", "Select_scan", "Select_full_join", "Sort_merge_passes", "Slow_queries", "Handler_read_rnd_next", "Bytes_sent", "Bytes_received", "Connections", "Aborted_connects", "Innodb_buffer_pool_read_requests", "Opened_tables", "Threads_created", "Innodb_log_waits", "Table_open_cache_misses"}
var mysqlGauges = []string{"Threads_running", "Threads_connected", "Innodb_row_lock_current_waits", "Innodb_buffer_pool_pages_dirty", "Max_used_connections"}

type mysqlRaw struct {
	t    time.Time
	vals map[string]float64
}

func readMySQL(ctx context.Context) (mysqlRaw, error) {
	names := append(append([]string{}, mysqlCounters...), mysqlGauges...)
	q := "SHOW GLOBAL STATUS WHERE Variable_name IN ('" + strings.Join(names, "','") + "')"
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "mysql", mysqlArgs("-Nse", q)...).Output()
	if err != nil {
		return mysqlRaw{}, err
	}
	r := mysqlRaw{t: time.Now(), vals: map[string]float64{}}
	for _, line := range strings.Split(string(out), "\n") {
		fs := strings.Fields(line)
		if len(fs) == 2 {
			v, _ := strconv.ParseFloat(fs[1], 64)
			r.vals[fs[0]] = v
		}
	}
	return r, nil
}

func diffMySQL(a, b mysqlRaw) map[string]any {
	dt := b.t.Sub(a.t).Seconds()
	if dt <= 0 {
		dt = 1
	}
	m := map[string]any{"t": b.t.Format(time.RFC3339Nano)}
	for _, k := range mysqlCounters {
		m[strings.ToLower(k)+"_per_s"] = round1((b.vals[k] - a.vals[k]) / dt)
	}
	for _, k := range mysqlGauges {
		m[strings.ToLower(k)] = b.vals[k]
	}
	return m
}

func mysqlQuery(ctx context.Context, q string) string {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "mysql", mysqlArgs("-e", q)...).CombinedOutput()
	if err != nil {
		return "error: " + err.Error() + "\n" + string(out)
	}
	return string(out)
}

func mysqlProcesslist(ctx context.Context) string {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	out, _ := exec.CommandContext(ctx, "mysql", mysqlArgs("-e", "SELECT id, user, command, time, state, LEFT(info, 160) AS info FROM information_schema.processlist WHERE command <> 'Sleep' AND info IS NOT NULL ORDER BY time DESC LIMIT 30")...).Output()
	return string(out)
}

// ---- expvar: flatten numeric fields (memstats + anything the app exposes, e.g. db stats, goroutines) ----

func readExpvar(ctx context.Context, url string) (map[string]any, error) {
	ctx, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	var v map[string]any
	if err := json.NewDecoder(io.LimitReader(res.Body, 4<<20)).Decode(&v); err != nil {
		return nil, err
	}
	out := map[string]any{"t": time.Now().Format(time.RFC3339Nano)}
	var walk func(prefix string, x any, depth int)
	walk = func(prefix string, x any, depth int) {
		switch t := x.(type) {
		case float64:
			out[prefix] = t
		case bool:
			if t {
				out[prefix] = 1
			} else {
				out[prefix] = 0
			}
		case map[string]any:
			if depth >= 2 {
				return
			}
			for k, vv := range t {
				if prefix == "memstats" && !(k == "HeapAlloc" || k == "HeapInuse" || k == "NumGC" || k == "PauseTotalNs" || k == "Sys" || k == "Mallocs" || k == "NumGoroutine") {
					continue
				}
				p := k
				if prefix != "" {
					p = prefix + "." + k
				}
				walk(p, vv, depth+1)
			}
		}
	}
	for k, vv := range v {
		if k == "cmdline" {
			continue
		}
		walk(k, vv, 0)
	}
	return out, nil
}

// ---- nginx stub_status ----

func readStubStatus(ctx context.Context, url string) (map[string]any, error) {
	ctx, cancel := context.WithTimeout(ctx, 1000*time.Millisecond)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
	m := map[string]any{"t": time.Now().Format(time.RFC3339Nano)}
	for _, line := range strings.Split(string(b), "\n") {
		fs := strings.Fields(line)
		if len(fs) >= 2 && fs[0] == "Active" {
			m["active"], _ = strconv.Atoi(fs[2])
		}
		if len(fs) >= 6 && fs[0] == "Reading:" {
			m["reading"], _ = strconv.Atoi(fs[1])
			m["writing"], _ = strconv.Atoi(fs[3])
			m["waiting"], _ = strconv.Atoi(fs[5])
		}
	}
	return m, nil
}

// ---- pprof capture ----

func fetchToFile(ctx context.Context, url, path string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		b := make([]byte, 300)
		n, _ := io.ReadFull(res.Body, b)
		return &httpErr{code: res.StatusCode, body: strings.TrimSpace(string(b[:n]))}
	}
	return writeFileFromReader(path, res.Body)
}

// httpErr keeps the start of the body: pprof says why it refused ("Could not enable CPU profiling: cpu profiling
// already in use", "profile duration exceeds server's WriteTimeout").
type httpErr struct {
	code int
	body string
}

func (e *httpErr) Error() string {
	if e.body != "" {
		return "http " + strconv.Itoa(e.code) + ": " + strings.Join(strings.Fields(e.body), " ")
	}
	return "http " + strconv.Itoa(e.code)
}

// ---- systemd / git / env summaries for the status API ----

func unitStates(ctx context.Context, units []string) []map[string]string {
	var out []map[string]string
	for _, u := range units {
		ctx2, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
		act, _ := exec.CommandContext(ctx2, "systemctl", "is-active", u).Output()
		en, _ := exec.CommandContext(ctx2, "systemctl", "is-enabled", u).Output()
		cancel()
		out = append(out, map[string]string{"unit": u, "active": strings.TrimSpace(string(act)), "enabled": strings.TrimSpace(string(en))})
	}
	return out
}

// discoverUnits lists running units that look app-related (isu*, nginx, mysql, redis, ...).
func discoverUnits(ctx context.Context) []string {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "systemctl", "list-units", "--type=service", "--all", "--no-pager", "--no-legend", "--plain").Output()
	if err != nil {
		return nil
	}
	var units []string
	for _, line := range strings.Split(string(out), "\n") {
		fs := strings.Fields(line)
		if len(fs) == 0 {
			continue
		}
		n := fs[0]
		l := strings.ToLower(n)
		if strings.Contains(l, "isu") || strings.HasPrefix(l, "nginx") || strings.HasPrefix(l, "mysql") || strings.HasPrefix(l, "mariadb") || strings.HasPrefix(l, "postgresql") || strings.HasPrefix(l, "redis") || strings.HasPrefix(l, "memcached") || strings.HasPrefix(l, "pdns") || strings.Contains(l, "payment") || strings.Contains(l, "mock") {
			units = append(units, n)
		}
	}
	sort.Strings(units)
	return units
}

func gitInfo(ctx context.Context, repo string) map[string]any {
	if repo == "" {
		return nil
	}
	run := func(args ...string) string {
		ctx2, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		a := append([]string{"-c", "safe.directory=*", "-C", repo}, args...)
		out, _ := exec.CommandContext(ctx2, "git", a...).Output()
		return strings.TrimSpace(string(out))
	}
	dirty := run("status", "--porcelain")
	n := 0
	if dirty != "" {
		n = len(strings.Split(dirty, "\n"))
	}
	return map[string]any{"branch": run("rev-parse", "--abbrev-ref", "HEAD"), "commit": run("rev-parse", "--short", "HEAD"), "subject": run("log", "-1", "--format=%s"), "committed_at": run("log", "-1", "--format=%cI"), "dirty_files": n}
}

func envSummary(path string) map[string]string {
	m := map[string]string{}
	f, err := openFile(path)
	if err != nil {
		return m
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		kv := strings.SplitN(line, "=", 2)
		if len(kv) != 2 {
			continue
		}
		k := kv[0]
		v := strings.Trim(kv[1], `"'`)
		if strings.Contains(strings.ToUpper(k), "PASS") || strings.Contains(strings.ToUpper(k), "SECRET") || strings.Contains(strings.ToUpper(k), "TOKEN") {
			v = "***"
		}
		m[k] = v
	}
	return m
}

// rowCounts returns exact COUNT(*) per table of every non-system schema, as "db.table count" lines
// (run start = right after /initialize, run end = after the bench; the report shows the growth).
func rowCounts(ctx context.Context) string {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "mysql", mysqlArgs("-Nse", "SELECT CONCAT(table_schema,'.',table_name) FROM information_schema.tables WHERE table_type='BASE TABLE' AND table_schema NOT IN ('mysql','sys','information_schema','performance_schema') ORDER BY 1")...).Output()
	if err != nil {
		return ""
	}
	var b strings.Builder
	b.WriteString("# host=" + hostname() + " time=" + time.Now().Format(time.RFC3339) + "\n")
	for _, t := range strings.Fields(string(out)) {
		db, tbl, ok := strings.Cut(t, ".")
		if !ok {
			continue
		}
		c, err := exec.CommandContext(ctx, "mysql", mysqlArgs("-Nse", "SELECT COUNT(*) FROM `"+db+"`.`"+tbl+"`")...).Output()
		if err != nil {
			continue
		}
		b.WriteString(t + " " + strings.TrimSpace(string(c)) + "\n")
	}
	return b.String()
}

func hostname() string {
	h, _ := os.Hostname()
	return h
}
