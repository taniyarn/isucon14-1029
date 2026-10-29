package main

import (
	"path/filepath"
	"strings"
)

// actionGroup: the work unit for parallel implementation — indexes together; code actions by the Go file that owns
// the SQL/handler; the rest = ops.
func actionGroup(a action, ci *codeIndex) (group, file string) {
	group = "ops"
	switch a.kind {
	case "index":
		group = "indexes"
		if t := strings.TrimSpace(strings.TrimPrefix(strings.SplitN(a.title, "(", 2)[0], "Add an index on")); isPdnsTable(t) {
			group = "ops:pdns"
		}
	case "n1", "aggregate", "endpoint", "other", "internal", "polling", "slowidx", "hotspot":
		if ci != nil {
			var fns []string
			if a.sql != "" {
				fns = ci.funcsFor(a.sql)
				if len(fns) == 0 {
					fns = ci.funcsByFragment(a.sql)
				}
			} else if i := strings.Index(a.where, "()"); i > 0 {
				fns = []string{a.where[:i]}
			}
			for _, fn := range fns {
				if f := ci.fileOf[fn]; f != "" {
					file = f
					break
				}
			}
		}
		switch {
		case file != "":
			group = "file:" + filepath.Base(file)
		case a.kind == "endpoint":
			group = "endpoint:" + strings.TrimPrefix(strings.SplitN(a.title, " (", 2)[0], "Speed up ")
		case a.kind == "polling" || a.kind == "internal":
			group = "ops"
		default:
			group = "sql:unmapped"
		}
	}
	return group, file
}

// actionKey: two actions are the same work when group, title and query match (an index is the same index whatever
// query asked for it).
func actionKey(a action) string {
	if a.kind == "index" {
		return a.group + "\x00" + a.title
	}
	return a.group + "\x00" + a.title + "\x00" + a.sql
}

// dedupeActions keeps the first occurrence of each action across hosts (lists[i] belongs to names[i]) and records
// the other hosts on it, so actions.json / Next actions never list the same work twice.
func dedupeActions(lists [][]action, names []string) [][]action {
	type pos struct{ i, j int }
	seen := map[string]pos{}
	out := make([][]action, len(lists))
	for i, l := range lists {
		for _, a := range l {
			k := actionKey(a)
			if p, ok := seen[k]; ok {
				f := &out[p.i][p.j]
				if names[i] != names[p.i] && !contains(f.also, names[i]) {
					f.also = append(f.also, names[i])
				}
				continue
			}
			out[i] = append(out[i], a)
			seen[k] = pos{i, len(out[i]) - 1}
		}
	}
	return out
}

func contains(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}
