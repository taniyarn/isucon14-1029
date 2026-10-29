---
name: parallel-setup
description: サーバーの準備が終わった直後（baseline ベンチを待たずに）、定型のインフラ作業（DB を svc3 に分離、計測パッチ、free wins）を 1 つの指示で、サブエージェントを worktree で並列に走らせて 1 本の PR にする手順。merge は baseline の後にハブが行う。app-map はレーン 3。
---

# parallel-setup

## いつ使うか

**`[DONE] servers` の直後（baseline と並行）**。要るのは `deploy.sh`（レーン 2 が `task deploy:init` で作ったもの）と `docs/server-info-*.txt`。kickoff ② の `docs/kickoff.md` が後から来たら、「データの置き場所」（付随 DB、ワーカー）を見直して PR に反映する。**baseline を待たない理由**: PR を作るまでに 10〜15 分かかり、baseline（初期状態）の計測とは独立しているから。merge だけは baseline の後（初期状態の数字を取るため）。2026-10-21 の練習では baseline を待ってから始め、1 回目と 2 回目のベンチの間が 15 分空いた。

**レーンで回すとき（当日の形）**: レーン 2（インフラ、`docs/lanes/2-infra.md`）が `[TASK] 2 setup` を受けて回す（A・B・D・E。**C の app-map はレーン 3 が `[TASK] 3 app-map` で別に回す**）。違いは 3 つ: (1) main の checkout には触らない（下の `git checkout main` / `git pull` は `git fetch` + `task wt:add NAME=integ-setup BRANCH=integ/setup` に読み替える）、(2) **merge・deploy・smoke はレーン 1**（PR を作って `[PR] #<n> ready setup` を送り、「手が空いたら」へ）、(3) 始めと終わり、サブエージェント待ち、push の直前に `task lane:status LANE=2 …`。`docs/kickoff.md` の「前提の確認」に ✗ がある（MySQL でない、nginx が無い、3 台でない）ときは、該当するサブエージェント（A / D）を起動せず `task lane:alert` を確かめ、外した理由を PR に書く。

## 人が言うこと（1 枚で回すとき。レーンではレーン 1 の `[TASK]`）

```
parallel-setup skill で
```

（DB ホストは HOSTS の 3 番目。別のホストにしたいときだけ「DB は svc2」のように添える）

## Claude（オーケストレーター）がやること

1. `git fetch && git checkout main && git pull --ff-only`。**`task hosts:ips` を実行して DB ホスト（HOSTS の 3 番目、`DEPLOY_FIRST` があればそれ）の private IP と hostname を取る**（読み取り専用。出力は `public -> private (hostname)`）。`task env:show` で HOSTS の並びと CONTEST を確認。`docs/kickoff.md` を読む（構成要素、データの置き場所、付随 DB、設定ファイルを読む別プロセス、Docker 起動か、言語ディレクトリ）。`deploy.sh` と `env.sh` を読む。`.env` は読めないので `task env:show`。
2. サブエージェント（A・B・D と E。1 枚で回すときは C も）を **同時に** 起動する（Agent ツール、`isolation: "worktree"`、ブランチ名は下の通り）。モデルは `model: "opus"`（定型作業は Opus 5.5 で十分。判断はオーケストレーター側）。各プロンプトの冒頭に「CLAUDE.md と docs/IMPLEMENTATION-NOTES.md を先に読む。デプロイとベンチはしない。指定ファイル以外は触らない」と、次の claims の段落（英語のまま）を入れる。起動の前にオーケストレーターも `docs/claims.md` を読み、同時に走っている parallel-fix（レーン 2）の `open` の行と A/B/D の担当ファイルが関数単位で重ならないかを見る（`main.go` の DSN / pool / 計測の行が重なりやすい）。

```
Before editing anything: read docs/claims.md on origin/main (`git fetch -q && git show origin/main:docs/claims.md`). Same function as an
`open` row of another branch = conflict: do not edit, report "claims conflict: <branch> <function>" and stop. Same file, other function = OK,
note "(same file as <branch>)". Append your row (branch setup/<name>, files / functions, layer, owner = subagent <A|B|C|D>, status open,
updated HH:MM) and push docs/claims.md ALONE to main exactly as docs/claims.md step 3 says (throwaway worktree on origin/main).
```

### サブエージェント A — `setup/db-split`（触るのは `etc/`、`env*.sh`、`deploy.sh`、`webapp/sql/` のみ）

```
Move the app's MySQL to svc3 (private IP <IP>, hostname <HOSTNAME> — both from `task hosts:ips`, filled in by the orchestrator). Keep MySQL installed and running on every host (other components such as DNS may use the local one; see docs/kickoff.md "データの置き場所"); only the app's connection moves.
1. etc/mysql/mysql.conf.d/zz-remote.cnf: bind-address = 0.0.0.0 (new file; keep zz-isucon.cnf slow-log settings as they are). deploy.sh's DB-host block installs it with etc_install and restarts mysql on the DB host only.
2. Grants: the AMI has only isucon@localhost, so app hosts fail with MySQL Error 1130 until this runs. Add webapp/sql/zz-grant-remote.sql: CREATE USER IF NOT EXISTS 'isucon'@'%' IDENTIFIED BY '<password from env.sh>'; GRANT ALL ON <db>.* TO 'isucon'@'%'; FLUSH PRIVILEGES; (idempotent). deploy.sh's DB-host block applies it; task deploy deploys the DB host first.
3. Do NOT edit env.sh (some contests regenerate it at boot). Put the DB host in env.db.sh (DB_HOST-like variable of this app = <IP>) and load it after env.sh via a systemd drop-in etc/systemd/system/<unit>.d/db.conf with a second EnvironmentFile=/home/isucon/env.db.sh line (deploy.sh installs the drop-in with etc_install + daemon-reload). On the DB host itself env.db.sh points to 127.0.0.1 — simplest: env.db.<hostname>.sh files and deploy.sh copies the matching one to env.db.sh.
4. webapp/sql/init.sh (run by /initialize) reads env.sh itself: make it also source env.db.sh, otherwise /initialize loads data into the local DB while the app reads svc3.
5. deploy.sh: fill DB_HOST (hostname of svc3) and DB_PRIVATE_IP (<IP>) at the top; the generated block already does: remote cnf + grants on the DB host, a TCP check of <IP>:3306 on app hosts before restarting the app (fails loudly instead of starting an app that cannot connect).
6. Never change the mode/owner of files under /etc: use etc_install (reads etc/MODES.txt). ISUCON13 broke /initialize because pdns.conf became 640 root and the app's pdnsutil could not read it.
7. If docs/kickoff.md lists another component that uses MySQL (e.g. PowerDNS), leave it on the local MySQL and say so.
8. systemd workers/timers that call the local app (e.g. a matcher that curls its own nginx, a batch timer) must run on ONE host only once the DB is shared: otherwise every host runs it against the same DB (ISUCON14: isuride-matcher on 3 hosts, matching without locks = double assignment). In deploy.sh enable/start them on svc1 only and `systemctl disable --now` them on the other hosts. List them from docs/kickoff.md "初動の注意" (side workers) and `etc/systemd/system/*.{service,timer}`.
Report in Japanese, in EXACTLY this form: branch@commit / files / actions (done|skipped why) / reboot (where the DB setting lives after reboot) / two-app risk / unverified.
```

### サブエージェント B — `setup/measure-patch`（触るのは `webapp/go/` のみ）

```
Apply the measurement patch per tools/sqltag/README.md: copy tools/sqltag/sqltag.go to webapp/go/sqltag/ (module path from go.mod). Put everything in a NEW file webapp/go/measure.go (sqltag.Register + driver name, the router middleware for chi/echo, expvar.Publish("db", db.Stats()), the 127.0.0.1:6060 pprof/expvar listener, the X-Isu-Uid response header set in the auth middleware once the user is known, all skipped when env MEASURE=0) and touch main.go in at most 3 lines: the driver-name swap in the DSN/open call, one call to install the middleware, one `go startMeasure()` — each marked "// measurement:". Subagent D edits the DSN/pool/ping/Debug lines of main.go at the same time: keep your main.go edits to those 3 lines so the merge stays clean. No performance changes. `cd webapp/go && CGO_ENABLED=0 GOOS=linux go build ./... && go vet ./...` must pass.
Report in Japanese: files changed; whether handlers pass request ctx to DB calls (if many use non-Context methods, list them — tags will be missing there).
```

### サブエージェント D — `setup/free-wins`（触るのは `webapp/go/main.go` の接続まわり、`etc/systemd/system/<unit>.d/`、`etc/nginx/`）

```
Apply the unconditional small wins that every top team did (docs/TECHNIQUES.md §8), one commit each, no behaviour change:
(1) DSN: InterpolateParams=true (no ADMIN PREPARE round trips); (2) pool: SetMaxOpenConns/SetMaxIdleConns 64, SetConnMaxLifetime 60s (the reference often pins 10) — if the DSN/pool code is shared by several DBs do it for each; (3) start-up: sqlx.Open (lazy) + a ping retry loop (up to 60 s) instead of sqlx.Connect, so the app survives a DB host that boots later; (4) framework logging: echo Debug=false, remove middleware.Logger()/pretty JSON, no per-request app log line (nginx keeps the access log); (5) etc/systemd/system/<unit>.d/restart.conf already has Restart=always and LimitNOFILE=65536 (deploy-init) — add Environment=GOGC=200 for Go (not higher: 4 GB hosts) and nothing else; (6) etc/nginx: worker_rlimit_nofile 65536, worker_connections 4096, upstream block to the app with keepalive 64 + proxy_http_version 1.1 + proxy_set_header Connection "" (edit the captured files under etc/nginx/ only). Check deploy.sh: the `etc_install etc/nginx/...` line must be active (deploy-init enables it when there is one site; if it is still commented, enable it with the real site file name) or the nginx change never reaches the hosts. Subagent B edits main.go too (3 marked lines): touch only the DSN/pool/ping/Debug/Logger lines.
Do NOT change JSON libraries, transactions, FOR UPDATE, caches, indexes or any handler logic (those are judged later). `cd webapp/go && go build ./... && go vet ./...` must pass; `sudo nginx -t` equivalent: check the config with `nginx -t -c` on the host is done by deploy.sh.
Report in Japanese: files changed; what was already in place and skipped; the exact nginx diff (a human reviews it — a wrong keepalive setting once halved a team's score).
```

### サブエージェント E — `infra/measure-off` と `infra/measure-on`（触るのは `deploy.sh` の新しい 1 ブロックだけ。統合ブランチには入れない）

特別賞が近づいたとき（score-sprint）に「merge → deploy → 1 ベンチ → 戻しを merge」だけで済むように、**計測 OFF の PR と戻しの PR を最初から open にしておく**（2026-10-28 の振り返り）。`task hosts:measure-off` だけだと、次の `task deploy` が `etc/nginx/nginx.conf`（ltsv の access_log）を入れ直して計測が半分戻る。`deploy.sh` に切り替えを置けば、どの deploy の後も repo の値どおりになる（drop-in・mysql の設定の退避・agent の disable はホストのファイルなので再起動の後も残る）。

```
Prepare two PRs that switch the measurement off / on through deploy.sh (do not merge, do not deploy).
What `task hosts:measure-off` does = tools/bootstrap/measure-off.sh off on every host: stop + disable isumeasure-agent;
move the slow-log config (zz-isucon.cnf) out of mysql.conf.d and restart the DB (dependent app units that were active
restart); nginx access_log ltsv → off in /etc/nginx/nginx.conf; a drop-in Environment=MEASURE=0 (zz-measure-off.conf)
on every running isu* unit (the measurement patch skips sqltag / pprof / the uid header) + restart.
`measure-off.sh on` reverses all of it. Calling it from deploy.sh keeps one implementation.
1. task wt:add NAME=measure-off (branch measure-off from origin/main) and rename the branch:
   git -C ../wt-measure-off branch -m infra/measure-off. In deploy.sh, right BEFORE the "# ---- 3. restart" section (after
   the nginx etc_install line, so measure-off.sh edits the nginx.conf the repo just installed), add exactly:
     # ---- 2d. measurement switch (score-sprint / final bench): 1 = off, 0 = on, empty = leave the hosts as they are.
     # Runs tools/bootstrap/measure-off.sh on every deploy, so a deploy never turns the access log back on behind you.
     MEASURE_OFF=1
     case "$MEASURE_OFF" in 1) sudo bash tools/bootstrap/measure-off.sh off ;; 0) sudo bash tools/bootstrap/measure-off.sh on ;; esac
   bash -n deploy.sh. Commit "deploy: measurement off (score-sprint)". task push DIR=../wt-measure-off SET_UPSTREAM=1, then
   task pr:create DIR=../wt-measure-off TITLE="[sprint] 計測 OFF（nginx access log / slow log / MEASURE=0 / agent）" BODY_FILE=…
   Body: what it turns off; "merge → deploy → 1 bench (score on the portal; no ops run) → merge infra/measure-on"; the
   journal line `hh:mm | infra/measure-off #<n> | 計測 OFF | 特別賞 | `; "通常のレビューの todo にまとめない".
2. In the same worktree: git -C ../wt-measure-off switch -c infra/measure-on (stacked on measure-off, NOT a git revert:
   the block must stay so the deploy turns measurement back on). Change only MEASURE_OFF=1 → MEASURE_OFF=0, commit
   "deploy: measurement back on", task push DIR=../wt-measure-off SET_UPSTREAM=1, PR (base main) titled
   "[sprint] 計測 ON に戻す（infra/measure-off の後）". Body: stacked — before measure-off merges its diff shows both
   commits, after it the diff is the one line.
3. claims: one row per branch, "deploy.sh 2d block only".
Report in Japanese: the two PR numbers and URLs, the deploy.sh line of the block, anything measure-off.sh cannot cover on
this AMI (no isumeasure-agent, PostgreSQL, a site-level access_log in etc/nginx/sites-enabled/ that stays on — list it;
if there is one, add `access_log off;` handling for it to the same block and say so).
```

E は A / B / D と同時に出してよい（E だけは `isolation` を付けず、プロンプトのとおり `task wt:add` で自分の worktree を作る。2 本目を 1 本目の上に積むため。触るのは `deploy.sh` の新しいブロックだけ。A が `deploy.sh` の DB の節を変えるので、merge のときに衝突したらレーン 2 が `[TASK] 2 rebase #<n>` で直す）。E の PR は `integ/setup` に入れず、`[PR] #m #n ready [sprint] measure-off / measure-on` として別に送る。ハブは特別賞（score-sprint）まで ship しない。

### サブエージェント C — `setup/app-map`（触るのは `docs/app-map.md`、`docs/reports/`、`tools/app-analyzer/` のみ。**レーンで回すときはレーン 3 の仕事**。1 枚で回すときだけここで起動）

```
Run the app-analysis skill against svc2 (never the bench host): tools/app-analyzer/analyzer.config.json is already doctor-OK with resolve → svc2 and "initialize" set. `cd tools/app-analyzer && npm run analyze -- analyzer.config.json > /tmp/analyze.log 2>&1` (watch the log; a flow is cut after flowTimeoutMs), then write docs/app-map.md (8 sections, ≤150 lines) from docs/reports/latest.md, webapp/go routes, the schema and docs/kickoff.md. If the crawl fails, write the code-only version and say so.
Report in Japanese: the 3 facts about the app that matter most (data model, state transitions, waits), and anything that will break with two app servers (process-local state, host-bound side effects).
```

3. A と B が終わったら統合ブランチに merge して **PR を作り、リンクを貼って止まる**: `git checkout main && git pull && git checkout -b integ/setup && git merge --no-ff setup/db-split && git merge --no-ff setup/measure-patch && git merge --no-ff setup/free-wins`（1 枚で回すときは `&& git merge --no-ff setup/app-map` も。ファイルは重ならない。衝突したら該当エージェントに差し戻す）。`cd webapp/go && go build ./... && go vet ./...` → `task pr:submit DIR=<integ の worktree> TITLE="parallel-setup: DB → svc3, measurement patch, free wins, app-map" BODY_FILE=<本文>`（commit・push・PR 作成を 1 回で）。PR 本文に 3 つの報告（files / actions / reboot / two-app risk / unverified）を表で。merge したら `docs/claims.md` の `setup/*` の行を `merged` にして push。
4. （レーン 2 ならここで `[PR]` を送り、「手が空いたら」へ。以下はレーン 1 か、1 枚で回すときの手順）人が OK と言ったら `gh pr merge --merge` → `task deploy`（終わったら `task worktrees:clean`）（DB ホストが先に配られる）→ `task smoke INIT=1 DNS=1`（svc1 = HOSTS の 1 台目が既定）（initialize が 200、ログイン OK、主要 GET に 5xx なし、DNS が引ける。失敗の切り分けは `task host:diag HOST=<svc1>`）→ **「ready for bench」と言って止まる**。C の `setup/app-map` も main に merge して push。レーンで回しているときの連絡は `docs/MESSAGES.md` の 1 行だけ（レーン 2 → 1 の `[PR] #<n> ready setup`）。ブランチごとのデプロイ検証はしない（効かなければ merge commit を `git revert -m 1`）。
5. 報告（表）: エージェント / 変更ファイル / 確認できなかったこと / 2 台構成で壊れる懸念。

## 人がベンチの後に見ること

Fleet で svc3 の mysqld が CPU を使い svc1 の mysqld がほぼ 0%、report の Verdict 下に pprof top、SQL 表に called from。`docs/app-map.md` を Discord に。これで §5 の作戦会議に入れる。

## 予算

オーケストレーターが読むのは kickoff.md、deploy.sh、env.sh とサブエージェントの報告だけ。サブエージェントは自分の担当ファイルだけ読む。
