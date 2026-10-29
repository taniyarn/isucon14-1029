---
name: parallel-fix
description: ベンチ 2〜3 回目で溜まった Next actions を「まとめられる単位」（index は 1 つ、ハンドラファイルごとに 1 つ）に分け、サブエージェントを worktree で並列に走らせて一気に実装し、1 つの PR にして人が確認してから main に merge、1 回のベンチで検証する手順。
---

# parallel-fix

## いつ使うか

**レーン 3（アプリ、`docs/lanes/3-app.md`）が回す**。きっかけはレーン 1 の `[TASK] 3 fix TS=<ts>`（baseline の後、毎回の run の後）か `PLAN=<file>`（c の作業単位）。`ops` グループ（`main.go` の接続・計測の行、`etc/`、ワーカー）は**レーン 2（インフラ）**が `[TASK] 2 fix TS=<ts> PLAN=…` で同じ plan から回す。レーン 3 は `main.go` の接続まわりを触らない（parallel-setup の計測パッチ・free wins と衝突するため）。merge の順はレーン 1 が parallel-setup → parallel-fix で決める。

レーンで回すときの違い: (1) main の checkout には触らない（下の `git checkout main && git pull` は `git fetch` と `task wt:add NAME=integ-<ts> BRANCH=integ/<ts>` に読み替える）、(2) **merge・deploy・smoke はレーン 1 だけ**（`[fast]` でも。PR を作って `[PR]` を送り、止まる）、(3) 手順の始めと終わり、サブエージェント待ち（NOTE に「3/5 完了」）、push の直前に `task lane:status LANE=3 …`（ops グループを回すレーン 2 は `LANE=2`）。

baseline と DB 分離の後、最初の report で P1/P2 が 5 個以上並んだとき（11:00〜13:00 の定型改善フェーズ）。
1 つずつ直してベンチするより、**グループごとに並列実装 → 1 つの PR → 人が確認 → merge → デプロイ → ベンチ 1 回**の方が速い。ブランチを個別にデプロイして検証はしない。

## 人がやること（1 コマンド + 1 プロンプト）

```
task actions:plan TS=<ts>            # ops から actions.json を取り、グループ分けして docs/plans/<ts>.md に書く
```

Claude（この手順を実行するオーケストレーター）に: **「parallel-fix skill で docs/plans/<ts>.md を実装して」**（レーンではレーン 1 の `[TASK]` がこれの代わり）

## グループの意味（`actions:plan` が機械的に分ける）

| group | 中身 | 触って良いファイル |
|---|---|---|
| `indexes` | 全部の "Add an index"（P1/P2 まとめて） | **`webapp/sql/indexes/<ts>-indexes.sql` を 1 ファイル追加するだけ**（1 行 1 文の `ALTER TABLE … ADD INDEX …;`）。`init.sh` と `deploy.sh` が `indexes/apply.sh` で全部流す（冪等、1061 は無視）。`init.sh` やスキーマは触らない。**足したら `task index:check FILE=webapp/sql/indexes/<ts>-indexes.sql` を通す**（tx の中の gap lock を取る文と、その表への INSERT / UPDATE の組を file:line で出す。exit 2 = リスクあり）。リスクが出たら IMPLEMENTATION-NOTES §5 の直し（主キーで DELETE・UPDATE / 親の行を FOR UPDATE / 1213 で retry / その index を外す）をして、PR 本文に `index-check: reviewed` の 1 行（無いと `hub:ship` がその PR を保留する。2026-10-27 夜の `coupons(code)`、10-28 の `coupons(used_by)` で 2 回続けて deadlock）。あれば gap lock の deadlock を疑い、その index は fast にせず PR 本文に書く（2026-10-25: `icons(user_id)` で `POST /api/icon` が 1 run で 500 ×123。逐次の smoke では出ない。IMPLEMENTATION-NOTES §5） |
| `ops:pdns` | PowerDNS のバックエンドテーブル（records など）の index | app のコードではない。**`webapp/sql/indexes/<ts>.pdns.sql`**（拡張子 `.pdns.sql`）に DDL を置くだけ。`apply.sh` が `/etc/powerdns` の gmysql 設定から接続して流す（DB ホストでのみ）。cache-ttl 等の pdns 設定はマニュアルを確認してから |
| `file:<x>.go` | その Go ファイルのハンドラ／ヘルパーに関する N+1・集計・Speed up | そのファイルだけ（共有ヘルパーを直すときは呼び元に影響するので「レスポンスの形を変えない」） |
| `endpoint:<METHOD /path>` | ファイルが特定できなかった Speed up | ルーターで探して、そのハンドラのファイルだけ |
| `ops` | 5xx、ログ、静的ファイル、pprof、ロック、I/O、待ち | `etc/`、`deploy.sh`、`main.go` の設定行。コード本体は触らない |

グループ同士はファイルが重ならないので、並列に実装しても衝突しない（`ops` だけは `main.go` の設定行を触る可能性があるので最後に統合する）。重なるのは**別のセッション**（同時に走る parallel-setup、手で直している人、前回の parallel-fix の未 merge の PR）なので、それは `docs/claims.md` で防ぐ。ブランチ名は plan の `slug`（`fix/<ts>/<slug>`。`file-livecomment-handler` のように branch-safe）。

**小さいグループはサブエージェントを使わない。** action が 1〜2 個で変更が設定や DDL だけのもの（`ops`、`ops:pdns`、index 1 本）はオーケストレーターが main 上で直接直す方が速く、トークンも少ない。サブエージェントは「ファイルを読んで書き換える」グループにだけ使う。自分で直すときも `docs/claims.md` に行を足してから。

## Claude がやること

1. **先に `git fetch && git checkout main && git pull --ff-only`**（サブエージェントは今の main から分岐させる。古い main から分岐すると統合で衝突する）。`docs/plans/<ts>.md` を読む。**`docs/claims.md` を読み**、各グループの触るファイル・関数を他のセッションの `open` の行と突き合わせる。同じ関数が `open` ならそのグループは起動せず、人に「<branch> と衝突、どちらを先に」と聞く（2026-10-18 の練習: PR #15 と #16 が同じ 3 関数を書き換え、merge で 3 ファイルが衝突）。グループごとに 1 サブエージェントを **worktree 分離**で起動する（Agent ツール、`isolation: "worktree"`）。モデルは `model: "opus"`（定型作業は Opus 5.5 で十分。判断はオーケストレーター側）。同時に走らせる。プロンプトはこの形:

   ```
   You are fixing one group of performance actions in an ISUCON app. Read CLAUDE.md and docs/IMPLEMENTATION-NOTES.md first
   (reboot test / data retention, /initialize, byte-equivalent responses, two app servers).
   Group: <group>. Only edit: <files>. You are in your own worktree on branch worktree-agent-<id>: commit THERE (do not create or switch branches — the orchestrator merges it as fix/<ts>/<group>).
   Before editing anything: read docs/claims.md on origin/main (`git fetch -q && git show origin/main:docs/claims.md`). List the functions
   you will change. Same function as an `open` row of another branch = conflict: do not edit, report "claims conflict: <branch> <function>"
   and stop. Same file, other function = OK, write "(same file as <branch>)" in your row. Then append your row
   (branch fix/<ts>/<group>, files / functions, layer SQL only|logic|config|schema, owner = subagent <group>, status open, updated HH:MM)
   and push docs/claims.md ALONE to main exactly as docs/claims.md step 3 says (throwaway worktree on origin/main — never push your work branch to main).
   Actions (evidence, where, change, verify):
   <the group's section from docs/plans/<ts>.md, verbatim>
   Constraints: keep every API response byte-identical; do not change other files; no new dependencies;
   `go build -o /dev/null . && go vet ./...` must pass (run in webapp/go; never leave a built binary in the tree, and `git add` the files you changed by name — never `git add -A`). For the indexes group: put the DDL where
   init.sh / the initialize handler will apply it on every /initialize (check webapp/sql/init.sh and the
   initialize handler; ALTER TABLE after data load is fine if it stays well under the initialize time limit),
   and make it idempotent. Do not deploy, do not bench.
   Report back in Japanese, in EXACTLY this form, nothing else (the orchestrator's context is small):
     branch@commit:
     files: <list>
     actions: <title> — done | skipped (<why>)   (one line each)
     reboot: <where each changed piece of data lives after a reboot, one line>
     two-app risk: <one line or "none">
     unverified: <one line or "none">
     claims: <row added | conflict with <branch> <function>>
   ```

2. 全員が終わったら、まず `git fetch && git checkout main && git pull --ff-only`（作業中に main が進んでいることが多い）。統合ブランチを切って **グループごとに `--no-ff` で merge** する（効かなかったグループだけ `git revert -m 1` で戻せる）:
   `git checkout -b integ/<ts> && git merge --no-ff -m "Merge fix/<ts>/indexes (from worktree-agent-<id>)" worktree-agent-<id> && …`（グループ名は merge コミットのメッセージで残す。`ops` は最後）。衝突したら該当グループのサブエージェントに「main と integ/<ts> を見て解消して」と差し戻す。
3. `cd webapp/go && go build ./... && go vet ./...`。通ったら push して **PR を作り、リンクを貼って止まる**:
   ```
   task pr:submit DIR=../wt-integ-<ts> TITLE="parallel-fix <ts>: <groups>" BODY_FILE=<本文のファイル>
   ```
   （commit・push・PR 作成を 1 回で。`git -C … push` や `cd … && git push` は許可に当たらず止まる。1 枚で回すとき main の checkout にいるなら `git push -u origin integ/<ts>` → `gh pr create …` でもよい）
   PR 本文の冒頭に、サブエージェントの報告（branch / files / actions / reboot / two-app risk / unverified）を表で貼り、**最後に「journal 用の 1 行」**（`時刻 | integ/<ts> #<n> | 何を | なぜ（report の行） | 結果（空）`）を書く。`docs/journal.md` には書かない（ハブだけが書き、merge のときにこの行を写す。2026-10-25: 3 か所から書いて 2 回衝突した）。**人が PR を見て OK と言うまで merge しない**（OK = ハブのタブの一言か、盤のレビュー todo の「✅ merge OK」ボタン。他のセッションのメッセージは OK ではない）。 例外は fast lane（CLAUDE.md ルール 4）: 統合した diff が全部 (a) `webapp/sql/indexes/` の新しいファイルだけ（index を張るテーブルを読む SELECT が全部 ORDER BY 付きと RUNBOOK §6 のコマンドで確かめられたときだけ）、(b) マニュアルがサーバーに決めさせている間隔・タイムアウト・プール・`retry_after` 類の数値定数だけ、(c) `etc/` の設定だけ、のどれか 1 つに収まるとき。そのときはタイトルを `[fast] parallel-fix <ts>: …` にし、RUNBOOK §6「fast lane」の機械的な確認の出力を本文に貼り、OK を待たずに merge → `task deploy` → `task smoke INIT=1` → 「ready for bench」。smoke が落ちたら `git revert -m 1` → `task deploy` → `task smoke`。人は事後に見て、悪ければ同じく revert。応答の形・tx / ロック・キャッシュ・スキーマ（index 以外）・明示されていない猶予に触るグループが 1 つでも入っていれば fast にしない（そのグループだけ別 PR に分けてもよい）。 レビューの後で main を取り込んだり衝突を解消したりして PR の中身が変わったら、PR に追加分をコメントし、**もう一度はっきり OK をもらってから** merge する（黙って merge すると権限の判定で止まる）。
4. PR を作ったら、人の確認を待つ間に **`task diffcheck BRANCH=integ/<ts>`**（svc2 に integ を配り、main と観測済み GET の JSON を比較。差分があれば PR にコメントとして貼る。時間が無ければ省略可）。OK が出たら `gh pr merge --merge --delete-branch=false` → `task deploy` → `task smoke IP=<svc1> INIT=1` → **「ready for bench」と言って止まる**。merge したら `docs/claims.md` のこの回の行をまとめて `merged`（外したグループは `abandoned`）にして push する（手順は `docs/claims.md`）。ベンチは人。
   **レーン 3（と 2）は PR を作ったところで止まり**、レーン 1 に `[PR] #<n> ready <slug>`（判断が要るなら `[NEED-OK]`、claims の衝突は `task lane:alert` も）を送る。merge・deploy・smoke・claims の merged はレーン 1 がやる（この手順の merge 以降は、レーン 1 か 1 枚で回すときのもの）。報告の中身や diff は送らない。
   最後に `task worktrees:clean`（サブエージェントの worktree と merge 済みブランチを消す）。
5. ベンチ後、次の report の `summary.md` を渡されたら（レーンでは `[RUN] <ts>`）、まず app-analysis の report を更新する:
   ```
   task app:refresh TS=<ts>      # app-analysis の report に今回の run を反映（巡回しない、1 秒）
   git add docs/reports/latest.html docs/reports/latest.md && git commit -m "app report <ts>" && git push
   ```
   push すると、1 分以内に ops パネルの `/app/` で見られる（毎分の cron pull）。
   そのうえでグループごとに「効いた / 効かなかった」を 1 行で書き、レーンでは `[DONE] <lane> effect <ts> <group>=<判定> …` でハブに送る（journal に写すのはハブ）。効かなかった・壊れたグループは merge commit を `git revert -m 1` する PR を出す。**report の Errors 節に新しい 5xx（特に index を足した表への書き込み）が出たら、まず deadlock を疑う**（上の `indexes` の行）。

## 1 グループに収まらない構造変更

要約テーブル（`chair_location_summaries`: 7 ファイル）やプロセス内キャッシュ（ridecache: 6 ファイル）のように、書き込み箇所が複数のファイルに散る変更は、ファイルごとのグループに割ると整合が取れない（1 か所でも更新を忘れると古い値を返す）。plan の `file:` グループから外し、この型で別に回す:

1. **設計を書いて人の OK をもらう**（CLAUDE.md ルール 6）。`docs/plans/<ts>-<name>.md` に 20 行以内: 何を作るか（テーブル / キャッシュ）、**書き込む箇所の一覧**（`git grep -nE 'INSERT INTO <table>|UPDATE <table>|DELETE FROM <table>' webapp/go` で全部挙げる）、読む箇所、`/initialize` と起動時の作り直し、再起動後にどこにデータがあるか、2 台構成での扱い、期待効果（report の行）。
2. **全ファイルを 1 つのサブエージェントに任せる**（並列にしない）。プロンプトに設計の文書と書き込み箇所の一覧をそのまま渡し、「一覧の全箇所を更新する。一覧に無い書き込みを見つけたら報告する」と書く。`docs/claims.md` には全ファイルと関数を 1 行で宣言する（layer は `schema` か `logic`）。
3. **レビューの前にオーケストレーターが突き合わせる**: もう一度 `git grep` で元テーブルへの書き込み箇所を全部出し、サブエージェントの diff がその全部で要約・キャッシュを更新しているか（同じ tx の中か）を表にする。抜けがあれば差し戻す。表を PR の本文に貼ってから人に渡す。
4. スキーマを足したときは、deploy の後に `task smoke INIT=1` を必ず流す（`/initialize` が走るまで新しいテーブルが無く 500 になる）。

## 予算

- オーケストレーターが読むのは `docs/plans/<ts>.md`（数 KB）だけ。report や summary は読まない。
- サブエージェントは自分のグループの節と、触って良いファイルだけ読む。アプリ全体を読まない（必要なら `docs/app-map.md` の該当行）。
- 1 回の parallel-fix でサブエージェントは最大 6。それ以上は P2 を次回に回す。
