package main

// measurement: everything for the measurement patch lives here (tools/sqltag/README.md).
// MEASURE=0 (systemd drop-in Environment=MEASURE=0) turns all of it off without removing code.

import (
	"expvar"
	"net/http"
	"net/http/pprof"
	"os"

	"github.com/go-sql-driver/mysql"
	"github.com/isucon/isucon14/webapp/go/sqltag"
)

// measurement: off when MEASURE=0.
var measureOn = os.Getenv("MEASURE") != "0"

// measurement: driver name used by sqlx.Connect/Open in main.go ("mysqltag" wraps go-sql-driver/mysql and
// prefixes every statement with /* METHOD path @ file.go:line */; "mysql" when MEASURE=0).
var dbDriverName = func() string {
	if !measureOn {
		return "mysql"
	}
	sqltag.Register("mysqltag", mysql.MySQLDriver{})
	return "mysqltag"
}()

// measurement: wraps the whole router so every request ctx carries "METHOD /raw/path" (the report
// normalizes ids). Wrapping outside chi keeps setup() untouched; the chi route pattern is not known
// before routing anyway.
func measureHandler(h http.Handler) http.Handler {
	if !measureOn {
		return h
	}
	return sqltag.Middleware(h)
}

// measurement: X-Isu-Uid response header (nginx ltsv logs it as uid:$upstream_http_x_isu_uid).
func setUidHeader(w http.ResponseWriter, id string) {
	if measureOn {
		w.Header().Set("X-Isu-Uid", id)
	}
}

// measurement: expvar "db" (pool stats: WaitCount / WaitDuration) and pprof on 127.0.0.1:6060 only,
// on a dedicated mux (never the public router). Call after db is set.
func startMeasure() {
	if !measureOn {
		return
	}
	expvar.Publish("db", expvar.Func(func() any { return db.Stats() }))
	m := http.NewServeMux()
	m.Handle("/debug/vars", expvar.Handler())
	m.HandleFunc("/debug/pprof/", pprof.Index)
	m.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	m.HandleFunc("/debug/pprof/profile", pprof.Profile)
	m.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	m.HandleFunc("/debug/pprof/trace", pprof.Trace)
	_ = http.ListenAndServe("127.0.0.1:6060", m)
}
