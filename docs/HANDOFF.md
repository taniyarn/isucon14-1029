# HANDOFF — 新しいセッションが最初に読むファイル

このファイルは「前のセッションと同じ知識・同じ動き方」を新しい Claude セッションに渡すためのもの。kit（テンプレート）でも、そこから作ったコンテスト repo でも、**作業前に必ず読む**。読み終えたら、当日のセッションは `docs/lanes/README.md` と自分のレーンのファイル、それ以外は `docs/RUNBOOK.md` の該当節に進む。

## 1. 何をしているか

- チーム pikopiko（taniyarn, shota3506, ytakaya。Go）が **ISUCON2026（2026-10-31 10:00–18:00 JST、3 台想定）** に出る。去年は 35,076 点だが再起動試験の「データ保持 NG」で失格。今年の最優先は「失格しない」、次に計測主導で改善を回すこと。
- このツール群は **`ytakaya/isucon-kit`（GitHub template）** にあり、大会・練習ごとに「Use this template」で repo を作る（例: `isucon13-20261012`）。kit に手を入れたら、動いているコンテスト repo へ **ファイルコピーで同期**する（template 由来の repo は履歴を共有しないので cherry-pick はできない。`cp` か `git -C kit diff <base> HEAD -- <paths> | git apply --3way`）。
- 練習は matsuu/aws-isucon の AMI で 3 台 + 自前の ops ホスト。これまでに ISUCON14（ISURIDE）、ISUCON13（isupipe）、ISUCON12 予選（isuports）で試した。

## 2. 構成（頭に入れておく地図）

```
手元 (Taskfile, .env) ──ssh──> svc1..3 (isumeasure agent, /home/isucon = このrepo)
        │                          ▲ outbound ssh トンネルのみ（競技ホストの SG は触らない）
        └──https 443 basic──> ops 3.112.200.249 = https://3-112-200-249.sslip.io  (Caddy → panel → tunnel)
```

| 場所 | 中身 |
|---|---|
| `tools/isumeasure/` | Go 単一バイナリ。`agent`（各ホスト常駐: /proc, MySQL status, expvar, access/slow log を tail、`POST /initialize` で run 開始、100 req 未満は破棄、panel から shadow run）、`panel`（ops: Fleet、Runs、collect、report、Score timeline、retro、fleet 全体の run 調整、「⏸ not benching now」で検出を 30 分止める）、`report`（Verdict → Where the time goes → Next actions。actions.json にグループ/slug）、`export`（DuckDB 用 CSV）、`retro` |
| `tools/bootstrap/` | `bootstrap.sh`（git 化 + template merge、etc/ と MODES.txt 取り込み、alp/slp、ltsv、slow log、server-info）、`hosts-setup.sh`、`deploy-init.sh`（systemd unit から deploy.sh を生成: etc_install、DB ホスト節、indexes/apply.sh）、`measure-off.sh`、`host-diag.sh`、`server-info.sh` |
| `tools/app-analyzer/` | Playwright 巡回 → `docs/reports/latest.html`。`resolve`（hosts 不要）、`login`、`initialize`、API 連打の throttle、flow の `api()`、`routes-from-bundle.mjs`、`smoke.mjs` |
| `tools/ops/` | ops ホストの compose（caddy / panel / tunnel）。`isucon.env` に CONTEST と SVC1..3 |
| `tools/sqltag/` | SQL に `/* METHOD route */` を付ける 3 行パッチ（echo / chi） |
| `tools/{kickoff,parallel-setup,parallel-fix,deep-dive(=isumeasure/deepdive),technique-scan,next-step,score-sprint,tuning,handoff,retrospective}/SKILL.md` | `.claude/skills/*` から symlink。当日の Claude の手順書 |
| `docs/TECHNIQUES.md`、`docs/techniques/sources/*.md` | ISUCON 9〜14 のテクニック集（中盤に technique-scan が全件判定する）と、大会別の調査原本（出典 URL 付き）。`docs/techniques/sources/measurement-practices.md` は他チームの計測手法の調査（導入候補） |
| `docs/RUNBOOK.md` | 当日の手順（上から打てば環境ができる）。`docs/IMPLEMENTATION-NOTES.md` は実装時の ISUCON 固有ルール（再起動試験が先頭） |
| `Taskfile.yml` + `.env`（gitignore） | 手元の全操作。`task --list`。`.env` は Claude からは読めない → `task env:show` |

**データの流れ**: ベンチ → agent が run を記録（ltsv の slice、1 秒系列、pprof、processlist）→ panel で collect（ホストから削除）→ `report.html` / `summary.md`（8 KB、Claude に渡すのはこれ）/ `actions.json` → `task actions:plan` でグループ分け → parallel-fix → main に merge → `task deploy` → `task smoke` → ベンチ。

## 3. 人と Claude の分担（守ること）

- **人がやる**: ベンチの実行、`.env` の編集、ポータル操作、GitHub の設定（deploy key、template 化）、サーバーを変える操作（`task deploy` 以外の ssh）。Claude は読み取り専用の `task`（`hosts:ips`、`host:diag`、`smoke`、`env:show`、`db:query`、`dns:check`）は自分で実行してよい。kit の開発環境では ssh 自体が拒否されることがあるので、その場合は人に頼む。
- **Claude がやる**: repo の編集、ローカルでのビルドと検証、commit/push、手順書の更新、スキルの実行（kickoff / parallel-setup / parallel-fix / deep-dive / technique-scan / retrospective）、サブエージェントの起動（worktree 分離）。
- **kit を直したらコンテスト repo に同期**する。同期するのは `Taskfile.yml`、`tools/`、`docs/RUNBOOK.md`、`docs/IMPLEMENTATION-NOTES.md`、`CLAUDE.md` のルール節。**同期しないもの**: `deploy.sh`、`env*.sh`、`etc/`、`webapp/`、`docs/kickoff.md`、`docs/app-map.md`、`docs/plans/`、`tools/app-analyzer/analyzer.config.json`、`alp.yml`（コンテスト固有）。
- **kit の PR の本文には「狙う指標」を書く**: どの練習記録の値（`practice-log/SCHEMA.md` のフィールド。例 `north_star ↓`、`agents.2.idle_min_before_prize ↓`、`speed.ship_median_s ↓`）が動くはずか。次の練習の後に `task practice:record` → `task practice:report` で動いたかを確かめる。
- `git add -A` の前に `git status` を見る。`.claude/worktrees/` は ignore 済みだが、サブエージェントの作業ツリーや `.env`、`node_modules`、`out/` を拾わない。
- commit メッセージは英語で要点を 1 行、末尾に `Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>`。
- 返答の言語は各自の `CLAUDE.local.md`（gitignore）で指定。無ければ相手の言語。結論を先に、表を使う。
- **コードを書き始める前に `docs/claims.md`**（origin/main のもの）を読み、触るファイル・関数が `open` の行と重ならないか確かめ、自分の行を足して push する（main への直接 commit はこのファイルと kickoff の成果物だけ）。merge / 中止で行の status を直す。
- **当日は A が 1 人で Claude セッション 4 枚、10:00 からチームに合流するまで（特別賞を越え、伸び悩むか構成が固まったら合流。lanes README §11）**（レーン 1 = ハブ、レーン 2 = インフラ、レーン 3 = アプリ、レーン c = スカウト。2026-10-21 の練習で 3 枚 → 4 枚に。理由は `docs/retro/2026-10-21-lanes-feedback.md`）。人は盤を `task lane:web`（ブラウザ）か `task lane:watch` で見る。**当日のセッションは `docs/lanes/README.md` と自分のレーンのファイルを読んで、それに従う**（時刻表ではなく出来事で動く）。状況は盤（`task lane:status`）、想定外は `task lane:alert`（影響 = 動かない kit の部品）、人への頼みは `task lane:todo`（そのまま貼れる形）。セッション同士は `docs/MESSAGES.md` の 1 行（`[HELLO]` / `[TASK]` / `[PHASE]` / `[PR]` / `[DONE]` …）だけで、状態は送らない。宛先は `[HELLO]` の表の名前に限る（`ListAgents` には似た名前の無関係なセッションも出る）。merge はレーン 1 だけ、OK は人がレーン 1 のタブに直接言う。読み取り専用のコマンドは `.claude/settings.json` で許可済み（PREP の 7）。
- **main に入れるのは PR 経由のみ**。ブランチを push → `gh pr create` → リンクを人に渡して止まる → OK 後に `gh pr merge` → `task deploy` → `task smoke`。例外は fast lane（CLAUDE.md ルール 4、タイトル `[fast]`: index ファイルの追加だけ（読む SELECT が全部 ORDER BY 付き）/ マニュアルがサーバーに任せた数値定数だけ / `etc/` の設定だけ）で、build が通れば OK を待たずに merge → deploy → smoke（落ちたら revert）し、人は事後にレビューする。parallel-setup / parallel-fix は `integ/<ts>` に `--no-ff` で統合した 1 本の PR。コードやコマンドはブロックに。推測と事実を分ける（「未確認」と書く）。

## 4. 変更を入れる前に手元でやる検証（これをやらずに push しない）

| 対象 | 検証 |
|---|---|
| `tools/isumeasure` | `cd tools/isumeasure && gofmt -l . ; go vet ./... && make -s linux darwin`。report は実データで: `dist/isumeasure-darwin-arm64 report -out /tmp/rep -code <contest>/webapp/go <collected run dir>`（run は `task run:pull TS=` か ops の `/data/<contest>/measurements/<ts>/<host>/` を curl で取得）。panel は `isumeasure panel -env <env> -data <dir> -listen 127.0.0.1:18089 -agent-base http://127.0.0.1:1999` で起動して `/api/*` を叩く。agent の挙動はローカルで agent 2 つ + panel を立て、ltsv を追記して試す（`/tmp/coord-test.sh` の形。access ログは `time:…\tmethod:…\turi:…\tstatus:…\treqtime:…` の ltsv） |
| `tools/app-analyzer` | `node --check *.mjs`、実機（空いている svc2）に対して `node analyze.mjs analyzer.config.json --skip-doctor`。約 60 秒。`docs/reports/latest.md` で `test:id` のような壊れた正規化が無いか、ホスト違いの重複行が無いか |
| `tools/bootstrap` | `bash -n`。git の init/join/merge は `/tmp` に bare repo + template + home を作って通す（`/tmp/bootstrap-merge-test.sh` の形）。`deploy-init.sh` は `etc/` をコピーした一時ディレクトリで実行して `bash -n deploy.sh` |
| `Taskfile.yml` | `task --list` と `task -n <task>`（dry run）。コロンを含むコマンドは引用符、`LANG` のような環境変数名を task 変数に使わない、dotenv の値はグローバル `vars:` から見えない（task 内 vars で） |
| スクリーンショット | HTML は Playwright（contest repo の `tools/app-analyzer/node_modules` にある）でスクショを撮って目で見る |

## 5. これまでに踏んだ落とし穴（同じことをしない）

- go-task の内蔵シェルは `for … do ( … ) & done` の背景 subshell を取り違える → 並列は bash スクリプトに出す。
- `docker compose restart` は `env_file` を読み直さない → `up -d --force-recreate`。
- GitHub の deploy key は 1 鍵 1 repo → コンテストごとに `tools/bootstrap/deploy_key` を作り、ops も同じ鍵で clone。
- 練習 AMI はドメインが本番と違う（`.dev` → `.local`）。nginx `server_name`・Cookie Domain・`pdnsutil` の 3 つで判定。
- ベンチを受けていないホストは DB が空（初期ユーザーで 401）→ analyzer の `initialize`。巡回は **svc2** に向ける。
- AMI の MySQL は `isucon@localhost` だけ → DB 分離で Error 1130。権限 SQL を DB ホストに先に当てる（`task deploy` は DB ホスト先）。
- `/etc` のファイルを `sudo cp` すると権限が変わる（pdns.conf 640 → pdnsutil 失敗 → `/initialize` fail）→ `etc_install`。
- `env.sh` を上書きしない年がある → `env.db.sh` を drop-in の 2 つ目の EnvironmentFile で。`init.sh` も source させる。
- ISUCON12 予選は app が `docker compose` で起動、テナント DB は SQLite → deploy.sh はネイティブ化、SQLite は slow log に出ない（`ISUCON_SQLITE_TRACE_FILE`）。
- ISUCON13 は PowerDNS が MySQL を叩く（DNS 水責め）。`records` の index は app ではなく ops:pdns グループ。
- Go の `flag` は位置引数の後のフラグを無視 → `flagsFirst`。chi と echo でルートの正規表現が違う（両対応済み）。sqltag は ctx を渡すクエリにしか付かない。
- Playwright で API を `route.fetch` すると Cookie が壊れた → throttle は `continue`/`abort` だけ。遅い API は待たず記録して先へ。
- 共有ヘルパー（`fill*Response` 等）は多数の endpoint から呼ばれる。直すならレスポンスの形を変えない。hot path だけ分岐する選択肢。
- `mysql -p<pass>` は stderr に警告を出す → スクリプトで出力をエラー判定するなら `MYSQL_PWD` を使う（indexes/apply.sh で踏んだ）。
- サブエージェントの worktree とブランチは残る → 1 周ごとに `task worktrees:clean`。
- `summary.md` 以外を Claude に読ませない（report.html 1.7 MB、生ログは DuckDB 経由）。
- **ops の panel が report 中に OOM（exit 137）→ 再起動ループ**（2026-10-13）。DB を 1 台に集めると slow log（long_query_time=0）が 44 MB gz になり、全エントリをメモリに載せていた。今は読みながら集計し（同じ run で 365 MB → 75 MB）、report は子プロセス（`REPORT_MEM_MB`）で 1 本ずつ。失敗は Runs に「report failed」と出て panel は生き残る。
- 練習のベンチを svc1 の上で回すと bench が CPU の 95% → **svc2 から**。SG に UDP 53、DNS の答えを svc1 の private IP に（練習専用、RUNBOOK 付録）。
- `unattended-upgrades` / `apt-daily` がベンチ中に動いた（bench 機 97%、DB 機 29%）→ bootstrap が timer と unit を mask。
- その mask を `disable --now` / `mask --now` でやっていたら、走っている unattended-upgrade の終了待ちで svc1 の bootstrap の 1b が 93 s（2026-10-25、hosts:setup 全体 181 s）→ `stop --no-block` + mask + SIGTERM で待たず、apt-get を使う箇所だけ上限付きでロック待ち（`apt_lock_wait`）。join の前半（`PHASE=prep`）は svc1 の init と同時に流す。各節の秒数は bootstrap の最後の `## timing` 行と hosts:setup の timing 節に出る。
- DB を別ホストに出すと app ホストの endpoint 表の SQL 列が空だった → report が全ホストの sqltag を結合。tag の `:username` と endpoint の `:id` も同じ鍵にした（単一ホストでもパラメータ付き endpoint は空だった）。
- parallel-setup（DB 分離）を飛ばして §5 に進んだ → §4 の終了条件と §8-1 に `deploy.sh` の `DB_HOST` を見る項目。
- ops の report で `GET /api/user/<name>/icon` がユーザーごとに 39 行に割れ、最重量 endpoint が上位 10 件から消えた（ops にコードが無く、ルーター由来の正規化が効かなかった）→ endpoint のグループは repo 直下の `alp.yml`（alp 互換、`task groups` で生成、kickoff phase 2 で commit）。report / export / panel が同じ normalizer（alp.yml → ルーター → id 置換）を使い、report 冒頭に「endpoint grouping: …」、割れていれば「Unmatched paths」節と警告が出る。ops は `task ops:repo` の clone から読む。
- IP を何度も打つのが面倒で間違いやすい → `HOST=` を取るタスクは全部 `1|2|3`（`.env` の HOSTS の順）、`svcN`、`db`（DB ホスト）、IP のどれでも受ける。省略は svc1。`task ssh` は `host:ssh` の別名。解決は `tools/bootstrap/host-ip.sh`。
- ops の panel が `/opt/isucon-ops/repo` を bind mount しているのに `task ops:repo` が `rm -rf` → clone で**ディレクトリを作り直し**、コンテナは消えた方を見続けた（report に `no alp.yml` / `router 0 routes`、icon が 1,000 行に割れた）→ ops:repo は同じディレクトリで init + fetch、初回は panel を recreate、cron で毎分 pull。`task ops:restart` / `task run:report` も追加。
- worktree で `go build ./...` → `git add -A` で 10 MB のバイナリが commit に入った → `.gitignore` に app 名のバイナリ、skill は `go build -o /dev/null .` と名前指定の `git add`。
- `task diffcheck` が比較 0 件で「差分なし」に見えた → 0 件は exit 1。
- `task smoke HOST=` が `--ip` を空で渡していた（`TARGET` を計算して `IP` を使う typo）→ 修正。
- `/initialize` 中の SQL（データ投入、zone import）が N+1 suspect に出た → report は initialize の応答時刻より前の slow log を除外。
- PR #15（ridecache）と PR #16（notification のクエリ、matching の tx）が同じ 3 つの関数を書き換えていて、merge で 3 ファイルが衝突した（2026-10-18）→ 書き始める前に `docs/claims.md` で宣言して突き合わせる（CLAUDE.md ルール 4 の例外 b）。
- 同じ作業ディレクトリで別セッションが `git checkout` して commit が main に混ざった → main の checkout はオーケストレーターだけ（CLAUDE.md ルール 4）。
- **2026-10-25 の練習（ISUCON13、1 人で 4 セッション）で踏んだもの**（原本 `docs/retro/2026-10-25-practice-feedback.md`）:
  - Taskfile の `env:` は、シェルで export された `SSH_KEY` が `.env` の値より優先される → `task runs` / `runs:wait` が ops に ssh できなかった。`.env` の値はコマンドの前に直接書く（`env:` に `.env` の値を書く task は同じ形にする）。
  - `ship.sh` の `$merged）`: `set -u` の下で全角括弧が変数名の続きとして読まれ unbound variable → ベンチの todo が作られずに終わった。日本語の直前の変数は `${merged}` と書く。
  - `git pull --ff-only origin main` が `Cannot fast-forward to multiple branches.` で落ちた（直前に人が GitHub で merge。手で同じコマンドを打つと通った。原因は未調査）→ ship.sh は `git fetch` + `git merge --ff-only FETCH_HEAD` に分ける。
  - `docs/journal.md` をハブ・レーン 3・レーン c の 3 か所から書いて 2 回衝突、`hub:ship 3 4 5` が #3 の衝突で全体停止 → journal はハブだけが書く（CLAUDE.md ルール 4 の例外 c）、hub:ship は衝突した PR を飛ばして残りを出す。
  - `tools/app-analyzer/analyzer.config.json` を kickoff phase 2 と app-analysis（レーン 3）の両方が新規作成して衝突 → 持ち主は kickoff phase 2、app-analysis は読むだけ。
  - `task hosts:setup` が 181 s、うち svc1 の「1b. stop background apt」だけで 93 s（`systemctl disable --now` が走っている apt の終わりを待ったと推測。未調査）。svc2 / 3 の join は svc1 の init（125 s）の後だった。
  - 新しい index（`icons(user_id)`）で `POST /api/icon` の DELETE → INSERT が gap lock で deadlock、1 run で 500 ×123。逐次の smoke では出ない → IMPLEMENTATION-NOTES §5、parallel-fix の indexes の行。
  - ハブがベンチの todo を閉じて作り直し、出た後に「#7 も先に入れるか」を聞いた → どの PR が載るか分からなくなった。帯が `ready` / `running` の間は ship しない（lanes README §7）。
- **2026-10-27 の練習（ISUCON13、1 人で 4 セッション、3,238 → 128,555）で踏んだもの**（原本 `docs/retro/2026-10-27-practice-feedback.md`）:
  - 人の「ベンチは 3 台以外から回す」をハブが「public IP が要る」と読み違え、DNS の答えを変えて人のベンチを止めた（実際は VPC 内から private IP 宛て）→ revert。初回ベンチまで 18 分 → 起動の 1 行に `ベンチ: <コマンド全文>`、前提はコマンドから決める（lanes README §0 の 4）。
  - 手元 PC からの `curl` / `dig` が社内プロキシ（Netskope）の TLS 中継で Empty reply → 練習の前にプロキシを切る（kickoff ① の todo）。
  - ハブが `hub:ship` の最中に main に plan を commit → ship の `git merge --ff-only FETCH_HEAD` が失敗、PR は GitHub で merge 済みのまま deploy されなかった → ship 中は main に commit しない、ship.sh が未 push の commit を見つけたら merge の前に止まる。
  - 同じファイルを触る 3 本の PR（#3 #4 #5）が import 行で衝突 → rebase と OK の取り直し → 同じレーンの同じファイルは 1 本の integ に。
  - PowerDNS の DB を svc2 に出したら、`task deploy` の順（svc3 → svc1 → svc2）で svc1 の pdns が起動できず deploy が止まり、その間の人のベンチが DNS 不通で fail → `DEPLOY_FIRST` に複数ホスト、pdns に `Restart=always` の drop-in。
  - 設計・scan・下読みのメモがローカルのブランチにしか無く、人が開けず `[PICK]` に答えにくかった → 文書は書いたらすぐ docs の PR、質問は 1 問 1 todo + `CHOICES` のボタン。
  - テンプレートに ISUCON14 の `docs/reports/latest.*` が残っていて、巡回前に古い analysis が見えた → placeholder に。
  - 9 回目以降、サーバーが 3 割空いたまま約 127,000 点で頭打ち → ベンチ機（c5.large）で bench が 161% / 200%。練習のベンチ機は c5.2xlarge 程度に（RUNBOOK 付録 8）。
  - DNS の A レコードの TTL 0 → 60 は逆効果（127,667 → 112,563、名前解決成功 36,007 → 33,647）→ revert。
  - キャッシュの PR で、追試の再起動直後に svc2 の app が未起動だと `/api/initialize`（svc2 のキャッシュを捨てる通知）が 500 → fail になる穴を、レーン c のレビューが見つけた → キャッシュ・2 台・initialize・tx の PR はレーン c のレビューを ship の条件に。

## 6. 検証済み / 未検証（2026-10-14 時点）

- 検証済み（実機）: hosts:setup（init/join の template merge、並列）、deploy:init → task deploy、計測パッチ、agent のベンチ検出と collect/report、Score timeline、retro（合成データ）、app-analysis（resolve / login / initialize / throttle、svc2 向け）、kickoff（isupipe）、parallel-fix（isupipe で 2 周）、DB 分離（子セッションが手動で実施。parallel-setup のプロンプトに落とし込み済み）、ops:reset、shadow run と 100 req 未満の破棄（ローカル agent 2 つで検証）。
- 未検証: app-analysis の Data model タブの行数（`task db:snapshot LABEL=latest` → `npm run report`。ER と表は合成データで確認済み）、report の pprof 節（flame graph HTML、call graph svg）を ops 上で生成すること（手元の合成プロファイルでは確認済み）、`task smoke` / `diffcheck` / `host:diag`（実機で未実行）、`deploy-init` の DB ホスト節と `indexes/apply.sh` の実機運用、`hosts:measure-off` → `db:snapshot` → `reboot-test` → 最終ベンチ（RUNBOOK §8 のチェックリスト。再起動試験の予行は次の練習で必ず通す）、report の「Tuning」節と `tuning` skill（agent が新たに取る MySQL カウンタ 5 つ、`listen_overflows_per_s` / `port_range`、`nginx-conf.txt` / `svc-env.txt`、ltsv の `upconnect` 列は次の練習で実機確認。ルールの閾値は合成データでしか見ていない）、report の「Bench output」節（`bench.log` を Runs の bench log 欄か `task bench:log` で付ける。13/14 の bench ソースで、ポータル向けのメッセージが重複排除 or 定期行だけであることは確認済み。実 run では未使用）、`technique-scan` skill（`docs/TECHNIQUES.md` 40 件を実 run に当てる判定。まだ一度も実行していない。次の練習の 13:00 枠で初回を回し、判定表の読みやすさと「要確認」の出方を見る）、ベンチ挙動の推測（report の "Bench shape" 行、endpoint 表の 304% 列、管制盤の score/POST 列、deep-dive §10 のクエリ。実 run 1 本で DuckDB と report の数字が一致することは確認、ベンチ側ログとの答え合わせは次の練習）、ltsv の `inm`（If-None-Match）列（bootstrap の再実行で入る。既存ホストは `task host:bootstrap` をもう一度）、alp.yml を ops の panel が `/opt/isucon-ops/repo/alp.yml` から読むこと（手元では 2 run × 3 条件で確認、`alp --config` の件数とも一致）、report の子プロセス実行と `REPORT_MEM_MB`（手元では 44 MB gz の実 run で 75 MB、合成 890 MB gz（実 run の 20 倍）でも 170 MB。ops 上は次の練習で）、bootstrap の apt 停止（実機未）、summary.md の Data growth 表（agent が run の開始/終了で COUNT(*) を `rows-start.txt` / `rows-end.txt` に記録。合成データで表の生成は確認済み、実機では agent 更新後の初ベンチで確認）、`task retention:from-run TS=` で run の行数を `docs/retention/latest.txt` に落とすこと、複数 run の retro、shadow run の実機（agent 更新 + ops:deploy 後の初ベンチで確認）。 2026-10-18 追加分（練習 run 20261018-011226 / 020042 の手元データで確認済み、実機は次の練習で）: sqltag の呼び出し元 `@ file:line`（単体テストのみ。ops の panel イメージに git を足したので `task ops:deploy` が要る）、report の Flows / Next step / SQL の code 列、`task flows`（Mermaid を CDN から読む）、`task app:refresh`（`npm run report -- --run` の経路は巡回データが無く構文チェックのみ）、`next-step` skill（未実行）。 score-sprint 一式（`prize_target` → report の行と Next step は run 020042 で確認。`measure-off.sh` の app への `MEASURE=0` の drop-in と、nginx の 2 台化の手順は実機未確認）。 2026-10-19 追加分: **1 人で 3 セッション**（MESSAGES の `[HELLO]` / `[PICK]` / `[DECIDED]`）。2026-10-20 追加分: **レーンの playbook と盤**（`docs/lanes/`、`task lane:*`、kickoff の前提の確認）。時刻表をやめて出来事で動かす形は未練習。次の練習で、盤の状態が手順ごとに変わること、頼みごとが全部 todo になること、前提外れ（練習 AMI の `.local` など）が alert で出ること、merge がレーン 1 だけで済むこと、`task lane:*` が許可待ちで止まらないことを確かめる。確認済みは「同じマシンのローカルセッションが `ListAgents` に出る」「入力待ちのセッションがメッセージで 1 ターン起きる」の 2 点だけ。**次の練習で 3 セッションを 1 人で通し、`[HELLO]` / `[PR]` / `[MERGED]` がレーン 1 に届くこと、無関係なセッション（名前の似た別の練習・大会）に 1 通も行かないこと、`.claude/settings.json` の許可で読み取りコマンドが止まらないことを確かめる**。

- 2026-10-25 追加分（未検証。次の練習で確かめる）: **盤のレビュー todo の決定ボタン**（`task lane:todo … PR=<n>` → web に PR リンクと「✅ merge OK」「✋ 要修正」、`task lane:wait` の `decision=ok|changes|done`、決定は web の POST だけが書けること）、**ベンチの帯**（`task lane:bench`、ship.sh の `ready`、runs:wait の `running` / `none`、`NEXT` の積み方）、**docs だけの PR の ship**（deploy・smoke を飛ばして `ops:repo` だけ、ベンチの todo なし）と衝突した PR を飛ばす `hub:ship`、H3〜H5 の自動 done、**hosts:setup の並列化**（apt を待たない停止、join の前半を init と並行、`BENCH_CMD` の提案行）、ops panel の App analysis のタブと `/app/?ref=<branch>`。ルールの変更（journal はハブだけ、練習専用の前提は main に直接、`[PICK]` の後は `[DECIDED]` まで実装しない、質問の todo はハブだけ）も次の練習で守られるかを見る。再起動試験（RUNBOOK §8-3）は 2026-10-25 も未実施。

- 2026-10-27 の練習で確かめたこと: 盤のレビュー todo の「merge OK」ボタンと `lane:wait` の `decision=ok`（PR 約 30 本で使えた）、ベンチの帯（ready / running / none）、docs だけの PR の ship（deploy なし）、衝突した PR を飛ばす `hub:ship`、H3〜H5 の自動 done、4 セッションのメッセージが無関係なセッションに行かないこと。ハブ以外のレーンは `lane:step` を打たないことがあり、ハブが直した。帯とロードマップのずれは人が「ちゃんと判定できていなさそう」と感じたが、どこでずれたかは記録していない。再起動試験は 2026-10-27 も未実施（3 回続けて）。
- 2026-10-27 追加分（未検証。次の練習で確かめる）: GitHub Project `ytakaya/5`（`task project:setup` / `project:add`、Contest / Lane / Status / Priority / Score の付与は 2026-10-27 に 1 件で確認）と issue テンプレート（`.github/ISSUE_TEMPLATE/`、web での表示は未確認）、todo の `CHOICES`（選択肢のボタン、`decision=<選択肢>`）と web の todo の id のバッジ、ship.sh の未 push の commit の検出、`DEPLOY_FIRST` の複数ホスト、`GH_PROJECT` での issue の Project 追加、`docs/reports/latest.*` の placeholder、handoff skill、チーム合流の目安（lanes README §11）、docs だけの PR を帯に関係なく ship すること。todo の `INPUT`（web の入力欄 → POST /input、`lane:wait` の `input.<n>=`）と `ENVKEY`（web のプロセスが `.env` の HOSTS / HOST_LOGIN / BENCH_CMD / GH_PROJECT / CONTEST の行だけを書き換える）、盤の最初の todo t2（IP → HOSTS）/ t3（マニュアルの URL とベンチ）を入力の todo にしたこと（単体テストと一時ディレクトリの盤でのみ確認。実際の起動の流れでハブが t3、レーン 2 が t2 を待って起きるかは次の練習）。
- 2026-10-27 追加分 2（特別賞までを速くする。未検証、次の練習で確かめる）: レビューの todo を「ベンチ 1 回に 1 つ」（`PR="7 8 9"`、トグルで外す、`ok_prs=` / `held_prs=`）、`hub:ship` が journal と claims を自動で書く（`tools/lanes/shiprec.py`）、`task deploy` の並列化（`tools/bootstrap/deploy-all.sh`。`DEPLOY_FIRST` は順番、残りは同時。所要時間を表示）、起動の todo のモデル指定（`tools/lanes/sessions.json`、`task lane:starts`）、「🏁 ベンチ終わった」ボタン → `task bench:done` → panel の `POST /api/bench-done`（全ホストの run を止め、collect → report → score を自動。2 回押しても害なし）、agent 0.3.2 の終わりの内訳（`end: <理由> after <s> s idle; signal +<s> s`）、時間の記録（盤の `events.jsonl`、Claude Code の hooks の `cc-events.jsonl`、任意で `task otel:up`）と `task lane:timeline`、`[TASK] 3 early-fix`（baseline の前の index と N+1）、`[TASK] 2 config-options`（構成の 3 案を 1 回で選ぶ）、練習のベンチの準備をレーンから外したこと（人が事前に済ませて t3 に書く）、`lane:timeline` の「止まっていた時間の内訳」（Claude Code の idle × 盤の状態、特別賞までの idle 分、3 分以上の停止とその時のハブ）、練習の盤の任意の todo `t0`（`task otel:up`。起動の CMD は `.env` の `LANES_OTEL` を表示のたびに読む）。**反映に要る操作（一度だけ）**: `task ops:deploy`、`task host:agent HOST=1|2|3`。
- **2026-10-27 夜の練習（ISUCON14、URL → 特別賞 21 分）で分かったこと**（原本 `docs/retro/2026-10-27b-practice-feedback.md`）: ship の停止はハブが kickoff phase 2 を main の checkout で走らせたため（phase 2 は worktree で）。帯が running → ready に戻ったのはハブの `task lane:bench STATE=ready … NEXT=` が原因（NEXT だけなら STATE を付けない）。`task lane:status` などが 20 s かかったのはツールではない（実行 0.1 s、Claude Code の Bash 1 回の前後の中央値はどれも 1.3〜2.3 s、20 s の外れ値は Edit / SendMessage にも出た）。`git push` は許可されておらず自動モードの判定に回っていた（許可に追加、`--force` / `-f` は禁止）。
- 2026-10-27 夜の report: DB ホストの nginx に外部スキャナの 1 件が入り、load window が 1 s になって svc3 の verdict が「idle 100%」と出た（実際は mysqld 190%）→ HTTP の少ないホストは最も忙しいホストの窓を使う（`fleetLoadWindows`）。「ベンチ終わった」の後、`meta.json` ができる前の timeline の行に req が無く、runs:wait が「req 0」と出した → agent の数を `pending` で返し、collect は meta を書いてからホストのコピーを消す。panel の画面: 「ベンチ終わった」は常に見える大きな帯（スコアは入れない）、スコアは run ごとの大きな入力欄と FAIL ボタン、delete は ⋯ の中で ts を打たないと消えない。**反映: `task ops:deploy`**。
- **2026-10-28 の練習（ISUCON14、URL → 特別賞 15.9 分、ベンチ 2 で越えた）**: ベンチ 1 → 2 の 8 分は PR の準備 3 分と止まり約 4 分（analyzer の設定が無い smoke の失敗、`git -C … push` の許可待ち 2 分、docs だけの ship を先に出した）。新しい index（`coupons(used_by)`）でまた deadlock（2 回目）→ `task index:check` と hub:ship の保留。setup の B / D と early-fix は `webapp/` が来た時点で始める。push は `task push` / `task pr:create`。lane:web は long-poll（todo が約 0.1 秒で出る）と通知の音。
- run が「running のまま」に見えた件（2026-10-27、練習の 10 run で確認）: agent の終わりの判定は正常（負荷が止まってから 10〜12 s で終了、影の run も 0〜3 s で止まる）。長かった 1 本（118 s）は MySQL の負荷で `/initialize` の最中に始まった run。見えにくかったのは panel の Runs の表が 30 s ごとの更新で、collect / report が手動だったため → ベンチ終わったボタンと 5 s ごとの更新に。

## 6b. モデルの使い分け（推奨）

| 役割 | モデル | 理由 |
|---|---|---|
| **レーン 1 ハブ**（当日の操作セッション） | **Opus 5.5 + fast mode**（`claude --model claude-opus-5-5`、起動後 `/fast`） | 短い判断を何十回も返す役で、返答の速さが周回の速さに直結する（2026-10-27 の練習で改める。読み違いはモデルではなく前提の渡し方の問題だった） |
| kickoff の前提の確認、構成の判断、最終レビュー | **Fable 5.1**（ハブのサブエージェント） | 判断の質が失格とスコアに直結する。回数が少ないので遅くてよい |
| **レーン 2 インフラ / レーン 3 アプリ** | **Opus 5.5**（`claude --model claude-opus-5-5`） | 設定・deploy の順・応答の形を変えない書き換えの精度。速さは parallel-fix の並列で稼ぐ |
| **レーン c スカウト**（deep-dive、設計、二重レビュー） | **Fable 5.1**（`claude --model claude-fable-5-1`） | 速さを急がない役。2026-10-27 に失格の芽（追試直後の `/initialize` 500、再起動で DNS の答えがずれる）を見つけた |
| parallel-setup / parallel-fix の**サブエージェント**（index、N+1、設定、app-map の執筆） | **Opus 5.5**（Agent ツールで `model: "opus"`） | 作業が定型でプロンプトに根拠が揃っている。速くて安く、1M コンテキストでファイルを多く読める。練習（isucon13）で DB 分離と parallel-fix を問題なくこなした |
| 小さな編集、ログ整形、app-map や handoff の表の書き起こし | Sonnet 5 か fast mode（`/fast`、Opus） | 根拠が揃っていて書き起こすだけ。待ち時間を減らす |

表は `tools/lanes/sessions.json` が持ち、起動の todo（`task lane:starts`）の CMD に入る。変えるときはそのファイルだけ。**エージェントの列**: 各レーンの `"agent"` は `claude`（既定）か `codex`（OpenAI Codex CLI。モデルは `codex.model`、既定 `gpt-6.1-sol`。起動は `codex --model … --approve-for-me -c sandbox_workspace_write.network_access=true --add-dir "$PWD/.." '<1 行>'`）。codex のレーンは `AGENTS.md`（`task agents:md` が CLAUDE.md から作る）を読み、合図は盤の `task lane:msg`（`docs/lanes/README.md` §12）。ハブは Claude のまま、Codex は 2 / 3 / c から試す。次の練習で `task lane:timeline` の Claude Code の節（レーンごとのターンの時間）を見て、合っているか確かめる。

kit の改修（このツール群そのものを直す作業）は Fable。理由は設計判断と落とし穴の記憶の両方が要るため。

## 7. 新しいセッションの始め方

1. このファイル → 当日なら `docs/lanes/README.md` と自分のレーンのファイル（人が起動の 1 行で指定する）、kit の改修なら `README.md` を読む。コンテスト repo なら `CLAUDE.md` の「This contest」節と `docs/kickoff.md` も。
2. `task env:show` で接続先（OPS_HOST、HOSTS、CONTEST）を把握。`git log --oneline -10` で直近の状態。
3. 人からの依頼が「詰まった」なら、まず `docs/RUNBOOK.md` の該当節と `/tmp/isucon-setup-<ip>.log` / `task host:diag` の出力を求める。推測で直さない。
4. kit を直したら §4 の検証 → commit/push → コンテスト repo へ同期（§3 の対象だけ）→ 人に「反映に必要な操作」（`task ops:deploy`、`task host:agent`、`git pull`）を 1 行で伝える。

開始時の一言（人が貼る）: **「docs/HANDOFF.md を読んでから始めて」**
