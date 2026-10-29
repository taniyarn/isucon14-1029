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
