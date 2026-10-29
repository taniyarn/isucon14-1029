package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// explain.go: at the end of a run the agent EXPLAINs the slowest normalized SELECTs of the run's slow log (one real
// example each) into explain.txt; the report shows key / rows / Extra in the SQL actions ("still slow with its
// index" was a generic range/LIKE/sort hint on 2026-10-18 where the plan would have said which).
//
// explain.txt:
//
//	=== <normalized sql>
//	id	select_type	table	partitions	type	possible_keys	key	key_len	ref	rows	filtered	Extra
//	1	SIMPLE	ride_statuses	NULL	ref	idx_…	idx_…	106	const	42	10.00	Using where; Using filesort

const explainTopN = 10

// collectExplain runs in the agent after the slow log slice is copied (best-effort; nothing when there is no SQL).
func collectExplain(ctx context.Context, dir string) bool {
	sa := newSlowAgg()
	walkSlowLog(filepath.Join(dir, "slow.log"), func(e slowEntry) {
		if !isAgentSQL(e.tag, e.sql) {
			sa.add(e)
		}
	})
	var cands []*sqlStat
	for _, s := range sa.finishAll() {
		if s.count >= 5 && strings.HasPrefix(strings.ToUpper(strings.TrimSpace(s.example)), "SELECT") && len(s.example) < 64<<10 {
			cands = append(cands, s)
		}
		if len(cands) >= explainTopN {
			break
		}
	}
	if len(cands) == 0 {
		return false
	}
	dbOf := schemaOfTables(ctx)
	var b strings.Builder
	for _, s := range cands {
		db := dbOf[strings.ToLower(firstTable(s.example))]
		args := []string{"--comments"}
		if db != "" {
			args = append(args, "-D", db)
		}
		args = append(args, "-e", agentSQL("EXPLAIN "+strings.TrimSuffix(strings.TrimSpace(s.example), ";")))
		c2, cancel := context.WithTimeout(ctx, 5*time.Second)
		out, err := exec.CommandContext(c2, "mysql", args...).CombinedOutput()
		cancel()
		fmt.Fprintf(&b, "=== %s\n", s.norm)
		if err != nil {
			fmt.Fprintf(&b, "error: %s %s\n", err, strings.TrimSpace(string(out)))
			continue
		}
		b.Write(out)
	}
	return os.WriteFile(filepath.Join(dir, "explain.txt"), []byte(b.String()), 0644) == nil
}

// schemaOfTables: table -> schema for the non-system schemas (the first one wins when two have the same table).
func schemaOfTables(ctx context.Context) map[string]string {
	out := map[string]string{}
	c2, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	b, err := exec.CommandContext(c2, "mysql", mysqlArgs("-Nse", "SELECT table_schema, table_name FROM information_schema.tables WHERE table_schema NOT IN ('mysql','sys','information_schema','performance_schema') ORDER BY table_schema")...).Output()
	if err != nil {
		return out
	}
	for _, ln := range strings.Split(string(b), "\n") {
		f := strings.Fields(ln)
		if len(f) == 2 {
			if _, ok := out[strings.ToLower(f[1])]; !ok {
				out[strings.ToLower(f[1])] = f[0]
			}
		}
	}
	return out
}

type explainRow struct {
	table, typ, key, rows, filtered, extra string
}

// loadExplain parses explain.txt into normalized sql -> plan rows ("error: …" plans are dropped).
func loadExplain(dir string) map[string][]explainRow {
	out := map[string][]explainRow{}
	b, err := os.ReadFile(filepath.Join(dir, "explain.txt"))
	if err != nil {
		return out
	}
	cur := ""
	var head []string
	for _, ln := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(ln, "=== ") {
			cur, head = strings.TrimPrefix(ln, "=== "), nil
			continue
		}
		if cur == "" || strings.TrimSpace(ln) == "" || strings.HasPrefix(ln, "error:") {
			continue
		}
		f := strings.Split(ln, "\t")
		if head == nil {
			head = f
			continue
		}
		col := func(name string) string {
			for i, h := range head {
				if strings.EqualFold(h, name) && i < len(f) {
					return f[i]
				}
			}
			return ""
		}
		out[cur] = append(out[cur], explainRow{table: col("table"), typ: col("type"), key: col("key"), rows: col("rows"), filtered: col("filtered"), extra: col("Extra")})
	}
	return out
}

func (r explainRow) String() string {
	s := fmt.Sprintf("%s: type=%s key=%s rows=%s", r.table, r.typ, r.key, r.rows)
	if r.filtered != "" && r.filtered != "100.00" && r.filtered != "NULL" {
		s += " filtered=" + r.filtered + "%"
	}
	if r.extra != "" && r.extra != "NULL" {
		s += " Extra=" + r.extra
	}
	return s
}

// explainNote: one sentence for an action ("" when the plan is unknown).
func explainNote(plans map[string][]explainRow, norm string) string {
	rows, ok := plans[norm]
	if !ok || len(rows) == 0 {
		return ""
	}
	var parts []string
	for _, r := range rows {
		parts = append(parts, r.String())
	}
	hint := ""
	all := strings.ToLower(strings.Join(parts, " "))
	switch {
	case strings.Contains(all, "type=all"):
		hint = " — a full scan: the index is not usable for this WHERE"
	case strings.Contains(all, "using filesort"):
		hint = " — the index finds the rows but not in ORDER BY order: append the ORDER BY column(s) to it"
	case strings.Contains(all, "using temporary"):
		hint = " — a temporary table (GROUP BY / DISTINCT): precompute or index the grouping columns"
	}
	return "EXPLAIN: " + strings.Join(parts, " | ") + hint + "."
}

// attachExplain adds the plan to SQL actions (index / still-slow / aggregate / N+1).
func attachExplain(acts []action, plans map[string][]explainRow) {
	keys := make([]string, 0, len(plans))
	for k := range plans {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for i := range acts {
		if acts[i].sql == "" {
			continue
		}
		if n := explainNote(plans, acts[i].sql); n != "" {
			acts[i].detail += " " + n
		} else if strings.Contains(acts[i].title, "still slow") {
			acts[i].detail += " (No EXPLAIN in this run — agents from this version collect it for the top slow SELECTs; meanwhile run EXPLAIN on the example in export/sql-examples.csv.)"
		}
	}
}
