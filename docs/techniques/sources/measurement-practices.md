# Catalogue: measurement/analysis ideas we do not have yet (ISUCON strong-team practice) — research agent output, 2026-10-17

Legend: effort S/M/L. "Evidence" lists only pages the agent actually opened (WebFetch or `gh api`); items based only on search-result snippets are marked "(search snippet; page not opened)". Anything not confirmed is "(unverified)".

---

## (1) Bench / user-behaviour analysis

### M1: Per-user identity in the access log (app → response header → nginx)
- What: the app's auth middleware sets `X-Isu-UserId` (or a pseudonymous session id) on the response; nginx logs it via `$upstream_http_x_isu_userid`. Enables per-user request sequences instead of per-client-IP/port heuristics.
- Why: Mirrativ (ISUCON11 final) did exactly this to reconstruct bench behaviour; NaruseJun (ISUCON13 winner) said the per-user access logs they produced for a Sankey diagram "シナリオを推定するために大分参考になった — ログを見たあとにマニュアルを読むと、この文章はこの意図かと分かる". EkeMinusYou's kit returns `X-Measurement-Session-ID` (a pseudonym, not the raw cookie) and logs it as `session_id`.
- How: Echo: `c.Response().Header().Set("X-Isu-UserId", userID)`; nginx LTSV add `\tuid:$upstream_http_x_isu_userid`; app can `Header().Del` for public safety or use an HMAC of the session cookie (isutools does HMAC(cookie)).
- Fit: access-log report → feeds existing session analysis with a true user key instead of client port; S.
- Evidence: https://tech.mirrativ.stream/entry/2021/10/29/152334 (Mirrativ, ISUCON11); https://zenn.dev/tohutohu/articles/923bdf5dcd73af (NaruseJun, ISUCON13); EkeMinusYou/isucon14-02 docs/measurement/nginx-access-log.md (gh).

### M2: Role-aware scenario graph (edges between endpoints per user, with gap stats)
- What: counter `flow_total{source_method,source_path,target_method,target_path}` built from consecutive requests of the same cookie identity, plus per-edge `start_gap_p50/p95_ms`, `idle_gap_avg_ms`, status split of from/to, `overlap_transitions` (next request started before previous finished = client concurrency), `ambiguous_order_transitions`.
- Why: mazrean (traP) visualised it as a Grafana Node Graph and read off 3 user patterns in ISUCON13 (配信検索 / 配信視聴 / 配信者), which told him which loops matter; the ISUCON11-final organisers state that scored requests (課題提出, お知らせ一覧) sit behind serial teacher→student dependencies, so the role transitions are what you must find.
- How: our n-gram/transition table already exists; add (a) per-edge timing percentiles, (b) overlap count (`t_start(next) < t_end(prev)`) to estimate per-user client parallelism, (c) grouping sessions by signature into "scenario cohorts" with `sessions, requests_per_session_avg, duration_p50/p95`.
- Fit: session analysis report; M.
- Evidence: https://trap.jp/post/2046/ and https://trap.jp/post/2434/ (mazrean); mazrean/isucon-go-tools http/prometheus.go `flow_total` (gh); EkeMinusYou tools/analysis/schema/user-transitions.sql (gh); https://isucon.net/archives/56163308.html (ISUCON11 final 講評).

### M3: Scored-action rate as a first-class run metric (score ÷ scored-event count, and events/sec over time)
- What: identify the endpoint(s) whose 2xx count is the score driver (e.g. ISUCON14 `POST /api/app/rides/{id}/evaluation`, ISUCON13 tip posts, ISUCON11-final 課題提出) and track per run: 2xx count, score/count ratio, and 1-second rate over the run (ramp shape), plus "final users / active users / chairs" parsed from bench output.
- Why: EkeMinusYou's score-structure doc tabulates 55 runs as `score | PASS | evaluation-2xx | final users (registered/active) | chairs`; the ratio exposed that the completion bonus is only ~7.6–7.9% and that distance dominates, so "waiting time" optimisations were de-prioritised. ISUCON14 講評 confirms the formula (迎車距離×0.1 + 乗車距離 + 完了×5).
- How: we have score ÷ key-POST; add per-second rate of the scored action and parse the bench stdout tail (users/chairs/region lines) into the run record.
- Fit: run timeline + access-log report; S.
- Evidence: EkeMinusYou docs/analysis/benchmark-score-structure.md (gh); https://isucon.net/archives/58869617.html (ISUCON14 講評).

### M4: Bench-output semantic timeline (warnings/errors with timestamps, cumulative error counts)
- What: parse the benchmarker's stdout into a table of `(t, level, message, count)`, keep per-run cumulative error counts and the "load level up / users didn't increase" lines.
- Why: fukkatsuso (ISUCON14) lists "ベンチマーカーのログを十分に見なかった (there were optimisation hints there)" as a top regret; catatsuy (ISUCON12) finished with 0 points because he never read the error log in the last 10 min (search snippet); ISUCON7/8 benchmarkers print "負荷レベルが上昇しました / レスポンスが遅いため上げられませんでした", ISUCON11 prints "ユーザーが増えませんでした" (search snippets). EkeMinusYou's DuckDB has `bench_errors` and a dashboard panel "cumulative error timeline".
- How: `bench.log` → regex for level/time; group identical messages (ISUCON9 manual: identical messages are collapsed, so count ≠ errors); show on the run timeline next to score.
- Fit: panel (run detail); S.
- Evidence: https://blog.fukkatsuso.com/posts/isucon14/; EkeMinusYou tools/analysis/schema/bench-errors.sql (gh); https://sfujiwara.hatenablog.com/entry/2018/10/25/084543 (load-level mechanics).

### M5: Load-growth model estimation from the log (users-over-time, parallelism per worker, ramp checkpoints)
- What: estimate the benchmarker's parallelism model: number of distinct identities per second, new identities per 10 s window, and check whether growth stalls after errors/timeouts.
- Why: ISUCON12-final bench raised worker parallelism every 10 s based on login/registration success counts and error deltas (helloyuki); ISUCON8 bench added users every ~1–5 s if no timeouts, and a 10 s timeout removes a user (fujiwara); ISUCON14 adds users via satisfaction and region word-of-mouth. Seeing "distinct users/sec" plateau tells you whether you are capped by the bench model or by errors.
- How: from M1's user key: `distinct uid per 1s`, `first-seen uid per 10s`; overlay with 5xx/499/timeouts per second.
- Fit: session analysis + per-second timeline; S–M.
- Evidence: https://zenn.dev/helloyuki/articles/cc257eb2592d5a; https://sfujiwara.hatenablog.com/entry/2018/10/25/084543; https://isucon.net/archives/58869617.html.

### M6: Polling/notification cadence vs `retry_after_ms` analysis
- What: for polling endpoints, measure inter-arrival per user (p50) and compare to the server-returned interval; count requests saved per +Xms.
- Why: ISUCON14 bench ticks at 30 ms and honours `retry_after_ms`; takonomura's commit log shows 30→500→1000 ms steps and later SSE, each a scoring decision; 講評 notes reducing it hurts satisfaction latency. Our polling detector exists but not the "server-controlled interval vs observed" comparison.
- How: per-user gap histogram for `GET /api/app/notification` etc.; from the app, log the `retry_after_ms` value emitted.
- Fit: session analysis deep-dive; S.
- Evidence: takonomura/isucon14 commit log (gh); https://isucon.net/archives/58869617.html.

### M7: Connection-reuse behaviour of the bench client (new connections per route, keepalive idle gaps)
- What: nginx `$connection` + `$connection_requests` in the log → per route: share of requests on a fresh connection, idle gap before reuse, TLS handshakes/sec.
- Why: EkeMinusYou derived (from `session_id` + connection number) that the ISUCON14 client only reuses an idle connection if the gap is <~10 s and that ending SSE at ARRIVED added +63% notification requests — a decision about where to close SSE. kawaemon hit ephemeral-port saturation (~65,535) with kernel time spiking.
- How: `log_format ... \tconn:$connection\tconnreq:$connection_requests\tssl_reused:$ssl_session_reused`; aggregate `connreq==1` ratio per route.
- Fit: access-log report; S.
- Evidence: EkeMinusYou docs/analysis/benchmarker-behavior.md (gh); https://zenn.dev/kawaemon/articles/529fd6a12ac43b.

### M8: "Hidden rate-limiter" detector: scored-action throughput vs endpoint stats disagreement
- What: flag when the scored-action rate is flat while top alp endpoints improve; look for serialisation points (flock, single-goroutine matcher, 500 ms matcher tick) via lock/wait metrics rather than latency.
- Why: ISUCON11-final organisers deliberately made the limiter invisible to alp ("CPU使用率の高い処理・累計レスポンスタイムの大きいエンドポイントとして統計に出ない部分が律速"); 白金動物園 (ISUCON13) missed that DNS record creation serialised by `flock` was the bottleneck because "it wasn't particularly slow" in the access log; ISUCON14 initial matcher handled 1 ride/500 ms.
- How: couple M3 with M18/M19 (lock waits, goroutine blocking); add a "serial sections" checklist from code (mutex, flock, single ticker loops) to the deep-dive.
- Fit: deep-dive query + report hint; M.
- Evidence: https://isucon.net/archives/56163308.html; https://blog.mirakui.com/entry/2023/11/27/152107; https://isucon.net/archives/58869617.html.

### M9: Scenario funnel with conversion/drop-off/retry per step (explicit scenario definitions)
- What: define expected scenarios (YAML: steps = route list); compute per step conversion %, drop-off, retries, p95, 4xx/5xx; diff between runs.
- Why: isutools' "Scenario Stories / Funnel" (49 sessions went `POST /api/icon → GET /api/tag → POST /api/livestream/reservation`) and traP's scenario analysis were both built because aggregate endpoint tables hide which step stalls users.
- How: our n-gram/drop-off tables + a funnel config; output conversion per step.
- Fit: session analysis; M.
- Evidence: ekusiadadus/isutools README (gh); https://trap.jp/post/2434/.

### M10: Bench-behaviour hypothesis log (dated, confidence-tagged, with the run IDs/queries as evidence)
- What: a running `benchmarker-behavior.md`: one heading per observed behaviour; lines `YYYY-MM-DD HH:MM 仮説/有力/確定: … 根拠: RUN, value, query. 影響: …`.
- Why: EkeMinusYou's kit formalises this; NaruseJun and Mirrativ both describe learning the scenario mid-contest and then re-reading the manual with new eyes.
- Fit: panel/retrospective bundle; S.
- Evidence: EkeMinusYou docs/analysis/benchmarker-behavior.md (gh).

### M11: alp with query strings kept (values masked) for scenario quirks
- What: `query_string: true, query_string_ignore_values: true` so `/search?tag=` vs `/search?name=` separate.
- Why: takonomura's alp.yml does this; a trustdock participant's lesson: "ベンチマーカーの動きは例年クセがあるので alp はクエリパラメータも含めて見るべき" (search snippet).
- Fit: access-log report config; S.
- Evidence: takonomura/isucon14 alp.yml (gh).

---

## (2) App runtime (Go) metrics

### M12: Request-attributed work breakdown (SQL time / external-HTTP time / unattributed per request, p95 per endpoint)
- What: per endpoint: `sql_count avg/max per request`, `sql_time share`, `external HTTP count + connect/DNS/TLS/wait`, `unattributed` (= handler time − sql − ext).
- Why: isutools' "Request dependency timing" and "Endpoint × SQL shape" (opt-in); p1ass (ISUCON13 4th) used OTel traces to see `fillLivestreamResponse` called N times per request — "HTTP logs are aggregated, traces show each request". Our sqltag gives SQL↔handler mapping but not the per-request residual.
- How: context-scoped counters in a DB wrapper (`isutools.SQLDriverName("mysql")` style) and `clientstats.WrapTransport` for `http.Client`; emit `X-App-Db-Time`/`X-App-Sql-Count` response headers so nginx logs `$upstream_http_x_app_db_time` without an app log join.
- Fit: access-log report (new columns) + deep-dive; M.
- Evidence: ekusiadadus/isutools README (gh); https://blog.p1ass.com/posts/isucon13/.

### M13: database/sql pool stats per second (WaitCount, WaitDuration, InUse, Idle, MaxOpen)
- What: `db.Stats()` sampled 1 s: `isutools_db_wait_count`, `wait_duration`, `connection_pool{status=idle|open|in_use}`, `max_idle_closed`, `max_lifetime_closed`.
- Why: both mazrean's isutools and EkeMinusYou's `sql-pool-metrics` collect this; MaxOpenConns tuning is a standard mid-contest decision and pool wait is invisible in slow logs.
- How: `expvar`/JSON endpoint `/debug/sql-pools` returning `sql.DBStats`; collector writes TSV per second.
- Fit: per-second timeline; S.
- Evidence: mazrean/isucon-go-tools skills/isutools-prometheus/SKILL.md (gh); EkeMinusYou tools/analysis/schema/sql-pool.sql + docs/measurement/go-profiling.md (gh).

### M14: Cache/lock/queue instrumentation that ships with the helper library
- What: `cache_hit_count{name,stat=hit|grace_hit|miss|replace}`, `cache_length{name}`, `locker_index_access{name,type=read|write}` (histogram of lock hold), `queue_counter{name,status=in|out}`, `pool_count{alloc|get|put}`.
- Why: mazrean's library makes every cache/lock you add observable; the ISUCON14 講評 and takonomura's log show caching + SSE decisions dominated, and "did my cache actually hit?" is otherwise guesswork.
- How: wrap `motoki317/sc` or own map cache with counters; expose via Prometheus or expvar; our per-second collector scrapes once per second.
- Fit: per-second timeline + deep-dive; M.
- Evidence: mazrean/isucon-go-tools SKILL.md metric reference (gh); https://trap.jp/post/2046/.

### M15: Domain-state gauges snapshot (app-specific "queue depth" numbers per second)
- What: an app endpoint returning fixed-name gauges of business state each second — e.g. ISUCON14: unassigned rides, matchable chairs, oldest waiting ride age, chairs per stage (assigned/enroute/pickup/carrying/arrived), matcher batch size, solver ms.
- Why: EkeMinusYou's `ride-metrics` snapshot ("平均未割当2,697件・配車可能251台、最古待ち9.29秒") is what let them judge matching quality vs. waiting; the 講評 says unmatched-ride queueing cascaded into polling load.
- How: `Snapshot` callback in the metrics server (`/debug/sql-pools` style JSON), collector adds the columns to the TSV; keep it O(1) (counters updated at transitions, no scans).
- Fit: per-second timeline (new series), small contest-day effort once the hook exists; M for the kit, S per contest.
- Evidence: EkeMinusYou docs/implementations/ride-metrics.md (gh).

### M16: Runtime-profile set beyond CPU: goroutine, allocs, mutex, block, threadcreate, plus `runtime/metrics` per second
- What: automatically collect `goroutine`, `allocs`, `mutex`, `block`, `threadcreate` profiles per run (we have CPU/heap/block/mutex) and sample `runtime.NumGoroutine`, `NumGC`, `PauseTotalNs`, `HeapAlloc`, `StackInuse` each second.
- Why: EkeMinusYou auto-collects "CPU・heap・allocs・goroutine" per RUN; isutools collects goroutine/mutex/block/threadcreate; kawaemon used user-vs-kernel time ratio shifts to see when the bottleneck moved to the kernel. Goroutine count over time is the quickest "SSE/long-poll leak" signal (SSE in ISUCON14).
- How: `expvar` + `expvar.Publish("goroutines", expvar.Func(...))`, curl `/debug/vars` each second; or `/debug/pprof/goroutine?debug=1` count line.
- Fit: per-second timeline; S.
- Evidence: EkeMinusYou docs/measurement/go-profiling.md (gh); ekusiadadus/isutools README (gh); https://zenn.dev/kawaemon/articles/529fd6a12ac43b. (Go-side `expvar` details from general docs — search snippet, not ISUCON-specific.)

### M17: Profile diff between runs (flame-graph diff) and PGO from the best run's profile
- What: `go tool pprof -diff_base prev.pprof cur.pprof` (or Pyroscope diff view) as a standard artefact; keep the best-run CPU profile to build with `-pgo`.
- Why: mazrean: Pyroscope's diff view "became particularly useful for comparing before/after"; NaruseJun pulled a successful pprof out of pprotein for a PGO build during ISUCON13.
- How: store `cpu.pprof` per run; report `pprof -top -diff_base`; `go build -pgo=runs/best/cpu.pprof`.
- Fit: panel (run compare) ; S.
- Evidence: https://trap.jp/post/2434/; https://zenn.dev/tohutohu/articles/923bdf5dcd73af.

### M18: Distributed tracing on the side (OTel → Jaeger on the monitoring host over Tailscale), off for final runs
- What: otelhttp + otelsql/otelsqlx spans per request; Span Metrics Connector to aggregate `db.statement` durations.
- Why: p1ass (4th, ISUCON13): "statistics guess, traces show"; found repeated `fillLivestreamResponse`; monitoring server reached via Tailscale so the contest VMs stay clean. Hatena's exercise notes traces missed PowerDNS's own DB load, so cross-check with DB metrics. Caution: ISUCON14 teams were failed for an extra instance in the VPC (pprotein users) — keep the collector off-VPC and disable before final.
- How: `otelhttp.NewHandler`, `otelsqlx`, OTLP exporter to `monitor:4317`; `jaegertracing/all-in-one`.
- Fit: deep-dive (optional); L.
- Evidence: https://blog.p1ass.com/posts/isucon13/; https://developer.hatenastaff.com/entry/2024/10/16/180559; https://blog.rmatsuoka.org/entry/2024/12/13/183327 (search snippet).

---

## (3) MySQL / InnoDB

### M19: `performance_schema.events_statements_summary_by_digest` instead of (or alongside) the slow log
- What: top-20 by `SUM_TIMER_WAIT` with `COUNT_STAR, AVG/MAX, SUM_LOCK_TIME, QUANTILE_95/99/999, SUM_ROWS_EXAMINED/SENT, SUM_NO_INDEX_USED, SUM_NO_GOOD_INDEX_USED, SUM_SELECT_FULL_JOIN, SUM_SELECT_SCAN, SUM_SORT_*, SUM_CREATED_TMP_DISK_TABLES, MAX_TOTAL_MEMORY, QUERY_SAMPLE_TEXT`; `TRUNCATE` the table at log rotation / after `/initialize`.
- Why: takonomura (ISUCON14 winner) has `scripts/sql-performance.sh` with exactly this query and commits "Truncate mysql performance_schema on log rotating" / "Truncate performance stats after initialization"; 白金動物園: "performance_schema=ON doesn't affect score, default on, reset = TRUNCATE, no tool needed, sample text ready for EXPLAIN, and NO_INDEX_USED columns were decisive since the start state had almost no indexes".
- How: the query above (`/pow(1000,4)` → seconds); note prepared statements are not digested unless `interpolateParams=true` (search snippet); `performance_schema_max_digest_length` default 1024 truncates long SQL.
- Fit: DB report (new table; cheaper than long_query_time=0 at high QPS); S.
- Evidence: takonomura/isucon14 scripts/sql-performance.sh + commits (gh); https://blog.mirakui.com/entry/2023/11/27/152107.

### M20: Live InnoDB lock-wait edge sampling (50 ms)
- What: poll `performance_schema.data_lock_waits` joined with `data_locks`, `threads`, `events_statements_current` every 50 ms → `(ts, requesting_sql, blocking_sql, lock_data, wait_key)`.
- Why: our processlist snapshots are coarse; EkeMinusYou collects `<host>-mysql-lock-waits.tsv` this way to attribute lock waits to the blocking statement; 白金動物園's missed `flock`-style serialisation is the class of problem this exposes on the DB side.
- How: `SELECT r.THREAD_ID, rs.SQL_TEXT, b.THREAD_ID, bs.SQL_TEXT, w.REQUESTING_ENGINE_LOCK_ID ... FROM performance_schema.data_lock_waits w JOIN data_locks r ... JOIN events_statements_current ...` on a 50 ms loop during the run.
- Fit: DB report (lock section) + per-second timeline; M.
- Evidence: EkeMinusYou docs/measurement/mysql.md + tools/analysis/schema/mysql-lock.sql (gh).

### M21: Rows_examined/Rows_sent ratio as the index triage rule
- What: per digest: `ratio = SUM_ROWS_EXAMINED / SUM_ROWS_SENT`; flag ratio > 5 (NaruseJun's rule: ~<5× means indexed, 100×+ means not).
- Why: tohutohu describes slp's Row Sent/Row Examined ratio as his second step after alp Sum; ISUCON11 practical guide shows `Rows_examined=579.2, Rows_sent=579.2` as the signature of a missing LIMIT rather than missing index (ratio 1 but huge absolute).
- How: one column + a sort in the existing per-statement table; also surface absolute rows_sent per request (LIMIT missing).
- Fit: DB report; S.
- Evidence: https://zenn.dev/tohutohu/articles/923bdf5dcd73af; https://isucon.net/archives/56082639.html.

### M22: Buffer-pool / InnoDB status rates per second (`Innodb_buffer_pool_reads` vs `read_requests`, `Innodb_buffer_pool_wait_free`, `Innodb_row_lock_time`, `Created_tmp_disk_tables`, `Select_full_join`, `Sort_merge_passes`, `Threads_connected` vs `max_connections`)
- What: extend our per-second `SHOW GLOBAL STATUS` series with the buffer-pool miss rate, `wait_free`, `Select_full_join`, `Select_scan`, `Sort_merge_passes`, and `Threads_connected/@@max_connections` headroom.
- Why: EkeMinusYou reads `SHOW GLOBAL STATUS` + `@@global.max_connections` every second over the unix socket per MySQL host; the general MySQL guidance is that `Threads_running` is the faster saturation signal and hit-rate < 99 % means too small a pool (search snippets).
- How: `SELECT VARIABLE_NAME, VARIABLE_VALUE FROM performance_schema.global_status WHERE VARIABLE_NAME IN (...)` once per second (avoids `SHOW STATUS`'s tmp-table side effect).
- Fit: per-second timeline (new series); S. (Partially have: we already sample several counters.)
- Evidence: EkeMinusYou docs/measurement/mysql.md (gh); other details are search snippets (unverified in ISUCON use).

---

## (4) nginx / OS / network

### M23: Upstream breakdown in the access log (`$upstream_addr`, `$upstream_status`, `$upstream_cache_status`, `$upstream_connect_time`, `$upstream_header_time`)
- What: per route and per upstream: requests, status split, `response_time` vs `upstream_time` sum/avg, cache HIT/MISS/BYPASS counts; connect vs header vs response time split.
- Why: EkeMinusYou's `upstreams` view and dashboard "Upstream response breakdowns"; the F5/NGINX APM guide shows `$upstream_connect_time`/`$upstream_header_time` locating whether time is in connect, app, or body (search snippet). With 3 hosts, "which upstream is slow / unevenly loaded" is a contest-day question our log can't answer today.
- How: add `\tupaddr:$upstream_addr\tupstatus:$upstream_status\tcache:$upstream_cache_status\tupconnect:$upstream_connect_time\tupheader:$upstream_header_time` to LTSV; buffer `access_log ... buffer=64k flush=1s`.
- Fit: access-log report (new section); S.
- Evidence: EkeMinusYou docs/measurement/nginx-access-log.md + tools/analysis/schema/upstreams.sql (gh).

### M24: Request-ID propagation (nginx `$request_id` → app log) for single-request drill-down
- What: `proxy_set_header X-Request-ID $request_id;` + log `reqid:$request_id`; app logs slow handler paths/SQL with the same id.
- Why: poyo (ISUCON13, Grafana Cloud) needed `request_id` just to stop Loki deduplicating same-second nginx lines; NGINX's APM guide: pick the top `$upstream_response_time` rows and grep the app log by id (search snippet). Pairs with M12 headers.
- Fit: access-log + app log deep-dive; S.
- Evidence: https://poyo.hatenablog.jp/entry/2023/11/26/170000.

### M25: Per-process CPU from /proc without extra exporters + per-service CPU seconds within the load window
- What: sample `/proc/<pid>/stat` for app, nginx, mysqld each second (utime/stime), and report CPU-seconds per service inside the exact load window, per host.
- Why: mazrean replaced htop with procfs-based metrics; EkeMinusYou reports "INFO開始後30〜60秒のホストCPUは1号機Go 51.76 CPU秒, 2号機 nginx 36.02…" to decide host roles; fukkatsuso used process-exporter. We have CPU per process per second but not the window-integrated "CPU seconds per service per host" comparison table.
- How: `proc-metrics` style collector (`-services isuride-go.service,nginx.service,mysql.service` via cgroup v2 `cpu.stat`); integrate over the load window.
- Fit: per-second timeline → run summary table; S.
- Evidence: https://trap.jp/post/2046/; EkeMinusYou docs/analysis/benchmark-score-structure.md (gh).

### M26: TCP connection-state and ephemeral-port exhaustion monitoring (`ss -s`, TIME-WAIT per destination, port range used)
- What: per second: `ss -s` timewait/estab; `ss -Htn state time-wait | awk '{print $4}' | sed -E 's/:[^:]+$//' | sort | uniq -c` (which peer eats ports); `sysctl net.ipv4.ip_local_port_range`; nginx→app established count.
- Why: kawaemon saw saturation at 65,535 ports with kernel time spiking; MIXI tuned kernel TIME_WAIT settings late in ISUCON14; the fix check is "ESTABLISHED to upstream stays small and stable" (search snippets). We have tcp states but not per-destination or port-range headroom.
- How: commands above; add `net.ipv4.ip_local_port_range` and used-port ratio to the timeline.
- Fit: per-second timeline + deploy checklist; S.
- Evidence: https://zenn.dev/kawaemon/articles/529fd6a12ac43b; https://zenn.dev/mixi/articles/10dd2dae1f1e00.

### M27: nginx on-CPU profile (perf) when nginx itself becomes the hot process
- What: `perf record -F 99 -p <nginx workers> -- sleep N` during the run → flame graph; EkeMinusYou has a dedicated `nginx-oncpu-profiler` collector (off by default; needs kernel `perf`).
- Why: in their ISUCON14 runs nginx on hosts 2/3 consumed 36/27 CPU-s per 60 s — more than MySQL (0.62) — so nginx config (TLS, buffering, logging, gzip) became the decision; with JSON logging per request they explicitly warn to compare "requests vs nginx CPU seconds" between runs before blaming the log.
- Fit: deep-dive (optional); M.
- Evidence: EkeMinusYou tools/README.md + docs/measurement/nginx-access-log.md (gh).

### M28: Load-window derivation from ingress request rate and measurement-quality checks
- What: compute the actual load window from request-rate transitions in the access log (not from bench start/stop), then report per collector: in-window samples, max gap ms, missing files — and refuse to compare runs whose coverage is bad.
- Why: EkeMinusYou's `load_windows` / `measurement_quality` views; isutools' "Collector Health: flags runs that should not be compared"; takonomura's `rotate-logs.sh` runs before every deploy so each run's logs are clean.
- Fit: panel (run validity badge); S–M.
- Evidence: EkeMinusYou tools/analysis/schema/semantic.sql (gh); ekusiadadus/isutools README (gh); takonomura/isucon14 scripts/rotate-logs.sh (gh).

### M29: Little's-law sanity panel (L = λ·W per endpoint and whole system)
- What: compute concurrency `L = req/s × avg latency` per endpoint and total; compare with CPU utilisation: "L rising with flat throughput and idle CPU ⇒ waiting (locks, pool, serial section)".
- Why: yuuki's memo ("平均待ち行列数 = 到着率 × 待ち時間; capacity planning: if latency doubles, throughput must double") and the ISUCON book's advice "リソース使用率が低いのに並列数を増やしてもスループットが増えなければ何かできそう" (search snippets); fujiwara (2026 talk): P99 degrades from ~80 % utilisation before averages move.
- How: we already have Σreqtime/sec (that is L); add per-endpoint L and the L-vs-CPU correlation line to the report.
- Fit: access-log report + per-second timeline; S.
- Evidence: https://kakehashi-dev.hatenablog.com/entry/2026/01/16/110121; https://memo.yuuk.io/entry/2016/03/27/023002 (search snippet).

---

## (5) Workflow / dashboard practices

### M30: Results pushed to the commit (GitHub commit comment / issue) automatically after every bench
- What: `make deploy` posts "Deployed <branch> (<sha>)" to Slack; after bench, `comment-score.sh <score>` + `report-logs.sh` attach score, alp, perf-schema stats and pt-query-digest as collapsible sections to the exact commit and to a running issue.
- Why: takonomura (ISUCON14 winner, solo) — every one of 122 commits has its measurement next to it; MONOS (ISUCON13, 13th) writes all results to an Issue when the bench ends; LAPRAS kept per-bench files to compare (search snippet). Our panel keeps a timeline; the missing piece is tying artefacts to the commit object where reviewers look.
- Fit: panel → GitHub integration; S.
- Evidence: takonomura/isucon14 Makefile + scripts/comment-score.sh, report-logs.sh (gh); https://www.onoe.dev/blog/isucon13/.

### M31: alp diff between runs with a "+Xms / −N calls" view and SQL diff showing what disappeared and what newly appeared
- What: run-to-run diff tables: per endpoint Δcount, Δsum, Δp99; per SQL digest Δtotal time, with new rows highlighted red.
- Why: fukkatsuso's "alp diff dashboard (+10ms comparisons)"; isutools' diff on private-isu: "two `posts JOIN users` queries (352 s, 110 s) disappeared, but new queries show in red — check what you reduced AND what you added"; hogesako (ISUCON13 9th) made "confirm the query disappeared from pt-query-digest top" a mandatory step (search snippet).
- Fit: panel (compare two runs); M.
- Evidence: https://blog.fukkatsuso.com/posts/isucon14/; ekusiadadus/isutools README (gh).

### M32: Grafana-style annotations: bench runs and deploys as vertical markers on every time-series
- What: write run start/end and deploy sha as annotations; all-in-one dashboard with annotations for "quick timeline navigation".
- Why: fukkatsuso; rinsuki (ISUCON12) streamed Jenkins deploy logs to Discord so everyone knew the state without screen sharing (103 deploys).
- Fit: panel; S.
- Evidence: https://blog.fukkatsuso.com/posts/isucon14/; https://rinsuki.hatenablog.jp/entry/2022/07/25/215902.

### M33: Fixed reading order and division of attention
- What: a documented order: (1) alp **Sum** (after handler grouping), (2) slp Rows_examined/Rows_sent, (3) pprof once DB load is gone; netdata/dstat on a shared monitor; one "analysis person" who runs measurements and directs; others on branches, merge only after a non-failing bench.
- Why: tohutohu (NaruseJun) describes exactly this; 白金動物園 had a dedicated analysis role; msm_2: "初手は netdata でどのミドルウェアが重いかを見て絞る"; MIXI kept dstat+top on a continuous display; fujiwara: dashboards ordered outside→inside (P99 latency, errors, RPS → LB connections → app CPU/mem → DB query counts).
- Fit: panel landing page order + README; S.
- Evidence: https://zenn.dev/tohutohu/articles/923bdf5dcd73af; https://blog.mirakui.com/entry/2023/11/27/152107; https://qiita.com/msm_2/items/c88c50a2be26a1c7f2ed; https://zenn.dev/mixi/articles/10dd2dae1f1e00; https://kakehashi-dev.hatenablog.com/entry/2026/01/16/110121.

### M34: Measurement kill-switch and "final run" checklist with overhead evidence
- What: one env var (`ISUTOOLS=off`-style) that disables all instrumentation, plus a pre-final checklist: stop nginx access log/echo logger/pprotein log, remove tracing, truncate perf_schema, run envcheck.
- Why: NaruseJun found a second access-log writer (pprotein's) still on and gained >400k after disabling; the 17:00 score jump in their graph "is the result of stopping logs"; syfm disabled app metrics/traces before the final bench; ISUCON14 teams were disqualified for leftover measurement instances (envcheck).
- Fit: panel button + checklist; S.
- Evidence: https://zenn.dev/tohutohu/articles/923bdf5dcd73af; https://syfm.hatenablog.com/entry/2023/11/28/235616; ekusiadadus/isutools README (gh).

### M35: Keep measurement cheap: scrape resolution 1 s, no raw-log streaming to a remote
- What: aggregate on the contest host and ship only results; local Grafana via SSH forward instead of SaaS.
- Why: poyo: 15 s node-exporter was too coarse for ISUCON, Loki hit the 5,000-stream limit and queries slowed to tens of seconds late in the day; fukkatsuso deliberately avoided streaming raw logs to Loki; mazrean ran Grafana locally with SSH port-forward.
- Fit: architecture guideline for our collector; S (already mostly aligned).
- Evidence: https://poyo.hatenablog.jp/entry/2023/11/26/170000; https://blog.fukkatsuso.com/posts/isucon14/; https://trap.jp/post/2046/.

### M36: Static-analysis "CRUD map" (function → table → operation) next to the DB report
- What: isucrud-style graph: which handler/function does which CRUD on which table; filter by function/table.
- Why: mazrean uses it to find cache opportunities (who writes the table you want to cache) and as context for automated fix suggestions; complements our sqltag mapping (runtime) with a static write-set view.
- How: `go/ast` scan for `db.Exec/Query` string literals → parse table + verb; emit DOT/HTML.
- Fit: data-model report; M.
- Evidence: https://trap.jp/post/2434/; https://trap.jp/post/2046/.

---

### Notes on confidence
- Items based on repositories read directly (takonomura, EkeMinusYou, mazrean, ekusiadadus) are concrete. EkeMinusYou/isucon14-02 is a 2026 practice-run kit (not a contest-day top team), but its measurement contracts are the most detailed published.
- M22's threshold values and M24/M26 command details beyond what the ISUCON posts state come from general MySQL/nginx/Linux sources surfaced in search (unverified in contest use).
- Sources not opened and therefore excluded from evidence: pprotein README (404 on default branch), narusejun/isucon13-final (no scripts committed), kazeburo per-second MySQL material (not found).
