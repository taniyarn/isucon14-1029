# journal

時刻 | branch / PR | 何を | なぜ（report の行） | 結果（score）

22:31 | docs/infra-app2 / #5 | [fast] docs: app ×2 の下準備（infra-app2.md） | — | merge 7401c2c、OK: fast（事後レビュー）
22:34 | setup/app-map / PR #? / #7 | app-map・annotations・app-early | app-analysis（巡回 svc2） | merge dbeaa4b、OK: fast（事後レビュー）
22:36 | baseline 20261029-133401 | main 95a0766（deploy なし） | score 761 | DB CPU-bound: mysqld 168%/200%, app 17%
22:40 | integ/setup / #1 | parallel-setup: measurement patch + free wins (B+D) | — | merge 339bb13、OK: 盤 22:39
22:40 | setup/db-split / #4 | parallel-setup: app DB -> svc3, matcher on svc1 only (A) | — | merge e4269dc、OK: 盤 22:39
22:40 | integ/early / PR #? / #6 | early-indexes 6本 + getChairStats / appGetNearbyChairs のN+1を1クエリに | コードのfile:line（baseline前、ルール1の例外） | merge 4f1ee9a、OK: 盤 22:39
22:40 | fix/20261029-133401/ops / #9 | [fast] mysql: thread_cache_size 256（ops グループ、20261029-133401） | — | merge 7dc280a、OK: fast（事後レビュー）
22:40 | docs/config-options / #10 | [fast] docs: 構成の案 3 つ（config-options、20261029-133401） | — | merge c6f8ff1、OK: fast（事後レビュー）
22:43 | docs-app-next / #12 | [fast] docs: app-next | — | merge f2730c3、OK: fast（事後レビュー）
22:45 | docs/infra-tuning / #13 | [fast] docs: etc/ だけで済むチューニング候補（infra-tuning.md） | — | merge a49251b、OK: fast（事後レビュー）
22:45 | scan/133401 / #14 | [fast] docs: technique-scan 20261029-133401 (early-scan included) | — | merge adb083c、OK: fast（事後レビュー）
22:52 | bench 2 20261029-134509 | main f2a76bc（#1 #4 #6 #9 #10） | score 8071（761 → 10.6 倍） | CPU is not the limit (idle 52%)
22:47 | infra/unit-wants / #8 | [fast] systemd: isuride-go を Requires=mysql → Wants=mysql（DB→svc3 後の再起動対策） | — | merge cc5ffdf、OK: 盤 22:46
22:47 | integ/20261029-133401 #? / #11 | index 3本 + nearby位置をJOIN + retry_after_ms app 200 / chair 100 + ownerGetChairsの窓を絞る | report 20261029-133401 P1 ride_statuses 24%+12%+8%、owner/chairs 30%、通知ポーリング | merge 3d82a87、OK: 盤 22:46
22:55 | effect 20261029-134509 | #4 db-split 効いた（svc1 mysqld 168%→1%、svc3 192%）/ #1 free wins 効いた / #9 thread_cache 効いた / #6 index・N+1 効いた（nearby 3947→112ms）
22:50 | docs-chair-distance / #18 | [fast] docs: design chair-distance (T7) | — | merge 31406cb、OK: fast（事後レビュー）
23:01 | 人の判断 | t25 T7 chair_distances の設計（#18）OK（盤）/ t24 #16 #15 #17 OK（盤）
23:02 | bench 3 20261029-134923 | main 2c9736a（#8 #11） | score 9225（8071 → +14%） | CPU is not the limit (idle 63%)
22:52 | integ/20261029-134509 #? / #16 | coupons を主キーで引く + getChairStats の ORDER BY を外す | report 20261029-134509 coupons used_by 10%・getChairStats 4% | merge 42d53f9、OK: 盤 22:51
22:52 | early/matching / PR #? / #15 | matching を待ち全件×空き椅子の到着順に + matcher 100 ms・app 直叩き | bench.log 不満100%がマッチ待ち、matcher 実測 1,182 ms（summary 133401） | merge 2ba6f9c、OK: 盤 22:51
22:52 | early/payment-idem / PR #? / #17 | 決済に Idempotency-Key（ride id）+ WithoutCancel、失敗時は再送 | 守り: 切断で決済済み+ロールバック → 次回の二重請求（クリティカル）の芽 | merge 5cef54c、OK: 盤 22:51
23:04 | effect 20261029-134923 | #11 owner 窓の絞り込み 効いた（owner/chairs 3767→281ms）/ retry_after 効いた（notification -36%）/ ただし pass=false（マッチング遅れ ×2）
23:12 | bench 4 20261029-135253 | main fe8a21d（#16 #15 #17） | score 28768 pass=true（9225 FAIL → 3.1 倍） | 特別賞の 88%、CPU is not the limit (idle 53%)
22:56 | impl/chair-distance #? / #19 | owner/chairs の総距離を要約テーブル chair_distances に（coordinate で加算、init.sh で作り直し） | report 20261029-134509 owner/chairs SQL 54%（#11 後 avg 281ms） | merge 06481fe、OK: 盤 22:55
22:58 | revert-19 / #21 | [fast] revert #19 (smoke: /api/initialize 500) | — | merge 44ee5fb、OK: fast（事後レビュー）
23:18 | 人の判断 | t31 特別賞はベンチ 5 の後に計測 OFF（#2 → 1 ベンチ → #3）（盤）/ #19 は smoke で initialize 500 → #21 で revert（ec64bd8）
23:00 | docs-ride-status / #23 | [fast] docs: design ride status column | — | merge bd8fc56、OK: fast（事後レビュー）
23:24 | bench 5 20261029-135903 | main ec64bd8（#19 の revert #21） | score 29637 pass=true（28768 → +3%） | 新しい 5xx: POST /api/app/users deadlock ×3 / 人の判断 t33 #22 OK・t34 rides.status 設計 OK（盤）/ effect: #15 matching 効いた（マッチ待ち不満 100%→48%、完了 96→382）、#17 問題なし
