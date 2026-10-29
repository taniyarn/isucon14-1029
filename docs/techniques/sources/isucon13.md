# ISUCON13 (2023) "ISUPipe" — performance technique catalogue

Sources actually opened: official repo isucon/isucon13 (README, docs/cautionary_note.md, docs/isupipe.md, bench/ source, webapp/pdns zone, development/pdns schema, provisioning pdns.conf), isucon.net 講評 (58001272), 受賞チームおよび全チームスコア (57993937), レギュレーション (57768216), kazeburo's DNS 水責め post, repos narusejun/isucon13-final (1st), oribe1115/isucon13 (2nd), ultra-fast-gopher/isucon13 (3rd), saba-in-the-kettle/isucon13 (4th), shirokanezoo/isucon13 (18th), onk/isucon13, Saza-ku/isucon13, tatsuru/isucon13, tmokmss/isucon13-infra, and 参加記 by tohutohu, 織時屋 (trap.jp), sakamo, p1ass, sorah, mirakui, hogesako, tondol, nakario, agatan, onk, tmokmss, wtatsuru, hirosuzuki, onoe (MONOS), yyamada12, mackee/soudai, iceman. Not found publicly: repos/write-ups for 5th FetchDecodeExecWrite, 6th takonomura, 7th qt, 8th negainoido, 10th 第8西東京市 (searched; nothing indexed).

---

## Regulation highlights (this contest)

- 3 x c5.large (2 vCPU / 4 GiB), gp3 40 GB each; bench is 8 vCPU Fargate. Only these EC2 instances may be used for the contest; instance-type changes = 失格. — https://github.com/isucon/isucon13/blob/main/docs/cautionary_note.md , https://github.com/isucon/isucon13/blob/main/README.md
- Security group: may open new ports, but "SSH(TCP/22)、HTTPS(TCP/443) および DNS(UDP/53) については競技に影響があり変更しないでください". — cautionary_note.md
- DNS is part of the benchmark: "ポータルから負荷走行を実行した場合、指定したサーバー(IPアドレス)のDNSサーバー(53/UDP)に名前解決を行い、得られたIPアドレスに対してHTTPSでアクセスを行います。DNSサーバーが動作していない場合や、ポータルに登録されたサーバー3台のIPアドレス以外が名前解決の結果として得られた場合、ベンチマーカーが走行せず、エラーとなります。" — cautionary_note.md
- DNS semantics: UDP only, no TCP fallback; "Aレコードの問い合わせに対して、Aレコードのみを返すようにしてください。CNAMEの再帰問い合わせは行いません。"; round robin: first A record is used, no failover; resolve timeout 2 s, up to 5 retries before load but "負荷走行中はリトライを行いません"; "TTLを設定することでベンチマーカーは指定された秒数またはベンチマーカー終了のどちらか短い時間まで、結果のIPアドレスをキャッシュします。" — cautionary_note.md (confirmed in bench/internal/resolver/dns.go: LRU cache keyed by name, honoured when `Ttl > 0`).
- DNS 水責め: "負荷走行中、DNSサーバに対していわゆる「DNS水責め攻撃」が行われます。… ベンチマーカーはDNS水責め攻撃およびスクレイピングを行い、名前解決ができると HTTPS によりアクセスを試みる動作を行います。失敗(NXDOMAINや応答なし)では来ません。" — cautionary_note.md
- Icon rules (isupipe.md): bench sends `If-None-Match: "{icon_hashの値}"`; "一致する場合にはステータスコード 304 でレスポンスすることができます(MAY)。条件付きGETリクエストでない場合、ステータスコード 304 をレスポンスしてはいけません(MUST NOT)。… アイコン画像が更新され… 200でレスポンスし画像データを送信しなければなりません(MUST)。APIで返却される icon_hash の値と … 配信されるアイコン画像は、ユーザーのアイコン画像が更新された後、2秒以内に変更が反映されている必要があります(MUST)。" — https://github.com/isucon/isucon13/blob/main/docs/isupipe.md
- Passwords: "パスワードのハッシュアルゴリズムを変更したり、bcryptのコストを変更してはいけません(MUST NOT)。" — isupipe.md
- /api/initialize: 42 s timeout; must return `{"lang": ...}` (empty lang = fail); "サーバーサイドで処理の変更・データ構造の変更などを行う場合、この処理が行っている内容を漏れなく提供してください。" — cautionary_note.md
- Bench flow: initialize (42 s) → 整合性チェック → 負荷テスト 60 s → 最終チェック; pretest failure = immediate fail; requests cut at end of load do not count. — cautionary_note.md
- 追試 (reboot test): "全サーバーの再起動後に負荷走行を実施し再現スコアを計測"; fail if the re-run fails, if "再現スコアが最終スコアの75%以下", if envcheck fails; and for top teams "負荷走行実行時にアプリケーションに書き込まれたデータが、サーバー再起動後に取得できない場合". — cautionary_note.md. Result: 96 teams failed 追試 (89 of them "再起動後スコア0"). — https://isucon.net/archives/57993937.html
- Do not touch envcheck.service / aws-env-isucon-subdomain-address.service files, isuadmin user. — cautionary_note.md
- External resources: "主催者の指示以外で利用が認められたサーバー以外の外部リソースを使用する行為（他のインスタンスに処理を委譲するなど) は禁止"; monitoring/dev/AI tooling/external container registry allowed. — cautionary_note.md
- Regulation: may change "複数台あるサーバーの役割の変更", "データベーススキーマの変更やインデックスの作成・削除", "キャッシュ機構の追加、ジョブキュー機構の追加による遅延書き込み", "他の言語による再実装"; must keep URIs, response structure, JS/CSS/media content; "サーバ再起動後にすべてのアプリケーションコードが正常動作する状態", "ベンチマーク実行時にアプリケーションに書き込まれたデータは再起動後にも取得できること". — https://isucon.net/archives/57768216.html
- Sakura prize = most "名前解決成功数" in the final run among top 30 (so DNS success count is reported, but is not score). — cautionary_note.md
- Static media (media.xiii.isucon.dev HLS/thumbnails) is not a tuning target and does not affect scoring. — cautionary_note.md

## Scoring model (this contest)

- "スコアは一回の負荷走行中にISUPipeの投げ銭(Tip)機能により、送金・寄付された金額(ISUCON)の合計となります。" — cautionary_note.md. Implementation: `benchscore.AddTip(tip)` is called only after a successful (201, validated) `POST /api/livestream/:id/livecomment`; final score = `GetTotalProfit()`. — https://github.com/isucon/isucon13/blob/main/bench/isupipe/client_livecomment.go , https://github.com/isucon/isucon13/blob/main/bench/internal/benchscore/profit.go , https://github.com/isucon/isucon13/blob/main/bench/cmd/bench/bench.go
- Tip amount is a function of reservation length: level 5 if totalHours ≥ 20, 4 if ≥ 15, 3 if ≥ 10 (LongHourThreshold), 2 if ≥ 5, else 1; ranges 10–100 / 100–1000 / 1000–5000 / 5000–10000 / … scaled by progress through the stream. Comment in code: "より長い配信枠のほうが高いレベルになる. 予約が捌けているほど高いレベルになる. … webappがライブコメント投稿を捌けていないと高額にならない". A viewer posts one tipped comment per hour of the stream, and leaves on first failed comment post ("ライブコメントを配信に投稿できないため、視聴者が離脱します"). — https://github.com/isucon/isucon13/blob/main/bench/internal/scheduler/livecomment_scheduler.go , https://github.com/isucon/isucon13/blob/main/bench/scenario/viewer.go
- Official note: "ISUCON13のスコアは、リクエストを処理した回数ではなく、Tipsの投稿の合計です。… Tipsの投稿に辿り着くシナリオは限られており、HTTPリクエストの最適化がスコアに結びつきにくい場面もありました。" — https://isucon.net/archives/58001272.html
- Only 1 in 10 viewers calls livestream statistics ("本来のクライアントは全員statsを呼ぶが、重すぎるので調整"). Spammers run at 2x viewer weight; moderated spam is posted expecting 400. — bench/scenario/viewer.go, bench/cmd/bench/benchmarker.go
- Error classes: `benchmark-application` (減点), `benchmark-timeout` (減点), `benchmark-critical` (仕様違反, fail), `system`. — https://github.com/isucon/isucon13/blob/main/bench/internal/bencherror/error.go. Manual exception: the log line "viewer_spam: failed to post livecomment (moderated spam) … (expected:400, actual:201)" is "ベンチに問題となり、減点対象ではありません". — isupipe.md
- DNS attack: `rate.NewLimiter(3000, 1)` global cap; attack parallelism starts at 2, x1.5 every 2 s while DNS fail-rate < 1 % and resolves/parallel > 50, capped at 15 ("DNS水責め負荷が上昇します"); random labels 10–31 chars + "0", 1/30 queries use 2–3 labels; 1 in 5 *resolved* attack names triggers an HTTPS scrape of `/`, `favicon.ico`, `api/user/me`, `api/tag`, `api/livestream/search?limit=50`. Score is unaffected; "名前解決成功数" is only reported. — https://github.com/isucon/isucon13/blob/main/bench/internal/attacker/dns.go , https://github.com/isucon/isucon13/blob/main/bench/cmd/bench/benchmarker.go ; kazeburo: "ISUCON13のDNS水責め攻撃はスコアに影響をしない、負荷走行中のサブ要素となっています。… 最大 3000 QPS以上にならないようにしてありました。… 最大15万から18万クエリ程度が最大だったと思われます。" — https://kazeburo.hatenablog.com/entry/2023/12/02/235258
- Pretest DNS: resolves `pipe` and 10 default records (must succeed); for 3 random names, only "サーバーリストに含まれていません" is an error — NXDOMAIN or timeout is tolerated. — https://github.com/isucon/isucon13/blob/main/bench/scenario/core_pretest_dnsrecord.go
- Pretest icon: unset icon must report the NoImage.jpg hash; after POST /api/icon the bench sleeps `IconHashAppliedDelay = 2s` then checks `/api/user/me`, `GET icon` without ETag (body must match), with matching ETag ("200でも304でもどっちでもOK"), with non-matching ETag (body must match), and livecomment.user.icon_hash. — https://github.com/isucon/isucon13/blob/main/bench/scenario/core_pretest_normal.go
- Initial score ≈ 3,000; indexes alone ≈ 10,000 (official). Top: NaruseJun 468,006; 織時屋 243,852; ultra_fast_gopher 214,941; 釜中の鯖 187,577. — https://isucon.net/archives/57993937.html

---

### T: Add the deliberately removed indexes (app DB and isudns)
- what: Add the ~11 indexes the reference schema lacks (livestream_tags(livestream_id), (tag_id,livestream_id), livestreams(user_id), icons(user_id), livecomments(livestream_id), themes(user_id), reactions(livestream_id,created_at), livestream_viewers_history(user_id,livestream_id), ng_words(livestream_id,user_id), reservation_slots(end_at), livecomment_reports(livestream_id)) and `records(name)` in the isudns DB, which was intentionally commented out (`-- CREATE INDEX nametype_index ON records(name,type);`).
- when it works: slow log shows full scans on these WHERE/ORDER BY columns; isudns `records` is queried per DNS query by PowerDNS gmysql backend.
- how to verify mid-contest: `SHOW INDEX FROM records` in isudns (only PK, domain_id, ordername exist), pt-query-digest / performance_schema on both databases; official list in the 講評.
- risk: 織時屋 forgot to apply the isudns index to the 2nd server at the end (估計 10% loss); onk "isudns indexing overlooked"; くすサポ: "Indexes applied but not actually deployed".
- evidence: official 講評 index list and "初期スコアは…3,000点前後ですが、インデックスを付与していくことで10,000点程度まで上昇します" https://isucon.net/archives/58001272.html ; isudns schema https://github.com/isucon/isucon13/blob/main/development/pdns/20_powerdns_schema.sql ; kazeburo "recordsテーブルにおいて、name に対してあるべきインデックスがなく" https://kazeburo.hatenablog.com/entry/2023/12/02/235258 ; 織時屋 10:39 3,180→4,820 (livestream_tags), 11:05 →7,399 (icons) https://trap.jp/post/2049/ ; NaruseJun commits 6928326ac5 "INDEX", 72528a2c4f, b452c418d1, b5b14cb5a3 https://github.com/narusejun/isucon13-final/commit/6928326ac5 ; 釜中の鯖 "Added missing database indexes" +31,553 https://blog.p1ass.com/posts/isucon13/ ; 釜中の鯖 commit 2463233ae3 https://github.com/saba-in-the-kettle/isucon13/commit/2463233ae3
- regulation note: schema/index changes explicitly allowed (レギュレーション).

### T: Store icon hash at write time (column + trigger, or app-side SHA256 on POST /api/icon)
- what: Add `icons.hash`/`users.icon_hash`, compute SHA256 once on upload (or via MySQL BEFORE INSERT/UPDATE trigger `SET NEW.icon_hash = SHA2(NEW.image, 256)`), and use the pre-computed NoImage hash `d9f8294e9d895f81ce62e73dc7d5dff862a4fa40bd4e0fecf53f7526a8edcac0` for users without icons. Removes the SHA256-over-LONGBLOB in every fillUserResponse.
- when it works: `icon_hash` is embedded in every user/livestream/comment/reaction response; only POST /api/icon changes it; 10% of streamer scenarios change icon.
- how to verify mid-contest: pprof shows sha256 + `SELECT image FROM icons` in fillUserResponse; schema `icons.image LONGBLOB`; manual says 2 s propagation allowed.
- risk: 織時屋 used DELETE+INSERT for icon rows (race; should be UPDATE); initial data users have no icons so NoImage path must be right (pretest checks it); 釜中の鯖 hit a struct-embedding bug returning theme ID as user ID.
- evidence: official 講評 trigger SQL and "fillUserResponse関数…都度SHA256の計算を行なっているところを、こちらのカラムに変更することで負荷の削減" https://isucon.net/archives/58001272.html ; NaruseJun 0b6f3f2394 "画像挿入時にハッシュを計算する" https://github.com/narusejun/isucon13-final/commit/0b6f3f2394 ; ultra_fast_gopher af58087e78 "add icon_hash to user", 9b84c41af9 https://github.com/ultra-fast-gopher/isucon13/commit/9b84c41af9 ; 釜中の鯖 f29dfe5ba6 "icon hashを格納" https://github.com/saba-in-the-kettle/isucon13/commit/f29dfe5ba6 (+19,325 at 12:22 per blog) ; 織時屋 12:14 →12,967 "Stored icon hash in DB" https://trap.jp/post/2049/ ; くすサポ 15:11 →32,711 https://scrapbox.io/tondol-pub/ISUCON13%E3%81%AB%E3%81%8F%E3%81%99%E3%82%B5%E3%83%9DISUCON%E9%83%A8%E3%81%A8%E3%81%97%E3%81%A6%E5%8F%82%E5%8A%A0%E3%81%97%E3%80%819%E4%BD%8D%E3%81%AB%E3%81%AA%E3%82%8A%E3%81%BE%E3%81%97%E3%81%9F
- regulation note: icon_hash must reflect an update within 2 s (isupipe.md).

### T: Return 304 on `If-None-Match` for icons (compare with the quotes)
- what: In GET /api/user/:username/icon compare `If-None-Match` to `"` + icon_hash + `"` (the bench sends the value quoted) and return 304 with no body; keep sending ETag on 200.
- when it works: Manual explicitly permits 304; bench pretest accepts 200 or 304 for matching ETag; every streamer-moderate and report scenario fetches icons with ETag.
- how to verify mid-contest: access log shows GET …/icon with 200 + large bytes; `grep If-None-Match` in nginx log; isupipe.md section "アイコン画像配信についての特記事項".
- risk: Quote mismatch wasted time for several teams (NaruseJun 214345ec66 "Etagの比較がミスってる"; 百万円ドリブン "ETagの値をダブルクオートで囲まずにヘッダーに入れていた" ~1 h lost). 304 without If-None-Match is a bench internal error. nginx-only 304 is hard because the validator is a custom SHA256, so most teams still hit the app for conditional requests (sorah: "あくまでも微増に留まったのは if-none-match の処理で結局 app サーバープロセス (puma) の時間を取られてしまったから").
- evidence: bench sends `req.Header.Set("If-None-Match", `"`+o.eTag+`"`)` and rejects 304 without ETag https://github.com/isucon/isucon13/blob/main/bench/isupipe/client_user.go ; manual https://github.com/isucon/isucon13/blob/main/docs/isupipe.md ; 講評 ad-hoc `strings.Contains(match[0], iconHash)` + 2023.12.01 addendum https://isucon.net/archives/58001272.html ; NaruseJun ad9c1deb66, 214345ec66 https://github.com/narusejun/isucon13-final/commit/214345ec66 ; 織時屋 bda010f43a (accepts both quoted/unquoted) https://github.com/oribe1115/isucon13/commit/bda010f43a ; ultra_fast_gopher 957fbd45a2 https://github.com/ultra-fast-gopher/isucon13/commit/957fbd45a2 ; tmokmss →13,031 https://tmokmss.hatenablog.com/entry/isucon13 ; MONOS 16:13 →63,000 https://www.onoe.dev/blog/isucon13/ ; 百万円ドリブン https://nakario.hateblo.jp/entry/2023/12/04/002631 ; sorah https://diary.sorah.jp/2023/11/28/isucon13
- regulation note: MAY 304 only on conditional GET; MUST 200+body after update.

### T: Move icon bytes out of MySQL into files; serve via nginx (`alias` / `X-Accel-Redirect`, `open_file_cache`)
- what: On POST /api/icon write `<hash>.jpg` (or `<username>.jpg`) to disk and insert only the hash; GET icon returns `X-Accel-Redirect: /internal/img/<hash>.jpg` (or nginx `alias` directly, with `try_files` falling back to NoImage). 白金動物園: nginx serves the static file when there is no `If-None-Match`, otherwise `rewrite` to an app route that returns 304 or X-Accel-Redirect.
- when it works: `icons.image LONGBLOB` read on every GET; images only change via one POST; initial users have no icons so /initialize just needs to clear the directory (and optionally place NoImage copies).
- how to verify mid-contest: `SELECT COUNT(*) FROM icons` after initialize (0 rows for initial data); slow log / pprof shows `SELECT image`; nginx access log bytes.
- risk: Multi-host: the file must exist on the host that serves it — NaruseJun forwarded the upload body to S1 (`/api/internal/icon`) and later made S1 compute the hash ("hash計算を片方のみで行う"); on reboot the files persist but /initialize must recreate directory state; onk spent 2.5 h on this path.
- evidence: NaruseJun 43f758615c "iconをファイル化", b83dc75cd3 "iconをnginxから返す" (`location /internal/img/(.*) { internal; open_file_cache max=100; alias … }`), c465bce3d1 "s3で処理", 887ba5091a https://github.com/narusejun/isucon13-final/commit/b83dc75cd3 ; 釜中の鯖 9c08a103da "iconをnginxから配信" (`location ~ ^/api/user/(.+)/icon$ { internal; alias …/icons/$1.jpg; }`) https://github.com/saba-in-the-kettle/isucon13/commit/9c08a103da (+54,047 at 16:35 per blog) ; ultra_fast_gopher 3e5d20b0fa https://github.com/ultra-fast-gopher/isucon13/commit/3e5d20b0fa ; 白金動物園 3568cb8653 "nginx 304 icon" (`if ($http_if_none_match) { rewrite … }`) https://github.com/shirokanezoo/isucon13/commit/3568cb8653 , 12:34 →20,160 https://diary.sorah.jp/2023/11/28/isucon13 ; onk 66f1419a4a (copy NoImage on register) https://github.com/onk/isucon13/commit/66f1419a4a , https://onk.hatenablog.jp/entry/2023/11/30/082631 ; MONOS 13:15 →16,941 https://www.onoe.dev/blog/isucon13/
- regulation note: media content must be byte-identical; data written during bench must survive reboot (files on disk do).

### T: Per-username icon-hash cache in process memory (or memcached) invalidated on POST /api/icon
- what: Cache `username → icon_hash` so GET icon / fillUserResponse never hit MySQL; delete the entry on upload; reset on /initialize.
- when it works: icon_hash only changes on one endpoint; 2-s grace.
- how to verify mid-contest: hit count of `SELECT hash FROM icons WHERE user_id=?` in slow log after indexes.
- risk: With app on multiple hosts the invalidation must reach all hosts (NaruseJun routed `/api/icon` and `/api/user/*/icon` to the same host via nginx; later fanned out). tmokmss found caches on server 3 were not reset on /initialize 10 min before the end.
- evidence: NaruseJun 067a8b9789 "iconのhashをcache", 84bb3e3445 "transaction取らなくてよかった" (`userNameIconCache`) https://github.com/narusejun/isucon13-final/commit/84bb3e3445 ; 織時屋 11731cf6c3 "cache user icons" + `iconHashCache` (motoki317/sc) https://github.com/oribe1115/isucon13/commit/bda010f43a ; MONOS 2085d9ec3c "iconhash memcache" https://github.com/Saza-ku/isucon13/commit/2085d9ec3c ; tmokmss https://tmokmss.hatenablog.com/entry/isucon13
- regulation note: 2-s propagation (isupipe.md).

### T: Cache `tags` whole-table in memory (never written after /initialize)
- what: Load all tags at startup and on /initialize into id→Tag and name→Tag maps; GET /api/tag and fillLivestreamResponse / search-by-tag read from memory.
- when it works: "他のクエリやコードを見てもtagsへの書き込みは存在しない" (tohutohu); no INSERT INTO tags anywhere in webapp.
- how to verify mid-contest: `grep -n "INTO tags" webapp/`; pprof/slow log shows `SELECT * FROM tags WHERE id = ?` N+1 inside fillLivestreamResponse.
- risk: none observed; must re-load on /initialize (NaruseJun resetTagCache in initialize and main).
- evidence: NaruseJun b584ed2c34 "tagキャッシュ" https://github.com/narusejun/isucon13-final/commit/b584ed2c34 and blog https://zenn.dev/tohutohu/articles/923bdf5dcd73af ; ultra_fast_gopher 793a63b9a2/b6e8e455da "Cache tags" https://github.com/ultra-fast-gopher/isucon13/commit/b6e8e455da ; onk 0c00d0cee7 "tagsをキャッシュにつっこんだ" (Redis) https://github.com/onk/isucon13/commit/0c00d0cee7 ; くすサポ 17:30 "GET /api/tag cached in-memory" https://scrapbox.io/tondol-pub/ISUCON13%E3%81%AB%E3%81%8F%E3%81%99%E3%82%B5%E3%83%9DISUCON%E9%83%A8%E3%81%A8%E3%81%97%E3%81%A6%E5%8F%82%E5%8A%A0%E3%81%97%E3%80%819%E4%BD%8D%E3%81%AB%E3%81%AA%E3%82%8A%E3%81%BE%E3%81%97%E3%81%9F
- regulation note: caching allowed; must be rebuilt by /initialize.

### T: Cache livestream→tags (set at reservation only) or fetch with a single IN/JOIN
- what: Replace the per-livestream `SELECT * FROM livestream_tags` + per-tag `SELECT * FROM tags WHERE id=?` with `SELECT * FROM tags WHERE id IN (SELECT tag_id FROM livestream_tags WHERE livestream_id=?)` cached per livestream_id (invalidated in reserveLivestreamHandler), and in search-by-tag use `JOIN livestream_tags`.
- when it works: livestream_tags are written only in POST /api/livestream/reservation.
- how to verify mid-contest: write-path grep; pprof fillLivestreamResponse.
- risk: 都営三田線 FAILED by moving the livestream and livestream_tags INSERTs out of the transaction while caching livestreams: "livestream はあるのに livestream_tags がない、という状況が瞬間的に発生しており、その刹那を観測されると正しくないデータが返ってしまう状態" — the cache then held the tag-less object with no invalidation (227,377 → fail).
- evidence: NaruseJun d0afb50f8f "livestreamTags キャッシュ", 92dcc35cc0 (JOIN for tag search) https://github.com/narusejun/isucon13-final/commit/d0afb50f8f ; 織時屋 aaeef0f826 "Cache livestream tags", 557d42a19c "fix N+1 in search livestreams" (15:42 →171,977) https://github.com/oribe1115/isucon13/commit/557d42a19c ; 釜中の鯖 de6a0ab405 "tagのN＋1改善" ; 都営三田線 https://blog.agatan.dev/posts/isucon13/
- regulation note: consistency is checked (benchmark-critical), fails lose everything.

### T: In-memory cache of users / themes / livestreams (immutable-after-create rows)
- what: `getUser(id)`, `userIDByName`, `getTheme(userID)`, `getLivestream(id)` served from sync.Map / cmap / sc caches; filled lazily or on initialize; invalidated only on the few writers (register, POST /api/icon for the full User response).
- when it works: users change only via register/icon; themes only on register; livestreams only on reservation (no UPDATE/DELETE); these are read in every fill*Response.
- how to verify mid-contest: grep UPDATE/DELETE on those tables in webapp; pprof shows `SELECT * FROM users WHERE id = ?` dominating.
- risk: multi-host invalidation (see /initialize fan-out); 都営三田線 failure above; 白金動物園 deliberately kept caching minimal/TTL to avoid bugs; 織時屋 regret "Slow query had JOIN using cached data but team didn't apply cache".
- evidence: NaruseJun defefdc026 "UserModel キャッシュ", 43e6ba6eaf "cache livestream" https://github.com/narusejun/isucon13-final/commit/43e6ba6eaf ; blog 12:00–13:00 →76,951 (UserModel cache + DB split), 16:00–17:00 →277,310 (livestream + ngword cache) https://zenn.dev/tohutohu/articles/923bdf5dcd73af ; 織時屋 55a135c7d5 "cache theme" (13:11 →48,494), 560cd20d4c "Cache users" (13:27 →63,968) https://github.com/oribe1115/isucon13/commit/560cd20d4c ; ultra_fast_gopher 90927e1d0e "Cache user", e5c47f3fb1 "Cache livestreams", 54970afb9f "name cache" ; tmokmss →34,985 https://tmokmss.hatenablog.com/entry/isucon13 ; 都営三田線 17:30 →227,377 then FAIL https://blog.agatan.dev/posts/isucon13/
- regulation note: cache allowed; reboot test re-runs bench so caches rebuilt from DB are fine.

### T: NG-word spam check in the application (strings.Contains) instead of SQL LIKE per word
- what: In POST livecomment, load the livestream's ng_words (cached per livestream_id) and check `strings.Contains(comment, word)` in Go instead of executing `SELECT COUNT(*) FROM (SELECT ? AS text) … LIKE CONCAT('%',?,'%')` once per NG word.
- when it works: the reference SQL is a constant-table trick executed N times per comment; spammers are 2x viewers so this is on the hottest write path; the tipped comment is the scoring request.
- how to verify mid-contest: slow log top by count shows the `(SELECT ? AS text) AS texts` query; code in postLivecommentHandler.
- risk: Case sensitivity: MySQL utf8mb4_bin LIKE is case-sensitive, so a naive `strings.ToLower` changed behaviour (ultra_fast_gopher had to fix "toLowerのロジックを半角英字のみに修正" / "Fix errors for mismatched case"). Bench expects 400 for moderated spam; with cross-host caches the 201 "一般エラー" appears.
- evidence: NaruseJun 75bc8f8a6e "NGワードチェック" https://github.com/narusejun/isucon13-final/commit/75bc8f8a6e ; 釜中の鯖 3dbc63253d/ffada77c4e "hitspamの判定をGoで" (15:35, +25,261) https://blog.p1ass.com/posts/isucon13/ ; 白金動物園 7a7fb07005 "Simple NG Words" https://github.com/shirokanezoo/isucon13/commit/7a7fb07005 ; ultra_fast_gopher c9a8b0f8eb https://github.com/ultra-fast-gopher/isucon13/commit/c9a8b0f8eb ; 織時屋 15:05 →81,037 https://trap.jp/post/2049/
- regulation note: response (400 vs 201) must match; manual lists the 201/400 log as not penalised.

### T: Moderation delete: one `DELETE … WHERE livestream_id=? AND comment LIKE '%word%'` for the newly added word only
- what: Replace the reference loop (for every NG word of the stream: `SELECT * FROM livecomments` (all streams!) then one DELETE per comment with the LIKE sub-select) with a single DELETE scoped to the livestream and the word just added; cache ng_words per livestream and drop the cache entry after commit.
- when it works: previously registered words have already purged older comments; only the new word can hit existing rows; AggressiveStreamerModerate inserts many dummy NG words, bloating ng_words ("ng_wordsテーブルが嵩む要因になる").
- how to verify mid-contest: slow log shows `SELECT * FROM livecomments` with huge rows_examined from moderateHandler; count of ng_words growing per run.
- risk: Counter caches must be decremented for deleted tips (白金動物園 did `UPDATE users SET total_tips = total_tips - ?`); 釜中の鯖 reverted a first attempt (0cf4285943) before landing it; NaruseJun added `time.Sleep(500ms)` after commit (blog: tuned to 1 s) to avoid the moderated-spam 201 error.
- evidence: NaruseJun 64054e39b6 "追加するNGワードのみを削除", f6c0e572c8 "cache ngwords", 92dcc35cc0 (sleep) https://github.com/narusejun/isucon13-final/commit/64054e39b6 ; blog "NGワードのスパム判定処理をアプリで行うようにした", 15:00–16:00 →204,805 "Optimized NG word deletion" https://zenn.dev/tohutohu/articles/923bdf5dcd73af ; ultra_fast_gopher 4e58fabd37 "delete livecomments" https://github.com/ultra-fast-gopher/isucon13/commit/4e58fabd37 ; 白金動物園 7a7fb07005 ("過去の NG ワードはスキャン不要") ; onk eb0bcfbeb3 "remove NG word loop." (16:20–16:35) https://onk.hatenablog.jp/entry/2023/11/30/082631 ; bench AggressiveStreamerModerateScenario comment https://github.com/isucon/isucon13/blob/main/bench/scenario/streamer.go ; 織時屋 11:01 →5,607 "Fixed N+1 in moderateHandler" https://trap.jp/post/2049/
- regulation note: deletion semantics must be preserved (bench checks moderated comments disappear/400).

### T: Statistics/ranking with one GROUP BY per metric instead of per-user/per-livestream loops
- what: User ranking: `SELECT u.id, COUNT(*) … GROUP BY u.id` for reactions and `SELECT u.id, IFNULL(SUM(l2.tip),0) … GROUP BY u.id` for tips, merge in memory, sort. Livestream ranking likewise `GROUP BY l.id`. Drop needless JOINs (`SELECT COUNT(*) FROM livecomment_reports WHERE livestream_id=?` instead of joining livestreams).
- when it works: stats handlers rank *all* users/livestreams on every call; only 1/10 viewers call livestream stats, but each call is O(N) queries.
- how to verify mid-contest: pprof/alp shows /api/user/:name/statistics and /api/livestream/:id/statistics with high total time; code loops over `SELECT * FROM users`.
- risk: tie-break ordering must match (score then name/id); onk's Redis stats attempt and mackee's "User statistics full Redis caching: Failed due to bugs"; 釜中の鯖 saw the biggest single jump when completed (+143,119 → 17:09).
- evidence: NaruseJun d1ebb3b830/cdac789a66 "N+1破壊", 6a00b83ee5 "JOINを除去", e8b672b6e9 https://github.com/narusejun/isucon13-final/commit/d1ebb3b830 ; 釜中の鯖 8c062f6cbf "143,119 stats改善" https://github.com/saba-in-the-kettle/isucon13/commit/8c062f6cbf , blog https://blog.p1ass.com/posts/isucon13/ ; ultra_fast_gopher e657174d61 "Remove n+1 for user stats handler" ; 織時屋 232e54ae36 "improve N+1 in live stats" ; 講評 "ユーザおよびライブ配信の統計情報 … N+1となっているクエリを1回のクエリ書き直したり、あらかじめ必要な計算を行なっておくテーブルを用意" https://isucon.net/archives/58001272.html ; mackee https://mackee.hatenablog.com/entry/2023/11/29/235454
- regulation note: none beyond response equality.

### T: singleflight / short TTL for the global ranking computation
- what: Wrap the all-users ranking build in `singleflight.Group` so concurrent stats requests share one computation (NaruseJun); 織時屋's `sc` cache gives similar dedup with TTL.
- when it works: ranking is global (same for every caller) and expensive; many concurrent callers.
- how to verify mid-contest: alp shows many concurrent stats calls; the ranking query set is identical across requests.
- risk: freshness — bench compares rank after its own posts; keep TTL ≈ 0 (singleflight only) unless you know the 猶予; mackee: "Singleflight in-memory caching: No score improvement" when applied without the N+1 fix first; くすサポ regret: not knowing singleflight.
- evidence: NaruseJun dbb481aa42 "userRankingの生成をsingleflight化" https://github.com/narusejun/isucon13-final/commit/dbb481aa42 ; 織時屋 `sc.NewMust(getTheme, 90*time.Second, 90*time.Second)` https://github.com/oribe1115/isucon13/commit/55a135c7d5 ; tondol https://scrapbox.io/tondol-pub/ISUCON13%E3%81%AB%E3%81%8F%E3%81%99%E3%82%B5%E3%83%9DISUCON%E9%83%A8%E3%81%A8%E3%81%97%E3%81%A6%E5%8F%82%E5%8A%A0%E3%81%97%E3%80%819%E4%BD%8D%E3%81%AB%E3%81%AA%E3%82%8A%E3%81%BE%E3%81%97%E3%81%9F ; mackee https://mackee.hatenablog.com/entry/2023/11/29/235454
- regulation note: none.

### T: Counter-cache columns for score/tips/reactions (or Redis ZSET leaderboard)
- what: Add `users.total_tips`, `users.total_reactions`, `users.score` (and livestream equivalents), update on POST livecomment/reaction and decrement on moderation delete; index `(score, name)` for ranking; alternatively maintain a Redis ZSET per leaderboard.
- when it works: the app owns every write path for tips/reactions; ranking reads dominate.
- how to verify mid-contest: all writers are postLivecommentHandler, postReactionHandler, moderateHandler; /initialize can recompute from initial data.
- risk: must survive reboot (DB columns do; Redis needs persistence/initialize rebuild); every delete path must adjust counters; onk's Redis approach was merged at 17:45 and the team did not reach the N+1 fixes.
- evidence: 白金動物園 d85152fda8 "care score", 281578fbba `alter table users add index idx1 (score, name)` https://github.com/shirokanezoo/isucon13/commit/d85152fda8 , sorah 13:41 →50,089 https://diary.sorah.jp/2023/11/28/isucon13 ; onk 08ad63ac6f "Livestreamのランク集計をredisでactiveにやる" https://github.com/onk/isucon13/commit/08ad63ac6f ; 都営三田線 "Pre-computed statistics stored in database" https://blog.agatan.dev/posts/isucon13/
- regulation note: data written during bench must be readable after reboot (top-team 追試 item).

### T: Kill fill*Response N+1 via JOIN / preload / passing the parent object once
- what: In getLivecomments/getReactions/search, fetch the livestream (with owner+theme) once and pass it to each fillXResponse; preload users with `WHERE id IN (...)`; 織時屋 wrote a single JOIN selecting `livestream.*, owner.*, owner.theme.*` into nested structs.
- when it works: official note lists fillLivecommentResponse, fillLivecommentReportResponse, fillLivestreamResponse, fillReactionResponse, fillUserResponse as the N+1 sources.
- how to verify mid-contest: pprof call graph; slow log by count (`SELECT * FROM livestreams WHERE id = ?`, `SELECT * FROM users WHERE id = ?`).
- risk: Empty-slice vs null JSON (`fillLivestreamResponseのN+1でEmptyのときはEmptyな配列にするようにした`); onk ran out of time on reaction/livecomment N+1 and reverted.
- evidence: 講評 https://isucon.net/archives/58001272.html ; NaruseJun e8b672b6e9 "livestreamの計算を外だし" https://github.com/narusejun/isucon13-final/commit/e8b672b6e9 ; 織時屋 560cd20d4c (JOIN query) , 12:55 →17,274, 14:26 →80,108 https://trap.jp/post/2049/ ; ultra_fast_gopher 8e0eb04544/8947e75e0d https://github.com/ultra-fast-gopher/isucon13/commit/8947e75e0d ; 釜中の鯖 11:02 +7,188, 11:39 +10,034, 13:29 +24,412 https://blog.p1ass.com/posts/isucon13/ ; 白金動物園 ecb8fde97b, ed2be4eab2 https://github.com/shirokanezoo/isucon13/commit/ecb8fde97b ; onk https://onk.hatenablog.jp/entry/2023/11/30/082631
- regulation note: response structure must be identical.

### T: Reservation slots: index + stop re-selecting each slot, use `start_at` range only
- what: `SELECT * FROM reservation_slots WHERE start_at >= ? AND end_at <= ? FOR UPDATE` then per-slot re-select was N+1; use the already-fetched `slot.Slot`, add index on `(start_at,end_at)` or `end_at`; 織時屋 switched both SELECT and UPDATE to `start_at >= ? AND start_at < ?` with `idx_start_at`.
- when it works: reservation is the gate to long streams, which earn the highest tip levels ("予約が捌けているほど高いレベルになる"); slots table has 8,759 hourly rows.
- how to verify mid-contest: slow log on reservation_slots; bench config NumSlots=5, LongHourThreshold=10.
- risk: FOR UPDATE lock range must still prevent overbooking; 白金動物園 briefly tried to reject short reservations ("Block short live" ≤2 h → 400, then "iyagarase sleep", then reverted "no iyagarase") — that is a spec violation route, don't.
- evidence: 織時屋 2d712ec7e1 "select slot", c7b4c2718d "start_atだけ使う" (16:52 →207,292, 17:17 →225,969) https://github.com/oribe1115/isucon13/commit/c7b4c2718d ; 釜中の鯖 f115001a29, 61066483da "slotのN+1改善" https://github.com/saba-in-the-kettle/isucon13/commit/61066483da ; MONOS 5587435ded "slot改善" ; 講評 index `reservation_slots (end_at)` ; bench https://github.com/isucon/isucon13/blob/main/bench/internal/config/reservation.go , tip levels https://github.com/isucon/isucon13/blob/main/bench/internal/scheduler/livecomment_scheduler.go ; 白金動物園 f3f3ecdb26, eac306323e, 9dbd9bf248 https://github.com/shirokanezoo/isucon13/commit/f3f3ecdb26
- regulation note: overbooking prevention must hold (consistency check).

### T: Set DNS TTL > 0 so the bench caches names (zone file, especially `pipe`)
- what: Change the zone template TTLs from 0 to 60–180 s (NaruseJun 60, 織時屋 180, 百万円ドリブン 120); the bench LRU-caches A records for `Ttl` seconds, eliminating most legitimate lookups.
- when it works: Manual: "TTLを設定することでベンチマーカーは指定された秒数…結果のIPアドレスをキャッシュします"; bench honours `ans.Header().Ttl > 0`; `pipe.u.isucon.dev` carries most traffic.
- how to verify mid-contest: `dig pipe.u.isucon.dev @127.0.0.1` shows TTL 0; zone file webapp/pdns/u.isucon.dev.zone; pdns.conf has all cache TTLs = 0.
- risk: kazeburo: with DNS round-robin load balancing a long TTL defeats the distribution; records added via `pdnsutil add-record … "0"` in registerHandler also need a TTL.
- evidence: cautionary_note.md ; bench resolver https://github.com/isucon/isucon13/blob/main/bench/internal/resolver/dns.go ; official zone (all 0) https://github.com/isucon/isucon13/blob/main/webapp/pdns/u.isucon.dev.zone ; NaruseJun 63a6fdc0b7 zone `$TTL 60` https://github.com/narusejun/isucon13-final/commit/63a6fdc0b7 ; 織時屋 b320146382 "TTLを3分で設定" https://github.com/oribe1115/isucon13/commit/b320146382 ; 百万円ドリブン "DNS TTL=120" (12:00 →11,908) https://nakario.hateblo.jp/entry/2023/12/04/002631 ; kazeburo https://kazeburo.hatenablog.com/entry/2023/12/02/235258
- regulation note: A-record only, first IP used, no failover.

### T: PowerDNS tuning: enable query caches, index `records.name`, move isudns MySQL to another host
- what: Set `query-cache-ttl`, `negquery-cache-ttl`, `cache-ttl` from 0 to positive values in /etc/powerdns/pdns.conf; add the records(name[,type]) index; point `gmysql-host` at a separate MySQL (pdns.d/gmysql-host.conf).
- when it works: PowerDNS gmysql issues SQL per query; water-torture names are random so only the negative cache + index help; 3,000 QPS cap.
- how to verify mid-contest: `top` shows pdns + mysqld CPU during bench; pdns.conf shows `cache-ttl=0 negquery-cache-ttl=0 query-cache-ttl=0`; isudns slow log.
- risk: Negative cache does not help random names beyond repeats; teams that only did this still saw PowerDNS "かなり重く" (NaruseJun moved on to in-app DNS).
- evidence: official pdns.conf https://github.com/isucon/isucon13/blob/main/provisioning/ansible/roles/powerdns/files/pdns.conf ; 講評 "PowerDNSのキャッシュ機能を有効にする / データベースに不足しているインデックスを付与する / アプリケーションのデータベースと分離する" https://isucon.net/archives/58001272.html ; kazeburo "negquery-cache-ttl=0 query-cache-ttl=0 あたりは設定しておいても良さそうです" ; NaruseJun 6e35cc3f49 "DNS" (`gmysql-host=192.168.0.13`) https://github.com/narusejun/isucon13-final/commit/6e35cc3f49 ; 百万円ドリブン "DNS-specific indexing" 12:40 →20,100 https://nakario.hateblo.jp/entry/2023/12/04/002631 ; tmokmss pdns.conf `gmysql-host=isu3`, `local-port=10053` https://github.com/tmokmss/isucon13-infra/blob/main/pdns.conf ; MONOS 5dd2207f38 "separate pdns mysql to isucon2"
- regulation note: PowerDNS may be replaced/reconfigured (middleware change allowed).

### T: Static zone backend (PowerDNS `launch=bind` or nsd) with reload on register
- what: Switch PowerDNS to the BIND zone-file backend (`launch=bind`, `bind-config=named.conf`) and regenerate the zone on /initialize, appending a line + `pdns_control bind-reload-now u.isucon.dev` on register; 白金動物園 used nsd + `nsd-control reload` with `flock` on the zone file.
- when it works: lookups become in-memory; records only change on register; kazeburo: "さくらのクラウドではDNS水責め攻撃の対策として、MySQL backendからBIND zone file backendに切り替えています".
- how to verify mid-contest: pdns CPU is in MySQL; register rate (POST /api/register) vs reload latency.
- risk: 白金動物園's main bottleneck: "Zone file writes required sequential flock operations and nsd-control reload waits (0.1-0.2s latency)", limiting register throughput and thus concurrent users; 織時屋 hit "shared library" errors trying SQLite backend; iceman FAILED at the end because DNS was in "謎の状態" after a self reboot (rcode=3 on a random name… actually a required name) — know the manual's zone reset procedure (`pdnsutil delete-zone`, rm lock file, reboot).
- evidence: wtatsuru pdns.conf/init_zone.sh/named.conf https://github.com/tatsuru/isucon13/blob/main/etc/powerdns/pdns.conf , https://github.com/tatsuru/isucon13/blob/main/webapp/pdns/init_zone.sh , blog 17:43 →27,053 (178,412 DNS queries) https://wtatsuru.hatenadiary.com/entry/2023/12/03/232650 ; 白金動物園 e6ead89cfa "nsd" https://github.com/shirokanezoo/isucon13/commit/e6ead89cfa , sorah https://diary.sorah.jp/2023/11/28/isucon13 , mirakui "ユーザ作成のリクエストをあまり捌けておらず" https://blog.mirakui.com/entry/2023/11/27/152107 ; 織時屋 post-mortem "BIND backend + pdns_control reload would have been optimal" https://trap.jp/post/2049/ ; iceman https://zenn.dev/iceman/articles/5aad04f18b128f ; kazeburo https://kazeburo.hatenablog.com/entry/2023/12/02/235258
- regulation note: records must persist across reboot (zone file on disk + reload at boot).

### T: Replace PowerDNS with an in-process DNS server (miekg/dns) answering from the users set
- what: Start a UDP :53 server inside the Go app; keep a set of registered usernames (seeded from initial zone + `SELECT name FROM users`, updated in registerHandler); answer A for known `<name>.u.isucon.dev.` with the webapp IP and NS for the apex; cache parsed RRs; stop pdns.
- when it works: all three top Go teams did this; register is the only writer; answer must be one of the 3 registered IPs; NS/SOA questions only for apex.
- how to verify mid-contest: `pdnsutil add-record` exec in registerHandler is the write path; manual's DNS rules; pretest checks `pipe` + 10 default names.
- risk: Reboot/retry: usernames must be rebuilt on boot (ultra_fast_gopher persisted `records.txt` every 5 s with atomic rename; 釜中の鯖 rebuilt from the users table); NaruseJun's `dns.NewRR` per answer was hot ("NewRRの負荷を落とす" → RR cache); 釜中の鯖 had to fix cache clearing on /initialize ("fix dns cache"); the DNS answer IP is hard-coded (NaruseJun 54.178.156.176) — env it. Port 53 needs root/capability.
- evidence: NaruseJun a413e4d123 "dns go impl", 54088c355f, 3ddd5d0273 "サブドメイン" (removes pdnsutil), 6c49ef1643 "zone initしない" https://github.com/narusejun/isucon13-final/commit/a413e4d123 ; blog "DNSをアプリに統合" https://zenn.dev/tohutohu/articles/923bdf5dcd73af ; ultra_fast_gopher bb331f6ff4 "in-memory dns server", 99f47d48d1 "Save DNS with safe" https://github.com/ultra-fast-gopher/isucon13/commit/99f47d48d1 ; 釜中の鯖 eadf47b2a5 "cache in memory", 7f3ff9feb2 https://github.com/saba-in-the-kettle/isucon13/commit/eadf47b2a5 , blog 14:28 "Replaced PowerDNS with app-based DNS (miekg/dns)" +26,685 https://blog.p1ass.com/posts/isucon13/ ; 講評 "DNSサーバを実装し、ユーザ名がDBになければゆっくりレスポンスをする、あるいはレスポンスをしない" https://isucon.net/archives/58001272.html
- regulation note: UDP/53 must answer during bench; retries not performed during load; NXDOMAIN/no-answer for random names is tolerated by pretest.

### T: Drop or delay water-torture queries (NXDOMAIN) — in-app `return` or dnsdist rules
- what: For names not in the user set, send no response (NaruseJun `return` before `WriteMsg`; ultra_fast_gopher "存在しないレコードの場合は何も返さない") or use dnsdist in front: `DelayResponseAction(1000)` on NXDOMAIN, `DropAction` for `QNameLabelsCountRule(0,4)`, `QPSPoolAction(2,"abuse")` for long names, `MaxQPSIPRule(...) + DelayAction(750)`.
- when it works: Manual: "失敗(NXDOMAINや応答なし)では来ません"; attack parallelism only grows while fail-rate < 1 % and ≥ 50 resolves/s per worker, so slow/absent answers stop the ramp-up and the scraping HTTP traffic; attacker labels are ≥ 11 chars.
- how to verify mid-contest: bench log "DNS水責め負荷が上昇します"; "名前解決成功数" in result (tmokmss: 50,938 → 6,050 after dnsdist); pdns CPU.
- risk: Real usernames must still resolve (the bench's own usernames are short/known; drop-by-length rules must not catch them); dnsdist needs pdns moved to another port; 都営三田線's dnsdist Rate Limit tuning gave 32,588 → 77,601 but team later failed for unrelated reasons; DNS success count is the Sakura prize criterion only.
- evidence: NaruseJun 81229e16e6 "水攻め、無視" https://github.com/narusejun/isucon13-final/commit/81229e16e6 ; ultra_fast_gopher 99f47d48d1 ; 白金動物園 dnsdist.conf + nsd.conf 49aa528b88 https://github.com/shirokanezoo/isucon13/commit/49aa528b88 , sorah "Dropped packets with ≥5 QName labels; rate-limited (2 qps) queries >33 bytes" https://diary.sorah.jp/2023/11/28/isucon13 ; tmokmss dnsdist.conf `addAction(MaxQPSIPRule(100, 32, 64, 100, 60), DelayAction(750))` https://github.com/tmokmss/isucon13-infra/blob/main/dnsdist.conf , →15,982 https://tmokmss.hatenablog.com/entry/isucon13 ; 都営三田線 14:19 →32,588, 15:24 →77,601 https://blog.agatan.dev/posts/isucon13/ ; kazeburo dnsdist example `addResponseAction(RCodeRule(DNSRCode.NXDOMAIN), DelayResponseAction(1000))` https://kazeburo.hatenablog.com/entry/2023/12/02/235258 ; bench coordinator https://github.com/isucon/isucon13/blob/main/bench/cmd/bench/benchmarker.go
- regulation note: cautionary_note.md DNS section; pretest tolerates failures for unknown names.

### T: (Anti-pattern) Wildcard `*.u.isucon.dev` record
- what: Register a wildcard A record so register never touches DNS.
- when it works: only if the app can absorb the scraping HTTP that follows every resolved attack name (1 in 5 resolved names → GET /, /api/tag, /api/livestream/search?limit=50 …).
- how to verify mid-contest: see attacker endpoints list in bench; nginx log will show random-subdomain Hosts.
- risk: 講評/kazeburo: "ワイルドカードがあると水責め攻撃の名前解決が成功し、スクレイピングを模したHTTPリクエストが増える … かえって負荷になる"; 織時屋 "feared increased HTTP load" and skipped it.
- evidence: https://isucon.net/archives/58001272.html ; https://kazeburo.hatenablog.com/entry/2023/12/02/235258 ; bench attacker https://github.com/isucon/isucon13/blob/main/bench/internal/attacker/dns.go ; https://trap.jp/post/2049/
- regulation note: allowed, but TLS cert covers `*.u.isucon.dev` so scraped requests reach nginx.

### T: Multi-host layout driven by the DNS answer (bench target ≠ app host) and nginx routing of write-side endpoints
- what: Point the portal at the DNS host, have DNS answer the nginx host's IP; split isupipe MySQL to its own host; keep DNS (+ its DB) on the nginx host or a third host; later route `/api/register`, `/api/icon`, `/api/user/*/icon`, `/api/login` to the host that owns DNS state / icon files via nginx `location` → `upstream sub`, and everything else to `upstream main` on the third host.
- when it works: DNS 3,000 QPS + scraping is CPU that can be isolated; register must update DNS state locally (pdnsutil / in-app set); icon files live on one host.
- how to verify mid-contest: 講評 suggested layouts "DNS と DNSのDB / nginxとアプリケーション / アプリケーションのDB" then "nginx、DNS、DNSのDB / アプリケーション / アプリケーションのDB"; `top` per host.
- risk: Each host's caches need invalidation (see next); 釜中の鯖 +65,663 when DNS isolated; くすサポ jumped 72,957 → 128,660 just by making the bench target the PowerDNS host (isu2) so the app host stopped seeing DNS load; iceman saw −10,000 when routing everything to one app host; 白金動物園 ran 2 app hosts with nginx weights.
- evidence: NaruseJun 0a80cd74a7, 79c879ca0d "分散", 09df17cc83 "LoginをS1で処理" https://github.com/narusejun/isucon13-final/commit/79c879ca0d , blog layouts S1 nginx+app(DNS, register, icon) / S2 MySQL / S3 app https://zenn.dev/tohutohu/articles/923bdf5dcd73af ; 織時屋 19b3db9ae6 "/api/registerはdnsサーバーで受ける", 91fdf6694e "s1をnginxとdnsのサーバーにした", 1c069565e7 (15:37 →160,819) https://github.com/oribe1115/isucon13/commit/19b3db9ae6 ; 釜中の鯖 16:53 "DNS endpoints isolated on separate instance" https://blog.p1ass.com/posts/isucon13/ ; tondol 16:54 →128,660 https://scrapbox.io/tondol-pub/ISUCON13%E3%81%AB%E3%81%8F%E3%81%99%E3%82%B5%E3%83%9DISUCON%E9%83%A8%E3%81%A8%E3%81%97%E3%81%A6%E5%8F%82%E5%8A%A0%E3%81%97%E3%80%819%E4%BD%8D%E3%81%AB%E3%81%AA%E3%82%8A%E3%81%BE%E3%81%97%E3%81%9F ; yyamada12 `/api/register_dns` on the DNS host https://yyamada12.hatenablog.com/entry/2023/11/28/224745 ; 白金動物園 beee8fa832 "app=isu1,isu2,isu3 rproxy=isu1 dns=isu1 db=isu3", e38eb1cd02 "weight 2:3:1" ; 講評 https://isucon.net/archives/58001272.html
- regulation note: all 3 servers must be registered in the portal; DNS may only return registered IPs.

### T: /initialize fan-out to clear per-host caches (`/api/initialize/slave`)
- what: The host receiving POST /api/initialize runs init.sh once, then calls an internal endpoint on every app host (errgroup) that resets in-memory caches and reloads tags/DNS sets.
- when it works: any in-memory cache + >1 app host; 42-s initialize budget.
- how to verify mid-contest: stale data errors in pretest after a redeploy; tmokmss's near-miss ("in-memory caches on server 3 weren't reset during POST /initialize").
- risk: Must also be idempotent on reboot (process start reload); 織時屋 "masterAPPでだけDB初期化", "s1→s3でinitializeを飛ばすようにした"; 釜中の鯖 "initializeも一台目".
- evidence: NaruseJun 79c879ca0d (`initializeSlaveHandler`) https://github.com/narusejun/isucon13-final/commit/79c879ca0d ; 織時屋 3184caae1d, e512fee90a https://github.com/oribe1115/isucon13/commit/3184caae1d ; 釜中の鯖 cbf4923df0, 99592c09a8 "clea cache init" https://github.com/saba-in-the-kettle/isucon13/commit/cbf4923df0 ; tmokmss https://tmokmss.hatenablog.com/entry/isucon13
- regulation note: initialize must restore everything the bench assumes (cautionary_note.md).

### T: Offload bcrypt to a less-loaded host via internal HTTP
- what: ultra_fast_gopher added `/api/bcrypt/sum` and `/api/bcrypt/compair` on node 3 and had register/login call them, moving bcrypt CPU off the main app host.
- when it works: bcrypt cost is fixed by the rules; register/login scale with user count; another host has idle CPU.
- how to verify mid-contest: pprof shows `bcrypt.GenerateFromPassword` / `CompareHashAndPassword`.
- risk: Extra hop latency; must stay inside the 3 servers (external delegation forbidden).
- evidence: ultra_fast_gopher 967f35bfc8 "bcryptを分けた" https://github.com/ultra-fast-gopher/isucon13/commit/967f35bfc8 , blog "Relocated bcrypt operations to less-constrained servers" https://blog.sakamo.dev/post/isucon13/ ; NaruseJun routed /api/login and /api/register to S1 (09df17cc83)
- regulation note: "bcryptのコストを変更してはいけません(MUST NOT)"; cross-instance delegation only among the team's 3 servers.

### T: Make register fast and non-blocking on DNS (async pdnsutil / in-app set), because register gates concurrency
- what: Run the DNS record registration concurrently with the bcrypt+INSERT (goroutine + WaitGroup), or replace `pdnsutil add-record` (fork/exec + MySQL) with an in-memory set insert; never serialize register behind a zone reload.
- when it works: bench guarantees only NumMustTryLogins=10 initial logins; all viewer/streamer scenarios start with register/login, so register throughput bounds everything.
- how to verify mid-contest: alp on POST /api/register latency; `exec.Command("pdnsutil", …)` in registerHandler.
- risk: 白金動物園's post-mortem: "The POST /api/register endpoint bottleneck limited total concurrent users" (flock + nsd-control reload); mirakui "ユーザ作成のリクエストをあまり捌けておらず … アクセスログでは検出困難"; iceman: "小さいpayload 高頻度のリクエスト … ユーザ登録 … を見逃した".
- evidence: 織時屋 87793e38ba "リクエストを並列に投げる" https://github.com/oribe1115/isucon13/commit/87793e38ba ; NaruseJun 3ddd5d0273 https://github.com/narusejun/isucon13-final/commit/3ddd5d0273 ; 白金動物園 https://diary.sorah.jp/2023/11/28/isucon13 , https://blog.mirakui.com/entry/2023/11/27/152107 ; bench `NumMustTryLogins` https://github.com/isucon/isucon13/blob/main/bench/internal/config/benchmark.go ; iceman https://zenn.dev/iceman/articles/5aad04f18b128f
- regulation note: DNS record must resolve for new users during the run.

### T: Remove needless transactions (read-only handlers, single-statement writes)
- what: Drop `BeginTxx`/`Rollback` around pure reads (getIconHandler, stats, search) and single INSERTs; keep FOR UPDATE for reservation slots.
- when it works: Reference wraps every handler in a tx; MySQL connection pool contention (initial `SetMaxOpenConns(10)`).
- how to verify mid-contest: code grep `BeginTxx`; `SHOW PROCESSLIST` waits.
- risk: onk reverted a wholesale "remove trx" PR (38b251e5e1) — keep the ones protecting multi-row invariants (reservation; livestream+tags, cf. 都営三田線 fail).
- evidence: NaruseJun 84bb3e3445 "transaction取らなくてよかった" https://github.com/narusejun/isucon13-final/commit/84bb3e3445 ; 織時屋 cfc0795153 "remove all transactions", b1acc59d14 https://github.com/oribe1115/isucon13/commit/cfc0795153 ; ultra_fast_gopher 3fc4a873ca "remove tx from getIconHandler" ; 白金動物園 d7cd2f41b2 "Remove transacrion" ; onk 38b251e5e1 revert https://github.com/onk/isucon13/commit/38b251e5e1 ; 都営三田線 https://blog.agatan.dev/posts/isucon13/
- regulation note: consistency checks.

### T: DB driver/pool settings: `interpolateParams=true`, larger pool
- what: Set `InterpolateParams=true` (avoid ADMIN PREPARE round-trips) and raise `SetMaxOpenConns`/`SetMaxIdleConns` (10 → 50–64).
- when it works: slow log/pt-query-digest shows `ADMIN PREPARE` entries; many short queries.
- how to verify mid-contest: `grep -c PREPARE` slow log; connectDB in main.go has `db.SetMaxOpenConns(10)`.
- risk: none reported.
- evidence: NaruseJun 5506b1d367 https://github.com/narusejun/isucon13-final/commit/5506b1d367 ; onk 4965f6113d "ADMIN PREPARE が出ないように interpolateParams=true に" https://github.com/onk/isucon13/commit/4965f6113d ; ultra_fast_gopher fa67b9e704 "setMaxOpens 10->50" ; MONOS 43668d2803 ; yyamada12 "ADMIN PREPARE Mitigation" https://yyamada12.hatenablog.com/entry/2023/11/28/224745 ; 百万円ドリブン 11:40 →9,257 https://nakario.hateblo.jp/entry/2023/12/04/002631
- regulation note: none.

### T: Faster JSON serializer (go-json-experiment / sonic / goccy) and `sync.Pool` for request bodies
- what: Replace echo's encoding/json serializer with go-json-experiment (`json.MarshalWrite`/`UnmarshalRead`), bytedance/sonic, or goccy/go-json; pool request buffers.
- when it works: responses are large nested JSON (livestream → owner → theme → tags) and pprof shows encoding.
- how to verify mid-contest: pprof top shows `encoding/json`.
- risk: Field-name/case differences (ultra_fast_gopher "fix json"); go-json-experiment is unstable API.
- evidence: NaruseJun acf7f354d7 "json" https://github.com/narusejun/isucon13-final/commit/acf7f354d7 , 8c61b4b5af "bufをpool" ; 織時屋 2522e64184 "use faster json encoding library" (sonic) https://github.com/oribe1115/isucon13/commit/2522e64184 ; 釜中の鯖 b08a27c879 ; tondol "JSON library switched to go-json" ; ultra_fast_gopher https://blog.sakamo.dev/post/isucon13/
- regulation note: JSON structure must be unchanged.

### T: Turn off all logging/profiling for the final run; raise GOGC
- what: Disable echo debug/logger middleware, nginx access_log, MySQL slow/general log, pprotein/pprof integration; set `GOGC=2000`; drop binlog.
- when it works: everything; several teams saw +10–30 % at the end.
- how to verify mid-contest: log file growth during bench; pprof shows runtime GC.
- risk: Forgetting to re-enable for profiling; hirosuzuki's team lost ~half the score from "Nginx KeepAlive有効化" (upstream keepalive misconfig) — test infra changes one at a time.
- evidence: NaruseJun f51d1030fc "logoff", final hour 277,310 → 468,006 "Disabled application access logs and pprof logging" https://zenn.dev/tohutohu/articles/923bdf5dcd73af ; 釜中の鯖 b08a27c879 `GOGC=2000`, 3e42cae2f6, 17:43 +186,373 "Removed logging and telemetry overhead" https://blog.p1ass.com/posts/isucon13/ ; 織時屋 fecec6aec0 "log off" ; ultra_fast_gopher a7f89a8f78, 0961806113 "Stop pprotein" ; MONOS 1eb0648398/9890fccb26 ; hirosuzuki https://zenn.dev/hirosuzuki/articles/b87b58fed1bfc9
- regulation note: none.

### T: MySQL server tuning (flush/doublewrite/binlog) — but measure `innodb_buffer_pool_size`
- what: `innodb_flush_log_at_trx_commit=0|2`, `innodb_flush_method=O_DIRECT`, `disable-log-bin`, `innodb_doublewrite=0`, `innodb_log_writer_threads=off`, `skip-name-resolve`, `performance_schema=OFF`.
- when it works: write-heavy livecomments/reactions; 4 GiB hosts.
- how to verify mid-contest: iostat/`SHOW ENGINE INNODB STATUS` log waits.
- risk: NaruseJun: "MySQLはinnodb_buffer_pool_sizeの設定を追加すると何故かスコアがかなり下がってしまった" — on 4 GiB with app co-located, a big pool can hurt; durability settings are acceptable only because 追試 reboots cleanly after the run (data written during bench must still be readable — fsync-per-second is a small risk window).
- evidence: NaruseJun 92dcc35cc0 mysqld.cnf https://github.com/narusejun/isucon13-final/commit/92dcc35cc0 , blog https://zenn.dev/tohutohu/articles/923bdf5dcd73af ; 釜中の鯖 74655ce0a4 "mysql tuning" https://github.com/saba-in-the-kettle/isucon13/commit/74655ce0a4 ; tmokmss settings list https://tmokmss.hatenablog.com/entry/isucon13
- regulation note: data must survive reboot (top-team 追試 item).

### T: Go PGO build from the last good benchmark's pprof profile
- what: Download the CPU profile collected by pprotein for the previous run and build with `-pgo=pgo.pb.gz` in the deploy script.
- when it works: Go ≥ 1.21, CPU-bound app, stable hot paths.
- how to verify mid-contest: pprof available via pprotein; Makefile `download-pgo`.
- risk: profile from a run with different code; build time.
- evidence: NaruseJun f3fe3145a1 "pgo download" https://github.com/narusejun/isucon13-final/commit/f3fe3145a1 , common/deploy.sh https://github.com/narusejun/isucon13-final/blob/master/common/deploy.sh , blog "pprofのプロファイル結果をダウンロードできるようにしたので、それを引っ張ってきてGoのPGOでビルド" https://zenn.dev/tohutohu/articles/923bdf5dcd73af
- regulation note: none.

### T: nginx upstream keepalive, HTTP/2, X-Accel internal locations
- what: `upstream … keepalive 256; keepalive_requests 1000000;` with `proxy_http_version 1.1; proxy_set_header Connection "";`, `listen 443 ssl http2`, `add_header Etag $upstream_http_etag` when proxying icon routes.
- when it works: bench uses isucandar agents with 5-s idle timeout; many short requests over TLS.
- how to verify mid-contest: nginx error log "worker_connections are not enough" (bench comment), TIME_WAIT counts.
- risk: hirosuzuki: enabling keepalive dropped 67,000 → ~30,000 until reverted (likely worker/connection limits); the bench itself notes worker_connection errors when idle timeout unset.
- evidence: NaruseJun 79c879ca0d, 92dcc35cc0 (http2) https://github.com/narusejun/isucon13-final/commit/92dcc35cc0 ; 釜中の鯖 ee8c2ab522 "tune nginx", 90d9aa66ef ; 白金動物園 70213935a0 "http2" ; bench config comment on ClientIdleConnTimeout https://github.com/isucon/isucon13/blob/main/bench/internal/config/benchmark.go ; hirosuzuki https://zenn.dev/hirosuzuki/articles/b87b58fed1bfc9
- regulation note: none.

### T: Post-moderation settle delay to keep "expected 400 actual 201" errors away
- what: After committing a new NG word, `time.Sleep(500ms–1s)` before responding (and only then drop the ng_words cache), so that by the time the bench posts the now-moderated spam, every host/cache rejects it with 400.
- when it works: cross-host app with per-host ng_words caches; bench posts moderated spam immediately after Moderate returns.
- how to verify mid-contest: bench log "viewer_spam: failed to post livecomment (moderated spam) … expected:400, actual:201".
- risk: Adds latency to moderate (not a scoring request); the manual says this specific error is not deducted, so the value is mainly avoiding viewer dropout noise — treat as low priority.
- evidence: NaruseJun 92dcc35cc0, f6c0e572c8 https://github.com/narusejun/isucon13-final/commit/f6c0e572c8 ; blog "NGワード追加時のsleepを1秒に調整" (17:00–18:00) https://zenn.dev/tohutohu/articles/923bdf5dcd73af ; manual "この「一般エラー」はベンチに問題となり、減点対象ではありません" https://github.com/isucon/isucon13/blob/main/docs/isupipe.md ; tondol unresolved same error https://scrapbox.io/tondol-pub/ISUCON13%E3%81%AB%E3%81%8F%E3%81%99%E3%82%B5%E3%83%9DISUCON%E9%83%A8%E3%81%A8%E3%81%97%E3%81%A6%E5%8F%82%E5%8A%A0%E3%81%97%E3%80%819%E4%BD%8D%E3%81%AB%E3%81%AA%E3%82%8A%E3%81%BE%E3%81%97%E3%81%9F
- regulation note: moderated spam must get 400 (viewer scenario asserts it).

### T: Fold `themes` (and icon hash) into `users` to make fillUserResponse a single row read
- what: Add `users.dark_mode` / `users.icon_hash` columns (白金動物園 did both in Ruby; onk's regret), so user/owner responses need no join.
- when it works: themes has one row per user, written only at register; icon hash written only at POST /api/icon.
- how to verify mid-contest: schema `themes(user_id, dark_mode)`; fillUserResponse does 2 extra queries per user.
- risk: /initialize must migrate initial data (ALTER + UPDATE from icons/themes within 42 s; initial users have no icons so just set NoImage hash).
- evidence: 白金動物園 3020e7ac96 "theme: dark_mode is embedded in users" https://github.com/shirokanezoo/isucon13/commit/3020e7ac96 , sorah "Merged icons and themes tables into users" https://diary.sorah.jp/2023/11/28/isucon13 ; ultra_fast_gopher ac94aa6d80 "initial_users_column.sql" + `users.icon_hash` https://github.com/ultra-fast-gopher/isucon13/commit/af58087e78 ; onk regret https://onk.hatenablog.jp/entry/2023/11/30/082631 ; iceman stored hash in users https://zenn.dev/iceman/articles/5aad04f18b128f
- regulation note: schema changes allowed.

### T: Verify reboot safety early (self 再起動 test) — DNS state is the usual casualty
- what: Reboot all three hosts mid-afternoon, confirm pdns/custom DNS, MySQL, app start in the right order and that `/api/initialize` + pretest pass; know `pdnsutil delete-zone u.isucon.dev; rm /opt/aws-env-isucon-subdomain-address.sh.lock; reboot`.
- when it works: always — 96 teams failed 追試 (89 with score 0 after reboot).
- how to verify mid-contest: `sudo reboot` on all 3, then `dig pipe.u.isucon.dev @<dns-host>` and a bench run; systemd `enable` for custom services (ultra_fast_gopher "add service file", 釜中の鯖 "add service file").
- risk: iceman: reboot at T-15 min broke DNS ("DNSが謎の状態に陥ってエラーとなり、解決できずそのままFailしました"); 釜中の鯖 "wait db" style ordering; mineo "再起動後スコア0".
- evidence: cautionary_note.md 追試 + DNS reset procedure https://github.com/isucon/isucon13/blob/main/docs/cautionary_note.md ; results https://isucon.net/archives/57993937.html ; iceman https://zenn.dev/iceman/articles/5aad04f18b128f ; 釜中の鯖 8b47821d4d "add service file", MONOS 1076f00612 "wait db" https://github.com/Saza-ku/isucon13/commit/1076f00612 ; wtatsuru 17:55 "Restart testing validation → 28,232" https://wtatsuru.hatenadiary.com/entry/2023/12/03/232650
- regulation note: fail if re-run fails or < 75 % of final score.

---

## Observations for the kit

- Order that worked for the top 4: indexes → icon hash column + 304 → N+1 in fill*Response → tags/users/livestreams in memory → NG-word logic in Go + single DELETE → stats GROUP BY → DNS off the app host (then custom DNS) → icons to nginx → logs/pprof off. NaruseJun's largest single jump (277k → 468k) was turning logging/pprof off and tuning the moderate sleep; 釜中の鯖's was stats N+1 (+143k).
- DNS: do not chase "名前解決成功数"; it is not score. Isolate DNS first (host split + TTL + index), then decide between bind/nsd zone file (register path must not block) and in-app miekg/dns (persist usernames for reboot).
- The two documented FAILs among strong teams were cache/transaction consistency (都営三田線) and DNS state after reboot (iceman), not bench verification of 304.
- Unverified: whether `TooManySpamThresholdPercentage` (30 %) is actually used anywhere in the bench (constant exists in config but no reference found in viewer.go/benchmarker.go/visit_page.go).
---

## 練習 2026-10-18（ISUPipe、svc2 からベンチ）で分かったこと — 原本: isucon13-20261028/docs/retro/20261018-practice-feedback.md

- 効いた順: ① index と N+1（search 4.7 s → 49 ms、moderate 6 s → 70 ms）、② icon の hash を列に持たせて `If-None-Match` 一致で 304（30,177 → 80,068。画像の読み出し 14,620 回 → 68 回）、③ ユーザー情報を users・themes・icons の 1 クエリで組み立て、icon GET は tx を張らない（→ 101,604）。
- `SHA2(image,256)` を SQL で計算するのは駄目: 一時的に DB の SQL 時間の 36% を食った。hash は `POST /api/icon` で Go が計算して列に保存。initialize は icons を TRUNCATE するので古い hash は残らない。
- DNS: svc1 の 2 コアのうち約 0.9 コアを DNS が使っていた（ローカル mysqld 66% + pdns_server 26%、毎秒約 1,700 件の `records` 引き）。svc1 の mysqld の SQL は 99.9% が PowerDNS。本番の筋は「DNS は svc2（pdns + isudns）、app は svc1、DB は svc3」。`gmysql-host` を変え、`pdnsutil` が書く isudns も寄せる。pdns は DB を待って再起動し続ける drop-in が要る。
- `zone-metadata-cache-ttl=0` を 60 にすると `domainmetadata` 引き 72k 回が消える。`negquery-cache-ttl=0` は残す（登録直後の名前が引けなくなるのを避ける）。
- register の `pdnsutil add-record` を isudns への INSERT に置き換えると平均 517 → 58 ms（追記型なので同じ行があれば INSERT しない。name は小文字 FQDN、ttl/prio/disabled/auth を合わせる）。TTL は zone と register の両方で 120 に（効果未計測）。
- moderate の書き換えは照合順序に注意: 元はプレースホルダ同士を接続の照合順序（utf8mb4_general_ci、大文字小文字を区別しない）で LIKE 比較していた。DELETE … EXISTS に書き換えるときは `COLLATE utf8mb4_general_ci` を明示。
- app の DB を svc3 に移して `zz-perf.cnf` を入れると `/initialize` は 15.2 s → 2.5 s。
- スコアを生む視聴者のループは「GET livecomment → POST livecomment → GET reaction → POST reaction」、POST の間隔は中央値 97 ms、各リクエスト 11〜16 ms で SQL はその 1 割（svc1 の CPU の奪い合いが主因と推測）。
- `/api/initialize` の応答は参考実装では `{"language":"golang"}`（マニュアルの `lang` と違うが参考実装のまま）。
