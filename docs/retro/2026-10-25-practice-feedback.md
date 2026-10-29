# kit へのフィードバック（ISUCON13 練習 2026-10-25、1 人で 4 セッション）

isucon-kit に反映する候補をまとめたもの。「人」は練習中に人が気づいたこと、「ハブ」はレーン 1 のセッションが気づいたこと。原因の欄で、確かめていないものには「推測」「未調査」と書いた。

## 流れ（JST。ベンチの時刻は run の TS から）

| 時刻 | 出来事 |
|---|---|
| 23:04 | ハブ起動、レーン 2 / 3 / c の起動 todo |
| 23:06 | マニュアル URL → kickoff ①（保存 + kickoff.md 下書き） |
| 23:08〜23:11 | `task hosts:setup`（181 s。svc1 の init 125 s → svc2 / 3 の join を並列） |
| 23:13 | `[DONE] servers`（deploy.sh 済み）+ PR #1（練習専用: DNS の答えを svc1 の private IP に） |
| 23:15〜23:17 | 人の OK → `hub:ship #1`（ship.sh の bug でベンチ todo が出ず、ハブが手で作成） |
| **23:20** | **baseline 3,074**（run 20261025-142038） |
| 23:24〜23:27 | `hub:ship #2` が `git pull --ff-only` で失敗（a19）→ ハブが手で pull → deploy → smoke |
| 23:26 | `hub:ship 3 4 5` が #3 の衝突で全体停止（a20）→ #4 #5 だけ出し直し |
| **23:30** | **2 回目 7,365**（run 20261025-143032。DB 分離 + free wins + pdns-quiet + icon_hash） |
| 23:32〜23:36 | `hub:ship #7`、`hub:ship #8` |
| **23:36** | **3 回目 54,815**（run 20261025-143647。特別賞の目標 50,000 を超えた。新しい 5xx: POST /api/icon の deadlock 123 件） |
| 23:37 | #3（app-map、docs のみ）をやっと merge |
| 23:45 頃 | 切り上げ（#9 #10 #11 は open のまま） |

## 人が気づいたこと

| # | 気づき | 事実 / 原因 | 直し方の案 |
|---|---|---|---|
| 1 | `task hosts:setup` が遅すぎる。初回ベンチが遅れた | 全体 181 s。うち svc1 の **「1b. stop background apt」だけで 93 s**（`+1s` → 次の節が `+94s`）。`systemctl disable --now unattended-upgrades` / `mask --now apt-daily.service` が、走っている apt の終わりを待って止まっていると推測（未調査）。次に svc1 の init（125 s）が終わるまで svc2 / 3 の join が始まらない | ① apt の停止を `--no-block`（`systemctl stop --no-block` + mask）にし、待たない。走っている apt は `pkill` で十分（ベンチまでに止まればよい）② svc2 / 3 の join の前半（apt / alp / slp の導入、nginx の ltsv、slow log、agent、ops 鍵）は repo を要らないので、svc1 の init と**同時に**流し、git の join だけ init の push を待つ ③ alp / slp は事前に作ったバイナリを scp（ダウンロードを待たない）④ 各節の秒数をサマリーに出す（今回は手で出力を読んで 93 s に気づいた） |
| 2 | app-analysis（app-map）が見られるようになるのが遅かった | #3 は 23:19 に PR、merge は 23:37（18 分）。① `tools/app-analyzer/analyzer.config.json` を **kickoff phase 2（ハブのサブエージェント）と app-analysis（レーン 3）の両方が新規作成**して衝突 → rebase ② rebase 後、`docs/journal.md`（ハブの直接 commit と衝突）でまた衝突 → `hub:ship 3 4 5` が #3 で止まり、#4 #5 も巻き添えになった ③ docs だけの PR も人の OK → `hub:ship`（deploy + smoke 付き）という同じ道を通った。どれも app-map の中身とは関係ない待ち | ① analyzer config の持ち主を 1 つに（kickoff phase 2 が作る。app-analysis は読むだけ、直すなら別 commit）② docs だけの PR（`docs/app-map.md`、`docs/reports/`、`docs/plans/`）は fast lane（ハブが `gh pr merge` + `task ops:repo`、deploy しない）③ `hub:ship` は 1 本の衝突で全体を止めず、衝突した PR を飛ばして残りを出す（今は最初の失敗で止まる）④ journal は下の「ハブ #3」 |
| 3 | `task lane:web` の進捗で、レーン 1（ハブ）の step が全部 ready のまま | ハブが `task lane:step ID=H1…H5 STATE=doing/done` を**一度も打たなかった**（1-hub.md には書いてあるが、トリガー表の「出すもの」に入っていない）。ベンチの B1〜B3 は panel の score から自動で done になる | ① ハブの step を出来事から自動で進める: H1 = `docs/manual/` の commit、H2 = kickoff ② の commit、H3 = `task actions:plan`、H4 / H5 = `task hub:ship` の成功。`lane:phase` / `actions:plan` / `hub:ship` の中で `lane:step` を呼ぶ ② 1-hub.md のトリガー表の「出すもの」に `lane:step` を足す |
| 4 | PR レビューを良くしたい。lane:web から PR に飛びたい。レビュー todo に「merge OK」ボタンを付け、押したらブラウザから Claude セッションに伝えたい | 今はレビュー todo の CMD が `gh pr view N --web`（コピーしてターミナルで打つ）。OK はハブのタブに手で打っていた（「#2 merge ok」「OK #3 #4 #5」）。**仕組みはほぼある**: `task lane:wait ID=<todo>` は ✅ で終わってレーンを起こす（lanes.py `wait_todo`）。ハブはこれを使っていなかった | ① todo に `PR=<n>` を持たせ、lane:web が `https://github.com/<repo>/pull/<n>` のリンクを出す（repo は `GIT_REMOTE` から）② `PR=` 付きの todo には ✅ の代わりに **「merge OK」「要修正」** の 2 ボタン。押すと todo に `decision=ok|changes` と時刻を書く ③ ハブはレビュー todo を作ったら `task lane:wait ID=<id>` を run_in_background で流す（`[PR]` を受けたら必ず）→ ボタンで起き、`decision=ok` なら `hub:ship` ④ ルールの変更が要る: CLAUDE.md ルール 4 は「OK はハブのタブで人が言う」。**盤のボタンも人の OK として数える**と書き換える。偽装の対策として、ボタンの POST だけが `decision` を書けるようにし、`task lane:*` のコマンドからは書けないようにする（セッションは `.claude/settings.json` で lane:web の API への curl を拒否）。完全には防げないので、merge の journal に「OK: 盤 hh:mm」と残す |
| 5 | 並行して色々直していて、いつベンチを回すのか分からなくなる。todo に出た瞬間に回してよいのか | **回してよい**（ベンチの todo は merge → deploy → smoke が通った後にだけ出る）。分からなくなった原因: ① 自動のベンチ todo が「ポータルで実行」で、練習の svc2 のコマンドに差し替えるため**ハブが todo を閉じて作り直した**（t22 → t23、t24 → 閉じて #8 の後に t27）② 出した後に「#7 も先に入れるか」を聞いた（ベンチ todo が出ているのに中身が変わりうる状態）③ どの PR が今回に載り、どれが次回かが todo に無かった | ① **ベンチ todo が出ている間、ハブは ship しない**（次の PR は「次のベンチ」と todo に書く）。盤の上に大きく「BENCH: いま回してよい（main <sha>: #7 #8）/ 実行中 / collect 待ち」を出す ② ベンチ todo の MSG に「載っている PR」と「次に回す PR」を必ず書く ③ 練習は `.env` の `BENCH_CMD` を `hosts:setup` が作る（svc2 の ssh + `--nameserver <svc1 private>`）。本番はポータルの操作文 ④ README §0 の「ベンチは早く、何度も」に「todo が出たら即。parallel-fix を待たない」を 1 行足す（実際に聞かれた） |
| 6 | ops app から app-analysis への遷移を分かりやすくしたい | 入口は panel のヘッダーにある白い小さなリンク「app-analysis report」1 つだけ（`panel.go:738`、別タブで `/app/`）。中身は ops の clone の `docs/reports/latest.html`（`panelapp.go`）なので、**main に merge されるまで 404 の文字だけ**（今回は #3 の merge 23:37 まで）。いつの巡回か、どの commit か、まだ無いのかが画面から分からない。盤（`task lane:web`）からはリンクが無い | ① panel の上にタブかカードで「App analysis」を置き、状態を出す（「まだ無い: app-analysis 実行中 / PR #n レビュー待ち」「hh:mm の巡回、commit xxx」）② merge 前でも見られるように、`/app/?ref=<branch>` で ops の clone の `git show origin/<branch>:docs/reports/latest.html` を返す（読むだけ）③ 盤（lane:web）のヘッダーと、レーン 3 の app-map の step のカードに `/app/` へのリンク ④ run の report の endpoint 行から app-map の該当行（画面 → API → ハンドラ）へのリンク（`docs/app-annotations.json` の endpoint をキーに） |

## ハブが気づいたこと

| # | 気づき | 事実 / 原因 | 直し方の案 |
|---|---|---|---|
| 1 | `hub:ship` が `git pull --ff-only` で落ちた（a19） | `fatal: Cannot fast-forward to multiple branches.`。同じ `git pull --ff-only origin main` を手で打つと通った。直前に PR #6 が人の手で merge されていた。原因は未調査（ship.sh の `git pull --ff-only -q origin main` と FETCH_HEAD / ブランチ設定の組み合わせを疑う） | ship.sh を `git fetch -q origin main && git merge --ff-only -q FETCH_HEAD` に分けて再現を見る。失敗したら 1 回 fetch し直してからやり直す |
| 2 | ship.sh の `$merged）` が unbound variable | 全角括弧が変数名の続きとして読まれた（`set -u`）。ベンチ todo が作られずに終わった | PR #6 で `${merged}` に直した → **kit に同期** |
| 3 | `task runs` / `runs:wait` が ops に ssh できない | Taskfile の `env:` は、シェルで export された `SSH_KEY` が `.env` より優先される | PR #6 で値をコマンドの前に直接書く形にした → **kit に同期**。`env:` に `.env` の値を書いている他の task も同じ形か確かめる |
| 4 | `docs/journal.md` が衝突の元 | ハブの直接 commit、レーン 3 の PR（#3 #8）、レーン c の PR（#9）の 3 か所から書き込み。2 回衝突した | journal はハブだけが書く（main に直接）。レーンは PR 本文に「journal 用の 1 行」を書き、ハブが merge 時に写す。または `docs/journal/<lane>.md` に分ける |
| 5 | docs だけの PR でも deploy + smoke（約 2 分）を通る | #3 #9 は docs のみ | `hub:ship` が差分を見て、`webapp/`・`etc/`・`deploy.sh`・`env*.sh` に触れなければ deploy を飛ばし `ops:repo` だけ |
| 6 | 練習専用の PR #1 が baseline を待たせた | RUNBOOK 付録は「レーン 2 が `[DONE] servers` の前に済ませる」だが、PR → 人の OK → ship の道を通った（約 5 分） | 練習専用の `env.sh` の変更は `practice/` ブランチを `deploy:init` と同じ commit で main に直接入れる（練習のみの例外として CLAUDE.md に書く）。または `hosts:setup` が `.env` の `PRACTICE=1` を見て自動で入れる |
| 7 | レーン c が `[DECIDED]` の前に実装と PR を出した。T41 の質問がレーン c とハブで 2 重（t12 / t13） | c-scout.md の `[PICK]` の流れが守られなかった。todo の宛先がレーン c のタブとハブのタブに割れた | c-scout.md に「`[PICK]` の後は `[DECIDED]` まで実装しない（設計まで）」。人への質問はハブだけが todo にする（レーンは `[PICK]` / `[NEED-OK]` を送るだけ） |
| 8 | index の追加で deadlock が生まれた | #8 の `icons(user_id)` の index で、`postIconHandler` の DELETE + INSERT が gap lock でぶつかり 500 が 123 件（3 回目の run）。smoke（逐次）では出ない | parallel-fix の index 追加のチェックに「同じ表に `DELETE … WHERE <その列>` + INSERT のパターンがあれば、gap lock の deadlock を疑う」を足す。report の Errors 節が拾えたのは良かった（a30） |
| 9 | 自動のベンチ todo の後、ハブが自分の判断で ship を足した | #8 の OK が t24 の直後に来て、t24 を閉じて #8 を入れた | 上の人 #5 と同じ（ベンチ todo の間は ship しない、が決まり。今回は人の OK が先に来たので例外にした） |
| 10 | 変更禁止の `aws-env-isucon-subdomain-address.service` が起動時に `env.sh` を書き換えるかもしれない（kickoff phase 2 の指摘。推測） | 再起動試験をしていないので未確認 | 次の練習で `task reboot-test` の後に `env.sh` の `ISUCON13_POWERDNS_SUBDOMAIN_ADDRESS` を `task host:diag` で見る項目を RUNBOOK §8 に |
| 11 | ハブが `[PR]` を受けてもレビュー todo の完了を待つ仕組み（`lane:wait`）を使わず、人のタブ入力を待っていた | 1-hub.md のトリガー表に `lane:wait` が無い（README §4 にだけある） | 1-hub.md の `[PR]` の行に「todo の直後に `task lane:wait ID=…` を run_in_background」を書く（人 #4 とセット） |
| 12 | 練習では再起動試験（RUNBOOK §8）まで行かなかった | 切り上げた | 次の練習は 3 回目のベンチの後に再起動試験だけでも通す |

## kit に同期するもの（コンテスト repo で直したもの）

- `tools/lanes/ship.sh`（`${merged}`）、`Taskfile.yml` の `runs` / `runs:wait` / `lane:web`（SSH_KEY を inline）— PR #6

## 直したこと（kit の文書側。ツール側は別の PR）

| 原本 | 直したこと | 場所 |
|---|---|---|
| 人 #5、ハブ #9 | ベンチの todo（帯 `ready`）が出たら即回す、parallel-fix を待たない。帯が `ready` / `running` の間はハブは ship しない、後から OK の PR は帯の `NEXT`。ベンチの todo に「載っている PR」「次に回す PR」 | lanes README §0 / §2 / §4 / §7、1-hub.md、RUNBOOK の「人に見えるもの」と §6 |
| 人 #4、ハブ #11 | `[PR]` → `task lane:todo … PR=<n>` → `task lane:wait` を背景で → `decision=ok` で ship / `changes` で `[TASK] <lane> fix #n`。盤の merge OK ボタンも人の OK（メッセージの OK は数えない）。journal の merge 行に「OK: 盤 hh:mm / タブ」 | CLAUDE.md ルール 4、lanes README §2 / §4 / §7、1-hub.md、MESSAGES、parallel-fix |
| 人 #2、ハブ #5 | docs だけの PR を fast lane の (d) に。`hub:ship` が deploy・smoke を飛ばす | CLAUDE.md ルール 4、RUNBOOK §6、3-app.md、app-analysis SKILL |
| ハブ #4 | journal はハブだけ（ルール 4 の例外 c）。レーンは PR 本文に「journal 用の 1 行」、効果は `[DONE] <lane> effect` | CLAUDE.md ルール 4 / 7、lanes README §6、各レーン、MESSAGES、parallel-fix、IMPLEMENTATION-NOTES §9 |
| 人 #2 | analyzer config の持ち主は kickoff phase 2、app-analysis は読むだけ | kickoff SKILL §2、app-analysis SKILL §1、3-app.md |
| 人 #3 | kickoff phase 1 / 2 の終わりに `lane:step ID=H1 / H2`、H3〜H5 は自動 | kickoff SKILL、1-hub.md |
| ハブ #7 | `[PICK]` の後は `[DECIDED]` まで実装しない。人への質問の todo はハブだけ | c-scout.md、lanes README §4、RUNBOOK §4-C、各レーン、MESSAGES |
| ハブ #6 | 練習専用の前提は PR を通さず main に直接（ルール 4 の例外 d、練習のときだけ） | CLAUDE.md ルール 4、2-infra.md、RUNBOOK 付録 |
| ハブ #8 | index を足す表に DELETE → INSERT があれば gap lock の deadlock を疑う | parallel-fix SKILL、IMPLEMENTATION-NOTES §5 |
| ハブ #10 | reboot の後、変更禁止のサービスが `env.sh` を書き換えていないかを `task host:diag` で見る（推測） | RUNBOOK §8-3 |
| ハブ #12 | 次の練習は 3 回目のベンチの後に再起動試験だけでも通す | RUNBOOK 付録 6 |
| 人 #6 | `/app/?ref=<branch>` と App analysis のタブを文書から指す（実装はツール側） | RUNBOOK、app-analysis SKILL |
| 人 #1、ハブ #1〜#3 | 文書は落とし穴と未検証の一覧だけ（実装はツール側） | HANDOFF §5 / §6、RUNBOOK §2 |
