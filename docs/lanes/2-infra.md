# レーン 2 — インフラ（このセッションへの指示）

あなたはサーバーと構成を持つセッション。サーバーの準備（人への todo、`task deploy:init`）、parallel-setup（DB 分離、計測パッチ、free wins）、`etc/` と `deploy.sh`、fast lane の設定 PR、構成変更（app ×2、付随サービスの移動）、tuning、score-sprint の PR をやる。**merge・deploy・ベンチはしない**（PR を作って `[PR]` をハブに送り、「手が空いたら」へ）。ハンドラのロジックはレーン 3。共通ルールは [README.md](README.md)。

## 持つもの

- 自分の worktree とブランチ（`setup/*`、`integ/setup`、`infra/*`、`tune/*`）、そのサブエージェント。
- `etc/`、`deploy.sh`、`env*.sh`、`webapp/sql/indexes/apply.sh` と `init.sh` の接続まわり、`main.go` の接続・計測の行（claims で宣言）。
- main の checkout では `git checkout` / `pull` をしない。`task wt:add NAME=<name>`（`git worktree add ../wt-<name> -b <name> origin/main` + main の `.env` への symlink。worktree の中でも `task` が動く）だけ。

## トリガー → やること → 出すもの

| トリガー | やること | 出すもの |
|---|---|---|
| 起動（人の 1 行） | `[HELLO] 2 <自分の名前>` をハブへ。`lane:status LANE=2 STATE=waiting-human STEP="HOSTS 待ち"`。盤に最初からある入力の todo ②「3 台の IP」（新しい盤なら `t2`）が open ならそれを使う（無ければ `task lane:todo LANE=2 MSG="3 台の IP を入力して送信" INPUT="svc1 IP|svc2 IP|svc3 IP" ENVKEY=HOSTS …`）→ 続けて `task lane:wait ID=<その id>` を run_in_background（人が盤で送信すると起きる。盤が `.env` の HOSTS を書く） | todo |
| HOSTS の todo の完了（`lane:wait` の `env.HOSTS=…`）/ 人の `done` | `task env:show` で HOSTS の並びを確かめる → `task host:peek`（と `HOST=3`）。DB・nginx・docker・ユニット・言語ディレクトリを README §3 の前提と比べる → 外れは alert。todo（CMD=`! task hosts:setup`、レーン 2 のタブで） | alert（あれば）、todo |
| hosts:setup の出力 | `NG` の行だけ読む（RUNBOOK §2 の表）。直せないものは alert。`task hosts:status` で 3 台 idle を確かめる。**出力の最後に `baseline: band ready, todo t<n>` が出たら、まず `[DONE] 2 baseline-ready t<n> main <sha>` をハブへ**（盤が帯と baseline の todo を作り済み。人はすぐベンチを回す。deploy:init は手元で `deploy.sh` を書くだけでホストに触らないので、baseline と並行してよい）。続けて **`task deploy:init` を回して `deploy.sh` を作り、unit と突き合わせて `CONFIGURED=1`**（`bash -n`。kickoff の例外 a で main に直接 commit）。**練習のベンチの準備（ベンチ機、SG、DNS の答え）はレーンの手順に入れない**: 人がレーンを始める前に済ませ、盤の t3 に「セットアップ済み、実行するマシンと `--webapp` の IP」を書く（RUNBOOK 付録）。レーン 2 はベンチのために `env.sh` や SG を変えない | **`webapp/` が origin/main に来たら（hosts:setup の bootstrap の push）すぐ、parallel-setup の B（計測パッチ）と D（free wins）のサブエージェントを出す**（サーバーの情報が要らない。DB 分離 A だけ `deploy:init` の後）。自分は続けて下の手順を進める（2026-10-28: baseline の後の約 3 分は setup の PR 待ちだった）。 `[DONE] servers` をハブへ |
| `[TASK] 2 config-options TS=<ts>`（baseline の後、ハブが出す） | **構成の判断を 1 回にまとめる**（2026-10-27: DB 分離 → 付随 DB の移動 → app ×2 を 3 回に分けて決め、それぞれ 1 周ずつかかった）。summary.md の Fleet と CPU by process（ホストごと、プロセスごと）から、最大 3 案を比べる: どのプロセスをどのホストへ（DB、付随サービスとその DB、app の台数と振り分け）、各案で空く CPU（数字は report から）、ベンチのコマンドやポータルの選択が変わるか、再起動試験の注意、PR の行数。推奨を 1 つ。`docs/plans/config-options.md`（**案ごとに 3 行**: 何がどう変わるか / 空く CPU（report の数字）/ ベンチ・ポータル・再起動の注意と PR の行数。比べる表は付録）を docs の PR（`[fast]`）で | `[DONE] 2 config-options #<n>`（ハブが `CHOICES` の todo にする）→ 選ばれた案を `[TASK] 2 infra <案>` で |
| `[TASK] 2 setup` | parallel-setup skill の A（DB 分離）・B（計測パッチ）・D（free wins）と **E（計測 OFF / ON の `[sprint]` PR 2 本）**。**baseline を待たずに始める**（merge はハブが baseline の後に。E は特別賞の直前まで open のまま）。サブエージェントは claims に宣言 | `integ/setup` の PR、`[PR] #a ready setup`、`[PR] #m #n ready [sprint] measure-off / measure-on`（ハブは表に置くだけ） |
| `[TASK] 2 fix TS=<ts> PLAN=<file>` | plan の ops グループ（接続数、設定、ワーカー）だけを parallel-fix の形で | PR、`[PR]` |
| `[TASK] 2 fast <knob> TS=<ts>` | report の Tuning 節の「すぐ（fast lane）」の ● を `etc/` だけの 1 行の PR に（タイトル `[fast]`、本文に兆候と推奨の行）。build 不要、RUNBOOK §6 の fast lane の確認 | `[PR] #<n> ready [fast] tune-<knob>` |
| `[TASK] 2 infra <テーマ>` | 構成変更（app ×2、付随サービスを別ホストへ、RUNBOOK §6a / §6b）。50 行を超えるなら先に設計を 20 行で人の OK。**付随 DB を別ホストに出すとき**: ① `.env` の `DEPLOY_FIRST` に **付随 DB のホスト → アプリ DB のホストの順**で並べる todo（例: 付随 DB が svc2、アプリ DB が svc3 なら `DEPLOY_FIRST="2 3"`、空白区切り、書いた順）（`task deploy` がそれらを先に、書いた順に配る。各ホストの `deploy.sh` は相手の DB の 3306 を確かめてから進むので、確かめられる側を先に。2026-10-27: svc3 → svc1 の順で、svc1 の pdns が svc2 の MySQL に繋げず起動失敗。2026-10-28: `"3 2"` で svc3 が先になり、svc2 の isudns にまだ届かず deploy が 2 回失敗）。**PR 本文に「必要な DEPLOY_FIRST: "2 3"（理由: …）」の行を必ず書く**（ハブが ship の前に `task env:show` で突き合わせる）② 付随サービスに `Restart=always` + `StartLimitIntervalSec=0` の drop-in（再起動試験で相手の DB が遅れても戻る）③ PR 本文に「merge 後に `host:diag NAME=` で DNS の答えを確かめる」④ 起動時に書き換わる `env.sh` の値（DNS の答えなど）は `env.db.<host>.sh` にも書き、付随の init スクリプトにも読ませる | PR、`[PR]` |
| `[TASK] 2 rebase #<n>` | そのブランチに origin/main を merge、衝突を解く、build / vet、push | `[PR] #<n> ready <slug> changed` |
| `[RUN] <ts>`（自分の PR が入った run） | summary.md で効いたか判定（Fleet の CPU の分かれ方、Tuning 節）。journal には書かない（ハブだけが書く）。下がったら revert の PR（本文に journal 用の 1 行） | `[DONE] 2 effect <ts> <slug>=<判定> …`（ハブが journal に写す）、`[PR]`（あれば） |
| `[MERGED] #<n>` | `task worktrees:clean`（自分の分） | 「手が空いたら」へ |
| `[PHASE] loop tuning TS=<ts>` | tuning skill: Tuning 節の ● を 1 つずつ、1 ノブ 1 PR（`etc/` だけなら `[fast]`） | PR、`[PR]` |
| `[PHASE] freeze` / `[PHASE] final` | 新しい PR を作らない。open の自分のブランチは claims を abandoned。final ではハブの §8 の todo の下準備（measure-off の確認、reboot-test の読み） | `STATE=idle` |
| `[STOP] <理由>` | 今の手順を止める（サブエージェントも） | `STATE=waiting NOTE="STOP: <理由>"` |
| claims の衝突 | 書かない | alert（warn）+ `[NEED-OK] …` |
| `[TASK] 2 fix #<n>`（レビューで ✋ 要修正） | PR のコメントを読んで同じブランチで直す、push | `[PR] #<n> ready <slug> changed` |

## 手が空いたら（ハブの `[TASK]` を待たずにやってよい。上から）

**効果判定の `[RUN]` を待って止まらない**（2026-10-27 夜: レーン 2 は 44% を他レーン待ち、`[RUN]` 待ちで 8 分止まった）。PR を出したら、次の run を待たずに下の 1〜3 へ進み、`[RUN]` が来たら割り込んで判定する。

1. 自分の PR が merge 待ちなら、次の構成変更の下準備: app ×2 の準備（nginx の upstream、2 台目の `env.db.<host>.sh`、ワーカーを 1 台に寄せる行）を **PR にせず** `docs/plans/infra-app2.md` に 20 行で。
2. 最新の report の Tuning 節と Fleet を読み、`etc/` だけで済むノブを `[fast]` 候補として `docs/plans/infra-tuning.md` に（15:00 の tuning で使う）。
3. 再起動試験の予行の読み（`task host:diag` で unit の enable、drop-in、`env.db.sh` の置き場所）。気になる点は alert（warn）。
4. 何も無ければ `lane:status STATE=idle STEP="手が空いた（1〜3 済）"`。ハブが次を出す。

## 盤に出すこと

STEP（今の手順）が変わるときに `lane:status LANE=2`（working / waiting / idle は hooks が自動。README §2）。サブエージェントを待つ間は `working`（NOTE に「サブエージェント 2/3 完了」）。PR は `task pr:submit DIR=../wt-<name> TITLE=… BODY=… MSG=…` の 1 回（README §8）。PR を出したら「手が空いたら」に進み、STEP にそれを書く。

経緯（`task lane:note`。手順ごとには書かない）: 構成・設定を変える PR を出したら（DB 分離、app ×2、tuning のノブ）1 行 `KIND=fix MSG="<何を → どう変えた（理由の数字 1 つ）>" REF="PR #n"`。config-options を出したら結論に BODY（`KIND=decision MSG="構成の案: 推奨 <案>" BODY="<案 1: 何が → どこへ / 空く CPU>\n<案 2 …>\n<案 3 …>" REF=docs/plans/config-options.md`）。

ロードマップ（`tools/lanes/plan.json`）の自分の step: **I1〜I5**。`lane:status` と一緒に `task lane:step ID=I4 STATE=doing` → PR を出したら `STATE=done NOTE="PR #5"`（やらないと決めたら `skip`）。I4 / I5 は merge で自動で done。**PR にならずに終わったら（plan の ops グループが空など）`task lane:step ID=I5 STATE=skip NOTE="PR なし: <理由>"` を打ってから `[DONE] 2 … PR なし` を送る**。 step を始めたら `task lane:items ID=… ITEMS="a|b|c"`（やることの箇条書き）、1 つ終えるごとに `task lane:item ID=… N=<番号>`。サブエージェントを出したら `lane:status … AGENTS=<数>`。

## 人に頼むこと（todo）

`.env` の HOSTS、`! task hosts:setup`、サーバー側でしか直せない NG（RUNBOOK §2 の手動コマンド）。**コマンドは CMD に**。作ってよいのは操作の todo だけ。PR の OK や判断の質問はハブが todo にする（こちらは `[NEED-OK]` を送るだけ）。

## 終わり

`[TASK]` ごとに PR が出ている（または理由が返答にある）、自分の claims の行が merged / abandoned、worktree が残っていない、`[DONE] 2 effect` を送ってある（PR 本文に journal 用の 1 行がある）。
