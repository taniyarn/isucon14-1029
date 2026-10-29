# deep-dive query cookbook (DuckDB over `export/*.csv`)

Run from the run directory (`measurements/<ts>/`): `duckdb -c "<sql>"`. Keep outputs small (`LIMIT`), aggregate instead of listing rows.
Columns: see `export/*.csv` headers. `t_rel_s` = seconds since the run started. `path` = URI with ids normalized to `:id`.
`slow.csv` holds the app's statements only: the measurement agent's own (`/* isumeasure */`, SHOW GLOBAL STATUS, processlist, row counts) are dropped at export.
Already in the report, no query needed: per-10 s count/avg of the top endpoints and internal jobs (**Per 10 s**), call interval × writes per call of `/api/internal/*` (**Periodic internal endpoints**), the deadlocked statement pair (**Errors**), and EXPLAIN of the slowest SELECTs (`<host>/explain.txt`, shown in the SQL actions).

## 0. orientation
```sql
-- load window and totals
SELECT min(t_rel_s), max(t_rel_s), count(*) req, sum(status>=500) err5, sum(static='true') static FROM 'export/access.csv';
-- requests per 10s with p99 (does throughput drop when latency rises?)
SELECT floor(t_rel_s/10)*10 t, count(*)/10.0 rps, round(quantile_cont(reqtime,0.99)*1000) p99_ms, sum(status>=500) err5 FROM 'export/access.csv' WHERE static='false' GROUP BY 1 ORDER BY 1;
```

## 1. where the time goes, and how it changes over the run
```sql
-- endpoint x 20s bucket: avg latency trend (a rising line = something grows with data: missing index, growing table scan)
SELECT method||' '||path ep, floor(t_rel_s/20)*20 t, count(*) n, round(avg(reqtime)*1000) avg_ms, round(quantile_cont(reqtime,0.99)*1000) p99_ms
FROM 'export/access.csv' WHERE static='false' GROUP BY 1,2 HAVING n>=10 ORDER BY 1,2;
-- same for SQL: does a statement get slower as the run proceeds?
SELECT sql_norm, floor(t_rel_s/20)*20 t, count(*) n, round(avg(query_time)*1000,2) avg_ms, round(avg(rows_examined)) rows_ex
FROM 'export/slow.csv' GROUP BY 1,2 HAVING n>=20 ORDER BY sum(query_time) DESC, 2 LIMIT 40;
```

## 2. in-flight concurrency (are we hitting a connection / worker / pool limit?)
```sql
-- requests in flight per second ≈ sum of reqtime over requests that overlap each second (approximation: Little's law)
SELECT floor(t_rel_s) s, count(*) rps, round(sum(reqtime),1) inflight_avg FROM 'export/access.csv' WHERE static='false' GROUP BY 1 ORDER BY 1;
-- compare with MySQL threads_running and app CPU from the timeline
SELECT t_rel_s, req, round(reqtime_p99*1000) p99_ms, cpu_user_pct, cpu_idle_pct, threads_running, questions_per_s, innodb_row_lock_waits_per_s FROM 'export/timeline.csv' ORDER BY t_rel_s;
```

## 3. per-request SQL (N+1), by endpoint when sqltag is enabled
```sql
-- executions per API request (needs tag column; without sqltag use the static 'called from' column in the report)
WITH req AS (SELECT method||' '||path ep, count(*) n FROM 'export/access.csv' WHERE static='false' GROUP BY 1)
SELECT s.tag, s.sql_norm, count(*) n, round(count(*)*1.0/max(r.n),2) per_req, round(sum(query_time),2) sum_s
FROM 'export/slow.csv' s JOIN req r ON r.ep = s.tag GROUP BY 1,2 ORDER BY sum_s DESC LIMIT 30;
-- where in the code (sqltag caller line): one statement's call sites with counts
SELECT loc, count(*) n, round(sum(query_time),2) sum_s FROM 'export/slow.csv' WHERE sql_norm LIKE 'SELECT * FROM livestreams WHERE id%' AND loc<>'' GROUP BY 1 ORDER BY sum_s DESC LIMIT 10;
```

## 4. sessions / bench behaviour
```sql
-- active sessions per role per 10s (user growth curve; which role stops growing first?)
SELECT floor(t_rel_s/10)*10 t, role, count(DISTINCT sess) n FROM 'export/access.csv' WHERE sess<>'' AND static='false' GROUP BY 1,2 ORDER BY 1,2;
-- what a single session does (pick one): sequence with gaps
SELECT t_rel_s, method||' '||path ep, round(reqtime*1000) ms, status FROM 'export/access.csv' WHERE sess=(SELECT sess FROM 'export/access.csv' WHERE role='app_session' LIMIT 1) ORDER BY t_rel_s LIMIT 60;
-- per-session request counts: are some sessions stuck (few requests) or hammering (polling)?
SELECT role, count(DISTINCT sess) sessions, round(avg(c)) avg_req, min(c), max(c) FROM (SELECT role, sess, count(*) c FROM 'export/access.csv' WHERE sess<>'' GROUP BY 1,2) GROUP BY 1;
```

## 5. errors and timeouts
```sql
-- what failed, when, and what the same session did just before
SELECT t_rel_s, method||' '||path, status, round(reqtime*1000) ms, sess FROM 'export/access.csv' WHERE status>=500 OR reqtime>=5 ORDER BY t_rel_s LIMIT 40;
-- then: grep the app log around that time:  grep -n "<HH:MM:SS>" app.log | head
```

## 6. locks and writes
```sql
-- statements with lock time; write mix over time
SELECT sql_norm, count(*) n, round(sum(lock_time)*1000) lock_ms, round(max(query_time)*1000) max_ms FROM 'export/slow.csv' WHERE lock_time>0 GROUP BY 1 ORDER BY lock_ms DESC LIMIT 15;
SELECT floor(t_rel_s/10)*10 t, sum(sql_norm LIKE 'INSERT%') ins, sum(sql_norm LIKE 'UPDATE%') upd, sum(sql_norm LIKE 'SELECT%') sel, sum(sql_norm LIKE 'COMMIT%') commits FROM 'export/slow.csv' GROUP BY 1 ORDER BY 1;
-- also read: processlist.txt (states like 'Waiting for lock'), innodb-status.txt (LATEST DETECTED DEADLOCK, TRANSACTIONS)
```

## 7. CPU by process over time
```sql
SELECT floor(t_rel_s/10)*10 t, name, round(avg(cpu_pct)) cpu FROM 'export/procs.csv' GROUP BY 1,2 HAVING cpu>=5 ORDER BY 1, 3 DESC;
```

## 8. keepalive / connections
```sql
-- requests per TCP connection (1 = no keepalive from the bench); upstream distribution across app servers
SELECT round(avg(connreq::INT),1) avg_req_per_conn, count(DISTINCT conn) conns FROM 'export/access.csv';
SELECT upstream, count(*) FROM 'export/access.csv' WHERE static='false' GROUP BY 1;
```

## 9. compare two runs
```sql
-- run A vs B per endpoint (export both, then):
SELECT a.ep, a.n n_a, b.n n_b, a.avg_ms avg_a, b.avg_ms avg_b
FROM (SELECT method||' '||path ep, count(*) n, round(avg(reqtime)*1000) avg_ms FROM '../<tsA>/export/access.csv' WHERE static='false' GROUP BY 1) a
FULL JOIN (SELECT method||' '||path ep, count(*) n, round(avg(reqtime)*1000) avg_ms FROM '../<tsB>/export/access.csv' WHERE static='false' GROUP BY 1) b USING (ep) ORDER BY coalesce(b.n,0) DESC;
```

## 10. bench behaviour (推測 — isucandar 型のベンチを仮定して読む。断定しない)
isucandar 製のサンプルベンチで確認した仕組み: スコア = Σ(タグ件数 × 倍率) − エラー、固定並列で前の応答が返ってから次を送る、
ステップが失敗するとそのシナリオは打ち切り、静的ファイルは 304 で検証省略。本番のベンチがそうとは限らないので、結論は「推測」と書く。
クライアントの鍵は Cookie のセッション（`sess`）、無ければ接続（`conn`）。**ログイン前後で鍵が変わる**ので、「最後が POST /api/login」は離脱ではなく鍵の切り替わり。
endpoint 列は report と同じ normalizer（alp.yml → ルーター → id 置換）でまとまっている（`task run:export TS=… CODE=webapp/go`）。
```sql
-- Q-scenario: クライアントごとの時刻順の並びから、4 連続の endpoint 列の上位 10 件。n = 出現回数、cycle = 4 ステップにかかった時間、step*_ms = 各ステップの平均応答
WITH a AS (
  SELECT coalesce(nullif(sess,''), 'conn:'||conn) AS client, t_rel_s AS t, reqtime, method||' '||path AS ep
  FROM 'export/access.csv' WHERE host='svc1' AND static='false' AND path NOT LIKE '%initialize%'),
s AS (
  SELECT client, t, reqtime, ep,
    lead(ep,1) OVER w AS e1, lead(ep,2) OVER w AS e2, lead(ep,3) OVER w AS e3, lead(t,3) OVER w AS t3,
    lead(reqtime,1) OVER w AS r1, lead(reqtime,2) OVER w AS r2, lead(reqtime,3) OVER w AS r3
  FROM a WINDOW w AS (PARTITION BY client ORDER BY t))
SELECT ep||' → '||e1||' → '||e2||' → '||e3 AS seq4, count(*) AS n,
  round(median(t3 - t),3) AS cycle_med_s, round(quantile_cont(t3 - t, 0.9),3) AS cycle_p90_s,
  round(1000*avg(reqtime)) AS step1_ms, round(1000*avg(r1)) AS step2_ms, round(1000*avg(r2)) AS step3_ms, round(1000*avg(r3)) AS step4_ms
FROM s WHERE e3 IS NOT NULL GROUP BY 1 ORDER BY n DESC LIMIT 10;
-- 6 連続が見たいときは lead を 5 まで足す。同じ endpoint の連続（icon × 4 など）は「一覧を描画している」と読む

-- Q-concurrency: 1 秒ごとの処理数 × 平均応答 = 同時処理数（Little の法則: Σreqtime/秒）と、動いているクライアント数。負荷窓の前半/中盤/後半で比べる
WITH b AS (
  SELECT floor(t_rel_s)::INT AS sec, count(*) AS rps, round(sum(reqtime),1) AS inflight,
         count(DISTINCT coalesce(nullif(sess,''), 'conn:'||conn)) AS clients
  FROM 'export/access.csv' WHERE host='svc1' GROUP BY 1),
w AS (SELECT min(sec) s0, max(sec) s1 FROM b WHERE rps > 0.3*(SELECT max(rps) FROM b))
SELECT CASE WHEN sec < s0 + (s1-s0)/3 THEN '1 start' WHEN sec > s1 - (s1-s0)/3 THEN '3 end' ELSE '2 mid' END AS phase,
  count(*) AS secs, round(avg(rps)) AS rps, round(median(inflight),1) AS inflight_med,
  round(quantile_cont(inflight,0.1),1) AS p10, round(quantile_cont(inflight,0.9),1) AS p90, round(avg(clients)) AS clients
FROM b, w WHERE sec BETWEEN s0 AND s1 GROUP BY 1 ORDER BY 1;
-- inflight が一定（前半≈後半、p10–p90 が狭い）なら固定並列: 応答時間の短縮がそのまま処理数とスコアになる。増えていくなら負荷上昇型: 伸びが止まる時刻の p99 / エラーを見る
-- （report の Verdict の "Bench shape" は同じ計算。数字が一致するか確かめる）

-- Q-dropoff: 流れが途中で切れたクライアント。最後のリクエストの endpoint と status、応答時間。login が最上位に来るのは鍵の切り替わり（上記）
WITH a AS (
  SELECT coalesce(nullif(sess,''), 'conn:'||conn) AS client, t_rel_s t, method||' '||path ep, status, reqtime,
         row_number() OVER (PARTITION BY coalesce(nullif(sess,''), 'conn:'||conn) ORDER BY t_rel_s DESC) rn
  FROM 'export/access.csv' WHERE host='svc1' AND static='false')
SELECT ep AS last_ep, CASE WHEN status>=500 THEN '5xx' WHEN status=499 THEN '499' WHEN status>=400 THEN '4xx' ELSE 'ok' END AS last_status,
  count(*) AS clients, round(1000*median(reqtime)) AS last_ms_med, round(1000*quantile_cont(reqtime,0.9)) AS last_ms_p90
FROM a WHERE rn=1 GROUP BY 1,2 ORDER BY clients DESC LIMIT 10;
-- 「その後に来るはずだった POST」の目安: 5xx/499 で終わったクライアント数 × Q-scenario でその endpoint の後に POST が続く割合

-- Q-304: endpoint ごとの 304 の割合（GET 全体に対して）と条件付き GET（If-None-Match 付き。ltsv の inm 列）の割合
SELECT method||' '||path AS ep, count(*) AS n,
  round(100.0*sum(CASE WHEN status=304 THEN 1 ELSE 0 END)/count(*),1) AS pct_304,
  round(100.0*sum(CASE WHEN inm<>'' THEN 1 ELSE 0 END)/count(*),1) AS pct_conditional
FROM 'export/access.csv' WHERE host='svc1' AND method='GET' GROUP BY 1 ORDER BY n DESC LIMIT 15;
-- report の endpoint 表の 304% 列と同じ分母（その endpoint の件数）

-- Q-score-link（run をまたぐ。各 run で task run:export を済ませ、スコアは管制盤の Score timeline から写す）
WITH scores(run, score) AS (VALUES ('20261013-153046', 0), ('20261013-155458', 0)),
a AS (SELECT regexp_extract(filename, '(\d{8}-\d{6})', 1) AS run, method||' '||path AS ep, count(*) AS n
      FROM read_csv_auto('measurements/*/export/access.csv', filename=true) WHERE host='svc1' GROUP BY 1,2)
SELECT a.run, score, n AS key_post, round(score::DOUBLE/n,2) AS score_per_post
FROM a JOIN scores USING(run) WHERE ep='POST /api/livestream/:livestream_id/livecomment' ORDER BY run;
-- endpoint ごとの件数とスコアの相関（run が 3 つ以上あるとき）
SELECT ep, round(corr(n, score),2) AS r, count(*) AS runs FROM a JOIN scores USING(run) GROUP BY ep HAVING count(*)>=3 ORDER BY r DESC NULLS LAST LIMIT 15;
```
答え合わせ: ベンチ側のログ（`/tmp/contestant.log`、`result.json` のタグ別件数）と、Q-scenario の n・Q-score-link の key_post を突き合わせる。2 回以上の練習で判断に使えたクエリだけ report に昇格する（今 report にあるのは Bench shape の 1 行、304% 列、管制盤の score/POST 列の 3 つ）。
