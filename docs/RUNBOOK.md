# RUNBOOK — 競技当日の手順（3 台 + ops 1 台）

ssh でサーバーに入るところから最終ベンチまで。コマンドは全部手元（この repo のルート）で打つ。`task --list` で一覧が出る。
時刻は ISUCON 本番（10:00 開始）基準。練習ではそのままずらす。

**当日は 1 人（A）が Claude セッションを 4 枚、10:00 から終わりまで回す**（レーン 1 = ハブ、レーン 2 = インフラ、レーン 3 = アプリ、レーン c = スカウト）。時刻表では動かさない: 各セッションは出来事（メッセージ、人の `done`、run）で動き、状況を **盤**（手元の PC だけの保存場所。人は `task lane:web` か `task lane:watch` で見る）に出す。セッションのルールは [`docs/lanes/README.md`](lanes/README.md) と各レーンのファイル（[1-hub](lanes/1-hub.md) / [2-infra](lanes/2-infra.md) / [3-app](lanes/3-app.md) / [c-scout](lanes/c-scout.md)）。このファイルの §1〜§9 は、レーンのファイルからリンクされる手順の詳細。

人ごとの仕事（開催前の準備、当日、終わった判定）は **[docs/ROLES.md](ROLES.md)**。

| 役割 | 担当 | 当日の仕事 |
|---|---|---|
| A（オペレーター） | | 4 枚のタブを起動し、盤の todo を上から片付ける（`.env`、`! task hosts:setup`、ポータルのベンチと collect / report / note、PR の OK）。話す相手はレーン 1 だけ |
| B（守り） | | マニュアル（追試・禁止事項）、PR レビュー（ROLES の 4 観点）、14:00 と 16:30 の再起動試験 |
| C（読みと判断） | | マニュアル（スコア式・猶予・自由をくれる文）、`[PICK]` の要確認に答える、report を読んで構成の判断の材料 |

<a id="lanes"></a>
## 起動と、人に見えるもの

先に盤を開いておく（どちらか。両方でもよい）。新しい repo の盤には最初の todo が 3 つ出る（t1 ハブの起動、t2 3 台の IP、t3 マニュアルの URL とベンチ。t2 / t3 は web の入力欄に入れて送信する: t2 は盤が `.env` の HOSTS を書く。タブには貼らない）。ハブが名前を出すと、2 / 3 / c の起動の todo が名前入りで足される:

```
task lane:web
```
```
task lane:watch
```

web はブラウザの盤（todo ごとに 📋 コピーと ✅ 完了、alert ごとに「直ったら」と解消のボタン）。web の **🗺 ロードマップ**には、レーンごとのやること（`tools/lanes/plan.json`）と進み具合、依存の矢印、特別賞（`alp.yml` の `prize_target`）までのクリティカルパス（金色）、最新・最高スコアと目標の % が出る（ベンチの step はスコアの入った run から自動で埋まる）。watch はターミナルの全画面の盤（流れない。数字キーで todo のコマンドをコピー、`q` で終わる）。1 件だけコピーしたいときは `task lane:copy ID=t5`。

4 枚とも repo のルートで `claude` を起動し、1 行を貼る（レーン 1 を最初に。2 / 3 / c にはレーン 1 が出した名前を入れる。1 行は [lanes/README.md §9](lanes/README.md)）。

人が見るのは 2 つだけ:

| 見る所 | 何が出るか | 人がすること |
|---|---|---|
| **盤**（`task lane:web` / `task lane:watch`） | **ベンチの todo**（main <sha>: 載っている PR / 次に回す PR。終わったらその todo の「🏁 終わった」。ベンチの帯 `ready` / `running` / `collect` は `lane:watch` / `lane:board` にだけ出る。web には出さない）、phase、4 レーンの状態（working / waiting-human / waiting / alert / idle と今の手順）、open の **alert**（想定外のこと、影響、直ったら誰が何で消すか）、open の **todo**（人にしかできない操作。コピーできるコマンド、理由、終わったら何をするか。PR のレビューは PR へのリンクと「✅ merge OK」「✋ 要修正」のボタン） | ベンチの todo が出たら**即**ベンチ（他の PR を待たない）。終わったらその todo の「🏁 終わった」。todo を上から片付ける。PR は読んでボタンを押す（タブに OK と打たなくてよい）。alert はレーン 1 のタブで相談する。「許可待ち」のレーンがあればそのタブを開いて許可する |
| **レーン 1 の返答** | 判断の材料（PR の一括レビューの表、`[PICK]` の要確認、run の判定、次に回す手順） | OK / 拒否 / 答えを言う。ベンチの ts を貼る |

レーン 2 / 3 / c のタブを開くのは、todo の AFTER に「レーン <n> のタブに」とあるときと、許可の prompt だけ。

出来事の順（時刻ではない。詳細は [lanes/README.md §10](lanes/README.md)）:

1. レーン 1 が kickoff ①（マニュアル）→ todo（`.env` の提案値）。レーン 2 が HOSTS の todo → `task host:peek` → `! task hosts:setup` の todo → `task deploy:init`（+ 練習専用の前提）→ `[DONE] servers`。
2. baseline ベンチの todo と帯 `ready` は **hosts:setup が 0 NG で終わった時点で盤が自動で作る**（deploy しない初期状態。deploy:init と `[DONE] servers` を待たない）。レーン 1 は `git pull` / `task ops:repo` → 同じターンで `[TASK] 2 setup` / `[TASK] 3 app-map` / `[TASK] c early-scan`、kickoff ②（前提の確認）はバックグラウンド。
3. baseline の ts → レーン 1 が `actions:plan` → `[TASK] 3 fix` / `[TASK] c technique-scan`。OK の出ている PR（setup → fix → c）を `task hub:ship` → **ベンチの todo**。
4. 以後のループ: run → レーン 1 が判定と次の `[TASK]`（2 = 構成・tuning、3 = parallel-fix、c = deep-dive（3 回目以降）/ technique-scan）→ PR → `hub:ship` → ベンチ。idle のレーンには「手が空いたら」から仕事を出す。
5. 時刻の区切りはレーン 1 だけが見て `[PHASE]` で配る: 12:30 technique-scan 2 周目（c）、15:00 tuning（2）、16:00 凍結、16:30 再起動試験（§8 を todo に）、17:00 最終レビュー（c）。

ループで回す skill（誰が回すかはレーンのファイル）:

| skill | いつ | レーン |
|---|---|---|
| `parallel-setup` | `[DONE] servers` の直後（baseline を待たない。merge は baseline の後） | 2 |
| `app-analysis` | `[DONE] servers` の直後 | 3 |
| `parallel-fix` | 毎回の run の後（ops グループはレーン 2） | 3（2） |
| `technique-scan` | 早期モードは `[DONE] servers` の後、run ありは baseline の後と 12:30 | c |
| `deep-dive` | 3 回目以降の run、P1 が尽きた、wait-bound、fail や遅さの理由が読めない | c |
| `next-step` | run の後、Next step の上位が拮抗した | 1（忙しければ c） |
| `tuning` | 15:00〜15:45 だけ | 2 |
| `score-sprint` | 特別賞の目標の 75% を越えたとき | 1（PR は 2） |

**ベンチの後（毎回）にハブが** `task app:refresh TS=<ts>` → `docs/reports/` だけの commit を main に push → `task ops:repo`（ops パネルの `/app/` の「今解決すべきこと」がその run で更新される。`hub:ship` が走っていない間に。**人は打たない**: main の checkout に書くので、ハブの ship と競合する）。parallel-fix / deep-dive / next-step はそれを読む（自分の worktree で打ち直してもよいが push しない）。panel の App analysis のタブに状態（まだ無い / PR レビュー待ち / いつの巡回か）が出る。**merge の前でも `/app/?ref=<branch>` でそのブランチの report が見られる**（app-map の PR は docs だけの fast lane なので、ハブが OK を待たずに merge してよい）。

<a id="s0"></a>
## 0. 前日までに済ませること

別ページ: [`docs/PREP.md`](PREP.md)（repo 作成、`.env`、deploy key、ops の起動と `task ops:deploy` / `task ops:env`、手元の道具、ssh 鍵、Basic 認証の共有、**Claude の許可設定**）。許可設定は 1 人で 3 セッションを回す前提で要る: `.claude/settings.json` の読み取り専用の許可が入っているか見て、練習の後に `/fewer-permission-prompts` で増やす（読み取り専用だけ。ssh、rm、systemctl、mysql の書き込み、main への push、merge、deploy は足さない）。当日の朝に残っているものがあれば先にそれを片付ける。

<a id="s1"></a>
## 1. 開始直後

[§1](#s1)〜[§4](#s4) はレーンのファイルが指す手順の詳細（出力の意味、詰まったときの対処、裏側で何が起きるか）。誰が何をいつ打つかは [docs/lanes/](lanes/README.md)。

<a id="s1a"></a>
### 1-A. kickoff ① マニュアルを Claude に読ませる

| 担当 / レーン | 所要 | 終了条件 |
|---|---|---|
| レーン 1 | 10 分 | `docs/kickoff.md`（下書き）が commit され、`.env` の提案値と人の todo が盤に出ている |

サーバーに入る前にできる。Claude が URL から `docs/manual/day.md` と `app.md` を取って commit する（GitHub は raw に変換、ログインが要るポータルは「本文を貼ってください」と聞いてくるので貼る）。続けて `docs/kickoff.md` の下書きを書く: アクセス方法（ホスト名、hosts 行、TLS）、ログイン方法、スコア式と fail 条件、整合性の猶予、変更禁止事項、初期化 API のパス、言語ディレクトリ名、去年との差分、去年と違うインフラ要素の有無。あわせて **`.env` に入れる値の提案**（`TRIGGER`、`LANGS`、`HOST_LOGIN`）と **人がやること**（チームのブラウザ用 hosts 行、ポータルで確かめること）を `task lane:todo` にする。A は盤の todo を片付ける（`.env` に反映）。

<a id="s1b"></a>
### 1-B. サーバーに入る

| 担当 / レーン | 所要 |
|---|---|
| レーン 2（todo を A が実行） | 5 分 |

ポータルに出た 3 台の IP を盤（`task lane:web`）の todo t2 に入力して送信する（盤が `.env` の `HOSTS` の行だけを書き換える。`.env` を手で直してもよい）。これが当日 IP を手で扱う唯一の場面で、以後のタスクは `HOST=1|2|3`（省略で svc1）で動く。

```
HOSTS=<IP1> <IP2> <IP3>     # 1 台目が init（git の起点）、残りが join
```

`task host:peek`（= 1 台目。`HOST=2` で他の箱）の出力で見るのは「アプリのユニット名と ExecStart」「言語ディレクトリ（`webapp/go` など）」「DB が MySQL か」「nginx か」「Docker で動いていないか」。残りの `.env` は前日（[PREP](PREP.md)）に埋めてあるはずで、当日に見直すのはこの 2 つ:

```
HOST_LOGIN=isucon           # ssh で入るユーザー（練習 AMI は ubuntu）。peek が通らなければここ
TRIGGER=^POST (/api)?/initialize   # 初期化 API のパス（1-A の提案値）
```

（`HOST_USER` / `CONTEST` / `GIT_REMOTE` / `LANGS` / `SSH_KEY` / `OPS_HOST` は前日分。空なら PREP を参照）

ポータル配布の鍵で `ssh isucon@IP` が通らない場合は配布された鍵ファイルを `SSH_KEY` に指定する。`task ops:env` の後、Fleet に 3 台の IP が並ぶ（agent 未導入なので赤で良い）。

<a id="s2"></a>
## 2. 3 台を一括セットアップ

| 時刻 | 担当 / レーン | 所要 |
|---|---|---|
| setup | レーン 2（A がレーン 2 のタブで `! task hosts:setup`） | 約 1 分 + 確認 |

`task hosts:setup` がやること: 全ホストを**同時に**始める。1 台目は bootstrap（init: repo の起点、merge → push）+ agent + ops 鍵、残りは bootstrap（join の前半 `PHASE=prep`: apt 停止・alp/slp・nginx ltsv・slow log）+ agent + ops 鍵 → 1 台目の push を待って git の join（`PHASE=git`）だけ。各ホストの全出力は `/tmp/isucon-setup-<ip>.log`、画面には `NG` / `##` と、ホストごとの所要秒数 1 行（`svc1 (ip): total 40s — bootstrap 35s (1 git 1s, 1b apt 0s, 2 tools 3s, …) | agent 3s | …`。遅い節が一目で分かる）。練習（`.env` の `PRACTICE=1` か `HOST_LOGIN=ubuntu`）で ISUCON13 なら、最後に svc2 から回す `BENCH_CMD=…` の 1 行を表示する（`.env` は書き換えない。貼るのは人）。bootstrap は `/home/isucon` を git 化してテンプレート repo と merge → push、`etc/` に nginx/mysql/systemd 等の設定を取り込み、alp/slp 導入（apt は使わない。duckdb は手元のみ）、nginx を ltsv ログに、MySQL スローログ on、`docs/server-info-<host>.txt` を書く。最後に `task ops:env` 相当（isucon.env の書き直しとトンネルの張り直し）も自動で走る。

**`NG` が出た行だけ読む**。目安は svc1 が 30〜40 秒、3 台で 1 分前後（apt を使わず、データファイルは push しない）。2026-10-25 は全体 181 s（svc1 の「1b. stop background apt」だけで 93 s、svc2 / 3 の join は svc1 の init の後）。apt を待たない停止と、join の前半を init と並行に流す形に直した（未検証。HANDOFF §6）。練習のときは最後に `BENCH_CMD` の提案行が出る（`.env` は人が書く。レーン 2 が todo にする）。よくある NG と対処:

| NG | 対処 |
|---|---|
| `push (deploy key / remote / branch protection?)` | deploy key が write 権限か、`GIT_REMOTE` が ssh 形式か。手動: `ssh isucon@IP1 'cd ~ && git push -u origin main'` |
| `nginx: generated config looked wrong; not applied` | `/etc/nginx/nginx.conf.failed` を見る。`access_log` が `http {}` 直下に無い構成。手動で ltsv の format と access_log を入れる |
| `mysql restart failed` | DB が MySQL でない（server-info を見る）。スローログは手動で |
| `isuXXX is enabled but inactive` | DB 再起動でアプリが落ちた。`ssh isucon@IP 'sudo systemctl start isuXXX'` |
| `fetch/reset from ...`（join 側） | 1 台目の push が失敗している。1 台目を直してから `task host:bootstrap HOST=2 ROLE=join` |

1 台だけやり直す: `task host:bootstrap HOST=1|2|3 ROLE=init|join`、`task host:agent HOST=2`、`task host:authorize-ops HOST=2`（`HOST=` は `.env` の HOSTS の何台目か。`db` = DB ホスト。IP も可）。

その後の `git pull` で手元にサーバーの初期コード（`webapp/`, `etc/`, `env.sh`, `docs/server-info-*.txt`）が入り、`task ops:repo` で ops にも同じ repo が入る（レポートの SQL→ハンドラ対応、run 間の commit 一覧に使う）。確認: Ops Appの Fleet で 3 台が緑（idle）、git 欄にコミットが出る。赤なら `task ops:logs SERVICE=tunnel` で ssh が通っているか見る（`Permission denied` → `task host:authorize-ops HOST=<IP>`、`timed out` → 22 の SG）。

<a id="s2b"></a>
### 2b. kickoff ② サーバーの中身で埋める

| 担当 / レーン | 所要 | 終了条件 |
|---|---|---|
| レーン 1 | 10 分 | `docs/kickoff.md` 完成（前提の確認の表を含む）、`CLAUDE.md` の冒頭と `deploy.sh`、`alp.yml`（`task groups`）が commit されている。前提外れは alert |

前提: `git pull` で `etc/`、`env.sh`、`webapp/`、`docs/server-info-<host>.txt` が手元に来ていること。Claude がやること:

- `docs/kickoff.md` を完成: 構成要素（systemd ユニット、DB、nginx、DNS やワーカー、**各要素のデータの置き場所**（MySQL / SQLite ファイル）、app が Docker で動いていないか）、初動の注意（journald への全リクエスト出力、`init.sh` の中身、ユニットの `Requires=`）。
- `CLAUDE.md` の「This contest」節（アプリ名と unit、付随コンポーネント、サーバーの役割、スコア式 3 行）を埋める。
- `task deploy:init` を実行して `deploy.sh` を作り、systemd ユニットと突き合わせて直す（Docker 起動なら「ネイティブ起動に切り替える」提案も書く）。
- `tools/app-analyzer/analyzer.config.json` を書いて `npm run doctor` を全部 OK にする。`/initialize` が body を読むなら `initializeBody` に入れる（`task smoke INIT=1` と巡回が使う）。
- `init.sh` / `init.sql` が DROP/CREATE するなら、`init.sh` の末尾に `bash "$(dirname "$0")/indexes/apply.sh"` を足して kickoff の commit に含める（別の PR にしない。merge まで index が `/initialize` のたびに消える）。
- 以上を commit して push し、**人がやること**を列挙する（通常はブラウザ用 hosts 行だけ）。
- **計測 OFF の PR とその戻しの PR を常に用意しておく**（レーン 2 が `[DONE] servers` の直後に parallel-setup のサブエージェント E で作る。[§4](#s4)）。`task hosts:measure-off` はホストの上で止めるだけなので、次の `task deploy` が `etc/nginx/nginx.conf` を入れ直して access log が戻る。`[sprint]` の `infra/measure-off` は `deploy.sh` の 2d ブロック `MEASURE_OFF=1` で、deploy のたびに各ホストで `tools/bootstrap/measure-off.sh off`（= `hosts:measure-off` と同じ: agent の停止と disable、slow log の設定の退避と DB の再起動、nginx の access log off、isu* の unit に `MEASURE=0` の drop-in と再起動）を走らせる。`infra/measure-on` はその上に積んだ `MEASURE_OFF=0` の 1 行（revert ではない。deploy が `measure-off.sh on` で戻す）。どちらも merge せずに open で置き、特別賞の 1 本（[§6e](#s6e)）と最終確認（[§8](#s8)）のときにすぐ入れる。

この `deploy.sh` は parallel-setup（[§4](#s4)）の A サブエージェントが DB 分離で使う。

<a id="s3"></a>
## 3. 初回ベンチ = baseline

**ベンチが終わったら（毎回）**: スコアが出たら盤のベンチの todo の「🏁 終わった」（盤の web に帯はもう無い。= `task bench:done [SCORE=<点>] [OUTPUT_FILE=<ベンチの出力>]`）。全ホストの run が止まり、collect → report → score は ops が自動（panel で手で押さない）。2 回押しても害はない。run が見つからない（404）ときは `TS=<ts>`（`task runs` で確かめる）。`task deploy` は `DEPLOY_FIRST` のホストを順番に、残りを同時に出す（出力の各行に `[ip]`、最後に所要時間）。

| 時刻 | 担当 / レーン | 所要 |
|---|---|---|
| baseline | 人（レーン 1 の todo。hosts:setup が 0 NG で終わると盤が自動で作る） | 10 分 |

1. ポータルからベンチを実行する（結果画面のメッセージは **Runs の「bench log」に貼る**。report に「Bench output」節ができ、警告の種類と負荷モデルの行だけが残る。練習は svc2 で `./bench run ... 2>&1 | tee /tmp/bench.out` → `task bench:log TS=<ts> FILE=/tmp/bench.out`）（対象は 1 台目。練習ではベンチを svc2 から回す。app と同じ箱で回すと bench プロセスが CPU を食い、数字が歪む）。agent は `POST /initialize` を見て自動で記録を始め、リクエストが止まったら終わる。手で開始/終了しない。
2. ベンチ結果（スコア、fail 理由）をコピーしておく。
3. Ops App → Runs に新しい run が出る → **run が ended になるのを待って**（ベンチの終了から 10〜30 秒。running の間に collect するとホストのデータが消え、`no run.json` で report が作れず、再 collect もできない。2026-10-21）**collect** → **report**。Runs 表に **score と note（"baseline"）**を入れる。ベンチ出力は bench 欄に貼る。
4. `summary.md` を全員に共有する（Ops Appのリンク。Verdict と Next actions の P1 だけ読めば良い）。**score は panel の欄に入れるだけでよい**: ハブは `task runs`（ts・score・verdict・next の一覧）で読み、`task runs:wait AFTER=<直前の run の ts>` で新しい run に気づく（AFTER は必ず付ける。既定は「score の入った最新の run」）。チャットに ts やスコアを貼る必要は無い。

誤検知した run は Runs 表の **delete** で消せる（ops とホストの両方から消える）。人がアプリを触る・手で `/initialize` を叩く・app-analysis を回す間は、Ops Appヘッダの **「⏸ not benching now (30 min)」** を押す（全ホストの agent が access log のトリガーを 30 分無視。自動で戻る。「▶ resume detection」で即復帰）。押し忘れても 100 リクエスト未満の run は自動で破棄される。

ベンチが記録されない時: まずヘッダが「detection paused until …」になっていないか。 `task hosts:status` で agent が `running` になっているか。`TRIGGER` が合っていない（初期化のパスが違う）、またはアクセスログが ltsv になっていない（`ssh isucon@IP 'tail -1 /var/log/nginx/access.log'` に `reqtime:` があるか）。

<a id="s4"></a>
## 4. parallel-setup

| 時刻 | 担当 / レーン |
|---|---|
| `[DONE] servers` の直後（baseline と並行。merge は baseline の後） | レーン 2（`[TASK] 2 setup`、A・B・D・E）。app-map はレーン 3（`[TASK] 3 app-map`） |

サーバーの準備が終わったら（baseline を待たずに）、定型のインフラ作業（DB 分離、計測パッチ、free wins）を **1 つの指示**で PR まで作る。merge は baseline の後。人が触るファイルは無く、渡す情報も無い（svc3 の private IP は Claude が `task hosts:ips` で引く）。Claude がやること（`tools/parallel-setup/SKILL.md`）:

| サブエージェント | ブランチ | 触るファイル | 中身 |
|---|---|---|---|
| A DB 分離 | `setup/db-split` | `etc/mysql/`、`env*.sh`、`deploy.sh`、`webapp/sql/` | svc3 の MySQL に `bind-address`、リモート権限、ホスト別 env.sh、`init.sh` の DB ホスト。付随 DB（DNS 等）は svc1 に残す |
| B 計測パッチ | `setup/measure-patch` | `webapp/go/` | sqltag + pprof/expvar（`127.0.0.1:6060`）。性能変更なし |
| D 小技 | `setup/free-wins` | `webapp/go/`（接続まわり）、`etc/systemd/`、`etc/nginx/` | interpolateParams、pool、起動時の DB 待ち、フレームワークのログ off、LimitNOFILE / GOGC、nginx keepalive と worker 上限（TECHNIQUES §8。ハンドラは触らない） |
| E 計測 OFF / ON | `infra/measure-off`、`infra/measure-on`（`[sprint]` の PR 2 本。integ に入れず open のまま） | `deploy.sh` の 2d ブロックだけ | `MEASURE_OFF=1` / `0` で deploy のたびに `measure-off.sh off` / `on`。特別賞（[§6e](#s6e)）と §8 でそのまま merge する（[§2b](#s2b)） |
| C app-map | `setup/app-map` | `docs/app-map.md`、`docs/reports/` | **svc2 に向けて**（`resolve` = svc2、`initialize` 付き。svc1/svc3 の作業やベンチと干渉しない）`npm run analyze` → 8 節の app-map |

A と B（と C）を `integ/setup` に merge → build/vet → **PR を作ってリンクを貼り、止まる**。人がレーン 1 で OK と言う → **レーン 1 が** `gh pr merge` → `task deploy`（DB ホスト = HOSTS の 3 番目を先に配る。権限 SQL が先に当たらないと app 側が MySQL Error 1130 で止まる）→ `task smoke IP=<svc1> INIT=1 DNS=1`（10 秒。initialize 200・ログイン・主要 GET・DNS）→ **「ready for bench」で止まる**。smoke が落ちたら `task host:diag HOST=1` で切り分け（ユニット、journal、ポート、etc の権限）。C は docs だけなので merge して push。最後に「変更ファイル / 確認できなかったこと / 2 台構成で壊れる懸念」の表。

**抜け防止**: 練習で §4 を飛ばして §5 に進んだことがある。`grep -E '^DB_(HOST|PRIVATE_IP)=' deploy.sh` が空なら A（DB 分離）が未実施。merge 後に埋まっていることが §4 の終了条件。

**人の仕事はベンチだけ**（2 回目）。確認: Fleet で svc3 の mysqld が CPU を使い svc1 の mysqld がほぼ 0%、report の Verdict 下に pprof top、SQL 表に「called from」、`docs/app-map.md` を Discord に貼る。この 2 回目の `summary.md` が [§5](#s5) の材料。

うまくいかないとき: DB 分離だけ失敗したら `git revert` でその merge を戻し、A のエージェントに差し戻す（計測パッチと app-map は残る）。手で確認したいときは `task host:ssh HOST=3` → `sudo mysql -e "SELECT host FROM mysql.user WHERE user='isucon'"`、svc1 で `grep DB env.sh`。

<a id="s4c"></a>
## 4-C. レーン c（スカウト）の先読みの手

| いつ | 担当 / レーン | 終了条件 |
|---|---|---|
| `[TASK] c manual` から 2 回目のベンチまで | レーン c（[c-scout.md](lanes/c-scout.md)）+ サブエージェント。人はレーン 1 経由でしか見ない | 先読みの手 1〜3 本が PR になり、2 回目のベンチの前に merge されている |

なぜ: 2026-10-18 の練習（ISUCON14）で一番伸びた 2 手（マッチングの作り直し 8,188 → 30,866、`retry_after_ms` 30 → 100/200 ms で 35,661 → 48,381）は、マニュアルとコードだけで読めたのに、入れたのは 3〜4 回目のベンチの後だった（report には P1 で出なかった）。型ごとの候補は [`docs/FIRST-HOUR.md`](FIRST-HOUR.md)。

1. `[TASK] c manual`（kickoff phase 1 の commit の後）: main の checkout では checkout / pull をせず、`git fetch` と worktree だけ。`docs/FIRST-HOUR.md` と origin/main のマニュアルを読み、問題の型を選ぶ。
2. `[TASK] c early-scan`（`webapp/` が origin/main に来た後）: `technique-scan skill を早期モードで（run 無し）`。kickoff phase 2 が書く `docs/kickoff.md` の「先読みの手」があれば突き合わせる（片方にしか無い手は理由を書く）。
3. **レーン c が自分で作業単位を選ぶ（最大 3）**。選び方は technique-scan 早期モードの決まりと `docs/FIRST-HOUR.md` のとおり。外すもの: 整合性の猶予に触るのにマニュアルの秒数が書けない手、クリティカルのある手（決済、通知の順序）で 30 分で検証できないもの、index と N+1（レーン 2）。選んだら `docs/technique-scan-early.md` の「レーン c の作業単位」に 1 行ずつ理由を書き（ブランチ `scan/early`）、レーン 1 に `[PICK]` を 1 行。**レーン 1 が表と todo で人に見せ、人は拒否と要確認の答えだけ返す**（`[DECIDED]`）。**`[DECIDED]` が来るまで実装しない**（設計と調査まで。要確認の無い単位も人の拒否を待つ。2026-10-25 に先に実装して PR を出し、質問の todo もレーン c とハブで 2 重になった）。人への質問の todo はハブだけが作る。拒否された単位は worktree と claims の行を捨てる。
4. 1 単位 1 worktree・1 ブランチ（`early/<slug>`）。書く前に `docs/claims.md` に宣言（owner = `lane c`）。`go build` / `go vet` → `task pr:submit DIR=../wt-<name> TITLE=… BODY=… MSG=…`（本文に「先読みの手」の行を貼る。CLAUDE.md ルール 1 の根拠はこの行）→ 止まる。fast lane（CLAUDE.md ルール 4）に入るものはタイトルに `[fast]`。全部揃ったらレーン 1 に `[PR]` を 1 行（複数を 1 行に）。
5. レーン 1 の一括レビューに載る。merge はレーン 1 が parallel-setup → parallel-fix → レーン c の順で行う（[lanes/README.md §7](lanes/README.md)）。衝突したら `[TASK] c rebase #<n>` が来る。
6. 2 回目のベンチの `[RUN]` で手ごとに効いたかを判定し、`docs/journal.md` に 1 行ずつ。効かなかったものは `git revert -m 1` の PR。

<a id="s4c-team"></a>
**注: チームで回すとき**（C か B がレーン c を持つ）。レーン c は C の手元の clone の Claude セッションで回す（A の main の checkout は触らない）。違いは 2 つだけ: 3 の作業単位は C が自分で見て決める（`[PICK]` は不要。要確認は C が自分で答える）、PR の OK は B のレビューの後に C がレーン 1 に言う（merge はレーン 1 のまま）。盤の更新（`LANE=c`）と `[HELLO]` / `[PR]` / `[NEED-OK]` は同じ。

<a id="s5"></a>
## 5. 作戦会議① → 最初の一括実装

| 時刻 | 担当 / レーン | 所要 |
|---|---|---|
| 2 回目のベンチの後（phase = loop） | レーン 1 が人と決め、`[TASK]` を配る | 15 分 + 実装 30 分 |

材料: `task app:refresh TS=<ts>` で更新した app-analysis の report（**今解決すべきこと** タブ: 割合 × 点との関係の順、ルール、issue / PR の状況、コードの行）と、2 回目のベンチ（DB 分離 + 計測パッチ後）の `summary.md`（Verdict、**Next step**（次に回す手順の候補と理由。ルール判定。panel の Runs にも 1 位が出る。上位が拮抗したら `next-step skill で。対象 run は <ts>`）、Where the time goes、**Flows**（点を生むループの 1 周と一番遅いステップ、どこで離脱したか）、Next actions。pprof と called from 付き。人が流れを一目で見たいときは `task flows TS=<ts>` の Sankey 図）、`docs/app-map.md`、マニュアルのスコア式。決めること: ボトルネックの仮説を 3 つ、担当（ファイル単位）、構成（[§6a](#s6a)。DB 分離は済んでいるはず）。

Next actions が 5 個以上並んでいたら、1 つずつ直さずに **parallel-fix** で一括実装する（1 周目の定番改善はほぼ全部これで片付く）。issue 化（[§5-B](#s5b)）は、並列実装に乗らない方針ものや、後で人が引き取るものに使う。

<a id="s5a"></a>
### 5-A. parallel-fix の動かし方

| 担当 / レーン | 所要 |
|---|---|
| レーン 2（`[TASK] 2 fix TS=<ts>`）が実装、レーン 1 が plan と merge | 30 分 |

```
task actions:plan TS=<ts>                 # ops から actions.json を取り、docs/plans/<ts>.md を書く（LLM なし、数秒）
cat docs/plans/<ts>.md | head -40         # グループ（indexes / file:<x>.go / endpoint:… / ops）と各 action の evidence・where・change を目視
git add docs/plans && git commit -m "plan <ts>" && git push
```

ここで人が決めるのは 2 つだけ。**やらないグループを消す**（例: 方針が決まっていない `endpoint:` 群、DNS の `records` のように別 DB の index）と、**MIN**（既定 P2。P1 だけにするなら `MIN=P1`）。消すときは plan の該当節を削って commit する。

レーンで回すときは、レーン 1 が plan を commit して `[TASK] 2 fix TS=<ts>` を送るだけ（レーン 2 は worktree の上で動く）。1 枚で回すときは Claude Code を **main の clean な作業ツリー**で開き、次のブロックをそのまま貼る:

```
parallel-fix skill で docs/plans/<ts>.md を実装して
```

Claude（オーケストレーター）がやること。人は見ているだけで良い:

1. plan を読み、`docs/claims.md`（origin/main）の `open` の行と各グループの関数を突き合わせ（同じ関数なら人に聞く）、**グループごとに 1 サブエージェントを worktree で同時起動**する。各サブエージェントは書き始める前に自分の行を claims に足して main に push する（このファイルだけの commit。CLAUDE.md ルール 4 の例外 b）（`../wt-fix-<group>`、ブランチ `fix/<ts>/<group>`）。各サブエージェントは自分の節と担当ファイルだけ読み、実装 → `go build ./... && go vet ./...` → commit。デプロイとベンチはしない。
2. 全部終わったら `integ/<ts>` にグループごと `--no-ff` で merge（`ops` は最後）し、build / vet → push → **PR を作ってリンクを貼り、止まる**。衝突が出たら該当グループに差し戻して解消させる。小さいグループ（設定や DDL だけ）はサブエージェントを使わず自分で直す。
3. 人が PR を確認して OK → レーン 1（ハブ）が `gh pr merge` → `task deploy` → `task smoke INIT=1` → **「ready for bench」と言って止まる**。`indexes` グループだけの PR のように fast lane（[§6](#s6)。index は読む SELECT が全部 ORDER BY 付きのときだけ）に収まるものは `[fast]` を付けて OK を待たずに進めてよい。
4. 最後に「グループ / 変更ファイル / done・skipped の一覧 / 2 台構成で壊れる可能性」を表で報告する。

人の仕事はここから:

```
# ポータルでベンチ → Ops Appで collect → report → score / note（note は "fix/<ts>: indexes, livecomment, livestream"）
```

結果の `summary.md` のリンクを Claude に渡して「グループごとに効いたか判定して」。効かなかった／壊れたグループは `git revert -m 1 <merge commit>` の PR を出してもらい、OK → merge → 再デプロイ・再ベンチ。効いたものはそのまま main に残る。

所要の目安: plan 1 分、サブエージェント 10〜15 分（並列）、統合とデプロイ 3 分、ベンチ 3〜5 分。**11:30 までに 1 周目が終わる**のが目標。2 周目以降は [§6](#s6) のループで、P1/P2 が再び 5 個以上溜まったらもう一度 parallel-fix。

詰まりやすい所:
- サブエージェントが `go build` で止まる → 大抵は共有ヘルパーのシグネチャ変更。「レスポンスの形を変えない、他ファイルを触らない」がプロンプトに入っているので、差し戻せば直る。
- `indexes` グループの DDL が `/initialize` で当たらない → `webapp/sql/init.sh` が流す SQL ファイルに入れるか、initialize ハンドラで `ALTER TABLE`（冪等に）。initialize の制限時間内に収まるか確認。
- worktree が残る → `git worktree prune`。ブランチは `fix/<ts>/*` で揃えてあるので後で消せる。

<a id="s5b"></a>
### 5-B. issue にする

| 担当 / レーン |
|---|
| 並列実装に乗らないものだけ、LLM なし |

```
task actions:issues TS=<ts> REPO=<org>/<repo> DRY=1   # 何が作られるか確認（タイトル + where）
task actions:issues TS=<ts> REPO=<org>/<repo>         # P1/P2 を issue に。MIN=P3 で全部
```

issue 本文は自己完結（Evidence = 数字と SQL、Where = ハンドラ / 呼び元、Change = DDL、How to verify = デプロイ → ベンチ → report の同じ行が下がる）。同じタイトルの issue があればスキップするので、ベンチごとに叩いて良い。**Claude に実装させるときは issue 番号を渡すだけ**（`gh issue view N`）。"Speed up <endpoint>" のような方針が要る issue だけ、人か Claude が `docs/app-map.md` の該当行を見て方針を 1 行足す。

<a id="s6"></a>
## 6. 改善ループ

| 時刻 | 所要 |
|---|---|
| 11:00〜16:00 | 1 周 10〜15 分 |

```
task wt:add NAME=<name>    # 1 人（1 AI セッション）1 ブランチ（../wt-<name> + .env の symlink）
# 書き始める前に docs/claims.md（origin/main）を読む → 同じ関数が open なら相手と相談 → 自分の行を足して main に push（手順は docs/claims.md）
# 直す → commit → push → gh pr create --base main --fill → PR のリンクを人に渡す
# 人が OK（ハブのタブか、盤のレビュー todo の ✅ merge OK）→ レーン 1 が task hub:ship（merge → deploy → smoke → ベンチの todo + 帯 ready。merge はレーン 1 だけ）
# 帯が ready / running の間は ship しない。その間の OK は帯の NEXT へ
# （[fast] の PR は OK を待たない: build/vet → merge → task deploy → task smoke INIT=1、落ちたら revert → deploy。下の「fast lane」）
# ポータルでベンチ（A が直列に。ベンチの todo が出たら即。他の PR を待たない。終わったら todo の「🏁 終わった」）
# Ops App: collect → report → score / note を入れる（note = 何を変えたか 1 行）
# 悪ければ revert の PR → OK → merge → task deploy
# merge / 中止したら docs/claims.md の自分の行を merged / abandoned に
```

- **P1/P2 が再び 5 個以上溜まったら 1 つずつ直さず、[§5-A](#s5a) の parallel-fix をもう一周**（plan → 並列実装 → main に merge → ベンチ 1 回）。
- **index を足す PR は `task index:check BRANCH=<branch>` が exit 0 か、PR 本文に `index-check: reviewed`**（`hub:ship` が自動で検査し、リスクがあって reviewed の行が無ければその PR だけ保留する）。
- **OK の後に head が変わった PR は merge しない**: `hub:ship` が盤の merge OK の時刻と PR の commit の日時を比べ、OK の後の commit があればその PR だけ保留して alert（merge は確かめた head に `--match-head-commit` で固定）。差分を見て OK を取り直す（タブで取り直したら `REOK="<n>"`）。タブの OK はハブが OK のときの `headRefOid` を控えて ship の直前に比べる。
- **付随 DB を別ホストに出す PR**は、本文に「必要な DEPLOY_FIRST」（付随 DB のホスト → アプリ DB のホストの順。例 `"2 3"`）を書き、ハブが ship の前に `task env:show` で突き合わせる。**`.env` を変える todo** は、閉じた後にハブが `task env:show` で値を確かめてから次へ（2026-10-28: todo は閉じたが `.env` は変わっていなかった）。
- **fast lane**（CLAUDE.md ルール 4）: 差分が全部次のどれか 1 つに収まる PR は、オーケストレーターが人の OK を待たずに merge してよい。PR は作ってリンクを貼り、人は事後に見る。悪ければ revert（これも fast）。

  | 区分 | 中身 | 機械的な確認（オーケストレーターが PR 本文に貼る） |
  |---|---|---|
  | (a) index | `webapp/sql/indexes/` に**新しいファイルを足すだけ** | `git diff --name-status origin/main...HEAD` が全部 `A webapp/sql/indexes/…`、**かつ** index を張るテーブルを読む SELECT が全部 `ORDER BY` 付き（T20 の出力順の罠。下のコマンドの出力が空）。確かめられない（SQL を文字列連結で組む、`.pdns.sql` のように app の外のクエリ）なら通常の PR |
  | (b) 数値定数 | 間隔・タイムアウト・プールの大きさ・`retry_after_ms` のような値で、**マニュアルがサーバーに決めさせている**もの（見出しを PR に引く） | 下のコマンドの出力が空（数字以外の行が変わっていない） |
  | (c) 設定 | `etc/` の mysql conf.d・nginx・sysctl.d・systemd drop-in だけ（耐久性を IMPLEMENTATION-NOTES §5 より下げる設定は除く） | `git diff --name-only origin/main...HEAD` が全部 `etc/…` |
  | (d) docs | `docs/` の下だけ（`docs/app-map.md`、`docs/app-annotations.json`、`docs/reports/`、`docs/plans/`、scan / deep-dive）。`docs/journal.md`（ハブだけ）、`docs/claims.md`、規則のファイル（`docs/IMPLEMENTATION-NOTES.md`、`docs/lanes/`、`docs/MESSAGES.md`）は除く | 下の (d) のコマンドの出力が空。`hub:ship` が差分を見て deploy・smoke を飛ばし `ops:repo` だけ（ベンチの todo も出ない。2026-10-25: app-map の PR が人の OK と deploy 付きの ship を待って 18 分） |

  対象外（必ず人の OK）: 応答の形を変えるもの、トランザクション・ロック、キャッシュ、スキーマ（index 以外）、マニュアルが明示していない整合性の猶予に触るもの。順番: `go build ./... && go vet ./...` → `task hub:ship PRS=…`（`gh pr merge --merge` → `task deploy` → `task smoke INIT=1` → 「ready for bench」。(d) は merge と `ops:repo` だけ）。smoke が落ちたら `git revert -m 1 <merge commit>`（これも fast）→ `task deploy` → `task smoke`。ブランチごとのデプロイはしない。盤のベンチの帯が `ready` / `running` の間はやらない。PR のタイトルは `[fast] …`。
  ```
  # (a) の確認: 新しい index のテーブルを読む SELECT のうち ORDER BY が無いものを出す。何も出なければ fast
  for t in $(git diff --name-only --diff-filter=A origin/main...HEAD -- webapp/sql/indexes | xargs cat | grep -oiE '(ALTER TABLE|ON) +`?[a-z0-9_]+' | awk '{print $NF}' | tr -d '`' | sort -u); do
    T="$t" perl -0777 -ne 'while (/(`[^`]*`|"(?:[^"\\]|\\.)*")/sg) { $q=$1; print "$ARGV: $q\n" if $q =~ /\bSELECT\b/i && $q =~ /\b(?:FROM|JOIN)\s+`?\Q$ENV{T}\E\b/i && $q !~ /ORDER\s+BY/i }' webapp/go/*.go
  done
  # (b) の確認: 変えた行が数字を除いて同じなら何も出ない
  git diff -U0 origin/main...HEAD -- webapp | grep -E '^[-+][^-+]' | sed -E 's/^[-+]//; s/[0-9]+(\.[0-9]+)?/N/g' | sort | uniq -c | awk '$1 % 2'
  # (d) の確認: docs/ の外か、除外したファイルに触れていれば出る。何も出なければ fast
  git diff --name-only origin/main...HEAD | grep -vE '^docs/' ; git diff --name-only origin/main...HEAD | grep -E '^docs/(journal\.md|claims\.md|IMPLEMENTATION-NOTES\.md|MESSAGES\.md|lanes/)'
  ```
- **ベンチは必ず A が直列に回す**。回す前に「bench: ブランチ / 見たいこと」を Discord に 1 行。
- レポートの見方: Verdict → Where the time goes → Next actions（P1 から）。app CPU-bound なら末尾の pprof 節へ: **flame graph**（クリックでズーム、正規表現で検索）と `pprof -top`。複数台構成にした後は Fleet のホスト別 CPU も見る。
- report の **Data growth** 表（run の開始直後 = initialize 後と終了時の行数、Δ、Δ/s）で、ベンチが書くテーブルとデータ保持チェックの基準値が分かる（agent が DB ホストで自動取得）。
- app-analysis の report は再巡回しない。作戦会議の前に `task retention:from-run TS=<ts>`（run の行数を取り込む。手で数えるなら `task db:snapshot LABEL=latest`）→ `cd tools/app-analyzer && npm run report` で、今のコードと**ベンチ後の行数**で Data model タブと handler 対応を作り直して latest.* を更新（数秒）。性能の所見は report の「Code scan / perf hints」に隔離してあり、正は bench の summary.md。
- 詰まったら（Verdict が「wait-bound」、P1 が尽きた）: `task deepdive TS=<ts>` → Claude に deep-dive skill。ベンチが fail したら `task smoke INIT=1` と `task host:diag HOST=1` を先に（ベンチを回すより速い）。
- 構成変更は [§6a](#s6a) の表に従う（DB 分離は 10:45 に先にやる。2 回目は 12:30〜14:00 に summary.md の数字で判定）。
- スコア推移はOps Appの Score timeline。「どの commit で上がった／下がった」はここで見る。

<a id="s6a"></a>
### 6a. サーバー構成の決め方

| 担当 / レーン |
|---|
| 時間帯別 |

構成変更は 3 回だけ。それぞれ「いつ」「何を見て」「Claude に何を渡して」決める。1 回目（DB 分離）は判断不要なので何も渡さない。2 回目以降は **最新 run の `summary.md`（約 8 KB。ホスト別の Verdict と CPU by process が入っている）** と一言の質問。report.html や生ログは渡さない。

| 時刻 | 構成 | 条件 | Claude に渡すもの / 聞くこと |
|---|---|---|---|
| 10:15 baseline | 初期構成（svc1 に全部） | 無条件。比較の基準を 1 回取る | なし |
| **10:25 DB 分離**（[§4](#s4) parallel-setup のサブエージェント A） | svc1 = nginx + app、**svc3 = MySQL**、svc2 = 空き（練習ではベンチ実行機） | 無条件。コード変更より先にやる。機械的で低リスク、以後の計測で mysqld と app の CPU が箱ごとに分かれて読みやすくなる | 渡すものなし（`docs/kickoff.md` と `etc/` を Claude が見る）。「DB を svc3（private IP）に出す変更を deploy.sh と etc/ で」。env.sh の DB ホスト、`bind-address`、権限、DNS 等の付随 DB を一緒に動かすか |
| **12:30〜14:00 2 回目** | 下の判定表 | index / N+1 を潰した後の run で判定 | `summary.md` + 「この数字で svc2 を何に使うべきか。候補 A/B/C のどれか、根拠と手順と壊れる可能性を」 |
| 15:00 微調整 | nginx keepalive / worker、MySQL バッファ、Redis 同居など | 残り時間で元が取れるものだけ | `summary.md` + 「16:00 までに終わる微調整を 3 つ」 |
| 16:00 | 確定。以後構成は触らない | | |

**2 回目の判定表**（直近 run の `summary.md` の「CPU by process」を箱ごとに見る）

| 観測 | 判定 | svc2 の使い方 |
|---|---|---|
| svc1 の app プロセス ≥ 60%、svc3 の mysqld < 60% | **app CPU 律速** | **app 2 台化**（[§6b](#s6b)）。1.6〜1.9 倍を期待 |
| svc3 の mysqld ≥ 70%、index を貼り尽くした | **DB 律速** | 分離できる系統（DNS の DB、ログ、セッション、重い集計テーブル）を svc2 の MySQL へ。無ければ SQL の作り直しに戻る（サーバー追加は効かない） |
| 特定プロセス（pdns、ワーカー、画像変換）が app と CPU を取り合う | **サブシステム律速** | そのプロセスを svc2 へ隔離 |
| DB 分離後も svc1 の mysqld が残る（SQL 表の上位が `domains` / `records` = PowerDNS） | **DNS 律速** | pdns のキャッシュ（`cache-ttl`、`query-cache-ttl`、`negquery-cache-ttl`）→ pdns と自分の MySQL ごと svc2 へ。app とは別の箱に |
| 全箱 CPU < 50%、p99 が高い、または sessions の増加が止まる | **待ち律速** | サーバーを増やさない。`task deepdive` → ロック、プール上限、直列化、ポーリング間隔を探す |
| app の RSS が箱の 60% 超 | **メモリ律速** | キャッシュをエンドポイントで分担して app 2 台（nginx の location で振り分け） |

判定に迷う run が出たら、`summary.md` を渡して「上の表のどれか」と聞けば済む。構成を変えたら必ず note に書く（例: "DB→svc3"）。timeline の Δ がその変更の効果になる。

<a id="s6b"></a>
### 6b. app 2 台化チェックリスト

| 担当 / レーン | 所要 |
|---|---|
| DB 分離の後、14:00 頃 | 20〜30 分 |

最終形は `svc1 = nginx + app（ベンチの向き先）`、`svc2 = app`、`svc3 = MySQL`。reverse proxy 専用機は作らない（CPU が余る）。

1. DB を外から受ける: `etc/mysql/mysql.conf.d/` に `bind-address = 0.0.0.0`、`isucon@'%'` の権限を確認（`SELECT host FROM mysql.user WHERE user='isucon'`）。同一 VPC なので SG は触らない。
2. ホスト別 env: `env.svc2.sh`（DB = svc3 の private IP）を repo に置き、`deploy.sh` の `[ -f "env.$HOST.sh" ] && cp …` 行を有効にする。
3. svc1 の nginx: `tools/bootstrap/etc/nginx-upstream-2app.conf.example` を参考に upstream（`127.0.0.1:8080` + svc2、`keepalive`）を `etc/nginx/` に入れ、`deploy.sh` の nginx 反映行を有効にする。
4. app が `0.0.0.0` で listen していることを確認。
5. `task deploy` → ベンチ → `task deepdive TS=…` のクックブック 8 番（`upstream` 列）で両 app に割れていること。

壊れる原因は 3 つだけ。`docs/app-map.md` の「状態」「待ちが発生する箇所」と report の Where で事前に当たりを付ける。
- **プロセス内の状態**（インメモリキャッシュ、mutex で守った採番、常駐ワーカー）→ DB/Redis に置くか、そのエンドポイントだけ nginx の `location` で svc1 に固定。
- **`/initialize` は svc1 にしか来ない** → svc2 の app は起動時に DB から再構築、または svc1 の app から svc2 の内部エンドポイントを叩く。
- **ホストに紐づく副作用**（isupipe の `pdnsutil` exec、ローカルファイル）→ そのエンドポイントを svc1 固定、または副作用を DB 書き込みに置き換える。

<a id="s6c"></a>
### 6c. テクニックの網羅チェック（technique-scan）

| 担当 / レーン | 所要 | 終了条件 |
|---|---|---|
| レーン c（`[PHASE] loop technique-scan`） | 15 分 | `docs/technique-scan-<ts>.md` に全件の判定表、○ の上位が `docs/plans/<ts>-techniques.md` に作業単位として並ぶ |

index / N+1 を潰して Next actions が尽きかけた頃（13:00 前後）に 1 回。`docs/TECHNIQUES.md`（ISUCON 9〜14 の公式解説・上位チームの commit・参加記から作ったテクニック集）の **全件** を、マニュアルの制約・Data growth 表（増えないテーブル）・app-map の書き込み経路・endpoint 表に照らして ○ / × / 要確認 にする。思いつきで 2〜3 個試すのではなく、漏れを無くすのが目的。

```
technique-scan skill で。対象 run は <ts>
```

出てきた「要確認」は人がマニュアルを読んで決める（整合性の猶予の秒数、ステータスコードの扱い）。○ は parallel-fix に渡す。

<a id="s6d"></a>
### 6d. パラメータ調整（tuning）

| 担当 / レーン | 所要 | 終了条件 |
|---|---|---|
| レーン 2（`[PHASE] loop tuning`、PR は B が見る） | 15:00〜15:45 | ● のノブを最大 3 つ、1 つ 1 ベンチで当て、残す/戻すが journal にある |

探索はしない。report の「Tuning」節は固定ルールで「ノブ / 現在値 / 兆候 / 推奨 / ●○」を出す（buffer pool のヒット率とデータ量、max_connections の使用率、temp disk table、GC の CPU 割合、pool の待ち、worker_connections のエラー、TIME_WAIT と port range、listen overflow）。● だけを 1 つずつ入れ、スコアが −3% 以内で兆候が消えたら残す。

**「いつ」の列で 2 つに分ける**（report の Tuning 節）: **すぐ（fast lane）** = 上限系（`max_connections`、`table_open_cache`、`thread_cache_size`、`worker_connections`、`worker_rlimit_nofile`、`somaxconn` / backlog、port range、`LimitNOFILE`、DB 接続プールの大きさ）。足りないときの損（エラー・待ち）が大きく、上げる害がほとんど無く、1 行なのでコードの変更と一緒に入れても切り分けられる → **● が出た run の直後に、ハブがレーン 2 に `[fast]` の `etc/` PR として渡し、次のベンチに載せる**（15:00 を待たない）。**最後（15:00 tuning）** = buffer pool、redo / log file、flush 系、GOGC、nginx の buffer、app ×2 の配分など、影響し合うか効果がぶれ（±5%）より小さいもの、コードが変わると最適値が動くもの → ここ（15:00〜15:45）で 1 ベンチ 1 つ。推奨値が今の値以下のときは ● にならず「既に推奨値以上: 原因は別」と出る。耐久性を削る設定（flush=0、tmpfs の redo）は入れない。free-wins（§4 の D）が既に入っている前提。

```
tuning skill で。対象 run は <ts>
```

<a id="s6e"></a>
### 6e. 特別賞を狙う（score-sprint）

| 担当 / レーン | 所要 |
|---|---|
| C が判断、A が Claude に指示、B が PR を見る | 準備 20 分 + 1〜2 ベンチ |

毎年「最初に N 点に到達した 1 チーム」に特別賞がある（ISUCON 9〜14 で 15,000〜80,000 点、達成は 11:54〜13:05）。目標は kickoff が `alp.yml` の `prize_target:` に書き、report の冒頭に「目標の何 % か」が出る。75% を越えると Next step が勧める。

1. Claude: `score-sprint skill で。対象 run は <ts>` → まず **ゲート**: Verdict が svc1 の app の CPU 律速なら app ×2 + 計測 OFF、DB 律速なら計測 OFF だけ（または sprint をやめて改善を続ける）、待ち律速ならやめる。app ×2 のときは、プロセス内の状態とホストに縛られる処理を表にし、svc1 に固定する endpoint を決めて、nginx の upstream を 2 台にする PR を作る。
2. B が PR を見る（固定漏れ = 2 台目で古い値を返す endpoint が無いか）→ OK → merge → `task deploy` → `task smoke`（svc1 と svc2 の両方）→ **計測 ON で 1 ベンチ**（sprint の前の run）→ 用意してある `[sprint]` の `infra/measure-off` の PR を merge → deploy（ハブの `task hub:ship`。app の計測も `MEASURE=0` で止まる。PR が無いときだけ `task hosts:measure-off`、その後ベンチまで deploy しない）→ ベンチ。
3. ベンチの後: ポータルでスコアとリーダーボードを確かめ、`infra/measure-on` の PR を merge → deploy（`hosts:measure-off` で止めたときは `task hosts:measure-on`）→ **同じ構成の計測 ON で 1 ベンチ**（sprint の後の run）。計測 OFF のベンチは ops の report が作られないので、前後の run で「構成の効果」と「計測 OFF の効果」を分ける。2 台構成を残すか revert するかを決める。
4. 計測 OFF は特別賞の 1 本と最終確認（[§8](#s8)）のときだけ。その他のベンチは計測 ON のまま。

練習（2026-10-18、ISUCON14）: 律速は svc3 の DB（2 コア）だったのに app ×2 にして 30,866 → 26,175。計測 OFF のベンチだったので、何が下げたのかを後から調べられなかった。

受賞チームの例: ISUCON12 の NaruseJun は 11:53 に計測とログを止めて 53,037 点。ISUCON14 の 3 位チームは slow log を止めても 31,362 点で届かず、接続プールとマッチング間隔の調整で 32,767 点を越えた。

<a id="s6f"></a>
### 6f. チームに合流する（GitHub Project）

| 担当 / レーン | 所要 |
|---|---|
| ハブが目安を見て人に聞く、A が共有、チーム全員 | handoff 10 分 + 共有 10 分 |

A が 4 セッションで特別賞とある程度の伸びまでを取り、**伸び悩むか構成が固まったら**チームに合流する（目安の表は [lanes README §11](lanes/README.md)）。

1. ハブ: handoff skill → `docs/handoff.md`（変更ごとの効果、律速の推移、逆効果、失格の芽、open の PR、次の手の候補）を docs の PR で main へ。
2. A: Discord に PR のリンク。チームは `docs/handoff.md` と最新の summary.md を読んで 10 分で役割を決める。
3. 以後のタスクは GitHub Project:
   - **Project は練習と本番で 1 つ（`ytakaya/5`「ISUCON」、作成済み）**。大会は item の `Contest` 欄（= `.env` の `CONTEST`）で分ける。項目: `Status`（Todo / In Progress / Done）、`Priority`（P1 / P2 / P3。report の action の優先度）、`Score`（数値。app-analysis の「今解決すべきこと」の点数を入れる。Board はこの降順に並べる）、`Lane`（人 / 2 インフラ / 3 アプリ / c スカウト）、`Contest`、`Effect`（score の前後）。作り直すときや別の owner では `task project:setup [OWNER=…]`（同じ名前があれば使う。何度打っても増えない）。gh には `gh auth refresh -h github.com -s project`（普通のターミナルで。`!` では対話できない）。
   - **当日やること**: `.env` に `GH_PROJECT=ytakaya/5`（練習と同じ）。web で Board のビューの絞り込みを `contest:"<CONTEST>"` に（ビューは API で作れない）。チームのメンバーは Project の Settings → Manage access で共同編集者に（一度だけ）。
   - `task actions:issues TS=<ts>` は `GH_PROJECT` があれば作った issue を Project にも入れ、`Contest`、`Status=Todo`、`Priority`（report の P1 / P2）を付ける。手で作った issue や PR は `task project:add URL=<url> [LANE=2] [PRIORITY=P1] [SCORE=<点数>]`。handoff の「次の手の候補」も issue にする。
   - issue は repo の issue テンプレート（`.github/ISSUE_TEMPLATE/`: 改善タスク / 調査 / 障害）の欄で作る。根拠（report の行）・触る関数・猶予・再起動・2 台の欄があり、レーンにそのまま `[TASK]` で渡せる。
   - **Claude が issue を作るとき（ハブ・handoff・人の依頼）は、作ったら必ず Project に `Priority` と `Score` 付きで入れる**: `gh issue create …` → `task project:add URL=<issue> LANE=<人|2|3|c> PRIORITY=<P1|P2|P3> SCORE=<点>`。
     - `SCORE`: app-analysis の Now の表（`docs/reports/latest.md` の `<!-- bench:start -->`、列 `pts`）にその endpoint / action があればその値。無いもの（scan・deep-dive・構成の手）は同じ式で見積もる: `100 × その endpoint の share × 重み（スコアの POST 3 / その前後の流れ 2 / 他 1）`、5xx を減らす手は `+100 + 失敗件数（最大 +300）`、endpoint に紐付かない手は P1 = 30 / P2 = 12。見積もった値は issue の本文に「Score は見積もり（式: …）」と書く。
     - `PRIORITY`: report の action の P1 / P2 / P3。report に無い手は、次のベンチで効く見込みが大きく根拠が数字であるものを P1、それ以外を P2、試す価値だけのものを P3。
     - Board は `Score` の降順で並べ、上から着手する（人もレーンも）。
   - 人が担当する issue はレーンに渡さない。レーンに渡すものは Assignee を空、`Lane` に 2 / 3 / c を入れ、ハブが `[TASK]` にする。
   - claims（`docs/claims.md`）は人もセッションも書く（同じ関数の衝突を防ぐのは Project では足りない）。
   - **メンバーの PR**: PR テンプレートの journal の行を埋めて出すだけ。ハブが run の判定のたびと ship の前に `task hub:prs` で拾い、盤にレビューの todo を出す（A への中継は要らない）。deploy と ship はハブだけ（メンバーは `task deploy` を打たない。盤・journal・ベンチの todo が合わなくなる）。

<a id="s7"></a>
## 7. 収束

時刻: 16:00〜16:30

効果の薄い変更を revert し、最終構成を main に固める。`task deploy` → ベンチで pass を確認。**16:30 以降は機能変更しない。**

<a id="s8"></a>
## 8. 最終チェックリスト

| 時刻 | 担当 | 進め方 |
|---|---|---|
| 16:30〜17:30 | A が操作、全員で見る | 上から順に。各行は「やること → 確認」。ベンチは人がポータルで回す |

**16:30 以降は機能変更しない。**

### 8-1. 凍結（5 分）
- [ ] main に未 merge のブランチが無い（`git branch --no-merged main`）。あれば捨てる判断を今する。
- [ ] 全ホストが main の同じ commit（Fleet の git 欄が 3 台とも一致、dirty 0）。違えば `task deploy`。
- [ ] `deploy.sh` の `DB_HOST` / `DB_PRIVATE_IP` が埋まっている（DB 分離済み）。空なら [§4](#s4) が抜けている。
- [ ] Ops Appの「⏸ not benching now」が **解除**されている（ヘッダが緑）。
- [ ] `docs/journal.md` に最終構成（どの箱が何、DB の場所、キャッシュの置き場）を 3 行で記録（書くのはハブ）。

### 8-2. 計測を外す（5 分）
- [ ] 計測を止める: `deploy.sh` の 2d ブロックがあれば `MEASURE_OFF=1` の PR（`infra/measure-off` が未 merge ならそれ）を merge → `task deploy`（以後の deploy でも止まったまま）。無ければ `task hosts:measure-off`（この後に deploy すると nginx の access log が戻る）。どちらも 3 台とも `OK isumeasure-agent stopped`、`slow_query_log=0`、`nginx access_log off`。
- [ ] アプリ側の `Debug` / リクエストロガー / `127.0.0.1:6060` の pprof リスナーを外した commit が main に入っている（IMPLEMENTATION-NOTES [§6](#s6)）。`task deploy`。
- [ ] `task host:diag HOST=1` の disk が十分（ログで埋まっていない）。

### 8-3. 再起動試験の予行（15 分。去年の失格はここ）
- [ ] `task db:snapshot LABEL=before`（全テーブルの行数を `docs/retention/before.txt` に）。付随 DB を別ホストに出していれば（例: PowerDNS の `isudns` を svc2 へ）、そのホストも `DB_HOST=<IP>` で取る。
- [ ] `task reboot-test`: 3 台 reboot → ssh 復帰を待つ → enabled なユニットが全部 active → svc1 に **initialize なし**で smoke（ログイン、主要 GET、DNS）。赤が出たら直して再実行。
- [ ] ブラウザでアプリを開き、ベンチ前に作ったデータ（投稿、予約、ユーザー）が見える。
- [ ] ポータルからベンチ → pass。
- [ ] `task db:snapshot LABEL=after` → **どのテーブルも行数が減っていない**（増えるのは正常）。減っていたら、その書き込み経路がメモリかローカルファイルに落ちている（IMPLEMENTATION-NOTES [§0](#s0)）。
- [ ] **変更禁止のサービスが起動時に設定を書き換えていないか**（未確認の推測。2026-10-25 の kickoff phase 2 の指摘）: 例として isucon13 の `aws-env-isucon-subdomain-address.service` が起動のたびに `env.sh` の `ISUCON13_POWERDNS_SUBDOMAIN_ADDRESS` を書き直すかもしれない。reboot の後に `task host:diag HOST=1` で `env.sh`（と kickoff.md の「構成要素」に挙げた設定ファイル）が reboot 前と同じかを見る。書き換わるなら、そのサービスは触らず、上書きされない置き場所（`env.db.sh` の drop-in など、HANDOFF §5）に移す。練習専用の DNS の答えが戻っていれば、ベンチが「サーバーリストに含まれていません」で落ちる。
- [ ] DB が app より後に起動しても app が自力で立ち上がる（`deploy.sh` が入れる restart drop-in。`systemctl cat <unit>` に `Restart=always` と `StartLimitIntervalSec=0`）。心配なら svc3 の mysql を `sudo systemctl restart mysql` して svc1 の app が数秒で復帰することを `task host:diag` で見る。

### 8-4. 最終レビュー（10 分、Claude に依頼）
- [ ] 「`docs/IMPLEMENTATION-NOTES.md` の [§0](#s0)〜4 と [§8](#s8) をチェックリストに、朝の main からの diff を最終レビューして」。見るのは: 再起動で消えるデータ、起動順の依存、再開しないワーカー、外部通信、ログ・pprof の残り、変更禁止ファイル。
- [ ] 指摘があれば直して `task deploy` → 8-3 の reboot-test をもう一度。

### 8-5. スコア確定（10 分）
- [ ] ベンチをもう一度回して pass。スコアをOps Appの note と `docs/journal.md` に書く。
- [ ] 以降はサーバーも repo も触らない。3 台は起動したまま（終了後の停止・操作は失格）。
- [ ] 最終スコアと構成、スコア推移（Score timeline のスクショ）をチームに共有。

計測に戻したいときは `task hosts:measure-on`。

<a id="s9"></a>
## 9. 終了後

```
task retro REPO_DIR=.       # ops から run 履歴を取り、retro.md / retro.html を生成（commit 間の差分付き）
```

Claude に「retrospective skill で <contest> の振り返りを書いて」→ `docs/retro/<contest>.md`。ops のデータは `/opt/isucon-ops/data/<CONTEST>/` に残るので、ops を止める前に `task run:pull TS=…` で残したい run を回収する。

---

## 付録: 一人で練習するとき（AMI 3 台 + ops）

1. AMI から 3 台起動（c5.large 相当、SG は 22 を自分の IP に、ベンチ用に 443 など AMI の README 通り）。
2. `.env`: `HOST_LOGIN=ubuntu`（AMI の初期ユーザー）、`HOST_USER=isucon`、`CONTEST=<練習名>`、`GIT_REMOTE` は練習用 repo（テンプレートから作る）。
3. [§2](#s2) 以降は同じ。
4. **ベンチの準備は練習の前に人が済ませる（レーンの手順には入れない）**。レーンを始める前に、ベンチ機（AMI 同梱の bench を svc2 か別インスタンスに）・SG・DNS の答えを用意して 1 回空打ちしておき、盤の t3 のベンチ欄に「セットアップ済み、<実行するマシン> から、`--webapp <IP>`」と書く。ハブとレーンはそれを記録して使うだけ（2026-10-27: レーンの中でベンチの前提をいじって約 10 分を失った）。
   - svc1 の上で回さない（bench が CPU の 95% を取り、判定が歪む。2026-10-13）。別インスタンスなら c5.2xlarge 程度（下の 8）。
   - SG に **UDP 53** のインバウンド（ベンチ機から svc1 へ）。
   - アプリが DNS を返す年（isucon13）は、DNS の答えが `--webapp` の IP になっているか確かめる（AMI の `env.sh` は svc1 の private IP のことが多い。VPC の中から private IP 宛てに回すなら変更不要。2026-10-27）。変えるなら、レーンを始める前に人が repo の `env.sh` を直して push しておく（本番には持ち込まない）。
   - **`env.sh` を変えただけでは DNS の答えは変わらない**: ゾーン（PowerDNS の `isudns`）が新しい IP で作り直されるのは `/api/initialize`（の中の `init_zone.sh`）が走ったときだけ。ベンチは `/initialize` の**前に** `pipe.u.isucon.local` を引くので、古いゾーンのままだと `「127.0.0.1」はサーバーリストに含まれていません` で 0 点（2026-10-28）。`env.sh` を変えたら（app の再起動の後に）ベンチの前にゾーンを 1 回作り直す。どちらか:
     ```
     curl -sk -X POST --resolve pipe.u.isucon.local:443:<svc1 public IP> https://pipe.u.isucon.local/api/initialize
     ```
     （手元から。`{"language":"golang"}` が返れば成功、約 15 秒。社内プロキシは切っておく）、または svc1 の isucon ユーザーで `~/webapp/pdns/init_zone.sh`。確かめ方: `task host:diag HOST=1 NAME=pipe.u.isucon.local` の `## dns` が `--webapp` の IP か、ベンチ機で `dig +short pipe.u.isucon.local @<--nameserver の IP>`。サーバーを再起動したら `aws-env-isucon-subdomain-address.service` が `env.sh` を書き換えることがあるので、もう一度確かめる。サーバーの `env.sh` を手で直したなら repo の `env.sh` も同じ値にして push（`task deploy` の `git reset --hard` で戻るため）。
   ```
   ssh isucon@<bench host>
   cd ~/bench && ./bench run --target https://pipe.u.isucon.local --enable-ssl --nameserver <svc1 private IP> --webapp <svc1 private IP>
   ```
   **ISUCON14（isuride）を別ホストのベンチから回すとき**（2026-10-18 の練習）。ベンチは決済サーバー（payment gateway）を自分のホストで立て、app はそこへ決済を投げる:
   ```
   ssh isucon@<bench host>
   cd ~/bench && ./bench run --addr <svc1 private IP>:443 --target https://isuride.xiv.isucon.net \
     --payment-url http://<bench private IP>:12346 --payment-bind-port 12346 2>&1 | tee /tmp/bench.out
   ```
   - **bench ホストの SG で TCP 12346（`--payment-bind-port` の値）を VPC 内（172.31.0.0/16）に開ける**（AWS コンソールで。戻り経路 = app → bench:12346）。閉じていると評価が 10 s でタイムアウトする（クリティカル）。
   - bench の private IP: `.env` の `BENCH_HOST`（コメントアウトされていると `task hosts:ips` に出ない）。無ければ `task host:peek HOST=<bench の public IP>` の出力で見る。
   - 症状で気づく: 評価がタイムアウトする。svc1 の DB の `settings` の `payment_gateway_url` が bench ホストを指している（`task db:query`）。初期化の body で決済の URL を渡す年は、手で試す前に analyzer config の `initializeBody` にも同じ値を入れる（`task smoke INIT=1` と巡回が使う）。
5. ops で report が作れなかったら（Runs に「report failed」）手元で作る:
   ```
   task run:pull TS=<ts>
   tools/isumeasure/dist/isumeasure-darwin-arm64 report -out measurements/<ts> -code webapp/go measurements/<ts>
   ```
6. **4 セッションを 1 人で通す**（本番と同じ形。[起動の 1 行](#lanes)で 4 枚のタブ）。確かめること: `[HELLO]` / `[PICK]` / `[PR]` / `[DONE]` が全部レーン 1 に届き、`[TASK]` / `[PHASE]` / `[MERGED]` が 2 / c に届く、**名前の似た無関係なセッション（他の練習・大会）に 1 通も行かない**、盤に 3 レーンの状態が出続ける（手順の始めと終わりで変わる）、人への頼みごとが全部 todo になっている（文章だけの頼みが無い）、前提外れ（練習 AMI の `.local` など）が alert で出る、merge がレーン 1 だけ、2 / c が読み取りコマンドと `task lane:*` の許可待ちで止まらない（止まったコマンドは PREP の 7）。結果は HANDOFF §6 に。
   **練習の最後の 20 分は再起動試験に固定する**（[§8-3](#s8)、改善より先。`docs/plans/final-steps.md` があればそれを上から）。2026-10-25 も 10-27 も切り上げて行かなかった。
8. **ベンチ機の大きさ**: ベンチを 3 台の外の別インスタンスで回すなら、c5.2xlarge 程度にする。c5.large（2 コア）だと bench が CPU の 8 割を使い（2026-10-27: 161% / 200%、idle 19%）、サーバーが 3 割空いたまま約 127,000 点で頭打ちになった。report の Verdict が「CPU is not the limit」になったら、ベンチ機で `top -d 1` を見る。
9. 手元 PC から svc1 への `curl` / `dig` は社内プロキシ（Netskope など）で通らないことがあるので、練習の前に切る。
11. **1 レーンだけ Codex で回してみる**（任意）: 練習の前に `tools/lanes/sessions.json` のレーン 3 か c を `"agent": "codex"` にし（`task lane:agent` で確認）、ハブは `task lane:wait-msg LANE=1` を背景で流す。途中で 1 回 `task lane:handoff LANE=<n>` で引き継ぎも試す（[lanes/README.md](lanes/README.md#12-codex-で回す--途中で引き継ぐ) §12）。`task lane:timeline` の 5b 節（読むまでの時間）と Codex のレーンの idle を retro に。
7. 終わったら **`task lane:timeline` の出力を retro に貼る**（どこに時間がかかったか: 節目、ship の各段、人の応答時間、レーンの状態、Claude Code のターン・許可待ち）。任意で、練習の前に `task otel:up` と `.env` の `LANES_OTEL=1`（トークン・API の応答時間も取れる。盤の t0）、終わったら `task otel:down`。collector が動いていないのに `LANES_OTEL=1` で起動した場合の挙動は公式ドキュメント（monitoring-usage）に明記が無い（送信の失敗は `claude --debug-file <path>` のログに `[3P telemetry]` のエラーとして出る、とだけある。セッションが止まるかは未確認）→ 先に `task otel:up` が通ったのを確かめてからセッションを起動し、docker が無ければ `LANES_OTEL` を書かない。それから `task retro`、インスタンス削除、ops の `data/<CONTEST>` は残して良い。
10. **練習の記録を kit に残す**（7 の後）（`practice-log/README.md`）: contest repo で `task practice:record SLUG=<題材> NOTES_CHANGED="この練習の前に kit で変えたこと" NOTES_HYPOTHESIS="何が動くと思ったか" NOTES_OBS="見えたこと" REBOOT=pass|fail|not_run OUT=<kit の checkout>` → kit で `task practice:report` → `practice-log/` の JSON と `report.html` を kit への PR に入れる（retro と同じ PR でよい）。
