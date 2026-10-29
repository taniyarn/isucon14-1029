// isumeasure: per-host measurement agent + report generator for ISUCON. Single static binary, no dependencies.
//
//	isumeasure agent   [flags]   run on a contest host (as root): samples /proc, MySQL, expvar; detects benches from the access log
//	isumeasure status  [-addr]   print the agent status (JSON) of a host
//	isumeasure report  <dir>     build report.md/report.html from a collected run directory (measurements/<ts>/)
package main

import (
	"fmt"
	"os"
)

const version = "0.3.2" // < 0.3.2: no end diagnostics (idle_sec, tail_reqs, final) and /stop ignores ?reason=bench-done; agents < 0.2.0 do not understand /start?follow=1 and /pause; < 0.3.0 do not tag their SQL, record columns, EXPLAIN or pprof failure reasons; < 0.3.1 ship slow logs with image blobs (literals > 256 bytes not collapsed)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "agent":
		err = runAgent(os.Args[2:])
	case "status":
		err = runStatus(os.Args[2:])
	case "report":
		err = runReport(os.Args[2:])
	case "panel":
		err = runPanel(os.Args[2:])
	case "export":
		err = runExport(os.Args[2:])
	case "flows":
		err = runFlows(os.Args[2:])
	case "groups":
		err = runGroups(os.Args[2:])
	case "retro":
		err = runRetro(os.Args[2:])
	case "version":
		fmt.Println("isumeasure", version)
	default:
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `usage:
  isumeasure agent  [-config FILE] [-listen 127.0.0.1:19100] [-data /var/lib/isumeasure] [-access /var/log/nginx/access.log] [-slow /var/log/mysql/slow.log] [-expvar http://127.0.0.1:6060/debug/vars] [-pprof http://127.0.0.1:6060] [-mysql] [-repo /home/isucon] [-units isuride-go,nginx,mysql]
  isumeasure status [-addr 127.0.0.1:19100]
  isumeasure report <measurements/<ts> dir> [-out DIR]
  isumeasure export <measurements/<ts> dir> [-out DIR] [-code DIR]   CSVs for DuckDB (deep-dive)
  isumeasure flows  <measurements/<ts> dir> [-depth 8] [-from "METHOD /path"] [-role COOKIE] [-host svc1]   Sankey of client flows (flows.html + flows.md)
  isumeasure groups -code webapp/go [-out alp.yml]        alp-compatible endpoint groups from the router (commit at the repo root)
  isumeasure retro  <data/<contest> dir> [-repo PATH] [-issues FILE]   retrospective material (retro.md/html)
  isumeasure panel  [-env /opt/isucon-ops/isucon.env] [-data /opt/isucon-ops/data] [-listen :8080] [-agent-base http://tunnel:1910]
  isumeasure version`)
}
