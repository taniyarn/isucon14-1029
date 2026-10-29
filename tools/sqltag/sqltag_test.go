package sqltag_test

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"io"
	"strings"
	"testing"

	"sqltag"
)

// fake driver that records the last statement text it received
type fakeDriver struct{}
type fakeConn struct{}
type fakeRows struct{}

var last string

func (fakeDriver) Open(string) (driver.Conn, error)  { return fakeConn{}, nil }
func (fakeConn) Prepare(string) (driver.Stmt, error) { return nil, driver.ErrSkip }
func (fakeConn) Close() error                        { return nil }
func (fakeConn) Begin() (driver.Tx, error)           { return nil, driver.ErrSkip }
func (fakeRows) Columns() []string                   { return []string{"x"} }
func (fakeRows) Close() error                        { return nil }
func (fakeRows) Next([]driver.Value) error           { return io.EOF }
func (fakeConn) QueryContext(_ context.Context, q string, _ []driver.NamedValue) (driver.Rows, error) {
	last = q
	return fakeRows{}, nil
}
func (fakeConn) ExecContext(_ context.Context, q string, _ []driver.NamedValue) (driver.Result, error) {
	last = q
	return driver.RowsAffected(0), nil
}

func init() { sqltag.Register("fake", fakeDriver{}) }

func TestCallerLocation(t *testing.T) {
	db, _ := sql.Open("fake", "")
	ctx := sqltag.With(context.Background(), "GET /api/x")
	rows, err := db.QueryContext(ctx, "SELECT 1") // this line must appear in the tag
	if err != nil {
		t.Fatal(err)
	}
	rows.Close()
	if !strings.HasPrefix(last, "/* GET /api/x @ sqltag_test.go:") || !strings.HasSuffix(last, " */ SELECT 1") {
		t.Fatalf("got %q", last)
	}
	db.ExecContext(context.Background(), "DELETE FROM t") // no endpoint tag: location only
	if !strings.HasPrefix(last, "/* @ sqltag_test.go:") {
		t.Fatalf("untagged: got %q", last)
	}
	sqltag.Caller = false
	db.ExecContext(context.Background(), "DELETE FROM t")
	if last != "DELETE FROM t" {
		t.Fatalf("Caller=false: got %q", last)
	}
	sqltag.Caller = true
}

func BenchmarkTaggedExec(b *testing.B) {
	db, _ := sql.Open("fake", "")
	ctx := sqltag.With(context.Background(), "GET /api/x")
	for i := 0; i < b.N; i++ {
		db.ExecContext(ctx, "DELETE FROM t")
	}
}
