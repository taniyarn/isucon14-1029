## ISUCON10 (2020) — ISUUMO (予選) / XSUCON (本選): technique catalogue

Sources actually opened: official repos `isucon/isucon10-qualify`, `isucon/isucon10-final` (README, docs/manual.md, docs/xsucon.md, schema, bench/benchmarker code via `gh api`), 予選レギュレーション, 予選当日マニュアル gist, both official 講評 posts, results/まとめ posts, and the repos/blogs listed per technique. Not reachable: `medium.com/finatext/*` (403), `nakario.hateblo.jp` and the 予選全スコア page (rate-limited) — marked (unverified) where relevant. No files were modified.

Legend: [Q] = 予選 ISUUMO, [F] = 本選 XSUCON, [Q+F] = seen in both rounds.

---

### T: [Q] Bot User-Agent → 503 (nginx `if ($http_user_agent ~ …) { return 503; }` or app middleware)
- what: Match the 10 regexes listed in the manual against `User-Agent` and answer `503 Service Unavailable` before the handler runs. Most top teams put it in nginx on the `/api` location (tatsuru moved it from `location /` to `location /api` since bench never requests static files).
- when it works: Manual explicitly allows it: 「bot は User-Agent が以下の正規表現にマッチする形式であり、このリクエストに対して `503 Service Unavailable` を返すことが許可されています。これに対するベンチマーカーからの減点は発生しません。」 The bench client treats 503 to non-bot clients as `ErrTemporary` (ignored, scenario retried) and bot clients ignore `ErrBot` entirely (`bench/client/client.go` L145, `bench/scenario/botScenario.go`).
- how to verify mid-contest: grep the manual for 503; `alp`/kataribe the access log by UA; note that bot workers only start at level 2 (`ListOfIncWorkers[2].BotWorker = 5` in `bench/parameter/parameter.go`), so no visible gain until score ≥ 600 — official: 「実はベンチマーカーは初期の時点では Bot からのリクエストをほとんど送りません」.
- risk: Regex escaping in nginx (tatsuru needed three fix commits: `/…/` delimiters are not nginx syntax; use quoted PCRE). Blocking the bench's own non-bot UAs with an over-broad regex would surface as temporary errors.
- evidence:
  - official 講評: https://isucon.net/archives/55025156.html (「503エラーで弾いても問題ないことが当日のマニュアルに記載されています」; nginx vs app both fine)
  - manual: https://gist.github.com/progfay/25edb2a9ede4ca478cb3e2422f1f12f6
  - bench 503 handling: https://github.com/isucon/isucon10-qualify/blob/master/bench/client/client.go , bot workers per level: https://github.com/isucon/isucon10-qualify/blob/master/bench/parameter/parameter.go
  - FCCPC (1st): "Bot mitigation via nginx" 726→920 https://fccpc.hateblo.jp/entry/2020/09/14/161420
  - tatsuru/isucon10q commits a6c1cb83, b819a475, 19144817 https://github.com/tatsuru/isucon10q/commit/19144817
  - y1r/isucon10-yosen 7047e6c2 https://github.com/y1r/isucon10-yosen/commit/7047e6c2 ; hiromu/isucon10-qual 66114e63; innocent-team/isucon10q 37926611; hirosuzuki/isucon10-qual c9a2b627
- regulation note: Only 503 is sanctioned; other status codes count as 「HTTP ステータスコードやレスポンスの内容などに誤りがある … 1 回で 50 点減点、10 回以上で失格」.

### T: [Q] Index for `low_priced` ORDER BY (`chair(price,id)` with `stock>0`, `estate(rent,id)`)
- what: Add `INDEX (price, id)` (teams also used `(price, id, stock)` or `(price, stock)`) on chair and `(rent, id)` on estate so `ORDER BY price ASC, id ASC LIMIT 20` is an index scan. SunPro went further and made `(rent,id)`/`(price,id)` the clustered PRIMARY KEY with `id UNIQUE`.
- when it works: Top page calls both endpoints constantly; level 0 of the bench runs only chair/estate search workers, so this is the first bottleneck (official: 「特に初期段階ではベンチマーカーは椅子と物件の検索しかしません」).
- how to verify mid-contest: `EXPLAIN` shows `Using filesort` on the two queries; pt-query-digest ranks them top by count.
- risk: Low. Note chair query also filters `stock > 0`; a pure `(price,id)` index still works with the filter applied on the scan.
- evidence:
  - official 講評 https://isucon.net/archives/55025156.html ; progfay level table (level 0 bottleneck) https://zenn.dev/progfay/articles/isucon10-qualify
  - hiromu/isucon10-qual 1e0bd2e7 (`INDEX idx_01 (rent,id)` / `(price,id)`; 572→712) https://github.com/hiromu/isucon10-qual/commit/1e0bd2e7 and f54f8c26 clustered PK https://github.com/hiromu/isucon10-qual/commit/f54f8c26 ; blog https://blog.mine-studio.com/2020/09/isucon10/
  - innocent-team/isucon10q 06be78f6 https://github.com/innocent-team/isucon10q/commit/06be78f6 ; hirosuzuki/isucon10-qual cda74f41, 4cded83a
- regulation note: 「DBスキーマの変更やインデックスの作成・削除」 explicitly allowed (http://isucon.net/archives/54753430.html).

### T: [Q] Mixed-direction `ORDER BY popularity DESC, id ASC`: negated generated column or MySQL 8 descending index
- what: MySQL 5.7 (as shipped) cannot use an index for `popularity DESC, id ASC`. Either (a) add `popularity_desc INTEGER AS (-popularity)` (or write `-popularity` at CSV insert) and index `(…, popularity_desc, id)` while changing the SQL to `ORDER BY popularity_desc, id`, or (b) upgrade to MySQL 8 and create `INDEX (cond, popularity DESC)`.
- when it works: Every search/recommend/nazotte query in ISUUMO uses this ORDER BY. Official: 「降順と昇順が組み合わさった ORDER BY は デフォルトでインストールされてる MySQL 5.7 では単純に index を貼っても効きません」.
- how to verify mid-contest: `EXPLAIN` still shows `Using filesort` after adding `(x, popularity DESC, id)` on 5.7 — the DESC keyword is parsed but ignored. akouryy's biggest regret: 「DESC INDEX が効かないという発想自体がなかった」.
- risk: MySQL 8 upgrade mid-contest cost time and 「設定の違いにより、パフォーマンスが劣化することもあります」 (official). kamipo's 感想戦 note: on MySQL 8 use `(rent_t, popularity DESC)` and target the ORDER BY … LIMIT optimisation.
- evidence:
  - official 講評 (generated column SQL) https://isucon.net/archives/55025156.html
  - akouryy (3rd) https://akouryy.hatenablog.jp/entry/2020/09/13/130415
  - matsuu (12th) MySQL 8 + descending index, 1144→1200 https://matsuu.hatenablog.com/entry/2020/09/13/131145
  - kamipo 感想戦 PR https://github.com/soudai/isucon10-qualify/pull/14 (slow-log before/after, `idx_rent_t_popularity (rent_t, popularity DESC)`), blog https://blog.kamipo.net/entry/2020/09/13/182835
- regulation note: Middleware replacement (MySQL version) allowed: 「データベースに利用するミドルウェアの変更」.

### T: [Q] nazotte: collapse the N+1 into one query (bounding box AND `ST_Contains(ST_PolygonFromText(…), POINT(latitude, longitude))` … `LIMIT 50`)
- what: The reference impl selects every estate in the bounding box and then issues one `ST_Contains` query per row. Replace with a single query that adds the polygon predicate and `ORDER BY popularity DESC, id ASC LIMIT 50` (NazotteLimit), so MySQL does the polygon test and the limit.
- when it works: Polygon is request-specific, so no caching; the win comes from removing per-row round-trips. Bench nazotte workers start at level 1 (score ≥ 300).
- how to verify mid-contest: Count `SELECT * FROM estate WHERE id = ? AND ST_Contains` in the slow/general log; kataribe shows `/api/estate/nazotte` top by total time (shallowverse, polamjag).
- risk: Keep the `ORDER BY … , id ASC` — tatsuru had validation failures when ORDER BY was dropped ("order by ないことで最終的なクエリ結果が不定になっていてバリデーションこけてるのではないか"). SQL injection-style string formatting of the polygon is what everyone did; polygon text must be the convex hull the frontend sends.
- evidence:
  - official 講評 https://isucon.net/archives/55025156.html
  - FCCPC (1st) 451→514 "Fixed N+1 problem in nazotte using polygon inclusion calculation" https://fccpc.hateblo.jp/entry/2020/09/14/161420
  - y1r/isucon10-yosen c68f6034 https://github.com/y1r/isucon10-yosen/commit/c68f6034 ; hiromu 0d971ff7 https://github.com/hiromu/isucon10-qual/commit/0d971ff7 (555 pts); innocent-team 051ece1b; hirosuzuki bdb4eb67 (1521→1857 per https://zenn.dev/hirosuzuki/articles/8a4f91d5dc384cba534d); tatsuru 5e028114/dccefe39
  - akouryy: subquery that applies `ST_Contains` only after ORDER BY/LIMIT candidates, 2087→2978 https://akouryy.hatenablog.jp/entry/2020/09/13/130415
  - kamipo 感想戦 PR https://github.com/soudai/isucon10-qualify/pull/18
- regulation note: none beyond response-structure preservation.

### T: [Q] nazotte: `POINT` column + `SPATIAL INDEX` (MySQL 8 needs `SRID 0`; 5.7 needs `STORED` generated column)
- what: Store `point POINT AS (POINT(latitude, longitude)) STORED NOT NULL` (5.7) or a `POINT NOT NULL SRID 0` column (8.0), add `SPATIAL INDEX`, and query `WHERE ST_Contains(ST_PolygonFromText(?), point)` — the MBR pre-filter then replaces the manual bounding-box filter.
- when it works: Official intended solution; MBR filtering is what `ST_Contains` does with a spatial index anyway.
- how to verify mid-contest: `EXPLAIN` type `range` with the spatial key; matsuu: avg response time 0.401s → 0.063s once `SRID 0` was added (「SRID 0 を追加すればインデックスが使われることを確認」).
- risk: On MySQL 8 the index is silently unused without SRID (shallowverse: 「MySQL8だとSRIDを指定しないとSPATIAL INDEXが使えない」 — they created a new `estate2` table with `position POINT` + `SPATIAL INDEX`). SunPro tried a POINT column and reverted (score dropped to 1216, cause not stated). Several teams instead did the polygon test in the app (official: 「アプリケーション側で多角形内にあるかを検証するチームも多かった」; がんもどき moved convex-hull work to the app, ~1427→~3000 together with DB split).
- evidence:
  - official 講評 (SQL + 5.7 STORED caveat) https://isucon.net/archives/55025156.html
  - y1r/isucon10-yosen e03271d8, d87384e2 https://github.com/y1r/isucon10-yosen/commit/e03271d8 ; blog https://www.y1r.org/posts/20200913-isucon10-qualify
  - matsuu 1200→1819 https://matsuu.hatenablog.com/entry/2020/09/13/131145 ; netmarkjp (MySQL CPU 90%→60%) https://netmark.jp/2020/09/2020-09-12-21-59.html
  - SunPro revert https://github.com/hiromu/isucon10-qual/commit/45ef8c2b , https://blog.mine-studio.com/2020/09/isucon10/
  - がんもどき https://trap.jp/post/1114/
- regulation note: schema changes allowed.

### T: [Q] Indexes on search conditions (price/rent/height/width/depth/door_*, kind, color) and range-ID columns
- what: Add single/composite indexes per filter column (`chair(price, stock)`, `chair(height, stock)`, `chair(kind, stock)`, `estate(rent, door_width)`, …) and, better, pre-bucket ranges into an integer category column (`rent_t`, `price_t`, `door_min/door_max`) so the search becomes `=` on a category with `(cat, popularity DESC)` index (MySQL 8).
- when it works: Bench is biased: 「実はベンチマーカーは price と rent に検索条件が多めに実施してくるように設計してあります」. innocent-team's log analysis: priceRangeId 1461 req, heightRangeId 1027, depthRangeId 521.
- how to verify mid-contest: tally query params in the access log; slow log with `long_query_time=0` + pt-query-digest.
- risk: Too many indexes slow the bulk CSV inserts; FCCPC's `door_min/door_max` index attempt broke `/initialize` and was abandoned; SunPro's `door_min/door_max` and `features` bitset changes caused more bench errors / slight regression and were reverted.
- evidence:
  - official 講評 index list https://isucon.net/archives/55025156.html
  - akouryy generated columns for categories 464→730 https://akouryy.hatenablog.jp/entry/2020/09/13/130415
  - matsuu/wtatsuru "range → ID" https://matsuu.hatenablog.com/entry/2020/09/13/131145 , https://wtatsuru.hatenadiary.com/entry/2020/09/13/000947
  - innocent-team/isucon10q c38ad112, 4bde5e18, access-log analysis in https://blog.utgw.net/entry/2020/09/13/092305
  - kamipo PR 14/15 (`*_t` columns, deleting useless indexes) https://github.com/soudai/isucon10-qualify/pull/14
  - hiromu 20a72cc8 (features bitset, regressed) https://github.com/hiromu/isucon10-qual/commit/20a72cc8
- regulation note: as above.

### T: [Q] `recommended_estate`: collapse the 6-way OR into "two smallest chair dims" (and index `popularity DESC`)
- what: A chair of dims (w,h,d) passes a door iff the two smallest dims ≤ (door_w, door_h) in some order; so replace the six `OR (door_width >= ? AND door_height >= ?)` with `(door_width >= min1 AND door_height >= min2) OR (door_width >= min2 AND door_height >= min1)` (innocent-team), or akouryy's equivalent `c0 <= door_min AND c1 <= door_max` against generated `door_min/door_max` columns. SunPro collapsed to a single condition.
- when it works: Called after every chair purchase in the chair scenario; the full scan + filesort (29,520 rows examined in kamipo's slow log) disappears.
- how to verify mid-contest: slow log shows `SELECT * FROM estate WHERE (door_width >= ? AND door_height >= ?) OR …` with `type: ALL`.
- risk: akouryy's "sqrt decomposition" over (door_min, door_max) cells (2h coding + 1.5h debug) gave 3127→3543 — high effort. Official hint: 「OR検索を UNION ALL に変更し、 index が効くようにする」.
- evidence:
  - FCCPC 619→687 "Removed OR conditions in recommended_estate", later chair caching here 3171→3502 https://fccpc.hateblo.jp/entry/2020/09/14/161420
  - innocent-team 858590f1 + bf84b654 https://github.com/innocent-team/isucon10q/commit/858590f1 ; https://blog.utgw.net/entry/2020/09/13/092305
  - hiromu 9ff9dc7e https://github.com/hiromu/isucon10-qual/commit/9ff9dc7e
  - akouryy https://akouryy.hatenablog.jp/entry/2020/09/13/130415 ; progfay credits them https://zenn.dev/progfay/articles/isucon10-qualify
  - kamipo PR 16 (0.0367s/29,520 rows → 0.0011s/29 rows with `INDEX idx_popularity (popularity DESC)`) https://github.com/soudai/isucon10-qualify/pull/16
- regulation note: response structure must match; result ordering unchanged.

### T: [Q] CSV 入稿 bulk INSERT (multi-row VALUES, optionally no transaction / prepared statement)
- what: `POST /api/chair` and `/api/estate` receive a CSV (official says 1000 rows; progfay says 500 per upload) and insert row-by-row inside a transaction. Build one `INSERT … VALUES (…),(…),…` and execute once.
- when it works: Draft workers start at level 2; `DraftTimeout = 5 * time.Second` and a timeout here is a critical error: 「イス・物件の CSV 入稿に失敗・タイムアウトした場合は失格となります」.
- how to verify mid-contest: look for `postEstateリクエスト タイムアウト` in bench output (team MN hit it); count INSERTs per request in the general log.
- risk: polamjag's team never got sqlx batch insert working (「異常な悲しさ」); nginx `client_body_buffer_size` needs raising for the multipart body (FCCPC `512k`, hiromu `2m`). Removing the transaction (SunPro) is fine since the bench does not test partial failure.
- evidence:
  - official 講評 https://isucon.net/archives/55025156.html ; parameters https://github.com/isucon/isucon10-qualify/blob/master/bench/parameter/parameter.go
  - FCCPC 687→713 https://fccpc.hateblo.jp/entry/2020/09/14/161420
  - hiromu 691b01e6, 81a78bf9 (712→880) https://github.com/hiromu/isucon10-qual/commit/691b01e6
  - y1r 0e3e5014/ce10ca7f https://github.com/y1r/isucon10-yosen/commit/0e3e5014 ; hirosuzuki 88d05688; MN timeout note https://zenn.dev/hirosuzuki/articles/8a4f91d5dc384cba534d
  - nginx buffer: hiromu e4408954 https://github.com/hiromu/isucon10-qual/commit/e4408954
- regulation note: data written during bench must survive reboot (「ベンチマーク実行時にアプリケーションに書き込まれたデータは再起動後にも取得できること」) — so keep writes in MySQL.

### T: [Q+F] Replace `SELECT … FOR UPDATE` + `UPDATE` with one conditional `UPDATE` and check `RowsAffected`
- what: [Q] `buyChair`: `UPDATE chair SET stock = stock - 1 WHERE id = ? AND stock > 0`; `affected == 0` → 404. [F] `ReceiveBenchmarkJob`: drop the `SELECT 1 … FOR UPDATE` lock, run `UPDATE benchmark_jobs SET status=SENT, handle=? WHERE id=? AND status=PENDING`, and treat `RowsAffected()==0` as "someone else took it, retry".
- when it works: Whenever the row transition is a single atomic condition; the official 講評 lists it: 「SELECT FOR UPDATE をやめて、 UPDATE と affected_rows の処理にし、ロックの時間を減らす」. In 本選 the virtual benchmarkers (one per team) hammer `ReceiveBenchmarkJob` with no wait, so lock contention is high.
- how to verify mid-contest: `SHOW ENGINE INNODB STATUS` / lock waits; in 本選, count `SELECT 1 FROM benchmark_jobs … FOR UPDATE` in general log.
- risk: Low for buyChair. In 本選, keep the status predicate in the UPDATE or two benchmarkers get the same job.
- evidence:
  - [Q] y1r c8a470cd https://github.com/y1r/isucon10-yosen/commit/c8a470cd ; hiromu 45342a78 (1422→1556) https://github.com/hiromu/isucon10-qual/commit/45342a78 ; kamipo PR 17 https://github.com/soudai/isucon10-qualify/pull/17
  - [F] takonomura 2eb7315b https://github.com/takonomura/isucon10-final/commit/2eb7315b ; kazeburo 9020a9a8 https://github.com/kazeburo/isucon10-final/commit/9020a9a8 ; 一口坂46 yami20: 「ロックを捨てて直接updateかける&AffectedRowsで存在判定」 10000→12015 https://gist.github.com/yami20/0dedc537f4baedecc54162867d9e298d
- regulation note: none.

### T: [Q] Split MySQL by table: chair DB on server 2, estate DB on server 3, app+nginx on server 1 (App×1 / DB×2)
- what: There are no JOINs between `chair` and `estate`, so run two MySQL instances and open two connection pools (`CHAIR_MYSQL_HOST` / `ESTATE_MYSQL_HOST`); `/initialize` runs schema+data load against both (in parallel goroutines per shallowverse).
- when it works: DB CPU pinned at 100% on 1-core/2GB boxes: 「今回の問題は DB の CPU が100%に張り付くケースが多いです… App x1 / DB x2 にすることで負荷分散ができます」. This was the single largest jump for FCCPC (1948→3171) and the official "turning point".
- how to verify mid-contest: `top` on the DB host; confirm no cross-table queries by grepping handlers (`recommended_estate` reads chair then estate — two queries, fine).
- risk: `/initialize` has a 30s limit; loading both DBs serially risked it (shallowverse parallelised). Must restore `bind-address=0.0.0.0` and user grants; both MySQL instances must come back on reboot.
- evidence:
  - official 講評 https://isucon.net/archives/55025156.html
  - FCCPC https://fccpc.hateblo.jp/entry/2020/09/14/161420 ; shallowverse 1400→2000 https://www.y1r.org/posts/20200913-isucon10-qualify , commit 5cf88b7c https://github.com/y1r/isucon10-yosen/commit/5cf88b7c , para init 62cf13be
  - akouryy 626→1331 https://akouryy.hatenablog.jp/entry/2020/09/13/130415 ; がんもどき https://trap.jp/post/1114/ ; MN 480→977 https://zenn.dev/hirosuzuki/articles/8a4f91d5dc384cba534d , commit b343c926; tatsuru 5d2e58a2
- regulation note: 「複数台あるサーバーの役割の変更」 allowed; only the 3 given servers may be used.

### T: [Q] In-memory cache of `low_priced` responses (and `search/condition`), invalidated on CSV POST
- what: Keep the 20-row low-priced lists in process memory, serve them without DB, recompute (or merge) when `/api/chair` / `/api/estate` CSV is posted or a chair is bought (stock→0 removes it). `/api/*/search/condition` is a static JSON — innocent-team cached it in nginx `proxy_cache` (10m).
- when it works: Response is identical for all users and changes only on the two append paths + buy; official lists 「API のレスポンス値をアプリケーションサーバのインメモリにキャッシュし、メモリから返す」.
- how to verify mid-contest: the only writes are `POST /api/chair`, `POST /api/estate`, `POST /api/chair/buy/:id` — grep routes; the bench verifies freshness right after CSV posts.
- risk: Verification failures when the cache lags: shallowverse's "refresh every few seconds" batch approach 「failed verify phase」; tatsuru reverted both in-memory low_priced PRs the same afternoon; FCCPC's per-request caches succeeded (+242/+244/+215). chy72's post-contest balanced-BST everything-in-memory reached 7827 but 「これを愚直に実装すると余裕でメモリ制限2GBはオーバーする」.
- evidence:
  - FCCPC 15:45–16:22 https://fccpc.hateblo.jp/entry/2020/09/14/161420
  - shallowverse failure https://www.y1r.org/posts/20200913-isucon10-qualify
  - tatsuru 8e3593be / daf27890 then reverts b3c57f95, 011386dc https://github.com/tatsuru/isucon10q/commit/8e3593be
  - innocent-team proxy_cache 34c3545a https://github.com/innocent-team/isucon10q/commit/34c3545a
  - chy72 https://chy72.hatenablog.com/entry/2020/09/23/000826
- regulation note: 「キャッシュ機構の追加」 allowed; data must persist (cache must be rebuildable after reboot).

### T: [Q] Avoid the double query for paginated search (COUNT cache / SQL_CALC_FOUND_ROWS) — mixed results
- what: Search returns `count` + page. Options: `SELECT SQL_CALC_FOUND_ROWS …` + `FOUND_ROWS()` (MN, +~200), `sync.Map` counts per rent bucket incremented on insert (matsuu 1819→2070), or a single CTE (fails on 5.7).
- when it works: Count only changes on CSV insert; works well when conditions are bucketed.
- how to verify mid-contest: compare rows examined by COUNT vs SELECT in slow log.
- risk: kamipo's 感想戦 reverted `SQL_CALC_FOUND_ROWS` back to `COUNT(*)` once the descending index was right (it forces a full index scan); innocent-team's merged COUNT+SELECT failed the compatibility check; `FOUND_ROWS()` needs the same connection (MN opened a dedicated connection per request — fragile).
- evidence:
  - hirosuzuki 7752d652 https://github.com/hirosuzuki/isucon10-qual/commit/7752d652 ; https://zenn.dev/hirosuzuki/articles/8a4f91d5dc384cba534d
  - matsuu https://matsuu.hatenablog.com/entry/2020/09/13/131145
  - kamipo PR 14 (removes SQL_CALC_FOUND_ROWS) https://github.com/soudai/isucon10-qualify/pull/14
  - innocent-team failure https://blog.utgw.net/entry/2020/09/13/092305
- regulation note: none.

### T: [Q] MySQL 5.7 query cache ON (`query_cache_type=1`, 16M)
- what: Enable the (5.7-only) query cache; identical search queries repeat a lot because range IDs are few.
- when it works: Read-heavy, few distinct queries, writes infrequent (CSV every few seconds invalidates chair/estate entries wholesale, but reads between are many). SunPro: 880→1422 — their biggest single step.
- how to verify mid-contest: `SHOW STATUS LIKE 'Qcache%'` hit ratio.
- risk: Gone in MySQL 8 (teams who upgraded for DESC indexes lose it). Invalidation on every CSV insert; polamjag found it "30 minutes before deadline" worth >100 pts.
- evidence:
  - hiromu 85191c1c https://github.com/hiromu/isucon10-qual/commit/85191c1c ; https://blog.mine-studio.com/2020/09/isucon10/
  - innocent-team f830a6bf https://github.com/innocent-team/isucon10q/commit/f830a6bf
  - polamjag/wtatsuru https://polamjag.hatenablog.jp/entry/2020/09/13/004703 , https://wtatsuru.hatenadiary.com/entry/2020/09/13/000947
- regulation note: none.

### T: [Q+F] Turn off framework debug/logging and MySQL slow/general logs; compact JSON
- what: [Q] `e.Debug = true`, `e.Logger.SetLevel(log.DEBUG)`, `middleware.Logger()` and pretty JSON are in the reference Go app (`webapp/go/main.go` L244–248); remove them, and disable `slow_query_log`/`log-queries-not-using-indexes` before the final run. [F] same for `middleware.Logger()`, OpenCensus tracing/profiler, MySQL slow log.
- when it works: Always, once profiling is done; official: 「echo のログが初期に設定されている debug モードだと整形された JSON がログに出力されます」.
- how to verify mid-contest: CPU profile shows JSON indent / log writers; check `/var/log/mysql` growth.
- risk: Losing diagnostics late; do it as the last step.
- evidence:
  - official 講評 https://isucon.net/archives/55025156.html ; reference main.go https://github.com/isucon/isucon10-qualify/blob/master/webapp/go/main.go
  - hiromu 5fe97893 "disable indents in json responses" (555→572) https://github.com/hiromu/isucon10-qual/commit/5fe97893 ; tatsuru 299aa65e, 3d1bf840; hirosuzuki f7d7fb1f; matsuu 2612→2837 "log suppression" https://matsuu.hatenablog.com/entry/2020/09/13/131145
  - [F] takonomura 0496d1de, 54bdcd77 https://github.com/takonomura/isucon10-final/commit/54bdcd77 ; tatsuru/isucon10-final 47de29bf, d3c49add; kazeburo 1c797710; innocent-team/isucon10f 8fe4e6d9, a66209d5
- regulation note: none.

### T: [Q+F] MySQL durability/IO settings for the contest (`disable-log-bin`, `innodb_flush_log_at_trx_commit=0`, `innodb_doublewrite=0`, buffer pool up)
- what: Relax durability and raise `innodb_buffer_pool_size`; matsuu also pointed `innodb_log_group_home_dir` at `/dev/shm`. kazeburo (本選 感想戦) added `innodb_adaptive_hash_index=0`, `max_connections=1024`, small sort/read buffers for 1GB RAM.
- when it works: Qualifier boxes had IOPS limit 200 and 100 Mbps IO (README); 本選 boxes 1–2GB RAM.
- how to verify mid-contest: `dstat` IO wait; `innodb_buffer_pool_size` vs data size.
- risk: Redo log on tmpfs is lost on reboot → risk at the 再起動試験 if MySQL cannot recover (matsuu's team passed, but it is a gamble). `flush_log_at_trx_commit=0` can lose the last second of writes on a crash — reboot test is a clean `poweroff`, so normally safe.
- evidence:
  - matsuu https://matsuu.hatenablog.com/entry/2020/09/13/131145 ; wtatsuru buffer pool 「盲点だった」 https://wtatsuru.hatenadiary.com/entry/2020/09/13/000947
  - kazeburo 感想戦 config https://kazeburo.hatenablog.com/entry/2020/10/14/111149
  - qualifier hardware limits https://github.com/isucon/isucon10-qualify (README)
- regulation note: 「サーバ再起動後にすべてのアプリケーションコードが正常動作する状態を維持すること」.

### T: [Q+F] DB connection-pool sizing (`SetMaxOpenConns`) — both directions matter
- what: Reference apps use `db.SetMaxOpenConns(10)`. [Q] innocent-team raised it to 50 and the bench FAILed; they settled on 10–12. [F] takonomura went 10→512→128→256 with `max_connections=1024`, because XSUCON is highly concurrent (many virtual users polling).
- when it works: Raise only when the app is waiting on pool acquisition and DB CPU has headroom (本選: after dashboard caching). On a 1-core DB (予選) more connections just add contention.
- how to verify mid-contest: pprof shows goroutines blocked in `database/sql.(*DB).conn`; `SHOW PROCESSLIST` length; `Threads_running`.
- risk: FAIL from timeouts when the DB is saturated (innocent-team); MySQL `max_connections` default 151 must be raised in tandem (takonomura 7c14d455).
- evidence:
  - innocent-team 84526bfd…5b20ad2b https://github.com/innocent-team/isucon10q/commit/5b20ad2b ; https://blog.utgw.net/entry/2020/09/13/092305
  - takonomura 21799939, 56630a83, 7c14d455 https://github.com/takonomura/isucon10-final/commit/21799939
  - buchy (本選) raised from 10 https://buchy.hatenablog.com/entry/2020/10/13/222809
- regulation note: none.

### T: [Q] Read replica for search traffic (nginx routes `/api/chair/search` to a replica host) — marginal/risky
- what: tatsuru set up MySQL replication to server 2, ran a second app there pointing at the replica, and had nginx `location = /api/chair/search { proxy_pass http://search; }`. `/initialize` got a speculative `time.Sleep` to let replication catch up.
- when it works: Only if replica lag stays below what the bench's verification tolerates right after CSV inserts.
- how to verify mid-contest: `SHOW SLAVE STATUS` Seconds_Behind_Master during a run.
- risk: matsuu's team tried dual-INSERT to two servers and got timeouts; the table split (above) gives the same CPU relief without lag. Qualified 2335 only.
- evidence:
  - tatsuru e800dc0a, a64febe4, 698f0724 https://github.com/tatsuru/isucon10q/commit/e800dc0a ; https://wtatsuru.hatenadiary.com/entry/2020/09/13/000947
  - matsuu https://matsuu.hatenablog.com/entry/2020/09/13/131145
- regulation note: fine as long as only the three servers are used.

### T: [Q+F] Reboot test readiness: service ordering, persistence, no tmp-only state
- what: Ensure MySQL (on whichever host), nginx/envoy and the app are `enabled`, app units have `After=mysql.service`/restart policies, data lives on disk, and `/initialize` works after reboot. 5 top-25 teams (theorem 7744, ultra_fast_gopher, rust_kanzen_rikai, 今年も運動会被り, NaruseJun) were disqualified at the 予選 reboot check; 本選 had 2 browser-check and 8 load-check fails.
- when it works: Always — the final score is 「再起動後に運営によって実行された負荷走行のスコア」.
- how to verify mid-contest: `sudo systemctl reboot` once with 30 min left, then run `/initialize` and a bench; monora: 「DBが先に上がってないと落ちることを思い出す」; to-hutohu: 「ちゃんと必要なサービスがすべて正しく起動しているか確認する！！！！」 (their MySQL→MariaDB swap is what broke them).
- risk: Envoy `LimitNOFILE` (本選, see below), MySQL on tmpfs, cross-host MySQL not reachable at boot.
- evidence:
  - 予選 results/disqualifications https://isucon.net/archives/55007956.html ; 本選 https://isucon.net/archives/55076348.html
  - manual steps https://github.com/isucon/isucon10-final/blob/main/docs/manual.md ; gist manual (予選)
  - to-hutohu https://to-hutohu.com/2020/09/20/isucon10%E4%BA%88%E9%81%B8%E3%81%AB%E5%8F%82%E5%8A%A0%E3%81%97%E3%81%A6%E5%86%8D%E8%B5%B7%E5%8B%95%E8%A9%A6%E9%A8%93%E3%81%A7fail%E3%81%97%E3%81%BE%E3%81%97%E3%81%9F/ ; monora https://blog.monora.me/2020/09/reboot-not-supoprted-at-isucon10-qual/ ; wtatsuru 本選 「九死に一生」 https://wtatsuru.hatenadiary.com/entry/2020/10/06/023051
- regulation note: 「アプリケーションは全て保存データを永続化する必要があります」 (予選 manual); 本選: 「運営は `POST /initialize` をリクエストしない旨留意すること」 at the browser check.

---

### T: [F] Audience dashboard: ≤1 s cache (TTL cache / singleflight / background refresh) + `Cache-Control: public, max-age=1` + ETag/304
- what: `GET /api/audience/dashboard` is identical for everyone (teamID=0). Serve one copy per ≤1 s: a mutex-guarded TTL cache (takonomura `getCachedLeaderboard`, 1 s), singleflight (hidekiy, がんもどき), or a goroutine refreshing every 250–900 ms (innocent-team 500 ms, kazeburo 500 ms with 700–900 ms TTL). Add `Cache-Control: public, max-age=1` + ETag so the bench's browser-like agents send conditional GETs / skip requests; sorah's reference used Varnish `ttl=0.5s, grace=0.2s`.
- when it works: Manual: 「`GET /api/audience/dashboard` — アプリケーションは、データの更新から最大 1 秒古い情報を返すことができます」 and 「実ベンチマーカーは … Conditional GET に対応しています」. Audience grows by one per successful virtual benchmark, each with a new browser session, so this becomes the dominant GET.
- how to verify mid-contest: alp top by total time; the benchmarker's `verifyLeaderboard` allows `cacheTime = 3s` for audience (2s for contestant) and skips the check after the freeze (`benchmarker/scenario/verify.go`).
- risk: 「期待より古い内容のリーダーボードが返却されています(GET /api/audience/dashboard)」 when the refresh itself stalls under load (official 講評); must also freeze the cache during score-freeze (tatsuru 73bc6bd4) and never serve the audience cache to contestants (their own team's live score differs). Thundering herd on expiry — official recommends singleflight/Varnish.
- evidence:
  - manual https://github.com/isucon/isucon10-final/blob/main/docs/manual.md ; benchmarker verify.go https://github.com/isucon/isucon10-final/blob/main/benchmarker/scenario/verify.go
  - official 講評 (Varnish, singleflight, ETag) https://isucon.net/archives/55140539.html
  - takonomura af6860a2 (first thing after logging off) https://github.com/takonomura/isucon10-final/commit/af6860a2
  - がんもどき 0.9 s cache → 11,852; singleflight → 13,719/14,162 https://trap.jp/post/1131/
  - tatsuru 9366a16d, 73bc6bd4, 0a1d0b97 https://github.com/tatsuru/isucon10-final/commit/73bc6bd4 ; kazeburo cdd80f80, f50c073e, d1de18c4 (If-Modified → 304 after freeze) https://github.com/kazeburo/isucon10-final/commit/d1de18c4 ; innocent-team/isucon10f 30debef7 https://github.com/innocent-team/isucon10f/commit/30debef7
  - 一口坂46 nginx `proxy_cache` 1 s on this path, ~5000→10000 https://blog.s-tajima.work/post/2020/isucon10-final-hz46/ , https://gist.github.com/yami20/0dedc537f4baedecc54162867d9e298d ; buchy go-cache https://buchy.hatenablog.com/entry/2020/10/13/222809 ; hidekiy https://blog.hidekiy.com/2020/10/isucon10-8-isucon.html
- regulation note: only the audience dashboard may be stale; 「上記以外の HTTP リクエスト — データの更新が即時反映されていることを期待してベンチマーカーは検証を行います」.

### T: [F] Precompute leaderboard: denormalise `student`, best/latest score, finish_count onto `teams` (or a `team_scores` table) at `ReportBenchmarkResult`, drop the 69-line SQL, sort in app
- what: On `saveAsFinished`, `UPDATE teams SET best_score = IF(? > best_score, ?, best_score), latest_score = ?, finish_count = finish_count + 1 …` (takonomura keeps two sets: frozen-visible and `real_*` for the own-team/after-contest view; kazeburo uses a `team_scores` table with the same IF() trick). `teams.student` is set at CreateTeam/JoinTeam instead of `SUM(student)=COUNT(*)` subquery. Leaderboard = `SELECT … FROM teams` + app sort by `latest_score DESC, latest_score_marked_at ASC`.
- when it works: Writes happen only in the gRPC report path (one per virtual benchmark); reads are 100× more frequent. Official: 「仮想負荷走行が完了したときにあらかじめ Leaderboard のデータを作っておく」, 「student flag や latest / best score をカラム化しておくとよい」.
- how to verify mid-contest: the big query becomes slow only as `benchmark_jobs` grows (initially tables are empty, so slow log is quiet — official warns about this).
- risk: Score-freeze semantics must be reproduced (own team sees real, others see frozen); takonomura had a "NOOOOO" + "Fix dashboard sort" sequence while getting it right; `teamGraphScores` (per-team score history) still needs `benchmark_jobs` rows (kazeburo 81bc7d7e trimmed that query). Official sorah solution batches updates within 0.3 s and asynchronously (ack not delayed).
- evidence:
  - official 講評 https://isucon.net/archives/55140539.html
  - takonomura 87085589, 99384a33, 1ca805b5 https://github.com/takonomura/isucon10-final/commit/99384a33
  - kazeburo 7edc6ffe https://github.com/kazeburo/isucon10-final/commit/7edc6ffe ; 感想戦 https://kazeburo.hatenablog.com/entry/2020/10/14/111149
  - 一口坂46 "dashboard query simplification" https://gist.github.com/yami20/0dedc537f4baedecc54162867d9e298d
- regulation note: response structure/ordering must be identical.

### T: [F] Cache the marshalled protobuf bytes (not the struct); return via `e.Blob(... "application/vnd.google.protobuf")`; optionally pre-gzip
- what: `proto.Marshal` of the leaderboard is CPU-heavy; cache the `[]byte` of the whole `DashboardResponse` and write it directly. kazeburo also stored a gzip(BestCompression) copy in the background refresher.
- when it works: Any response that is byte-identical across users (audience dashboard, `teams`/`contestants` lists after registration closes — official: 「protobuf にした状態でキャッシュしておくのもいいと思います」).
- how to verify mid-contest: pprof top shows `proto.Marshal`/reflection; がんもどき: 「構造体ではなくprotobufの`[]byte`をキャッシュ」 14,162→16,718.
- risk: takonomura's first attempt (8b9890a6) was reverted and redone (864eb22f) — easy to get the wrapper message wrong.
- evidence:
  - takonomura 864eb22f https://github.com/takonomura/isucon10-final/commit/864eb22f
  - がんもどき https://trap.jp/post/1131/ ; innocent-team/isucon10f 12902907 "proto.Marshal した結果をそのままキャッシュ"
  - kazeburo f50c073e https://github.com/kazeburo/isucon10-final/commit/f50c073e
- regulation note: none.

### T: [F] Benchmark queue (`ReceiveBenchmarkJob`): kill the sleep-poll loop — Redis BRPOP, in-process channel, or job-id push from web to bench server; lock-free claim
- what: Reference `pollBenchmarkJob` loops 10× with 50 ms sleeps doing `SELECT … WHERE status=PENDING ORDER BY id LIMIT 1` then `SELECT … FOR UPDATE`. Options: (a) Redis list + `BRPOP` (official sorah); (b) kazeburo: web app POSTs the new job id to a small HTTP server (`:60051`) inside the gRPC process, which feeds a channel that `pollBenchmarkJob` selects on with a 500 ms timeout; (c) at minimum drop `FOR UPDATE` (see T above) and shorten the sleep (takonomura 25 ms).
- when it works: Manual: virtual benchmarkers 「待ち時間を挟まず、即時にリトライ」 and there are as many as teams, so with TeamCapacity 100+ the poll storm is constant.
- how to verify mid-contest: general log dominated by the PENDING select; envoy access log of `/xsuportal.proto.services.bench.BenchmarkQueue/ReceiveBenchmarkJob`.
- risk: Channel/queue state is in-process → must survive restarts (jobs still in MySQL with status PENDING; kazeburo falls back to the DB). kazeburo's `ORDER BY RAND() LIMIT 1` to spread contention got him to 60k in 感想戦 but was later replaced by the id channel. Don't make the bench server depend on the web process being on the same host unless envoy routes accordingly.
- evidence:
  - reference https://github.com/isucon/isucon10-final/blob/main/webapp/golang/cmd/benchmark_server/main.go ; manual polling text https://github.com/isucon/isucon10-final/blob/main/docs/xsucon.md
  - official 講評 (Redis BRPOP) https://isucon.net/archives/55140539.html
  - kazeburo 3be8029f, 13accd6b, 6134f30e https://github.com/kazeburo/isucon10-final/commit/3be8029f ; https://kazeburo.hatenablog.com/entry/2020/10/14/111149 (「ジョブの起動速度の改善とMySQLの負荷削減」)
  - takonomura 2eb7315b https://github.com/takonomura/isucon10-final/commit/2eb7315b ; kazeburo ea15f2c1 (poll 5×100 ms)
- regulation note: 「ジョブキュー機構の追加による遅延書き込み」 allowed; consistency must hold 5 s after the run ends.

### T: [F] Raise `TeamCapacity` (10 → 50–120) once the app has headroom — 大会規模ボーナス up to ×2
- what: The reference returns 403 on `POST /api/registration/team` after `TeamCapacity = 10`. Raising it admits more virtual contestants (3 per team), multiplying contestant score by 1.2…2.0 at 60/120/180/240/300 contestants, and more teams → more benchmarks → more audience.
- when it works: Manual: 「11 チーム目以降のチーム登録リクエスト … に対し HTTP レスポンスコード 403 を返してください」; bonus table in the manual. Every top team's big jump was this: がんもどき 16,718→29,822 (10→50); 一口坂46 12,015→~14,000 (→80, final 110); takonomura stepped 15/30/60/80/100/120/150 and finished at 120; tatsuru 20→40→50→65 (90 reverted).
- how to verify mid-contest: timeouts (100 per −100 pt) and `Too many open files` on envoy rise with capacity; watch 「致命的な問題」 — if zero teams register, instant fail. Capacity is a constant in `main.go` — change via env or constant.
- risk: Overshooting causes timeout storms/FAIL (takonomura reverted 150 and 130; kazeburo oscillated 100–150; wtatsuru stayed at 65 for safety). Needs envoy nofile and DB pool raised first.
- evidence:
  - manual https://github.com/isucon/isucon10-final/blob/main/docs/manual.md , https://github.com/isucon/isucon10-final/blob/main/docs/xsucon.md
  - takonomura 663ccdf2…ed37c271, final `TeamCapacity = 120` https://github.com/takonomura/isucon10-final/commit/73b4bd86
  - がんもどき https://trap.jp/post/1131/ ; yami20 https://gist.github.com/yami20/0dedc537f4baedecc54162867d9e298d ; tatsuru c6c3a220/0b1a0813/e135f25d; kazeburo 感想戦 best at 100 https://kazeburo.hatenablog.com/entry/2020/10/14/111149 ; wtatsuru https://wtatsuru.hatenadiary.com/entry/2020/10/06/023051
- regulation note: 403 for over-capacity is the sanctioned mechanism; any other code is a status error.

### T: [F] Envoy `LimitNOFILE=65536` (systemd drop-in) — or envoy dies under load
- what: Envoy's unit lacks a file-descriptor limit; at high TeamCapacity it hits `Too many open files` and crashes/stops accepting.
- when it works: Any team keeping envoy (reference front) and raising capacity.
- how to verify mid-contest: `journalctl -u envoy` for "Too many open files"; `cat /proc/$(pidof envoy)/limits`.
- risk: buchy (37,501 during contest) FAILed the final check because envoy crashed under load — 「LimitNOFILE を設定し忘れ」; nonylene hit the same near the end; hidekiy saw it when scaling capacity.
- evidence:
  - takonomura 848ce592 https://github.com/takonomura/isucon10-final/commit/848ce592 ; wtatsuru 「LimitNOFILE=65535」 https://wtatsuru.hatenadiary.com/entry/2020/10/06/023051
  - buchy https://buchy.hatenablog.com/entry/2020/10/13/222809 ; nonylene https://nonylene.hatenablog.jp/entry/2020/10/06/021113 ; hidekiy https://blog.hidekiy.com/2020/10/isucon10-8-isucon.html
- regulation note: reboot-test implication — a drop-in file is persistent; `ulimit` in a shell is not.

### T: [F] Serve static assets (`/packs/*.js|css`, `favicon.ico`, SPA HTML) from nginx with `Cache-Control`/`expires 1d`, gzip_static or brotli; route via envoy prefix or replace envoy
- what: Reference serves HTML/JS/CSS from the app (`srv.Static("/", "public")`). Put nginx on the static path (innocent-team: envoy `prefix: "/"` → `xsuportal_static` cluster on nginx :2525, with symlinks for every SPA route; kazeburo: `/packs` and `/favicon.ico` → `xsuportal_web_static` cluster; tatsuru: nginx `location /packs { root …/frontend/public; }`), add `expires 1d` / `max-age=3600`, and pre-compress (`gzip_static`; sorah: static brotli — bench sends `Accept-Encoding: gzip, br, deflate`).
- when it works: Manual: 「ブラウザのキャッシュ有効期限の制御によってリクエストが発生していなかったりしても、ベンチマーカーはそれらのキャッシュを利用してレスポンスがあったものとみなします」; each new audience member is a fresh browser session, so static traffic scales with audience. Benchmarker verifies assets by checksum and accepts 200/304 (`verify.go`).
- how to verify mid-contest: envoy/nginx access log by path — `/packs/*.js` large bodies dominate bytes.
- risk: JS/CSS content must be byte-identical (regulation); per-user caches are not shared (`Cache-Control: public` doesn't help across users). tatsuru had to switch nginx `listen 443 ssl http2` off: 「HTTP/2 だとベンチマーカーがたまにエラー吐くので切っとく」. 一口坂46 replaced envoy with nginx (HTTP/2 + gRPC upstream) and then had a last-minute port-80 redirect status issue.
- evidence:
  - official 講評 https://isucon.net/archives/55140539.html ; manual https://github.com/isucon/isucon10-final/blob/main/docs/manual.md ; verify.go https://github.com/isucon/isucon10-final/blob/main/benchmarker/scenario/verify.go
  - innocent-team/isucon10f 0835afee https://github.com/innocent-team/isucon10f/commit/0835afee
  - kazeburo envoy config https://github.com/kazeburo/isucon10-final/blob/master/config.yaml , 感想戦 gzip_static https://kazeburo.hatenablog.com/entry/2020/10/14/111149 , aaadb0da Cache-Control middleware
  - tatsuru/isucon10-final d6aa1a71, c3dc7ed7, 6ff3a591 https://github.com/tatsuru/isucon10-final/commit/c3dc7ed7
  - 一口坂46 https://blog.s-tajima.work/post/2020/isucon10-final-hz46/
- regulation note: 「JavaScript/CSS ファイルの内容」「画像および動画等のメディアファイルの内容」 must not change; compression allowed (「HTTP レスポンスの圧縮」).

### T: [F] Server-role split exploiting the uneven specs (isu1 2c/1GB, isu2 2c/2GB, isu3 4c/1GB) and envoy prefix routing to per-role clusters
- what: Put MySQL on the 2GB box (or the 4-core box when CPU-bound), the web app on the 4-core box, keep envoy + gRPC bench server light. Envoy can send specific prefixes to different clusters (kazeburo: dashboards + clarifications → `xsuportal_web2`, gRPC → api cluster, static → nginx), and weight endpoints by listing them twice (takonomura "Change isu2 weight").
- when it works: Official: 「もしこのスペック差に気付いてないと、3台への振り分けをする際に苦しむ」; app-bound from the start, so distributing web across two hosts is immediately effective.
- how to verify mid-contest: `nproc`/`free -m` on each host first; `top` per role.
- risk: 一口坂46 used only 2 servers out of 「アンバランスな構成への忌避感」 and regretted it; nonylene moved MySQL twice chasing CPU vs memory. With 1GB RAM, 「ちょっと無理をするとサーバから応答がなくなってしまう」 (official).
- evidence:
  - official 講評 + sorah layout https://isucon.net/archives/55140539.html ; specs https://github.com/isucon/isucon10-final (README)
  - takonomura 7d219a52 (mysql→isu2), 90bf205e/58d76266 (mysql→isu3), e42799c6/3e7f18c3 (web on isu2 with weight) https://github.com/takonomura/isucon10-final/commit/e42799c6
  - がんもどき layout https://trap.jp/post/1131/ ; kazeburo 感想戦 https://kazeburo.hatenablog.com/entry/2020/10/14/111149 ; yami20 https://gist.github.com/yami20/0dedc537f4baedecc54162867d9e298d ; nonylene https://nonylene.hatenablog.jp/entry/2020/10/06/021113
- regulation note: 3 servers only; hostnames `isu1-3.t.isucon.dev` pre-registered; TLS cert `*.t.isucon.dev` must validate for the bench.

### T: [F] Clarifications: store `last_clar_id` on `teams`, fix N+1 team lookups with `IN (…)`
- what: `ListNotifications` ran `SELECT id FROM clarifications WHERE (team_id=? OR disclosed=TRUE) AND answered_at IS NOT NULL ORDER BY id DESC LIMIT 1` on every poll; takonomura updates `teams.last_clar_id` in `RespondClarification` (all teams if disclosed, else that team) and returns `team.LastClarID`. `ListClarifications` (admin and contestant) fetched the team per row → one `sqlx.In` query. (sorah's solution: `teams.last_answered_clarification_id` counter cache.)
- when it works: Notification polling is 「ひたすらポーリング」 by every virtual contestant; the admin polls `GET /api/admin/clarifications` constantly.
- how to verify mid-contest: query count per request in general log; alp on `/api/contestant/notifications`.
- risk: hidekiy's attempt to cache clarifications failed integrity checks (immediacy required for non-audience endpoints). Disclosure flips must update every team's pointer in the same transaction.
- evidence:
  - takonomura 8a297279, 18159a31, f41dbcb1 https://github.com/takonomura/isucon10-final/commit/8a297279
  - official 講評 (counter cache) https://isucon.net/archives/55140539.html
  - がんもどき N+1 fix 29,822→30,123 https://trap.jp/post/1131/ ; innocent-team/isucon10f 29274bf3/c1a241a2; tatsuru/isucon10-final fc1f5a8d, 96e4e36d; kazeburo 2c706a68
  - hidekiy failure https://blog.hidekiy.com/2020/10/isucon10-8-isucon.html
- regulation note: immediate consistency expected on all non-audience endpoints.

### T: [F] Web Push for notifications + empty `ListNotifications` for subscribed contestants (optional; winner skipped it)
- what: Implement `POST/DELETE /api/contestant/push_subscriptions` (store endpoint/p256dh/auth), send `Notification` protobuf via `webpush.SendNotification` with VAPID on job finish / clarification answer, and return an empty list (HTTP 200) from `GET /api/contestant/notifications` when the contestant has a subscription. Official refinement: push individual notifications, but deliver disclosed (全体) clarification answers via polling to avoid a thundering herd.
- when it works: Manual: 「Web Push で全ての通知を送るように変更した場合は、通知リスト（`GET /api/contestant/notifications`）のリクエストに対して空の通知リストを返す事ができます」; sample code per language exists (`send_web_push`). One VAPID key must be shared across hosts.
- how to verify mid-contest: the push-service in the benchmarker records 「不正な Web Push メッセージ」 errors; bench accepts 503 on subscribe (`action.go` L258), so 503-until-ready is safe.
- risk: 一口坂46: first attempt FAILed, fixed by copying the provided sample → 14,000→18,953. Official: many 「実装したけどスコアが下がったのでポーリング方式に切り戻した」 because all contestants react at once. kazeburo removed the transaction in `ListNotifications` and got 「あるべき通知を受信していないことが検知されました」/「リーダーボード上の最終 ID 検証に失敗しました」 → FAIL; restoring it gave 40–50k. takonomura (1st, 49,545) never implemented push — his final `SubscribeNotification` still returns 503 「WebPush は未対応です」 and `ListNotifications` keeps the transaction.
- evidence:
  - manual https://github.com/isucon/isucon10-final/blob/main/docs/xsucon.md ; benchmarker action.go https://github.com/isucon/isucon10-final/blob/main/benchmarker/scenario/action.go
  - official 講評 https://isucon.net/archives/55140539.html
  - yami20 https://gist.github.com/yami20/0dedc537f4baedecc54162867d9e298d ; tatsuru ab4ac290, 1dd899a5 (「プッシュ通知対応したので常に空でいいとレギュレーションにあった」) https://github.com/tatsuru/isucon10-final/commit/1dd899a5 ; kazeburo 2925cb5d, ebcdb550, 0f8bb7eb https://github.com/kazeburo/isucon10-final/commit/ebcdb550 ; kazeburo FAIL post-mortem https://kazeburo.hatenablog.com/entry/2020/10/05/174140 ; innocent-team/isucon10f 1926e6b6/1434afdc; hidekiy https://blog.hidekiy.com/2020/10/isucon10-8-isucon.html
  - takonomura final main.go (503 still) https://github.com/takonomura/isucon10-final/blob/master/webapp/golang/cmd/xsuportal/main.go
- regulation note: 「Web Push 実装の追加」 explicitly allowed; push messages must be RFC8291-encrypted + VAPID or they count as errors.

### T: [F] Cache immutable-after-phase data in memory: `contest_config`, teams/contestants after registration, `push_subscriptions`, VAPID key
- what: `contest_config` never changes after `/initialize`; `teams`/`contestants` never change after the registration phase; `push_subscriptions` rarely. Load once (kazeburo "cache config", "cache push_subs", "preload VAPIDKey" — the reference re-read the PEM on every notify).
- when it works: Official: 「contest_config テーブルは、/initialize 以降は更新されることがない … contestants や teams テーブルは、仮想チーム登録フェーズが終わった後は更新されることがない」.
- how to verify mid-contest: `getCurrentContestStatus` is called in every handler (grep); registration ends at `contest_starts_at`.
- risk: Caching contestants/teams *during* registration broke two teams: がんもどき's in-memory contestant cache caused deadlocks and was reverted at 17:20; innocent-team reverted "login required を分離して…ユーザー情報はDBを見に行かない" at 17:40. Caches must be reset on `/initialize` (innocent-team 5cd84356) and are lost on restart (fine: rebuilt from DB).
- evidence:
  - official 講評 https://isucon.net/archives/55140539.html
  - kazeburo 57ca6a66, 3e2b2f76, a8b17559 https://github.com/kazeburo/isucon10-final/commit/a8b17559 ; 感想戦 https://kazeburo.hatenablog.com/entry/2020/10/14/111149
  - がんもどき https://trap.jp/post/1131/ ; innocent-team/isucon10f 13426a0d (revert) https://github.com/innocent-team/isucon10f/commit/13426a0d
- regulation note: none beyond consistency.

### T: [F] Remove needless transactions / `LIMIT 1` / `FOR UPDATE` on hot reads, `singleflight` on per-team lists — but keep the ones that guard consistency
- what: `makeLeaderboardPB` wraps two SELECTs in a tx (kazeburo removed it — then reverted); `getCurrentContestant/Team` use `LIMIT 1 FOR UPDATE` only when `lock` is true; `ListBenchmarkJobs` per team was wrapped in `sfGroup.Do("listbench<team>")` (kazeburo). `JoinTeam` member count → select members once and compute student flag (takonomura).
- when it works: Reads on a single table with no cross-row invariant.
- how to verify mid-contest: `Trace tx begin` (takonomura instrumented tx.Begin spans) to see where tx waits are.
- risk: The one transaction that matters is `ListNotifications` (select + mark read); removing it = FAIL (kazeburo). Uncommitted tx leaks exhausted がんもどき's pool at 14:00.
- evidence:
  - kazeburo 43c85f49 → 01644e3c revert, efff8565, f50c073e https://github.com/kazeburo/isucon10-final/commit/43c85f49 ; https://kazeburo.hatenablog.com/entry/2020/10/05/174140
  - takonomura 442463de "Trace tx begin", 87085589 https://github.com/takonomura/isucon10-final/commit/87085589
  - がんもどき https://trap.jp/post/1131/
- regulation note: integrity check 5 s after load; 「ジョブキューによる遅延処理等を追加した場合は、このタイミングまでに処理の整合性をとってください」.

### T: [F] Add `contestants(team_id)` index; use `INSERT` result id instead of a follow-up `LastInsertId` query
- what: Reference schema has no index on `contestants.team_id` although every team lookup joins members; both takonomura and kazeburo added `ALTER TABLE contestants ADD INDEX idx1 (team_id)`. がんもどき replaced an extra `SELECT LAST_INSERT_ID()` round-trip with the driver's `LastInsertId()` (30,666→31,818).
- when it works: Always; schema is small and empty at start so it costs nothing.
- how to verify mid-contest: `schema.sql` — only `benchmark_jobs` and `notifications` have indexes.
- risk: none.
- evidence:
  - schema https://github.com/isucon/isucon10-final/blob/main/webapp/sql/schema.sql
  - takonomura 054c524b https://github.com/takonomura/isucon10-final/commit/054c524b ; kazeburo cdd80f80 (same index) https://github.com/kazeburo/isucon10-final/commit/cdd80f80
  - がんもどき https://trap.jp/post/1131/
- regulation note: none.

### T: [F] Envoy ↔ app tuning: `max_concurrent_streams` for gRPC, TLS/HTTP2 termination at envoy, keepalive to app
- what: sorah's solution tuned envoy `max_concurrent_streams` to the gRPC server's capacity so load balancing respects it, and terminated HTTP/2+TLS at envoy to relieve Ruby. nginx users set `proxy_http_version 1.1` + keepalive upstreams (tatsuru) and `grpc_pass` for the two bench services.
- when it works: When gRPC servers are on multiple hosts or have limited worker counts.
- how to verify mid-contest: envoy stats `upstream_rq_pending_overflow`; app goroutine counts.
- risk: nonylene: 「普通の Reverse Proxy として使うなら Nginx のほうが扱いやすい」 — envoy YAML costs time; 一口坂46 replaced it entirely.
- evidence:
  - official 講評 https://isucon.net/archives/55140539.html
  - tatsuru/isucon10-final 627c347f, 4edc40a1 ("grpcにしないとね") https://github.com/tatsuru/isucon10-final/commit/627c347f
  - nonylene https://nonylene.hatenablog.jp/entry/2020/10/06/021113 ; 一口坂46 https://blog.s-tajima.work/post/2020/isucon10-final-hz46/
- regulation note: none.

---

### Techniques appearing in both 予選 and 本選
- Conditional `UPDATE` + `RowsAffected` instead of `SELECT … FOR UPDATE` (buyChair / ReceiveBenchmarkJob).
- Disable echo debug/`middleware.Logger`, tracing, MySQL slow log before the final run.
- Connection-pool sizing (up in 本選, *down/unchanged* in 予選).
- Relaxed MySQL durability settings on IO-limited VMs.
- Reboot-test discipline (service ordering, `LimitNOFILE`, persistence); disqualifications in both rounds.
- N+1 removal via `IN (…)`/single query (nazotte; clarifications/teams).
- Role split across 3 servers driven by measured CPU (DB split by table in 予選; by endpoint/role in 本選).

---

### Regulation highlights (ISUCON10)

予選 (http://isucon.net/archives/54753430.html , manual https://gist.github.com/progfay/25edb2a9ede4ca478cb3e2422f1f12f6):
- Do not change: 「アクセス先のURI、ただしサーバー側で生成する部分（IDなど）は文字種を変えない範囲で自由に生成しても良い」, 「レスポンス (HTMLのDOM, JSONオブジェクト等) の構造（表示に影響しない範囲での空白文字の増減は許可される）」, 「JavaScript/CSSファイルの内容」, 「画像および動画等のメディアファイルの内容」.
- Allowed: 「DBスキーマの変更やインデックスの作成・削除」, 「キャッシュ機構の追加、ジョブキュー機構の追加による遅延書き込み」, 「他の言語による再実装」, role changes across the 3 servers.
- Must hold: 「サーバ再起動後にすべてのアプリケーションコードが正常動作する状態を維持すること」, 「ベンチマーク実行時にアプリケーションに書き込まれたデータは再起動後にも取得できること」; manual: 「アプリケーションは全て保存データを永続化する必要があります」, 「アプリケーションはブラウザ上での表示を初期状態と同様に保つ必要があります」.
- External network: 「ベンチマーク中に…主催の指示以外で利用が認められたサーバー以外の外部リソースを使用する行為は禁止する」; 「競技に利用できる計算機資源は主催者側が指定した3台までのインスタンスのみです」; metrics SaaS allowed only if it has no score effect.
- Static files: 「ベンチマークからのリクエストは、 API に対してのみ行われます。画像や HTML などの静的ファイルに対するリクエストは行われませんが、追試にてブラウザ上でアプリケーションが動作するかを確認します」.
- Status codes: 「HTTP ステータスコードは、基本的に参考実装と同一のものを想定しています」; bots → 503 allowed; timeouts outside CSV upload are not penalised; CSV upload failure/timeout = critical fail.
- `/initialize` ≤ 30 s and must return `{"language": "..."}` (empty = fail).
- Post-contest: no server operations until announced; 5 teams disqualified at reboot check (https://isucon.net/archives/55007956.html).

本選 (https://github.com/isucon/isucon10-final/blob/main/docs/manual.md , https://github.com/isucon/isucon10-final/blob/main/docs/xsucon.md):
- Same "do not change" list; additionally allowed: 「Web Push 実装の追加」, 「HTTP レスポンスの圧縮」, 「キャッシュ機構の追加、ジョブキュー機構の追加による遅延書き込み」.
- Caching: audience dashboard may be ≤1 s stale; everything else must reflect updates immediately 「ただし、ベンチマーカーが検知しない限りは古い情報を返しても構いません」; Conditional GET / `Cache-Control` honoured; caches are per virtual user, never shared.
- Timeouts: `/initialize` 20 s (run twice; fail if exceeded); dashboards 2 s; other HTTP 10 s; gRPC unlimited; 5 s drain after load, then integrity check.
- Team cap via 403 on `POST /api/registration/team`; max 3 contestants per team.
- Web Push: RFC8291 encryption + RFC8292 VAPID mandatory; invalid messages count as errors; polling may return empty list (200) when all notifications are pushed.
- TLS: bench connects to `isuN.t.isucon.dev` over HTTPS and validates the `*.t.isucon.dev` cert; `/etc/hosts` entries editable at own risk.
- Final score = best of 3 organiser runs after `poweroff` + ≥10 min, then browser check without `/initialize`; fail twice = disqualified.
- Servers: isu1 2c/1GB, isu2 2c/2GB, isu3 4c/1GB (README); 「競技に利用できる計算機資源は運営側が用意した 3 台のインスタンスのみ」.

### Scoring model (ISUCON10)

予選 ISUUMO (manual gist; bench code https://github.com/isucon/isucon10-qualify/tree/master/bench ; progfay https://zenn.dev/progfay/articles/isucon10-qualify):
- 「スコア = (イスの購入件数 + 物件の資料請求件数) - 減点」. Only conversions score; searches/nazotte/bots score nothing by themselves.
- Penalties: status/response errors −50 each, fail at 10; critical error (CSV upload fail/timeout) = immediate fail; score < 0 = fail; `/initialize` > 30 s = fail.
- Bench shape (parameter.go): level 0 starts with 3 chair-search + 3 estate-search workers; level 1 (score ≥ 300) adds 3 nazotte workers; level 2 (≥ 600) adds 5 bot workers + 1 chair-draft + 1 estate-draft; levels 3–11 (800, 900, 1000, 1100, 1200, 1300, 1450, 1600, 1800, 2000) each add +1 worker of every kind. A worker reruns its scenario immediately on success and sleeps ~1.5 s on failure (`SleepTimeOnFailScenario`); bots sleep ~500 ms between runs. `DefaultAPITimeout` 2 s, `ThresholdTimeOfAbandonmentPage` 1 s (progfay: 「APIが1秒以内に返ってこない場合はシナリオをやり直す」), `DraftTimeout` 5 s, `VerifyTimeout` 10 s, load 60 s. 503 to a non-bot client = temporary error (not scored, scenario retried); 503 to a bot client = silently accepted.
- Official rehearsal best ≈ 8,000; 予選 top: FCCPC 4100, shallowverse 4072, FetchDecodeExecWrite 3804 (theorem 7744 disqualified at reboot) — https://isucon.net/archives/55007956.html (per-team table page not opened; unverified numbers from search snippets).

本選 XSUCON (manual.md; benchmarker https://github.com/isucon/isucon10-final/tree/main/benchmarker/scenario):
- 「スコア = (仮想選手スコア * 大会規模ボーナス) + 仮想オーディエンススコア - 減点」. Contestant score: completed virtual benchmark 10 pts (counted when the team views the job detail after the finish notification), first view of an answered clarification 10 pts, contestant dashboard view 2 pts. Audience: 1 pt per 10 dashboard views. Bonus ×1.0/1.2/1.4/1.6/1.8/2.0 at 1–59/60–119/120–179/180–239/240–299/300+ contestants.
- Penalties: compatibility/integrity check fail or fatal app problem = immediate fail; status/response errors −50 each, fail at 100; timeouts −100 per 100, no fail; ≤0 = fail.
- Bench model: phases at 0/10/50/60 s (registration → contest → score freeze → end). Virtual contestants enqueue immediately whenever allowed, poll notifications, dashboards and clarifications continuously; virtual benchmarkers (one per team) poll `ReceiveBenchmarkJob` with no sleep and finish runs instantly; audience starts at 0 and +1 per successful virtual benchmark, −1 per audience error; audience polls dashboard every ~1 s (`loadAudienceDashboard`). Leaderboard freshness check allows 2 s (contestant) / 3 s (audience) lag and is skipped after the freeze for audience (`verify.go`). Browser-like agent fetches HTML, `/packs/*.js|css`, favicon with checksum verification and supports 304.
- Results: takonomura 49,545 (Go, solo), Azeit 43,366, がんもどき 34,767, 一口坂46 31,432, 百万円ドリブン 30,834; organiser Ruby reference solution 36,768 at TEAM_CAPACITY=65; kazeburo post-contest 101,249 at capacity 100 — https://isucon.net/archives/55076348.html , https://isucon.net/archives/55140539.html , https://kazeburo.hatenablog.com/entry/2020/10/14/111149.