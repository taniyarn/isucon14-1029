# ISUCON12 (2022) technique catalogue — 予選 ISUPORTS / 本選 ISUCONQUEST

Scope note: everything below comes from sources I opened in this session (official repos via `gh api`, isucon.net posts, gists, team repos' commit diffs, and 参加記 blogs). Where I only have a blog's description and not a diff, the evidence line says so. Nothing marked "(unverified)" was confirmed.

Official results used for context:
- 予選 (698 teams): 1 第7西東京市 86,740 / 2 シン・ウー馬場ーイー2 75,800 / 3 たつや 65,721 / 4 theorem 53,751 / 5 Takedashi 52,957 / 6 :old_noto_innocent: 50,696 / 7 釜中の鯖 35,642 / 8 ソレイユ 34,635 / 10 NaruseJun 33,089 / 11 と 31,962 / 12 織時屋 31,806 / 17 Zatsu-X-Club (27,084 雑草係 is 17th; Zatsu-X-Club's blog says 17th — team-name mismatch unverified). https://isucon.net/archives/56838276.html
- 本選 (30 teams): 1 NaruseJun 341,258 / 2 織時屋 242,653 / 3 と 231,428 / 4 takonomura 219,830 / 7 :old_noto_innocent: 167,491 / 12 シン・ウー馬場ーイー2 106,254. Disqualified after browser check: kyncon 268,125, 釜中の鯖 218,852, 第7西東京市 189,465, Team YVR 59,742, 快適PandA 119,875. https://isucon.net/archives/56923294.html

Repos read (commit lists in order, selected diffs):
- 予選: icchy/isucon12q@final (第7西東京市), matsuu/isucon12q (シン・ウー馬場ーイー2), oribe1115/isucon12-qualify (織時屋), Zatsu-X-Club/isucon12q, saba-in-the-kettle/isucon12-qualify (default branch found; commit list 404'd — not read).
- 本選: narusejun/isucon12-final, oribe1115/isucon12-final, innocent-team/isucon12f, matsuu/isucon12f, saba-in-the-kettle/isucon12-final.
- Official: isucon/isucon12-qualify (webapp/go/isuports.go, sql/admin/10_schema.sql, sql/tenant/10_schema.sql, sql/init.sh), isucon/isucon12-final (webapp/go/main.go, admin.go, sql/init.sh, sql/3_schema_exclude_user_presents.sql, sql/6_id_generator_init.sql).

---

## 予選 ISUPORTS techniques

### T: Billing only after competition finish (early return + cache / precompute at finish)
- what: `billingReportByCompetition` scans visit_history + player_score for every competition on every `GET /api/organizer/billing` and `GET /api/admin/tenants/billing`. Return zeros for unfinished competitions and compute once (at `finish`) or cache indefinitely for finished ones.
- when it works: The app's own code says the amount is only defined after finish (`// 大会が終了している場合のみ請求金額が確定するので計算する` in official isuports.go). Official 講評: 「大会が終了した時点で課金情報が確定し、その後は変更されない」「終了API呼ばれたタイミングで1回だけ課金情報を計算してMySQLのテーブルに保存する」. Manual grants 3 s propagation: 「POST /api/organizer/competition/:competition_id/finish … GET /api/organizer/billing, GET /api/admin/tenants/billing は上記APIの情報の反映まで、レスポンスを返してから3秒の猶予が許容されます」.
- how to verify mid-contest: read the billing function for an `if comp.FinishedAt.Valid` branch after expensive queries; grep the manual for 「猶予」; alp shows `GET /api/organizer/billing` / `admin/tenants/billing` as top total-time endpoints (10 pt each).
- risk: Must invalidate/compute on `finish`; if you compute asynchronously you have ≤3 s. Billing must survive reboot (store in MySQL, not only in-process map) because the bench's post-reboot run calls it again (team いすもな failed post-reboot with 500 on /api/admin/tenants/billing; Aquaharaimori returned an empty tenants array — https://isucon.net/archives/56838181.html).
- evidence:
  - 第7西東京市 commit "perf: visit_historyの定義変更" moves the whole scan inside `if comp.FinishedAt.Valid` — https://github.com/icchy/isucon12q/commit/28c16690 (blog: 2,598 → 3,602 at 11:05, https://icchy.hatenablog.jp/entry/2022/07/26/000905)
  - 織時屋 "終了するまで計算しない" early return with zeros + "billingReportCache"/"cache indefinitely" — https://github.com/oribe1115/isucon12-qualify/commit/0d5ba8b4 , https://github.com/oribe1115/isucon12-qualify/commit/863c85f5 (blog: 16:10 21,600 pts, https://trap.jp/post/1640/)
  - Official 解説 (acidlemon, Node.js): billing_report table written at finish, "開発中のベンチマークスコアが5倍になりました" — https://isucon.net/archives/56842718.html
  - 釜中の鯖: "大会が終了していない場合は...計算する必要がない" early return + TTL cache — https://blog.p1ass.com/posts/isucon12/
  - Zatsu-X-Club: billingReportByCompetition2 (14,315) — https://github.com/Zatsu-X-Club/isucon12q/commit/7abe7767 (blog https://zenn.dev/tosa/articles/ba19f3610cc331)
- regulation note: 「キャッシュ機構の追加、ジョブキュー機構の追加による遅延書き込み」 is explicitly allowed (https://isucon.net/archives/56671734.html); data written during the bench must be readable after reboot.

### T: visit_history compaction to one row per (tenant, competition, player) + INSERT IGNORE
- what: Replace the 3.2M-row append-only `visit_history` (index only on tenant_id) with a PK-keyed table holding `MIN(created_at)` per (tenant_id, competition_id, player_id); on ranking view do `INSERT IGNORE` instead of append.
- when it works: Only `MIN(created_at)` per player is ever read (`SELECT player_id, MIN(created_at) … GROUP BY player_id` in official billing code). Official 講評: 「`visit_history` テーブルは初期状態で320万行以上ありますが、インデックスは `tenant_id` の1カラムしか存在しない」「初期の320万行が20万行まで削減できます」.
- how to verify mid-contest: grep all reads of the table; if every read aggregates MIN/first, the table can be keyed. Check `webapp/sql/admin/10_schema.sql` (INDEX only `tenant_id_idx`). Slow log will show the GROUP BY over visit_history.
- risk: Compaction must run inside `/initialize` ≤30 s (do it with INSERT…SELECT GROUP BY in init.sql or pre-build at boot and restore). ソレイユ tried twice and abandoned due to bugs (https://diary.hatenablog.jp/entry/2022/07/26/022052). 織時屋's last-minute "where not exists" variant was reverted (invalid SQL: `INSERT … VALUES (…) where not exists …`) — https://github.com/oribe1115/isucon12-qualify/commit/1efdc8da , revert https://github.com/oribe1115/isucon12-qualify/commit/7d9dc391.
- evidence:
  - 第7西東京市 `sql/admin/95_patch.sql` (PRIMARY KEY (tenant_id, competition_id, player_id), INSERT…SELECT min(created_at) GROUP BY) and app `INSERT IGNORE` — https://github.com/icchy/isucon12q/commit/28c16690
  - シン・ウー馬場ーイー2 "visitに変更 4936": new `visit` table filled in init.sql from GROUP BY, error on insert ignored — https://github.com/matsuu/isucon12q/commit/169314e4 (3,831 → 4,936)
  - Official 解説 step 6 (322万行→20万行, "mysqlの負荷は大体1/10くらい" together with index + ID change) — https://isucon.net/archives/56842718.html
  - Alternative (no compaction): covering index `(tenant_id, competition_id, player_id, created_at)` — official 解説 step 1.
- regulation note: schema changes allowed (「データベーススキーマの変更やインデックスの作成・削除」).

### T: Replace `REPLACE INTO id_generator` with app-side IDs (UUID / xid / tenant-prefixed time)
- what: `dispenseID()` does `REPLACE INTO id_generator (stub) VALUES ('a')` on adminDB with a deadlock-retry loop (official isuports.go L101-125) for every player, competition, and every CSV score row. Replace with UUID v4 (hyphens stripped), xid, or `${tenantId}-${unixnano}`.
- when it works: IDs are `VARCHAR(255)` (tenant schema) so any unique string works. Official 講評: 「IDは文字列型なので、UUIDやULID、他にも一意になるような文字列であれば代用できる」. Regulation: 「サーバー側で生成する部分（IDなど）は文字種を変えない範囲で自由に生成しても良い」.
- how to verify mid-contest: pt-query-digest / slow log top entry is `REPLACE INTO id_generator`; schema shows VARCHAR id columns; check the regulation for ID character-set restrictions (ISUPORTS: hex string in reference, UUID hex-without-hyphens is safe).
- risk: 織時屋 stripped hyphens from UUID to stay within the original character class (commit "UUIDの-を削除するようにした"). Keep IDs unique across app servers if you go multi-app.
- evidence:
  - シン・ウー馬場ーイー2 "uuid 3831" (2,561 → 3,831) — https://github.com/matsuu/isucon12q/commit/cdf251ee ; blog says mysql-query-digest ranked it top — https://matsuu.hatenablog.com/entry/2022/07/24/153403
  - 織時屋 "idの生成をuuidにした" / "UUIDの-を削除" — https://github.com/oribe1115/isucon12-qualify/commit/60112d79 , https://github.com/oribe1115/isucon12-qualify/commit/95a1d935
  - と: dispenseID → xid — https://toqoz.hateblo.jp/entry/2022/07/25/201939
  - ソレイユ: Redis INCR-based dispense_id (17:00) — https://diary.hatenablog.jp/entry/2022/07/26/022052 (blog only)
  - Official 解説 step 5 (`${tenantId}-${unixepochnano}`, "倍近くになるはず") — https://isucon.net/archives/56842718.html
- regulation note: as above; also appears in 本選 (see T: Snowflake IDs).

### T: Remove flock; use a single DELETE+INSERT transaction for CSV score import
- what: The reference takes a per-tenant file lock (`flockByTenantID`) in score import, ranking, player, and billing handlers. Replace with one transaction around DELETE + bulk INSERT (SQLite or MySQL) so readers never see the empty window.
- when it works: The only reason for the lock is the comment `// DELETEしたタイミングで参照が来ると空っぽのランキングになるのでロックする` (official isuports.go ~L1052). Official 講評: 「DELETEとINSERTを1トランザクションで実行すれば、排他のための `flock` を丸ごとなくすことができます」.
- how to verify mid-contest: pprof shows time in flock/`syscall.Flock`; CPU is under-used (織時屋: "CPUが50%程度しか使われていないこと、そのほとんどがMySQLであること"). Confirm that every writer is DELETE-then-INSERT and wrap it.
- risk: Dropping flock without a transaction fails the bench's 整合性チェック (official 解説: 「トランザクション張らずにflockを外すと…ベンチマーカーの整合性チェックフェーズで引っかかります」). On MySQL the DELETE+INSERT pattern deadlocks under concurrent imports — see next technique. Note the README's reproduce mode: the real-day bench retried empty rankings without sleep, so an empty ranking window was punished harder on the day (https://github.com/isucon/isucon12-qualify README).
- evidence:
  - 第7西東京市 "feat: remove flock" (comments out both lock sites) 7,972 → 8,662 — https://github.com/icchy/isucon12q/commit/cb25ef8a
  - シン・ウー馬場ーイー2 "flockなくしてトランザクションにした 43629" (34,464 → 43,629) — https://github.com/matsuu/isucon12q/commit/488645a6
  - たつや (3rd): removed flock → DB transactions — https://ryotarai.hatenablog.com/entry/2022/07/29/183652
  - 釜中の鯖: in-process mutex instead of flock — https://blog.p1ass.com/posts/isucon12/
- regulation note: none specific; appears only in 予選.

### T: Deadlock-tolerant score import (retry on 1213, or append-only + "latest row" semantics)
- what: With flock gone and MySQL in play, concurrent `DELETE … WHERE tenant_id=? AND competition_id=?` + bulk INSERT deadlocks. Fixes seen: retry transaction on MySQL error 1213; or stop deleting — `REPLACE INTO last_player_score` then `DELETE … AND created_at < now`; or append-only with a commit-tracking table.
- when it works: Error logs show `Error 1213: Deadlock found`; score import is a 10-pt Critical endpoint (a failure costs 10 %).
- how to verify mid-contest: grep app log for 1213; check whether import handler deletes by range before inserting.
- risk: Retry loop with sleep increases latency under load; the ranking must still reflect the last CSV for each player.
- evidence:
  - 第7西東京市 "feat: DeadLockの修正" (retry up to 10x with 50 ms sleep on 1213) 11,346 → 23,121 — https://github.com/icchy/isucon12q/commit/52c7a816
  - シン・ウー馬場ーイー2 "latest_scoreの更新方針を変更" (REPLACE INTO then DELETE older-than-now) — https://github.com/matsuu/isucon12q/commit/269d6d1b
  - たつや: "スコア入稿部分でデッドロックが発生していた" → append-only + `player_score_commit` table — https://ryotarai.hatenablog.com/entry/2022/07/29/183652
  - ソレイユ: 16:18 fixed deadlock on CSV import, switched to bulk insert — https://diary.hatenablog.jp/entry/2022/07/26/022052
- regulation note: none.

### T: Keep only the last CSV row per player (player_score 370万行 → 18万行)
- what: Store one row per (tenant, competition, player) = the last occurrence in the CSV, as a PK. Ranking/player queries no longer need `ORDER BY row_num DESC` / `HAVING max(row_num)`.
- when it works: Official 講評: 「各参加者ごとにCSV内で出現する最後の行がリーダーボードで有効になるため、その1行のみを保存すれば十分です」「`player_score` テーブルは初期状態の全テナント合計370万行から、18万行程度に削減されます」. Confirm in code: ranking reads `… ORDER BY row_num DESC` and dedupes by player in app.
- how to verify mid-contest: read `competitionRankingHandler`/`playerHandler` for dedupe-by-player loops; count rows vs distinct players in a tenant DB.
- risk: Dedupe must happen while parsing the CSV (keep last). Needs an initial-data migration step within `/initialize` (or pre-built data restored by copy).
- evidence:
  - 第7西東京市 `sql/tenant/95_patch.sql` (PRIMARY KEY (tenant_id, competition_id, player_id), window `row_number()` to pick rn=1) 45,857 → 66,960 — https://github.com/icchy/isucon12q/commit/16e60eae
  - ソレイユ: `player_last_score` table (12:45) — https://diary.hatenablog.jp/entry/2022/07/26/022052
  - シン・ウー馬場ーイー2 `last_player_score` — https://github.com/matsuu/isucon12q/commit/269d6d1b
- regulation note: schema change allowed. 予選 only.

### T: SQLite → MySQL migration with pre-built physical data restored in /initialize
- what: Convert all tenant .db files into MySQL tables (tenant_id already exists on every row) and make `/initialize` restore from a pre-built state (copy of `/var/lib/mysql`, or pre-loaded tables + `DELETE … WHERE id > N` style trimming) rather than re-importing.
- when it works: Tenant tables already carry `tenant_id` (official tenant schema) so a single table per entity works: 「tenantDBの各テーブルにはtenant_idカラムが含まれておりきちんとデータが入っていたので、テーブルを分ける必要はないと判断」 (matsuu). Manual: `/initialize` has a 30 s timeout; init.sh just `cp -r ../../initial_data/*.db` for SQLite.
- how to verify mid-contest: time a full import; if >30 s you need physical restore or data compaction first (visit_history/player_score shrink makes import feasible — 講評). Confirm you can observe MySQL (slow log, pt-query-digest) — this was matsuu's stated reason: 「SQLiteのままではOODAループを回すための最初のO(Observe)ができない」.
- risk: Migration scripts bite: 第7西東京市's converter had `for i in (1, 100)` instead of `range(1, 101)` and only loaded 2 tenants, causing initialize timeouts and a rollback to SQLite 12:40-14:50 (「この辺りで予選落ちも覚悟」). 織時屋's migration hit `max_allowed_packet` on bulk insert and after batching into 5,000 rows the score halved; they reverted. 帰ってきた鍋部 migrated before fixing N+1 and ended with 0 points. Teams that stayed on SQLite also made finals (釜中の鯖 7th, と 11th, 織時屋 12th), so this is optional.
- evidence:
  - 第7西東京市 "feat: sqlite3 to MySQL" — https://github.com/icchy/isucon12q/commit/8eb612b8 ; restore `/var/lib/mysql` on each `/initialize` and 2-server split 8,662 → 45,857 at 15:45 — https://icchy.hatenablog.jp/entry/2022/07/26/000905 (restore method from blog only)
  - シン・ウー馬場ーイー2 "mysqlへplayerとplayer_scoreを移動 13349", "competitionをmysqlに移動した 15672" — https://github.com/matsuu/isucon12q/commit/1018b887 , https://github.com/matsuu/isucon12q/commit/3493bba7 ; blog https://matsuu.hatenablog.com/entry/2022/07/24/153403
  - たつや: used the `sqlite3-to-sql` script shipped in the repo; tenant ID mod 2 across two MySQL servers — https://ryotarai.hatenablog.com/entry/2022/07/29/183652
  - 織時屋 failure — https://trap.jp/post/1640/ ; 帰ってきた鍋部 failure — https://zenn.dev/catatsuy/articles/da25f1128650b1
  - Official 講評 on the tension and 30 s — https://isucon.net/archives/56850281.html
- regulation note: 「データベースに利用するミドルウェアの変更」 allowed; data written during the bench must persist across reboot (MySQL on disk satisfies this).

### T: Staying on SQLite: indexes on every tenant DB + index-at-create
- what: Add `CREATE INDEX … ON player_score(tenant_id, competition_id, player_id)` to all 100+ existing tenant .db files (loop in shell) and to the tenant schema used by `createTenantDB`.
- when it works: Tenant schema has no secondary index (official `webapp/sql/tenant/10_schema.sql`); ranking/player queries filter on (tenant_id, competition_id[, player_id]).
- how to verify mid-contest: `sqlite3 x.db .indexes`; remember `/initialize` copies the original files back, so indexes must be re-applied in init (or applied to the initial_data copies).
- risk: 釜中の鯖 reflected that indexes "didn't apply to pre-existing SQLite tenant databases—a critical oversight" (https://blog.p1ass.com/posts/isucon12/). Official 解説 warns SQLite is single-writer: keep 1-pt APIs from blocking 10-pt APIs (「1点のAPIが10点のAPIのクエリを邪魔してはいけない」).
- evidence:
  - 織時屋 `add_index.sh` loops `cat ../add_inedx.sql | sqlite3 $var` over all .db (17:10 → ~26,000) — https://github.com/oribe1115/isucon12-qualify/commit/9c4304c8
  - シン・ウー馬場ーイー2 "sqliteインデックス追加 6785" (4,936 → 6,785) — https://github.com/matsuu/isucon12q/commit/f635445c
  - Official 解説 step 8 (1.5x) — https://isucon.net/archives/56842718.html
- regulation note: none.

### T: Ranking materialisation / cache (rebuild on score import; cache finished competitions forever)
- what: Keep a `ranking` table (or in-memory JSON) rebuilt when a CSV is imported; serve `GET /api/player/competition/:id/ranking` from it with rank_after pagination. For finished competitions the response never changes, so cache indefinitely.
- when it works: Ranking only changes on score import and on disqualification; the manual allows 3 s for disqualification to propagate to ranking: 「POST /api/organizer/player/:player_id/disqualified … GET /api/player/competition/:competition_id/ranking … 3秒の猶予」. Finished competitions cannot receive new CSVs (app manual: 「大会を終了すると入稿できなくなる」).
- how to verify mid-contest: alp: ranking is the most frequent 1-pt endpoint; check `competitionScoreHandler` is the only writer of player_score.
- risk: Still must record the visit (visit_history) on every call for billing. Cache must be purged on import/disqualify; multi-app needs shared cache or sticky routing. 釜中の鯖's ranking cache initially FAILed due to nginx `worker_connections` ("Connection reset by peer"), fixed with `worker_connections`/`worker_rlimit_nofile` (+15,000 pts in the last 30 min).
- evidence:
  - Zatsu-X-Club "Feature/bilbefore": in-memory `rankCache` keyed by competition, filled only when `competition.FinishedAt.Valid` — https://github.com/Zatsu-X-Club/isucon12q/commit/e7f15cec
  - と: `ranking` table for pagination (16:00-17:00) — https://toqoz.hateblo.jp/entry/2022/07/25/201939
  - たつや: JSON cache for two APIs purged on score submission — https://ryotarai.hatenablog.com/entry/2022/07/29/183652
  - 釜中の鯖: ranking cache + nginx fix — https://blog.p1ass.com/posts/isucon12/
  - Official 解説 step 9 (`INSERT … ON DUPLICATE KEY UPDATE` into ranking table) — https://isucon.net/archives/56842718.html
- regulation note: 「キャッシュ機構の追加」 allowed.

### T: Player detail cache with short TTL and explicit forget on import/disqualify
- what: Cache `GET /api/player/player/:id` result per (tenant, player) for ~3 s, and `Forget` on CSV import and disqualification.
- when it works: Manual's 3 s grace for disqualification propagation to `GET /api/player/player/:player_id`. 釜中の鯖 also cached the disqualified flag for 3 s (「失格情報を 3 秒キャッシュする」).
- how to verify mid-contest: alp hit counts for player endpoint; measure cache hit rate (織時屋 saw ~30 %).
- risk: Low immediate gain (織時屋: "minimal score impact"), pays off only after bigger bottlenecks are gone. Disqualified players must get 403 within 3 s (Critical error otherwise).
- evidence:
  - 織時屋 "playerHandlerにキャッシュを実装" using `sc.New(..., 3*time.Second, 3*time.Second)` with `forgetPlayerHandlerCache` in disqualify and import — https://github.com/oribe1115/isucon12-qualify/commit/21dabb65
  - Zatsu-X-Club "playerCacheの作成 score 3343", "playerscoreのキャッシュ" — https://github.com/Zatsu-X-Club/isucon12q/commit/4981e07b , https://github.com/Zatsu-X-Club/isucon12q/commit/615d742f
  - 釜中の鯖 — https://blog.p1ass.com/posts/isucon12/
- regulation note: manual 「反映までの猶予時間について」 section.

### T: Bulk INSERT for CSV scores and players/add; N+1 removal in ranking/player
- what: One multi-row INSERT for all CSV rows (and players/add), JOIN `player` to get display_name in ranking instead of per-row SELECT, dedupe player existence check with a set.
- when it works: Reference does `NamedExecContext` per row and `retrievePlayer` per CSV row; CSV rows per import are in the thousands.
- how to verify mid-contest: slow log counts of `INSERT INTO player_score` ≈ rows; pprof in import handler.
- risk: SQLite variable limit / MySQL `max_allowed_packet` (織時屋 hit this on migration); chunk large batches.
- evidence:
  - 第7西東京市 "feat: CSVの挿入をBulkで実行" (7,972), "fix n+1 for CSV read" (11,346), "feat: Add Playerのバルク実行" — https://github.com/icchy/isucon12q/commit/c764dc13 , https://github.com/icchy/isucon12q/commit/42367c59 , https://github.com/icchy/isucon12q/commit/eaa83f2e
  - 織時屋 "competitionScoreHandlerでbulk insert" (13:50 12,700), "バルクインサート" playersAdd (16:35 23,000), ranking N+1 — https://github.com/oribe1115/isucon12-qualify/commit/cedbf28c , https://github.com/oribe1115/isucon12-qualify/commit/f5920728 , https://github.com/oribe1115/isucon12-qualify/commit/813ad8aa
  - シン・ウー馬場ーイー2 "N+1を消した 34464" (15,672 → 34,464), "insert中のretrievePlayerを削除" — https://github.com/matsuu/isucon12q/commit/5c629449 , https://github.com/matsuu/isucon12q/commit/ee8ce3d7
  - Official 解説 steps 2-3 — https://isucon.net/archives/56842718.html
- regulation note: none. Also used in 本選 (obtainPresent/gacha bulk).

### T: Tenant-aware multi-server routing by subdomain (SQLite on several hosts)
- what: nginx routes by `$host` tenant-name pattern (e.g. subdomain length buckets) to 3 app servers each owning a subset of tenant .db files; admin goes to server 1; tenant DB file creation deferred to first access on the owning host.
- when it works: Tenant is identified by host (`<tenant>.t.isucon.dev`), each tenant has its own SQLite file, and cross-tenant reads exist only in admin billing (which can call the other hosts or use MySQL).
- how to verify mid-contest: confirm the only cross-tenant endpoint is `GET /api/admin/tenants/billing`; check `POST /api/admin/tenants/add` creates the DB file (move creation to first tenant access).
- risk: 織時屋 prototyped tenant-per-server proxying and abandoned it with 20 min left because admin aggregation was unhandled; 釜中の鯖 left server 3 unused because SQLite is file-local. New tenants created during the bench must land on the right host.
- evidence:
  - Official 解説 steps 10-11 (nginx `if ($host ~ "^[a-z0-9-]{1,9}.t.isucon.dev")` buckets, AddTenant deferral) — https://isucon.net/archives/56842718.html
  - 織時屋 abandoned attempt — https://trap.jp/post/1640/
  - たつや: tenant ID mod 2 across two MySQL hosts (after migration) — https://ryotarai.hatenablog.com/entry/2022/07/29/183652
- regulation note: 「複数台あるサーバーの役割の変更」 allowed; only the 3 CloudFormation EC2s may serve traffic.

### T: App/DB server split + extra app server behind nginx (3-node layout)
- what: Server 1 nginx + app, server 2 MySQL, server 3 second app (after MySQL migration). Raise `SetMaxOpenConns` (10 → 50) and `interpolateParams=true`.
- when it works: After moving tenant data to MySQL the DB is the shared bottleneck; the reference pins `adminDB.SetMaxOpenConns(10)`.
- how to verify mid-contest: top on each host; MySQL CPU vs app CPU.
- risk: Cross-host MySQL needs `bind-address` change and the app must wait for DB at boot (systemd `StartLimitBurst=999` / DB ping loop) so the reboot test passes.
- evidence:
  - 第7西東京市: 2-server split 8,662 → 45,857; "feat: DBの設定の更新" (MaxOpenConns 50, InterpolateParams) — https://github.com/icchy/isucon12q/commit/d3581d9f ; blog https://icchy.hatenablog.jp/entry/2022/07/26/000905
  - シン・ウー馬場ーイー2 "サーバ分割 63991" (44,051 → 63,991), "3台活用 72760", "interpolateなど 20303" — https://github.com/matsuu/isucon12q/commit/26db1724 , https://github.com/matsuu/isucon12q/commit/ae764d27 , https://github.com/matsuu/isucon12q/commit/ca0daccd ; `StartLimitBurst=999` noted in https://matsuu.hatenablog.com/entry/2022/07/24/153403
  - Zatsu-X-Club "Db split (#37)" (mysqld bind-address commented out, app points at isuports-1.t.isucon.dev), "再起動対策にDBpingを導入" — https://github.com/Zatsu-X-Club/isucon12q/commit/75d8314f , https://github.com/Zatsu-X-Club/isucon12q/commit/49a328eb
- regulation note: manual: `isuports-1..3.t.isucon.dev` are pre-registered in /etc/hosts and may be used freely.

### T: Disable logging / switch JSON encoder / go-json (late free points)
- what: Turn off echo access/debug logs, nginx access log, MySQL slow log; replace encoding/json with goccy/go-json.
- when it works: Always at the end; ~5-10 % each.
- how to verify mid-contest: profile shows json encode / log write.
- risk: Keep the ability to re-enable logs for debugging; remember to disable pprof/pprotein collectors that call out (external calls that improve score are forbidden; pure metrics are allowed).
- evidence:
  - 第7西東京市 "feat: echoのログの無効化" (66,960 → 76,721), "feat: use go-json" (→ 86,872) — https://github.com/icchy/isucon12q/commit/6e6175fb , https://github.com/icchy/isucon12q/commit/8ab959f9
  - 織時屋 log removal +~5,000 (17:30) — https://trap.jp/post/1640/
  - ソレイユ: debug logging off ≈ +10 % — https://diary.hatenablog.jp/entry/2022/07/26/022052
  - と: go-json, LimitNOFILE — https://toqoz.hateblo.jp/entry/2022/07/25/201939
- regulation note: appears in both rounds (本選: NaruseJun "logoff", 織時屋 17:44 190,894 → 231,371, 釜中の鯖 "Turn off Logs", innocent "echoのLogger消す").

### T: MySQL durability relaxation + binlog off + buffer pool (both rounds)
- what: `disable-log-bin`, `innodb_flush_log_at_trx_commit=0/2`, `innodb_doublewrite=0`, bigger `innodb_buffer_pool_size`, redo log on /dev/shm (with AppArmor + tmpfiles.d entries).
- when it works: Write-heavy bench (score import; 本選 presents/sessions). Default config has binlog on and 128 M buffer pool (matsuu's mysqltuner output in repo).
- how to verify mid-contest: `result/mysqltuner.txt` style check; `SHOW VARIABLES LIKE 'log_bin'`.
- risk: Redo log on tmpfs is lost on reboot — data written during the bench must still be readable after reboot; matsuu's setup used `innodb_flush_log_at_timeout=3`, so a crash loses ≤3 s. Only acceptable because the organisers reboot cleanly (`sudo systemctl reboot`, 本選 manual) — still a gamble. AppArmor must allow /dev/shm.
- evidence:
  - シン・ウー馬場ーイー2 予選 "visitに変更" includes `disable-log-bin` — https://github.com/matsuu/isucon12q/commit/169314e4 ; 本選 "mysqlチューニング 12503" (7,888 → 12,503) full config incl. AppArmor and tmpfiles — https://github.com/matsuu/isucon12f/commit/17d6993b
  - Official 解説 step 13 — https://isucon.net/archives/56842718.html
- regulation note: 「ベンチマーク実行時にアプリケーションに書き込まれたデータは再起動後にも取得できること」.

### T: Nginx upstream keepalive / worker limits (connection reset fix)
- what: `proxy_http_version 1.1; proxy_set_header Connection "";` + `keepalive` upstream, raise `worker_connections`, `worker_rlimit_nofile`, systemd `LimitNOFILE`.
- when it works: Bench reports "connection reset by peer" / 502 once the app gets fast and concurrency climbs.
- how to verify mid-contest: nginx error.log; bench error text.
- risk: SysCa was disqualified post-reboot because nginx returned 502 on all three runs (https://isucon.net/archives/56838181.html) — make sure nginx and app come up in order after reboot.
- evidence:
  - 釜中の鯖 FAIL → fix → +15,000 — https://blog.p1ass.com/posts/isucon12/
  - シン・ウー馬場ーイー2 "nginx周りをチューニング" (予選), "nginxチューニング 37716" (本選, 20,011 → 37,716) — https://github.com/matsuu/isucon12q/commit/c62ffa20 , https://github.com/matsuu/isucon12f/commit/95da037a
  - Official 解説 step 12 — https://isucon.net/archives/56842718.html
  - NaruseJun 本選 `worker_cpu_affinity auto` — https://github.com/narusejun/isucon12-final/commit/1526f214
- regulation note: both rounds.

### T: Use the 429 Retry-After escape hatch for tenants/add and competitions/add (not observed used)
- what: Manual allows `POST /api/admin/tenants/add` and `POST /api/organizer/competitions/add` to return 429 + Retry-After; bench waits and retries without scoring.
- when it works: If tenant DB creation (SQLite file + schema via `sqlite3` subprocess) becomes the lock hotspot.
- how to verify mid-contest: read the manual section 「429 Too Many Requestsについて」.
- risk: No points for those requests; I found no top team using it (unverified that anyone did).
- evidence: manual — https://gist.github.com/mackee/4320c18919c8f6f1867849378a17e651
- regulation note: explicitly permitted by manual.

---

## 本選 ISUCONQUEST techniques

### T: App-side Snowflake IDs (drop `UPDATE id_generator SET id=LAST_INSERT_ID(id+1)`)
- what: Reference `generateID()` does a single-row UPDATE on `id_generator` with a 100x deadlock-retry loop (official main.go L1855-1875), called many times per login/gacha/present. Replace with bwmarrin/snowflake (node id per server via env) or an atomic counter seeded above the initial max.
- when it works: IDs are BIGINT and nothing depends on contiguity; `6_id_generator_init.sql` sets `id=100000000001` so a counter starting above that avoids collisions with initial data. Official 講評: 「SnowflakeIDのような、App側での採番を想定していました。」
- how to verify mid-contest: slow log top = `UPDATE id_generator`; count generateID calls per request.
- risk: Atomic in-process counters collide after restart or across app servers unless seeded with time / server id (織時屋 first used `(next<<3)|serverId` seeded at `100000000001/8`, then switched to snowflake because of "restart collisions"). With app-side sharding by id, Snowflake's low bits are timestamp-skewed — NaruseJun had to shard on `id>>23` ("fix bure") to balance DBs. Users' IDs must survive reboot.
- evidence:
  - NaruseJun "snowflake" (8,315 → 24,498 at 10:50; blog "generateIDが重い問題") — https://github.com/narusejun/isucon12-final/commit/f0a73553 ; shard key fix `dbs[int(id>>23)%len(dbs)]` — https://github.com/narusejun/isucon12-final/commit/8f51513e ; blog https://zenn.dev/tohutohu/articles/8c34d1187e1b21
  - 織時屋 "Optimize id generator" (atomic+serverId; 13,291 → 16,798) then "use snowflake as id gen" (16:03 160,260) — https://github.com/oribe1115/isucon12-final/commit/5ec9c9d1 , https://github.com/oribe1115/isucon12-final/commit/67a3faf4
  - 釜中の鯖 "Update ID generation" (`uniqueIdBase + n*100000000000`) — https://github.com/saba-in-the-kettle/isucon12-final/commit/d5472517
  - innocent "UnixTime generateID", "lastIDばらけさせる" — https://github.com/innocent-team/isucon12f/commit/d47dfbf3 , https://github.com/innocent-team/isucon12f/commit/88ff5820
  - シン・ウー馬場ーイー2 instead used MySQL AUTO_INCREMENT on each table ("AUTO_INCREMENTに変更 20011", 12,503 → 20,011) — https://github.com/matsuu/isucon12f/commit/ffd64578
- regulation note: same ID rule as 予選 (server-generated parts free within character type). Appears in both rounds.

### T: Master data in process memory keyed by master_version, refreshed via version check + singleflight
- what: Load `item_masters`, `login_bonus_masters`, `login_bonus_reward_masters`, `gacha_masters`, `gacha_item_masters`, `present_all_masters` into memory. `apiMiddleware` already reads `SELECT * FROM version_masters WHERE status=1` per request; use that version string as the cache key and re-load when it changes; wrap the reload in `singleflight` so a master update does not stampede.
- when it works: Masters change only via `PUT /admin/master` (CSV upload, `INSERT … ON DUPLICATE KEY UPDATE`, official admin.go L217-490), which the bench calls once around 20 s into the run; the request header `x-master-version` must match the active version (422 otherwise). Official 講評: 「マスターテーブルのデータは、全てキャッシュに入れる想定でした。」「キャッシュのキーにマスターバージョンのプレフィックスをつけてキャッシュする手法が、パージ漏れを無くす意味でも、有効な手法だと考えています。」
- how to verify mid-contest: find every write path to `*_masters` (only admin.go); confirm `/initialize` resets masters (init.sh reloads schema+data) so the cache must be reset there too.
- risk: Multi-app: each app must notice the version change (NaruseJun kept the per-request version SELECT for that reason: 「複数台でアプリを動かすことを想定して毎回master_versionだけは確認」), or push the update to all apps. innocent's late master cache "failed restart test" and was dropped (https://blog.utgw.net/entry/2022/08/29/112033). 本選 manual: 「PUT /admin/masterのエンドポイントが成功していなかった場合 … 追試の時の目視チェックでも確認します」; FetchDecodeExecWrite and チームO were first judged "ログインできない" in the browser until organisers reset the master version (https://isucon.net/archives/56923294.html).
- evidence:
  - NaruseJun "login_bonus_masters, item_mastersのキャッシュ" (`shouldRecache` compares version, `recache` under RWMutex; 13:50 → 178,000) — https://github.com/narusejun/isucon12-final/commit/11a556d1 ; "recache singleflight" (score stabilised 20万〜27万 → 27万) — https://github.com/narusejun/isucon12-final/commit/2de5b6e2 ; "一台のとき特別対応" (`ISUCON_ICHIDAI` skips the version SELECT, admin update calls `forceRecache`) — https://github.com/narusejun/isucon12-final/commit/c7b5f821
  - 釜中の鯖 "versionmaster をキャッシュする" (go-cache, set from adminUpdateMaster) — https://github.com/saba-in-the-kettle/isucon12-final/commit/3bcd8a4a
  - 織時屋 chose replication of masters to every DB shard instead (see sharding) — https://github.com/oribe1115/isucon12-final/commit/829b6d9d
- regulation note: cache allowed; `/initialize` must reset it.

### T: User-ID sharding across 4 MySQL hosts; masters replicated to every shard; admin double/triple-write
- what: `selectDatabase(userID)` → one of 2-4 MySQL servers; all `user_*` tables live on the shard of their user; master tables are written to every shard by `PUT /admin/master` (fan-out with errgroup/WaitGroup); `/initialize` fans out to `/initializeOne` on each DB host in parallel.
- when it works: Every user-facing query is scoped by `user_id` (sessions, presents, items, cards, decks, login bonuses, bans); only admin endpoints are cross-user. Official 講評: 「App 1台、DB 4台が良いバランスだと思います。」 and 「受け取り前のプレゼントテーブルと、受け取り済みのプレゼント履歴テーブルにわけ、テーブルのデータ件数を減らした上で、水平分割することを想定していました。」
- how to verify mid-contest: grep queries without `user_id` in WHERE (only admin + masters); check `init.sh` runtime per host fits in 60 s when run in parallel.
- risk: (1) Session lookup happens before the user id is known → embed user id in the session id (next technique). (2) Hash must balance: 織時屋's XOR hash was uneven, switched to xxHash; NaruseJun used `id>>23` for Snowflake. (3) Transactions no longer span shards — 織時屋 marked `//TXをはがしたので要注意` where user_presents inserts left the tx. (4) Each shard needs full `/initialize` within 60 s; 織時屋 pushed initialize to the other hosts ("initializeを別の2台に投げる"). (5) Admin ban must hit the right shard (NaruseJun "balance" fixed adminUser/adminBanUser to `selectDatabase(userID)`).
- evidence:
  - NaruseJun "DoubleWrite Admin" (db.go `selectDatabase(id)=dbs[id%len]`, `/initializeOne` fan-out) → "balance" → "4 DB" (14:08 → 210,000) — https://github.com/narusejun/isucon12-final/commit/b19e8870 , https://github.com/narusejun/isucon12-final/commit/78ddd0c9 , https://github.com/narusejun/isucon12-final/commit/81793a75 ; blog 13:45 ~162,000 for 2 DBs — https://zenn.dev/tohutohu/articles/8c34d1187e1b21
  - 織時屋 "user_presentsをDB分散" (`getDB(userID)=…[userID%3]`, 14:16 71,078), "use xxhash", "masterをすべてのユーザー用DBに転送", "admin以外ですべてユーザーDBを使う", 5 hosts (15:47 147,250) — https://github.com/oribe1115/isucon12-final/commit/78f46a3e , https://github.com/oribe1115/isucon12-final/commit/9ce6c29a , https://github.com/oribe1115/isucon12-final/commit/829b6d9d , https://github.com/oribe1115/isucon12-final/commit/298dc1f5 ; blog https://trap.jp/post/1710/
  - innocent "userIDごとに接続先を振り分ける", "admin.go triple write DB", "/initialize で各DBに向けて初期化する" (15:28 119,577) — https://github.com/innocent-team/isucon12f/commit/74a65ad8 , https://github.com/innocent-team/isucon12f/commit/012ec31f , https://github.com/innocent-team/isucon12f/commit/7f3b9cfa
  - 釜中の鯖 "Use two DBs" → 4 DBs (~120K → 200K+) — https://github.com/saba-in-the-kettle/isucon12-final/commit/3dcb12b5 ; https://blog.p1ass.com/posts/isucon12-final/
  - シン・ウー馬場ーイー2 did NOT shard during the contest (2 servers, 106,254) and reached 391,711 in 延長戦 after sharding: 「シャーディングを自分で実装した経験がなかった」「想像していたよりは簡単に実装できた」 — https://matsuu.hatenablog.com/entry/2022/09/11/154640
- regulation note: only the 5 provided servers; all shards must come back after `sudo systemctl reboot` and 10+ min wait.

### T: Embed user id in session id / one-time token so the shard can be chosen before auth
- what: Session id becomes `"<uuid>::<userID>"` (or `"<userID>,<uuid>"`); `checkSessionMiddleware` parses the user id from the token to pick the shard, then validates.
- when it works: Needed as soon as `user_sessions` is sharded; the session string is opaque to the client (bench only echoes `x-session`).
- how to verify mid-contest: look at where `SELECT * FROM user_sessions WHERE session_id=?` runs relative to `getUserID(c)`.
- risk: innocent lost time to integrity-check failures: 「セッションキーを発行した先のユーザー用のDBを見ることができておらず」 until the fix at 17:23. Admin sessions are separate (keep on shard 0).
- evidence:
  - NaruseJun "sticky session" — https://github.com/narusejun/isucon12-final/commit/b9ec84a7
  - innocent "user_sessionにuser idを含める" — https://github.com/innocent-team/isucon12f/commit/964a1068 ; blog https://blog.utgw.net/entry/2022/08/29/112033
  - 織時屋 changed `user_session` PK to user_id (blog 16:03) and "user_sessionsのunique制約からdeleted_atを削除" — https://github.com/oribe1115/isucon12-final/commit/32e2d15e
- regulation note: response structure unchanged (session id is still a string).

### T: Hard-delete sessions/one-time tokens instead of soft-delete; unique index on token
- what: Replace `UPDATE … SET deleted_at=?` + `WHERE deleted_at IS NULL` with `DELETE`, drop `deleted_at` from unique keys, and index `(token, token_type)` / `session_id`.
- when it works: Nothing reads expired/used sessions or tokens afterwards (grep: only `checkSessionMiddleware`, `checkOneTimeToken`, `login`, `listGacha`, `listItem`). The one-time-token design is explained in the app manual (「処理自体は１回しかしないように制御している」) — deleting is semantically the same.
- how to verify mid-contest: check the unique key `uniq_token (user_id, token, deleted_at)` and `uniq_session_id (user_id, deleted_at, session_id)` in `3_schema_exclude_user_presents.sql`; slow log for `SELECT * FROM user_one_time_tokens WHERE token=?`.
- risk: Browser verification after reboot must still log in; nothing else observed.
- evidence:
  - 織時屋 "one time token hard delete" + "add token, token_type index", "expiredなuser_sessionsをhard deleteするようにした" — https://github.com/oribe1115/isucon12-final/commit/eb97474e , https://github.com/oribe1115/isucon12-final/commit/b48aea04 , https://github.com/oribe1115/isucon12-final/commit/dabbf598
  - シン・ウー馬場ーイー2 "StartLimitBurstとSELECTしてからUPDATEをUPDATEだけに" (DELETE sessions, `UNIQUE (session_id)`, 78,910 → 85,692) — https://github.com/matsuu/isucon12f/commit/115b7759
  - NaruseJun "user_one_time_tokens INDEX" — https://github.com/narusejun/isucon12-final/commit/1123c521
- regulation note: none.

### T: user_presents index on (user_id, deleted_at, created_at) and dropping COUNT(*) in list
- what: `listPresent` does `SELECT * … WHERE user_id=? AND deleted_at IS NULL ORDER BY created_at DESC, id LIMIT 100 OFFSET ?` plus `SELECT COUNT(*)` for `isNext`. Add composite index; compute `isNext` by fetching LIMIT+1 instead of COUNT. Longer-term: split received presents into a history table (official expectation).
- when it works: `user_presents` is the biggest table (init.sh has a dedicated path: schema "exclude_user_presents", `LOAD DATA INFILE … 5_user_presents_not_receive_data.tsv`); present index page is a 3-pt request.
- how to verify mid-contest: `SHOW INDEX FROM user_presents`; slow log for the COUNT.
- risk: COUNT removal changes `isNext` semantics only if the implementation is wrong; verify with the browser (presents page).
- evidence:
  - 織時屋 "user_presents index" (11:09 18,073), blog describes (`user_id`,`deleted_at`) + `created_at DESC` — https://github.com/oribe1115/isucon12-final/commit/a73747b0 ; https://trap.jp/post/1710/
  - 釜中の鯖 "Reduce sql call" in improve-list-present (removed COUNT; blog ~200K+) — https://github.com/saba-in-the-kettle/isucon12-final/commit/d50bd1d1 ; https://blog.p1ass.com/posts/isucon12-final/
  - シン・ウー馬場ーイー2 "user_presentsのインデックスを調整" — https://github.com/matsuu/isucon12f/commit/a2152132
  - Official 講評 table-split expectation — https://isucon.net/archives/56959385.html
- regulation note: none.

### T: Composite index on user_present_all_received_history (user_id, present_all_id) — the first 10x
- what: `obtainPresent` checks per present_all_master whether the user already received it; the table only has a PK. Add `(user_id, present_all_id)` index (and `present_all_id` alone for admin).
- when it works: Every login (3 pt) calls `obtainPresent`; this was the top slow query at 10:30 for every team.
- how to verify mid-contest: slow log; `SHOW CREATE TABLE user_present_all_received_history` (PK only).
- risk: none observed.
- evidence:
  - 織時屋 "add idx_user_present_all_received_history_user_id_present_all_id" (600 → 13,291 at 10:36) — https://github.com/oribe1115/isucon12-final/commit/a3bddbd6
  - innocent "user_present_all_received_historyに複合インデックス" (10,749 → 20,264) — https://github.com/innocent-team/isucon12f/commit/6f586428
  - シン・ウー馬場ーイー2 "インデックス追加 7888" (534 → 7,888) — https://github.com/matsuu/isucon12f/commit/96f24f47
  - NaruseJun "add index one" (250 → 8,315) — https://github.com/narusejun/isucon12-final/commit/b8d52541
- regulation note: none.

### T: Bulk INSERT presents / bulk UPDATE on receive; obtainItem(s) batched per item type
- what: `obtainPresent`, `drawGacha` insert one `user_presents` row per present; `receivePresent` loops `obtainItem` → per item: SELECT then UPDATE/INSERT. Collect items by type (coin / card / material) and do one `UPDATE users SET isu_coin=isu_coin+?`, one multi-row INSERT for cards, `INSERT … ON DUPLICATE KEY UPDATE amount=amount+?` for materials, and one `UPDATE user_presents SET deleted_at … WHERE id IN (?)`.
- when it works: Bench receives up to 100 presents per call (`PresentCountPerPage=100`), gacha 10-pull; 2-3 pt requests.
- how to verify mid-contest: pprof shows `obtainItem` dominating `receivePresent`; count queries per request in slow log.
- risk: Big refactor late in the day; 織時屋 finished it at 17:40 (147,250 → 190,894) after parallelising by item type and debugging failures; NaruseJun reverted a buggy gacha rewrite at 17:00 (blog). Validation must be done up-front for all items (innocent: "同時にvalidationしないと常にErrItemNotFoundが返る"). 釜中の鯖's receive path returned success without inserting in an early-return branch and the browser check caught it (see risk section below).
- evidence:
  - NaruseJun "bulk insert", "batch update", "bulk update", "draw gacha の N+1", "obtain45items", "デッキ初期化のN+1をなくす" (24,498 → ~89,000 by 12:55) — https://github.com/narusejun/isucon12-final/commit/8a12622d , https://github.com/narusejun/isucon12-final/commit/740a9833 , https://github.com/narusejun/isucon12-final/commit/2ae8cb0a , https://github.com/narusejun/isucon12-final/commit/276f4df6 , https://github.com/narusejun/isucon12-final/commit/c39cbdaf
  - 織時屋 "receivePresent N+1" (27,927 → 29,008), "fix insert into user_presents N+1 in drawGacha", obtainItems PR #9 — https://github.com/oribe1115/isucon12-final/commit/d68a6b47 , https://github.com/oribe1115/isucon12-final/commit/ef4e9477 , https://github.com/oribe1115/isucon12-final/commit/dbe9950b
  - innocent "obtainPresentでbulk insert" (10,749), "ItemObtainerにまとめたあとbulk insert/updateするロジック" (43,862) — https://github.com/innocent-team/isucon12f/commit/35a19f4c , https://github.com/innocent-team/isucon12f/commit/f88f3aab
  - シン・ウー馬場ーイー2 "obtainItemでSELECTしてからUPDATEしているのをUPDATE一発に変更 54744" (`UPDATE users SET isu_coin=isu_coin+?`) — https://github.com/matsuu/isucon12f/commit/ac41d06b
  - 釜中の鯖 "bulk update", "coinをbulk insert", "ON DUPLICATE KEY UPDATE for multi-item receipt" (~20K) — https://github.com/saba-in-the-kettle/isucon12-final/commit/328d7657 , https://blog.p1ass.com/posts/isucon12-final/
- regulation note: none; same pattern as 予選 bulk CSV insert.

### T: Ban-check cache (negative cache with invalidation on /admin/user/:id/ban)
- what: `apiMiddleware` + `login` run `SELECT * FROM user_bans WHERE user_id=?` on every request. Cache "not banned" in a concurrent map; `adminBanUser` removes/sets the entry; reset on `/initialize`.
- when it works: Bans are written only by `POST /admin/user/:userID/ban` (admin.go); banned-user login scenario fires every 0.5 s (official 講評 scenario list) and must return the BAN response (1 pt each).
- how to verify mid-contest: grep writers of `user_bans`; confirm single app (or push invalidation to all apps).
- risk: Multi-app invalidation — NaruseJun enabled it only in 1-app mode ("Banを一台のとき", gated by `isIchidai`); 釜中の鯖 cached both true/false and set on ban.
- evidence:
  - NaruseJun "Banを一台のとき" (16:15 → 348,000 per blog) — https://github.com/narusejun/isucon12-final/commit/8bd330bc
  - 釜中の鯖 "user banを削減" — https://github.com/saba-in-the-kettle/isucon12-final/commit/52c5df32
- regulation note: cache allowed; must survive `/initialize` reset.

### T: Single app server with 1-app-only shortcuts (skip version SELECT, in-memory caches) + 4 DB
- what: Run one app (nginx + Go on s1) against 4 MySQL shards; when `ISUCON_ICHIDAI=1`, skip the per-request `version_masters` SELECT and trust the in-process master cache, since the only writer (`PUT /admin/master`) runs in the same process and calls `forceRecache`.
- when it works: App CPU is not the bottleneck after sharding; official layout advice 「App 1台、DB 4台」.
- how to verify mid-contest: app host CPU < DB hosts; alp latency dominated by DB.
- risk: If you later add a second app you must flip the flag off — otherwise masters/bans go stale. innocent split the app across 2 servers (134,670) and then hit the session/shard bug.
- evidence:
  - NaruseJun "一台のとき特別対応" (15:20 ~290,000 per blog) — https://github.com/narusejun/isucon12-final/commit/c7b5f821 ; blog layout "1 App (Nginx included) + 4 DBs" — https://zenn.dev/tohutohu/articles/8c34d1187e1b21
  - 織時屋 final: s2 app+nginx, s1/s3/s4/s5 DB — https://trap.jp/post/1710/
- regulation note: 5 servers only.

### T: Go runtime knobs: GOGC, sync.Pool for hot slices, CPU affinity, go-json, interpolateParams, pool sizing
- what: `GOGC=2000`, `sync.Pool` for `[]*UserPresent` etc., nginx `worker_cpu_affinity auto`, goccy/go-json, DSN `interpolateParams=true`, `SetMaxOpenConns/SetMaxIdleConns` tuned (織時屋 250 idle; matsuu 140; MySQL `max_connections=500`).
- when it works: After DB work is spread out, app GC/alloc shows up in pprof.
- how to verify mid-contest: pprof alloc profile; `runtime.gcBgMarkWorker` share.
- risk: High GOGC on a 4 GB box; fine for 60 s runs. innocent's go-json import was reverted at 17:32 (build issue) — https://github.com/innocent-team/isucon12f/commit/af6e8dbb.
- evidence:
  - NaruseJun "GOGC=200" (actually sets GOGC=2000), "cpu affinity", "*ArrPool" series, "go-json" — https://github.com/narusejun/isucon12-final/commit/752e7662 , https://github.com/narusejun/isucon12-final/commit/1526f214 , https://github.com/narusejun/isucon12-final/commit/2332077d , https://github.com/narusejun/isucon12-final/commit/19f2b130 ; post-contest 691,921 with GOMAXPROCS=1 + single nginx worker, UNIX socket, MariaDB, async writes (blog, post-contest only)
  - 織時屋 "InterpolateParams=true" (18,073 → 27,927), "mysqlのmax_connectionを500にした", "SetMaxIdleConns(250)" — https://github.com/oribe1115/isucon12-final/commit/481ee920 , https://github.com/oribe1115/isucon12-final/commit/c47e73ab , https://github.com/oribe1115/isucon12-final/commit/81d32852
  - innocent "interpolateParams=true" (43,862 → 54,594) — https://github.com/innocent-team/isucon12f/commit/5eaf8f3e
  - シン・ウー馬場ーイー2 "DB接続数周りを調整 61679" — https://github.com/matsuu/isucon12f/commit/934f458a
- regulation note: none; also used in 予選 (go-json, InterpolateParams).

### T: Avoid deadlock rabbit holes in 本選 (don't FOR UPDATE / reorder tables blindly)
- what: Negative result: under concurrent login/receive, matsuu spent 16:47-17:08 trying table-order changes and `FOR UPDATE`, score dropped 61,197 → 35,649, reverted. Root cause not found during the contest (「テーブルの更新順が原因と思い込んでしまった」). Prefer shrinking transactions (single UPDATE, bulk ops) and sharding, which removed contention for other teams.
- evidence: https://github.com/matsuu/isucon12f/commit/c1580763 , https://github.com/matsuu/isucon12f/commit/004e58ce , https://github.com/matsuu/isucon12f/commit/396dda00 ; https://matsuu.hatenablog.com/entry/2022/09/11/154640
- regulation note: none.

---

## Techniques present in both 予選 and 本選
- App-side ID generation replacing a MySQL single-row generator (UUID/xid in 予選; Snowflake/atomic in 本選).
- Bulk INSERT / bulk UPDATE and N+1 removal (CSV + players in 予選; presents/items/gacha in 本選).
- `interpolateParams=true`, connection pool sizing, go-json, log disabling at the end.
- MySQL durability/binlog relaxation and buffer pool increase (matsuu both rounds).
- nginx keepalive/worker limits/nofile.
- Splitting app and DB across hosts; 本選 extends to user-id sharding over 4 DB hosts.
- In-process caches with explicit invalidation keyed on the only write path (billing/ranking/player in 予選; masters/bans/sessions in 本選) and a reset in `/initialize`.
- Hard-delete / compaction of append-only tables whose readers only need one row (visit_history & player_score in 予選; sessions/tokens in 本選).

---

## Regulation highlights (ISUCON12)

予選 レギュレーション https://isucon.net/archives/56671734.html
- Allowed: 「複数台あるサーバーの役割の変更」「データベーススキーマの変更やインデックスの作成・削除」「データベースに利用するミドルウェアの変更」「キャッシュ機構の追加、ジョブキュー機構の追加による遅延書き込み」「他の言語による再実装」.
- Must not change: 「アクセス先のURI、ただしサーバー側で生成する部分（IDなど）は文字種を変えない範囲で自由に生成しても良い」「レスポンス (HTMLのDOM, JSONオブジェクト等) の構造」「JavaScript/CSSファイルの内容」「画像および動画等のメディアファイルの内容」.
- Reboot/data: 「サーバ再起動後にすべてのアプリケーションコードが正常動作する状態を維持すること」「ベンチマーク実行時にアプリケーションに書き込まれたデータは再起動後にも取得できること」「コンテスト進行用のメンテナンスコマンドが正常に動作するよう互換性を保つこと」.
- External: 「主催者の指示以外で利用が認められたサーバー以外の外部リソースを使用する行為は禁止」 but 「モニタリングやテスト、開発などにおいては、PCや外部のサーバーを利用しても構わない」.
- Disqualification: 「ベンチマークの結果に捏造があると判断される場合には、判断基準の公開と共にそのチームは失格とする」.

予選 当日マニュアル https://gist.github.com/mackee/4320c18919c8f6f1867849378a17e651
- Only the CloudFormation EC2s (c5.large x3) may be used; changing instance type = 失格; env-checker must pass at the end; mixed stacks on portal at end = 失格.
- Do not touch `isucon-env-checker` files or `blackauth.service` (blackauth is used for the organisers' manual browser check — do not stop it).
- `/initialize` 30 s timeout; app compatibility check; 60 s load. 「初期化処理は用意された環境内で、ベンチマーカーが要求する範囲の整合性を担保します。サーバーサイドで処理の変更・データ構造の変更などを行う場合、この処理が行っている内容を漏れなく提供してください」.
- 3 s propagation grace for finish → billing and disqualified → player/ranking/competitions endpoints.
- 429 + Retry-After allowed on tenants/add and competitions/add (no points, retried).
- 追試: reboot all servers then bench; fail if bench fails or 「再現スコアが最終スコアの85%以下」; for top teams also 「負荷走行実行時にアプリケーションに書き込まれたデータが、サーバー再起動後に取得できない場合」 and 「アプリケーションのブラウザ上での挙動が、初期状態と同様に保たれていない場合」.
- `POST /initialize` must return `{"lang": "..."}` (empty = fail).
- Hostnames `isuports-1..3.t.isucon.dev` pre-registered; TLS cert is `*.t.isucon.dev`.

本選 当日マニュアル https://gist.github.com/shirai-suguru/770d30d16688a07ba78e0a188cd99f9f
- 5 servers (2 vCPU/4 GB) provided; 「競技に利用できる計算機資源は主催者が用意した 5 台のサーバーのみです」.
- `/initialize` up to 60 s; other requests 10 s timeout; 「ベンチマーカーは HTTP リダイレクトを処理しません」; 「ベンチマーカーはデータの更新が即時反映されていることを期待して検証を行います」 (no grace period, unlike 予選).
- App trusts `x-isu-date` header for time; day boundary is JST 0:00 — 「時刻に関するロジックを変更する際、注意ください」.
- `/admin` endpoints: integrity-checked only, no points; 「PUT /admin/masterのエンドポイントが成功していなかった場合 … 追試の時の目視チェックでも確認します」.
- 追試: `sudo systemctl reboot` all servers, wait 10+ min, 3 bench runs on the last-benched server, best score counts; 2 fails out of 4 = 失格; then reboot again and browser check (「正常な動作が確認できなかったチームは失格」「負荷走行実行時にアプリケーションに書き込まれたデータが、サーバー再起動後にも取得できることが確認できなかった場合も失格」).
- `POST /initialize` must return `{"language": "..."}`.
- No separate 本選 regulation page exists on isucon.net (search confirmed only the 予選 regulation + 本選 manuals).

本選 アプリケーションマニュアル https://gist.github.com/shirai-suguru/accb96c5f86200b5c16e1d2a8b533cc1 — one-time token semantics (「処理自体は１回しかしないように制御している」), login once per JST day, login bonus is given directly (no present), `/admin` is admin-only.

Real consequences observed (https://isucon.net/archives/56923294.html , https://isucon.net/archives/56838181.html):
- 本選 browser-check 失格: 釜中の鯖 and 第7西東京市 「ガチャを引き、プレゼントを獲得後にアイテムを開いたのち、「素材アイテム」「時短アイテム」を確認しても、獲得したものが1件も表示されない」; kyncon 「「素材アイテム」「時短アイテム」を用いた「カード」の強化ができない」. p1ass's root cause: 「プレゼントを受け取る処理で早期 return によって INSERT してなくても成功レスポンスを返していること」 — the bench did not catch it (https://blog.p1ass.com/posts/isucon12-final/). NaruseJun explicitly budgeted browser testing because 「ベンチマーカーの整合性チェックがかなりゆるい」.
- 予選 追試 失格: yasuo (security group port 8080 open), 121212 (ranking display_name empty after reboot), SysCa (nginx 502 after reboot), いすもな (billing 500 after reboot), Aquaharaimori (admin billing returned empty array).

## Scoring model (ISUCON12)

予選 (manual https://gist.github.com/mackee/4320c18919c8f6f1867849378a17e651)
- Per successful request: 1 pt — `GET /api/player/player/:id`, `GET /api/player/competition/:id/ranking`, `GET /api/player/competitions`; 10 pt — `POST /api/admin/tenants/add`, `GET /api/admin/tenants/billing`, `GET /api/organizer/players`, `POST /api/organizer/players/add`, `POST /api/organizer/player/:id/disqualified`, `POST /api/organizer/competitions/add`, `POST /api/organizer/competition/:id/finish`, `POST /api/organizer/competition/:id/score`, `GET /api/organizer/billing`, `GET /api/organizer/competitions`.
- Penalty: each error −1 % of score; Critical errors (tenants/add, competitions/add, score, finish, disqualified, and any `GET /api/player/*` served to a disqualified player) −10 % each; run is cut off once 100 % is reached. 429 on tenants/add, competitions/add earns nothing but is not an error.
- Bench: initialize (30 s) → compatibility check → 60 s load; requests outstanding at the end are dropped. Bench issues its own JWTs (blackauth unused during load). Reproduce-mode note: on the day the bench re-requested empty rankings without sleeping (PR #226 added sleep afterwards; `-reproduce` restores day behaviour) — https://github.com/isucon/isucon12-qualify.
- Final score = last run during the contest; 追試 must be ≥85 % and pass.

本選 (manual https://gist.github.com/shirai-suguru/770d30d16688a07ba78e0a188cd99f9f)
- `POST /login` normal user 3 pt; banned-user login handled correctly 1 pt; `POST /user` 3 pt; `GET /user/:id/present/index/:n` 3 pt; `POST /user/:id/present/receive` 2 pt; `POST /user/:id/gacha/draw/:gachaId/:n` 2 pt; others 1 pt; `/admin/*` 0 pt (integrity only).
- Penalty: wrong status/body or timeout (>10 s) −15 pt each; >50 such errors ends the run; initialize/integrity-check failure = fail (0).
- Bench: initialize (60 s) → integrity check → 60 s load. Official 講評 scenarios: existing-user login flow (7 API calls), new-user flow (11 calls), banned-user login every 0.5 s, master update once at ~20 s; organisers expected 250-300k and NaruseJun hit 341,258 (https://isucon.net/archives/56959385.html).
- Special prize to the first team past 50,000 (NaruseJun at 11:54 after disabling logging/metrics for the attempt).
- Final score = best of 3 post-reboot runs (fail = 0), tie-break by average.

## Sources I could not use
- saba-in-the-kettle/isucon12-qualify and ToQoz/isucon12q: `gh api …/commits` returned 404 (probably branch name/visibility); their 予選 details come from blogs only.
- No 参加記 found for 本選 3rd (と / ToQoz) or takonomura 本選; FetchDecodeExecWrite none found.
- 本選 official 講評 fetched twice; it does not spell out the scoring/scenario numbers beyond what is quoted above.