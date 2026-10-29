# ISUCON implementation notes — read before changing code（実装時の ISUCON 固有の注意）

For humans and AI agents. Every item is a known way to lose points or be disqualified. When in doubt, the contest manual wins over this file.

## 0. The one rule: the reboot test (去年の失格原因)

After the contest the organizers **reboot all servers and re-run the bench**. Your final score counts only if the app still passes and the **data written during the earlier bench is still there**. Last year we had 35,076 points and FAILED here ("データ保持NG").

Checklist for every change (an agent must be able to answer each):
- **Where does this data live after a reboot?** Anything the checker can read back (users, posts, balances, statuses, uploaded files) must be in MySQL (or on disk under a persistent path), or be rebuilt from MySQL at process start. In-memory-only writes = disqualification.
- **Caches are derived, never authoritative.** A cache must be rebuilt from the DB at startup and on `/initialize`. Write-through caches must write the DB first.
- **Files**: never write under `/tmp` or `/dev/shm`. Use a path in `/home/isucon` and keep the same path in every app server (or store blobs in the DB).
- **Services start by themselves**: new units (redis, a worker) need `systemctl enable`. **The DB may come up after the app** (DB on another host, or MySQL doing crash recovery): the app must not die permanently. systemd's default start limit (5 starts in 10 s) marks a unit failed forever — `deploy.sh` installs a drop-in (`Restart=always`, `RestartSec=3`, `StartLimitIntervalSec=0`) so the app keeps retrying; do not remove it. If the app caches at startup from the DB, make that lazy or retried, not fatal. `Requires=mysql.service` on the app unit is only right while the DB is on the same host.
- **Rehearse the re-test yourself**: `task hosts:measure-off` → `task db:snapshot LABEL=before` → `task reboot-test` (reboots all, checks units, smokes without initialize) → bench → `task db:snapshot LABEL=after` (no table may lose rows) → bench again.
- **Config files keep their mode and owner**: install them with `etc_install` (uses `etc/MODES.txt` captured by bootstrap). A `640 root` `pdns.conf` made `pdnsutil` (run by the app user) fail and `/initialize` with it.
- **Settings live in files**: `SET GLOBAL …`, `sysctl -w`, `ulimit` in a shell are lost on reboot. Put them in `etc/` (mysql conf.d, sysctl.d, systemd override) and apply via `deploy.sh`.
- **Background goroutines / workers** (matching, batch settlement) must re-arm at startup, and must be idempotent because the reboot can happen mid-job.
- **Do not rely on `/initialize` having run** after the reboot: the re-test may or may not call it. Startup must work on an already-populated DB.
- 16:30 onwards: `task hosts:measure-off` → `task hosts:reboot` → open the app, check data from the previous bench is visible, bench again (RUNBOOK §8).

## 1. `/initialize`

- Must finish within the manual's limit (often 10–30 s). Heavy DDL goes into the SQL files `init.sh` loads, or an idempotent `ALTER TABLE` guarded by `IF NOT EXISTS` / a check against `information_schema`.
- Must reset **every** cache in **every** app server (svc1's handler calls svc2's internal reset endpoint, or caches are rebuilt lazily from the DB after init).
- Must not delete or alter data that `init.sh` does not touch (the bench may pre-seed through the API before calling initialize in some contests: read the manual).
- Returns exactly the documented body (e.g. `{"language": "golang"}`); the language field often decides the leaderboard's language tag.

## 2. Responses must stay byte-equivalent in meaning

- Same status codes, same JSON fields, same `null` vs `[]` vs omitted, same number types (int vs float, string ids), same timestamp format and timezone, same ordering and tie-breaks (`ORDER BY created_at DESC, id DESC` — keep the secondary key when you rewrite a query).
- Pagination: keep limit/offset semantics and the "has next" rule.
- Validation order: if the original returns 400 before 404, keep that order; the checker tests error paths too.
- Error bodies are rarely checked, but status codes always are. A 500 costs score; a wrong 4xx can fail a scenario.
- Static files (HTML/JS/CSS/images) must be served with identical bytes. Serving them from nginx is fine; minifying or re-encoding is not.

## 3. Consistency windows (read the manual, then write the number here)

- Default assumption: **a write must be visible on the next read**. Only cache reads whose staleness the manual explicitly allows (e.g. "rankings may lag up to N seconds", "notifications within N seconds").
- Allowed staleness for this contest: ______ (fill in from `docs/kickoff.md`).
- No read replicas unless such a window exists for every read that would use them.

## 4. Multiple app servers (we will likely run two)

- No process-local state that is read back by the checker: counters, id generators (`AUTO_INCREMENT` or the DB), "latest status" maps, sessions (cookie-based or in the DB).
- Process-local **reference** caches (tags, master data, static config) are fine if every instance rebuilds them from the DB at startup and on `/initialize`.
- Endpoints with host-bound side effects (exec of a local tool such as `pdnsutil`, writing local files, a single worker) are pinned to svc1 in nginx (`location = /api/x { proxy_pass http://127.0.0.1:8080; }`) or the side effect is moved into the DB.
- Mutexes only protect in-process invariants; they do not serialize across hosts. Use DB row locks (`SELECT … FOR UPDATE`) or unique keys for cross-host invariants (reservations, balances).

## 5. SQL and MySQL

- Indexes: idempotent DDL, applied where `/initialize` or startup will apply it. Composite index column order = equality columns first, then range/order columns. Check `information_schema.statistics` in the final review.
- **A new index can create deadlocks.** If the indexed table has a `DELETE … WHERE <that column> = ?` followed by an `INSERT` in the same tx (replace-by-delete), the new secondary index turns the DELETE into next-key / gap locks on it, and concurrent requests deadlock (2026-10-25: `icons(user_id)` → `POST /api/icon` returned 500 ×123 in one run; the sequential `task smoke` never shows it). Before adding such an index, grep for that pattern; if found, make the DELETE hit the primary key (SELECT the ids first) or use `INSERT … ON DUPLICATE KEY UPDATE`, or retry on deadlock (1213) — and watch the report's Errors section on the next run.
- N+1 fixes: batch with `WHERE id IN (?)` (sqlx `In`) or JOIN; keep the result order the handler relied on.
- Transactions: keep the original isolation; do not hold a tx across network calls or sleeps; `FOR UPDATE` only on the rows you update.
- Connection pool: the default `SetMaxOpenConns(10)` is far too small. Budget: `open_per_app_host × app_hosts + other clients (pdns, workers, admin) + 20 ≤ max_connections` (zz-perf.cnf sets 1024). Typical: 100–200 per app host; `SetMaxIdleConns` = open; `SetConnMaxLifetime` a few minutes. Watch `WaitCount` in expvar — if it grows, raise open; if MySQL CPU is pegged, lowering it can help.
- DB host defaults (`zz-perf.cnf`, installed by deploy.sh on the DB host): `innodb_flush_log_at_trx_commit=2`, `sync_binlog=0`, `skip-log-bin`, `max_connections=1024`. The re-test is a clean reboot, so the ≤1 s durability window is acceptable; never set it to 0 with a durability requirement in the manual.
- Handlers that run external commands (`exec.Command`, e.g. `pdnsutil`): the report flags them; replace with the equivalent INSERT/API call — a process per request does not scale and breaks on a second app host without that tool.
- `interpolateParams=true` in the DSN saves a round trip per query. Keep `parseTime` as the app expects.
- Never `DROP`/`TRUNCATE` outside `init.sh`. Never change column types the bench compares.

## 6. Go specifics

- Do not share `*sqlx.Tx` or `*sql.Rows` across goroutines; close rows; check `rows.Err()`.
- Unbounded goroutines per request will exhaust connections; use the DB pool as the limiter or a semaphore.
- JSON: `omitempty` changes the response shape — add it only if the original had it. Keep struct field order.
- Money/points: integers, never float.
- Time: use the DB's or the original code's timezone; `time.Now()` vs `NOW()` must agree with the original.
- `ListenAndServe` on `0.0.0.0` (or `:8080`) so svc1's nginx can reach svc2.
- Remove `Debug = true`, request loggers, and pprof listeners before the final bench (`task hosts:measure-off` handles logs; the app's own debug flags are yours).

## 7. nginx

- `keepalive` to upstreams with `proxy_http_version 1.1` and `proxy_set_header Connection ""`; `worker_connections` ≥ 4096; `client_max_body_size` as the original.
- Keep `proxy_set_header Host $host` when the app or cookies depend on the host (subdomains).
- Do not shorten timeouts below the bench's; do not add caching headers unless the manual allows browser/CDN caching.
- `access_log off` only at the end (bootstrap's ltsv log is what the agent measures).

## 8. Things that disqualify regardless of score

- Editing files the manual lists as forbidden (env checker services, organizer users, their ssh keys), changing the security group, adding servers or external services, exposing the app to the internet beyond the given ports, stopping servers after the contest ends before the organizers say so.
- Any resource outside the three servers during the bench (external cache, API, DNS resolver, package downloads at runtime).

## 9. Hygiene that keeps the team fast

- One change per branch, deploy with `task deploy BRANCH=…`, note "what changed" in the panel for every bench.
- Before editing, claim your files and functions in `docs/claims.md` (read it on `origin/main`; same function as an `open` row = conflict, stop and report). PR #15 and #16 of the 2026-10-18 practice rewrote the same 3 functions and conflicted at merge.
- Record each change (time, branch, what, why = report row, result = score Δ): lane 1 writes `docs/journal.md`; other sessions put the line in the PR body and lane 1 copies it (CLAUDE.md rule 4 exception c).
- Prefer specializing a hot path over editing a shared helper with many callers (copy as `<helper>For<Endpoint>` and optimize the copy; keep the shared one).
- Before 17:00: run the final review (CLAUDE.md rule 10) against this file's sections 0–4 and 8.
