# ISUCON14 (2024-12-08) "ISURIDE" — performance technique catalogue (research agent output, 2026-10-17)

Sources actually opened: official repo `isucon/isucon14` (docs/manual.md, docs/ISURIDE.md, README, webapp/sql/1-schema.sql, webapp/go/*.go, bench/benchmarker/world/*.go, bench/benchmarker/webapp/client_user.go, bench/benchmarker/scenario/scenario.go), isucon.net posts (まとめ 58818382, 解説と講評 58869617, 結果 58837992, 関連エントリ 58835757), public repos read commit-by-commit via `gh api` (takonomura/isucon14 1st; okashoi/isucon14 3rd; shirokanezoo/isucon14 10th Ruby; matsuu/isucon14f 21st; mazrean/isucon14 感想戦 40万点; fujiwara/isucon14-exam advisor's 事前回答 267,496), and ~20 参加記 blogs. No public repo was found for 2nd place 第9西東京市 (whywaita/journey/icchy) — GitHub search and web search returned nothing (unverified / not found).

Reference-implementation facts that drive everything below (webapp/go, 1-schema.sql):
- `chair_locations` is append-only: every `POST /api/chair/coordinate` INSERTs a row; `GET /api/owner/chairs` recomputes total distance with `LAG() OVER (PARTITION BY chair_id ORDER BY created_at)` over the whole table; `GET /api/app/nearby-chairs` does `SELECT * FROM chairs` then per-chair rides + latest location (2N+1).
- `ride_statuses` is an append-only history with `app_sent_at` / `chair_sent_at` columns; "latest status" = `ORDER BY created_at DESC LIMIT 1`; "chair is free" = `COUNT(chair_sent_at)=6` per ride.
- Matching: `isuride-matcher.service` curls `GET /api/internal/matching` every `ISUCON_MATCHING_INTERVAL` (0.5s); handler matches ONE oldest ride to a RANDOM active chair (`ORDER BY RAND()`, up to 10 tries).
- Notifications: JSON polling with `retry_after_ms: 30`; bench client honours `retry_after_ms` (default 30ms) and switches to SSE parsing if `Content-Type` contains `text/event-stream` (bench/benchmarker/webapp/client_user.go).
- Payment: on non-204 from `POST /payments`, reference calls `GET /payments` and compares count with the user's rides; retries up to 5 times with 100ms sleep; no `Idempotency-Key`.

---

### T: Composite indexes on the five hot lookups
- what: Add `chairs(access_token)`, `users(access_token)`, `owners(access_token)`, `rides(chair_id, updated_at DESC)`, `rides(user_id, created_at DESC)`, `ride_statuses(ride_id, created_at DESC)`, `chair_locations(chair_id, created_at DESC)`, `coupons(user_id, used_by, created_at)` / `coupons(used_by)`.
- when it works: schema has no secondary indexes at all (1-schema.sql only has PKs/UNIQUEs on users/owners; `chairs.access_token` has none); auth middleware does `SELECT * FROM chairs WHERE access_token = ?` on every chair request.
- how to verify mid-contest: `SHOW INDEX FROM chairs` (no access_token index), pt-query-digest / performance_schema top queries; the 講評 lists exactly these as "推奨インデックス例".
- risk: Alone it only reaches "4000点程度"; indexes must be in `1-schema.sql`/init.sh so they survive `/api/initialize` and reboot.
- evidence: official 講評 — "Go言語では4000点程度までスコアを伸ばせることを確認" https://isucon.net/archives/58869617.html ; takonomura commits c6ad9ef7 (`idx_chairs_created_at`, `idx_chairs_owner_id_created_at`), 81c95e44 (`rides(chair_id, created_at)`), 7c38e45d (`chairs(access_token)`), fc1b3d15 (coupons) https://github.com/takonomura/isucon14/commit/c6ad9ef7 ; shirokanezoo 15-schema-alter.sql in commit 28a06512 https://github.com/shirokanezoo/isucon14/commit/28a06512 ; 都営三田線 (4th) "CREATE TABLE に必要なインデックスを書き足していく" 599→3282 https://zenn.dev/layerx/articles/3bd55b77e047c4 ; matsuu 935→2896 https://github.com/matsuu/isucon14f (README/commit ebcd5df9).
- regulation note: manual: "競技に伴って処理の変更やデータ構造の変更などを行う場合、初期化処理が行っている内容を漏れなく提供してください" https://github.com/isucon/isucon14/blob/main/docs/manual.md

### T: Kill `chair_locations` — keep latitude/longitude/total_distance on `chairs` (UPSERT instead of append)
- what: Add `latitude, longitude, total_distance, location_updated_at` (or `total_distance_updated_at`) columns to `chairs`; `POST /api/chair/coordinate` becomes `UPDATE chairs SET latitude=?, longitude=?, total_distance = total_distance + |Δlat|+|Δlon|, location_updated_at=?`; `GET /api/owner/chairs` becomes `SELECT * FROM chairs WHERE owner_id=?`; `nearby-chairs` filters `WHERE is_active AND ABS(latitude-?)+ABS(longitude-?) <= ?`. Backfill from initial data in init SQL.
- when it works: a history table whose only readers are "latest row per key" and "running sum per key" (owner_handlers.go LAG window; app_handlers.go `ORDER BY created_at DESC LIMIT 1`); nobody reads the history itself. The 講評 states it explicitly: "新しく椅子の位置情報が記録されるとき次のtotal_distanceは1つ前の位置情報とそれまでのtotal_distanceから計算できる".
- how to verify mid-contest: grep handlers for `chair_locations` — only INSERT in coordinate, LAG in owner/chairs, LIMIT 1 in nearby-chairs; access log shows `POST /api/chair/coordinate` as the top-volume write; slow log shows the LAG query (one team measured "1回の実行に3秒かかっており、これが155回呼び出されていた" https://note.com/youtangai/n/n41fb21a3527b (unverified, cited via search snippet only)).
- risk: (1) `total_distance_updated_at` must be the time of the latest coordinate; motemen's team stopped updating chair_location and used a slightly different timestamp → bench warning "総走行距離がわずかにズレている" https://motemen.hatenablog.com/entry/2024/12/isucon14 . (2) UPSERT evaluation order: iceman's team hit "totalの行を先頭に移動させて解決" because `ON DUPLICATE KEY UPDATE` applied the new lat/lon before the distance delta https://zenn.dev/iceman/articles/1780deef84eda8 . (3) Backfill of initial data must be in init.sh (shirokanezoo added 90-total-distance.sql / 91-location.sql).
- evidence: takonomura 933f378b/4e1b283b (schema), 0f809140 "Eliminate chair_locations" https://github.com/takonomura/isucon14/commit/0f809140 ; shirokanezoo fb1867db "Remove chair location" (Ruby, same UPDATE) https://github.com/shirokanezoo/isucon14/commit/fb1867db ; fujiwara 4ce96a6e "chairに位置情報を持たせてしまう", 3fcbdbf3 "total_distance, moved_atを追加" https://github.com/fujiwara/isucon14-exam/commits ; matsuu 58ad325d "5647 最終的な緯度経度を記録しておくよう実装変更" (5288→5647) https://github.com/matsuu/isucon14f/commit/65258cf6 ; TAK848 "履歴参照を不要にしました" 7,000→9,000 https://zenn.dev/tak848/articles/20241208-isucon14-writeup ; 9th 焼肉ジャンボチキン "その時点までの移動距離も移動履歴のテーブルに含めるようにして、改善" https://krtx.hatenablog.com/entry/2024/12/09/131052 ; iXi (28th) "total distance をキャッシュ" 8,855→16,425 https://zenn.dev/mixi/articles/10dd2dae1f1e00 .
- regulation note: manual 猶予時間: "`GET /api/owner/chairs` で返される椅子の移動距離合計に反映されるまでに3秒の猶予が許容されます" and "`GET /api/app/nearby-chairs` で返される椅子の座標は、過去3秒以内に `POST /api/chair/coordinate` で送信された座標に一致する必要があります" https://github.com/isucon/isucon14/blob/main/docs/manual.md

### T: Buffer `POST /api/chair/coordinate` writes and bulk-INSERT on a ticker (async coordinate ingestion)
- what: Accept the coordinate into an in-process buffer, return `recorded_at` immediately, and flush with a single multi-row `INSERT ... VALUES (...),(...)` every 10–50ms; PICKUP/ARRIVED status transitions are still evaluated per request.
- when it works: a POST that only appends and whose downstream readers have a stated reflection grace (3秒). The 講評: "POST /api/chair/coordinateについては他に利用されるエンドポイントも3秒の反映猶予があるため、非同期化・バルクインサート化が可能".
- how to verify mid-contest: manual 猶予時間 section; the chair simulation only advances after the coordinate response ("椅子は…位置情報を更新するリクエストが成功したことを確認するまでは、移動しません" ISURIDE.md), so latency of this endpoint directly throttles chair movement (= score).
- risk: The PICKUP/ARRIVED transition is driven by this endpoint and is NOT covered by the grace ("猶予されるのは位置情報のみで椅子が割り当て可能かは即時に反映される必要があった" — 講評); buffering must not delay status detection. Buffer must be cleared in `/initialize` (okashoi e736e438 resets `CoordinateBuf`).
- evidence: okashoi (3rd) commits e048ef99 "Add buffer for coordinates and auto flush", 8a1f963e "fix: bulk insert" (single `NamedExecContext` with slice), 3539977a "ticker 50ms" https://github.com/okashoi/isucon14/commit/8a1f963e ; 講評 https://isucon.net/archives/58869617.html .
- regulation note: same 3秒 grace as above.

### T: Raise `retry_after_ms` (30 → 300–1000ms), with a short value only when something is pending
- what: Return `retry_after_ms: 500`/`1000` when there is no ride or no unsent status; return 30ms (or short) when a status was just delivered so the next status arrives quickly.
- when it works: a polling endpoint whose interval the SERVER controls via the response body: "クライアントはレスポンスの `retry_after_ms` で指定された時間(ms)が経過した後に再度リクエストを送信します" (ISURIDE.md). The bench client code sets `waitTime = RetryAfterMs` when present (client_user.go).
- how to verify mid-contest: access log — `GET /api/app/notification` and `GET /api/chair/notification` dominate request count (講評: "30ms間隔という超頻度でポーリング"); the requirement is only "状態が変更されてから3秒以内に通知されていること".
- risk: Longer intervals delay noticing state changes → lower evaluation ("頻度を下げるとユーザーや椅子が状態変化に気づくまでの時間が長くなり、満足度が下がる" — 講評). Evaluation criteria (bench request.go): matched within 100 ticks (3s), pickup within ideal+15 ticks, drive within ideal+5 ticks — a 1s poll on the chair side eats into those margins.
- evidence: takonomura aa3ef720 (30→500), db568f69 (→1000 when no ride), 09471f0d "Increase retry after ms on ongoing rides" (500 default, 30 when a status was just sent) https://github.com/takonomura/isucon14/commit/09471f0d ; okashoi b2e934c6/43c15ac1 "ポーリング間隔を 500 ms に伸ばす" https://github.com/okashoi/isucon14/commits ; 都営三田線 30→100ms (4981→5578), 100→300 (→30936), 300→1000 with batching (→39027) https://zenn.dev/layerx/articles/3bd55b77e047c4 ; ミレニアム 6,000→14,000 with 3000ms then 500ms https://blog.utgw.net/entry/2024/12/09/095202 ; iceman "30ms → 1000ms … スコアは50％くらい上がる" https://zenn.dev/iceman/articles/1780deef84eda8 ; overcached 5044→8658 "1000ms baseline, 200ms when no rides" https://tech.edge2992.dev/posts/20241218_isucon14_overcached/ ; TAK848 30→300ms 4,000→7,000 https://zenn.dev/tak848/articles/20241208-isucon14-writeup ; shirokanezoo 94f27ba2 "retry_after_ms: 500". 講評: JSON API + retry tuning alone was enough — "JSON APIの状態でも少なくとも10万点は取れることを確認" https://isucon.net/archives/58869617.html .
- regulation note: ISURIDE.md "どちらの実装においても、状態が変更されてから3秒以内に通知されていることが期待されます".

### T: Batch all pending status transitions into one notification response (drain the queue)
- what: In one notification request, return every unsent `ride_statuses` row in order (or stream them), rather than one per poll, so long intervals don't stall the at-least-once ordered delivery.
- when it works: the spec requires ordered at-least-once delivery of every transition ("発生したすべての状態遷移を、発生した順序通りに、少なくとも1回以上" ISURIDE.md), and the reference marks exactly one row `app_sent_at` per call — so with 1s polls a ride needs ≥6 polls.
- how to verify mid-contest: read `appGetNotification` — `… AND app_sent_at IS NULL ORDER BY created_at ASC LIMIT 1`.
- risk: Order errors are critical: "ユーザーに想定していないライドの状態遷移の通知がありました" aborts the run (manual). Chair side must not see a new ride before COMPLETED of the previous ("椅子がライドの完了通知を受け取る前に、別の新しいライドの通知を受け取りました").
- evidence: 都営三田線 "1回の通知リクエストで溜まっている通知を stream で全部返すようにする" 30936→39027 https://zenn.dev/layerx/articles/3bd55b77e047c4 ; takonomura a00586c4 "Fix chair notification algorithm" (look at last 2 rides so the previous ride's COMPLETED is sent before the new ride) https://github.com/takonomura/isucon14/commit/a00586c4 ; okashoi 758be8b0 "Retrieve and process multiple ride statuses efficiently".
- regulation note: see 通知の順序 in ISURIDE.md.

### T: SSE notifications with in-process signalling (channel per user/chair) + 1s safety poll
- what: Switch `GET /api/{app,chair}/notification` to `text/event-stream`; send the latest state immediately on connect, then wait on a per-user/per-chair channel that `POST /rides`, matching, `POST /rides/{id}/status`, coordinate (PICKUP/ARRIVED) and evaluation signal; fall back to a 1s timer poll. Set `X-Accel-Buffering: no` (or `proxy_buffering off`) so nginx flushes.
- when it works: manual explicitly allows SSE and defines the format ("SSE…を利用してリアルタイム通知を実装することも可能です"; "コネクションを確立した後、即座に最新のライドの状態を送信しなければなりません"). Bench timeout for notification endpoints is 60s (manual table).
- how to verify mid-contest: bench client checks `Content-Type` contains `text/event-stream` and parses `data:` lines (client_user.go); test with curl -N.
- risk: Many teams lost hours: 都営三田線 "SSE/Pub-Sub…未解決のバグ", hitode909 "SSE 実装を途中で断念", motemen "スコアが上がらず本体に入れられなかった", mazrean spent 12-08 afternoon on "notification cache fix" ×15 then reverted all; 講評: "優勝得点程度まで得点を上げるためには必須ではありませんでした". Multi-app-server SSE needs cross-host pub/sub (Redis) or the ride→signal mapping breaks. Chair SSE connection is reused across rides ("ライド終了後もSSEのコネクションを使いまわす" — trap 2443) so closing it loses the MATCHING event.
- evidence: takonomura 3dcb1c18 "Use SSE for chair notification" (http.Flusher, `writeMessage`), 29e17798 "Bypass nginx buffering for SSE" (`X-Accel-Buffering: no`), 4283a1f7 "Signaling" (sync.Map of chan per chair; signalled from matching/status/evaluation), 43632d29 "Make app notification SSE" https://github.com/takonomura/isucon14/commit/3dcb1c18 https://github.com/takonomura/isucon14/commit/4283a1f7 ; shirokanezoo (Ruby) 713f2929 "SSE on Golang" + eaff73d7 Redis publish + e8d60729 nginx `proxy_buffering off; proxy_read_timeout 10m; chunked_transfer_encoding off` https://github.com/shirokanezoo/isucon14/commit/e8d60729 ; fujiwara 19e7eba2/5486504a/3eafa3d1 sse commits https://github.com/fujiwara/isucon14-exam/commits ; mazrean 感想戦 "アプリ通知SSE＆イベントバス" 26,710→31,454, "椅子通知SSE" →46,286 https://trap.jp/post/2443/ .
- regulation note: ISURIDE.md 通知エンドポイント section; manual timeout table (notification 60s).

### T: Long-polling instead of SSE (hold the JSON request up to the 60s timeout)
- what: Keep the JSON schema but block the handler until a new status exists (or a deadline), then return it.
- when it works: bench timeout for the two notification endpoints is 60s vs 10s for everything else (manual).
- how to verify mid-contest: manual "ベンチマーカーのタイムアウト" table.
- risk: Goroutine/connection count; the 講評 says few teams did it ("実装チームは少なかった模様" per summary).
- evidence: 講評 "JSON APIの状態でもタイムアウトが60秒なのを利用して、ロングポーリングすることも一応想定" https://isucon.net/archives/58869617.html (no team commit found — unverified as practised).
- regulation note: as above.

### T: `is_free`/`is_busy`/`is_matchable` flag on `chairs` instead of counting `chair_sent_at`
- what: Add `is_free` (set FALSE at match, TRUE at COMPLETED/evaluation) and a STORED generated `is_matchable = is_active AND is_free` with an index; matching and nearby-chairs query `WHERE is_matchable = TRUE`.
- when it works: "chair is available" is derived in the reference by a 3-level subquery over `ride_statuses` (`COUNT(chair_sent_at) = 6`), and the state machine is strictly linear (ISURIDE.md state diagram), so a boolean flipped at two points is equivalent.
- how to verify mid-contest: internal_handlers.go `SELECT COUNT(*) = 0 FROM (SELECT COUNT(chair_sent_at) = 6 …)`; nearby-chairs loops rides per chair calling `getLatestRideStatus`.
- risk: Freeing too early makes a chair get a new ride before it has received COMPLETED → critical error "椅子がライドの完了通知を受け取る前に、別の新しいライドの通知を受け取りました". shirokanezoo moved `is_busy=false` to "after returning complete notification" (995ba36e, f0f23486 "is_busy=false should only be happen once completed"). takonomura instead sets `is_free=TRUE` at evaluation (541c9cd1) and relies on the notification algorithm fix (a00586c4). Flag must be backfilled from initial data (commented backfill SQL in 541c9cd1).
- evidence: takonomura 541c9cd1 "Add is_matchable" https://github.com/takonomura/isucon14/commit/541c9cd1 ; shirokanezoo 28a06512 "is_busy" (+`SELECT … FOR UPDATE` then `UPDATE chairs SET is_busy=TRUE`) https://github.com/shirokanezoo/isucon14/commit/28a06512 ; hitode909 "chairsにcan_assignカラムを足して" https://blog.sushi.money/entry/2024/12/09/143622 ; matsuu 65258cf6 `last_status` column maintained by `AFTER INSERT ON ride_statuses` TRIGGER https://github.com/matsuu/isucon14f/commit/65258cf6 ; fujiwara 06fdb38c "chair_sent_atの更新をやめてride中の椅子をメモリに覚える" (sync.Map `chairsInRide`) https://github.com/fujiwara/isucon14-exam/commit/06fdb38c .
- regulation note: none beyond critical-error list in manual.

### T: Use `rides.evaluation IS NOT NULL` as "ride completed"
- what: Replace every `getLatestRideStatus(...) == "COMPLETED"` with `ride.Evaluation != nil` (evaluation is written in the same transaction that inserts COMPLETED).
- when it works: the spec ties the two: "COMPLETED に遷移する際、ユーザーはそのライドに対して評価を行います" (ISURIDE.md); `appPostRideEvaluatation` does `UPDATE rides SET evaluation` + `INSERT ride_statuses COMPLETED` in one tx.
- how to verify mid-contest: grep for `status != "COMPLETED"` in appGetRides, appPostRides (conflict check), nearby-chairs, owner sales.
- risk: none observed; keep it consistent with the notification path.
- evidence: takonomura ccb33604 "Use evaluation column to check ride is completed" and 4856d43b "Remove join for ride_statuses" (owner sales: `WHERE chair_id=? AND evaluation IS NOT NULL AND updated_at BETWEEN …`) https://github.com/takonomura/isucon14/commit/ccb33604 ; fujiwara cdfa16fd "evaluationで完了したrideを判定" https://github.com/fujiwara/isucon14-exam/commits .
- regulation note: none.

### T: Bulk greedy matching — all waiting rides × all free chairs, nearest chair per ride, in one call
- what: Load all `is_matchable` chairs and up to that many unmatched rides (oldest first), compute Manhattan distance in Go, assign the nearest remaining chair to each ride, UPDATE `rides.chair_id` and flip the flag, then signal notifications.
- when it works: matching endpoint is internal and freely changeable ("`GET /api/internal/matching`は…自由に利用方法及び仕様を変更して構いません" ISURIDE.md); reference matches one ride per 500ms with a random chair. Score weights: pickup-to-destination distance counts 1.0, match-position-to-pickup counts 0.1, +5 per completed ride (manual); evaluation Dispatch criterion is `distance(chair, pickup) < 10 * speed` (bench request.go).
- how to verify mid-contest: bench output breakdown "N%のライドはマッチされた椅子が乗車地点までに掛かる時間…" (3rd place saw "86.7%" dissatisfaction, matsuu "61.1%"); internal_handlers.go `ORDER BY RAND()`; `rides WHERE chair_id IS NULL` backlog.
- risk: Random matching wastes the 0.1-weighted distance and fails the 100-tick matching criterion when backlog builds ("ライドが長時間マッチングされませんでした" critical error after 30s — bench user.go). Multiple matchers (several app hosts or app-internal ticker + systemd service) double-assign: 講評 "複数のmatcherが動く場合には…排他制御を行う必要がある"; overcached "複数台構成…matching を無効化し忘れてスコア低下", iceman needed `WHERE chair_id IS NULL` guard. Over-aggressive proximity tuning can also fail to help (3rd place spent 4h without gain).
- evidence: takonomura 88eae785 "Bulk matching" https://github.com/takonomura/isucon14/commit/88eae785 ; 都営三田線 "空いてる chair のうち最も近いものを選ぶようにする" 11068→21372 https://zenn.dev/layerx/articles/3bd55b77e047c4 ; matsuu 65203201 "21023 internalのマッチングを高速化" (loop over all rides, `ORDER BY ABS(?-last_latitude)+ABS(?-last_longitude)`) 6025→21023 https://github.com/matsuu/isucon14f/commit/65203201 ; fujiwara 70c27c78 "1度のmatching呼び出しで全部マッチさせる"; overcached 8658→25518; Maxif 6630→13,073 https://a01sa01to.com/articles/2024/12/isucon14/ ; itohdak 7,385→8,469 https://zenn.dev/itodhak/articles/bc9f98c3853c6a ; 講評 example code (greedy nearest, "距離 < 400") https://isucon.net/archives/58869617.html .
- regulation note: ISURIDE.md ライドのマッチング section.

### T: Run the matcher inside the app (goroutine ticker) and shorten the interval (500ms → 30–100ms)
- what: Replace the systemd curl loop with an in-process `time.Ticker` (or set `ISUCON_MATCHING_INTERVAL=0.1`), and make the endpoint a no-op or disable `isuride-matcher.service`.
- when it works: the manual documents the interval knob; evaluation Matching criterion = matched within 100 ticks (3s); 3rd place found "93.8%のライドは椅子がマッチされるまでの時間" was the dissatisfaction source.
- how to verify mid-contest: `/home/isucon/env.sh`, `systemctl status isuride-matcher`.
- risk: If apps run on several hosts, each host's ticker is a competing matcher (see 排他制御 risk above). Disable the service on hosts that don't run the app and make sure it is enabled on the one that does, or the reboot test finds no matcher. hitode909 went the opposite way late ("マッチング頻度を下げて、良い条件の椅子が余るのを待つ").
- evidence: okashoi d2172463 "perf: Go側でmatchingする" (100ms ticker), 82fffd66 "マッチングを30msにした"; blog: "0.5s → 0.1s" 8,775→25,758 https://blog.pinkumohikan.com/entry/isucon-14 ; takonomura recipe.rb enables `isuride-matcher.service` only on ISU01 https://github.com/takonomura/isucon14/blob/main/recipe.rb ; itohdak 500→100ms 3,833→4,237; matsuu env.sh 0.2→0.1 (commit 65203201); ミレニアム: late "0.5s vs 0.1s" misconfiguration stalled score https://blog.utgw.net/entry/2024/12/09/095202 .
- regulation note: ISURIDE.md: "`isuride-matcher.service`についても同様に、自由に利用方法及び仕様を変更して構いません".

### T: Speed-aware / score-based matching (prefer fast chairs for long rides; cut off far chairs)
- what: Join `chair_models.speed`; score candidate pairs e.g. `25/pickupDistance + destinationDistance/10 + 10/age`, multiplied by speed; skip pairs with pickup distance >100; or sort by distance then `speed DESC` when ride length ≥20.
- when it works: ISURIDE.md: "椅子には移動速度の異なるモデルが複数存在します。ユーザーの待機時間や全体としての流動性を考慮して割り当てを行うことが望ましい"; bench speeds are 2/3/5/7 (world/chair_model.go); Dispatch criterion uses `10*speed`; two separated regions exist ("離れた2つの地域") so cross-region matches are hopeless (hitode909: clusters near 0 and near 300, cutoff 200; iceman: region cutoff "スコアにかなり響いてました").
- how to verify mid-contest: `SELECT name, speed FROM chair_models`; plot pickup coordinates from `rides`.
- risk: Diminishing returns in 8h: 3rd place "Geographic proximity matching algorithm (4+ hours invested, no score gain)"; Maxif "全然上がらず"; toshimaru's Hungarian algorithm "件数が多すぎるとマッチングに時間を要して、使い物にならず" https://blog.toshimaru.net/isucon14/ .
- evidence: fujiwara d8e18674 "マッチングをスコアでやる", da6a3947 "speedを考慮したスコアリング" (`score *= float64(chair.Speed)`) https://github.com/fujiwara/isucon14-exam/commit/da6a3947 ; matsuu edea175f "遠いものは速いものに割り当て" (TRIGGER copies speed onto chairs; `speed DESC` for d≥20) 21023→21150 https://github.com/matsuu/isucon14f/commit/edea175f ; hitode909 "距離/スピードで所要時間を計算して最小化" https://blog.sushi.money/entry/2024/12/09/143622 ; mazrean 感想戦 priority `(経過時間ms/5000)²` 250,130→336,850 https://trap.jp/post/2443/ ; 講評 "距離/速度 <= 10 かどうか", "二部マッチング問題として解くことができる" https://isucon.net/archives/58869617.html .
- regulation note: none.

### T: `nearby-chairs` as a single range query on `chairs`
- what: `SELECT * FROM chairs WHERE is_active = TRUE AND is_free = TRUE AND ABS(latitude-?)+ABS(longitude-?) <= ?` (after the lat/lon denormalisation), no per-chair loops.
- when it works: reference does `SELECT * FROM chairs` + N rides queries + N location queries (2N+1); the 講評 lists it as a designed N+1 ("ウィンドウ関数を使用した共通テーブル式で1回のクエリに統合可能").
- how to verify mid-contest: app_handlers.go appGetNearbyChairs; slow log count.
- risk: Must still exclude chairs on an unfinished ride (fujiwara's first cut was labelled "おそらく仕様違反" and later fixed in 3062194a "nearby改善"); position must be ≤3s old (manual).
- evidence: takonomura 739e7c4c then 0f809140 https://github.com/takonomura/isucon14/commit/739e7c4c ; 都営三田線 "getNearbyChairs の N+1 解消" 39027→43685 https://zenn.dev/layerx/articles/3bd55b77e047c4 ; fujiwara 2e0bf534 https://github.com/fujiwara/isucon14-exam/commit/2e0bf534 ; mazrean 感想戦 "appGetNearbyChairs内chairロケーションキャッシュ" 75,038→104,350 https://trap.jp/post/2443/ .
- regulation note: manual 3秒 grace for coordinates.

### T: In-memory latest ride status (and chair stats) cache, updated at every transition
- what: Map `ride_id → status` guarded by RWMutex/sync.Map, written at MATCHING/ENROUTE/PICKUP/CARRYING/ARRIVED/COMPLETED insert points, read by `getLatestRideStatus`; reset in `/initialize`. Same for `getChairStats` (total rides / avg evaluation per chair).
- when it works: status history is append-only and all writers are in the app; `getLatestRideStatus` is called from ~8 handlers.
- how to verify mid-contest: grep `getLatestRideStatus(`; performance_schema top statement = `SELECT status FROM ride_statuses … LIMIT 1`.
- risk: Only valid with a single app process (or shared store); 2 app hosts caused "通知...expect: MATCHING, got: COMPLETED"-style errors for every (https://tech.every.tv/entry/2024/12/18/183000) and toshimaru. Cache must be warmed/reset on `/initialize`. Invalidate-on-write was slower than update-in-place for mazrean (+21,044 after switching "破棄→更新").
- evidence: okashoi 144412cc "Update ride status cache on ride status changes" https://github.com/okashoi/isucon14/commit/144412cc ; overcached 25518→28156 https://tech.edge2992.dev/posts/20241218_isucon14_overcached/ ; mazrean 感想戦 "getLatestRideStatusesオンメモリキャッシュ" 46,286→75,038, "ride statusオンメモリキャッシュ" →188,429 https://trap.jp/post/2443/ ; fujiwara 2e2e267b "on memory ride status"; 最上川 chair stats map https://zenn.dev/kawaemon/articles/f40529e0c447f5 .
- regulation note: reboot test — see Regulation highlights (data written during the final bench must be readable after reboot; cache must be rebuildable from DB).

### T: Access-token → entity cache in auth middleware
- what: `sync.Map` of access_token → user/chair/owner (or id) populated on first lookup and at registration; reset in `/initialize`.
- when it works: every authenticated request does `SELECT * FROM {users,chairs,owners} WHERE access_token = ?`; tokens never change after creation.
- how to verify mid-contest: middlewares.go; `POST /api/app/users`, `/api/owner/owners`, `/api/chair/chairs` are the only writers.
- risk: minimal ("スコアには微量の効果があった" iceman) once indexes exist; multi-host needs per-host warm-up.
- evidence: okashoi 16ec1faa https://github.com/okashoi/isucon14/commit/16ec1faa ; 都営三田線 "In-memory access token caching" 30936→32003 https://zenn.dev/layerx/articles/3bd55b77e047c4 ; fujiwara 7c0f7162 "session cache"; mazrean 407ba076 "isurus: accesstoken cache".
- regulation note: none.

### T: DB on its own host; app+nginx+matcher+payment_mock on one host; third host idle or second app
- what: Move MySQL to server 2 (`bind-address`, `ISUCON_DB_HOST=192.168.0.12`), keep nginx+Go+matcher on server 1; disable services on hosts that don't need them via a recipe so the layout survives reboot.
- when it works: MySQL CPU is the first wall ("DB CPU制約" — mazrean; every: "MySQL CPU 175%"); c5.large ×3.
- how to verify mid-contest: `top` on the all-in-one host; mysqltuner "Highest connection usage 96% (145/151)" (matsuu result/mysqltuner.txt).
- risk: A second APP host breaks in-memory caches/SSE signalling and duplicates the matcher — 講評: "isuride-matcherの存在によって複数台構成に移行した際にベンチマークがfail". 最上川 reverted DB separation "due to data consistency issues" and was later disqualified for data not persisting after reboot (cause unknown to them) https://zenn.dev/kawaemon/articles/f40529e0c447f5 . Maxif abandoned 3-host split "DB connection requirements".
- evidence: takonomura recipe.rb (ISU01: isuride-go, matcher, payment_mock, nginx; ISU02: mysql only; ISU03: nothing) + env.sh https://github.com/takonomura/isucon14/blob/main/recipe.rb ; 都営三田線 "DB moved to separate instance" 5578→10221; RAKUS (7th) "1台をnginx+アプリ用、もう1台をDB用" 18,000→26,000 https://tech-blog.rakus.co.jp/entry/20241210/isucon14 ; matsuu 29b20353 "25409 DBサーバ分割" (20216→25409); TAK848 9,000→23,000; shirokanezoo d162796b "app=isu1,isu2 db=isu3" (Ruby, 2 app hosts + Redis pub/sub) https://github.com/shirokanezoo/isucon14/commits .
- regulation note: manual: "競技終了時に全てのサーバーが起動した状態でないと、追試が実施できず失格"; only the 3 CloudFormation EC2s may be used.

### T: MySQL: disable binlog, `innodb_flush_log_at_trx_commit=2`, `O_DIRECT`, buffer pool 1G, `max_connections` up
- what: `disable_log_bin`, `innodb_buffer_pool_size=1GB`, `innodb_flush_log_at_trx_commit=2`, `innodb_flush_method=O_DIRECT`, `max_connections=65535` (reference default 151 was saturated).
- when it works: write-heavy coordinate/status inserts; 4GiB RAM host dedicated to MySQL.
- how to verify mid-contest: mysqltuner / `SHOW GLOBAL STATUS LIKE 'Max_used_connections'`.
- risk: `flush_log_at_trx_commit=2` loses up to 1s of commits on OS crash, acceptable for the reboot test only if MySQL shuts down cleanly. matsuu put redo logs on `/dev/shm` (d22a6bdb "redoを/dev/shmへ", 25409→25333, no gain) — tmpfs is wiped on reboot; they reported passing the restart test, but it is a data-loss hazard (unverified beyond their own report).
- evidence: takonomura 4feb5a38, 94072bd9, 26bf8472 https://github.com/takonomura/isucon14/commit/94072bd9 ; toshimaru "innodb_buffer_pool_size…disable_log_bin" https://blog.toshimaru.net/isucon14/ ; matsuu 23d058f5 "mysqldチューニング".
- regulation note: 追試 reboots all three hosts twice and checks "負荷走行実行時にアプリケーションに書き込まれたデータが、再起動後に取得できるかどうか" (manual).

### T: Go MySQL driver: `InterpolateParams=true`, large pool, `sqlx.Open` + ping loop
- what: `dbConfig.InterpolateParams = true` (client-side interpolation, no prepare round-trip); `SetMaxOpenConns/SetMaxIdleConns` 100–512, `ConnMaxLifetime` 10s; use `sqlx.Open` (lazy) with a retry ping so the app starts even if MySQL on the other host is still booting.
- when it works: tx-heavy handlers each issue 3–8 queries; reboot ordering between hosts is not guaranteed.
- how to verify mid-contest: main.go DSN; `isuride-go.service` fails on boot if `sqlx.Connect` panics before MySQL is up.
- risk: `sqlx.Connect` panic at boot = failed reboot test; takonomura also added `StartLimitInterval=0` so systemd restarts indefinitely.
- evidence: takonomura 19508fc5, afaf2dd5 (mysql.go `waitDB`/`pollDB`, 512/512/10s), 83afe3d2 "Use open", c71929ce "No limit" https://github.com/takonomura/isucon14/commit/afaf2dd5 ; okashoi 7ac7a6cb "動的プレースホルダーを有効化", 29cb0256 pool 100/100; 都営三田線 "interpolateParams=true" 21372→23894; matsuu 371702fd "935 interpolateParams追加".
- regulation note: reboot test.

### T: Payment gateway: `Idempotency-Key` + skip the `GET /payments` verification, bounded concurrency
- what: Send `Idempotency-Key: <ride_id>` on `POST /payments`; on non-2xx just retry the POST (no `GET /payments` + ride-count comparison); optionally cap concurrent payment calls (semaphore of 2) and cache `payment_gateway_url` from `/initialize`.
- when it works: ISURIDE.md defines the header: "同じ`Idempotency-Key`ヘッダを持つリクエストが複数回送信されたとしても冪等に処理されます"; bench payment server returns errors even when the payment succeeded, and the verify call is slow (講評).
- how to verify mid-contest: payment_gateway.go retry loop; `/home/isucon/webapp/payment_mock/openapi.yaml`.
- risk: Wrong amount or double charge is a critical error ("決済サーバーに誤った支払いがリクエストされました"; bench payment.go `既に支払い済みです` / `支払い額が不正です`). Payments in flight at bench end must finish within 5s. The 講評 author admits it was mostly a bluff: "かなりのブラフになってしまったのですがこれに囚われていた人はあまりいなかったようで安心".
- evidence: 講評 "Idempotency-KeyヘッダにRideIDなど指定することでエラーになったとしても確認APIを叩かずにそのまま再度リクエスト" https://isucon.net/archives/58869617.html ; fujiwara ad8b39ec "paymentへのリクエストを2並列にする" (+url cache) https://github.com/fujiwara/isucon14-exam/commit/ad8b39ec ; iXi "Idempotency-Key implementation, exponential backoff" 3,850→7,157 (bundled with other changes) https://zenn.dev/mixi/articles/10dd2dae1f1e00 ; 3rd place listed it as not reached https://blog.pinkumohikan.com/entry/isucon-14 .
- regulation note: manual: "負荷テスト終了時点で行われていた決済処理は負荷テスト終了後5秒以内に完了する必要があります".

### T: Owner sales/chairs: pre-filter by owner, drop the `ride_statuses` JOIN, cache per manual grace
- what: `GET /api/owner/chairs` → `SELECT * FROM chairs WHERE owner_id=?` with denormalised totals; `GET /api/owner/sales` → `rides WHERE chair_id IN (...) AND evaluation IS NOT NULL AND updated_at BETWEEN …` (one query, group in Go); optionally a 3s response cache for owner/chairs.
- when it works: reference computes totals for ALL chairs then filters by owner (講評: "全椅子の総走行距離を計算した後にOwnerIDで絞り込む"); owners poll these to decide to buy chairs (bench owner.go: `desiredChairNum(res.Total)` → more chairs = more score capacity).
- how to verify mid-contest: owner_handlers.go; bench world: chairs only increase when owner sales checks succeed, so this endpoint gates late-game growth (mazrean: "owner API高速化（N+1削除）" 336,850→389,765).
- risk: `ValidateSales` tolerates values between snapshot and current ("salesが小さい/大きいデータがあります"), so stale caches beyond the grace fail.
- evidence: takonomura 4856d43b https://github.com/takonomura/isucon14/commit/4856d43b ; 都営三田線 "Optimized ownerGetChairs" 3282→3742; ミレニアム "3-second cache as specified in manual" https://blog.utgw.net/entry/2024/12/09/095202 ; mazrean 感想戦 https://trap.jp/post/2443/ ; 講評 https://isucon.net/archives/58869617.html .
- regulation note: 3秒 grace for total distance (manual).

### T: Turn off logging/monitoring/agents before the final run
- what: `access_log off`, remove chi `middleware.Logger`, stop netdata/datadog/ssm-agent/snapd, disable slow query log and tracing (`ENABLE_TRACING` env), remove pprof/fgprof.
- when it works: every contest; here app CPU became the bottleneck after DB fixes (mazrean: "中期: アプリケーション CPU制約").
- how to verify mid-contest: `top`; nginx access log write rate at 30ms polling is enormous.
- risk: Teams that left measurement EC2s in the VPC were disqualified by envcheck — see Regulation highlights (not logging per se, but same "clean up before end" discipline).
- evidence: takonomura 70eef3b3 "Disable chi logger", 21630c27 "Disable monitoring" (netdata, ssm-agent, snapd stopped; `access_log off`) https://github.com/takonomura/isucon14/commit/21630c27 ; okashoi 7a0156ff "perf: stop app log"; shirokanezoo ea6a3c74 "Bye datadog"; iXi "disabled Trace, Logger, Datadog, PGO" 18,000→22,358; itohdak "Log disabling" →10,229; 3rd place "disabled slow query log" 25,758→31,362 https://blog.pinkumohikan.com/entry/isucon-14 .
- regulation note: none.

### T: nginx for SSE and high connection counts
- what: `worker_rlimit_nofile 100000; worker_connections 4096`, `http2` on 443, static frontend via `try_files $uri /index.html` from `/home/isucon/webapp/public/`, `/api/` proxied to the app; for SSE either `X-Accel-Buffering: no` from the app or `proxy_buffering off; proxy_read_timeout 10m` on the notification locations.
- when it works: thousands of long-lived notification connections.
- how to verify mid-contest: `nginx -T`; SSE events not arriving until buffer fills = buffering on.
- risk: nginx itself became the final CPU limit at 40万点 ("Nginx CPU 100%張付" — mazrean) when co-located with the app.
- evidence: takonomura ip-192-168-0-11/nginx.conf https://github.com/takonomura/isucon14/blob/main/ip-192-168-0-11/nginx.conf and 29e17798; shirokanezoo e8d60729 https://github.com/shirokanezoo/isucon14/commit/e8d60729 ; mazrean 感想戦 "worker_connections 2048→4096, worker_rlimit_nofile 8192→16384" https://trap.jp/post/2443/ ; iXi kernel TCP TIME_WAIT tuning 16,425→18,000 https://zenn.dev/mixi/articles/10dd2dae1f1e00 .
- regulation note: TLS cert `*.xiv.isucon.net` must keep working (manual TLS証明書); frontend must still render in 追試.

### T: Regenerate / patch the initial data instead of recomputing at `/initialize`
- what: Produce a modified `3-initial-data.sql` (or extra 9x-*.sql) with the new denormalised columns pre-filled, so `/initialize` stays under 30s and doesn't run window functions over `chair_locations`.
- when it works: `/initialize` has a 30s timeout (manual) and the backfill UPDATE with LAG is heavy.
- how to verify mid-contest: time `~/webapp/sql/init.sh`.
- risk: Initial data must stay consistent with what the bench expects (prevalidation compares `data/owner/*`, `data/user/*` fixtures).
- evidence: takonomura 517c4669 / 60e6d941 "Regenerate initial data" https://github.com/takonomura/isucon14/commits ; shirokanezoo fb1867db adds 90-total-distance.sql, 91-location.sql to init.sh https://github.com/shirokanezoo/isucon14/commit/fb1867db ; matsuu 4-change.sql backfill https://github.com/matsuu/isucon14f/commit/65258cf6 .
- regulation note: manual 負荷走行: `POST /api/initialize`（30秒でタイムアウト）.

### T: Separate matching-notification trigger from DB polling (signal on match, status, evaluation)
- what: After `UPDATE rides SET chair_id`, after each `INSERT ride_statuses`, and after evaluation, push a signal to the chair's/user's channel so the SSE goroutine wakes immediately rather than on its timer.
- when it works: with SSE; evaluation criteria reward fast reaction (Pickup within ideal+15 ticks, Drive within ideal+5 ticks — request.go).
- how to verify mid-contest: compare bench breakdown percentages before/after.
- risk: Signal must be non-blocking (`select { case ch <- struct{}{}: default: }`) or handlers stall.
- evidence: takonomura 4283a1f7 "Signaling char notification for faster" (`defer signalCharNotification(best.ID)` in matching; signals in chairPostRideStatus and evaluation) https://github.com/takonomura/isucon14/commit/4283a1f7 ; shirokanezoo e71b18d3 "internal_handler: publish ride" via Redis.
- regulation note: none.

### T: Chair model speed lookup cached / copied onto `chairs`
- what: Copy `chair_models.speed` into `chairs.speed` via BEFORE INSERT TRIGGER (or load the 55-row `chair_models` table into memory at start) for matching.
- when it works: `chair_models` is master data that never grows after `/initialize` (2-master-data.sql); `chairs.model` is TEXT in the reference (fujiwara changed it to VARCHAR(50) to join).
- how to verify mid-contest: `SELECT COUNT(*) FROM chair_models` before/after bench.
- risk: TRIGGERs run in MySQL and add write cost; in-memory is cheaper.
- evidence: matsuu edea175f `CREATE TRIGGER update_speed BEFORE INSERT ON chairs` https://github.com/matsuu/isucon14f/commit/edea175f ; fujiwara da6a3947 (JOIN chair_models, `model VARCHAR(50)`) https://github.com/fujiwara/isucon14-exam/commit/da6a3947 .
- regulation note: none.

### T: Coupon lookups: index and shard `coupons`
- what: Index `coupons(user_id, used_by, created_at)` and `(used_by)`; the `CP_NEW2024`-first then oldest-unused rule is a 2-query path per ride creation/notification (`calculateDiscountedFare` is called from notification too).
- when it works: `calculateDiscountedFare` runs on every notification response in the reference.
- how to verify mid-contest: app_handlers.go lines ~965-1000; `coupons` PK is `(user_id, code)` only.
- risk: none noted.
- evidence: takonomura fc1b3d15 "Add indexes to coupons"; fujiwara 857ebec8 "index coupons", 0d42ba0f "couponを分割", 052e9aaf "shard coupons again" https://github.com/fujiwara/isucon14-exam/commits ; cache fare per ride in SSE process (takonomura 43632d29 `FareRideID/Fare`).
- regulation note: none.

### T: Faster JSON / PGO / Go version bump (late-stage app-CPU squeeze)
- what: `go-json`/`sonic` for encoding, `strings.Builder` for the coordinate response, Go PGO with a bench profile, Go 1.24.
- when it works: once DB is off the critical path and app CPU is pinned.
- how to verify mid-contest: pprof shows encoding/json and net/http dominating.
- risk: Only worth it after the structural fixes; gains are single-digit-% each.
- evidence: mazrean 感想戦 "Go 1.24へアップグレード" 389,765→417,923, "sonic", PGO https://trap.jp/post/2443/ ; iXi "go-json replacement", "PGO addition" https://zenn.dev/mixi/articles/10dd2dae1f1e00 .
- regulation note: none.

### T: Embedded KV (badger) / Redis for chair location and cross-process pub/sub
- what: Move the hottest small state (chair location) into an embedded store on the app host, or use Redis pub/sub when the app language can't hold SSE connections itself (Ruby → Go sidecar).
- when it works: multi-process app servers (Ruby/Puma) that can't share in-memory maps; or when MySQL round-trips dominate.
- how to verify mid-contest: language/runtime model; count of processes.
- risk: Extra moving part for the reboot test (Redis must be enabled); shirokanezoo's SSE sidecar needed systemd dependencies (d33cd4fe).
- evidence: shirokanezoo 713f2929 "SSE on Golang" + eaff73d7 Redis publish + 837c4e53 "Add redis gem" (final 34,413 per commit f4ba6225 "score/34413") https://github.com/shirokanezoo/isucon14/commits ; mazrean 感想戦 "chair location badgerへ移動" 125,394→133,876 https://trap.jp/post/2443/ .
- regulation note: Redis/badger on the competition hosts is allowed; external services are not.

---

### Regulation highlights (this contest)
- Hardware: 3× c5.large (2vCPU/4GiB), gp3 20GB, built from the team's own CloudFormation template; changing instance type or having multiple stacks on the portal at end = 失格. Bench: ECS Fargate 8vCPU. https://github.com/isucon/isucon14/blob/main/docs/manual.md , https://github.com/isucon/isucon14/blob/main/README.md
- Must not change: `envcheck.service` files, `/opt/isucon-env-checker`, `isuadmin` user; security group TCP/22 and TCP/443 must stay. Run `sudo /opt/isucon-env-checker/envcheck` yourself. Extra EC2 in the same VPC (pprotein box) or a 40GB EBS caused envcheck failures and disqualification: TAK848 "pproteinのインスタンスを削除してから試したところ成功" https://zenn.dev/tak848/articles/20241208-isucon14-writeup ; itohdak "4 EC2 instances detected… EBS 40 GB" https://zenn.dev/itodhak/articles/bc9f98c3853c6a ; every (API server left disabled) https://tech.every.tv/entry/2024/12/18/183000 .
- 追試: reboot all 3 hosts; envcheck; bench against the final-score host — FAIL or ≤75% of final score up to 3 attempts = 失格; reboot again; "負荷走行実行時にアプリケーションに書き込まれたデータが、再起動後に取得できるかどうかを確認します" — 最上川 (8th-equivalent, 37,127) was disqualified for "データ保持違反" https://zenn.dev/kawaemon/articles/f40529e0c447f5 ; frontend must render and match reference structure. All 3 servers must be running at contest end.
- External resources: only the 3 EC2s may serve traffic; "他のサーバーに処理を委譲するなど" forbidden; monitoring/dev on outside machines OK; AI code tools OK; external container registry for deploy OK.
- `GET /api/internal/matching` and `isuride-matcher.service` may be freely changed; `ISUCON_MATCHING_INTERVAL` in `/home/isucon/env.sh`. https://github.com/isucon/isucon14/blob/main/docs/ISURIDE.md
- Notifications: JSON (`retry_after_ms`) or SSE (`text/event-stream`, `data: <json>\n`), send latest state immediately on connect, every transition at least once in order, "状態が変更されてから3秒以内に通知されていることが期待されます". Bench timeouts: `/api/initialize` 30s, notification endpoints 60s, everything else 10s.
- Grace: coordinates in `nearby-chairs` within 3s; owner total distance within 3s; matched ride must be notified to user and chair within 30s.
- Payment: `Idempotency-Key` supported; in-flight payments must finish ≤5s after bench end; wrong/duplicate payment is a critical error.
- Status/consistency: 200+ warnings or any critical error (unexpected notification, wrong transition order, new ride before COMPLETED on chair, matching >30s, evaluation timeout, evaluated-but-unpaid) = FAIL. Bench client expects HTTP 200 for notification JSON (client_user.go) — non-200 is an error.
- Language prize requires `language` field in `/api/initialize` response.

### Scoring model (this contest)
- Manual: score = `椅子がライドとマッチした位置から乗車位置までの移動距離の合計 * 0.1` + `椅子の乗車位置から目的地までの移動距離の合計` + `ライド完了数 * 5`. https://github.com/isucon/isucon14/blob/main/docs/manual.md
- Bench implementation (bench/benchmarker/world/request.go, scenario/scenario.go): per completed ride `Score = Sales + dist(matchPos, pickup)*100/10` where `Sales = 500 + dist(pickup, dest)*100` (undiscounted — coupons do NOT reduce score); final = Σ owner.SubScore / 100 (+ partial score of in-flight rides when not final). Only owners' chairs' completed rides count, so chair count × throughput is the ceiling. https://github.com/isucon/isucon14/blob/main/bench/benchmarker/world/request.go
- Growth loop (world.go, region.go, owner.go, user.go): 1 tick = 30ms. Each ride gets evaluation 1 + [matched <100 ticks] + [dist(chair,pickup) < 10×speed] + [pickup delay ≤ ideal+15 ticks] + [drive delay ≤ ideal+5 ticks]. Every 60 ticks each region adds `round(5 × avgEvaluation/5)` users; a ride rated ≥4 invites a new user (max 3 per user); users with first-ride rating 1 or average ≤2 leave (min 10 active per region). Owners add chairs when `desiredChairNum(total sales)` grows (geometric: first chair at 15,000 sales, ratio 1.02, linear after 50) and only when their `GET /api/owner/chairs` / `sales` calls validate. Chair speeds: 2, 3, 5, 7 (chair_model.go). Matching >30s is a critical error (user.go).
- 講評 framing: "椅子が人を乗せている状態は空車時の10倍のスコア", "全エンドポイントが30ms以内にレスポンスを返せていることが理想的", top teams reached 58,153 (takonomura, Go, solo), 55,003 (第9西東京市, +23,968 in the hidden last hour), 52,150 (// TODO…, first past 32,767 at 12:20). https://isucon.net/archives/58869617.html , https://isucon.net/archives/58837992.html
- Winner's timeline (takonomura, from commit times UTC+9 and framinal's walkthrough https://blog.framinal.life/entry/2024/12/08/235842 ): 10:08 tooling; 10:53 indexes; 11:57 retry_after 500ms; 12:10 nearby N+1; 12:28–13:29 chair_locations → chairs + is_matchable; 14:22–14:30 binlog off/InterpolateParams/mysqld/bulk matching; 15:12 retry_after 1000; 16:17 chair SSE; 17:22 app SSE; 17:36 monitoring off; 17:44 `sqlx.Open`.
