package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// columns.go: which columns each table really has, so an index suggestion never names a column of a JOINed table
// (2026-10-18: rides(chair_id, status, …) — status lives in ride_statuses). Source: the agent's db-catalog.txt
// "--- columns" section (information_schema.columns at the end of the run); fallback: CREATE TABLE statements in
// the app's SQL files (<code>/../sql/*.sql and similar), for runs from agents that did not collect columns.

func tableColumns(runDir, codeDir string) map[string]map[string]bool {
	out := catalogColumns(runDir)
	if len(out) > 0 {
		return out
	}
	if codeDir == "" {
		return out
	}
	root := filepath.Join(codeDir, "..")
	filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			rel, _ := filepath.Rel(root, p)
			if n := d.Name(); n == "node_modules" || n == ".git" || n == "vendor" || strings.Count(rel, string(filepath.Separator)) >= 3 {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(d.Name(), ".sql") {
			if st, err := d.Info(); err == nil && st.Size() < 2<<20 {
				if b, err := os.ReadFile(p); err == nil {
					for t, cs := range schemaColumns(string(b)) {
						if out[t] == nil {
							out[t] = map[string]bool{}
						}
						for c := range cs {
							out[t][c] = true
						}
					}
				}
			}
		}
		return nil
	})
	return out
}

// catalogColumns parses db-catalog.txt "--- columns" (table_name<TAB>column_name).
func catalogColumns(dir string) map[string]map[string]bool {
	out := map[string]map[string]bool{}
	b, err := os.ReadFile(filepath.Join(dir, "db-catalog.txt"))
	if err != nil {
		return out
	}
	in := false
	for _, ln := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(ln, "--- ") {
			in = strings.HasPrefix(ln, "--- columns")
			continue
		}
		if !in {
			continue
		}
		f := strings.Split(ln, "\t")
		if len(f) < 2 || strings.EqualFold(f[0], "table_name") {
			continue
		}
		t, c := strings.ToLower(strings.TrimSpace(f[0])), strings.ToLower(strings.TrimSpace(f[1]))
		if out[t] == nil {
			out[t] = map[string]bool{}
		}
		out[t][c] = true
	}
	return out
}

var createTableRe = regexp.MustCompile("(?is)CREATE\\s+TABLE\\s+(?:IF\\s+NOT\\s+EXISTS\\s+)?(?:`?\\w+`?\\.)?`?(\\w+)`?\\s*\\((.*?)\\)\\s*(?:ENGINE|;|DEFAULT\\s+CHAR|CHARSET|CHARACTER\\s+SET|COLLATE\\s*=)")
var colDefRe = regexp.MustCompile("^\\s*`?(\\w+)`?\\s+\\w")

func schemaColumns(sql string) map[string]map[string]bool {
	out := map[string]map[string]bool{}
	for _, m := range createTableRe.FindAllStringSubmatch(sql, -1) {
		t := strings.ToLower(m[1])
		if out[t] == nil {
			out[t] = map[string]bool{}
		}
		for _, ln := range strings.Split(m[2], "\n") {
			cm := colDefRe.FindStringSubmatch(ln)
			if cm == nil {
				continue
			}
			switch strings.ToUpper(cm[1]) {
			case "PRIMARY", "KEY", "INDEX", "UNIQUE", "CONSTRAINT", "FOREIGN", "FULLTEXT", "SPATIAL", "CHECK":
				continue
			}
			out[t][strings.ToLower(cm[1])] = true
		}
	}
	return out
}
