package main

// Tuning: fixed rules that turn the run's measurements into "which knob, current value, evidence, suggested value".
// No parameter search — a bench takes minutes and scores wobble ±5-10 %, so only knobs with a clear signal are
// listed, and the `tuning` skill applies them one per bench late in the day (15:00-15:45). Inputs per host:
// mysql.jsonl (per-second counters), sys.jsonl, app.jsonl (expvar memstats + db.Stats), nginx.jsonl (stub_status),
// db-catalog.txt (SHOW GLOBAL VARIABLES + table sizes), nginx-conf.txt, svc-env.txt, nginx-error.log, the CPU profile.

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

type knob struct {
	area, name, current, signal, suggest string
	level                                int    // 0 fine (not shown), 1 worth a look, 2 act
	when                                 string // "now" = a limit: fast-lane PR right after the run; "later" = interacts / small effect: 15:00 tuning
}

// knobWhen: fixed table. "now" = capacity limits — running short costs errors or queueing, raising them costs almost
// nothing, a one-line etc/ change that is easy to tell apart even when shipped with code changes. "later" = knobs that
// interact with each other or with the data size (their best value moves as the code changes) or whose effect is
// smaller than the bench noise (±5 %): one per bench in the 15:00 tuning window.
func knobWhen(name string) string {
	for _, p := range []string{"max_connections", "table_open_cache", "open_files_limit", "thread_cache_size", "worker_connections",
		"worker_rlimit_nofile", "net.core.somaxconn", "ip_local_port_range", "tcp_max_syn_backlog", "LimitNOFILE", "SetMaxOpenConns"} {
		if strings.HasPrefix(name, p) {
			return "now"
		}
	}
	return "later"
}

// firstNum: the leading number of a current-value string ("4000", "1024 (used 900)"); ok=false when there is none.
func firstNum(s string) (float64, bool) {
	m := strings.TrimSpace(numRe.FindString(s))
	if m == "" {
		return 0, false
	}
	f, err := strconv.ParseFloat(m, 64)
	return f, err == nil
}

var numRe = regexp.MustCompile(`^\s*[0-9]+(\.[0-9]+)?`)

func kvFile(path string) map[string]string { // "name\tvalue" lines (mysql CLI / systemctl show) → map
	out := map[string]string{}
	b, err := os.ReadFile(path)
	if err != nil {
		return out
	}
	for _, l := range strings.Split(string(b), "\n") {
		if i := strings.IndexAny(l, "\t="); i > 0 {
			out[strings.TrimSpace(l[:i])] = strings.TrimSpace(l[i+1:])
		}
	}
	return out
}

func winAvg(rows []map[string]any, w loadWindow, key string) (avg, max float64, n int) {
	for _, r := range rows {
		if !inWindow(w, str(r["t"])) {
			continue
		}
		v := num(r[key])
		avg += v
		if v > max {
			max = v
		}
		n++
	}
	if n > 0 {
		avg /= float64(n)
	}
	return
}

func winDelta(rows []map[string]any, w loadWindow, key string) (delta, secs float64) { // cumulative expvar counters
	var first, last map[string]any
	n := 0
	for _, r := range rows {
		if !inWindow(w, str(r["t"])) {
			continue
		}
		if first == nil {
			first = r
		}
		last = r
		n++
	}
	if n < 2 {
		return 0, 0
	}
	t0, _ := parseSec(str(first["t"]), w.fromT.Location())
	t1, _ := parseSec(str(last["t"]), w.fromT.Location())
	return num(last[key]) - num(first[key]), t1.Sub(t0).Seconds()
}

var pprofRowRe = regexp.MustCompile(`(?m)^\s*\S+\s+([\d.]+)%\s+[\d.]+%\s+\S+\s+([\d.]+)%\s+(\S+)`)

// gcShare: cum% of runtime.gcBgMarkWorker in `pprof -top` output (the GC's own CPU), 0 when unknown.
func gcShare(top string) float64 {
	for _, m := range pprofRowRe.FindAllStringSubmatch(top, -1) {
		if strings.HasSuffix(m[3], "runtime.gcBgMarkWorker") {
			v, _ := strconv.ParseFloat(m[2], 64)
			return v
		}
	}
	return 0
}

func tuningAdvice(h *hostRun, w loadWindow, procs []procAgg, pprofTopTxt string) []knob {
	var ks []knob
	add := func(area, name, current, signal, suggest string, level int) {
		ks = append(ks, knob{area, name, current, signal, suggest, level, knobWhen(name)})
	}
	// addLimit: a "raise it to target" knob. When the current value already reaches the target the knob is not the
	// fix (no ●): the signal comes from somewhere else, say so instead of recommending the value it already has.
	addLimit := func(area, name, current, signal, suggest string, level int, target float64, elsewhere string) {
		if cur, ok := firstNum(current); ok && target > 0 && cur >= target {
			add(area, name, current, signal, fmt.Sprintf("already ≥ %.0f (the suggested value): %s", target, elsewhere), 1)
			return
		}
		add(area, name, current, signal, suggest, level)
	}
	vars := kvFile(filepath.Join(h.Dir, "db-catalog.txt"))
	env := kvFile(filepath.Join(h.Dir, "svc-env.txt"))
	ngx := kvFile(filepath.Join(h.Dir, "nginx-conf.txt"))
	ngxErr, _ := os.ReadFile(filepath.Join(h.Dir, "nginx-error.log"))
	errCount := func(s string) int { return strings.Count(string(ngxErr), s) }
	mysqldHere, appHere, nginxHere := false, false, false
	for _, p := range procs {
		switch {
		case strings.HasPrefix(p.name, "mysqld") || strings.HasPrefix(p.name, "mariadbd"):
			mysqldHere = mysqldHere || p.avgPct > 5
		case strings.HasPrefix(p.name, "nginx"):
			nginxHere = nginxHere || p.avgPct > 1
		case strings.Contains(p.name, "isu") || strings.Contains(p.name, "app"):
			appHere = appHere || p.avgPct > 5
		}
	}

	// ---- MySQL (only where mysqld does work) ----
	if mysqldHere && len(h.MySQL) > 0 {
		reads, _, _ := winAvg(h.MySQL, w, "innodb_buffer_pool_reads_per_s")
		reqs, _, _ := winAvg(h.MySQL, w, "innodb_buffer_pool_read_requests_per_s")
		pool, _ := strconv.ParseFloat(vars["innodb_buffer_pool_size"], 64)
		poolMB := pool / 1048576
		dataMB := 0.0
		if b, err := os.ReadFile(filepath.Join(h.Dir, "db-catalog.txt")); err == nil { // table rows: schema, table, rows, data_mb, index_mb
			in := false
			for _, l := range strings.Split(string(b), "\n") {
				if strings.HasPrefix(l, "--- tables") {
					in = true
					continue
				}
				if strings.HasPrefix(l, "---") {
					in = false
				}
				if in {
					if fs := strings.Split(l, "\t"); len(fs) >= 5 {
						d, _ := strconv.ParseFloat(fs[3], 64)
						i, _ := strconv.ParseFloat(fs[4], 64)
						dataMB += d + i
					}
				}
			}
		}
		hit := 100.0
		if reqs > 0 {
			hit = 100 * (1 - reads/reqs)
		}
		cur := fmt.Sprintf("%.0f MB (data+index %.0f MB)", poolMB, dataMB)
		switch {
		case reqs > 0 && hit < 99 && dataMB > poolMB:
			add("mysql", "innodb_buffer_pool_size", cur, fmt.Sprintf("buffer pool hit %.1f%% (%.0f disk reads/s); data does not fit", hit, reads), fmt.Sprintf("DB-only host: 60%% of RAM (≥ %.0f MB to hold the data); restart mysql in a quiet minute", dataMB*1.2), 2)
		case reqs > 0 && hit < 99.5:
			add("mysql", "innodb_buffer_pool_size", cur, fmt.Sprintf("buffer pool hit %.1f%% (%.0f disk reads/s)", hit, reads), "raise if the host is DB-only (60% of RAM); otherwise leave", 1)
		}
		maxConn, _ := strconv.ParseFloat(vars["max_connections"], 64)
		_, usedMax, _ := winAvg(h.MySQL, w, "max_used_connections")
		_, connMax, _ := winAvg(h.MySQL, w, "threads_connected")
		if usedMax == 0 {
			usedMax = connMax
		}
		if maxConn > 0 && usedMax >= 0.8*maxConn {
			addLimit("mysql", "max_connections", fmt.Sprintf("%.0f (used %.0f)", maxConn, usedMax), "connections within 20% of the limit (Aborted_connects / 'Too many connections' follow)", "1024 in zz-perf.cnf (already there if deploy-init ran) — and check the app pool (below) is the thing growing", 2, 1024, "lower the app pools (SetMaxOpenConns × app hosts + pdns/workers must stay below it)")
		}
		tmpDisk, _, _ := winAvg(h.MySQL, w, "created_tmp_disk_tables_per_s")
		if tmpDisk >= 1 {
			add("mysql", "tmp_table_size / max_heap_table_size", vars["tmp_table_size"]+" / "+vars["max_heap_table_size"], fmt.Sprintf("%.1f on-disk temp tables/s (GROUP BY / ORDER BY with TEXT/BLOB spills)", tmpDisk), "64M–256M for both; better: fix the query (SELECT only needed columns, index for the ORDER BY)", lvlIf(tmpDisk >= 10, 2, 1))
		}
		sortMerge, _, _ := winAvg(h.MySQL, w, "sort_merge_passes_per_s")
		if sortMerge >= 1 {
			add("mysql", "sort_buffer_size", vars["sort_buffer_size"], fmt.Sprintf("%.1f sort merge passes/s", sortMerge), "2M–8M (per connection!); better: an index that matches the ORDER BY", 1)
		}
		opened, _, _ := winAvg(h.MySQL, w, "opened_tables_per_s")
		if opened >= 1 {
			addLimit("mysql", "table_open_cache", vars["table_open_cache"], fmt.Sprintf("%.1f tables opened/s (cache too small)", opened), "4000 (plus open_files_limit)", 2, 4000, "check open_files_limit (mysqld's LimitNOFILE) and table_definition_cache; temp tables also count as opened")
		}
		thrCreated, _, _ := winAvg(h.MySQL, w, "threads_created_per_s")
		if thrCreated >= 1 {
			addLimit("mysql", "thread_cache_size", vars["thread_cache_size"], fmt.Sprintf("%.1f threads created/s (connections not reused)", thrCreated), "256; and make the app keep a pool (SetMaxIdleConns = SetMaxOpenConns)", 2, 256, "the app opens new connections: SetMaxIdleConns = SetMaxOpenConns, SetConnMaxLifetime of minutes")
		}
		logWaits, _, _ := winAvg(h.MySQL, w, "innodb_log_waits_per_s")
		if logWaits > 0 {
			add("mysql", "innodb_log_file_size / innodb_log_buffer_size", vars["innodb_log_file_size"], fmt.Sprintf("%.1f redo log waits/s", logWaits), "innodb_log_file_size 512M–1G, innodb_log_buffer_size 64M (restart; needs a quiet minute)", 2)
		}
		fsyncs, _, _ := winAvg(h.MySQL, w, "innodb_data_fsyncs_per_s")
		if v := vars["innodb_flush_log_at_trx_commit"]; v != "" && v != "2" && v != "0" && fsyncs > 100 {
			add("mysql", "innodb_flush_log_at_trx_commit", v, fmt.Sprintf("%.0f fsyncs/s with per-commit flush", fsyncs), "2 (zz-perf.cnf; deploy-init installs it on the DB host — it is missing here)", 2)
		}
		thrRun, _, _ := winAvg(h.MySQL, w, "threads_running")
		if thrRun > 8 {
			add("mysql", "(app pool / concurrency)", fmt.Sprintf("Threads_running avg %.1f", thrRun), "more running threads than cores: the DB is queueing, extra connections only add contention", "lower the app's SetMaxOpenConns toward 2× cores; fix the top queries first", 1)
		}
	}

	// ---- Go app (expvar memstats + db.Stats) ----
	if appHere && len(h.App) > 0 {
		gcN, secs := winDelta(h.App, w, "memstats.NumGC")
		pauseNs, _ := winDelta(h.App, w, "memstats.PauseTotalNs")
		_, heapMax, _ := winAvg(h.App, w, "memstats.HeapAlloc")
		share := gcShare(pprofTopTxt)
		gogc := env["GOGC"]
		if gogc == "" {
			gogc = "100 (default)"
		}
		if secs > 0 && (share >= 10 || gcN/secs >= 20) {
			add("go", "GOGC", gogc, fmt.Sprintf("GC %.1f%% of CPU (pprof), %.0f GCs/s, pause %.0f ms total, heap peak %.0f MB", share, gcN/secs, pauseNs/1e6, heapMax/1048576), "GOGC=400 (Environment= in the unit drop-in) when heap peak × 4 fits in free RAM; or GOMEMLIMIT=<60% of RAM> with GOGC=off", 2)
		} else if secs > 0 && (share >= 5 || gcN/secs >= 10) {
			add("go", "GOGC", gogc, fmt.Sprintf("GC %.1f%% of CPU, %.0f GCs/s", share, gcN/secs), "GOGC=200 is the free-wins default; 400 if RAM allows", 1)
		}
		waits, _ := winDelta(h.App, w, "db.WaitCount")
		waitNs, _ := winDelta(h.App, w, "db.WaitDuration")
		_, maxOpen, _ := winAvg(h.App, w, "db.MaxOpenConnections")
		_, inUseMax, _ := winAvg(h.App, w, "db.InUse")
		if waits > 0 && secs > 0 {
			add("go", "SetMaxOpenConns / SetMaxIdleConns", fmt.Sprintf("%.0f (in use peak %.0f)", maxOpen, inUseMax), fmt.Sprintf("%.0f pool waits/s, %.0f ms waited in total", waits/secs, waitNs/1e6), "raise to 64–128 only if the DB host's Threads_running stays below ~2× its cores; otherwise the wait is the DB, not the pool", lvlIf(waits/secs >= 5, 2, 1))
		}
		if lim := env["LimitNOFILE"]; lim != "" && lim != "infinity" {
			if n, _ := strconv.Atoi(lim); n > 0 && n < 65536 {
				add("go", "LimitNOFILE", lim, "file-descriptor limit below 65536 on the app unit", "LimitNOFILE=65536 in the unit drop-in (deploy-init writes it)", 2)
			}
		}
	}

	// ---- nginx ----
	if nginxHere {
		wc, _ := strconv.Atoi(ngx["worker_connections"])
		_, activeMax, _ := winAvg(h.HTTP, w, "active")
		if n := errCount("worker_connections are not enough"); n > 0 || (wc > 0 && activeMax > 0.8*float64(wc)) {
			addLimit("nginx", "worker_connections", ngx["worker_connections"], fmt.Sprintf("%d 'worker_connections are not enough' in error.log, active peak %.0f", n, activeMax), "worker_connections 4096 + worker_rlimit_nofile 65536 (etc/nginx/nginx.conf)", 2, 4096, "check worker_rlimit_nofile and upstream keepalive (each client connection holds an upstream one)")
		}
		if n := errCount("Too many open files"); n > 0 {
			add("nginx", "worker_rlimit_nofile", ngx["worker_rlimit_nofile"], fmt.Sprintf("%d 'Too many open files'", n), "worker_rlimit_nofile 65536", 2)
		}
		upc, upn := 0.0, 0
		for _, ev := range h.Access {
			if i := strings.Index(ev.raw, "\tupconnect:"); i >= 0 {
				v := ev.raw[i+11:]
				if j := strings.IndexByte(v, '\t'); j >= 0 {
					v = v[:j]
				}
				if f, err := strconv.ParseFloat(v, 64); err == nil {
					upc += f
					upn++
				}
			}
		}
		_, twMax, _ := winAvg(h.Sys, w, "tcp_timewait")
		if upn > 0 && upc/float64(upn) > 0.0005 || twMax > 10000 {
			cur := ngx["keepalive"]
			if cur == "" {
				cur = "(no upstream keepalive)"
			}
			sig := fmt.Sprintf("TIME_WAIT peak %.0f", twMax)
			if upn > 0 {
				sig = fmt.Sprintf("avg upstream connect %.2f ms over %d requests, %s", 1000*upc/float64(upn), upn, sig)
			}
			add("nginx", "upstream keepalive", cur, sig, "upstream { keepalive 64; } + proxy_http_version 1.1 + proxy_set_header Connection \"\" (free-wins D does this)", lvlIf(twMax > 20000, 2, 1))
		}
		if n := errCount("upstream prematurely closed") + errCount("no live upstreams"); n > 0 {
			add("nginx", "upstream errors", "", fmt.Sprintf("%d upstream closed / no live upstreams in error.log", n), "app keepalive timeout ≥ nginx keepalive_timeout; check the app's restart count", 2)
		}
	}

	// ---- kernel ----
	lo, _, ln := winAvg(h.Sys, w, "listen_overflows_per_s")
	if ln > 0 && lo > 0 {
		add("kernel", "net.core.somaxconn / backlog", "", fmt.Sprintf("%.1f listen-queue overflows/s", lo), "net.core.somaxconn=4096, net.ipv4.tcp_max_syn_backlog=4096 (etc/sysctl.d), nginx listen ... backlog=4096", 2)
	}
	_, twMax, _ := winAvg(h.Sys, w, "tcp_timewait")
	_, rng, _ := winAvg(h.Sys, w, "port_range")
	if rng > 0 && twMax > 0.5*rng {
		add("kernel", "ip_local_port_range / tcp_tw_reuse", fmt.Sprintf("range %.0f, TIME_WAIT peak %.0f", rng, twMax), "ephemeral ports more than half used by TIME_WAIT", "net.ipv4.ip_local_port_range = 10000 65000, net.ipv4.tcp_tw_reuse = 1; and keepalive to the upstream so fewer connections are made", 2)
	}
	return ks
}

func max1(n int) int {
	if n < 1 {
		return 1
	}
	return n
}

func lvlIf(cond bool, a, b int) int {
	if cond {
		return a
	}
	return b
}
