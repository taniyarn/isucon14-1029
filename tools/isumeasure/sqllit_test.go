package main

import (
	"bytes"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// normSQLOld is normSQL before the literal shortening (the reference output).
func normSQLOld(q string) string {
	q = sqlStrRe.ReplaceAllString(q, "?")
	q = sqlNumRe.ReplaceAllString(q, "?")
	q = sqlValuesRe.ReplaceAllString(q, "VALUES (...)")
	q = sqlInRe.ReplaceAllString(q, "IN (...)")
	q = wsRe.ReplaceAllString(q, " ")
	return strings.TrimSpace(q)
}

func blob(n int) string {
	var b strings.Builder
	for i := 0; b.Len() < n; i++ {
		switch i % 7 {
		case 0:
			b.WriteString(`\'`) // escaped quote
		case 1:
			b.WriteString(`\\`) // escaped backslash
		case 2:
			b.WriteString(`\0`)
		case 3:
			b.WriteString("\xff\xd8\xff\xe0") // binary (invalid UTF-8)
		case 4:
			b.WriteString(`"`) // the other quote inside
		case 5:
			b.WriteString("12345")
		default:
			b.WriteString("JFIF\x01\x10")
		}
	}
	return b.String()
}

func TestNormSQLUnchanged(t *testing.T) {
	long := strings.Repeat("x", 300)
	qs := []string{
		"SELECT * FROM users WHERE id = 1",
		"SELECT * FROM users WHERE name = 'alice' AND age > 20.5",
		`SELECT * FROM t WHERE a = "dq" AND b = 'it\'s'`,
		"SELECT * FROM t WHERE id IN (1, 2, 3, 4) ORDER BY created_at DESC, id DESC LIMIT 10",
		"INSERT INTO t (a, b) VALUES (1, 'x'), (2, 'y'), (3, 'z')",
		"UPDATE t SET a = 'x''y' WHERE id = 7",
		"SELECT\n  a,\n  b\nFROM t\nWHERE c = 'multi\nline'",
		"INSERT INTO livecomments (user_id, livestream_id, comment, tip, created_at) VALUES (1, 2, '" + strings.Repeat("あ", 80) + "', 0, 1700000000)",
		"INSERT INTO t (a) VALUES ('" + strings.Repeat("ab", 128) + "')", // exactly 256 bytes: kept
		"INSERT INTO t (a, b) VALUES " + strings.Repeat("(1, 'short'), ", 100) + "(2, 'end')",
		"/* GET /api/x @ a.go:1 */ SELECT 1",
	}
	for _, q := range qs {
		if got, want := normSQL(q), normSQLOld(q); got != want {
			t.Errorf("normSQL changed\n q:    %.120q\n got:  %q\n want: %q", q, got, want)
		}
		if strings.Contains(q, long) {
			continue
		}
		if s := shortenSQLLiterals(q); s != q {
			t.Errorf("statement without long literals was rewritten: %.120q → %.120q", q, s)
		}
	}
	// long literals: same normalized form as before (the literal becomes ?)
	longs := []string{
		"INSERT INTO icons (user_id, image) VALUES (1, _binary'" + blob(80000) + "')",
		"INSERT INTO icons (user_id, image) VALUES (1, '" + blob(300) + "'), (2, '" + blob(5000) + "')",
		"UPDATE users SET bio = \"" + strings.Repeat("y", 1000) + "\" WHERE id = 3",
		"SELECT * FROM t WHERE a = '" + long + "' AND b IN (1,2)",
		// processlist info cut mid-literal (LEFT(info, 160) counts characters): the open tail stays as it was
		"INSERT INTO users (name, d) VALUES('" + long + "', '" + strings.Repeat("普段グラフィック", 30),
	}
	for _, q := range longs {
		if got, want := normSQL(q), normSQLOld(q); got != want {
			t.Errorf("long literal: normSQL differs\n got:  %q\n want: %q", got, want)
		}
	}
}

func TestShortenSQLLiterals(t *testing.T) {
	b := blob(80000)
	q := "INSERT INTO icons (user_id, image) VALUES (42, _binary'" + b + "');"
	got := shortenSQLLiterals(q)
	want := "INSERT INTO icons (user_id, image) VALUES (42, _binary'…(" + itoa(len(b)) + " bytes)');"
	if got != want {
		t.Fatalf("got %q\nwant %q", got, want)
	}
	// double quotes, and a single quote inside a double-quoted literal does not end it
	d := strings.Repeat("it's ", 100)
	if got := shortenSQLLiterals(`SELECT "` + d + `", 'ok' FROM t`); got != `SELECT "…(`+itoa(len(d))+` bytes)", 'ok' FROM t` {
		t.Errorf("double quote: %q", got)
	}
	// escaped backslash right before the closing quote ends the literal
	e := strings.Repeat("a", 300) + `\\`
	if got := shortenSQLLiterals(`SELECT '` + e + `', 7`); got != `SELECT '…(`+itoa(len(e))+` bytes)', 7` {
		t.Errorf("escaped backslash: %q", got)
	}
}

const slowSample = `# Time: 2026-10-20T16:17:52.912867Z
# User@Host: isucon[isucon] @ host [1.2.3.4]  Id:   229
# Query_time: 0.000032  Lock_time: 0.000000 Rows_sent: 0  Rows_examined: 0
SET timestamp=1792513072;
/* POST /api/icon @ user_handler.go:149 */ INSERT INTO icons (user_id, image) VALUES (1, _binary'BLOB');
# Time: 2026-10-20T16:17:52.919263Z
# User@Host: isucon[isucon] @ host [1.2.3.4]  Id:   229
# Query_time: 0.010652  Lock_time: 0.000001 Rows_sent: 1  Rows_examined: 8759
SET timestamp=1792513072;
SELECT * FROM users WHERE name = 'bob' AND id = 12;
`

// streaming in small chunks gives the same output as one write, and the headers/numbers survive
func TestLitShortenerStreaming(t *testing.T) {
	b := blob(100000)
	in := strings.Replace(slowSample, "BLOB", b, 1)
	want := strings.Replace(slowSample, "BLOB", "…("+itoa(len(b))+" bytes)", 1)
	for _, chunk := range []int{1, 3, 7, 4096, len(in)} {
		var out bytes.Buffer
		s := newLitShortener(&out, maxSQLLiteral)
		for i := 0; i < len(in); i += chunk {
			j := i + chunk
			if j > len(in) {
				j = len(in)
			}
			s.Write([]byte(in[i:j]))
		}
		s.Close()
		if out.String() != want {
			t.Fatalf("chunk %d: got\n%.600s\nwant\n%s", chunk, out.String(), want)
		}
	}
}

// an unbalanced quote must not swallow the next slow-log entry
func TestLitShortenerHeaderGuard(t *testing.T) {
	in := strings.Replace(slowSample, "_binary'BLOB');", "'"+strings.Repeat("z", 400)+"\n-- unterminated", 1)
	var out bytes.Buffer
	s := newLitShortener(&out, maxSQLLiteral)
	io.WriteString(s, in)
	s.Close()
	got := out.String()
	if !strings.Contains(got, "\n# Time: 2026-10-20T16:17:52.919263Z\n# User@Host:") ||
		!strings.Contains(got, "# Query_time: 0.010652  Lock_time: 0.000001 Rows_sent: 1  Rows_examined: 8759") ||
		!strings.Contains(got, "SELECT * FROM users WHERE name = 'bob' AND id = 12;") {
		t.Fatalf("header swallowed:\n%s", got)
	}
	// short unterminated literal: kept verbatim
	in2 := strings.Replace(slowSample, "_binary'BLOB');", "'abc", 1)
	out.Reset()
	s = newLitShortener(&out, maxSQLLiteral)
	io.WriteString(s, in2)
	s.Close()
	if out.String() != in2 {
		t.Fatalf("short unterminated literal changed:\n%s", out.String())
	}
}

// the report reads a slow log shipped with blobs (old agent) and one shipped shortened (new agent) the same way
func TestWalkSlowLogShortens(t *testing.T) {
	dir := t.TempDir()
	in := strings.Replace(slowSample, "BLOB", blob(200000), 1)
	f, _ := os.Create(filepath.Join(dir, "slow.log.gz"))
	gz := gzip.NewWriter(f)
	io.WriteString(gz, in)
	gz.Close()
	f.Close()
	var got []slowEntry
	walkSlowLog(filepath.Join(dir, "slow.log"), func(e slowEntry) { got = append(got, e) })
	if len(got) != 2 {
		t.Fatalf("entries: %d", len(got))
	}
	if got[0].tag != "POST /api/icon" || !strings.Contains(got[0].sql, "_binary'…(") || len(got[0].sql) > 200 {
		t.Errorf("blob entry: %+v", got[0])
	}
	if normSQL(got[0].sql) != "INSERT INTO icons (user_id, image) VALUES (...);" {
		t.Errorf("norm: %q", normSQL(got[0].sql))
	}
	if got[1].qt != 0.010652 || got[1].examined != 8759 || got[1].sql != "SELECT * FROM users WHERE name = 'bob' AND id = 12;" {
		t.Errorf("second entry: %+v", got[1])
	}
	// agent side: copyRangeGzSQL ships the shortened form
	raw := filepath.Join(dir, "raw.log")
	os.WriteFile(raw, []byte(in), 0644)
	dst := filepath.Join(dir, "out.log.gz")
	n, err := copyRangeGzSQL(raw, dst, 0, int64(len(in)))
	if err != nil || n != int64(len(in)) {
		t.Fatalf("copy: %d %v", n, err)
	}
	rc, _ := openMaybeGz(filepath.Join(dir, "out.log"))
	b, _ := io.ReadAll(rc)
	rc.Close()
	if strings.Contains(string(b), "JFIF") || !strings.Contains(string(b), "# Query_time: 0.000032") {
		t.Errorf("shipped slow log not shortened:\n%.400s", b)
	}
}

func TestIsAgentSQLLong(t *testing.T) {
	long := "INSERT INTO icons (user_id, image) VALUES (1, _binary'" + blob(5000) + "')"
	if isAgentSQL("", long) {
		t.Error("blob insert taken for the agent's")
	}
	if !isAgentSQL("", "SHOW GLOBAL STATUS WHERE Variable_name IN ("+strings.Repeat("'x',", 400)+"'y')") {
		t.Error("long SHOW STATUS not recognised")
	}
	if !isAgentSQL("", "SELECT id FROM information_schema.processlist WHERE info NOT IN ("+strings.Repeat("'x',", 400)+"'y')") {
		t.Error("long information_schema query not recognised")
	}
}
