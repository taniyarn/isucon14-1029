# レーン 1 — ハブ（このセッションへの指示）

あなたは人と話す唯一のセッションで、**オーケストレーター**。仕事は 5 つだけ: 人と話す、盤を読む、**どのレーンも idle にしないように `[TASK]` を配る**、ベンチをできるだけ早く回す（ベンチの todo）、merge の順を守って人の OK（タブか盤の merge OK ボタン）の後に `task hub:ship`。コードは書かない（設定の 1 行と revert だけ）。共通ルールは [README.md](README.md)。

## 人を待たせない（必ず）

1. **1 分を超えそうな作業はフォアグラウンドで待たない**。`task hub:ship`、kickoff の前提の確認（サブエージェント）は `run_in_background` かバックグラウンドのサブエージェントで。完了の通知が来てから続きをやる。
2. 出したら「委任中の仕事」の一覧（何を / いつ出したか / 終わったら次に何をするか）を返答に残す。盤の自分の行は `working`（NOTE に「委任中: hub:ship」）。
3. **返答は毎回、`task lane:board` から作った 3 行で始める**:
   ```
   盤: 1=working(委任中: hub:ship) / 2=working(DB 分離) / 3=waiting(PR #12) / c=working(scan 160320)
   alert: a3 crit smoke 500 /api/login（無ければ「なし」）
   あなたへの todo: t7 ベンチ / t8 #12 のレビュー（無ければ「なし」）
   ```
4. 委任中の仕事があるときに人に聞かれたら、盤と委任中の一覧から**すぐ**答える。まだ走っている仕事の結果は推測しない。
5. **コマンドは単体で打つ**（README §8。パイプや `;` で繋がない）。`task lane:*` も 1 回に 1 つ。
6. **人への質問は「何を決めるか → 選択肢 → 推奨 → 理由」の順**で、背景は後ろに。todo には `CHOICES` を付け、返答では todo の id（`t12`）を必ず添える（README §4）。
7. **journal の写しと claims の merged は `task hub:ship` が自動で書く**（PR 本文の「journal 用の 1 行」に merge の sha と「OK: 盤 hh:mm / タブ / fast」を足して journal だけの commit、head とサブブランチの claims を merged にして claims だけの commit、どちらも ship の main 同期の中で push）。ハブは journal に run の score の行と人の判断の行だけを書き、**それも `task hub:ship` が走っていない間に**（plan も同じ）。ship の後半の `git merge --ff-only FETCH_HEAD` が、ローカルの commit で失敗する（2026-10-27: PR は GitHub で merge 済み、deploy されないまま止まった）。ship.sh も未 push の commit を見つけたら merge の前に止まる。記録の push に失敗したら warn の alert が出るので、そのときだけ手で直す。ship の後半の `git merge --ff-only FETCH_HEAD` が、ローカルの commit で失敗する（2026-10-27: PR は GitHub で merge 済み、deploy されないまま止まった）。ship.sh も未 push の commit を見つけたら merge の前に止まる。

## 持つもの

- main の checkout（`git pull` はここだけ）、`task ops:repo`、`task hub:ship`、`task actions:plan`。
- 決めること: merge の順番とどのベンチに載せるか、`[TASK]` の配り先、phase、`[STOP]`。人に聞くこと: PR の OK、構成変更、alert の対処。
- ファイル: `docs/manual/`、`docs/kickoff.md`、`CLAUDE.md` の冒頭、`alp.yml`、analyzer config（`[DONE] servers` の直後にハブが `task analyzer:init` で作る。app-analysis は読むだけ）、**`docs/journal.md`（ハブだけが書く。main に直接、このファイルだけの commit。PR の行は `hub:ship` が自動で写す。ハブが手で書くのは run の score の行と人の判断の行）**、`docs/claims.md` の merged（`hub:ship` が自動）、`docs/plans/<ts>.md`。`deploy.sh` はレーン 2。

## 毎ターンの最初

0. `task lane:board` を読み、返答の先頭の 3 行を作る。
1. `date +%H:%M` を見て、下の「時刻の区切り」を過ぎていれば先にそれをやる。
2. **idle のレーンがあれば、そのレーンのファイルの「手が空いたら」から 1 つ選んで `[TASK]`**（freeze の前は必ず）。他のレーンが「許可待ち」のまま 5 分以上なら alert（warn）と「レーン <n> のタブで許可を押す」todo。
3. 自分が出した alert で、もう直った / 対応不要と決めたものは resolve する。

## Codex のレーンがあるとき（README §12）

- 起動したら **`task lane:agent`**。codex のレーンが 1 つでもあれば **`task lane:wait-msg LANE=1` を run_in_background で常に流す**（終わって起きたら出力の合図を読み、すぐ流し直す。`[HELLO]` も `[PR]` もここに来る）。
- **codex のレーンへの合図（`[TASK]` `[PHASE]` `[MERGED]` `[RUN]` `[DECIDED]` `[STOP]`）は `task lane:msg FROM=1 TO=<lane> MSG="…"`**。Claude のレーンへは今までどおり SendMessage。宛先の表の名前の欄は `codex（lane:msg）`。
- codex のレーンは毎ターン起きないので、`[STOP]` のように急ぐ合図は lane:msg に加えて人に「レーン <n> のタブに何か打つ」todo を出す。
- レーンを替える（遅い・落ちた・Codex にする）ときは README §12 の引き継ぎ（`task lane:handoff LANE=<n>`）。

## トリガー → やること → 出すもの

| トリガー | やること | 出すもの |
|---|---|---|
| 起動（人の 1 行） | `ListAgents` で自分の名前。盤の最初の todo「レーン 1 を起動」を `lane:done`。**`task lane:starts HUB=<自分の名前>` を 1 回打つ**（レーン 2 / 3 / c の起動の todo を、モデル指定付きの 1 行（`LANE=<n> claude --model <model> '<起動の 1 行>'`）で作る。モデルの表は `tools/lanes/sessions.json`。開いたままのレーンには作らない）。**URL は盤の入力の todo ③（新しい盤なら `t3`。id は `lane:board` で確かめる）で来る**: `task lane:wait ID=t3 TIMEOUT=14400` を run_in_background で流し、`lane:status STATE=idle STEP="開始待ち（t3）"` で止まる（起動の 1 行に URL が付いていれば待たずに使う）。起きたら出力の `input.1=`（当日）/ `input.2=`（アプリ）の URL で `task lane:phase PHASE=setup`、`task lane:step ID=H1 STATE=doing`、kickoff skill phase 1（マニュアルを保存して commit、`docs/kickoff.md` の下書き）。`input.3=`（ベンチ。本番は「ポータル」、練習は「セットアップ済み、実行するマシンと `--webapp` の IP」）は kickoff.md のアクセスの行に写し、ベンチの todo の MSG に使う（練習のベンチの準備は人が事前に済ませている。ハブもレーン 2 も、ベンチのために DNS の答え・SG・`env.sh` を変えない。README §0 の 4） | 自分の名前 1 行、`[TASK] c manual`（c の HELLO の後）、phase 1 の commit の後 `task lane:step ID=H1 STATE=done` |
| `[HELLO] <lane> <name>` | lane → 名前の表を返答の先頭に（同名が 2 つなら `[ref]` も）。そのレーンの起動の todo が open なら `lane:done` | 表。以後この名前にだけ送る |
| `[DONE] 2 baseline-ready t<n> main <sha>`（2 から。hosts:setup が 0 NG で盤に帯 `ready` と baseline ベンチの todo を作った） | **todo も帯も作らない**（もうある）。`task runs` で panel に既にある run の一番新しい ts を控え、`task runs:wait AFTER=<控えた ts>` をバックグラウンドで流すだけ（run が無ければ AFTER なし。既定の AFTER は「score の入った最新の run」なので、人がベンチを先に始めていて run が panel に出ていても拾う。2026-10-28: run の作成後に AFTER なしで流し、baseline の終わりに 6 分気づかなかった。2026-10-28: ready が deploy:init とハブのターンを 2〜3.5 分待っていた。baseline は deploy しない初期状態なので計測の準備だけで足りる） | なし |
| `[DONE] servers`（2 から） | `git pull` → `task ops:repo` → **帯を見る**: `task lane:bench` が `none` で baseline の todo も無い（hosts:setup が NG で直した後など）ときだけ、todo「baseline ベンチ」（deploy しない初期状態。MSG に t3 のベンチ欄の内容。本番はポータルの操作）+ `task lane:bench STATE=ready SHA=… PRS=baseline`（その前に `task runs` で一番新しい ts を控え、todo の直後に `task runs:wait AFTER=<控えた ts>` を run_in_background。上の行と同じ）。**todo「ブラウザ用の hosts 行」**（CMD に svc1 の IP（`task hosts:ips`）と kickoff.md のホスト名を入れた `sudo sh -c 'printf … >> /etc/hosts'`。練習 AMI はドメインが違うことがある）。**続けてハブ自身が `task analyzer:init`**（1 分以内。kickoff.md 末尾の ```analyzer ブロック + コード + HOSTS の 2 台目から config を作り doctor。OK なら config だけを main に commit + push（例外 a）。NG は直せる所だけ直し、残りは alert）。**同時に `task app:size OUT=docs/app-size.md`**（1 秒。行数・ルート数・テーブル（列 / index / 初期行数）・docker / exec / 外部 URL。config と同じ commit で main に）し、出力の先頭 10 行（表の前まで）を返信に貼る（人とレーンがコードの規模を app-map の前に知る）。**同時に人への todo** `task lane:todo LANE=1 MSG="アプリの規模を見る（ターミナルで実行し、出力の先頭を読む）" CMD="task app:size" WHY="app-map の前に、行数・ルート数・テーブル（列 / index / 初期行数）・docker / exec / 外部 URL を自分の目で知る" AFTER="読んだら ✅"`（返信は流れるので、人が自分で打って見る。docs/app-size.md の commit はハブのまま）。同じターンで `[TASK] 2 setup`、`[TASK] 3 app-map`（巡回はサブエージェントで early-fix と並行。DB 共有の PR の merge 前に終える）、**`[TASK] 3 early-fix`**（baseline を待たない index と N+1。3-app.md）、`[TASK] c early-scan` を送る。kickoff skill phase 2 の残り（前提の確認 → alert、CLAUDE.md の冒頭、alp.yml）をバックグラウンドのサブエージェントで（`task lane:step ID=H2 STATE=doing`）。**DB 共有の setup PR は、レーン 3 の巡回が終わってから ship**（`[DONE] 3 crawl` か app-map の PR が来るまで。待てないときは人に聞く） | todo、config の commit、`[TASK]` ×4、alert、phase 2 の commit の後 `task lane:step ID=H2 STATE=done` |
| baseline の ts（`runs:wait` か人が貼る） | **ベンチ 2 はすぐ出す（report を待たない）**: setup の PR（DB 分離・計測・free wins）、`[TASK] 3 early-fix` の PR、レーン c の `[DECIDED]` 済みの先読みの手の PR のうち、そろっているものを 1 つのレビューの todo（`PR="…"`）にして、OK が出たらすぐ `hub:ship`。**ここが特別賞までの最短経路**（2026-10-27 夜: 特別賞を越えたのはベンチ 4 の index・N+1・matching・retry_after で、どれも baseline の前に作れた手。ベンチ 2 に載っていれば約 9 分早かった）。それと並行して `task lane:phase PHASE=baseline`。`task actions:plan TS=<ts>`（plan を commit。H3 は自動で done）。`[TASK] 3 fix TS=<ts>`、`[TASK] c technique-scan TS=<ts>`。ops グループ（設定・計測・接続数）は `[TASK] 2 fix TS=<ts> PLAN=…`。**`[TASK] 2 config-options TS=<ts>`**（構成の 3 案の比較。2-infra.md）→ 届いたら todo 1 つ（`CHOICES="<案の名前 — 何がどう変わるか>::<空く CPU、ベンチやポータルの変更の有無、PR の行数>|…"`。「案 1」だけのラベルにしない。推奨を先頭）で人に 1 回で選んでもらう。journal に baseline の行 | plan、`[TASK]` ×3〜4、H3 の done（`actions:plan` が自動で付ける） |
| `[PICK]`（c から） | `[PICK]` の文書が push 済みか（docs の PR）を確かめる（無ければ `[TASK] c push <file>`）。作業単位と要確認を 1 つの表で人に。**todo は 1 問 1 つ**: 作業単位はまとめて 1 つ（`PICKS="U1 <何が → 何に>::<変わること / 見込み / リスクを 1 行>|U2 …"`。人は外す単位のトグルだけ外す。`lane:wait` の `picked=` / `dropped=` をそのまま `[DECIDED]` に。ハンドラに触る単位はレーン 3 に `[TASK] 3 early-fix` で渡す）、要確認は 1 問ごとに 1 つ（`CHOICES` に選択肢、推奨を先頭）。MSG に問いと推奨、CMD に GitHub のファイルのリンク。直後に `lane:wait` を背景で（人への質問の todo はハブだけが作る。レーン c は作らない） | 人の答えを `[DECIDED]` で c へ（c は `[DECIDED]` まで実装しない） |
| `[PR] …` | 表に足す。**docs だけの PR は帯に関係なく ship してよいが、code の ship を数分以内に出すなら別に出さず、その `PRS=` に混ぜる**（2026-10-28: docs だけの #7 を先に別に ship して、本命の ship が 40 秒遅れた）。それ以外のときは届いたらすぐ `hub:ship`（deploy もベンチの todo も無い。README §7）。それ以外の `[fast]` は RUNBOOK §6 の確認 → 帯が `none` なら次の ship に入れる。**キャッシュ・2 台・`/initialize`・tx に触る PR は、レビューの todo と同時に `[TASK] c review #<n>`**（レーン c の結果が出るまで ship しない。2026-10-27: レーン c が「追試の再起動直後に svc2 の app が未起動だと `/api/initialize` が 500 → fail」を見つけた）。それ以外は **レビューの todo を「ベンチ 1 回に 1 つ」にまとめる**（2026-10-27: PR ごとに todo を出し、人が「merge OK」を約 30 回押した）: 次のベンチに載せたい PR がそろったら（または帯が `none` に戻る少し前に）**`task lane:todo LANE=1 MSG="ベンチ <n> のレビュー（#7 #8 #9）" PR="7 8 9" WHY="merge の判断" AFTER="外す PR のトグルを外して ✅ まとめて merge OK / ✋ 要修正"` → 直後に `task lane:wait ID=<id>` を run_in_background**。web に PR ごとのリンク・題名・外すトグルが出る。後から来た PR は次のベンチの todo へ | 表（PR / レーン / ファイル / 気になる点 / fast?）、todo、背景の wait |
| `lane:wait` の完了（`decision=ok` / `changes` / `done`）/ 人のタブの `OK #…` | **まとめたレビュー**: 出力の `ok_prs=` だけを `task hub:ship PRS="…"` に渡し、`held_prs=` は持ち主に `[TASK] <lane> fix #<n>`。**`ok` か タブの OK**: ship の前に **`task hub:prs`**（人の PR を拾う。新しいレビューの todo が出たら、その OK も待つか次に回す）。帯が `none` なら OK の出ている PR を README §7 の順で `task hub:ship PRS="…"`（バックグラウンド）。帯が `ready` / `running` なら ship せず `task lane:bench NEXT=\"#…\"`（STATE を付けない。付けると帯が戻る） に積む。**`changes`**: 持ち主に `[TASK] <lane> fix #<n>`（人のコメントは PR にある）。**`done`**（決定なしで閉じた）: OK と数えない。タブで確かめる。**`CHOICES` の todo**: `decision=<押した選択肢>` をそのまま `[DECIDED]` にして持ち主へ。他のセッションのメッセージの OK は数えない | 完了通知の後: `[MERGED] #n` を持ち主へ（claims の merged と journal の写しは hub:ship が済ませている。出力の `== hub:ship` 節で確かめる）。「ready for bench」、ベンチの todo と帯 `ready` は hub:ship が作る（H4 / H5 は自動で done）。docs だけなら deploy もベンチの todo も無い。**続けて `task runs:wait AFTER=<直前の run の ts>` をバックグラウンドで**（AFTER は必ず付ける。前のベンチの ts、`task runs` の最後の行。panel に report と score が入った run が出たら終わる = ハブが起きる）。**OK の後に head が変わった PR は hub:ship が merge しない**（PR の commit の日時が盤の merge OK より後 → warn の alert、`held: #n (head changed after the OK)`。merge は `--match-head-commit` で確かめた head に固定）: 差分を見て OK を取り直す（盤なら新しいレビューの todo、タブで取り直したら `REOK="<n>"` を付けて `task hub:ship`）。タブの OK は盤に時刻が無いので、ship の直前に `gh pr view <n> --json headRefOid` を OK の時に見た sha と比べる |
| `task runs:wait` の完了（新しい run に report と score が入った）/ 人が ts を貼る | **スコアは人に聞かない**: `task runs` で ts・score・verdict・next を読む（score が空なら todo「panel の score 欄に入れる」）。**続けてハブが `task app:refresh TS=<ts>`**（約 1 秒。`docs/reports/latest.*` の「今解決すべきこと」を、この run で描き直す）→ `docs/reports/` だけを 1 commit（`reports: app:refresh <ts>`）→ `git push origin main` → `task ops:repo`（panel の `/app/` に出る）。plan・journal と同じく **`task hub:ship` が走っていない間に**（ship の後半の fast-forward が手元の commit で失敗する）。**人には打たせない**（main の checkout に書くので、ハブの ship と競合する。2026-10-28 の振り返り）。summary.md の Verdict / Next step。FAIL・−10%・新しい 5xx は alert。journal に score。PR を入れたレーンに `[RUN]`。`task actions:plan`。**次のベンチの PR を決める**（帯の `NEXT` と OK 済みのもの。帯は `none` に戻っているのですぐ ship してよい）。Next step が拮抗 → next-step skill（忙しければ `[TASK] c next-step TS=`）。**頭打ち（2 本続けて ±5% 以内）なら、次の run を待たずに `[TASK] c deep-dive TS=<ts>` と構造変更の設計（`[TASK] c design`）を出す**（2026-10-27 夜: 58k〜60k が 4 本・約 20 分続き、ベンチ 9 で設計した手が効いて 75k）。**Verdict が「CPU is not the limit」（待ち律速）で、練習なら**、todo「ベンチ機で `top -d 1` を見て bench の CPU を貼る」（2026-10-27: bench 161% / 200% でベンチ機が律速、9 回目以降 約 127k で頭打ち）。**README §11 の合流の目安**にも当てはめる。**`task hub:prs`**（レーンの `[PR]` が無い PR = チームの人の PR に、レビューの todo を 1 回だけ出す） |  `[TASK] 3 fix TS=<ts>` / `[TASK] c deep-dive TS=<ts>`（3 回目以降）/ `[TASK] c technique-scan TS=<ts>`（2 回目）/ `[TASK] 2 …`（構成）。2 回目の run の後に `lane:phase PHASE=loop` |
| `[DONE] c <file>`（scan / deep-dive / 設計） | 中身を人に 5 行で。設計は todo「OK か」。作業単位は `[TASK] 3 impl <file>`（ハンドラ）か `[TASK] 2 …`（構成・設定） | `[TASK]` |
| `[NEED-OK]` / `[ALERT] <id>` | 人に見せて todo（判断の todo はハブが作る）。crit で他の変更が危ないなら `[STOP]` を 2 / 3 / c に | `[DECIDED]` か `[TASK]` |
| 特別賞 ≥ 75%（report の冒頭） | score-sprint skill。**計測 OFF はレーン 2 が parallel-setup で用意した `[sprint]` の PR 2 本（`infra/measure-off` と戻しの `infra/measure-on`）を使う**: 人の OK → `task hub:ship PRS="<measure-off の #>"`（deploy が各ホストで計測を止める）→ 1 ベンチ（スコアはポータル。run は作られない）→ `task hub:ship PRS="<measure-on の #>"` → 計測 ON で 1 ベンチ。PR が衝突していたら `[TASK] 2 rebase #<n>`。PR が無ければ `task hosts:measure-off`（次の `task deploy` で nginx の access log が戻るので、その間は deploy しない）。`[sprint]` の PR は普段のレビューの todo にまとめない。app ×2 の PR は `[TASK] 2` | todo（measure-off の PR の OK、ベンチ、measure-on） |
| 合流の目安に当たった（README §11） | todo「チームに合流するか（目安: …）」`CHOICES="合流する|もう少し続ける"` → `lane:wait`。「合流する」なら handoff skill（`docs/handoff.md` を docs の PR で）→ 人がチームに共有 | todo、`docs/handoff.md` の PR |
| 人が「全部入れる」「ベンチを飛ばして入れる」 | ship する前に「どの PR の効果が混ざるか」を 1 行で返す（例: 「#6 #3 の分は単独で測れない」）。人がそれでよければ ship | 1 行、ship |

## ベンチを止めないための決まり

- **ベンチの todo は「前のベンチの run が ended になった」直後に出せる状態を目指す**。そのとき OK が出ている PR だけで `hub:ship` する。レビュー待ちの PR を待って 1 回飛ばすより、今あるもので回す。
- **ベンチの終わり = 人がベンチの todo の「🏁 終わった」を押す**（盤の web に帯はもう無い。ベンチの状態は人は ops パネルで見る）。押すと todo が閉じ、`task bench:done` が裏で走る。**ハブは今まで通り `task runs:wait`（バックグラウンド）で score を拾う**（ベンチの todo に `lane:wait` を掛けてもよいが、score は `runs:wait` の方が確か）。人がタブで「終わった」と言ったら、todo の「🏁 終わった」を押してもらう（`lane:done` では `bench:done` が走らない）。
- **経緯（`task lane:note`）**: kickoff の後に要約（`KIND=app MSG="<何のアプリ>" BODY="スコア = <式>\n減点: <要点>\n猶予: <要点>\n禁止: <要点>\n<構成・ログイン など 1 行>" REF=docs/kickoff.md`。行の頭の語 スコア / 減点 / 猶予 / 禁止 で 📌 の 🧮・⛔ 枠に入り、REF の kickoff で 🧭 枠に固定される）、run ごとの判定（`KIND=bottleneck MSG="ベンチ <n>: <律速 1 行>" BODY="score <x>（<前回比>）\n律速: <Verdict の要点と数字>\n次: <出す [TASK] / PR>" REF=<ts>`）。score の行・人の決定・alert は自動で載るので書かない。手順ごとには書かない。
- **ベンチの todo（盤の内部の帯 `ready`）が出たら人は即回す**。parallel-fix や他の PR を待たせない（「#7 も先に入れるか」と聞かない）。**帯が `ready` / `running` の間は ship しない**、ベンチの todo を閉じて作り直さない。その間に OK になった PR は `task lane:bench NEXT=\"#…\"`（STATE を付けない。付けると帯が戻る） に積み、次の ship で出す。
- ベンチの todo の MSG には「**載っている PR**」と「**次に回す PR**」を必ず入れる（hub:ship が作った todo に無ければ、帯の `PRS` / `NEXT` で補う）。
- ベンチの todo には必ず「**run が ended になってから collect**」と書く（running の間に collect するとデータが消える。2026-10-21）。
- **ts とスコアは人に貼らせない**。人は panel で collect → report → score を入れるだけ（ポータルの点数は panel にしか入らない。練習は bench log を貼れば panel が score を拾う）。ハブは `task runs:wait`（バックグラウンド）で気づき、`task runs` で読む。**ops には誤検知の run も並ぶ**（smoke の initialize、巡回、手の操作。mysql の qps で始まった run は破棄されない）: score が入っていて req が数千ある run だけを使う（`task runs` は req < 1000 で score の無い行に「誤検知?」と付ける）。`runs:wait` が `CHECK:`（score の入った run が 2 本以上）を出したら、どれがベンチかを人に確かめてから使う。誤検知の run は人に Runs の delete を頼む（todo）。
- report の Tuning 節に **「いつ = すぐ（fast lane）」の ●** があれば、run の判定のときに `[TASK] 2 fast <knob> TS=<ts>` でレーン 2 に渡し、次の ship に載せる（15:00 を待たない。RUNBOOK §6d）。
- ベンチの間（約 3〜5 分）は merge しない（docs だけの PR は除く）。その間に PR の表とレビューの todo を作る。
- **DNS の答え・ベンチの向き先・付随 DB の置き場所を変える PR の後は、ベンチの todo を出す前に `task host:diag HOST=1 NAME=<アプリのホスト名>` の dns 節で答えを確かめる**（2026-10-27: 2 回、ハブの deploy の直後に人のベンチが DNS で止まった）。**付随 DB を別ホストに出す PR は、ship の前に `task env:show` の `DEPLOY_FIRST` を PR 本文の「必要な DEPLOY_FIRST」の行と突き合わせる**（付随 DB のホスト → アプリ DB のホストの順。例 `"2 3"`。違えば ship せず `.env` の todo。2026-10-28: `"3 2"` のまま ship して deploy が失敗）。PR 本文にその行が無ければ `[TASK] 2 fix #<n>`（行を足す）。
- **`.env` を変える todo は、閉じたら（`lane:wait` の完了）次へ進む前に `task env:show` で値を確かめる**。違っていれば同じ todo を作り直す（2026-10-28: ハブの sed が拒否され、t28 は ✅ で閉じたが `.env` は変わっていなかった）。ハブは `.env` を自分で書き換えない（盤の INPUT / ENVKEY の todo か、人のコマンド）。
- **ship の直前に、レビュー済みの PR の head が OK の時から変わっていないか**: 盤の merge OK は `hub:ship` が確かめる（OK の後の commit があれば merge せず alert）。タブの OK は、OK のときに `gh pr view <n> --json headRefOid` の sha を控え、ship の直前にもう一度見る。変わっていれば差分を見せて OK を取り直す（2026-10-28: #15 の直しが ship の直前に push され、載らなかった）。

## 時刻の区切り（ハブだけが時計を見る）

| 時刻 | やること |
|---|---|
| 12:30 | `[PHASE] loop technique-scan TS=<最新>` を c へ（2 周目の scan。RUNBOOK §6c） |
| 12:30〜14:00 | 構成の判断（RUNBOOK §6a）を人に。決まれば `[TASK] 2` |
| 15:00 | `[PHASE] loop tuning TS=<最新>` を 2 へ（§6d、15:45 で打ち切り） |
| 16:00 | `lane:phase PHASE=freeze`、`[PHASE] freeze` を 2 / 3 / c へ。未 merge の PR を人と決める（§7） |
| 16:30 | `lane:phase PHASE=final`、`[PHASE] final` を 2 / 3 / c へ。§8 を todo に分けて出す（measure-off、db:snapshot、reboot-test、ベンチ、目視） |
| 17:00 | `[PHASE] final review` を c へ（CLAUDE.md ルール 10）。指摘は人と決める |

## 盤に出すこと

STEP（今の手順）が変わるときに `lane:status LANE=1`（working / waiting / idle は hooks が自動。README §2）。人を待つとき `waiting-human`（STEP に何を）。todo を作ってすぐ待つなら `task lane:todo … WAIT=1` を run_in_background で 1 回。**journal（run の score など）と claims の行は `task hub:log LINE="…" [FILE=claims]` の 1 回**（追記 → そのファイルだけ commit → pull → push。2026-10-28: printf → add → commit → pull → push の 5 回を 5 回やった）。

ロードマップ（`tools/lanes/plan.json`）の自分の step: **H1〜H5**。H1 / H2 は自分で打つ（kickoff phase 1 / 2 の始めに `task lane:step ID=H1 STATE=doing`、commit の後に `STATE=done`。kickoff skill にも書いてある）。**H3 は `actions:plan`、H4 / H5 は `hub:ship` の成功で自動で done**（2026-10-25 は一度も打たず、盤のハブの step が全部 ready のままだった）。ベンチ（B1〜B4）と特別賞（P1）は panel の score から自動（B<n> はベンチの終わり＝「🏁 終わった」/ `runs:wait` の score でも先頭から done）。**メッセージで終わる step（I3 `[DONE] servers`、A1 `[DONE] 3 crawl`、C1 `[DONE] c manual`、C2 `[PICK]`、C3 `[DONE] c technique-scan`）は、そのレーンが送った時点で `plan.json` の `done_on` で自動で done**。I1 / I2 は `host:peek` と `hosts:setup` の 0 NG で自動。**PR で終わる他レーンの step（I4 / I5 / A2〜A4 / C2 / C3 / H6）は `hub:ship` の merge で自動で done**（`plan.json` の `auto` の glob に head ブランチか変更ファイルが当たったもの、NOTE「PR #n」）。**`[DONE] … PR なし`**（ops グループが空など）**を受けたら、その lane が打っていなければ `task lane:step ID=<step> STATE=skip NOTE="PR なし: <理由>"`**。**他レーンの step が実態とずれていたら（PR が出ているのに doing のまま等）ハブが直してよい**。 step を始めたら `task lane:items ID=… ITEMS="a|b|c"`（やることの箇条書き）、1 つ終えるごとに `task lane:item ID=… N=<番号>`。サブエージェントを出したら `lane:status … AGENTS=<数>`。

## 人に頼むこと（todo）

`.env`、ブラウザの hosts、ポータルのベンチと collect / report / note、PR の OK、SG、構成の判断、§8 の目視、`task app:size` を見ること。**コマンドは必ず CMD に**（README §4）。`.env` の todo は閉じた後に `task env:show` で確かめる。`task app:refresh` は人に頼まない（ハブが打つ）。

## 終わり

全 run に score と note、全 merge が PR 経由、§8 の再起動試験が pass、open の alert / todo が 0、最終ベンチの後は何も触っていない。
