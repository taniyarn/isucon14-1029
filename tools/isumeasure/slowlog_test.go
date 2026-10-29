package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func parseSlowSample(t *testing.T, body string) []slowEntry {
	t.Helper()
	p := filepath.Join(t.TempDir(), "slow.log")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	var got []slowEntry
	walkSlowLog(p, func(e slowEntry) { got = append(got, e) })
	return got
}

// MySQL 8: every entry opens with an RFC3339 "# Time:" line.
func TestWalkSlowLogMySQL(t *testing.T) {
	got := parseSlowSample(t, `# Time: 2026-10-28T14:52:10.123456Z
# User@Host: isucon[isucon] @ localhost [127.0.0.1]  Id:    12
# Query_time: 0.002000  Lock_time: 0.000010 Rows_sent: 1  Rows_examined: 100
SET timestamp=1793199130;
SELECT * FROM isu WHERE jia_isu_uuid = 'a';
# Time: 2026-10-28T14:52:10.223456Z
# User@Host: isucon[isucon] @ localhost [127.0.0.1]  Id:    13
# Query_time: 0.001000  Lock_time: 0.000000 Rows_sent: 0  Rows_examined: 0
SET timestamp=1793199130;
INSERT INTO isu_condition (a) VALUES (1);
`)
	if len(got) != 2 {
		t.Fatalf("entries = %d, want 2", len(got))
	}
	if want := time.Date(2026, 10, 28, 14, 52, 10, 123456000, time.UTC); !got[0].at.Equal(want) {
		t.Errorf("at = %v, want %v (the # Time: line, not SET timestamp)", got[0].at, want)
	}
	if got[0].examined != 100 || got[1].sql == got[0].sql {
		t.Errorf("unexpected entries: %+v", got)
	}
}

// MariaDB: "# Time:" (not RFC3339) only when the second changes; each entry opens with "# User@Host:".
func TestWalkSlowLogMariaDB(t *testing.T) {
	got := parseSlowSample(t, `# Time: 261029  0:09:38
# User@Host: isucon[isucon] @ localhost []
# Thread_id: 1911  Schema: isucondition  QC_hit: No
# Query_time: 0.000425  Lock_time: 0.000120  Rows_sent: 4  Rows_examined: 8
# Rows_affected: 0  Bytes_sent: 198
SET timestamp=1793199578;
SELECT * FROM isu WHERE jia_user_id = 'u1';
# User@Host: isucon[isucon] @ localhost []
# Thread_id: 1911  Schema: isucondition  QC_hit: No
# Query_time: 0.000000  Lock_time: 0.000000  Rows_sent: 0  Rows_examined: 0
# Rows_affected: 0  Bytes_sent: 0
SET timestamp=1793199578;
# administrator command: Quit;
# User@Host: isucon[isucon] @ localhost []
# Thread_id: 1912  Schema: isucondition  QC_hit: Yes
# Query_time: 0.000023  Lock_time: 0.000000  Rows_sent: 1  Rows_examined: 3
# Rows_affected: 0  Bytes_sent: 64
SET timestamp=1793199578;
SELECT COUNT(*) FROM isu_condition;
# Time: 261029  0:09:39
# User@Host: isucon[isucon] @ localhost []
# Thread_id: 1913  Schema: isucondition  QC_hit: No
# Query_time: 0.010000  Lock_time: 0.000000  Rows_sent: 0  Rows_examined: 0
# Rows_affected: 1  Bytes_sent: 11
SET timestamp=1793199579;
INSERT INTO isu_condition (a) VALUES (1);
`)
	if len(got) != 3 {
		t.Fatalf("entries = %d, want 3 (one per # User@Host:, Quit dropped): %+v", len(got), got)
	}
	if got[1].sql != "SELECT COUNT(*) FROM isu_condition;" || got[1].examined != 3 {
		t.Errorf("second entry = %+v", got[1])
	}
	if !got[0].at.Equal(time.Unix(1793199578, 0)) || !got[2].at.Equal(time.Unix(1793199579, 0)) {
		t.Errorf("at from SET timestamp: %v, %v", got[0].at, got[2].at)
	}
	if got[2].qt != 0.01 {
		t.Errorf("qt = %v", got[2].qt)
	}
}

// The agent slices the log mid-stream: a MariaDB slice may start at "# User@Host:" with no "# Time:" at all.
func TestWalkSlowLogMariaDBSliceWithoutTime(t *testing.T) {
	got := parseSlowSample(t, `# User@Host: root[root] @ localhost []
# Query_time: 0.000017  Lock_time: 0.000000  Rows_sent: 1  Rows_examined: 0
SET timestamp=1793199619;
SELECT 1;
# User@Host: root[root] @ localhost []
# Query_time: 0.000017  Lock_time: 0.000000  Rows_sent: 1  Rows_examined: 0
SET timestamp=1793199620;
SELECT 2;
`)
	if len(got) != 2 || got[0].sql != "SELECT 1;" || !got[1].at.Equal(time.Unix(1793199620, 0)) {
		t.Fatalf("got %+v", got)
	}
}
