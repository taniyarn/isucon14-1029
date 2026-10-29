package main

import (
	"strings"
	"testing"
)

func TestIsAgentSQL(t *testing.T) {
	agent := []string{
		"/* isumeasure */ SELECT 1",
		"SHOW GLOBAL STATUS WHERE Variable_name IN ('Questions')",
		"select @@version_comment limit 1",
		"SELECT id, user, command, time, state, LEFT(info, 160) AS info FROM information_schema.processlist WHERE command <> 'Sleep'",
		"SELECT CONCAT(table_schema,'.',table_name) FROM information_schema.tables WHERE table_type='BASE TABLE'",
		"SELECT COUNT(*) FROM `isuride`.`rides`;",
		"SHOW ENGINE INNODB STATUS",
		"FLUSH SLOW LOGS",
		"EXPLAIN SELECT * FROM rides WHERE id = 1",
	}
	app := []string{
		"SELECT COUNT(*) FROM rides WHERE chair_id = ?",
		"SELECT * FROM ride_statuses WHERE ride_id = ? ORDER BY created_at DESC LIMIT 1",
		"UPDATE rides SET evaluation = ? WHERE id = ?",
	}
	for _, q := range agent {
		tag, _, sql := splitTag(q)
		if !isAgentSQL(tag, sql) {
			t.Errorf("not recognised as the agent's: %s", q)
		}
	}
	for _, q := range app {
		tag, _, sql := splitTag("/* GET /api/x @ a.go:1 */ " + q)
		if isAgentSQL(tag, sql) {
			t.Errorf("app SQL taken for the agent's: %s", q)
		}
	}
}

// isAgentSQL (split regexps + prefix for long statements) answers like the single agentSQLRe on ordinary statements
func TestIsAgentSQLParity(t *testing.T) {
	qs := []string{
		"SHOW GLOBAL STATUS", "  show full processlist", "SELECT * FROM INFORMATION_SCHEMA.TABLES WHERE 1",
		"SELECT 1 FROM x JOIN information_schema.columns c", "SELECT information_schema_x FROM t",
		"SELECT COUNT(*) FROM `a`.`b`;", "SELECT COUNT(*) FROM `a`.`b` WHERE id = 1", "EXPLAIN SELECT 1",
		"SELECT * FROM users WHERE id = 1", "UPDATE t SET x = 'information_schema.tables'",
		"INSERT INTO t VALUES ('Information_Schema.Statistics')", "select @@version_comment limit 1",
	}
	for _, q := range qs {
		if got, want := isAgentSQL("", q), agentSQLRe.MatchString(strings.TrimSpace(q)); got != want {
			t.Errorf("%q: got %v want %v", q, got, want)
		}
	}
}
