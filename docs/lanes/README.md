# レーン — 4 つの Claude セッションの共通ルール（全セッションが最初に読む）

当日は 1 人（A）が Claude Code のセッションを **4 枚**（ハブ + 3 レーン）、10:00 から**チームに合流するまで**回す（合流の目安は §11。合流後もセッションは残してよい）。時刻表では動かさない。**各セッションは「出来事（トリガー）」で動き、状況は盤に出す**。盤は手元のノート PC だけにある（repo の git common dir の下。同じ PC の worktree は全部同じ盤を見る）。人は **`task lane:web`（ブラウザ）か `task lane:watch`（ターミナルの 5 枚目のタブ）** を開いておき、そこと ハブの返答を見ていれば全部分かる。

読む順: このファイル → 自分のレーンのファイル（[1-hub.md](1-hub.md) / [2-infra.md](2-infra.md) / [3-app.md](3-app.md) / [c-scout.md](c-scout.md)）→ 必要になったら RUNBOOK の該当節。

## 0. 原則（2026-10-21 / 10-27 の練習で学んだこと）

1. **ベンチは早く、何度も**。baseline は deploy しない初期状態で `[DONE] servers` の直後に回す。次のベンチは「前のベンチが終わった時点で揃っている PR」で回す（全部を待たない）。**ベンチの todo が出たら（盤の内部の帯が `ready`。web に帯は無く、ベンチの todo だけが出る）人は即回す。終わったらその todo の「🏁 終わった」。parallel-fix や他の PR を待たない**（todo は merge → deploy → smoke が通った後にだけ出る）。帯が `ready` / `running` の間、ハブは ship しない。後から OK になった PR は帯の `NEXT` に積み、次のベンチに載せる（2026-10-25: ベンチの todo を閉じて作り直し、どの PR が載るのか分からなくなった）。
2. **準備はベンチを待たない**。DB 分離・計測パッチ・free wins・app-map は `[DONE] servers` で着手し、PR まで作っておく。baseline の結果を待つのは parallel-fix（report の行を直す仕事）だけ。merge は baseline の後。
3. **レーンを idle にしない**。各レーンのファイルに「手が空いたら」の列がある。ハブは毎ターン盤を見て、idle のレーンにそこから `[TASK]` を出す。idle でいてよいのは freeze の後だけ。
4. **ベンチの前提は言葉から推測しない**。練習のベンチは人がレーンを始める前にセットアップ済みで、盤の t3 に「実行するマシンと `--webapp` の IP」が書いてある。ハブとレーンはそれを記録して使うだけで、**ベンチのために DNS の答え・SG・`env.sh` を変えない**。分からなければ t3 の書き方で人に聞く（2026-10-27: 「3 台以外から回す」を「public IP が要る」と読み違え、DNS の答えを変えて人のベンチを止め、revert で初回ベンチが約 10 分遅れた）。
5. **人が見る可能性のある文書は、書いたらすぐ push する**（docs だけの PR、`[fast]`）。`[PICK]` の scan、設計、下読みのメモも同じ。「PR はまだ出さない」はコードにだけ使う（2026-10-27: 設計 3 本と下読みがローカルのブランチにしか無く、人は開けず、merge まで待った）。

## 1. 4 つのセッション（分け方）

| レーン | 名前 | 持つもの | やらないこと |
|---|---|---|---|
| `1` | ハブ | 人との会話、kickoff（マニュアル、前提の確認）、main の checkout（`git pull`、`task ops:repo`）、PR の一括レビューの表、**merge（唯一）**、`task hub:ship`（merge → deploy → smoke → ベンチの todo）、run を読んで次を決める（next-step）、`lane:phase`、最終段（再起動試験） | コードを書く（revert と設定の 1 行を除く） |
| `2` | インフラ | サーバーの準備（人への todo、`task deploy:init`）、parallel-setup（DB 分離、計測パッチ、free wins）、`etc/` と `deploy.sh`、fast lane の PR（`etc/`、数値定数）、app ×2 などの構成変更、tuning、score-sprint の PR | merge、deploy、ベンチ、ハンドラのロジック |
| `3` | アプリ | app-map（app-analysis）、parallel-fix（report の index / N+1 / ハンドラ）、設計済みの構造変更の実装、revert の PR | merge、deploy、ベンチ、`etc/`・`deploy.sh`（レーン 2） |
| `c` | スカウト | マニュアルの読み込み、早期 technique-scan と先読みの手（1〜3 PR）、run ごとの technique-scan、deep-dive、構造変更の設計、最終レビュー | merge、deploy、ベンチ、report に出ている index / N+1（レーン 3） |

なぜ 4 枚か（前回の「4 枚目を作らない」を撤回した理由）:

- **merge と deploy を 1 か所に集める**のは変えない。ハブが順番に merge し、deploy は 1 回、smoke も 1 回。
- 前回はハブがオーケストレーターを兼ねつつ、ビルドが「サーバー準備 + DB 分離 + parallel-fix」を全部持っていた。練習では DB 分離と parallel-fix が同じレーンで直列になり、インフラの準備がベンチを待っていた。**インフラ（etc/・deploy.sh・構成）とアプリ（ハンドラ）は触るファイルが重ならない**ので、別レーンにすれば本当に並行に動く。
- report の行を実装する手（アプリ）と、report に出ない手を探す手（スカウト）を分ける理由は前と同じ（2026-10-18 に一番伸びた 2 手は report に P1 で出なかった）。
- ハブは手を空けておく。1 分を超える作業（`task hub:ship`、kickoff の前提の確認）はバックグラウンドに出して自分では待たない。返答は毎回、`task lane:board` から作った 3 行で始める（[1-hub.md](1-hub.md)）。

## 2. 盤（board）— 状況は盤に出す。メッセージでは送らない

| コマンド | いつ |
|---|---|
| `task lane:status LANE=<1\|2\|3\|c> STATE=<working\|waiting-human\|waiting\|alert\|idle> STEP="<今の手順>" [NOTE="<補足>"]` | **STEP（今の手順）を変えるとき、人を待つとき（`waiting-human`）**。STATE の working / waiting / idle は hooks が自動で付ける（Claude のレーン、`LANE=<n> claude …` で起動したとき: プロンプトを受けたら working、`[PR]` / `[DONE]` で始まる SendMessage で waiting、ターンの終わりで idle（working のときだけ。`AGENTS=n` でサブエージェントが走っている間は working のまま）。alert と手で付けた waiting / waiting-human は上書きしない。STEP は変えない）。**SendMessage の直後に `lane:status` を打たない**（2026-10-28: 111 回打った最多のコマンド。うち約 37 回が SendMessage の直後で、1 回ごとにモデルの往復が 1 回増える）。Codex のレーンには hooks が無いので、今までどおり手順の始めと終わり・待ちで打つ |
| `task lane:alert LANE=… LEVEL=<warn\|crit> MSG="…" IMPACT="…"` / `task lane:resolve ID=…` | 下の §3 の条件のどれかに当たったとき。**直ったら出したレーンが resolve**（盤には「直ったら: task lane:resolve ID=…」と出る） |
| `task lane:todo LANE=… MSG="…" [CMD="…"] [PR="<n> <n> …"] [CHOICES="OK\|やめる"] WHY="…" AFTER="…"` / `task lane:done ID=…` | 人にしかできない操作を頼むとき（§4）。`PR="7 8 9"` を付けると盤の web に PR ごとのリンク・題名・外すトグルと **「✅ まとめて merge OK」「✋ 要修正」** が出る（PR のレビュー。**ベンチ 1 回に 1 つ**。作るのはハブだけ。`lane:wait` は `ok_prs=` / `held_prs=` / `decision=` を出す）。`CHOICES="<名前>::<説明>|…"` を付けると選択肢ごとのカード（名前を太字、説明を 2〜3 行、先頭に「推奨」）が出る（判断の質問。作るのはハブだけ）。`PICKS="U1 <名前>::<説明>|U2 …"` を付けると単位ごとのトグルと「✅ 選んだものを OK」が出て、`lane:wait` が `picked=` / `dropped=` を出す（`[PICK]` の一部だけ OK）。盤の web の「🔔 通知を有効にする」を押すと、新しい todo（ベンチの todo は別の音）・alert でブラウザの通知と音（🔊 で切り替え）が出る。盤の web は変化を待ち受けて返すので、todo は作られてから約 0.1 秒で出る（裏のタブではブラウザが間引くことがある）。`INPUT="ラベル1\|ラベル2"` を付けるとラベルごとの入力欄と「📨 送信」が出る（URL・IP などの値を人に入れてもらう。PR / CHOICES とは併用不可）。`ENVKEY=<HOSTS\|HOST_LOGIN\|BENCH_CMD\|GH_PROJECT\|CONTEST>` を足すと、送信した値を盤の web が `.env` のその行に書く（HOSTS は空白区切りで連結。他のキーは入力欄 1 つ） |
| `task lane:wait ID=<id>`（run_in_background）/ `task lane:todo … WAIT=1`（run_in_background） | todo の完了を待つ。**作ってすぐ待つなら `lane:todo … WAIT=1` を run_in_background で 1 回**（id を出してから待つ。`lane:todo` → `lane:wait` の 2 回が 1 回になる）。終わると `decision=<ok\|changes\|done\|選んだ選択肢>` を出す（`ok` / `changes` は PR 付きの todo のボタン、`CHOICES` 付きは押したボタンの文字列、`done` は ✅ / `lane:done`）。`INPUT` 付きは先に入力欄ごとに `input.<n>=<値>`、`ENVKEY` 付きは `env.HOSTS=<IP>`（他のキーは値を出さず `env.<KEY>=set`）、最後に `decision=done`（送信せずに `lane:done` で閉じたら `input=none`）。**決定と入力は盤の web の POST だけが書ける**（セッションからは書けない） |
| ベンチの todo の **「🏁 終わった」**（人が盤の web で押す） | **ベンチの終わり = この 1 ボタン**。`hub:ship` が作るベンチの todo と、hosts:setup が作る baseline のベンチの todo（どちらも `KIND=bench`）に出る（✅ の代わり）。スコアとベンチの出力の最後の数行を任意で入れられる。押すと todo が閉じ（`lane:wait` は `decision=done`、スコアを入れたら先に `score=<n>`）、裏で `task bench:done` が走り、全ホストの run が止まり、ops が collect → report → score を自動で行う（panel で手で押さない）。**盤の web にベンチの帯はもう無い**（ベンチの状態は ops パネルで見る。2026-10-28: 帯と panel の 2 か所で迷い、帯のボタンが押されず約 6 分失った）。ハブは今まで通り `task runs:wait` で score を拾う |
| `task lane:bench STATE=<none\|ready\|running\|collect> [SHA=<sha>] [PRS="#7 #8"] [NEXT="#9"]` | 盤の内部のベンチの状態（`lane:board` の 1 行目に出る。web には出さない）。`ready` は `hub:ship` が、`running` / `none` は `runs:wait` が、`collect` は「🏁 終わった」が自動で付ける。ハブが手で打つのは、`NEXT`（次のベンチに回す PR）を足すときと、自動がずれたときだけ。`hub:ship` は `ready` / `running` の間 ship しない |
| `task lane:note LANE=<1\|2\|3\|c> KIND=<app\|bottleneck\|scan\|fix\|decision\|score\|incident> MSG="<見出し、120 字以内>" [REF="PR #7 / ts / t12 / docs のパス"] [BODY="<結論、5 行まで。行は \n で区切る。1 行 160 字以内>"]` | 盤の web の **「📜 経緯」** に大事な事実を 1 行足す。経緯はベンチごとに 1 枚のカード（見出し「ベンチ N · score X（前回比 ±y%）· 時刻」、新しい 2 枚だけ開く、5 行まで）: 🚢 載った PR、🔥 律速（そのベンチの後の最初の `KIND=bottleneck` の見出し）、🎯 効果（`[DONE] … effect`。無ければその PR の `KIND=fix`）、🙋 人の決定、🚨 crit alert。最初のベンチの前は「準備」（kickoff の app note と決定）。todo の作成・warn alert・手順ごとの note は出ない。**「すべて表示」** で全行（新しい順、KIND ごとに絞り込み、`BODY` 付きの行は ▸ で本文）。上の **「📌 いまの要点」** は 6 枠固定（空なら「kickoff の後にハブが書く」などの案内）: 🧭 アプリ（ハブの kickoff note = `REF` に kickoff。後の app note で上書きされない。+ app-map の見出し 1 行）/ 🧮 スコア計算（kickoff note の `BODY` で スコア・減点・猶予 から始まる行。無ければ `docs/kickoff.md` の スコア）/ ⛔ 禁止・失格（`BODY` の 禁止 から始まる行。無ければ `docs/kickoff.md`）/ 🔥 今の律速（最新の bottleneck。新しいベンチの後は「古い」印）/ 📈 スコア（最新・前回比・ベスト・目標の %）/ 🙋 決定待ち・方針（決定待ちの todo、最新の `KIND=decision`、最新の人の決定）。**手順ごとには書かない**。書くのは下の「経緯に書くとき」だけ。人の決定（todo の id と答え・`ok_prs`）、ベンチの終わり、`runs:wait` の score、alert は**呼ばなくても自動で**載る |
| `task lane:starts HUB=<名前>` | ハブだけ。レーン 2 / 3 / c の起動の todo を作る（CMD はモデル指定付きの 1 行。表は `tools/lanes/sessions.json`） |
| `task lane:timeline [FROM=hh:mm]` | どこに時間がかかったか（盤の `events.jsonl` と Claude Code の hooks の `cc-events.jsonl` から）: 時系列、節目（URL → baseline → 各ベンチ → 特別賞）、ship の各段、人の応答時間、レーンの状態の配分、Claude Code（ターンの時間、遅いツール、許可待ち、サブエージェント）、止まっていた時間の内訳（Claude Code が止まっていた間の盤の状態: 人待ち / 他レーン待ち / やることなし / 盤の打ち忘れ。特別賞までの idle 分、3 分以上止まった間とその時のハブの STEP）。Markdown なので retro に貼る |
| `task practice:record …` / `task practice:report`（練習の後） | 練習 1 回を 1 つの JSON（`practice-log/SCHEMA.md`: URL → 特別賞、周回、費用、レーンごとの idle と merge、人待ち）にして kit の `practice-log/` に置く / 全練習の推移を 1 枚の HTML に。kit の変更が効いたかを見る（`practice-log/README.md`） |
| `task lane:phase PHASE=<setup\|baseline\|loop\|freeze\|final>` | ハブだけ |
| `task lane:board` | 何かを決める前（open の alert / todo、他のレーンの状態を見る）。読み取りだけ |
| `task lane:step ID=<I4> STATE=<doing\|done\|skip\|todo> [NOTE="PR #5"]` | ロードマップ（`tools/lanes/plan.json` の step）の進み具合。**手順の始めに doing、終わりに done**（PR を出したら done、NOTE に PR 番号）。依存（deps）が全部 done になった step は自動で ready、ベンチ（B1〜B4）と特別賞（P1）は panel の score から自動（B<n> はベンチの終わり＝ベンチ todo の「🏁 終わった」か `runs:wait` の score でも、まだ done でない先頭が done）。`plan.json` の `done_on`（正規表現）を持つ step は、その lane が送った `[DONE]` / `[PR]` / `[PICK]`（SendMessage のフック、Codex は `lane:msg`）が当たると自動で done（例: `[DONE] servers` → I3、`[DONE] c manual` → C1、`[PICK]` → C2）。自動の done は手の doing / todo より優先。PR で終わる step は `plan.json` の `auto`（head ブランチ / 変更ファイルの glob）で `hub:ship` の merge 時に自動で done。PR なしで終わったら `STATE=skip`。`todo` で手の印を消す |
| `task lane:web` / `task lane:watch` / `task lane:copy ID=t5` | 人用。web はブラウザの盤（📋 コピー、✅ 完了、解消のボタン、ベンチの todo の 🏁 終わった、🗺 ロードマップ: レーンごとのやること・進み具合・依存の矢印・特別賞までのクリティカルパス・スコアと目標、📜 経緯）、watch はターミナルの全画面の盤（数字キーで todo の CMD をコピー、`q` で終わる）、copy は todo の CMD をクリップボードへ |

**経緯に書くとき（`lane:note`。各レーンのファイルにも 1 行ある）**: 見出しは 1 行。**`BODY` は結論だけに付ける**（人が盤の web で後から追いつけるように。5 行まで、根拠の数字 1 つずつ、詳細は docs に書いて `REF` で指す）。ハブ = kickoff の要約（`KIND=app REF=docs/kickoff.md`、BODY: `スコア = …` / `減点: …` / `猶予: …` / `禁止: …` で始まる行と構成 1 行。行の頭の語で 📌 の 🧮・⛔ 枠に振り分けられる）と、run ごとの判定（`KIND=bottleneck`、BODY: score・律速・次の手）/ レーン 3 = app-map の要約（`KIND=app`、BODY あり）と、修正 PR ごとの律速（`KIND=fix REF="PR #n"`、1 行）/ レーン 2 = 構成・設定の変更（`KIND=fix`、1 行）と config-options の結論（`KIND=decision`、BODY: 案ごとに 1 行）/ レーン c = early-scan・technique-scan の結論（`KIND=scan`、BODY: 採用 / 見送りの単位ごとに 1 行の理由）と deep-dive の結論（`KIND=bottleneck`、BODY あり）。障害は alert が自動で載る（追記するなら `KIND=incident`）。人の決定は自動。

**ロードマップの雛形は `tools/lanes/plan.json`**（step = id / lane / title / deps / stage / how）。練習ではそのまま使う。本番で問題に合わせて変えたくなったらハブが編集して commit（kickoff の例外 a には入れない。普通の PR、練習ならそのまま main）。

**盤はノート PC ごと**。別の PC のチームメイトには見えない（ops パネルにも出ない）。チームで回すときは MESSAGES の 1 行を Discord に貼る。

STATE の意味: `working` = 自分で進めている / `waiting-human` = 人の操作か許可を待っている / `waiting` = 他のレーンを待っている（NOTE に何を）/ `alert` = alert を出して止まっている / `idle` = 本当にやることが無い（**freeze の前に idle が続いたら、ハブがそのレーンの「手が空いたら」から `[TASK]` を出す**）。

`lane:status` には **`AGENTS=<n>`**（今走らせているサブエージェントの数。起動したら n、全部戻ったら 0。盤のカードに 🤖×n）を足せる。

**進み具合は見込みの時間ではなく、チェックリストで出す**: step を始めたら、その step でやることを `task lane:items ID=A3 ITEMS="indexes|livecomment|livestream|stats"` で登録し、1 つ終えるごとに `task lane:item ID=A3 N=2`（始めたものは `STATE=doing`）。盤のレーンのカードに箇条書き（✅ / 🔵 / ⬜）と n/m のバー、ロードマップのカードに n/m が出る。項目は「終わったと言える単位」にする（サブエージェント 1 つ、PR 1 本、グループ 1 つ）。parallel-fix / parallel-setup はグループ = 項目、technique-scan は「読む → 判定 → 作業単位 → [DONE]」、hub:ship は「merge → deploy → smoke → todo」。

STEP は 40 字以内で「何を」: `parallel-setup（DB 分離）`、`parallel-fix 160320（4 グループ）`、`PR #12 レビュー待ち`。盤のコマンドの出力（id）は返答に残す。

## 3. alert — 想定と違うことが起きたら、すぐ盤に出す

| 条件 | 誰が | LEVEL |
|---|---|---|
| **kickoff の前提と違う**（下の表）| 見つけたレーン（ハブの前提の確認、インフラの `task host:peek`） | 次の手順が使えないなら crit、回避できるなら warn |
| ベンチが FAIL | ハブ | crit |
| スコアが前回より 10% 以上下がった | ハブ | crit |
| report に前回無かった 5xx が出た | ハブ | warn（1% 以上なら crit） |
| `task smoke` が落ちた | ハブ（`task hub:ship` が自動で出す） | crit |
| claims の衝突（同じ関数が他のブランチで open） | 見つけたレーン | warn（+ ハブに `[NEED-OK]`） |
| 許可の prompt で 5 分以上止まっている | ハブ（盤で他レーンの STEP が「許可待ち」のまま 5 分以上） | warn |
| マニュアルと実機が違う（ドメイン、ポート、ユニット名、初期化のパス、ベンチの向き先） | 見つけたレーン | warn（ベンチが通らない種類なら crit） |

**MSG は事実 1 行、IMPACT は「kit のどれが動かないか」**。**同じことを 2 つ出さない**（先に `task lane:board`。練習では a2 と a9 がほぼ同じ内容で並んだ）。**直ったら、または「対応不要と決めた」ら出したレーンがすぐ resolve**（warn を残したままにしない。人には消し方が分からない）。crit を出したら `[ALERT] <id> <要点>` をハブに 1 行送る。

### kickoff の前提と、外れたときの IMPACT

| 前提 | 外れた例 | IMPACT に書くこと（動かない / 直しが要る kit の部品） |
|---|---|---|
| DB は MySQL / MariaDB | PostgreSQL、SQLite | slow log（bootstrap、`measure-off.sh`）、agent の MySQL status と report の SQL 表・Data growth、`indexes/apply.sh`、`task db:query` / `db:snapshot`、sqltag（`mysql.MySQLDriver` を包んでいる → pgx / pq の driver に）、`zz-perf.cnf` / `zz-isucon.cnf`、parallel-setup A（`bind-address` と GRANT → `listen_addresses` と `pg_hba.conf`）、tuning の MySQL ノブ、IMPLEMENTATION-NOTES §5 の `interpolateParams`。PostgreSQL なら `pg_stat_statements` / `log_min_duration_statement=0` で代える。SQLite は HANDOFF §5（ISUCON12）の `ISUCON_SQLITE_TRACE_FILE` |
| 言語ディレクトリが `webapp/go`（`go.mod` がそこ） | `webapp/golang`、Go が無い、モジュールが下の階層 | `deploy.sh` のビルド行、parallel-setup B（計測パッチ）、`task groups`（alp.yml）、report の `-code`、parallel-fix の `go build` / `go vet` の場所、analyzer の `codeDir` |
| app は systemd のユニット | docker compose | `task deploy:init`（ユニットから deploy.sh を作る）、restart の drop-in（IMPLEMENTATION-NOTES §0）、`MEASURE=0` の drop-in、`task host:diag` の journal、`task reboot-test` のユニット確認。ネイティブ起動への切り替えを先にやる（HANDOFF §5 の ISUCON12） |
| 前に nginx | h2o / caddy / envoy / apache、app が直接 443 | ltsv の access log（bootstrap が飛ばす）→ agent のベンチ検出（`TRIGGER`）と report の endpoint 表が全部、parallel-setup D の nginx、tuning の nginx、`measure-off` の access_log、app 2 台化の upstream |
| 3 台 | 1 台、2 台、5 台 | `.env` の HOSTS、parallel-setup A（DB を svc3 へ）、`task deploy` の DB ホスト先行、RUNBOOK §6a の構成表 |
| ドメインとポートがマニュアルどおり | 練習 AMI の `.local`、ポート違い | analyzer の `resolve` / `baseURL`、`task smoke`、`/etc/hosts` の行、nginx の `server_name`、ベンチの向き先 |
| initialize の制限時間が書いてある | 書いていない | index を `/initialize` で当てる方法（ALTER の時間）。人に「ポータルかマニュアルで制限時間を確かめる」todo |
| 外部依存が無い | 決済、外部 API、DNS、ベンチ側のサーバーへの戻り経路 | `task smoke INIT=1`（`initializeBody`）、SG（ベンチの戻りのポート。RUNBOOK 付録）、app 2 台、整合性。FIRST-HOUR の型 3・4 |

## 4. 人への todo — 頼むことは必ず todo にする

セッションは手元の sudo、ポータル、AWS、`.env` の編集ができない。それが要るときは **`task lane:todo` を作り、同じ内容を返答にも書く**。

**人への質問・判断の todo（PR の OK、`[PICK]` の拒否と要確認、猶予の解釈、構成の判断）はハブだけが作る**。レーン 2 / 3 / c は `[PICK]` / `[NEED-OK]` をハブに送るだけ（2026-10-25: 同じ質問がレーン c とハブの 2 か所に todo で出た）。レーンが自分で作ってよいのは、自分の手順に要る**操作**の todo（`.env` の HOSTS、`! task hosts:setup`、SG）だけ。

- **CMD**: 人がそのまま貼る 1 行（コマンド全文、または「どの画面の、どのボタン」）。**コマンドは必ず CMD に入れる**（盤の 📋 / 数字キー / `task lane:copy` でコピーできる。MSG に混ぜるとコピーできない）。手順が 2 行以上なら、人が 1 回で貼れる 1 行にまとめる（`a && b`）か、todo を分ける。
- **MSG**: 何をするか 1 行（日本語）。
- **WHY**: 1 行。何が止まっているか。
- **AFTER**: 終わったら人が何をするか（「何もしない（ハブが盤で気づく）」が一番良い。次が「ハブのタブに `done`」）。
- 返答にも同じコマンドを、**1 コマンド 1 コードブロック**で書く（ターミナルでも選択しやすい）。
- **✅ で作ったレーンが起きる**: その todo の完了を待つレーンは、`task lane:todo` の直後に `task lane:wait ID=<id>` を **run_in_background** で流す。人が盤の ✅（web）/ `d`+番号（watch）/ `task lane:done` で消すと、その待ちが終わってレーンに通知が来る。だから AFTER は「✅ を押すだけ」でよい（タブに `done` と書かなくてよい）。PR 付きの todo（`PR=<n>`）は ✅ の代わりに「✅ merge OK」「✋ 要修正」で、待ちの出力は `decision=ok` / `decision=changes`（ハブの使い方は [1-hub.md](1-hub.md) の `[PR]` の行）。
- 起きたレーンは読み取りのコマンドで本当に終わったかを確かめる（例: hosts:setup なら `task hosts:status`）。人が ✅ を押さずにタブで `done` と言ったら、作ったレーンが `task lane:done ID=…`。
- **値を入れてもらう todo は入力欄**（`INPUT="当日マニュアル URL|アプリマニュアル URL"`）。人はタブに貼らず盤に入力して送信し、待っているレーンは `lane:wait` の `input.<n>=` で受け取る。値は 1 行・500 字まで（改行・制御文字は弾かれる）。盤には「入力済み」とだけ出て、値は出ない。`.env` に入る値（HOSTS など）は `ENVKEY=` を付ける: 書くのは人が起動した `task lane:web` のプロセスで、許可したキーの行だけを書き換える（他の行はそのまま、ファイルのモードも保つ）。セッションは `.env` を読まない（確かめるのは `task env:show`）。
- **判断の質問は 1 問 1 todo、選択肢はボタン**（`CHOICES="OK|やめる"`、推奨を先頭に）。MSG は「何を決めるか / 推奨と理由 1 行」、根拠の文書は push 済みのファイルのリンク（GitHub）にする。ローカルのブランチにしか無い文書を読ませない（2026-10-27: `[PICK]` の「作業単位 3 つの拒否と Q1 Q2」を 1 つの todo にまとめ、文書も push されておらず、人は何を聞かれているか分からなかった）。人がタブで答えるときは todo の id（`t12` など）を添えてもらう（「おk」だけでは何への答えか曖昧になった）。
- **人が判断する文書（設計・構成の案・scan）の型**（2026-10-27 夜: plans と scan が長く、何を変えるのか読み取りにくかった）: 先頭に「決めてほしいこと」。1 案 / 1 単位は **3 行**（変わること = 何が → 何に・触る場所 / 見込み = 数字 1 つか「推測」/ リスク = 猶予・再起動・2 台）。設計の文書は全体で 15 行以内（何を・なぜ（数字 1 つ）・書き込み箇所・再起動と 2 台・見込み）。根拠の表は末尾の付録へ。
- **ハブの返答で todo に触れるときは必ず id を付ける**（`t12 ベンチ`）。人が「t12 って何?」と聞き返せる。

| 例 | MSG | CMD | WHY | AFTER |
|---|---|---|---|---|
| hosts | ブラウザ用の hosts 行（ハブが `[DONE] servers` で出す） | `sudo sh -c 'printf "<svc1 IP> pipe.u.isucon.dev\n<svc1 IP> test001.u.isucon.dev\n" >> /etc/hosts'`（IP は `task hosts:ips`、名前は kickoff.md のアクセス） | ブラウザで目視（巡回は analyzer の resolve で不要） | ✅ を押すだけ |
| サーバー | 3 台のセットアップ | `! task hosts:setup`（レーン 2 のタブで） | サーバーを変える操作は人が打つ | 何もしない（`!` の出力でレーン 2 が起きる） |
| ベンチ | ベンチ（main <sha>: 載っている PR #7 #8 / 次に回す PR #9） | ポータルの Job Enqueue → svc1 → Enqueue（練習は事前にセットアップしたベンチ機で） | main の <sha> を測る | **出たら即回す**。スコアが出たらこの todo の **「🏁 終わった」**（スコアと出力の最後の数行は任意。裏で `task bench:done`）。collect → report → score は ops が自動。ハブには何も言わなくてよい（ハブは `runs:wait` で拾う） |
| レビュー | `#12（integ-160320、レーン 3）をレビュー` | `gh pr view 12 --web`（`PR=12` で盤にリンクとボタン） | merge の判断は人 | 盤の **✅ merge OK** か **✋ 要修正** を押す（ハブのタブで「OK #12」でもよい） |
| 判断 | `#12 は fast にしない（tx に触る）。入れるか`（推奨: OK、理由 1 行） | —（`CHOICES="OK|やめる"`） | merge の判断は人 | 盤のボタンを押す（タブで答えるなら id 付きで「t12 OK」） |

## 5. セッション間のメッセージ — 合図だけ

形と例は [docs/MESSAGES.md](../MESSAGES.md)。

- メッセージは **他のレーンに動いてほしいときの合図**だけ: `[HELLO]`、`[TASK]`、`[PHASE]`、`[PR]`、`[MERGED]`、`[DEPLOYED]`、`[RUN]`、`[DONE]`、`[PICK]` / `[DECIDED]`、`[NEED-OK]`、`[ALERT]`、`[STOP]`。
- **状態は盤に出す。メッセージで送らない**。
- 宛先: 2 / 3 / c はハブにだけ送る。ハブは 2 / 3 / c に送る。レーン同士では送らない。
- 宛先の名前は `[HELLO]` の表のものだけ。**同じ名前のセッションが 2 つ並んだら `<名前> [ref]` で送る**（練習ではレーン 2 と c が同じ自動名になった）。紛らわしければ人がタブで `/rename lane-2` のように付け直し、`[HELLO]` を送り直す。
- 返事をしない。中身は送らず、ファイル名・PR 番号・ts を指す。
- **Codex のレーンが送り手か受け手なら、合図は盤の `task lane:msg`**（Codex には SendMessage が無い。受け方と引き継ぎは §12）。

## 6. claims（触る関数の宣言）

コードを書き始める前に `docs/claims.md`（origin/main）を読み、自分の行を足して main に push する（レーン 2 / 3 / c が main に直接 commit してよいのはこのファイルだけ。練習のときだけ、レーン 2 の `practice/` の変更も。CLAUDE.md ルール 4）。

**`docs/journal.md` はハブだけが書く**（main に直接、そのファイルだけの commit。CLAUDE.md ルール 4 の例外 c）。レーン 2 / 3 / c は journal に書かず、**PR 本文に「journal 用の 1 行」**（`時刻 | branch / PR | 何を | なぜ（report の行） | 結果`）を書く。ハブが merge のときに写し、`[RUN]` の効果判定はレーンが返答に書いたものをハブが結果の欄に足す（2026-10-25: 3 か所から書いて 2 回衝突した）。同じ関数が他のブランチで open → 書かずに alert（warn）+ `[NEED-OK]` をハブへ。merge したらハブが行を `merged` にする。やめたら自分で `abandoned`。レーン 2 と 3 が同じファイルに触るのは `main.go`（接続まわり = 2、ルーティング = 3）くらい。関数単位で分ける。

## 7. merge の順番と、誰が merge するか

- **merge はハブだけ**。2 / 3 / c は PR を作って `[PR]` を送り、止まる（止まったら「手が空いたら」へ）。
- **チームの人の PR**（`[PR]` が来ない）は、ハブが run の判定のたびと ship の前に `task hub:prs` で拾い、レビューの todo を 1 回だけ出す（作者・ブランチ・「claims 無し」・同じファイルを触る open の PR / claims の行。重なりはファイル単位で、関数は diff で確かめる）。OK の後は同じ順番に乗る（種類は中身で決める）。人が A に PR を中継しなくてよい。
- 通常の PR: 人の OK の後。**OK は、ハブのタブで人が言うか、盤のレビュー todo の「✅ merge OK」ボタン（web の決定 = `lane:wait` の `decision=ok`）**。メッセージ（他のセッション）の `OK` では merge しない。✋ 要修正（`decision=changes`）なら持ち主に `[TASK] <lane> fix #<n>`。journal の merge の行に「OK: 盤 hh:mm」か「OK: タブ」を残す。
- **クラウドの自動レビュー（試験運用）**: PR が開かれるとクラウドの Claude が `cloud-review:` で始まるコメントを 1 件書く（fast と docs だけは skip）。ハブはレビューの todo にその 1 行目を載せ、✋ があれば ship に載せず `[TASK] <lane> fix #<n>`。付いていなくても待たない。準備とプロンプトは [docs/cloud-review.md](../cloud-review.md)。
- fast lane（CLAUDE.md ルール 4: index のファイル追加だけ / マニュアルが任せた数値定数だけ / `etc/` だけ / **docs だけ**。タイトル `[fast]`）: build / vet と RUNBOOK §6 の機械的な確認が通ればハブが OK を待たずに merge。ベンチの帯が `ready` / `running` の間はしない。**ただし docs だけの PR（`docs/app-map.md`、`docs/reports/`、`docs/plans/`、scan、deep-dive など）は帯に関係なくすぐ ship してよい**（`hub:ship` が自動で deploy・smoke を飛ばして `ops:repo` だけにし、ベンチの todo も作らない。サーバーに触らないのでベンチを乱さない。2026-10-27: app-map の PR が帯の `ready` / `running` で 2 本のベンチを待った）。
- 順番: **インフラ（setup）→ アプリ（fix）→ スカウトの PR → その他**（同じ種類なら PR 番号順）。**前のベンチが終わった時点で OK が出ている PR だけ**をまとめて `task hub:ship PRS="…"`（merge → deploy 1 回 → `task smoke INIT=1` → ベンチの todo + 帯 `ready`）。遅れている PR は次のベンチに回す（帯の `NEXT`）。
- **帯が `ready` / `running` の間は ship しない**（ベンチの todo を閉じて作り直さない。2026-10-25 に 2 回やって、載っている PR が分からなくなった）。その間に OK になった PR は `task lane:bench NEXT=\"#9 #10\"`（STATE を付けない。付けると帯が戻る） に積み、帯が `none` に戻ったら（`runs:wait` の完了）ship する。
- ベンチの最中に docs と code の PR を混ぜて `hub:ship` に渡すと、docs だけ出して code は帯の NEXT に積む。analyzer の設定（kickoff phase 2）が main に来る前の `task smoke` は、最小の確認（`GET /` が 500 未満）になり、initialize は飛ばす（`SMOKE_HOST` を `.env` に書ける。2026-10-28: 設定が無くて smoke が落ち、ベンチの todo が出なかった）。
- **`hub:ship` が止まるのは 3 つだけ**: main の checkout に未 push の commit がある / `docs/journal.md`・`docs/claims.md` が未 commit / merge で入ってくるファイルが未 commit。それ以外の未 commit のファイルは名前を出して進む。docs だけの PR は帯が running でも `FORCE` 無しで出る（帯は変わらない）。code の ship は帯が `ready` / `running` の間は待つ。
- 盤に **`!! レーン N: waiting 7m … → 手が空いたら から [TASK]`** が出たら、ハブはそのレーンのファイルの「手が空いたら」から `[TASK]` を送る（2026-10-27 夜: レーン 2 が 44% を待ちで過ごした）。
- **同じファイルを触る PR が同じレーンから複数出そうなら、レーンは 1 本の integ ブランチにまとめて出す**（parallel-fix の形）。別々に出すと、先の merge で import 行のような無関係の場所が衝突し、rebase → OK の取り直しで 1 ベンチ遅れる（2026-10-27: #3 #4 #5 が `user_handler.go` の import で衝突）。
- 先の merge で衝突したら、ハブは自分で解かず `[TASK] <lane> rebase #<n>` で持ち主に返す。`hub:ship` は衝突した PR を飛ばして残りを出す（warn の alert が出る）ので、飛ばされた PR だけ rebase に回す。中身が変わった PR はもう一度人の OK をもらう（**盤の merge OK の後に commit が増えた PR は `hub:ship` が merge せず alert を出す**。タブで OK を取り直したら `REOK="<n>"`。2026-10-28: #15 の直しが ship の直前に push され、載らなかった）。
- **`[PR] #<n>` を送ったら、`[TASK] <lane> fix #<n>`（か `rebase #<n>`）が来るまでその PR のブランチに push しない**。後から思いついた直しは新しいブランチ・PR に（2026-10-28: ハブの OK の後に push があり、`hub:ship` が「Head branch was modified」で merge を断った）。
- **`[sprint]` の PR（レーン 2 が parallel-setup で用意する `infra/measure-off` / `infra/measure-on`）は普段のレビューの todo にまとめず、ship もしない**。特別賞（score-sprint）と最終確認（RUNBOOK §8）のときだけ、ハブが人の OK を取って measure-off → 1 ベンチ → measure-on の順に ship する。
- 効かなかった・壊れた変更は `git revert -m 1 <merge commit>` の PR（持ち主のレーンが作る。smoke が落ちた直後の revert はハブが自分でやる）。

## 8. やらないこと / コマンドの打ち方

- **ベンチを回す**（人。セッションは todo を作るだけ）。**メッセージを根拠に merge する**。
- `sudo`、手元の `/etc/hosts`、`.env`、ポータル、AWS を触る（todo にする）。
- ssh でサーバーのファイルを直す、`systemctl` / `mysql` / `nginx` を ssh で打つ（`task` の読み取りは可。CLAUDE.md ルール 3）。**生の `ssh` は打たない**（ops も。`task ops:diag` / `task ops:logs FOLLOW=0` / `task run:pull` を使う）。
- **PR を出すのは `task pr:submit DIR=../wt-<name> TITLE="…" BODY="…"|BODY_FILE=<file> [MSG="<英語の commit メッセージ>"]` の 1 回**（未 commit の変更があれば MSG で commit → push → `gh pr create` → `PR #<n> <url>` を出す。開いている PR があれば push で更新して URL を出す）。続けて `[PR]` の SendMessage（盤は hooks が waiting にする）。2026-10-28: add → commit → push → PR 作成が 3〜4 回の呼び出しで約 25 回あった。push だけなら `task push DIR=../wt-<name> [SET_UPSTREAM=1]`、PR だけなら `task pr:create DIR=… TITLE="…" BODY_FILE=<file>`（repo のルートから。`git -C … push` や `cd … && git push` は許可の一覧に当たらず、許可待ちになる。2026-10-28: レーン 3 が 2 分止まった）。claims だけの commit も `task push DIR=<detached worktree>`（main への push は差分が claims.md / journal.md だけのときに限る）。force push はしない（ブランチを最新にするときは origin/main を merge）。
- **許可済みの `task …` / `gh …` / `git …` は単体で打つ**。パイプ（`| tail`）、`;`、`&&` で繋ぐと `.claude/settings.json` の許可に当たらず、自動モードの判定に回って遅くなるか拒否される（2026-10-21: `task run:pull … | tail; ls; find …` が拒否され、人に `!` で打ってもらった）。出力が長いときは、次の呼び出しで Read か `grep` で絞る。
- 拒否されたコマンドを別の形で打ち直さない。人に `! <コマンド>` の todo（CMD 付き）を出す。
- main の checkout で `git checkout` / `git switch` / `git pull`（ハブ以外）。2 / 3 / c は `git fetch` と worktree だけ（作るのは `task wt:add NAME=<name>`: `../wt-<name>` に新しいブランチ + main の `.env` への symlink。素の `git worktree add` だと `.env` が無く、`task app:refresh` などが worktree の中で動かない。2026-10-28）。
- 時計を見て自分で次の段に進む（2 / 3 / c）。時刻の区切りはハブの `[PHASE]` で来る。
- 盤を更新せずに長い作業に入る。

## 9. 起動の 1 行（人がタブに貼る）

**最初に盤を開く**（`task lane:web` か `task lane:watch`）。新しい repo の盤には、最初から人の todo が 3 つ出ている: ①`t1`「レーン 1（ハブ）を起動」（貼る 1 行付き）②`t2`「3 台の IP」（入力欄 3 つ。送信すると盤が `.env` の HOSTS を書き、レーン 2 が起きる）③`t3`「マニュアルの URL 2 つとベンチ」（入力欄 3 つ: 当日マニュアル URL / アプリマニュアル URL / ベンチ（本番: ポータル / 練習: セットアップ済み、実行するマシンと `--webapp` の IP）。競技が始まったら送信し、ハブが起きる）。①でハブが名前を出すと、ハブが `task lane:starts` で ④〜⑥「レーン 2 / 3 / c を起動」の todo を作る。**各 todo の CMD はモデル指定付きの 1 行**（`LANE=<n> claude --model <model> '<起動の 1 行>'`。sessions.json で `"agent": "codex"` のレーンは `LANE=<n> codex --model … '<起動の 1 行>'`、§12。そのままターミナルに貼る。`LANE=` は計測のレーンの印なので消さない）。上から 📋 → 貼る → ✅（②③は入力 → 送信）で進む。**IP と URL はタブに貼らず盤に入力する**。下の表は同じ 1 行。

**モデル（`tools/lanes/sessions.json` で変える）**: 1 ハブ = Opus 5.5 + fast mode（`claude --model claude-opus-5-5`、起動したら `/fast`）/ 2 インフラ = Opus 5.5 / 3 アプリ = Opus 5.5 / c スカウト = Fable 5.1（`claude --model claude-fable-5-1`）。ハブは短い判断を何十回も返すので速さ、c は deep-dive・設計・二重レビューで判断の質（2026-10-27: 失格の芽を見つけたのは c）。重い判断（kickoff の前提、構成の判断）はハブが Fable のサブエージェントに出してよい。

**Claude Code の計測**: repo の `.claude/settings.json` の hooks が、各セッションのターン・ツール・許可待ち・サブエージェントの時刻を盤の `cc-events.jsonl` に自動で書く（セッション側の操作は要らない）。任意で OpenTelemetry も: 練習の前に `task otel:up`、`.env` に `LANES_OTEL=1`（起動の 1 行に OTel の環境変数が付く。起動の todo の CMD は表示・コピーのたびに `.env` を読み直すので、lane:web の再起動は要らない）、終わったら `task otel:down`。練習の盤（`.env` の `PRACTICE=1` か `HOST_LOGIN=ubuntu`）には、t1 の前に任意の todo `t0`（`task otel:up`）が出る。やらないなら ✅ だけ。

| タブ | 貼る 1 行（盤の todo の CMD はこれにモデルと `LANE=` を付けたもの） |
|---|---|
| 1 | `あなたはレーン 1（ハブ）。docs/lanes/README.md と docs/lanes/1-hub.md を読んで従って。最初に ListAgents で自分の名前を出して。`（URL とベンチは盤の t3 に入力する。この 1 行の末尾に `マニュアル: 当日 <URL> / アプリ <URL> / ベンチ: <ポータル、または練習: セットアップ済み・実行するマシンと IP>` を付けてもよい） |
| 2 | `あなたはレーン 2（インフラ）。docs/lanes/README.md と docs/lanes/2-infra.md を読んで従って。レーン 1 の名前は <名前>。最初に [HELLO] を送って。` |
| 3 | `あなたはレーン 3（アプリ）。docs/lanes/README.md と docs/lanes/3-app.md を読んで従って。レーン 1 の名前は <名前>。最初に [HELLO] を送って。` |
| c | `あなたはレーン c（スカウト）。docs/lanes/README.md と docs/lanes/c-scout.md を読んで従って。レーン 1 の名前は <名前>。最初に [HELLO] を送って。` |

マニュアルの URL がまだ無い（開始前）なら、ハブは名前を出し、t3 を `lane:wait` で待って盤を `idle` にする（取りに行かない）。セッションを作り直したら同じ 1 行を貼る。**t3 のベンチ欄**: 本番は「ポータル」、練習は「セットアップ済み、<実行するマシン> から、`--webapp <IP>`」（ベンチの準備は練習の前に人が済ませる。RUNBOOK 付録。レーンの手順には入れない）。ハブは kickoff.md のアクセスの行とベンチの todo に写すだけ。手元 PC から svc1 への `curl` / `dig` は社内プロキシ（Netskope など）で通らないことがあるので、練習の前にプロキシを切っておく（2026-10-27: TLS が中継されて Empty reply）。

## 10. 1 日の流れ（出来事の順。時刻ではない）

```
1: 起動 → URL → kickoff ①（マニュアル保存）→ [TASK] c manual
2:   HELLO → todo(HOSTS) → peek → todo(hosts:setup) → deploy:init → [DONE] servers
3:   HELLO → コードを読む（ルーター、スキーマ、N+1 の目星）。webapp/ が来るまでは kickoff.md / manual
c:   HELLO → [TASK] c manual → 型と先読みの手
2:   hosts:setup 0 NG → 盤が帯 ready + todo(baseline ベンチ) を自動で → [DONE] 2 baseline-ready（deploy:init はその後ろで並行）
1:   ← baseline-ready: task runs → runs:wait AFTER=<一番新しい ts> を背景で
1:   ← [DONE] servers: git pull, ops:repo, kickoff ②（前提の確認）をバックグラウンドで（baseline の todo はもうある）
     同時に [TASK] 2 setup / [TASK] 3 app-map / [TASK] c early-scan、todo(人: task app:size を見る)
2:   parallel-setup（DB 分離 + 計測 + free wins）→ [PR] #a setup      ← baseline を待たない
     + 計測 OFF / ON の [sprint] PR 2 本（open のまま。特別賞と §8 でだけ ship）
3:   app-analysis → docs/app-map.md（PR）→ コードの N+1 の目星
c:   early-scan → [PICK] → (人) [DECIDED] → 1〜3 PR → [PR]
1:   ← baseline の ts: actions:plan → [TASK] 3 fix TS / [TASK] c technique-scan TS   phase=baseline
1:   ← [PR]: todo(レビュー, PR=<n>) → lane:wait を背景で → decision=ok で次の ship に / changes で [TASK] <lane> fix #n
1:   帯が none なら OK の出ている PR を hub:ship（setup → fix → c）→ todo(ベンチ) + 帯 ready（人は即回す）。ready / running の間の OK は帯の NEXT へ
1:   ← run の score（runs:wait）: app:refresh TS → docs/reports/ を main に push → ops:repo（ハブ。人は打たない）
1:   ← ts: phase=loop。以後「run → 判定 → [TASK]（2 = 構成・tuning、3 = fix、c = deep-dive / scan）→ PR → hub:ship → ベンチ」
1:   時刻の区切り（12:30 / 15:00 / 16:00 / 16:30 / 17:00）は [PHASE] で 2 / 3 / c に
1:   ← 合流の目安（§11）に当たった: 人に「合流するか」を CHOICES で聞く → OK なら handoff skill → チームに共有、以後は GitHub Project
```

RUNBOOK の各節（§1〜§9）は手順の詳細。レーンのファイルからリンクしている。

## 11. チームに合流する（いつ、何を渡すか）

A は 4 セッションを 1 人で回して**特別賞（`alp.yml` の `prize_target`）を取り、ある程度スコアを伸ばすところまで**を担当する。チームの残りはその間、別の準備（マニュアルの深読み、次の構造変更の設計、再起動試験の予行など）をする。構成を 1 人で決めて回す方が速いため（2026-10-27: 構成の判断 3 回 = DB → svc3、isudns → svc2、app ×2 で 3,238 → 128,555）。

**合流の目安**（ハブが run の判定のたびに見る。どれか 1 つで人に聞く）:

| 目安 | 見るもの |
|---|---|
| 特別賞の目標を越え、その後の run が 2 本続けて ±5% 以内（伸び悩み） | `task runs` の score |
| 構成が固まった（DB・付随サービスの置き場所・app の台数を変える案が残っていない、または人が保留にした） | journal の構成の行、open の `infra/*` PR |
| report の Verdict が「CPU is not the limit」でベンチ機かベンチの並列が律速（練習）、または残りの手が 50 行超の構造変更ばかり（設計の分担が要る） | summary.md の Verdict、deep-dive の次の手 |
| 15:00（tuning の前）を過ぎた | 時計 |

当たったら、ハブが `task lane:todo LANE=1 MSG="チームに合流するか（目安: <当たったもの>）" CHOICES="合流する|もう少し続ける" …`。「合流する」なら:

1. **handoff skill**（`tools/handoff/SKILL.md`）で `docs/handoff.md` を作る: 変更ごとの表（PR / 何を / 根拠 / 効果 = 前後の run と効いた指標 / 状態）、律速の推移、逆効果だったもの、失格の芽と対処、open の PR とその持ち主、次の手の候補。journal と `task runs` から作る（推測は「推測」と書く）。docs だけの PR で push。
2. 人がそれをチームに共有する（Discord に PR のリンク）。
3. 以後のタスクは **GitHub Project**（RUNBOOK §6f）で管理する。`task actions:issues` は `.env` の `GH_PROJECT` があれば issue を Project にも入れる。次の手の候補は issue にして Project へ。
4. 4 セッションは止めなくてよい。ハブは merge と ship を続け、レーン 2 / 3 / c は Project の issue を `[TASK]` として受ける（担当者が人なら、その issue はレーンに渡さない）。claims は人とセッションの両方が使う。**メンバーは PR を出すだけ**（PR テンプレートの journal の行を埋める）: ハブの `task hub:prs` がレビューの todo にし、A が盤で OK → ハブが ship。Discord で A に中継しなくてよい。

## 12. Codex で回す / 途中で引き継ぐ

**いつ使うか**: Claude の使用量や速さが足りないレーンを OpenAI Codex CLI で回すとき、またはセッションの文脈が膨らんで遅くなった・落ちたレーンを新しいセッション（Claude → Codex、Claude → 新しい Claude）に替えるとき。**ハブは Claude のままを推奨**（Codex には背景の待ちとサブエージェントの代わりが無く、`hub:ship` や `lane:wait` をフォアグラウンドで待つことになる）。まず 2 / 3 / c のどれか 1 つから。

**どのレーンが Codex か**は `tools/lanes/sessions.json` の各レーンの `"agent": "claude" | "codex"`（`task lane:agent` で表示、盤のレーンのカードに Claude / Codex の印）。起動の todo（t1、`task lane:starts`）の CMD はレーンごとのテンプレートで作られる。Codex は `LANE=<n> codex --model <model> --approve-for-me -c sandbox_workspace_write.network_access=true --add-dir "$PWD/.." '<起動の 1 行>'`（フラグの意味・リスク・未確認の点は `AGENTS.md` の「Codex で動くとき」）。Codex は `CLAUDE.md` の代わりに **`AGENTS.md`**（`task agents:md` が CLAUDE.md から作る。CLAUDE.md を変えたら作り直す。`task agents:md CHECK=1` が 1 なら古い）を読み、skill は `SKILL.md` を直接読む。

**合図の運び方**（§5 と [MESSAGES.md](../MESSAGES.md) の形はそのまま）:

| 送り手 → 受け手 | 運び方 |
|---|---|
| Claude ↔ Claude | 今までどおり `SendMessage`（盤の `lane:msg` でもよい） |
| **Codex が片側にいる**（送る・受ける） | **`task lane:msg FROM=<自分> TO=<宛先> MSG="<1 行>"`**（盤に 1 行残る。宛先はレーン番号） |
| 受け手が Claude（相手が Codex） | `task lane:wait-msg LANE=<自分>` を **run_in_background** で流しておく（届くと終わって起きる。読んだら流し直す）。**ハブは sessions.json に codex のレーンが 1 つでもあれば、起動直後から常に流しておく** |
| 受け手が Codex | 起こされない。**手順の始めと終わり、待ちに入る前に `task lane:inbox LANE=<自分>`**、待つ間は `task lane:wait-msg LANE=<自分> TIMEOUT=300` をフォアグラウンドで繰り返す |

盤の web はレーンのカードに直近 4 件のメッセージ（誰 → 誰、時刻、未読は ●）と未読数 ✉n を出す。`task lane:timeline` は送った数と「読むまでの時間」を 5b 節に出す。Codex には Claude Code の hooks が無いので、Codex のレーンの計測は盤のイベント（`lane:status` / `lane:msg` / todo）だけ: `lane:status` を必ず打つ。**経緯（`task lane:note`）は Codex でも同じ**（task を打つだけ。書くときは §2 の「経緯に書くとき」）。`lane:msg` のメッセージは経緯には載らない（合図であって結論ではない。結論は `lane:note` で書く）。`task lane:handoff` の引き継ぎの 1 枚には、そのレーンが書いた経緯の新しい 8 行が載る。

**引き継ぎの手順**（どのレーンでも同じ。人が決めて、ハブかそのレーン自身が回す）:

1. 前のセッションに「手を止めて、今の作業を commit / push して `task lane:status … STATE=waiting NOTE="引き継ぎ中"`」と言う（止められないほど遅い・落ちたなら飛ばす）。
2. repo のルートで **`task lane:handoff LANE=<n>`** → `docs/handoff-lane<n>.md`（盤の行とチェックリスト、そのレーンの open の todo / alert、未読と直近のメッセージ、最後の `[TASK]`、open の claims、open の PR、worktree の未 commit / 先行 commit、レーンのファイルから引いた「次にやること」、後継の起動の 1 行とコマンド）。Claude の hooks が見たセッション名の表（`[HELLO]`）も入る。
3. 後継を Codex にするなら sessions.json のそのレーンを `"agent": "codex"` にしてから 2 を打つ（起動のコマンドが Codex の形になる）。ハブが Codex に替わるなら、他のレーンの起動の 1 行にも lane:msg の注意が付く。
4. 前のセッションを閉じる（2 つのセッションが同じレーンで書かない）。新しいタブで、ファイルの最後の「コマンド」を貼る。
5. 後継は `AGENTS.md` / `CLAUDE.md` → この README → レーンのファイル → `docs/handoff-lane<n>.md` を読み、`task lane:inbox LANE=<n>` と `task lane:board` を見てから続ける。Claude の後継は `[HELLO]` を送り直す。**ハブの後継（Claude）は `[HELLO] 1 <新しい名前>` を 2 / 3 / c に送る**（Claude のレーンへは SendMessage、Codex のレーンへは `lane:msg`）。受けたレーンは以後その名前に送る。
6. `docs/handoff-lane<n>.md` は手元のメモ（commit しなくてよい。人に見せるなら docs だけの PR）。
