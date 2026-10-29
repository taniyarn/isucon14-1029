package main

import (
	"strings"
	"testing"
)

func TestIndexSuggestionColumns(t *testing.T) {
	cols := schemaColumns("CREATE TABLE rides (\n  id VARCHAR(26) NOT NULL,\n  chair_id VARCHAR(26) NULL,\n  evaluation INT NULL DEFAULT NULL,\n  created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),\n  updated_at DATETIME(6) NOT NULL,\n  PRIMARY KEY (id)\n) ENGINE=InnoDB;\nCREATE TABLE IF NOT EXISTS `chairs` (\n `id` VARCHAR(26) NOT NULL,\n `is_active` TINYINT(1) NOT NULL,\n PRIMARY KEY (`id`)\n);\nCREATE TABLE ride_statuses (id VARCHAR(26) NOT NULL, ride_id VARCHAR(26) NOT NULL, status ENUM('A') NOT NULL);")
	if !cols["rides"]["updated_at"] || !cols["chairs"]["is_active"] || cols["rides"]["primary"] {
		t.Fatalf("schema parse: %v", cols)
	}
	cases := []struct{ q, table, want string }{
		{"SELECT * FROM rides r JOIN ride_statuses rs ON rs.ride_id = r.id WHERE r.chair_id = ? AND rs.status = ? ORDER BY r.updated_at DESC", "rides", "chair_id,updated_at"},
		{"SELECT c.id, cl.latitude FROM chairs c LEFT JOIN chair_location_summaries cl ON cl.chair_id = c.id WHERE c.is_active = TRUE AND c.id NOT IN ( SELECT r.chair_id FROM rides r JOIN ride_statuses rs ON rs.ride_id = r.id WHERE r.chair_id IS NOT NULL GROUP BY r.id HAVING COUNT(rs.chair_sent_at) < ? );", "chairs", "is_active"},
		{"SELECT r.*, (SELECT cp.discount FROM coupons cp WHERE cp.used_by = r.id LIMIT ?) AS d FROM rides r WHERE r.chair_id = ? ORDER BY r.created_at", "rides", "chair_id,created_at"},
		{"SELECT * FROM rides WHERE status = ? AND chair_id = ?", "rides", "chair_id"}, // status is not a rides column
	}
	for _, c := range cases {
		tb, got := indexSuggestion(c.q, cols)
		if tb != c.table || strings.Join(got, ",") != c.want {
			t.Errorf("%s\n  got %s(%v) want %s(%s)", c.q, tb, got, c.table, c.want)
		}
	}
	// unknown table: no column list to check against, keep what the query says
	if _, got := indexSuggestion("SELECT * FROM t WHERE a = ? ORDER BY b", cols); strings.Join(got, ",") != "a,b" {
		t.Errorf("unknown table: %v", got)
	}
}
