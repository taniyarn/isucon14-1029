# ISUCON11 (2021) technique catalogue — 予選 ISUCONDITION / 本選 ISUCHOLAR

Sources actually opened: official repos (isucon/isucon11-qualify, isucon/isucon11-final: README, docs/manual.md, docs/isucondition.md, docs/isucholar.md, webapp/sql schemas, webapp/go/main.go), isucon.net posts (regulation 55854734, 予選解説 56044867, 予選攻略法 56082639, 予選結果 56021237, 予選全スコア 56021246, 本選解説 56163308, 本選結果 56098785, まとめ 56019610), team repos via `gh api` (y1r/isucon11-yosen, fujiwara/isucon11-q, narusejun/isucon11-qualify, matsuu/isucon11q, tatsumack/isu11q, fujiwara/isucon11-f, narusejun/isucon11-final, y1r/isucon11-honsen, ichyo/isucon11f, matsuu/isucon11f, kazeburo/isucon11-final, tatsumack/isu11f) and the 参加記 posts cited below. 都営三田線 (予選2位) and FetchDecodeExecWrite (本選3位) did not publish repos I could find; blog only for the former.

Results context: 予選 top 5 = shallowverse 1,464,232 / 都営三田線… 1,026,200 / NaruseJun 919,720 / fujiwara組 769,405 / Oops! 530,673; border (25th) 106,094 (https://isucon.net/archives/56021246.html). 本選 top 5 = fujiwara組 135,523 / NaruseJun 119,984 / FetchDecodeExecWrite 100,258 / 焼肉ジャンボチキン 96,344 / negainoido 91,582; 12 of 30 teams failed (https://isucon.net/archives/56098785.html).

---

## Part A — 予選 ISUCONDITION

### T: Composite index → composite PRIMARY KEY (jia_isu_uuid, timestamp) on isu_condition, drop AUTO_INCREMENT id
- what: Reference schema has `isu_condition(id AUTO_INCREMENT PK)` with every read doing `WHERE jia_isu_uuid=? ORDER BY timestamp DESC`. Step 1: add index `(jia_isu_uuid, timestamp)`. Step 2: make it the PK and delete the surrogate `id` so there is one B-tree for the write-heavy table (clustered on the read key).
- when it works: Time-series table whose reads are always "by device, ordered by time" and whose rows are never addressed by `id`; manual guarantees uniqueness: 「コンディションの時刻情報が重複することはない」「既に送られたコンディションの時刻情報よりも常に先の時刻となる」(docs/isucondition.md).
- how to verify mid-contest: `grep -n "isu_condition" main.go` — confirm no code reads `.ID` of a condition; slow log shows `Rows_examined` ≫ `Rows_sent` on that table; `SHOW CREATE TABLE`. On MariaDB `DESC` index direction is a no-op (official).
- risk: typo'd column (ウー馬場ーイー2 indexed `created_at` instead of `timestamp`, fixed 20,127→37,019). After PK change a second secondary index doubles write cost — fujiwara removed the extra index in the same PR (「インデックス1個にまとめて書き込み負荷を下げる」).
- evidence:
  - official 攻略法 1,902→20,742 with composite PK: https://isucon.net/archives/56082639.html
  - official 解説「(jia_isu_uuid, timestamp)を複合PrimaryKeyに設定することで、より負荷を下げることができました」https://isucon.net/archives/56044867.html
  - fujiwara組 PR#30 374,366→684,959: https://github.com/fujiwara/isucon11-q/commit/864a90cd and /commit/287fbb44; blog https://sfujiwara.hatenablog.com/entry/2021/08/23/174355
  - ウー馬場ーイー2 index typo fix 20,127→37,019: https://matsuu.hatenablog.com/entry/2021/08/22/141130
  - 都営三田線 "App-side sorting (remove isu_condition index)" 372,101→400,792: https://blog.agatan.dev/posts/isucon11-qualify
  - Classi practice post: AUTO_INCREMENT lock removal "major impact": https://tech.classi.jp/entry/2021/12/23/133000
- regulation note: 「データベーススキーマの変更やインデックスの作成・削除」explicitly allowed (https://isucon.net/archives/55854734.html). Schema change must be reproduced by `/initialize` (manual: 「この処理が行っている内容を漏れなく提供してください」).

### T: Bulk INSERT inside POST /api/condition
- what: Reference loops `INSERT` per condition inside a tx. Build one multi-row `INSERT ... VALUES (...),(...)`.
- when it works: Any POST whose body is an array that becomes N rows (here ISU sends several conditions per request).
- how to verify: slow log `Query_time` small but count huge for `INSERT INTO isu_condition`; pt-query-digest top by count.
- risk: Alone it gives small gains (ウー馬場ーイー2 saw −520; fujiwara +8,686) — the big win needs the drop-rate/async steps below.
- evidence:
  - official: 「一件ずつのクエリから bulk/batch insert へ」https://isucon.net/archives/56044867.html; 攻略法 22,092→29,022 (with LIMIT 1) https://isucon.net/archives/56082639.html
  - fujiwara組 PR#11 39,704→48,346: https://github.com/fujiwara/isucon11-q/commit/bc9cd176
  - shallowverse: https://github.com/y1r/isucon11-yosen/commit/cb7cf1ca
  - Mirrativ: https://github.com/tatsumack/isu11q/commit/db31058a
- regulation note: none beyond data persistence.

### T: Asynchronous buffered writes for POST /api/condition (channel + ticker, bulk flush every 0.2–2 s, return 202 immediately)
- what: Handler validates and pushes conditions into a channel, returns `202 Accepted`; a goroutine flushes a multi-row INSERT every N ms or when batch size reached (shallowverse: 5,120 rows / 1–2 s; NaruseJun: 200 ms per shard).
- when it works: Manual explicitly tolerates delayed visibility: 「`POST /api/condition/:jia_isu_uuid` で受け取ったコンディションの反映が遅れることをベンチマーカーは許容しています」(GET /api/isu, graph, condition, trend) with the constraint 「`GET /api/condition/:jia_isu_uuid` と `GET /api/isu/:jia_isu_uuid/graph` の間で取得できる情報は 1 秒以内に整合性を保つ必要があります」. Also POST timeout is 100 ms with 「減点の対象とはなりません」 — so the handler must just be fast.
- how to verify: Read the 「キャッシュについて」 section of the manual; check the POST is append-only (no read-after-write in the same request); alp shows POST /api/condition as top count.
- risk: Buffered rows are lost on crash/restart → 追試 requires 「負荷走行実行時にアプリケーションに書き込まれたデータはサーバー再起動後にも取得できること」; keep the flush interval ≪ 1 s or flush on SIGTERM. 都営三田線 kept only a 10 ms window out of caution (「fail にびくびくって 10ms 以内の書き込みしかまとめないようにしていた」). Validation errors (400) can no longer be returned once you've accepted — shallowverse silently skipped invalid rows.
- evidence:
  - official: 「複数リクエストを跨いで纏めたコンディションを一定間隔でINSERTするとさらに速くなります」https://isucon.net/archives/56044867.html
  - shallowverse → 35,000+: https://github.com/y1r/isucon11-yosen/commit/6f7e5f43 ; params https://github.com/y1r/isucon11-yosen/commit/2c811080 ; blog https://y1r.org/posts/20210831-isucon11-qualify/
  - NaruseJun ~107k→158k: https://github.com/narusejun/isucon11-qualify/commit/d375e46e ; blog https://6715.jp/posts/30/
  - 都営三田線 18:12 async+batch →372,101: https://blog.agatan.dev/posts/isucon11-qualify
  - Classi 136,226→222,174 "most effective improvement": https://tech.classi.jp/entry/2021/12/23/133000
- regulation note: 「キャッシュ機構の追加、ジョブキュー機構の追加による遅延書き込み」explicitly allowed (https://isucon.net/archives/55854734.html).

### T: dropProbability — tune, move, then remove (and the hourly-cap variant)
- what: Reference drops 90% of POST /api/condition requests (`dropProbability := 0.9` returns 202 without writing, with a Warn log). Teams (a) lowered it stepwise as capacity grew, (b) moved the sampling from "reject whole request" to "drop individual rows at insert time", (c) set it to 0 once the pipeline could absorb everything, or (d) replaced it with a per-ISU per-hour cap (Mirrativ kept ≤30 conditions/hour) because graph scoring saturates at n>20 per hour.
- when it works: Manual: 「ユーザはコンディションのデータ欠損を許容しますが、理想的には全てのコンディションのデータが保存されることを期待しています」; scoring: 当日グラフ Good=60 for n>20, 完成グラフ Good=150 for n>20 — more stored conditions ⇒ higher multiplier (「dropProbabilityを下げることで、一日の中にあるコンディションの数が多くなり、グラフの確認による加点を大きくできます」).
- how to verify: grep `dropProbability`; watch DB CPU after each decrement; the scoring table in manual 「スコア計算」.
- risk: Lowering it before the write path is fixed hurts: ウー馬場ーイー2 went 0.6→0.9 (more dropping) and jumped 269,411→379,417 because their DB was saturated. Lower it last.
- evidence:
  - fujiwara組 PR#24 (`dropProbability` removed) 73,570→374,366 「ここで dropProbability=0 で通るようになったのでスコアが跳ね上がりました」: https://github.com/fujiwara/isucon11-q/commit/1a5e79e9 ; https://sfujiwara.hatenablog.com/entry/2021/08/23/174355
  - shallowverse 0.9→0.4 (final 1.5M) and drop moved to insert time: https://github.com/y1r/isucon11-yosen/commit/c60efc34 , /commit/2c811080
  - ウー馬場ーイー2 reverted to 0.9 +110,006: https://github.com/matsuu/isucon11q/commit/af9e57fd
  - Mirrativ per-hour cap: https://github.com/tatsumack/isu11q/commit/e80a1121 ; blog https://tech.mirrativ.stream/entry/2021/08/25/120426
  - official 攻略法 53,272→152,271 on removing it: https://isucon.net/archives/56082639.html
- regulation note: Manual says drop is allowed but 「反映を遅らせた場合、加点の機会が減る可能性に留意してください」.

### T: GET /api/trend — LIMIT 1 for latest condition
- what: Reference selects all conditions per ISU and uses `[0]`; add `ORDER BY timestamp DESC LIMIT 1` (and collapse the character→isu→condition N+1 into a single query).
- when it works: Any "latest row per entity" read; slow log shows `Rows_sent=579 Rows_examined=579` style lines.
- how to verify: pt-query-digest Rows_sent vs rows used in code.
- risk: none.
- evidence: official 攻略法 (Rows_sent=579.2) https://isucon.net/archives/56082639.html ; fujiwara PR#6 ~17.9k→30k https://github.com/fujiwara/isucon11-q/commit/06190135 ; ウー馬場ーイー2 +2,509 https://matsuu.hatenablog.com/entry/2021/08/22/141130 ; Mirrativ https://github.com/tatsumack/isu11q/commit/1ddfb746 ; shallowverse https://github.com/y1r/isucon11-yosen/commit/38574bd2 , /commit/2f5ad4fe

### T: GET /api/trend — serve a periodically rebuilt cache (background goroutine 0.5–1 s, Varnish TTL 0.8 s, or singleflight+50 ms)
- what: /api/trend has no parameters and no auth; compute it once per interval and return the cached JSON to every request. Variants: ticker goroutine writing `sync.Map`/RWMutex (shallowverse, NaruseJun, kazeburo); Varnish in front with `unset req.http.cookie` and `ttl=0.8s grace=0.2s` (ウー馬場ーイー2); singleflight + 50 ms coalescing (都営三田線).
- when it works: Endpoint listed in manual as delay-tolerant (`GET /api/trend` in 「コンディションの反映について」); response identical for all users (「ログイン状況に関わらずコンテンツの出し分けがなく」); request count grows with user count because viewers poll it.
- how to verify: handler has no `c.Param`/session lookup; alp shows it as top-count GET late in the run; check cache is reset in `/initialize` (NaruseJun commit 「trendUpdateをinitialize時にもする」).
- risk: (1) As users scale, fd/connection limits bite — official: 「ファイルディスクリプタ数の上限を増やすなど…緩和しないとエラーが出てfailすることがあります」. (2) The background worker logging DB errors every 0.5 s filled server 3's disk and nearly failed the reboot check for 手が3本あるテディーベア. (3) Oops! reverted trend cache after GOAWAY errors. (4) Stale trend slows the "ユーザが増える" loop — bench users only appear when trend changes.
- evidence:
  - official 「一度レスポンスを計算したら、そのデータをキャッシュしておき、全てのリクエストに対してそのデータを返すことができます」https://isucon.net/archives/56044867.html
  - shallowverse cron 1 s: https://github.com/y1r/isucon11-yosen/commit/300c8343
  - NaruseJun 0.5 s updater (~26k): https://github.com/narusejun/isucon11-qualify/commit/1e24895d , /commit/2c58a423
  - ウー馬場ーイー2 Varnish 37,019→44,034: https://github.com/matsuu/isucon11q/commit/ab294623 ; https://matsuu.hatenablog.com/entry/2021/08/22/141130
  - 都営三田線 singleflight 143,372→161,966: https://blog.agatan.dev/posts/isucon11-qualify
  - teddy bear disk-full near miss: https://trap.jp/post/1357/ ; Oops! GOAWAY: https://kjmkznr.github.io/post/20210822/isucon11-qual/
- regulation note: 「アプリケーションはベンチマーカーが検知しない限りは古い情報を返しても構いません」(manual).

### T: Deliberately throttle /api/trend (sleep) to pace user growth
- what: NaruseJun added `time.Sleep(500ms)` in getTrend (later reduced) so viewers get the trend slower, which slows the benchmark's user-growth loop and keeps the app under the 1 s timeout on the scoring endpoints.
- when it works: Bench model where a non-scoring endpoint drives load growth (「トレンドの変化があると評判が良くなり、ユーザが増加するようになっています」) and the scoring endpoints (condition/graph) earn more when they are fast.
- how to verify: Read 「ISU の仮想時間」/user model sections; watch whether score rises when trend latency increases.
- risk: trend requests >1 s incur timeout penalties (−1 per 10). Highly bench-specific; NaruseJun tuned "getTrend delay" repeatedly (~295k→346k, then param tuning to 1.17M).
- evidence: https://github.com/narusejun/isucon11-qualify/commit/7a79bd16 , /commit/851b6e66 ; blog https://6715.jp/posts/30/ ("throttle trend response" at 13:30 and 15:00).
- regulation note: none.

### T: Pre-computed condition level column so WHERE + LIMIT can be pushed into SQL
- what: `condition` is a CSV string `is_dirty=true,is_overweight=false,...`; GET /api/condition filters by level in Go after fetching everything. Add a `level`/`condition_true_count` column (STORED GENERATED column counting "=true", or computed at insert), filter `WHERE level IN (?)` and apply `LIMIT`.
- when it works: Any filter done in app code after an unbounded SELECT; the filter key is a pure function of row data (so it can be a generated column or computed on write).
- how to verify: look for loops that `break` after `limit` items in handlers; official warning that the issue is LIMIT not normalisation per se.
- risk: Adding LIMIT without moving the filter into SQL returns wrong sets → Mirrativ hit consistency failures; isucon_friends was disqualified in 追試 (「conditionの絞り込みで info,warning を指定しても warning しか表示されない」). Schema change must be applied in `/initialize` (fujiwara ran `ALTER TABLE ... ADD COLUMN` + backfill UPDATE there).
- evidence:
  - official 攻略法 29,022→32,462: https://isucon.net/archives/56082639.html ; 解説 on the misunderstanding https://isucon.net/archives/56044867.html
  - NaruseJun generated column + LIMIT ~72k→107k: https://github.com/narusejun/isucon11-qualify/commit/5fd5fa04
  - fujiwara PR#16/#19 (ALTER in initialize, true_count): https://github.com/fujiwara/isucon11-q/commit/31764cf7 , /commit/7ca04178 , /commit/c4f5002c
  - Mirrativ `level` column (→~280k): https://github.com/tatsumack/isu11q/commit/45a149d5
  - 都営三田線 40,120→83,782: https://blog.agatan.dev/posts/isucon11-qualify ; teddy bear ~26k→50k https://trap.jp/post/1357/
  - disqualification: https://isucon.net/archives/56021237.html
- regulation note: schema changes allowed; response structure must not change.

### T: Graph endpoint — fetch only the requested 24 h window
- what: `GET /api/isu/:uuid/graph?datetime=` reads all conditions of the ISU; add `AND ? <= timestamp AND timestamp < ?` for the day (some used 25 h to be safe).
- when it works: Endpoint takes a time parameter but query ignores it; Rows_sent anomalously large.
- how to verify: compare the SQL with the parameters parsed in the handler.
- risk: 都営三田線 first cut both ends and lost points; fixing the window to start-side only gave 114,958→143,372.
- evidence: official 32,462→35,372 https://isucon.net/archives/56082639.html ; fujiwara PR#4 https://github.com/fujiwara/isucon11-q/commit/23435a75 ; NaruseJun 346k→631k https://6715.jp/posts/30/ ; shallowverse https://github.com/y1r/isucon11-yosen/commit/58aed6c2 ; Mirrativ https://github.com/tatsumack/isu11q/commit/21c582ed ; kazeburo BETWEEN https://kazeburo.hatenablog.com/entry/2021/08/24/144237

### T: Icon image out of the DB (LONGBLOB) → file + nginx (try_files / X-Accel-Redirect) + Cache-Control max-age
- what: `isu.image` is a LONGBLOB selected by `SELECT *` in list endpoints and served by Go. Options stacked by teams: select explicit columns (never the blob) → write icon to `../images/<uuid>/icon` at POST /api/isu and let nginx serve (`try_files`, or app returns `X-Accel-Redirect`) → add `expires 86400s`/`Cache-Control: max-age=3600` so the bench's browser-like client stops requesting → `UPDATE isu SET image=NULL` after export to shrink rows.
- when it works: Manual 「Conditional GET のサポートについて」: bench honours Cache-Control / 304 and 「ブラウザのキャッシュ有効期限の制御によってリクエストが発生していない場合も、ベンチマーカーはそれらのキャッシュを利用してレスポンスがあったものとみなします」. kataribe showed /icon as #2 by count (26k requests, 490 MB) in ウー馬場ーイー2's early logs.
- how to verify: `SHOW CREATE TABLE isu` for blob; alp/kataribe bytes column; check initial data includes icons that must be dumped in `/initialize`.
- risk: Two teams lost their qualification in 追試 because icons broke after reboot: morupoppo 「ISUの画像が表示されない (500)」, さばかんちゃーはんかれー 「(404)」(https://isucon.net/archives/56021237.html). Exported files must survive reboot and be recreated by `/initialize` for seeded ISUs. ウー馬場ーイー2 found gzip of static files decreased score.
- evidence:
  - ウー馬場ーイー2 X-Accel-Redirect 146,710→208,386: https://github.com/matsuu/isucon11q/commit/6fd8706e ; https://matsuu.hatenablog.com/entry/2021/08/22/141130
  - fujiwara PR#17 (file + try_files), PR#28 (NULL image): https://github.com/fujiwara/isucon11-q/commit/b7b8da6a , /commit/cb60eafa
  - Mirrativ file + `Cache-Control: max-age=3600`: https://github.com/tatsumack/isu11q/commit/ae2da92c , /commit/44608585
  - NaruseJun icon to file (~52k): https://github.com/narusejun/isucon11-qualify/commit/e3cc3134
  - shallowverse in-memory icon cache: https://github.com/y1r/isucon11-yosen/commit/a135d2e4
  - Oops! "specifying columns to avoid image": https://kjmkznr.github.io/post/20210822/isucon11-qual/
- regulation note: 「画像および動画等のメディアファイルの内容」must not change; serving path may.

### T: Static assets and index.html via nginx with long expires (+ gzip_static)
- what: Replace `e.Static`/`getIndex` with nginx `try_files $uri /index.html; expires 86400s;`; optionally `gzip_static` with pre-compressed assets.
- when it works: Same Conditional-GET rule as above; kataribe shows `/assets/vendor.*.js` 743 KB × thousands.
- how to verify: alp by bytes; confirm bench requests assets (it does — see kataribe rows).
- risk: ウー馬場ーイー2: gzip decreased score (cause unknown); 都営三田線 static via nginx briefly lowered score until cache-control on index.html (19,300→25,392).
- evidence: ウー馬場ーイー2 nginx config https://matsuu.hatenablog.com/entry/2021/08/22/141130 ; shallowverse gzip_static https://y1r.org/posts/20210831-isucon11-qualify/ , https://github.com/y1r/isucon11-yosen/commit/8fb3f187 ; 都営三田線 https://blog.agatan.dev/posts/isucon11-qualify ; NaruseJun gzipped https://github.com/narusejun/isucon11-qualify/commit/f3e67c1d
- regulation note: JS/CSS content must not change (gzip is fine, minify is not).

### T: Kill logging (echo Debug/access log, the drop-warning log, nginx access_log, MySQL slow log) — appears in both rounds
- what: Reference echo runs with `Debug=true` and a logger middleware; `dropProbability` emits a Warn per dropped request; later turn off nginx access log and MySQL slow log before the final run.
- when it works: `top` shows `systemd-journal` near the top; official: 「これはアプリケーションがログを出力しすぎているということを表している」.
- how to verify: `top`/`journalctl -f` volume; disk usage.
- risk: none, but keep a way to re-enable for debugging. Disk-full from logs broke env-checker for the teddy bear team.
- evidence: official 35,372→39,405 https://isucon.net/archives/56082639.html ; 都営三田線 31,258→37,395→40,120 and slow log off 400,792→593,482 https://blog.agatan.dev/posts/isucon11-qualify ; teddy bear ~50k→80k+ https://trap.jp/post/1357/ ; fujiwara PR#3/#8/#9 https://github.com/fujiwara/isucon11-q/commit/e8a8e702 , /commit/49d95f7b ; 本選: fujiwara "slow query log disabled" final gain https://sfujiwara.hatenablog.com/entry/isucon11-final ; NaruseJun https://github.com/narusejun/isucon11-final/commit/be665b51
- regulation note: none.

### T: `interpolateParams=true` in the Go MySQL DSN (both rounds)
- what: Client-side placeholder interpolation; removes the Prepare/Execute/Close round trips (`ADMIN PREPARE` in pt-query-digest).
- when it works: Go + go-sql-driver/mysql with sqlx; pt-query-digest shows PREPARE at top by count.
- how to verify: `grep -n "interpolateParams" main.go`; pt-query-digest.
- risk: none noted; 都営三田線 got 593,482→1,045,520 from it at 18:30 (large because everything else was already fixed).
- evidence: https://blog.agatan.dev/posts/isucon11-qualify ; fujiwara PR#2 https://github.com/fujiwara/isucon11-q/commit/bebfed08 ; Mirrativ https://github.com/tatsumack/isu11q/commit/61292fa4 ; 本選 fujiwara https://github.com/fujiwara/isucon11-f/commit/3b232668 ; ウー馬場ーイー2 本選 31,928→36,321 https://matsuu.hatenablog.com/entry/2021/09/20/204341 ; Mirrativ 本選 "ADMIN PREPARE消す" https://github.com/tatsumack/isu11f/commit/0ac0b5fd

### T: Cache "existence" lookups per request (user/session, ISU by uuid, ISU name) in process memory
- what: `getUserIDFromSession` does `SELECT COUNT(*) FROM user` on every request; replace with an in-memory set populated at login/`/initialize`; same for `isu` row by (user, uuid) and isu name.
- when it works: Tables that only grow (user, isu) and never update the cached fields; append-only writes go through the same app (or cache is per-key fill-on-miss).
- how to verify: `grep -n "SELECT COUNT" main.go`; confirm no DELETE/UPDATE on the table.
- risk: With multiple app servers fill-on-miss is fine; pre-population must be done in `/initialize`. fujiwara's Redis branch scored worse than in-process maps.
- evidence: fujiwara PR#23 user cache 48,346→73,570 and PR#26/#29 https://github.com/fujiwara/isucon11-q/commit/2028bb36 , /commit/ecf2ef74 , /commit/d7cf3875 ; shallowverse https://github.com/y1r/isucon11-yosen/commit/ebc41ca5 , /commit/4bca6aab ; NaruseJun https://github.com/narusejun/isucon11-qualify/commit/db6fa897
- regulation note: cache allowed.

### T: Multi-server split: 1 DB + 2 app, POST /api/condition (and graph) on its own app; nginx upstream keepalive; unix socket for co-located app
- what: Move MariaDB to server 2 (`bind-address 0.0.0.0`, env MYSQL_HOST=192.168.0.x); nginx on server 1 routes `/api/condition/*` (and later `/api/isu/*/graph`) to app on server 3, everything else local; `upstream { keepalive 16..128; keepalive_requests 10000000 }`, `proxy_http_version 1.1; proxy_set_header Connection "";`; co-located app listens on a unix socket.
- when it works: Single-server CPU saturated after query fixes; POST stream is independent of GET traffic.
- how to verify: `top` per server; alp count by path to pick what to pin.
- risk: Bench connects to `isucondition-1.t.isucon.dev` only (manual) — other hosts are reachable only via private IPs; `/initialize` must hit every server's state. JIA activate `target_base_url` host restriction limits where POST can land (only the 3 hostnames, https, no port).
- evidence: official 39,405→53,272 https://isucon.net/archives/56082639.html ; 「アプリケーション2台（DB1台）構成…この変更で大きくスコアが伸びます」https://isucon.net/archives/56044867.html ; ウー馬場ーイー2 79,819→146,710 (DB) and 208,386→269,411 (3rd app) https://matsuu.hatenablog.com/entry/2021/08/22/141130 ; 都営三田線 161,966→198,583→214,492 https://blog.agatan.dev/posts/isucon11-qualify ; NaruseJun lb/keepalive/unix socket 158k→295k→836k https://github.com/narusejun/isucon11-qualify/commit/3d9f96bf , /commit/99b30367 , /commit/d45bc40b ; fujiwara 「2台目をnginxにする」https://github.com/fujiwara/isucon11-q/commit/641c1439
- regulation note: 「複数台あるサーバーの役割の変更」allowed; only the 3 CloudFormation instances may be used.

### T: Shard / split-write isu_condition across DB servers
- what: NaruseJun opened a `*sqlx.DB` per host and chose the shard by first byte of `jia_isu_uuid` (`int(id[0]) % 3`), with per-shard async bulk inserts; 都営三田線 used DB1 for reads and DB2 for condition writes.
- when it works: One hot table dominating writes; reads are always keyed by the shard key (uuid).
- how to verify: all queries on the table include `jia_isu_uuid=?` (trend did not → they rebuilt trend per shard).
- risk: `/initialize` must run on every shard (NaruseJun 「3台に対してinit」); they later collapsed from 2 DB to 1 DB when CPU was unbalanced. Complex; only worthwhile after single-DB tuning.
- evidence: https://github.com/narusejun/isucon11-qualify/commit/ce2f2a0a , /commit/2c928717 , /commit/7bc9d6ae ; blog https://6715.jp/posts/30/ ; 都営三田線 https://blog.agatan.dev/posts/isucon11-qualify
- regulation note: allowed (role change / middleware).

### T: HTTP/2 + keepalive limits (http2_max_requests, keepalive_requests, worker_connections, fd limits)
- what: Bench uses HTTP/2 and keeps connections open; nginx default `http2_max_requests 1000` forces GOAWAY and the Go bench client logged "http2: client connection force closed via ClientConn.Close". Set `http2_max_requests 20000`, `keepalive_requests 20000`, `keepalive_timeout 600s`, `worker_connections` ≫, `lingering_close always`, raise `LimitNOFILE`.
- when it works: Scores plateau with sporadic client errors after caching makes the app fast; official warns limits must be raised as users grow.
- how to verify: nginx error log / bench error messages; `ss -s`.
- risk: NaruseJun gave up ~185k points at the end to defensively mitigate GOAWAY-related fails (peak 1,173,984 → final 919,720); Oops! reverted trend cache because of GOAWAY.
- evidence: shallowverse 35k→70k with `http2_max_requests 20000` https://y1r.org/posts/20210831-isucon11-qualify/ ; NaruseJun 「kanwa saku」https://github.com/narusejun/isucon11-qualify/commit/a944d4d3 ; blog https://6715.jp/posts/30/ ; official fd warning https://isucon.net/archives/56044867.html ; ウー馬場ーイー2 本選 `http2_max_requests 1000000` https://github.com/matsuu/isucon11f/commit/c8ee4e7a
- regulation note: none.

### T: MariaDB/MySQL durability-relaxing config (both rounds)
- what: `innodb_flush_log_at_trx_commit = 0/2`, `sync_binlog=0`, `innodb_doublewrite=0`, `innodb_flush_method=O_DIRECT(_NO_FSYNC)`, `disable-log-bin` (本選 MySQL 8 has binlog on by default), `innodb_log_group_home_dir=/dev/shm` (needs AppArmor rule), larger buffer pool, `max_connections`.
- when it works: Write-heavy workload; fsync visible in iotop.
- how to verify: `SHOW VARIABLES LIKE 'innodb_flush_log%'`; official 本選: 「MySQL8.0では、デフォルトでbinlogが出力される点には注意が必要です」.
- risk: Data within the last second is lost on crash, but 追試 reboots cleanly (`systemctl poweroff`) so committed data survives; keep /dev/shm redo only if you accept the risk (ウー馬場ーイー2 used it and passed).
- evidence: NaruseJun 「mysql秘伝のタレ」https://github.com/narusejun/isucon11-qualify/commit/f7221d91 ; kazeburo config https://kazeburo.hatenablog.com/entry/2021/08/24/144237 ; ウー馬場ーイー2 本選 https://github.com/matsuu/isucon11f/commit/c8ee4e7a ; official 本選 https://isucon.net/archives/56163308.html
- regulation note: 「設定の変更…は一切禁止しない」.

### T: Remove transactions (and FOR UPDATE) from read paths (both rounds)
- what: Reference wraps most GETs in `Beginx()`…`Commit()`; replace with plain `db.Get/Select`. 本選 `DownloadSubmittedAssignments` does `SELECT COUNT(*) ... FOR UPDATE`; ウー馬場ーイー2 removed it and did the UPDATE directly.
- when it works: Read-only handlers; official 本選 says the bench tolerates it: 「ユーザーがそこまでの精度を求めていないという想定のもと、トランザクションを外しても許容するようなベンチマーカーとなっています」.
- how to verify: grep `Beginx`; slow log shows `COMMIT`/`BEGIN` counts; ウー馬場ーイー2 added comments to COMMIT to see which handler's tx was slow.
- risk: 本選 整合性チェック still bites on write paths — チーム中目黒乗り過ごし failed with 「成績取得の整合性エラー」after aggressive changes (exact cause not identified in their post). kazeburo found `ReadOnly: true` tx no better than removing.
- evidence: NaruseJun 予選 https://github.com/narusejun/isucon11-qualify/commit/9bd536e2 ; fujiwara 本選 PR#15 https://github.com/fujiwara/isucon11-f/commit/aa347662 ; ichyo +2,000 https://github.com/ichyo/isucon11f/commit/1b2047d5 ; ウー馬場ーイー2 FOR UPDATE https://github.com/matsuu/isucon11f/commit/a4e4d638 ; kazeburo https://github.com/kazeburo/isucon11-final/commit/139d6559 , https://kazeburo.hatenablog.com/entry/2021/09/21/153936

---

## Part B — 本選 ISUCHOLAR

### T: Announcement fan-out: INSERT … SELECT / bulk INSERT into unread_announcements
- what: `POST /api/announcements` selects all registered users of the course and INSERTs one `unread_announcements` row per user. Replace with `INSERT INTO unread_announcements (announcement_id,user_id) SELECT ?, user_id FROM registrations WHERE course_id=?` or one multi-row INSERT.
- when it works: Any fan-out write proportional to course size (teacher endpoints are on the critical path: 「教員が利用するエンドポイントの改善を行うことで学生の課題の提出頻度が増える」— Mirrativ's post-mortem).
- how to verify: grep the INSERT in a `for` loop; kataribe shows POST /api/announcements p99 >1 s.
- risk: Announcement ID uniqueness is guaranteed by manual (「お知らせの ID は一意であることが保証されます」) so no dedupe needed.
- evidence: fujiwara PR#4 https://github.com/fujiwara/isucon11-f/commit/dd9625e8 ; NaruseJun https://github.com/narusejun/isucon11-final/commit/37ed373e ; Oops! 10:00 step https://kjmkznr.github.io/post/20210919/isucon11-final/ ; kazeburo https://github.com/kazeburo/isucon11-final/commit/7222c2c6
- regulation note: none.

### T: Unread tracking in Redis sets (or invert to a "read" table)
- what: Keep `unread_announcements:<user>` as a Redis SET; `SAdd` on announcement creation, `SRem`/`SIsMember` on detail read, `SCard` for `unread_count`; or invert so that "unread" = no row exists (shallowverse's first version), avoiding the fan-out INSERT.
- when it works: `GET /api/announcements` is the #1 request (1 point each) and joins `unread_announcements`; unread count is per user.
- how to verify: schema `unread_announcements(is_deleted)`; the list query's 3-way JOIN.
- risk: Redis must be flushed/rebuilt in `/initialize` (fujiwara 「redisの初期化する」, shallowverse `FlushAll` + rebuild from table) and must persist across the reboot test (Redis RDB/AOF, or rebuild from MySQL at boot). Inverted representation changes `unread` semantics for pre-existing data — handle initial rows.
- evidence: fujiwara PR#23/#24 https://github.com/fujiwara/isucon11-f/commit/9282542d , /commit/f3b2c95c ; shallowverse https://github.com/y1r/isucon11-honsen/commit/cfaaf566 , /commit/43537986 ; blog https://www.y1r.org/posts/20210919-isucon11-final
- regulation note: middleware change allowed.

### T: Announcements: denormalise course_name, cache announcement bodies in memory, drop JOINs, (optionally) dedicated 3rd server
- what: Add `announcements.course_name` at insert; cache `AnnouncementDetail` by id in a `sync.Map`; rewrite list as "unread rows for user → announcement rows by id IN (?)" instead of 3 JOINs; the official intended design puts the three announcement endpoints on server 3.
- when it works: Announcement rows are immutable after insert (only `is_deleted` per user changes), course name never changes.
- how to verify: grep UPDATE on `announcements`/`courses.name` (none).
- risk: Cache must be reset on `/initialize`; multi-app means fill-on-miss per server (fine since immutable).
- evidence: official 「お知らせ系のエンドポイント…を3台目に切り出す方針は、分かりやすく効果の高い改善」https://isucon.net/archives/56163308.html ; fujiwara https://github.com/fujiwara/isucon11-f/commit/0ed818b2 , /commit/03cae142 , /commit/9a18ae67 ; NaruseJun https://github.com/narusejun/isucon11-final/commit/a2e7bf8f , /commit/afffb95d ; Mirrativ https://github.com/tatsumack/isu11f/commit/f87480e3 , /commit/9d8701a4
- regulation note: none.

### T: Announcement list paging — index/PK (user_id, announcement_id) and seek paging instead of OFFSET
- what: `unread_announcements` PK is `(announcement_id,user_id)`; queries filter by user then order by announcement id → swap to `(user_id, announcement_id)` (as PK, dropping extra index). Official also suggests seek paging via the `Link` header.
- when it works: Manual: 「`Link` レスポンスヘッダー…URL のパス部、クエリ部の変更は許容されています」— you may change the next-page URL to carry a cursor.
- how to verify: EXPLAIN the list query; kataribe `GET /api/announcements` ~10k requests at 100 ms mean.
- risk: Changing `page=` semantics must still match browser behaviour (「ベンチマーカーは参考実装をブラウザで閲覧した際の挙動と同様の動作をします」).
- evidence: ichyo PR#11 +5,000 https://github.com/ichyo/isucon11f/commit/83fb6b47 , /commit/a9f634ce ; fujiwara PR#21 https://github.com/fujiwara/isucon11-f/commit/4e34edbb ; Oops! 45,423→71,785 rewriting pagination by announcement id https://kjmkznr.github.io/post/20210919/isucon11-final/ ; official OFFSET→seek https://isucon.net/archives/56163308.html
- regulation note: as above.

### T: GPA statistics — short-TTL cache + singleflight (or recompute only when a course closes)
- what: `GET /api/users/me/grades` runs a constant-argument 4-JOIN query over all students for gpa avg/max/min/t-score. Cache the `[]float64` for ~0.9–1 s with singleflight (fujiwara), a 1 s ticker goroutine (kazeburo), or maintain a `gpas(user_id, credits, total_scores)` table updated at `SetCourseStatus(closed)` (shallowverse), or a `users_total_score` table (ウー馬場ーイー2).
- when it works: Manual: GPA counts only 修了した科目 → changes only on course close; official: 「GPA一覧は科目が終了したタイミング以外では更新されないので、科目の終了と終了の間の呼び出しはリクエスト間で使い回すことができ」. Tolerance of 0.01 on gpa/t-score values.
- how to verify: the query has no per-user argument; `PUT /api/courses/:id/status` is the only writer of `status`.
- risk: Teams that assumed "always changing ⇒ cannot cache" lost (Mirrativ reflection). ウー馬場ーイー2 saw a negative 偏差値 for S99999 at the end (unresolved). Pre-computed tables must handle "no registrations" and division by zero (shallowverse fix commits). kazeburo's 1 s ticker + replica approach ended in 整合性 fail (cause unverified).
- evidence: fujiwara PR#12 https://github.com/fujiwara/isucon11-f/commit/f316f93a , /commit/2e6c9abe ; gihyo interview 「スコアが大きく変わらなくても」load dropped https://gihyo.jp/news/interview/2021/10/2901 ; kazeburo https://github.com/kazeburo/isucon11-final/commit/040ba886 ; shallowverse https://github.com/y1r/isucon11-honsen/commit/f3b27fa3 ; ウー馬場ーイー2 68,795→75,163 https://github.com/matsuu/isucon11f/commit/f1e1ebc3 ; official "3 items for 100k+": 「複数台構成への移行とGPA計算のcall supression、zipの無圧縮」https://isucon.net/archives/56163308.html ; Mirrativ https://tech.mirrativ.stream/entry/2021/10/29/152334
- regulation note: 誤差 0.01 allowed (manual).

### T: Per-course score N+1 → IN-queries, per-class submission counter table, singleflight on per-course totals
- what: grades handler loops courses → classes → `SELECT COUNT(*) FROM submissions WHERE class_id=?` and per-user score lookups. Replace with `IN (?)` batch queries, a `submissions_count(class_id,cnt)` upserted at submit, and `user_course_total_scores` maintained on `RegisterScores`.
- when it works: Counts only increase on `POST .../assignments` (scoring endpoint) and scores set on `PUT .../scores`.
- how to verify: pprof/trace shows GetGrades query count ∝ courses×classes.
- risk: Counter upsert inside the submit tx adds a hot row; Mirrativ made the counter insert async (risk of undercount).
- evidence: Mirrativ https://github.com/tatsumack/isu11f/commit/83a62af7 , /commit/b1744965 ; fujiwara PR#16/#20 https://github.com/fujiwara/isucon11-f/commit/14d90c0b , /commit/1871427a ; NaruseJun total_score table https://github.com/narusejun/isucon11-final/commit/4a83f7bd , /commit/0d00f64d ; shallowverse https://github.com/y1r/isucon11-honsen/commit/64060617 , /commit/84b6810d
- regulation note: none.

### T: Zip export — archive/zip with Store (no compression), hard links instead of cp, nginx X-Accel-Redirect
- what: Reference `exec`s `rm -rf`, `mkdir`, `cp` per file, then `zip -j -r`. Replace with Go `archive/zip` writing `zip.Store` entries straight from source files (fujiwara: 「10分で一発実装」), or at least `ln` instead of `cp` (kazeburo), parallel cp (NaruseJun), then return `X-Accel-Redirect: /assignments/<class>.zip` so nginx streams the file; `proxy_buffering off` on the export location.
- when it works: Export is the slowest endpoint (kataribe mean 2.1 s, many timeouts at 5 s); bandwidth is not the limit; official: 「実際にはstored(無圧縮)を採用しているチームがほとんどでした」.
- how to verify: kataribe row for `/assignments/export`; `top` shows `zip` processes / deflate in pprof.
- risk: Four teams were disqualified in the browser 追試 on this endpoint: Oops! and 今年こそは出場 「提出課題のダウンロードが404」, SNE 「500」(https://isucon.net/archives/56098785.html). Oops!'s cause: replaced `SELECT COUNT(*)` with `UPDATE … RowsAffected()`, so the 2nd download (already `submission_closed=true`) returned 404 — 「ベンチマーカーでは1度しかアクセスしないため、Failにならずに済んでいた」. ichyo filled the disk because `/initialize` did not delete old files. Zip files must be regenerable after reboot.
- evidence: fujiwara PR#5/#22 https://github.com/fujiwara/isucon11-f/commit/261e8b14 , /commit/ebde8e92 ; kazeburo https://github.com/kazeburo/isucon11-final/commit/2744b961 ; NaruseJun https://github.com/narusejun/isucon11-final/commit/364d2a41 , /commit/a827b439 ; ウー馬場ーイー2 `proxy_buffering off` 75,163→77,029 https://github.com/matsuu/isucon11f/commit/5657951b ; Oops! post-mortem https://kjmkznr.github.io/post/20210919/isucon11-final/ ; ichyo disk full https://blog.ichyo.jp/posts/isucon11f/ ; official https://isucon.net/archives/56163308.html
- regulation note: response must stay a valid zip with the same entry names.

### T: Assignment upload — stream to disk (io.Copy), write after commit / in a goroutine, `proxy_request_buffering off`
- what: Reference `io.ReadAll` → `os.WriteFile` inside the tx. Stream with `io.Copy` into the file; commit first then write (or write in a goroutine after responding); nginx `proxy_request_buffering off` on the assignments location; pin this endpoint to one app server so the file and the zip live on the same disk.
- when it works: Memory limited to 2 GB (README: 「競技用サーバーのメモリは元の 4GB から 2GB に制限」); submissions are 3 points each and the most valuable request.
- how to verify: `POST .../assignments` ~9k requests, mean 100 ms; memory pressure in `free`.
- risk: Async write means the export zip may run before the file lands (shallowverse used temp file + rename; Mirrativ/kazeburo used goroutines); files must survive reboot (they are on disk, fine). Multi-app without pinning → file missing on the other host.
- evidence: NaruseJun https://github.com/narusejun/isucon11-final/commit/3b318d62 ; shallowverse https://github.com/y1r/isucon11-honsen/commit/bf0a152d ; Mirrativ https://github.com/tatsumack/isu11f/commit/f1ef5b46 ; kazeburo https://github.com/kazeburo/isucon11-final/commit/ce327c1d ; ウー馬場ーイー2 https://github.com/matsuu/isucon11f/commit/c8ee4e7a ; ichyo/雑談係 pinning https://blog.ichyo.jp/posts/isucon11f/ , https://sanposhiho.com/posts/isucon11-final/ ; official 「streamで書き出すようにするとメモリ確保を削減できます」https://isucon.net/archives/56163308.html
- regulation note: data persistence rule.

### T: Drop all FOREIGN KEY constraints; CHAR(26) ids as latin1; PK/UNIQUE reorder
- what: Every table has FKs (schema above). Comment them out in `1_schema.sql`; fujiwara also switched `CHAR(26)` ULID columns to `CHARACTER SET latin1` to shrink index bytes.
- when it works: Official: 「外部制約キーは…INSERT・UPDATE・DELETE時に参照先・参照元レコードを確認する必要があるため、追加のCPUオーバーヘッドが生じます」; app already validates existence before inserts.
- how to verify: `SHOW CREATE TABLE`; percentage of time in INSERT/UPDATE.
- risk: Small gains (ichyo +2,000); kotamanegi hit FK trouble when trying to move announcements to a separate DB (so drop FKs before splitting tables across DBs).
- evidence: fujiwara PR#19/#13 https://github.com/fujiwara/isucon11-f/commit/6995cb95 , /commit/dfcc0282 ; NaruseJun https://github.com/narusejun/isucon11-final/commit/6dd1f156 ; ichyo PR#13 https://github.com/ichyo/isucon11f/commit/705f7ad3 ; Mirrativ https://github.com/tatsumack/isu11f/commit/d4e5698c ; kotamanegi https://kotamanegi.hatenablog.com/entry/2021/09/20/122240
- regulation note: schema change allowed.

### T: In-memory caches for immutable/slow-changing rows (course, class, teacher name, registrations, course.status) + persist caches across restart
- what: `sync.Map` caches for `courses`, `classes`, `users.name` (teacher), registrations per user; NaruseJun serialised `ClassSubmissionCache` to a gob file on SIGTERM and reloaded it at boot (「シャットダウン体制」) to survive the reboot test.
- when it works: Rows written once by teacher endpoints and read by thousands of student requests; `course.status` is the only mutable field (cache invalidation on `PUT /status`).
- how to verify: list of UPDATE statements per table in main.go.
- risk: Multi-app cache coherence for `status` (fujiwara keyed caches and reset on initialize); without persistence, in-memory counters are wrong after reboot → 追試 failure.
- evidence: fujiwara PR#14/#18 https://github.com/fujiwara/isucon11-f/commit/78dc39ee , /commit/bfb16d93 ; NaruseJun https://github.com/narusejun/isucon11-final/commit/c9bb183a , /commit/2217fa4d , /commit/56bf089f ; sfujiwara blog "sync.Map" https://sfujiwara.hatenablog.com/entry/isucon11-final
- regulation note: 「サーバー起動後にすべてのアプリケーションコードが正常動作する状態を維持すること」.

### T: Conditional GET / ETag 304 on per-user GETs (GET /api/courses/:id/classes)
- what: NaruseJun kept an in-memory ETag per (course,user), invalidated on class add/submission, and returned 304 when `If-None-Match` matched.
- when it works: Manual (both rounds) says the bench behaves like a browser and counts 304 as a successful response.
- how to verify: Confirm the bench actually sends `If-None-Match` (check nginx log for 304s after deploying).
- risk: 手が3本あるテディーベア was disqualified: 「GET /api/courses/:coursesID、GET /api/announcementsが初回も304」— returning 304 without a validator on the first request breaks the browser check. Only 304 when the request carries the validator.
- evidence: NaruseJun https://github.com/narusejun/isucon11-final/commit/73c23344 ; disqualification https://isucon.net/archives/56098785.html ; manual Conditional GET section (docs/manual.md in isucon/isucon11-final)
- regulation note: as above.

### T: 本選 server layout: DB on its own box, app×2, heavy endpoints pinned; double-write to two MySQLs and balance reads
- what: DB separation gave the single largest jump (ウー馬場ーイー2 42,491→68,795; ichyo ~73k at 10:00). nginx `upstream backend { server isu1:7000; server isu3:7000; keepalive 128; }` with `/assignments` and `/me/grades` pinned to one app. NaruseJun went further: app writes to both `DB` and `SubDB` (double write in every handler + initialize) and alternates reads (`atomic` counter), effectively a hand-rolled replica without lag.
- when it works: DB CPU at 100% on one box; read-heavy endpoints that tolerate the write path doing 2 INSERTs.
- how to verify: `top` on each host; which endpoints need local disk.
- risk: kazeburo's MySQL replica (async) approach was 「あまり効果が無かった」 and reads from a lagging replica contributed to fails; double-write must be transactional-enough (NaruseJun ignored errors on SubDB in some paths — risky). ichyo's first nginx/app split had 50% failure rate and was reverted.
- evidence: ウー馬場ーイー2 https://github.com/matsuu/isucon11f/commit/35ded4d3 ; ichyo round-robin https://github.com/ichyo/isucon11f/commit/64e87fe2 , blog https://blog.ichyo.jp/posts/isucon11f/ ; NaruseJun https://github.com/narusejun/isucon11-final/commit/0bd00573 , /commit/0d00f64d , /commit/4b5e675d ; kazeburo https://github.com/kazeburo/isucon11-final/commit/f992bac1 ; gihyo (fujiwara DB split at ~11:00 → 80k) https://gihyo.jp/news/interview/2021/10/2901
- regulation note: only the 3 provided servers.

### T: Connection pool size and JSON serializer tuning (both rounds)
- what: `db.SetMaxOpenConns` 10→20→40→60 (fujiwara), 128 (ichyo), 150 (ウー馬場ーイー2 予選); `goccy/go-json` as echo serializer (fujiwara, NaruseJun). ウー馬場ーイー2 found 100→200 conns made 本選 worse (`FOR UPDATE` lock waits).
- when it works: Go app with `database/sql` default 2 idle conns; CPU time in encoding/json visible in pprof.
- how to verify: pprof; `SHOW PROCESSLIST` for lock waits before raising the pool.
- risk: More connections amplify lock contention — fix locks first.
- evidence: fujiwara https://github.com/fujiwara/isucon11-f/commit/4a0937d0 , /commit/398bdd7f ; Oops! 71,785→92,244 from MaxOpenConns tuning https://kjmkznr.github.io/post/20210919/isucon11-final/ ; ウー馬場ーイー2 39,072 「db接続数を増やしたら遅くなった(FOR UPDATEで待っている)」https://github.com/matsuu/isucon11f/commit/942ea3a2 ; NaruseJun https://github.com/narusejun/isucon11-final/commit/40ff0bd1
- regulation note: none.

### T: /initialize as the place to apply schema/cache/file resets (both rounds)
- what: Run `ALTER TABLE` + backfill, export seed icons to disk, flush Redis / in-memory caches, delete old assignment/zip files, pre-warm trend cache, bulk-insert precomputed tables; `sleep` before returning if replicas/background jobs need to catch up. Budget is 20 s.
- when it works: Always; the bench calls `/initialize` before every run (本選 twice).
- how to verify: time the endpoint; check every cache/global you added has a reset line there.
- risk: Forgetting one item caused the icon 404/500 disqualifications (予選) and disk-full (本選 ichyo). ichyo "init-isu3" shows initialize had to be fanned out to other hosts.
- evidence: fujiwara https://github.com/fujiwara/isucon11-q/commit/31764cf7 , https://github.com/fujiwara/isucon11-f/commit/f3b2c95c ; NaruseJun https://github.com/narusejun/isucon11-qualify/commit/2c58a423 , /commit/2c928717 ; shallowverse 「SpeedUp initialize / bulk init」https://github.com/y1r/isucon11-honsen/commit/d1da8b1c , /commit/51b8b751 ; kazeburo sleep https://github.com/kazeburo/isucon11-final/commit/c51b3525 ; ichyo https://github.com/ichyo/isucon11f/commit/f12b6ca1
- regulation note: 「コンテスト進行用のメンテナンスコマンドが正常に動作するよう互換性を保つこと」; initialize must respond in 20 s and return `{"language": ...}`.

### Techniques that appear in both 予選 and 本選
Logging off; `interpolateParams=true`; DB-on-separate-server + app×2 with endpoint pinning; nginx upstream keepalive / unix socket / http2 limits; MySQL durability config; bulk INSERT; removing read transactions; in-process `sync.Map` caches reset in `/initialize`; singleflight (trend in 予選, GPA/total-score in 本選); N+1 → IN queries; precomputed columns/tables (level/true_count vs gpas/total_scores/submission_count); nginx `X-Accel-Redirect` for files (icon vs zip); Conditional GET / Cache-Control.

---

## Regulation highlights (this contest)

予選 (https://isucon.net/archives/55854734.html ; manual https://github.com/isucon/isucon11-qualify/blob/main/docs/manual.md):
- Must not change: 「アクセス先のURI」(server-generated IDs may differ within the same character class), 「レスポンス (HTMLのDOM, JSONオブジェクト等) の構造」(whitespace OK), 「JavaScript/CSSファイルの内容」, 「画像および動画等のメディアファイルの内容」.
- Explicitly allowed: 「複数台あるサーバーの役割の変更」「データベーススキーマの変更やインデックスの作成・削除」「データベースに利用するミドルウェアの変更」「キャッシュ機構の追加、ジョブキュー機構の追加による遅延書き込み」「他の言語による再実装」.
- Must keep: 「サーバ再起動後にすべてのアプリケーションコードが正常動作する状態を維持すること」「ベンチマーク実行時にアプリケーションに書き込まれたデータは再起動後にも取得できること」; maintenance commands compatible.
- External resources: forbidden during bench 「主催者の指示以外で利用が認められたサーバー以外の外部リソースを使用する行為」; monitoring/dev on external machines OK. Only the 3 CloudFormation c5.large instances; changing instance type = 失格.
- Do not touch `isucon-env-checker.service` files or `/opt/isucon-env-checker`; do not delete non-isucon users/keys. Port 5000 (JIA mock) security group must stay. Env checker must pass at contest end; mixed stacks on portal = 失格.
- Final score = bench after 「全サーバーの再起動後に負荷走行」; 追試 checks data persists after reboot and 「ブラウザ上での挙動を初期状態と同様に保っていること」.
- Bench: connects only to `isucondition-1.t.isucon.dev` (TLS `*.t.isucon.dev`); JIA activate requires `target_base_url` host ∈ {isucondition-1,2,3.t.isucon.dev}, https, no port. No redirects followed. `/initialize` 20 s, must return `{"language": "..."}`.
- Staleness: 「ベンチマーカーは下記を除き、データの更新が即時反映されていることを期待して検証を行います。ただし、アプリケーションはベンチマーカーが検知しない限りは古い情報を返しても構いません」; condition propagation may be delayed for GET /api/isu, graph, condition, trend; condition↔graph must agree within 1 s. Conditional GET / 304 supported; caches are per bench user (no cross-user sharing of `Cache-Control: public`).

本選 (manual with embedded レギュレーション: https://github.com/isucon/isucon11-final/blob/main/docs/manual.md):
- Same change-allowed/forbidden lists as 予選 (HTML/JS/CSS/media content, URIs, response structure). Only the 3 provided servers (c5.large, memory capped at 2 GB per README). Ports other than 22 closed; access via SSH port-forward.
- Reboot test: 「主催者がサーバーに SSH 接続し `sudo systemctl poweroff` によるシャットダウンを行った後、AWS のインスタンス起動 API（ec2:StartInstances）で起動する」, wait ≥10 min, bench 3 times, best counts; then another reboot (possibly next day) and browser check; data written during bench must be retrievable; fail twice = 失格.
- Staleness: 「ベンチマーカーはデータの更新が即時反映されていることを期待して検証を行います。ただし、アプリケーションはベンチマーカーが検知しない限りは古い情報を返しても構いません」; Conditional GET supported; `Link` header not parsed per RFC 8288 — path/query may be changed; announcement IDs unique; grade floats tolerance 0.01; students may register courses not in search results.
- Official 解説 on consistency: 「ユーザーのニーズを満たす範囲で整合性を提供すれば良い」(transactions may be removed) https://isucon.net/archives/56163308.html.

## Scoring model (this contest)

予選 (manual 「スコア計算」): start 1,000 after compatibility check. Points: `GET /api/condition/:uuid` — new conditions seen since last view: Info 20 pts / 50 件, Warning 8 / 50, Critical 4 / 50. `GET /api/isu/:uuid/graph` — per daily graph, n = min conditions per hour data point (future points ignored): 当日 Good 60 (n>20) / Normal 40 / Bad 24 / Worst 4; 完成したグラフ (past days, once each) Good 150 / Normal 100 / Bad 60 / Worst 10. Penalties: wrong status/body −1 each, fail at 100; timeouts −1 per 10, no fail; `POST /api/condition` never penalised (100 ms timeout, no deduction); other requests 1 s timeout. Bench: `/initialize` (20 s) → compatibility check → 60 s load; virtual time runs 30,000× (1 real s = 30,000 s); three actors — ISUs post conditions, users (score) view list/condition/graph and act on non-Info ISUs, viewers watch `/api/trend` and 「トレンドの変化があると評判が良くなり、ユーザが増加する」; 「いずれかが詰まると次の行動に移るまでに時間がかかるため、スコアが伸び悩む仕組み」. Two benchmarker versions were used in the final tally due to a bug; the higher was adopted (https://isucon.net/archives/56021237.html). Sources: https://github.com/isucon/isucon11-qualify/blob/main/docs/manual.md , https://isucon.net/archives/56044867.html.

本選 (manual 「スコア計算」): no initial points. `POST /api/courses/:courseID/classes/:classID/assignments` success = 3 pts; `GET /api/announcements` success = 1 pt; nothing else scores (「倍率のような概念はありませんでした」). Penalties: wrong status/body −50 each, fail above 100; timeouts (5 s) −100 per 100, no fail. Run: initialize → compatibility check → initialize → 60 s load → 10 s retry of timed-out requests (not scored) → 整合性チェック (fail = 0). 特別賞 for first team to 80,000 during the contest. Bench model (official 解説): teachers open courses; registration closes when 50 students registered or 2 s elapsed; each course has 5 classes; students and teachers run serial scenarios and 「履修登録の前の「成績確認」や課題提出の前の「お知らせ確認」が１人でも遅れると全員待たされる」; max points ∝ courses × registered students; reference GetGrades timed out so students slept 10 s. Sources: https://github.com/isucon/isucon11-final/blob/main/docs/manual.md , https://isucon.net/archives/56163308.html , https://isucon.net/archives/56098785.html.

## Not found / unverified
- No public repo for 都営三田線 (予選2位; blog said one was planned) or FetchDecodeExecWrite (本選3位); their techniques come from blogs/results only.
- Exact root cause of チーム中目黒乗り過ごし's 本選 整合性 fail and ウー馬場ーイー2's negative t-score is not stated by the teams (unverified).
- NaruseJun's own 本選 参加記 was not found; their 本選 techniques are from the repo commits.