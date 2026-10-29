# claims — 誰がどのファイル・関数を変えているか（衝突の予防）

エージェント（とセッション）が**書き始める前に**自分の行を足す宣言表。
このファイルだけは main に直接 commit してよい（CLAUDE.md ルール 4 の例外）。1 回の commit で触るのはこのファイルだけ。

## 手順（書き始める前に必ず）

1. `git fetch -q && git show origin/main:docs/claims.md` を読む（自分の worktree の古いコピーではなく origin/main）。
2. 自分が触る予定のファイル・関数と、`status = open` の行を突き合わせる。
   - **同じ関数** → 衝突。書き始めずにオーケストレーター（人）に報告する。順番を決めるか、片方にまとめる。
   - **同じファイルの別の関数** → 進めてよい。自分の行の「files / functions」に `(same file as <branch>)` と書く。
   - 重ならない → 進める。
3. 自分の行を足して、このファイルだけを main に push する。作業ブランチの上では commit しない（`HEAD:main` で作業の commit まで main に乗る）。origin/main の使い捨て worktree でやる:
   ```
   git fetch -q origin && git worktree add -q --detach ../wt-claims-<name> origin/main
   cd ../wt-claims-<name>      # docs/claims.md の表に 1 行足す
   git commit -q -m "claims: <branch> open" -- docs/claims.md
   git push origin HEAD:main || { git pull -q --rebase origin main && git push origin HEAD:main; }
   cd - && git worktree remove ../wt-claims-<name>
   ```
   rebase で表が衝突したら（同時に誰かが足した）、両方の行を残して `git add docs/claims.md && git rebase --continue` → push。足した後にもう一度手順 2 を見る（同時に同じ関数を宣言していないか）。
4. merge されたら `merged`、やめたら `abandoned` に変え、`updated` を書き換えて同じやり方で push する。触るファイル・関数が増えたら、その時点で行を直す（増えた分も手順 2 で確かめる）。

layer: `SQL only`（クエリと index だけ。ハンドラの流れは変えない）/ `logic`（ハンドラの処理、キャッシュ、tx）/ `config`（`etc/`、`deploy.sh`、`main.go` の設定行）/ `schema`（テーブル追加・列追加、`webapp/sql/`）。`schema` と `logic` は同じテーブルを読む他の行とも重なりやすいので、tables も書く。

練習（2026-10-18、ISUCON14）の実例: PR #15（ridecache）と PR #16（notification を 1 本のクエリに、matching を 1 つの tx に）が同じ 3 つの関数を書き換えていて、merge で 3 ファイルが衝突した（サブエージェントに差し戻して解消）。どちらもこの表に行があれば、書き始める前に分かった。

## 表

| branch | files / functions | layer | owner (session) | status | updated |
|---|---|---|---|---|---|
| fix/example/ride-cache（例。消してよい） | `webapp/go/app_handlers.go`: `appGetNotification`, `getLatestRideStatus` | logic | lane2 / subagent ridecache | abandoned | 10:00 |
| setup/free-wins | `webapp/go/main.go`: `setup` (DSN / pool / Open+ping retry, Logger middleware line) (same file as setup/measure-patch); `etc/nginx/nginx.conf`, `etc/nginx/sites-available/isuride.conf`; `etc/systemd/system/isuride-go.service.d/` | config | lane2 / subagent D | merged | 22:40 |
| setup/measure-patch | `webapp/go/measure.go` (new), `webapp/go/sqltag/sqltag.go` (new), `webapp/go/main.go`: `main` (ListenAndServe line + `go startMeasure()`), `setup` (driver name on the sqlx.Connect line only; same function as setup/free-wins, coordinated by lane 2: only the driver-name token), `webapp/go/middlewares.go`: `appAuthMiddleware`, `ownerAuthMiddleware`, `chairAuthMiddleware` (1 line each) | config | lane2 / subagent B | merged | 22:40 |
| infra/measure-off | `deploy.sh`: 2d block only (MEASURE_OFF=1, calls tools/bootstrap/measure-off.sh) (same file as setup/db-split) | config | lane2 / subagent E | open | 22:27 |
| infra/measure-on | `deploy.sh`: 2d block only (MEASURE_OFF=0, stacked on infra/measure-off) (same file as setup/db-split) | config | lane2 / subagent E | open | 22:27 |
| setup/db-split | `deploy.sh`: section 0 (DB_HOST / DB_PRIVATE_IP, env.db.sh copy, db.conf drop-in, matcher on svc1 only); `etc/mysql/mysql.conf.d/zz-remote.cnf` (new), `etc/MODES.txt` (2 lines), `etc/systemd/system/isuride-go.service.d/db.conf` (new; same dir as setup/free-wins), `env.db.<host>.sh` (new), `webapp/sql/zz-grant-remote.sql` (new), `webapp/sql/init.sh` (env.db.sh source) | config | lane2 / subagent A | merged | 22:40 |
| integ/early | `webapp/sql/indexes/early-indexes.sql` (new); `webapp/go/app_handlers.go`: `getChairStats`, `appGetNearbyChairs` | SQL only + logic (N+1, tables rides / ride_statuses / chair_locations / chairs) | lane3 | merged | 22:40 |
| setup/app-map | `docs/app-map.md`, `docs/app-annotations.json`, `docs/plans/app-early.md` (new) | docs | lane3 | merged | 22:34 |
| infra/unit-wants | `etc/systemd/system/isuride-go.service` (Requires=mysql -> Wants=mysql); `deploy.sh`: section 2a only (etc_install of the unit) (same file as setup/db-split, infra/measure-off) | config | lane2 | merged | 22:47 |
| integ/20261029-133401 | `webapp/sql/indexes/20261029-133401-indexes.sql` (new); `webapp/go/app_handlers.go`: `appGetNotification` (retry_after_ms), `appGetNearbyChairs` (stacked on integ/early); `webapp/go/chair_handlers.go`: `chairGetNotification` (retry_after_ms); `webapp/go/owner_handlers.go`: `ownerGetChairs` | SQL only + logic (rides / ride_statuses / chair_locations) | lane3 | merged | 22:47 |
| fix/20261029-133401/ops | `etc/mysql/mysql.conf.d/zz-perf.cnf` (thread_cache_size) | config | lane2 | merged | 22:40 |
| early/matching | `webapp/go/internal_handlers.go`: `internalGetMatching`; `etc/systemd/system/isuride-matcher.service.d/interval.conf` (new); `etc/MODES.txt` (1 line); `deploy.sh`: section 0 matcher block only (same file as infra/measure-off, infra/unit-wants) | logic + config (tables rides / chairs / chair_locations / ride_statuses, read; rides.chair_id UPDATE) | lane c | merged | 22:47 |
| early/payment-idem | `webapp/go/payment_gateway.go`: `requestPaymentGatewayPostPayment`; `webapp/go/app_handlers.go`: `appPostRideEvaluatation` (the call line only; same file as integ/20261029-133401, other functions) | logic | lane c | merged | 22:47 |
| integ/20261029-134509 | `webapp/go/app_handlers.go`: `calculateDiscountedFare`, `getChairStats` (same file as integ/20261029-133401) | SQL only | lane3 | merged | 22:52 |
| impl/chair-distance (design first) | `webapp/go/owner_handlers.go`: `ownerGetChairs` (same function as integ/20261029-133401: stacked after it); `webapp/go/chair_handlers.go`: `chairPostCoordinate`; `webapp/sql/` new table chair_distances + backfill | schema + logic (chairs / chair_locations) | lane3 | merged | 23:10 |
| integ/20261029-135253 | `webapp/go/app_handlers.go`: `appGetNearbyChairs` (latest position from chair_distances; matching in internal_handlers.go is lane c) | SQL only | lane3 | open | 23:10 |
