# レーン 3 — アプリ（このセッションへの指示）

あなたはアプリのコードを持つセッション。app-map（app-analysis）、parallel-fix（report の index / N+1 / ハンドラ）、設計済みの構造変更の実装、自分の変更の revert をやる。**merge・deploy・ベンチはしない**（PR を作って `[PR]` をハブに送り、「手が空いたら」へ）。`etc/`・`deploy.sh`・接続設定はレーン 2。共通ルールは [README.md](README.md)。

## 持つもの

- 自分の worktree とブランチ（`setup/app-map`、`fix/<ts>/*`、`integ/<ts>`、`impl/*`）、そのサブエージェント。
- ハンドラのファイル（claims で宣言した関数）、`webapp/sql/indexes/*.sql`（追加だけ）、`docs/app-map.md`。
- main の checkout では `git checkout` / `pull` をしない。`git fetch` と worktree だけ。

## トリガー → やること → 出すもの

| トリガー | やること | 出すもの |
|---|---|---|
| 起動（人の 1 行） | `[HELLO] 3 <自分の名前>` をハブへ。`docs/manual/` と `docs/kickoff.md` があれば読む。**`docs/app-size.md`（ハブが `[DONE] servers` で作る。無ければ自分で `task app:size`）を最初に読む**: 行数・ルート・テーブルの規模から、app-map と early-fix をどのファイル / テーブルから見るか決める | `lane:status LANE=3 STATE=waiting STEP="webapp 待ち"` |
| `[TASK] 3 app-map` | app-analysis skill。**巡回（svc2）はすぐバックグラウンドのサブエージェントで回し、early-fix と並行**（どちらかを先に、にしない。2026-10-28: early-fix を先にして巡回が遅れ、URL → app-map 完成が 23 分）。analyzer config はハブが `[DONE] servers` の直後に `task analyzer:init` で main に入れる。**自分では新規作成しない**: まだなら code-only で書き始め、config が main に来たら巡回。直すなら別 commit にしてハブに一言。**巡回は DB 共有の setup PR が merge される前に終える**（svc2 の DB がまだ独立している間。共有後の svc2 で initialize するとベンチの DB を消す）。共有の後に回すのは、ベンチが回っていないとき（`task lane:board` の 1 行目 `bench: none`）だけ `task app:crawl`（#29 で入る。検出を止めて巡回）。並行して、コードから「見れば分かる N+1・index の無い WHERE・ループの中の SELECT」を `docs/plans/app-early.md` に表で（report が来たら突き合わせる） | 巡回が終わったらすぐ `[DONE] 3 crawl docs/reports/latest.md`（ハブはこれで DB 共有の PR を ship できる）、`setup/app-map` の PR（docs だけなので fast lane。タイトル `[fast]`）、`[PR] #b ready [fast] app-map` |
| `[TASK] 3 early-fix`（ハブが `[DONE] servers` で出すが、**`webapp/` が origin/main に来た時点で `[TASK]` を待たずに始めてよい**。2026-10-28: baseline の後の約 3 分は early-fix の PR 待ち。巡回のサブエージェントと並行） | **baseline のベンチが終わるまでに PR を出す**（ハブがベンチ 2 に載せる。間に合わない分は次へ）。**baseline の report を待たずに**、コードとスキーマだけで確実に効く 2 種類を PR まで作る（2026-10-27: 一番効いた #8（5.8 倍）は baseline の report を待ってから作り、3 回目のベンチまでかかった）。① **index**: `webapp/sql/` の CREATE TABLE と、ハンドラの `WHERE` / `ORDER BY` の列を突き合わせ、無い index を `webapp/sql/indexes/early-indexes.sql` に（読む SELECT が全部 ORDER BY 付きなら fast lane。**`task index:check BRANCH=<branch>` を通し、リスクが出たら直して PR 本文に `index-check: reviewed`**（無いと hub:ship が保留。IMPLEMENTATION-NOTES §5）: 同じ tx の DELETE→INSERT、`SELECT … FOR UPDATE` の後の INSERT、一意の列への INSERT どうし。2026-10-27 夜: `coupons(code)` の index で `POST /api/app/users` が deadlock した）② **見れば分かる N+1**: ループの中の 1 件ずつの SELECT（`fill*Response` の中身など）を `IN` / JOIN に。応答の形・順序は変えない。parallel-fix の形（index 1 グループ + Go ファイルごと）で `integ/early` に。baseline の report が来たら、report の行と突き合わせて PR 本文に「report の行」を足す（CLAUDE.md ルール 1 の例外: baseline の前の手はコードの `file:line` を根拠にする） | `integ/early` の PR（index だけなら `[fast]`）、`[PR] #e ready integ-early` |
| `[TASK] 3 fix TS=<ts>` | parallel-fix skill（`docs/plans/<ts>.md`。**ops グループと `main.go` の接続まわりは外す**＝レーン 2）。`docs/plans/app-early.md` の行で report と合うものを同じ PR に | `integ/<ts>` の PR、`[PR] #c ready integ-<ts>` |
| `[TASK] 3 impl <設計ファイル>` | parallel-fix skill の「1 グループに収まらない構造変更」の型（1 サブエージェント、書き込み箇所の突き合わせ表） | PR、`[PR]` |
| `[TASK] 3 rebase #<n>` | そのブランチに origin/main を merge、衝突を解く、build / vet、push | `[PR] #<n> ready <slug> changed` |
| `[RUN] <ts>`（自分の PR が入った run） | summary.md でグループごとに効いたか判定（journal には書かない。ハブだけが書く）。下がった / 壊したグループは `git revert -m 1` の PR（本文に journal 用の 1 行） | `[DONE] 3 effect <ts> <group>=<判定> …`（ハブが journal に写す）、`[PR] #<n> ready revert-<slug>`（あれば） |
| `[MERGED] #<n>` | `task worktrees:clean`（自分の分） | 「手が空いたら」へ |
| `[PHASE] freeze` / `[PHASE] final` | 新しい PR を作らない。open の自分のブランチは claims を abandoned | `STATE=idle` |
| `[STOP] <理由>` | 止める（サブエージェントも） | `STATE=waiting NOTE="STOP: <理由>"` |
| claims の衝突 | 書かない | alert（warn）+ `[NEED-OK] …` |
| `[TASK] 3 fix #<n>`（レビューで ✋ 要修正） | PR のコメントを読んで同じブランチで直す、build / vet、push | `[PR] #<n> ready <slug> changed` |

**文書は書いたらすぐ push**: `docs/plans/app-early.md` のような下読みのメモや設計は、app-map の PR を待たずに docs だけの PR（`[fast]`）で出す（README §0 の 5）。**同じファイルを触る変更が続くときは 1 本の integ ブランチに**（README §7。別々の PR は import 行で衝突して rebase と OK の取り直しになる）。

## 手が空いたら（ハブの `[TASK]` を待たずにやってよい。上から）

1. 最新の `docs/plans/<ts>.md` に残っている P2 / P3 のうち、自分の PR と関数が重ならないものを次の `fix/<ts>/*` に（claims を宣言してから。PR にして `[PR]`）。
2. 最新の report の endpoint 表の上位 3 本について、ハンドラを読んで「次に効きそうな手」を `docs/plans/app-next.md` に 5 行（期待効果、触る関数、猶予に触るか）。ハブが次の `[TASK]` に使う。
3. 自分が入れた変更の「応答が元と同じか」の確認（並び順、`null` と `[]`、エラーの順番。IMPLEMENTATION-NOTES §2）を、`task smoke` の出力と app-map の flow で。違えば revert か修正の PR。
4. 何も無ければ `lane:status STATE=idle STEP="手が空いた（1〜3 済）"`。

## 盤に出すこと

STEP（今の手順）が変わるときに `lane:status LANE=3`（working / waiting / idle は hooks が自動。README §2）。サブエージェントを待つ間は `working`（NOTE に「サブエージェント 3/4 完了」）。PR は `task pr:submit DIR=../wt-<name> TITLE=… BODY=… MSG=…` の 1 回（README §8）。

経緯（`task lane:note`。手順ごとには書かない）: app-map ができたら要約に BODY（`KIND=app MSG="<アプリの規模 1 行>" BODY="<主な画面と重い API>\n<大きいテーブル>\n<exec・外部コマンドなど気になる所>" REF=docs/app-map.md`）、修正の PR を出したら律速 1 行（`KIND=fix MSG="<何が遅かった → 何をした>" REF="PR #n"`、BODY なし）。

ロードマップ（`tools/lanes/plan.json`）の自分の step: **A1〜A4**。`lane:status` と一緒に `task lane:step ID=A3 STATE=doing` → PR を出したら `STATE=done NOTE="PR #6"`（やらないと決めたら `skip`）。A2〜A4 は merge で自動で done（`setup/app-map`、`integ/<ts>`、`impl/*` / `integ/<名前>`）。A1 は `[DONE] 3 crawl` か app-map / integ-early の `[PR]` を送った時点で自動で done（`plan.json` の `done_on`）。 step を始めたら `task lane:items ID=… ITEMS="a|b|c"`（やることの箇条書き）、1 つ終えるごとに `task lane:item ID=… N=<番号>`。サブエージェントを出したら `lane:status … AGENTS=<数>`。

## 人に頼むこと（todo）

ほぼ無い。app-analysis の巡回でログインの資格情報が本当に無いときだけ（CMD 付き）。PR の OK や判断の質問はハブが todo にする（こちらは `[NEED-OK]` を送るだけ）。**journal には書かない**: PR 本文に「journal 用の 1 行」（`時刻 | branch / PR | 何を | なぜ（report の行） | 結果`）を書けば、ハブが merge のときに写す。

## 終わり

`[TASK]` ごとに PR が出ている（または理由が返答にある）、自分の claims の行が merged / abandoned、worktree が残っていない、グループごとの効果を `[DONE] 3 effect` で送ってある。
