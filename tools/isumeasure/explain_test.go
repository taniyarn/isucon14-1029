package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExplainAttach(t *testing.T) {
	dir := t.TempDir()
	norm := "SELECT id, status, app_sent_at FROM ride_statuses WHERE ride_id = ? AND app_sent_at IS NULL ORDER BY created_at ASC LIMIT ?"
	txt := "=== " + norm + "\nid\tselect_type\ttable\tpartitions\ttype\tpossible_keys\tkey\tkey_len\tref\trows\tfiltered\tExtra\n1\tSIMPLE\tride_statuses\tNULL\tref\tidx_a\tidx_a\t106\tconst,const\t42\t100.00\tUsing index condition; Using filesort\n=== SELECT broken\nerror: exit status 1 ERROR 1064\n"
	os.WriteFile(filepath.Join(dir, "explain.txt"), []byte(txt), 0644)
	plans := loadExplain(dir)
	if len(plans) != 1 || plans[norm][0].key != "idx_a" || plans[norm][0].rows != "42" {
		t.Fatalf("plans: %+v", plans)
	}
	acts := []action{{title: "Query on ride_statuses is still slow with its index (5% of SQL time)", sql: norm}, {title: "Query on rides is still slow with its index", sql: "SELECT 1"}}
	attachExplain(acts, plans)
	if !strings.Contains(acts[0].detail, "key=idx_a rows=42") || !strings.Contains(acts[0].detail, "ORDER BY column") {
		t.Errorf("detail: %s", acts[0].detail)
	}
	if !strings.Contains(acts[1].detail, "No EXPLAIN") {
		t.Errorf("missing-plan note: %s", acts[1].detail)
	}
}
