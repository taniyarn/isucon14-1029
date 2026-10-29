// Package sqltag prefixes every SQL statement with a comment naming the HTTP handler that issued it,
// e.g. `/* GET /api/app/notification */ SELECT ...`, so MySQL's slow log (and isumeasure report) can group
// queries by endpoint. Zero runtime cost beyond a string concat; only queries issued with a context carrying the tag are tagged.
//
// Integration (3 lines):
//
//	import "yourmodule/sqltag"           // copy this file into the webapp (e.g. webapp/go/sqltag/)
//	sqltag.Register("mysqltag", mysql.MySQLDriver{})   // once, before sql.Open / sqlx.Connect
//	db, err := sqlx.Connect("mysqltag", dsn)            // instead of "mysql"
//	mux.Use(sqltag.Middleware)                          // net/http middleware: tags by METHOD + path (route pattern via sqltag.Pattern)
//
// Router-specific route patterns (optional; the report normalizes ids in raw paths anyway):
//
//	chi:  sqltag.Pattern = func(r *http.Request) string { return chi.RouteContext(r.Context()).RoutePattern() }
//	echo: e.Use(func(next echo.HandlerFunc) echo.HandlerFunc { return func(c echo.Context) error {
//	          r := c.Request(); c.SetRequest(r.WithContext(sqltag.With(r.Context(), r.Method+" "+c.Path()))); return next(c) } })
//
// Handlers must pass the request context to the DB (GetContext/SelectContext/ExecContext/QueryContext/BeginTx).
// Inside goroutines/batch jobs use: ctx = sqltag.With(ctx, "matcher")
//
// The comment also carries the app's source line that issued the statement: `/* GET /api/x @ livecomment_handler.go:233 */`
// (first stack frame outside database/sql, sqlx and this package; resolved once per call site, then cached).
// The report shows it as the "code" column, so a normalized statement leads straight to the line to edit.
// Set Caller = false (or env SQLTAG_CALLER=0) to tag with the endpoint only.
package sqltag

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
)

// Caller adds " @ file.go:line" (the app frame that issued the statement) to the comment.
var Caller = os.Getenv("SQLTAG_CALLER") != "0"

// Pattern, when set, returns the route pattern for a request (e.g. "/api/users/:id"); nil = raw path.
var Pattern func(*http.Request) string

type ctxKey struct{}

// With returns a context whose SQL statements will be prefixed with the given tag.
func With(ctx context.Context, tag string) context.Context {
	return context.WithValue(ctx, ctxKey{}, tag)
}

// Tag returns the tag stored in ctx, or "".
func Tag(ctx context.Context) string {
	s, _ := ctx.Value(ctxKey{}).(string)
	return s
}

// Middleware tags the request context with "METHOD route-pattern" (raw path unless Pattern is set).
func Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if Pattern != nil {
			if p := Pattern(r); p != "" {
				path = p
			}
		}
		next.ServeHTTP(w, r.WithContext(With(r.Context(), r.Method+" "+path)))
	})
}

// Register wraps base (e.g. mysql.MySQLDriver{}) and registers it under name.
func Register(name string, base driver.Driver) { sql.Register(name, &tagDriver{base: base}) }

type tagDriver struct{ base driver.Driver }

func (d *tagDriver) Open(dsn string) (driver.Conn, error) {
	c, err := d.base.Open(dsn)
	if err != nil {
		return nil, err
	}
	return &tagConn{Conn: c}, nil
}

// OpenConnector lets database/sql use the driver's connector when available (keeps DSN parsing in the base driver).
func (d *tagDriver) OpenConnector(dsn string) (driver.Connector, error) {
	if dc, ok := d.base.(driver.DriverContext); ok {
		c, err := dc.OpenConnector(dsn)
		if err != nil {
			return nil, err
		}
		return &tagConnector{Connector: c, d: d}, nil
	}
	return &dsnConnector{dsn: dsn, d: d}, nil
}

type tagConnector struct {
	driver.Connector
	d *tagDriver
}

func (c *tagConnector) Connect(ctx context.Context) (driver.Conn, error) {
	conn, err := c.Connector.Connect(ctx)
	if err != nil {
		return nil, err
	}
	return &tagConn{Conn: conn}, nil
}
func (c *tagConnector) Driver() driver.Driver { return c.d }

type dsnConnector struct {
	dsn string
	d   *tagDriver
}

func (c *dsnConnector) Connect(context.Context) (driver.Conn, error) { return c.d.Open(c.dsn) }
func (c *dsnConnector) Driver() driver.Driver                        { return c.d }

type tagConn struct{ driver.Conn }

func tagged(ctx context.Context, query string) string {
	if strings.HasPrefix(query, "/*") {
		return query
	}
	t := Tag(ctx)
	if Caller {
		if loc := callerLoc(); loc != "" {
			t += " @ " + loc
		}
	}
	if t == "" {
		return query
	}
	return "/* " + strings.ReplaceAll(strings.TrimSpace(t), "*/", "") + " */ " + query
}

// frameInfo caches, per program counter, whether the frame belongs to the DB plumbing and its "file.go:line".
type frameInfo struct {
	skip bool
	loc  string
}

var frameCache sync.Map // uintptr -> frameInfo

var skipPrefixes = []string{"database/sql", "github.com/jmoiron/sqlx", "runtime.", "context.", sqltagPkg}

var sqltagPkg = func() string {
	pc, _, _, _ := runtime.Caller(0)
	name := runtime.FuncForPC(pc).Name() // ".../sqltag.init.func1" or similar
	if i := strings.LastIndex(name, "/"); i >= 0 {
		if j := strings.Index(name[i:], "."); j >= 0 {
			return name[:i+j+1]
		}
	}
	return "sqltag."
}()

// callerLoc returns "file.go:line" of the first stack frame outside database/sql, sqlx and this package.
func callerLoc() string {
	var pcs [32]uintptr
	n := runtime.Callers(3, pcs[:])
	for _, pc := range pcs[:n] {
		var fi frameInfo
		if v, ok := frameCache.Load(pc); ok {
			fi = v.(frameInfo)
		} else {
			fr, _ := runtime.CallersFrames([]uintptr{pc}).Next()
			fi.skip = fr.Function == ""
			for _, p := range skipPrefixes {
				if strings.HasPrefix(fr.Function, p) {
					fi.skip = true
					break
				}
			}
			if !fi.skip {
				fi.loc = filepath.Base(fr.File) + ":" + strconv.Itoa(fr.Line)
			}
			frameCache.Store(pc, fi)
		}
		if !fi.skip {
			return fi.loc
		}
	}
	return ""
}

func (c *tagConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	if q, ok := c.Conn.(driver.QueryerContext); ok {
		return q.QueryContext(ctx, tagged(ctx, query), args)
	}
	return nil, driver.ErrSkip
}

func (c *tagConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	if e, ok := c.Conn.(driver.ExecerContext); ok {
		return e.ExecContext(ctx, tagged(ctx, query), args)
	}
	return nil, driver.ErrSkip
}

func (c *tagConn) PrepareContext(ctx context.Context, query string) (driver.Stmt, error) {
	if p, ok := c.Conn.(driver.ConnPrepareContext); ok {
		return p.PrepareContext(ctx, tagged(ctx, query))
	}
	return c.Conn.Prepare(tagged(ctx, query))
}

func (c *tagConn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	if b, ok := c.Conn.(driver.ConnBeginTx); ok {
		return b.BeginTx(ctx, opts)
	}
	return c.Conn.Begin()
}

// pass-throughs so database/sql keeps using the optimized interfaces of the base driver
func (c *tagConn) Ping(ctx context.Context) error {
	if p, ok := c.Conn.(driver.Pinger); ok {
		return p.Ping(ctx)
	}
	return nil
}
func (c *tagConn) ResetSession(ctx context.Context) error {
	if r, ok := c.Conn.(driver.SessionResetter); ok {
		return r.ResetSession(ctx)
	}
	return nil
}
func (c *tagConn) IsValid() bool {
	if v, ok := c.Conn.(driver.Validator); ok {
		return v.IsValid()
	}
	return true
}
func (c *tagConn) CheckNamedValue(nv *driver.NamedValue) error {
	if n, ok := c.Conn.(driver.NamedValueChecker); ok {
		return n.CheckNamedValue(nv)
	}
	return driver.ErrSkip
}
