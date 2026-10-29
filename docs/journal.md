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
