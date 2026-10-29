# ISUCON9 (2019) performance-technique catalogue — 予選 ISUCARI / 本選 ISUTRAIN (ISUXPRESS予約)

Sources actually opened (all URLs below were fetched; nothing is from memory):
- Official: `github.com/isucon/isucon9-qualify` (README, docs/manual.md, docs/faq.md, docs/external.md, webapp/docs/APPLICATION_SPEC.md, webapp/docs/EXTERNAL_SERVICE_SPEC.md, webapp/sql/01_schema.sql, bench/scenario/{campaign,load,verify,action,check,scenario}.go, bench/server/{server,shipment,payment}.go, bench/asset/asset.go, bench/session/webapp.go, cmd/bench/main.go); `github.com/isucon/isucon9-final` (README, docs/MANUAL.md, docs/IMPLEMENT.md, docs/webapp_spec.md, docs/payment_spec.md, webapp/sql/01_schema.sql); isucon.net 予選レギュレーション (53555007), 予選 解説と講評 (53789931), 本選 解説と講評 (53916974), 予選 1日目結果 (53786743), 本選出場者 (53789846), 本選フォトレポート (53877800), 本選 全チームスコア (53882494), 予選/本選 関連エントリまとめ (53789734 / 53877593).
- Repos read commit-by-commit with `gh api .../commits` + `.../commits/{sha}` patches: takonomura/isucon9-qualify (nil, 予選 2日目 1位 52,440), tohutohu/isucon9 (にがり, 予選 1日目 1位 36,270; commits all titled "bench", so final main.go was read instead), shirokanezoo/isucon9q (白金動物園 予選 15,100), shirokanezoo/isucon9f (白金動物園 本選 優勝 35,801), methane/isu9q (ようするにメガネ, 予選 25,200), shmurakami/isucon9_final (しゃかラン, 本選 4位 13,016), shugo/isucon9_final (本選, commit list only).
- 参加記: to-hutohu.com (にがり), takono.io (nil), yosuke-furukawa (isucon_friends 27,470), matsuu + netmarkjp (いんふらえんじにあー as Code 26,460 / 本選 11位), methane (予選 5位 / 本選 fail), rosylilly gist + rosylilly 本選 + mirakui 予選 + mirakui 本選 (白金動物園), labs.gree.jp (ARINE, 本選 特別賞, browser-check NG 29,963), shmurakami (本選 4位), soudai (本選 12位), nakario + chy72 ×2 (百万円ドリブン, incl. 本選 感想戦 105,417), kamipo, sambaiz.
- Could not open: catatsuy's Medium post (HTTP 403 both via WebFetch and curl), tkuchiki's blog (JS-only), sugyan's 本選 post (URL not found), sorah's 本選 post (none exists for 2019 per search). nil's 本選 (2位 29,704) code/blog was not found (takonomura has no isucon9-final repo) — marked unverified where referenced.

Notation: [Q] = 予選 ISUCARI, [F] = 本選 ISUTRAIN, [Q+F] = seen in both rounds.

---

## Part 1 — 予選 ISUCARI techniques

### T: [Q+F] Static master table (categories) → load once into process memory / hard-code
- what: `categories` (43 rows, parent/child tree) is never written after `/initialize`. Load it into a map at start-up (or hard-code it as a constant) and resolve `getCategoryByID` / parent-category-name / child-ID lists from memory; also return `GET /settings` categories from memory.
- when it works: a table with no INSERT/UPDATE path anywhere in the app (grep the write paths: ISUCARI has none for `categories` or `configs` except `/initialize`). Official: 「カテゴリについては更新がありませんでしたので、ソースコードに直接書いたチームが多かったと思われます。」
- how to verify mid-contest: `grep -n "INSERT INTO \`categories\`\|UPDATE \`categories\`" webapp/*/*`; check `init.sh` only loads `02_categories.sql`; count rows (`SELECT COUNT(*) FROM categories` = 43).
- risk: in a multi-app-server setup the cache must be filled on every process; にがり had to add a second `/initialize`-like endpoint on app #2 and call it from app #1 (「categoriesとconfigのキャッシュは2台目に/initializeのキャッシュのみを行うエンドポイントを生やし、1台目から叩くようにしました」). Loading at process start (nil) avoids this.
- evidence:
  - nil commit "Avoid calling categories table" (categories.go with `loadCategories()`, `categoryByID`, `childCategories`): https://github.com/takonomura/isucon9-qualify/commit/8f6d41ee
  - にがり blog 「11:40 categoryとconfigのオンメモリキャッシュ → 3500」(from 2700): https://to-hutohu.com/2019/09/09/isucon9-qual/
  - 白金動物園 "Hard-code categories" + "eliminate categories query": https://github.com/shirokanezoo/isucon9q/commit/fc313c67 , https://github.com/shirokanezoo/isucon9q/commit/6025c4c8
  - methane team "categories テーブルをオンメモリ化": https://github.com/methane/isu9q/commit/c2875d10
  - official 講評: https://isucon.net/archives/53789931.html
- regulation note: allowed under 「キャッシュ機構の追加」(https://isucon.net/archives/53555007.html). `/initialize` must still reset state in ≤20 s (manual).

### T: [Q] `configs` (payment/shipment service URL) in memory
- what: the two external-service URLs written by `POST /initialize` are read on every external call via `SELECT val FROM configs`. Keep them in a process variable (set in `/initialize`) instead.
- when it works: `configs` is only written in `/initialize`; values are the same for the whole run.
- how to verify: schema `configs(name PRIMARY KEY, val)`; grep writes → only `postInitialize`.
- risk: same multi-process fan-out as categories (にがり's extra endpoint); 白金動物園 moved config to Redis instead ("config on redis") to share across hosts.
- evidence:
  - にがり blog (same 3500 step) + main.go `config["payment_service_url"] = ri.PaymentServiceURL` in postInitialize: https://github.com/tohutohu/isucon9/blob/master/webapp/go/main.go
  - matsuu blog 「configs をアプリのインメモリに保存」: https://matsuu.hatenablog.com/entry/2019/09/08/195735
  - methane team "config cache": https://github.com/methane/isu9q/commit/adbea20b
  - 白金動物園 "config on redis": https://github.com/shirokanezoo/isucon9q/commit/13886cef
- regulation note: same as above.

### T: [Q+F] N+1 on users/categories in list endpoints → `IN (...)` batch or JOIN
- what: `GET /new_items.json`, `GET /new_items/:root_category_id.json`, `GET /users/:id.json`, `GET /users/transactions.json` call `getUserSimpleByID` and `getCategoryByID` per item (48 items/page). Collect the distinct seller IDs, run one `SELECT ... FROM users WHERE id IN (?)` (sqlx.In), build a map, and fill from it; categories from memory.
- when it works: any per-row lookup of a small dimension (users, categories) in a paginated list. Official: 「出品者・購入者についてはJOINで取得するか、必要なユーザ情報をIN句を使い一気に取得してアプリケーションの中でデータを保管するなどの方法でアプローチしたチームが多かったようです。」
- how to verify: pt-query-digest with `long_query_time=0` shows `SELECT * FROM users WHERE id = ?` at the top by count; alp shows `/new_items*.json` dominant.
- risk: JOIN variant drags `items` row locks into the read (rosylilly: 「あとから思えば IN で別引きしたほうが items のロックに巻き込まれなかったなと思う」). にがり's first "4N+1→2N+1" JOIN rewrite actually dropped the score (3500→3400) before the users cache fixed it.
- evidence:
  - nil "Fix N+1 in GET /new_items/:category_id.json" (sqlx.In on users and categories): https://github.com/takonomura/isucon9-qualify/commit/b70d040a ; "Fix N+1 in GET /new_items.json": https://github.com/takonomura/isucon9-qualify/commit/f4251999 ; "Fix N+1 in get transactions": https://github.com/takonomura/isucon9-qualify/commit/d8375e78
  - 白金動物園 "Use inner join on new items" / "on transaction": https://github.com/shirokanezoo/isucon9q/commit/f70d520b , https://github.com/shirokanezoo/isucon9q/commit/70461d86 ; "Fix buyer N+1": https://github.com/shirokanezoo/isucon9q/commit/8463ba3d
  - methane team "userSimpleのN+1解消": https://github.com/methane/isu9q/commit/a5434b56
  - nil blog: https://www.takono.io/posts/2019/09/isucon/ ; rosylilly gist: https://gist.github.com/rosylilly/79bd3b146c36aa16eb10e7949571edea
- regulation note: response shape must stay identical (bench verifies `category`, `parent_category_name`, seller `num_sell_items` per item — verify.go `checkItemSimpleCategory`).

### T: [Q] `items` indexes for created_at pagination: `(created_at)`, `(category_id, created_at)`, `(seller_id, created_at)`, `(buyer_id, created_at)`
- what: every list endpoint does `WHERE ... AND (created_at < ? OR (created_at <= ? AND id < ?)) ORDER BY created_at DESC, id DESC LIMIT 49`; the initial schema only has `idx_category_id`. Add the composite indexes with `created_at` last.
- when it works: keyset pagination on `created_at DESC, id DESC` with a leading equality column.
- how to verify: `webapp/sql/01_schema.sql` (`items` has only `INDEX idx_category_id`); `EXPLAIN` shows filesort; slow log top-by-total is the `new_items`/`users/:id` queries.
- risk: none reported; add them to `01_schema.sql`/init so they survive `/initialize` and reboot (shmurakami in 本選 nearly lost indexes not written into the schema file).
- evidence:
  - nil: https://github.com/takonomura/isucon9-qualify/commit/c6947ac3 (created_at), https://github.com/takonomura/isucon9-qualify/commit/ab05d9c9 (seller_id,created_at), https://github.com/takonomura/isucon9-qualify/commit/26c2ba34 (category_id,created_at), buyer_id index in https://github.com/takonomura/isucon9-qualify/commit/6d68a384
  - にがり 「11:10 indexを貼る → 2700」; isucon_friends 「Index貼ったり、OR検索をUNIONに変える 2100→2800」: https://yosuke-furukawa.hatenablog.com/entry/2019/09/08/232231
  - matsuu: itemsに (status), (created_at), (seller_id, status), (buyer_id): https://matsuu.hatenablog.com/entry/2019/09/08/195735
  - official: 「初期状態ではデータベースのインデックスが不足していたり…ここを改善していくと、着実にスコアがあがったかと思います。」
- regulation note: 「DBスキーマの変更やインデックスの作成・削除」explicitly allowed.

### T: [Q+F] `(seller_id = ? OR buyer_id = ?)` → UNION of two index-friendly selects
- what: `GET /users/transactions.json` filters `WHERE (seller_id = ? OR buyer_id = ?) AND status IN (...)`. Rewrite as `SELECT * FROM (SELECT ... WHERE seller_id = ? ... UNION SELECT ... WHERE buyer_id = ? ...) t ORDER BY created_at DESC, id DESC LIMIT ?` so each branch uses its own index. The same trick was used in 本選 for the three-way segment-overlap OR.
- when it works: OR across two different indexed columns where the optimizer will not do index_merge well.
- how to verify: `EXPLAIN` shows full scan / `Using where` on `items` for that query.
- risk: UNION creates a temporary table; 白金動物園 in 本選 later split it into three separate queries ("no more union for get_reserved_seats") when the union became noticeable.
- evidence:
  - official: 「SELECT * FROM items WHERE seller_id = ? UNION SELECT * FROM items WHERE buyer_id = ?」 https://isucon.net/archives/53789931.html
  - nil "Use UNION instead of OR": https://github.com/takonomura/isucon9-qualify/commit/6d68a384
  - 白金動物園 本選 "Use UNION instead of OR" (segment overlap): https://github.com/shirokanezoo/isucon9f/commit/1064bebe ; later https://github.com/shirokanezoo/isucon9f/commit/14351c3e
- regulation note: none.

### T: [Q] Stop calling the shipment `/status` API from `GET /users/transactions.json`; answer from `shippings.status`
- what: the reference calls `POST /status` on the shipment service once per transaction row (0.8 s each, serialised). The app already stores `shippings.status` (initial/wait_pickup/shipping/done, updated in `/ship`, `/ship_done`, `/complete`). Return that column and make zero external calls in the list endpoint.
- when it works: when the bench only checks the status at points where the app itself has just updated the DB. Official: 「ベンチマーカーでのチェックの仕様により、取引一覧でのAPIアクセスは実は必要がなく、配送ステータステーブルの情報を表示すればエラーにならないようになっておりました。」 Bench action.go only asserts `initial` after /buy, `wait_pickup` after /ship, `shipping` after /ship_done, `done` after /complete, and verify.go only asserts `ShippingStatus != ""`.
- how to verify: read `bench`-visible behaviour via the state table in APPLICATION_SPEC.md (ISUCARI ステータス遷移表) and the app's own writes to `shippings.status`; the shipment server adds `withDelay()` to `/create /request /accept /status` (bench/server/shipment.go) and the delay is set to 800 ms (cmd/bench/main.go `ss.SetDelay(800 * time.Millisecond)`).
- risk: `/ship_done` and `/complete` still must call the API (they drive the real state machine; `/complete` must see `done`). methane kept a commented-out "fail するようなら再確認する" fallback.
- evidence:
  - nil "Use database value instead of external services" (removes the goroutine fan-out, selects `s.status AS shipping_status`): https://github.com/takonomura/isucon9-qualify/commit/1cd1a0eb ; nil blog 「shippings テーブルの status を取ってくるだけで問題なく機能しそう」→ 17,260 by that point: https://www.takono.io/posts/2019/09/isucon/
  - methane team "shipping status の呼び出しを減らす": https://github.com/methane/isu9q/commit/a4901b68 ; blog 「GET /transaction のなかからの呼び出しを削除してその時点の暫定1位」: https://methane.hatenablog.jp/entry/2019/09/10/ISUCON9%E4%BA%88%E9%81%B8%E5%8F%82%E5%8A%A0%E8%A8%98_%285%E4%BD%8D%E9%80%9A%E9%81%8E%29
  - netmarkjp 「/users/transactions.json 内の外部APIとのやりとりがジャンプアップのポイント」16000→26000: https://netmark.jp/2019/09/post-1461.html
  - bench: https://github.com/isucon/isucon9-qualify/blob/master/bench/scenario/action.go , https://github.com/isucon/isucon9-qualify/blob/master/cmd/bench/main.go
- regulation note: none forbids it; the manual's 「即座に反映」 rule concerns item lists, not shipment state.

### T: [Q] Cache shipment-status responses only in terminal/known states (`initial`, `done`), never cache `wait_pickup`/`shipping`
- what: intermediate variant used by teams that kept calling the API: cache `initial` right after `/create` (known), cache `done` forever (never changes), skip caching for in-flight states.
- when it works: external API responses that are immutable after a known transition; the EXTERNAL_SERVICE_SPEC state list makes `done` terminal.
- how to verify: the status spec section 「ステータスの仕様」 in EXTERNAL_SERVICE_SPEC.md.
- risk: 白金動物園 first cached everything with a 10 s TTL and had to back it out (rosylilly: 「wait_pickup 以降…想像以上にベンチマークはシビアにチェックしてくる。ならばキャッシュするだけ無意味」).
- evidence:
  - 白金動物園 "Redis cache" → "Do not cache shipment api" (keeps `redis.set` for initial/done, TTL 0 otherwise): https://github.com/shirokanezoo/isucon9q/commit/a18614a1 , https://github.com/shirokanezoo/isucon9q/commit/86eb73bb ; gist: https://gist.github.com/rosylilly/79bd3b146c36aa16eb10e7949571edea
  - matsuu 「StatusがdoneになったものをアプリのインメモリでキャッシュT」19000→26000: https://matsuu.hatenablog.com/entry/2019/09/08/195735
- regulation note: none.

### T: [Q+F] Run external API calls concurrently (shipment create ∥ payment token in `/buy`; per-row status calls in lists)
- what: in `POST /buy` fire `APIShipmentCreate` in a goroutine/thread while `APIPaymentToken` runs, then `wg.Wait()`; in `transactions.json` (if still calling) fan out the status calls. Ruby teams used `expeditor` with a 16–32 thread pool.
- when it works: independent idempotent-ish calls each costing a fixed 0.8 s; the transaction stays open meanwhile, so this also halves lock hold time on the item row.
- how to verify: access log shows `/buy` ≈ 1.6 s+ and `/users/transactions.json` ≈ N×0.8 s (にがり's kataribe: mean 2.49 s, max 5.7 s for transactions.json).
- risk: the shipment API returned nginx 400/502 under high concurrency (nil, methane) → add retry (next block). Payment must not be called twice for one item (critical error, see scoring).
- evidence:
  - nil "Parallel buy": https://github.com/takonomura/isucon9-qualify/commit/ce3e0853
  - にがり 「16:10 APIの非同期化 → 35000」(from 22000): https://to-hutohu.com/2019/09/09/isucon9-qual/
  - isucon_friends 「/buy 内でAPI呼び出し…並列処理化、/users/transactions.json…並列処理化。25000点に一気に伸びた」(from 16000): https://yosuke-furukawa.hatenablog.com/entry/2019/09/08/232231
  - 白金動物園 "expeditor shipment/payment": https://github.com/shirokanezoo/isucon9q/commit/45976e05 ; "bulk_shipment_status": https://github.com/shirokanezoo/isucon9q/commit/17a6b35d
  - official: 「必要な回数のAPI呼び出しを非同期・並列で行うことでかかる時間を短くする方法が考えられます」
- regulation note: external services may only be reached from the 3 contest servers (manual 「競技に利用できる計算機資源は主催者側が指定した3台まで」); the bench shipment/payment servers have `withIPRestriction()`.

### T: [Q] Retry transient 4xx/5xx from the external APIs
- what: on `status code: 400; body: <html>` (an nginx error page, not an API error) or 502, sleep 10 ms–1 s and retry up to 3 times.
- when it works: errors not listed in the API spec (so they are infrastructure, not business errors) that disappear on retry.
- how to verify: log the error body; nil: 「エラーは外部 API のドキュメントに書かれているものではなく、nginx が出力するもの」.
- risk: methane's team spent hours on 502s that turned out to be an organiser-side outage (「追記: 障害だったようです」). Retrying `/token` (payment) for the same item risks the 多重決済 critical error — retry only non-payment calls or only on connection-level failure.
- evidence:
  - nil "Retry when failed to fetch status": https://github.com/takonomura/isucon9-qualify/commit/5f6a7533
  - 白金動物園 "shipment, payment retry" (`start_with_retry(tries: 3, sleep: 1)`): https://github.com/shirokanezoo/isucon9q/commit/d90d4ca7
  - methane team "APIリトライ": https://github.com/methane/isu9q/commit/dc77e68e ; blog: https://methane.hatenablog.jp/entry/2019/09/10/ISUCON9%E4%BA%88%E9%81%B8%E5%8F%82%E5%8A%A0%E8%A8%98_%285%E4%BD%8D%E9%80%9A%E9%81%8E%29
- regulation note: bench payment server emits `ErrCritical "多重決済を検知しました"` (bench/server/payment.go) → one occurrence fails the run.

### T: [Q] Per-item application mutex + "already sold" short-circuit before opening the DB transaction in `/buy`
- what: `itemMutex[itemID % 100].Lock()` at the top of `postBuy`; keep a `soldCache map[int64]bool` and return 403 `item is not for sale` immediately when set; set it when `targetItem.Status != on_sale` is observed. Waiters queued behind the mutex then fail fast instead of each opening a tx and waiting on the `FOR UPDATE` row lock while the winner does two 0.8 s API calls.
- when it works: 人気者出品 — bench sends 80–160 simultaneous `/buy` for one item every 8 s (campaign.go `popularListing(ctx, 80+j*20, 1000+j*100)`); only one can win; 10 % of buyers deliberately use a failing card (`FailedCardNumber`), so you cannot just let the first requester win without actually attempting payment.
- how to verify: `SHOW PROCESSLIST` full of `SELECT * FROM items WHERE id = ? FOR UPDATE` (matsuu tagged them `/* postBuy */` to tell apart); `Lock wait timeout` / `Too many connections` in logs; bench messages 「タイムアウトしました」 on /buy.
- risk: a process-local mutex only works if `/buy` is routed to one app process (白金動物園 pinned `location = /sell`/`/buy`-style paths to one host; nil/にがり ran all non-login traffic on one app). Removing `FOR UPDATE` outright caused errors (nakario: 「もっと FOR UPDATE を大胆に消してみる→エラーが出たので大急ぎで戻す」).
- evidence:
  - methane team "log and lock" (itemMutex) + "sold cache": https://github.com/methane/isu9q/commit/0a214ce2 , https://github.com/methane/isu9q/commit/f98b1d6d ; blog: 「売れた商品をキャッシュするようにするとタイムアウトが解決しました」
  - matsuu 「POST /buy のデッドロック対策として sync.Map の LoadOrStore() を使って楽観ロックもどき」15000→16000: https://matsuu.hatenablog.com/entry/2019/09/08/195735
  - official: 「1つの商品につき1つのトランザクションのみが開始されるようアプリケーションにて制御を行い、ロックが解放された直後のトランザクション開始前に商品が購入できるステータスにあるかどうかをチェックすると…」
  - bench campaign.go: https://github.com/isucon/isucon9-qualify/blob/master/bench/scenario/campaign.go
- regulation note: status codes must match the reference (403 `item is not for sale` is what the reference returns for a sold item).

### T: [Q] Route `POST /login` to dedicated app servers (bcrypt offload) via nginx `location /login { proxy_pass login_upstream; }`
- what: bcrypt cost-10 compare ≈ 0.1 s CPU per login and dominates pprof once the campaign is on. Keep nginx+app+(or DB) on isu01 and send `/login` with weighted upstream to isu02/isu03 (nil: 2:7:1 across three hosts). Everything else stays on one app so in-memory caches/mutexes remain valid.
- when it works: a CPU-bound, stateless endpoint (reads `users`, writes only a session cookie) whose cost cannot be reduced legally; the bench needs many successful logins to fuel 人気者出品 (campaign.go: 「buyerが足りない場合はログインを意図的に遅くしている可能性があるのでペナルティとして実行しない」).
- how to verify: pprof flame graph with `bcrypt.CompareHashAndPassword` on top; CPU 100 % on app host while DB idle; after enabling campaign the score drops (nil: 17,260 → 6,850 when campaign turned on before the split).
- risk: sleeping/throttling login is explicitly punished (official: 「sleepをいれて処理を遅らせるなどの対策でログインをさぼると、人気者出品の発動に必要な人数が集まらず」). Session store must be shared or cookie-based (ISUCARI uses gorilla cookie store, so no shared store needed).
- evidence:
  - nil "Only login for isu02" / "Serve login on isu03 too": https://github.com/takonomura/isucon9-qualify/commit/ab8a1565 , https://github.com/takonomura/isucon9-qualify/commit/73b2ac72 ; blog: 「残りの1台のサーバーを POST /login だけに使う…スコアは 34,240」(from 6,850)
  - にがり 「15:45 構成変更 → 22000」(1台を/login専用, from 10000): https://to-hutohu.com/2019/09/09/isucon9-qual/
  - いんふらえんじにあー 「2台目と3台目は /login のアクセスのみ振り分け」: https://matsuu.hatenablog.com/entry/2019/09/08/195735
  - 百万円ドリブン 「bcryptの計算専用に２台のサーバを使う…複数台化した時点(16:42)で跳ね上がってる」: http://nakario.hateblo.jp/entry/2019/09/09/231122 , https://chy72.hatenablog.com/entry/2019/09/11/162304
  - official: 「サーバーインスタンスを追加してログイン処理専用とする方法も考えられたでしょう。」
- regulation note: manual forbids 「パスワードを平文で保存する」; nothing forbids distributing login.

### T: [Q] Lower bcrypt cost / progressively re-hash to SHA-256 on successful login
- what: (a) set `BCRYPT_COST=4` for newly registered users; (b) on each successful bcrypt login, re-store the password as SHA-256 so subsequent logins are cheap; isucon_friends dumped the re-hashed table between bench runs and baked it into the initial data.
- when it works: only when the bench logs in the same users repeatedly (it does: `asset.GetRandomBuyer()` pool) and you can persist the rehash across `/initialize` (otherwise it resets every run).
- how to verify: count distinct accounts in `POST /login` from the access log; chy72: 「実はログインはそんなに成功しないことが分かりました…数十回ベンチを回す必要がありそう」.
- risk: lowering cost only affects new registrations (にがり: 「ベンチーマーカーからの新規ユーザーはほとんどなく効果はありませんでした」). Storing plaintext is a disqualification; SHA-256 (salted) is a hash so allowed, but it is a judgment call — isucon_friends 「fail されないかビクビクしてた」.
- evidence:
  - isucon_friends 「bcrypt 剥がして SHA256 に rehash…10000点から少しずつ伸びて…14000点」: https://yosuke-furukawa.hatenablog.com/entry/2019/09/08/232231
  - 白金動物園 "BCRYPT_COST=4": https://github.com/shirokanezoo/isucon9q/commit/2f65b030
  - にがり main.go `bcrypt.GenerateFromPassword([]byte(password), 4)`: https://github.com/tohutohu/isucon9/blob/master/webapp/go/main.go
  - chy72 bcrypt analysis (Go/Python/Ruby bcrypt speed ≈ same): https://chy72.hatenablog.com/entry/2019/09/11/162304
- regulation note: 「以下に示す改変を行ってはなりません - パスワードを平文で保存する」(docs/manual.md).

### T: [Q] Raise `campaign` in `/initialize` (0→4) only once login capacity exists
- what: return `{"campaign": N}`; each level adds one more `Load()` worker (scenario.go: 還元率 0→負荷2, 1→3, 2→4, 3→5, 4→6) and enables 人気者出品. Bump it when CPU/DB have headroom; it is the only lever that raises the ceiling.
- when it works: after the per-request costs are down and `/login` is offloaded; otherwise more users = more bcrypt = timeouts.
- how to verify: nil's experiment: turning campaign on at 17,260 dropped to 6,850 until login was split; best score at campaign=3 (53,460), final at 4 (52,440). にがり jumped 4500→5000 at campaign=1 with headroom.
- risk: sambaiz's team forgot it existed (「仕様をたいして読まなかったのでキャンペーンの数値が変えられることも忘れていた」).
- evidence:
  - bench scenario.go campaign table: https://github.com/isucon/isucon9-qualify/blob/master/bench/scenario/scenario.go
  - nil commits "Enable campaign" → "Increase campaign level to 4": https://github.com/takonomura/isucon9-qualify/commit/2d252fe1 … https://github.com/takonomura/isucon9-qualify/commit/935f47b4 ; blog https://www.takono.io/posts/2019/09/isucon/
  - isucon_friends 「キャンペーン還元率を 1 => 2, 3, 4 と一気に変えていった。16000点に」; 白金動物園 campaign=1→2→3→4→3: https://github.com/shirokanezoo/isucon9q/commits/master
- regulation note: manual 「campaignが還元率の設定となります。有効な値は 0 以上 4 以下の整数」; 「ｲｽｺｲﾝ還元の費用が下で説明するスコアから引かれることはありません」.

### T: [Q] Personalise `GET /new_items.json` to the buyer's parent category (the "recommend" bonus)
- what: each initial-data buyer only ever buys one parent category (`asset.GetUser(id).BuyParentCategoryID`). If >80 % of the 48 items on the first timeline page are in that parent category, the bench treats the timeline as "recommended": it skips the 7-category browse (7×(10 pages+20 items)) and instead buys twice per scenario loop. Implement by filtering `/new_items.json` by the logged-in user's preferred root category (derive from their past purchases in `transaction_evidences.item_root_category_id`).
- when it works: this contest only — the manual explicitly invites it: 「新着一覧については、上記の制限を満たした上でよりユーザにあわせた商品の一覧を返すことで、購入の機会を増やすことができます。」 Bench `loadIsRecommendNewItems`: `float64(isTarget)/float64(len(items)) > 0.8`.
- how to verify: `SELECT buyer_id, item_root_category_id, COUNT(*) FROM transaction_evidences GROUP BY 1,2` shows one root category per buyer (official: 「ソファーカテゴリを購入するユーザはソファーカテゴリのみを購入している」); still must return exactly 48 items, `created_at DESC`, only `on_sale`/`sold_out`.
- risk: shuffling the order failed (methane: 「新着商品の順番をページ内でシャッフルしてみたところベンチマーカーに怒られた」). Picking the category by `user_id % n` (白金動物園) passed the checker and reduced lock contention (10,000→12,000) but missed the real bonus (rosylilly: 「事前登録済みの総てのユーザーは自分が買ったことのあるカテゴリを3つから4つ持っている…無念」). Official says the gain is 「数%」; the bench code suggests more (2× buys per loop).
- evidence:
  - bench load.go `loadIsRecommendNewItems` / 「recommended なら購入2倍」: https://github.com/isucon/isucon9-qualify/blob/master/bench/scenario/load.go
  - 白金動物園 "User fitting" / "Category id for user" / "Lock a category": https://github.com/shirokanezoo/isucon9q/commit/9cb1b58f , https://github.com/shirokanezoo/isucon9q/commit/965827fe , https://github.com/shirokanezoo/isucon9q/commit/f60cfa77 ; mirakui: https://blog.mirakui.com/entry/2019/09/13/003917
  - official 講評 「新着商品一覧でそのユーザが購入するカテゴリにあわせて一覧を返すことで数%ですが、スコアがあがるようになってます」: https://isucon.net/archives/53789931.html
- regulation note: 「古いデータの削除、非表示はベンチマーク上で許可されません。各商品一覧取得APIが一度に返す商品数は初期実装と同じ状態を保つ必要があります。」(manual) — the per-user filtering must still satisfy these.

### T: [Q] Hide the viewer's own items and sold-out items from the timeline (contested)
- what: 白金動物園 additionally filtered `/new_items.json` with `seller_id != current_user` and `status = 'on_sale'` only, reasoning that only buyable items create revenue.
- when it works: passed the ISUCON9 checker (verify.go only asserts status ∈ {on_sale, sold_out} and ≥48 items), but it sits against the manual's wording.
- how to verify: run the bench; read verify.go `verifyItemIDsFromNewItems`.
- risk: manual says 「古いデータの削除、非表示はベンチマーク上で許可されません」; this is a grey area — treat as "worked for one team, not officially endorsed".
- evidence: https://github.com/shirokanezoo/isucon9q/commit/821d59da ("Do not show sold out"), https://github.com/shirokanezoo/isucon9q/commit/896921b5 ; gist https://gist.github.com/rosylilly/79bd3b146c36aa16eb10e7949571edea
- regulation note: as above.

### T: [Q] In-process user cache (`userMap` with RWMutex) + split hot counters out of `users`
- what: cache `users` rows in memory for `getUser`/`getUserSimpleByID` (write-through on `/sell` which increments `num_sell_items`, and `/bump` which updates `last_bump`). 白金動物園 instead moved `num_sell_items`/`last_bump` into a new `user_stats` table so the `users FOR UPDATE` in `/sell`/`/bump` no longer collides with reads.
- when it works: 4,000 users, ~200 KB; only `/sell` and `/bump` write to `users` (and `/register`). Multi-app invalidation is the blocker: にがり gave up the users cache when going to 3 app servers, and could only re-enable it after making non-login traffic single-host.
- how to verify: grep `UPDATE \`users\``; look at `postSell`/`postBump` (`SELECT ... FOR UPDATE` on users).
- risk: stale `num_sell_items` is checked by the bench (check.go compares `NumSellItems` before/after sell).
- evidence:
  - にがり main.go `userMap`/`userMapMux`, blog 「13:20 getUserSimpleByIDのキャッシュ → 4200」「14:00 …getUserもオンメモリキャッシュに → 7400」: https://github.com/tohutohu/isucon9/blob/master/webapp/go/main.go , https://to-hutohu.com/2019/09/09/isucon9-qual/
  - matsuu 「UserSimple をアプリのインメモリにキャッシュ保存( sync.Map )…更新のたびにキャッシュを更新」
  - 白金動物園 "user_stats table" (03_process.sql moves columns): https://github.com/shirokanezoo/isucon9q/commit/7eeb4098 , https://github.com/shirokanezoo/isucon9q/commit/4cba67a2
- regulation note: data must survive reboot — caches must be rebuilt from MySQL, not be the source of truth.

### T: [Q+F] Split roles across the 3 hosts: nginx+app on #1, MySQL on #3 (and a second app on #2)
- what: `MYSQL_HOST` to the VPC IP of host 3, `bind-address` changed, `max_connections` raised; nginx on #1 fronts everything (bench hits only one IP). ISUCARI's `init.sh` already honours `MYSQL_HOST`, so `/initialize` keeps working.
- when it works: once the single host is CPU-saturated by app+DB together (にがり: 「DBの方はかなりCPUに余裕が出てきたにもかかわらず、AppがCPU100%ベタ付き」).
- how to verify: `dstat`/`top` per host; nginx `upstream` logs.
- risk: in-process caches/mutexes break across app hosts (see above). In 本選, matsuu's team never got MySQL out of docker and lost the multi-host benefit; netmark lost the end of the contest making MySQL auto-start after stripping docker.
- evidence:
  - nil "Split database to isu03": https://github.com/takonomura/isucon9-qualify/commit/c74a0569 ; nginx upstream with weights: https://github.com/takonomura/isucon9-qualify/commit/73b2ac72
  - にがり 「14:00 構成変更 Nginx+App, DBの2台構成 → 7400」「14:45 Nginx+App, App, DBの3台構成に → 10000」
  - isucon_friends 「3台構成にして、nginx, app, DB にした。この時点で 8800点程度」
  - 白金動物園 本選 "isu3 is now running mysql", "enable isu2" (two puma hosts): https://github.com/shirokanezoo/isucon9f/commit/0860beab , https://github.com/shirokanezoo/isucon9f/commit/98ffac92
- regulation note: 「複数台あるサーバーの役割の変更」allowed; 本選 manual: 「ベンチマーク中に利用が認められたサーバー以外の外部リソースを使用する行為…は禁止」.

### T: [Q] nginx: HTTP/2, keepalive upstream, `worker_rlimit_nofile`, `gzip_static`, `open_file_cache`, serve `/static` and `/upload` directly with `Cache-Control`
- what: `listen 443 ssl http2`; `upstream app { server 127.0.0.1:8000; keepalive N; }` with `proxy_http_version 1.1`; `worker_rlimit_nofile 100000; worker_connections 4096`; pre-gzipped assets (`*.gz`) with `gzip_static on`; `root /home/isucon/isucari/webapp/public; location /static/ { add_header Cache-Control "public max-age=86400"; }`; `client_body_buffer_size` up to avoid temp files on image upload.
- when it works: bench talks HTTPS and supports HTTP/2 (official: Go 1.13 `ForceAttemptHTTP2`); initial nginx is stock Ubuntu config (official: 「ほぼデフォルトで状態であるため、ベンチマーク中にnginx が使うCPUが高くなります」). Static files are md5-checked by the bench (asset.go `calcMD5`) so content must be byte-identical.
- how to verify: `nginx -T`; CPU of nginx worker in `top`; "Too many open files" in error.log (matsuu hit it).
- risk: nil reported no measurable score change from static-file serving late in the day; the big wins are elsewhere. JSON gzip did shrink responses (isucon_friends).
- evidence:
  - nil "Configure nginx" / "Serve static files on nginx" / "Gzipstatic files": https://github.com/takonomura/isucon9-qualify/commit/7144bb21 , https://github.com/takonomura/isucon9-qualify/commit/317a4c74 , https://github.com/takonomura/isucon9-qualify/commit/b6e3dff6
  - matsuu: keepalive_requests, http2, upstream keepalive, static from nginx, worker_rlimit_nofile, client_body_buffer_size: https://matsuu.hatenablog.com/entry/2019/09/08/195735
  - isucon_friends 「nginxのエンドポイントを HTTP/2 にした」「Accept-Encoding が gzip だったら gzip 返すように」
  - 白金動物園 "extend client_body_buffer_size": https://github.com/shirokanezoo/isucon9q/commit/4be26060
- regulation note: レギュレーション forbids changing 「JavaScript/CSSファイルの内容」「画像および動画等のメディアファイルの内容」; serving them from nginx (with gzip/cache headers) is fine; 本選 adds 「静的ファイルの内容の変更」= fail.

### T: [Q+F] Connection limits: MySQL `max_connections`, Go `SetMaxOpenConns/SetMaxIdleConns`, `http.Transport.MaxIdleConnsPerHost` for the external APIs
- what: `max_connections = 1024`–4096; `db.SetMaxOpenConns(512); SetMaxIdleConns(512); SetConnMaxLifetime(10s)`; `http.DefaultTransport.(*http.Transport).MaxIdleConnsPerHost = 1000`–4096 so parallel external calls reuse TLS connections instead of opening new ones (default is 2 idle/host).
- when it works: after parallelising external calls (otherwise idle connections pile up / handshake storm), and after campaign raises concurrency ("Too many connections" — methane).
- how to verify: `SHOW GLOBAL STATUS LIKE 'Threads_connected'`, `Max_used_connections`; app logs `Too many connections`; TIME_WAIT count.
- risk: none reported beyond memory.
- evidence:
  - nil "Increase max connections": https://github.com/takonomura/isucon9-qualify/commit/e67785e9 ; "Configure database connection": https://github.com/takonomura/isucon9-qualify/commit/adca81f6
  - にがり main.go `MaxIdleConnsPerHost = 1000` (「GoのHTTP Clientは同じホストへの接続本数が制限されている」→4500): https://github.com/tohutohu/isucon9/blob/master/webapp/go/main.go
  - matsuu 「http.DefaultTransportのMaxIdleConnsPerHostを4096に引き上げ」16000→19000; 「appからMySQLの同時接続数を最大4096まで引き上げ」
  - isucon_friends 「MaxIdleConns と MaxIdleConnsPerHost を拡張、1 ホスト 3000 コネクション」
- regulation note: none.

### T: [Q] Move QR PNG (`shippings.img_binary`, mediumblob) out of MySQL to files and serve via `X-Accel-Redirect`; keep uploaded item images on the host that received them
- what: on `/ship` write the QR PNG to `/home/isucon/public.local/upload/<host>/tx_<id>.png`, store only `image_name`; `/transactions/:id.png` returns `X-Accel-Redirect: /_upload/<host>/...` and nginx proxies `/_upload/N` to the host that holds the file. Item images uploaded via `/sell` are likewise kept local and `get_image_url` points to the owning host (`IMG_SERVER_MAP`), with `/upload/N/` locations proxied to that host.
- when it works: multi-app setups where uploads land on one of several hosts; blob columns bloating `SELECT *` on shippings.
- how to verify: `shippings.img_binary mediumblob NOT NULL` in schema; `/upload` served from the app in reference; bench md5-checks the QR (`DownloadQRURL`) and item images.
- risk: image md5 must match exactly; redirects (301 rewrite) were replaced by proxy_pass. Data must persist reboot (files on disk are fine).
- evidence: https://github.com/shirokanezoo/isucon9q/commit/cc094b3c ("remove transaction_evidence img_binary"), https://github.com/shirokanezoo/isucon9q/commit/b34872bf ("_upload"), https://github.com/shirokanezoo/isucon9q/commit/b8fb2ed2 ("img_server_map"), https://github.com/shirokanezoo/isucon9q/commit/ed6736e1 ("process sell only on isu1")
- regulation note: 「アプリケーションは全て保存データを永続化する必要があります」(manual) — files on local disk satisfy it.

### T: [Q+F] Final-run hygiene: disable access log, slow log, pprof/tracing/New Relic/stackprof, netdata
- what: `access_log off;`, comment out `slow_query_log`, remove profiler middleware, stop monitoring agents before the last bench.
- when it works: always; several teams' final bump came from this.
- how to verify: n/a.
- risk: mirakui accidentally left stackprof enabled in the final 本選 run; 「外部のメトリクス計測サービスの使用のみ特例として許可」 so New Relic is legal but costs CPU.
- evidence:
  - nil "Disable tracing"/"Disable profiler"/"Disable logging"/"Stop netdata": https://github.com/takonomura/isucon9-qualify/commit/5c6edaae , https://github.com/takonomura/isucon9-qualify/commit/740d7e2b , https://github.com/takonomura/isucon9-qualify/commit/df0e1b0e , https://github.com/takonomura/isucon9-qualify/commit/125eb975
  - 白金動物園 "Sayonara" (New Relic off, slow log off, ltsv off) → 15,100; "access_log off; retry": https://github.com/shirokanezoo/isucon9q/commit/6d9b6716 , https://github.com/shirokanezoo/isucon9q/commit/0eea615c ; 本選 "Remove New Relic": https://github.com/shirokanezoo/isucon9f/commit/7138931f
  - isucon_friends 「最後にすべてのログをオフにして…27420点」; にがり 「最後にすべてのログ出力を止め」
- regulation note: 「外部のメトリクス計測サービスの使用のみ特例として許可しますが、スコアを向上させるいかなる効果も持つものであってはいけません」(manual).

### T: [Q] Shrink/relax row locks in `/buy` family — what worked and what did not
- what: methane dropped `FOR UPDATE` on the seller `users` row inside `/buy` (only the item row needs the lock). 白金動物園 tried `LOCK IN SHARE MODE` on items (33884cba) and went back to `FOR UPDATE` (03cf4a62), tried an extra `transaction_evidences FOR UPDATE` and removed it. nakario removed "obviously unnecessary" FOR UPDATEs (passed), then more (failed, reverted at 18:05). isucon_friends set the isolation level to READ UNCOMMITTED (+1,000, 「fail されないかビクビク」).
- when it works: locks on rows that the transaction does not modify (seller row in `/buy` is read-only).
- how to verify: list every `FOR UPDATE` and ask what the tx later `UPDATE`s; `innodb_lock_wait_timeout=1` to surface deadlocks quickly (matsuu).
- risk: double-sell → bench `BuyWithMayFail` detects two winners (critical).
- evidence: https://github.com/methane/isu9q/commit/0a214ce2 ; https://github.com/shirokanezoo/isucon9q/commit/33884cba , https://github.com/shirokanezoo/isucon9q/commit/03cf4a62 , https://github.com/shirokanezoo/isucon9q/commit/8c56ce46 , https://github.com/shirokanezoo/isucon9q/commit/2295bd74 ; nakario http://nakario.hateblo.jp/entry/2019/09/09/231122 ; furukawa https://yosuke-furukawa.hatenablog.com/entry/2019/09/08/232231
- regulation note: none; correctness is enforced by the checker.

### T: [Q] Detach DB/HTTP work from the request context (`context.Background()`) so client disconnects at the 60 s cut-off don't abort half-done transactions
- what: nil replaced `r.Context()` with `context.Background()` for tx/external calls after adding tracing contexts.
- when it works: bench force-closes in-flight requests at the end (499 in nginx), and Go cancels the context → rollbacks/half states.
- how to verify: 499s in the access log; "context canceled" in app log.
- risk: lost cancellation; fine for a 60 s bench.
- evidence: https://github.com/takonomura/isucon9-qualify/commit/cb3cee95 ("Use context.Background() to prevent cancel")
- regulation note: manual: 499 requests are 「減点の対象外」.

### T: [Q+F] Measurement set-up that the winners used (so the above can be found in time)
- what: nginx LTSV + alp/kataribe; `long_query_time=0` + pt-query-digest (mirakui: look at *fast* high-count queries, not only slow ones); pprof / Stackdriver Profiler (nil) / stackprof flame graph auto-published on the contest nginx (sorah); request-ID in a response header + as SQL comment `/* reqid:... */`; logged-in user ID in a response header logged by nginx; POST params to a JSON file queried with trdsql; `SELECT /* postBuy */ ...` tags for SHOW PROCESSLIST.
- evidence: mirakui 本選 https://blog.mirakui.com/entry/2019/10/14/011554 ; 白金動物園 commits "Add request id", "Record user_id if exists", "Logging POST params", "Add req_id into some sqls as comments": https://github.com/shirokanezoo/isucon9f/commit/a0d7cb1d , https://github.com/shirokanezoo/isucon9f/commit/bb1cadbc , https://github.com/shirokanezoo/isucon9f/commit/10e97e40 , https://github.com/shirokanezoo/isucon9f/commit/096b8872 ; にがり Makefile (make kataru/slow/pprof → Slack): https://github.com/tohutohu/isucon9/blob/master/Makefile ; nil blog tool list; matsuu 「SELECT /* postBuy */ * FROM items」.

---

## Part 2 — 本選 ISUTRAIN (ISUXPRESS予約) techniques

### T: [F] Index `train_timetable_master(date, train_class, train_name, station)` first — nothing scores before it
- what: `/api/train/search` scans the 2.8 M-row timetable per candidate train; initial schema has no indexes (and most master tables have no PRIMARY KEY). This one index takes the reference from fail to ≈1,500.
- when it works: immediately; official: 「列車検索が遅すぎて初期スコアが出ません。次の通りDBにIndexを作ると、1500点くらい獲得できます。」
- how to verify: `docs/IMPLEMENT.md` literally tells you; `webapp/sql/01_schema.sql` shows no keys on master tables.
- risk: see the PRIMARY KEY / ordering trap next.
- evidence: official https://isucon.net/archives/53916974.html ; IMPLEMENT.md https://github.com/isucon/isucon9-final/blob/master/docs/IMPLEMENT.md ; 白金動物園 "Add index on train_timetable_master": https://github.com/shirokanezoo/isucon9f/commit/5f3470d9 ; ARINE 「Indexをはってベンチを流す。5,089点」https://labs.gree.jp/blog/2019/10/19561/ ; 白金動物園 also "Add index: seat_reservations.reservation_id" https://github.com/shirokanezoo/isucon9f/commit/abd0e419
- regulation note: IMPLEMENT.md says 「データベースのスキームは変更しないでください」 but that is the porting guide; the contest manual allows 「DBスキーマの変更やインデックスの作成・削除」.

### T: [F] Do not add a PRIMARY KEY / clustered index to key-less master tables without adding `ORDER BY` — insertion order is part of the expected output
- what: `train_master`, `seat_master`, `fare_master` etc. have no PK; the reference `SELECT * FROM train_master WHERE date=? AND train_class IN (...) AND is_nobori=?` relies on heap order (= train_name order). Adding a PK/secondary index changes the scan order → pretest fails (「初期データと明らかに異なるデータがレスポンスに含まれている」), and dropping the index does not restore the old order. Fix: add `ORDER BY train_name` (and `ORDER BY` on every master read) before indexing, or re-import data.
- when it works: any query without ORDER BY on a table whose physical order was meaningful.
- how to verify: official 6.4: 「一部のテーブルにおいてPRIMARY KEYが設定されておらず、ORDER BYによる順序制御がなかったためにPRIMARY KEYの設定で順序が変わってしまい、pretestで失格判定になる場合がありました」.
- risk: いんふらえんじにあー lost until 15:45 (「インデックスで大爆死」, data restored from another host's physical backup); しゃかラン first green bench only at 12:58 (「元々KEYが無かったテーブルにインデックスを追加したことで、取得する順番が変わってしまった」).
- evidence: shmurakami "order" commit adding `order by train_name`: https://github.com/shmurakami/isucon9_final/commit/10c7c339 ; blog https://shmurakami.hatenablog.com/entry/2019/10/10/163630 ; matsuu https://matsuu.hatenablog.com/entry/2019/10/06/145445 ; netmark https://netmark.jp/2019/10/post-1485.html ; official https://isucon.net/archives/53916974.html
- regulation note: manual 「列車検索の結果件数が10件未満の場合も失格とします」; pretest compares against initial data.

### T: [F] Dump master tables into code / process memory selectively (station, fare, distance_fare, seat_master, train_master) — but not the 2.8 M-row timetable
- what: `station_master` (82), `distance_fare_master` (~10), `fare_master` (81), `seat_master` (3,942), `train_master` (70,272) are read-only; 白金動物園 dumped them with Ruby `Marshal` into source files and replaced every `db.xquery` on them with hash lookups (`STATIONS_HASH_BY_NAME`, `FARE_MASTER_BY_CLASS["#{train_class}\0#{seat_class}"]`, `SEAT_MASTER_BY_CLASS`, `Isutrain.get_train(date, class, name)`). The timetable (2.8 M rows) must stay in MySQL (or be memoised per accessed key — chy72).
- when it works: RAM is 1 GB (official: 「Linuxにはメモリを1GBしか認識させていません」); the masters above fit, the timetable does not. Official: 「変動しない情報はメモリに載せることもしつつ、全ては乗り切らないのでメモリだけでは解決しません」. Secondary benefit: fewer pages in the InnoDB buffer pool (rosylilly: 「ここにアクセスしなければしないだけ、MySQL のメモリが空くことを狙っていた。事実、再起動後にスコアが上がった」).
- how to verify: `SELECT COUNT(*)` per table (official numbers above); `free -m`; sambaiz hit OOM after loading everything and raising available_days.
- risk: OOM (sambaiz), gopls eating RAM on the server (matsuu). Docker-compose log buffering also ate memory (matsuu/netmark).
- evidence: 白金動物園 "Static station master" https://github.com/shirokanezoo/isucon9f/commit/8f05d4d4 , "Dump master tables" https://github.com/shirokanezoo/isucon9f/commit/d15c54b6 , "embed DISTANCE_FARE_MASTER, FARE_MASTER" https://github.com/shirokanezoo/isucon9f/commit/3babaedd , "SEAT_MASTER_BY_CLASS" https://github.com/shirokanezoo/isucon9f/commit/9a36b9e2 , "Good bye train master" https://github.com/shirokanezoo/isucon9f/commit/e13716e5 , "Good bye seat master"/"Remove seat master" https://github.com/shirokanezoo/isucon9f/commit/92d3255e , https://github.com/shirokanezoo/isucon9f/commit/cbeff5fc ; ARINE 「seat masterをインラインにすることで…25,841点」(from 15,066): https://labs.gree.jp/blog/2019/10/19561/ ; methane 「distance_fare_master のオンメモリ化」https://methane.hatenablog.jp/entry/2019/10/09/ISUCON9%E6%B1%BA%E5%8B%9D%E5%8F%82%E5%8A%A0%E8%A8%98_%28%E3%83%81%E3%83%BC%E3%83%A0%E5%90%8D%3A_%E3%82%88%E3%81%86%E3%81%99%E3%82%8B%E3%81%AB%E3%83%A1%E3%82%AC%E3%83%8D%E3%81%8C%E5%A4%A7%E5%A5%BD ; chy72 感想戦 「train_masterのハードコーディング…train_timetable_masterのメモ化」105,417: https://chy72.hatenablog.com/entry/2019/10/29/075831 ; shmurakami 「master系のテーブルの中身をメモリ上に乗せて」, 「運賃計算周りでDBにアクセスする必要をなくし」11,376.
- regulation note: 「OSのメモリサイズが提供時と同じ状態であること」(manual) — you cannot un-cap the 1 GB.

### T: [F] Denormalise: `reservations.departure_id/arrival_id` and `seat_reservations.{date,train_class,train_name}` + composite index, so overlap checks need no JOIN to `station_master`/`reservations`
- what: add integer station IDs to `reservations` (set on insert from the in-memory station map) and copy `date/train_class/train_name` into `seat_reservations` on insert; add `seat_reservations(date,train_class,train_name,car_number)` index. Then `get_available_seats`, `/seats` and the reserve lock query read a single table. 「インサートコストよりセレクトコストが重い」.
- when it works: overlap test is on station IDs being ordered (official: 「乗車区間の重複は、駅IDが順番に並んでいることを利用します」); writes are few vs reads.
- how to verify: schema: `reservations.departure/arrival varchar(100)` (names), `seat_reservations` has only 4 columns and no index.
- risk: `/initialize` TRUNCATEs `seat_reservations`, `reservations`, `users` — the ALTERs must live in your schema/init files; forgot-index-in-schema nearly bit しゃかラン.
- evidence: 白金動物園 "reservation station id" https://github.com/shirokanezoo/isucon9f/commit/335c12e6 , "Seat resevation and date train info" https://github.com/shirokanezoo/isucon9f/commit/d37fd2d7 , "add index for seat_reservations" https://github.com/shirokanezoo/isucon9f/commit/1b9912e4 , "Change avail seats query" https://github.com/shirokanezoo/isucon9f/commit/c22d0341 , "Unjoin resevertion" https://github.com/shirokanezoo/isucon9f/commit/bdb99866 ; methane 「reservation テーブルの arrival, depature カラムに駅名ではなくIDを入れるようにした。これで getAvailableSeats で station との join が消せた」
- regulation note: schema changes allowed.

### T: [F] `/api/train/seats`: one query for all reserved seats of the car (overlapping segments), compute `is_occupied` in-app; car list from memory
- what: reference loops every seat of the car (×~100) and for each runs `seat_reservations ⋈ reservations` then a reservation lookup (N+1 ×2) and compares stations in Ruby/Go. Replace with one `SELECT seat_row, seat_column FROM seat_reservations JOIN reservations ... WHERE car_number=? AND date=? AND train_class=? AND train_name=? AND (<overlap condition>)` (3 UNION branches or 3 queries), build a set, map over the in-memory seat list. `simple_car_information_list` (16 cars) also comes from memory.
- when it works: official: 「これらのテーブルをJOINし、乗車区間重複のチェックもSQLで行うことで、1度のクエリで空席情報を取得することができます…これだけでもこのエンドポイントの処理時間が1/10程度になります。」
- how to verify: alp: `/api/train/seats` (3 pts) and `/api/train/search` top by total time; slow log shows thousands of `SELECT s.* FROM seat_reservations s, reservations r WHERE ... seat_row = ? AND seat_column = ?`.
- risk: the official overlap SQL has separate nobori/kudari conditions — get the inequalities right (pretest checks 不正な区間).
- evidence: 白金動物園 "reduce seats api query" (`get_reserved_seats`): https://github.com/shirokanezoo/isucon9f/commit/6c3e43a4 ; official 5.3.2 SQL: https://isucon.net/archives/53916974.html ; ARINE 「seatの重いクエリをなくしたところスコアがグッと上がる。12,867点」(from 5,089); chy72 「各席毎にDBに問い合わせるのをやめて、対象車の予約を一括で取ってきてアプリ側で席を管理」; shugo "seats: クエリ回数を減らした", "seats: メモリから読むようにした": https://github.com/shugo/isucon9_final/commits/master
- regulation note: none.

### T: [F] `/api/train/search`: fetch 10 trains' departure/arrival times and 4 seat-class availabilities in batched queries, not per train ×(2+4)
- what: official: 「発着時刻や料金はJOINや副問い合わせを使い、出来るだけクエリ数を削減する」「10編成×4種別まとめて取得できるようなSQL文を書くことでクエリ数を減らす」. chy72: pre-fetch the ~10 candidate trains then query availability once per batch.
- when it works: every scenario starts with search (official: 「全てのシナリオはこの列車検索を通るため」); 3 pts per success.
- how to verify: pt-query-digest count of `train_timetable_master` lookups per search request (request-ID trick makes this obvious).
- risk: ORDER BY must keep train_name order (see PK trap).
- evidence: official https://isucon.net/archives/53916974.html ; chy72 「getAvailableSeatsの対象となる列車リスト(およそ10車)を予め取得してから問い合わせることでN+1を消す」https://chy72.hatenablog.com/entry/2019/10/29/075831 ; shugo "件数だけ集計するよう修正" (count only): https://github.com/shugo/isucon9_final/commits/master
- regulation note: 「列車検索の結果件数が10件未満の場合も失格」.

### T: [F] Cache the per-(train_class, direction, seat_class, smoking, from, to) available-seat *count* in Redis with a TTL (2 s → 30 s)
- what: `get_available_seats` returns a count used only to render ○/△/× (threshold 10); 白金動物園 cached the count in Redis `psetex` 2000 ms, then 30 000 ms, keyed without date/train_name.
- when it works: the search result only shows coarse availability; 「実際、ここのチェックで問題でるほどチケット売れたチームいないんじゃないか？」(rosylilly). Caveat he gives himself: a count that never changes is a sign tickets aren't selling.
- how to verify: the ○/△/× thresholds in the search handler; bench did not reject the 30 s cache.
- risk: returning a fixed '－' for all four marks failed immediately (「Allowed?」→「Fix fake avail」: 「ダメでした」). Stale availability can let a user attempt a seat that is gone (reserve then fails, not a critical error).
- evidence: https://github.com/shirokanezoo/isucon9f/commit/3d467e59 (fake, failed), https://github.com/shirokanezoo/isucon9f/commit/307fb0a9 (revert), https://github.com/shirokanezoo/isucon9f/commit/114c0473 (cache 2 s), https://github.com/shirokanezoo/isucon9f/commit/e57f0b9b (30 s); rosylilly https://rosylilly.hatenablog.com/entry/2019/10/07/020627
- regulation note: 「キャッシュ機構の追加」allowed; pretest/finalcheck still validate reservations and fares.

### T: [F] `POST /api/train/reserve`: load all `seat_reservations` of that train once `FOR UPDATE`, then test seats in-app; skip the second validation pass for あいまい予約
- what: reference, for fuzzy reservation, loops 16 cars × every seat and runs a `FOR UPDATE` select per seat, then re-runs the full 指定席 validation on the chosen seats. 白金動物園: one `SELECT * FROM seat_reservations WHERE date=? AND train_class=? AND train_name=? FOR UPDATE` into a hash keyed `car-row-col`, then in-memory checks (「何十本と出ていたクエリが1本になると効果が高い」). Official adds: once fuzzy logic found seats they are known free → reserve directly (「わざわざ後ろに構えている指定席予約ロジックを通さず即予約でも問題ない」).
- when it works: reserve is 5 pts + 10 bonus (+10–25 for adjacent seats), and fuzzy scenarios run repeatedly (official: 「初期の曖昧検索では課題アプリケーションのレスポンスがどんどん遅くなり、タイムアウトで減点」).
- how to verify: slow log count of the per-seat `FOR UPDATE` select; 4xx rate on `/reserve` in alp (ARINE saw many 4xx → seats already taken).
- risk: lock scope = whole train (still coarse); official suggests per-car locks (「ロックは車両毎にかけ」). Double booking = critical / disqualification (「同一座席の二重発券などの重要なエラー」).
- evidence: https://github.com/shirokanezoo/isucon9f/commit/dac871db ("Fix seat_reservations select"); official 5.3.3 https://isucon.net/archives/53916974.html ; chy72 「あいまい検索で前から順に16車両分回すN+1も消す」「あいまい検索時に二重にvalidationしているのをやめる」; methane 「reservation の N+1 解消」
- regulation note: 「同一座席の二重発券」= fail.

### T: [F] `SKIP LOCKED` on the reserve selects — tried and reverted
- what: 白金動物園 replaced `FOR UPDATE` with `SKIP LOCKED` (MySQL 8) on the seat/reservation selects, then reverted within 40 min.
- when it works: not here — skipping locked rows means a concurrent reservation is invisible, which is exactly the double-booking race.
- evidence: https://github.com/shirokanezoo/isucon9f/commit/06b3503c , https://github.com/shirokanezoo/isucon9f/commit/429d90c2 , https://github.com/shirokanezoo/isucon9f/commit/f65761b8 ("Revert SKIP LOCKED")
- regulation note: as above.

### T: [F] Cancel: enqueue the payment cancellation and bulk-cancel via `POST /payment/_bulk` from a background worker within 5 s
- what: `POST /api/user/reservations/:id/cancel` → `redis.lpush('isutrain:cancel_queue', payment_id)` and return; a systemd service (`cancelbot.rb`) drains the list every 1 s and calls `/payment/_bulk`. Reference called `DELETE /payment/:id` synchronously (~1 s per request).
- when it works: manual: 「60秒間の負荷走行ののちに、5秒以上待ってから、整合性チェックを行います。このタイミングまでに、外部決済サービスのキャンセルなど処理の整合性をとってください。」 and payment_spec documents `/payment/_bulk` (「配列の途中に誤った決済IDがあると無視し」). Official: 「決済のキャンセルは5秒以内毎にバッチ処理でバルクキャンセルするという実装を行い、レスポンスを早く返すことができるようになります。」
- how to verify: alp shows cancel ≈1 s; `payment_spec.md` lists `_bulk`.
- risk: the queue must drain before finalcheck and survive restarts (use Redis/DB, not process memory). methane's team hit errors when they delayed cancels without bulking, then a nil-pointer in a rushed bulk implementation and failed; しゃかラン (PHP) deliberately skipped it (1 pt endpoint).
- evidence: 白金動物園 "cancelbot" + `systemd/isutrain-cancelbot.service`: https://github.com/shirokanezoo/isucon9f/commit/9972b64c ; official https://isucon.net/archives/53916974.html ; payment spec https://github.com/isucon/isucon9-final/blob/master/docs/payment_spec.md ; methane 本選 blog; netmark 「cancel APIのバルク化」(wanted, not done); chy72 「キャンセル処理 bulkする」
- regulation note: 「ジョブキュー機構の追加による遅延書き込み」explicitly allowed; data must persist after reboot.

### T: [F] Tune `available_days` in `/initialize` (10 → 20/25/30/50/70/100/366) to the load you can absorb
- what: more days = more users (monthly ramp, GW 4/29–5/6 and Olympics 7/24–8/9 add load). Raise gradually; each timeout costs 5 pts and the bench can always out-load you (366 max).
- when it works: after search/seats/reserve are fast. ARINE 20 days → 15,066 (special prize); しゃかラン 50 → 4,374, final 70; 白金動物園 20 → 50 (reverted) → 30 → 25 final; shugo 366 → 25 → 100 → 366; chy72 感想戦 100 on 1 app + 1 DB = 105,417.
- how to verify: timeout counts in the bench message; 白金動物園 「Available days を…20日に増やしてみる。即下げる。秒で怒られた」. Official: 「available_days の数値をいきなり大きくする場合はベンチマーカーからの負荷を捌ききれずにタイムアウトとなり、減点されていました」.
- risk: methane's team did not notice the knob until 17:20 (「/initialize で負荷を指定できることに気づいておらず（大ポカ）」) and the cancel backlog then exploded.
- evidence: https://github.com/shirokanezoo/isucon9f/commit/43d3ac41 , https://github.com/shirokanezoo/isucon9f/commit/d5d58981 , https://github.com/shirokanezoo/isucon9f/commit/78d7bc4d , https://github.com/shirokanezoo/isucon9f/commit/06c27f95 , https://github.com/shirokanezoo/isucon9f/commit/1df2dbd0 , https://github.com/shirokanezoo/isucon9f/commit/9fa8603d , https://github.com/shirokanezoo/isucon9f/commit/ee3a37ef ; ARINE https://labs.gree.jp/blog/2019/10/19561/ ; shmurakami https://shmurakami.hatenablog.com/entry/2019/10/10/163630 ; shugo commits https://github.com/shugo/isucon9_final/commits/master
- regulation note: manual 「10日から366日までが指定できます。予約可能な日数を広げるほど多くの予約リクエストが来るようになります。」

### T: [F] Bound request time with an app-level timeout (rack-timeout 4–5 s) so stuck requests don't hold threads
- what: `use Rack::Timeout, service_timeout: 5`; also `innodb_lock_wait_timeout` lowered (shugo: 1 s → 8 s).
- when it works: Ruby/puma thread pools under lock waits; 5 pts per timeout anyway, so better to free the worker.
- risk: a timed-out request mid-transaction must roll back (白金動物園 added `db_ensure_transaction_close` handling in the same commit).
- evidence: https://github.com/shirokanezoo/isucon9f/commit/aca07b6a , https://github.com/shirokanezoo/isucon9f/commit/32648da6 ; shugo "ロックタイムアウトを1秒に修正"/"8に修正": https://github.com/shugo/isucon9_final/commits/master
- regulation note: timeouts are 「1回毎に5点減点、失格にはならない」.

### T: [F] Take the app (and ideally MySQL) out of docker-compose; nginx → puma unix socket; two app hosts + one DB host
- what: hosts shipped with app+MySQL+payment mock in docker-compose behind host nginx; docker-proxy burned CPU and compose log buffering ate the 1 GB. 白金動物園 ran puma natively on isu1/isu2 (`upstream localapp { server unix:/run/isutrain/puma.sock }`, `upstream app { isu1:8888; isu2:8888 }`), MySQL on isu3.
- when it works: always, but budget the time: systemd units for everything, and test reboot early.
- risk: netmark: 「docker-compose のログ出力によるメモリ消費」; 「webappをdockerから剥がしたがゆえにMySQLが自動起動しない…自動起動対応が17:59までかかって」; matsuu 「剥がすなら中途半端にせず全部剥がすべき」. Final score = first bench after organiser reboot, so autostart failures are fatal.
- evidence: 白金動物園 nginx.conf https://github.com/shirokanezoo/isucon9f/blob/master/nginx.conf , "native ruby deployment" https://github.com/shirokanezoo/isucon9f/commit/15fc68cb ; netmark https://netmark.jp/2019/10/post-1485.html ; matsuu https://matsuu.hatenablog.com/entry/2019/10/06/145445 ; shmurakami "add multi server", "return b,c servers" https://github.com/shmurakami/isucon9_final/commits/master
- regulation note: 「サーバ再起動後にすべてのアプリケーションコードが正常動作する状態を維持すること」; 「最終スコアは、競技時間終了後、再起動後に主催者によって実行した最初のスコアを採用する」.

### T: [F] Browser check is real: keep the SPA working (index.html for all routes, `/api/settings`, search ≥10 rows)
- what: the post-contest check opens the site in a browser; ARINE (29,963, would have been 2nd) and 3 other teams were voided for browser-check NG.
- how to verify: open the UI after every big deploy; ARINE: 「webはトップが出るか出ないかくらいしか見ず…ブラウザでの挙動チェックで落ちた」.
- evidence: https://labs.gree.jp/blog/2019/10/19561/ ; score list https://isucon.net/archives/53882494.html
- regulation note: 「アプリケーションはブラウザ上での表示を初期状態と同様に保つ必要があります - 列車検索の結果件数が10件未満の場合も失格とします」.

### T: [F] (感想戦, post-contest) Where the ceiling was: N+1 + hard-coding beats clever locking
- what: chy72's replay reached 105,417 with available_days=100 on 1 app + 1 DB by (a) removing every N+1 in search/seats/reserve, (b) hard-coding the essential part of train_master (departure time / 6 station pairs / class), (c) memoising timetable lookups per key, (d) bulk cancel, (e) caching cookie decoding and reducing SHA-256 iterations 10→1 for login (unspecified by rules). He did not change the seat-assignment algorithm. Marked as post-contest evidence, not a winning-day commit.
- evidence: https://chy72.hatenablog.com/entry/2019/10/29/075831
- regulation note: 「パスワードの保存方法が変更されていないこと」(manual) — reducing hash iterations arguably changes the storage method; treat as risky.

---

## Techniques present in both rounds (for reuse)
- Static master data → memory/hard-code (categories ↔ station/fare/seat/train masters), with the 本選 caveat of 1 GB RAM.
- OR → UNION for index use (transactions ↔ segment overlap).
- Kill N+1 with one batched query + in-app map (users/categories ↔ seat_reservations/reservations).
- External-API cost moved off the request path (status from DB / parallel calls ↔ bulk cancel in background) — in both rounds the manual/spec contained the sentence that licensed it.
- Three-host split with nginx on host 1; Redis on the DB host (白金動物園 both rounds).
- Logs/profilers off for the final run; reboot test before 17:30.
- Observability: LTSV + alp/kataribe, `long_query_time=0`, request-ID/user-ID in logs.

---

## Regulation highlights — 予選 (ISUCARI)
- May change anything on the 3 instances (ecs.sn1ne.large, 2 vCPU/4 GB): 「利用が認められた競技用サーバーのみでアプリケーションの動作が可能であれば、どのような変更を加えても構いません」; allowed list: 複数台の役割変更 / スキーマ・インデックス / ミドルウェア変更 / キャッシュ・ジョブキューによる遅延書き込み / 他言語再実装 — https://isucon.net/archives/53555007.html , https://github.com/isucon/isucon9-qualify/blob/master/docs/manual.md
- Must not change: 「アクセス先のURI」「レスポンス(HTML)のDOM構造」「JavaScript/CSSファイルの内容」「画像および動画等のメディアファイルの内容」 — https://isucon.net/archives/53555007.html (bench md5-checks static/js, static/css, images, QR PNG).
- External resources: only the 3 instances; 「外部のメトリクス計測サービスの使用のみ特例として許可…スコアを向上させるいかなる効果も持つものであってはいけません」; bench shipment/payment mocks are IP-restricted.
- Reboot/persistence (追試): 「処理実施後に再起動が行われた場合、再起動前に行われた処理内容が再起動後に保存されている必要があります」; 「ブラウザ上での表示を初期状態と同様に保つ」; 「パスワードを平文で保存する」forbidden; `isucon_admin` account must remain.
- `/initialize` ≤ 20 s, must return `campaign` (0–4) and non-empty `language`; must reset state the checker needs.
- Item-list semantics: 「商品が出品・編集された場合は…即座に反映」「古いデータの削除、非表示はベンチマーク上で許可されません。各商品一覧取得APIが一度に返す商品数は初期実装と同じ状態を保つ」 but 「新着一覧については…よりユーザにあわせた商品の一覧を返すことで、購入の機会を増やすことができます」.
- Status codes: 「HTTPステータスコードは、基本的に参照実装と同一のものを想定」; 499 after the 60 s cut-off is not penalised.
- Ranking: 最後に提出したスコアがfailなら失格; ties broken by best score then earliest.

## Regulation highlights — 本選 (ISUTRAIN)
- Same allowed-list wording; explicitly forbidden: 「他のインスタンスに処理を委譲するなど」external resources during bench; 「他チームの競技用サーバや、決済代行サービスAPIにアクセスすること」(payment mock shared with bench — don't hit it during someone's run) — https://github.com/isucon/isucon9-final/blob/master/docs/MANUAL.md
- Fail conditions: 「静的ファイルの内容の変更」, `/initialize` > 20 s, 互換性チェック失敗 (「同一座席の二重発券などの重要なエラー」), checker failures.
- 追試: data persistence after reboot; browser display 「列車検索の結果件数が10件未満の場合も失格」; 「パスワードの保存方法が変更されていないこと」; 「OSのメモリサイズが提供時と同じ状態であること」(1 GB cap stays).
- Final score = 「競技時間終了後、再起動後に主催者によって実行した最初のスコア」(reboot → wait 10 min → bench; fail → one more reboot+bench).
- Consistency window: 「60秒間の負荷走行ののちに、5秒以上待ってから、整合性チェックを行います。このタイミングまでに、外部決済サービスのキャンセルなど処理の整合性をとってください」.
- `/initialize` returns `available_days` (10–366) + `language`; `/initialize` TRUNCATEs `seat_reservations`, `reservations`, `users` (webapp_spec.md).
- 「初期実装は遅すぎてスコアが出ません」「ロック機構がうまくなく、二重発券される場合がある」are declared known problems.

## Scoring model — 予選
- `取引が完了した商品（椅子）の価格の合計（ｲｽｺｲﾝ） - 減点 = スコア`; initial listing price 100; each successful 人気者出品 raises the listing price (`priceStoreCache.Add(20)` in campaign.go). Not request-count based. — https://github.com/isucon/isucon9-qualify/blob/master/docs/manual.md , https://github.com/isucon/isucon9-qualify/blob/master/bench/scenario/campaign.go
- Penalties: critical error (1 → fail; e.g. 多重決済, wrong payment amount); wrong status/content 500 ｲｽｺｲﾝ each, ≥10 → fail; timeouts: beyond 200, 5,000 per further 100, no fail; score ≤ 0 → fail.
- Bench: `/initialize` → verify → 60 s load (Check + 2 Load workers + `campaign` extra Load workers + Campaign goroutine: 500 buyer logins pooled, 人気者出品 every 8 s starting at 13 s with 80+20j concurrent buyers at 1000+100j price, 10 % using a failing card; executes only if `BuyerPool.Len() >= num+10`, i.e. slow logins silently cancel it) → 1 s wait → post-check. Load scenarios are rate-limited to one iteration per 3 s and interleave GETs between sell and buy (「MySQLを守るためにGETの速度を落とすチートが可能…それを防ぐ」). "Recommended" timelines (>80 % of first page in the buyer's root category) skip the 7-category browse and buy twice per loop. — bench/scenario/{scenario,load,campaign}.go, cmd/bench/main.go
- External mocks: shipment and payment both `SetDelay(800 ms)` on every call during the run; payment `FA10` card numbers always fail.
- Data: 50,000 items / 4,000 users / 43 categories; 90 % of items by 10 % of users; 30 % already transacted; each buyer buys exactly one root category. — https://isucon.net/archives/53789931.html

## Scoring model — 本選
- `エンドポイント毎の点数 * 成功リクエスト数 + ボーナス点 - 減点`; points: signup/login/logout/stations/commit/cancel/reservations(list,detail) 1, search 3, seats 3, reserve 5; bonus +10 per successful 仮予約, あいまい予約 adjacency +10/15/20/25 for 2/3/4/5 adjacent seats. — https://github.com/isucon/isucon9-final/blob/master/docs/MANUAL.md
- Penalties: critical → fail; wrong status/content −5 each, 1,000 → fail; timeout −5 each, no fail.
- Bench phases: pretest (immediate fail if spec not met: 検索0件, 静的ファイル変更, 初期データと異なる, 不正予約, 料金誤り) → 60 s benchmark (normal scenario, あいまい予約 scenario repeated, abnormal scenarios probing double booking / non-stop stations / invalid segments; load scales with `available_days` by month, GW and Olympics peaks) → finalcheck (payment API ledger vs reservations/fares; silent-success bugs fail here). — https://isucon.net/archives/53916974.html
- Special prize: first team past 15,000 (ARINE); final results after organiser reboot: 1 白金動物園 35,801 / 2 nil 29,704 / 3 はしもとせいこ 13,996 / 4 しゃかラン 13,016; ARINE 29,963 voided by browser check. — https://isucon.net/archives/53882494.html

## Notes on gaps
- nil's 本選 (2位) approach: no public repo/blog found (unverified).
- catatsuy's organiser Medium post on the bench/external services: not retrievable (403); its content is covered by the bench source read directly.
- tkuchiki/sugyan/sorah 本選 posts: not opened.