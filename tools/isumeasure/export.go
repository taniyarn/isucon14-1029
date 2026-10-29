package main

import (
	"encoding/csv"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

func sortStrings(a []string) { sort.Strings(a) }

// export: flatten a collected run into CSVs that DuckDB (or anything) can query — the input for deep-dive analysis.
//
//	access.csv   one row per request (parsed ltsv)
//	slow.csv     one row per statement (normalized sql + tag + timings)
//	sql-examples.csv  normalized sql -> one raw example
//	timeline.csv 1-second merged series (http/sys/mysql/procs)
//	procs.csv    per-second per-process CPU
func runExport(args []string) error {
	fs := flag.NewFlagSet("export", flag.ContinueOnError)
	out := fs.String("out", "", "output dir (default: <dir>/export)")
	code := fs.String("code", envOr("CODE_DIR", ""), "app source dir: same router-based grouping as the report")
	groups := fs.String("groups", "", "alp.yml (default: next to / above -code, or ./alp.yml)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	var ci *codeIndex
	if *code != "" {
		ci = loadCodeIndex(*code)
	}
	gcfg, err := loadGroups(findGroupsFile(*groups, *code))
	if err != nil {
		return err
	}
	nz := newNormalizer(gcfg.groups, gcfg.src, ci) // the endpoint column matches the report's tables exactly
	if fs.NArg() < 1 {
		return fmt.Errorf("usage: isumeasure export <measurements/<ts> dir>")
	}
	dir := fs.Arg(0)
	if *out == "" {
		*out = filepath.Join(dir, "export")
	}
	var hosts []*hostRun
	if _, err := os.Stat(filepath.Join(dir, "run.json")); err == nil {
		h, err := loadHostRun(dir, "")
		if err != nil {
			return err
		}
		hosts = append(hosts, h)
	} else {
		entries, _ := os.ReadDir(dir)
		for _, e := range entries {
			if e.IsDir() && e.Name() != "export" {
				if h, err := loadHostRun(filepath.Join(dir, e.Name()), e.Name()); err == nil {
					hosts = append(hosts, h)
				}
			}
		}
	}
	if len(hosts) == 0 {
		return fmt.Errorf("no run.json under %s", dir)
	}
	os.MkdirAll(*out, 0755)
	w := func(name string, header []string, rows func(func([]string))) error {
		f, err := os.Create(filepath.Join(*out, name))
		if err != nil {
			return err
		}
		defer f.Close()
		cw := csv.NewWriter(f)
		cw.Write(header)
		rows(func(r []string) { cw.Write(r) })
		cw.Flush()
		return cw.Error()
	}
	f := func(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }
	t0 := hosts[0].Start
	for _, h := range hosts {
		if !h.Start.IsZero() && h.Start.Before(t0) {
			t0 = h.Start
		}
	}
	rel := func(t time.Time) string { return f(t.Sub(t0).Seconds()) }

	if err := w("access.csv", []string{"host", "ts", "t_rel_s", "method", "path", "uri", "status", "reqtime", "apptime", "size", "role", "sess", "ua", "conn", "connreq", "upstream", "static", "inm"}, func(emit func([]string)) {
		for _, h := range hosts {
			for _, ev := range h.Access {
				role, sess := "", ev.sess
				if i := strings.IndexByte(sess, '='); i >= 0 {
					role, sess = sess[:i], sess[i+1:]
				}
				conn, connreq, upstream, inm := "", "", "", ""
				for _, fld := range strings.Split(ev.raw, "\t") {
					switch {
					case strings.HasPrefix(fld, "conn:"):
						conn = fld[5:]
					case strings.HasPrefix(fld, "connreq:"):
						connreq = fld[8:]
					case strings.HasPrefix(fld, "upstream:"):
						upstream = fld[9:]
					case strings.HasPrefix(fld, "inm:"): // If-None-Match (conditional GET; needs the inm field in the ltsv format)
						if inm = fld[4:]; inm == "-" {
							inm = ""
						}
					}
				}
				emit([]string{h.Name, ev.at.UTC().Format(time.RFC3339Nano), rel(ev.at), ev.method, nz.normQuiet(ev.uri), ev.uri, strconv.Itoa(ev.status), f(ev.reqtime), f(ev.apptime), strconv.Itoa(ev.size), role, sess, ev.ua, conn, connreq, upstream, strconv.FormatBool(isStatic(ev.uri)), inm})
			}
		}
	}); err != nil {
		return err
	}
	examples := map[string]string{}
	if err := w("slow.csv", []string{"host", "ts", "t_rel_s", "query_time", "lock_time", "rows_sent", "rows_examined", "tag", "sql_norm", "loc"}, func(emit func([]string)) {
		for _, h := range hosts {
			walkSlowLog(filepath.Join(h.Dir, "slow.log"), func(e slowEntry) { // streamed: the entries are not held in memory
				if isAgentSQL(e.tag, e.sql) {
					return // the measurement agent's own statements (SHOW GLOBAL STATUS, processlist, row counts)
				}
				n := normSQL(e.sql)
				if _, ok := examples[n]; !ok {
					examples[n] = e.sql
				}
				emit([]string{h.Name, e.at.UTC().Format(time.RFC3339Nano), rel(e.at), f(e.qt), f(e.lt), strconv.Itoa(e.sent), strconv.Itoa(e.examined), e.tag, n, e.loc})
			})
		}
	}); err != nil {
		return err
	}
	if err := w("sql-examples.csv", []string{"sql_norm", "example"}, func(emit func([]string)) {
		for n, ex := range examples {
			if len(ex) > 2000 {
				ex = ex[:2000]
			}
			emit([]string{n, ex})
		}
	}); err != nil {
		return err
	}
	// timeline: 1s rows per host, merged by second
	sysKeys := []string{"cpu_user_pct", "cpu_sys_pct", "cpu_iowait_pct", "cpu_idle_pct", "load1", "mem_used_pct", "mem_avail_mb", "disk_read_mb_s", "disk_write_mb_s", "disk_util_pct", "net_rx_mb_s", "net_tx_mb_s", "tcp_inuse", "tcp_timewait", "ctxt_per_s", "procs_running"}
	myKeys := []string{"questions_per_s", "com_select_per_s", "com_insert_per_s", "com_update_per_s", "com_delete_per_s", "com_commit_per_s", "com_rollback_per_s", "threads_running", "threads_connected", "innodb_row_lock_waits_per_s", "innodb_row_lock_time_per_s", "innodb_row_lock_current_waits", "select_scan_per_s", "select_full_join_per_s", "created_tmp_disk_tables_per_s", "innodb_data_fsyncs_per_s", "handler_read_rnd_next_per_s", "slow_queries_per_s", "connections_per_s", "bytes_sent_per_s", "innodb_buffer_pool_reads_per_s"}
	httpKeys := []string{"req", "err_5xx", "err_4xx", "reqtime_sum", "reqtime_max", "reqtime_p99"}
	header := []string{"host", "ts", "t_rel_s"}
	header = append(header, httpKeys...)
	header = append(header, sysKeys...)
	header = append(header, myKeys...)
	sec := func(s string, loc *time.Location) string { // UTC second key; a zone-less http "t" is host-local (parseSec)
		if t, err := parseSec(s, loc); err == nil {
			return t.UTC().Format("2006-01-02T15:04:05")
		}
		return s
	}
	if err := w("timeline.csv", header, func(emit func([]string)) {
		for _, h := range hosts {
			rows := map[string]map[string]any{}
			get := func(k string) map[string]any {
				r := rows[k]
				if r == nil {
					r = map[string]any{}
					rows[k] = r
				}
				return r
			}
			for _, m := range h.HTTP {
				r := get(sec(str(m["t"]), h.Start.Location()))
				for _, k := range httpKeys {
					r[k] = m[k]
				}
			}
			for _, m := range h.Sys {
				r := get(sec(str(m["t"]), h.Start.Location()))
				for _, k := range sysKeys {
					r[k] = m[k]
				}
			}
			for _, m := range h.MySQL {
				r := get(sec(str(m["t"]), h.Start.Location()))
				for _, k := range myKeys {
					r[k] = m[k]
				}
			}
			keys := make([]string, 0, len(rows))
			for k := range rows {
				keys = append(keys, k)
			}
			sortStrings(keys)
			for _, k := range keys {
				t, _ := time.Parse("2006-01-02T15:04:05", k)
				row := []string{h.Name, k + "Z", rel(t)}
				for _, col := range header[3:] {
					if v, ok := rows[k][col]; ok {
						row = append(row, f(num(v)))
					} else {
						row = append(row, "")
					}
				}
				emit(row)
			}
		}
	}); err != nil {
		return err
	}
	if err := w("procs.csv", []string{"host", "ts", "t_rel_s", "name", "pid", "cpu_pct", "rss_mb"}, func(emit func([]string)) {
		for _, h := range hosts {
			for _, m := range h.Procs {
				t, _ := time.Parse(time.RFC3339Nano, str(m["t"]))
				list, _ := m["procs"].([]any)
				for _, x := range list {
					pm, _ := x.(map[string]any)
					emit([]string{h.Name, t.UTC().Format(time.RFC3339Nano), rel(t), str(pm["name"]), f(num(pm["pid"])), f(num(pm["cpu_pct"])), f(num(pm["rss_mb"]))})
				}
			}
		}
	}); err != nil {
		return err
	}
	fmt.Printf("exported to %s: access.csv slow.csv sql-examples.csv timeline.csv procs.csv (t0=%s)\n", *out, t0.UTC().Format(time.RFC3339))
	return nil
}
