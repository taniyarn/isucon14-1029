# クラウドの自動レビュー（スケジュールタスクの GitHub トリガー）

**決めたこと**: PR が開かれたら、Anthropic のクラウドで Claude が PR をレビューしてコメントを 1 件書く。手元の PC もハブも PR を見張らない（GitHub → webhook → クラウドのセッション）。fast lane と同じ種類の PR はトリガーの filter とプロンプトで飛ばす。**2026-10 の時点では試験運用**: 次の練習でレーン c のレビューと並べて、速さと指摘を比べる。
**課金**: サブスクリプションの使用量から対話のセッションと同じく引かれる（上限を超えたら、usage credits を有効にしていれば従量）。GitHub イベントには 1 時間あたりの上限があり、超えた分は捨てられる（数字は公開されていない）。research preview。出典: https://code.claude.com/docs/en/routines
**大会ごとの更新は不要**: プロンプトは共通のまま。当日の知識はプロンプトが実行のたびに `origin/main` の `docs/kickoff.md`（kickoff が書く「レビューで見ること」の節）と `docs/app-map.md` から読む。claude.ai のタスクを当日書き換えない。
**役割**: レビューの指摘は「材料」。merge の OK は今までどおり人が盤で出す。クラウドは push も approve もしない。

## 1. 一度だけの準備（大会の repo を作った後）

1. repo に Claude の GitHub App を入れる（https://github.com/apps/claude 、対象はその repo だけ）。claude.ai で GitHub の連携が済んでいること。
2. https://claude.ai/code/routines → 新しいタスク（CLI なら `/schedule`）。
   - **Repositories**: 大会の repo 1 つだけ（PR のイベントで起動すると、その PR の head から始まる）。
   - **Connectors**: 全部外す（要らない）。
   - **Trigger**: GitHub event → `pull_request.opened`。filter（全部が一致したときだけ起動）:

     | Filter | 演算子 | 値 | 理由 |
     |---|---|---|---|
     | Base branch | equals | `main` | |
     | Is draft | equals | `false` | |
     | Title | matches regex | `^(?!\[fast\]).*` | fast lane の PR（タイトルが `[fast]` で始まる）を飛ばす。否定先読みが通らなければ、この行を外してプロンプト側の skip に任せる |
     | Head branch | is not one of | `claims/*` などの記録用ブランチがあれば | |

   - **Prompt**: §3 をそのまま貼る。
3. 試し方: 小さな PR を 1 本開いて、数分以内に `cloud-review:` で始まるコメントが付くこと。付かなければタスクの History で Failed / 起動していないかを見る。

**やり直し**（push で直した後にもう一度見てほしいとき）: トリガーに `pull_request.labeled` を足し、filter に Labels contains `re-review` を付ける。人かハブが PR に `re-review` のラベルを付けると再レビューが走る（毎 push で走らせない。数が増えて上限に当たるため）。

## 2. 何を飛ばすか（lane review と同じ線引き）

| 飛ばす（コメントは `cloud-review: skip <理由>` の 1 行だけ） | 見る |
|---|---|
| タイトルが `[fast]` で始まる（filter で起動もしない。`task pr:submit` が docs だけ・index の新規ファイルだけの PR に自動で付ける） | 上の 3 つ以外の code の変更すべて |
| 変更が `docs/` だけ | 特に CLAUDE.md の「Never fast」: 応答の形、トランザクション・ロック、キャッシュ、スキーマ（index 以外）、マニュアルが明示していない整合性の猶予、2 台構成、`/initialize` |
| 変更が `webapp/sql/indexes/` の新規ファイルだけ（`task index:check` が見る） | |

## 3. プロンプト（そのまま貼る）

```
あなたは ISUCON の大会用 repo の PR レビュアー。この PR を読み取りだけでレビューし、PR にコメントを 1 件だけ書いて終わる。push・commit・approve・request changes・ラベルの変更はしない。コードのビルドや実行もしない。日本語で書く。

1. まず変更ファイルを見る（gh pr diff --name-only）。次のどれかなら、コメント「cloud-review: skip <理由>」を 1 行だけ書いて終わる:
   - タイトルが [fast] で始まる
   - 変更が docs/ の下だけ
   - 変更が webapp/sql/indexes/ の新規ファイルだけ
2. 読む: まず git fetch origin main。大会固有の知識は PR のブランチではなく最新の main から読む: git show origin/main:docs/kickoff.md（特に「レビューで見ること」の節: 整合性の猶予の秒数、触ってはいけないもの、スコアと失格の条件）と git show origin/main:docs/app-map.md（構成: app の台数、DB の置き場所）。無ければ飛ばす。続けて docs/IMPLEMENTATION-NOTES.md（§0〜§8）、CLAUDE.md の rule 4（Never fast）、PR の本文と diff、diff が触る関数の呼び出し元。kickoff.md の「レビューで見ること」と IMPLEMENTATION-NOTES が食い違ったら kickoff.md（当日のマニュアル由来）を優先する。
3. 探すもの（重い順）:
   - 再起動試験で失格になる芽: メモリだけにあって再起動で消えるデータ、起動時に DB が無いと落ちる処理、/initialize で戻らないキャッシュ、/tmp への書き込み
   - 2 台構成で壊れるもの: プロセス内のキャッシュ・カウンタ・mutex に頼る書き込み、svc1 にしかない副作用（ファイル、外部コマンド）
   - 応答が変わるもの: ステータスコード、JSON のフィールド・null と [] の違い・型、並び順と同順位の扱い（ORDER BY の第 2 キー）、ページング、検証の順番
   - 整合性の猶予: マニュアルが許していない古い値の返却
   - SQL: N+1 の直し方で順序が変わる、新しい index と「同じ tx の DELETE → INSERT」でのデッドロック、FOR UPDATE の範囲、tx の中の外部 HTTP
   - Go: rows の Close / Err 漏れ、goroutine の無制限な起動、omitempty の追加、お金の float
   見つからない項目は書かない。推測は「推測」と書く。好みやスタイルの指摘はしない。
4. コメントの形（この形だけ。前置きなし）:
   1 行目: 「cloud-review: ok」か「cloud-review: <n> 件（✋ <a> / ⚠ <b>）」
   続けて 1 件 1 行: 「✋ 要修正 | ⚠ 確認 — <file>:<line> — <何が起きるか（具体的な入力と結果）> — <IMPLEMENTATION-NOTES の節>」
   ✋ は失格・FAIL・誤った応答になるもの、⚠ はスコアを落とすか人の判断が要るもの。最大 8 件。
```

## 4. 当日の使い方

- **ハブ**: レビューの todo を作るとき、PR のコメントに `cloud-review:` があればその 1 行目を todo に載せる（`gh pr view <n> --comments`）。✋ があれば ship に載せず、持ち主のレーンに `[TASK] <lane> fix #<n>`（人の PR なら人に Discord で）。⚠ は todo に書いて人の判断に任せる。まだ付いていなければ待たない（レビューは材料。ship を止める理由にしない）。
- **人**: 盤のレビューの todo で、cloud-review の 1 行目を見て OK / 外す。
- **レーン c**: 試験運用の間は、今までどおり `[TASK] c review #n`（キャッシュ・2 台・`/initialize`・tx の PR）もやる。練習の後、同じ PR で両方の指摘と「PR が開かれてからコメントまでの時間」を比べ、クラウドが同等以上なら本番ではレーン c のレビューをやめる。

## 5. 練習で記録すること

| 項目 | どこから |
|---|---|
| PR が開かれてから `cloud-review:` のコメントまでの時間（中央値・最大） | `gh pr view <n> --json createdAt,comments` |
| skip された数 / レビューされた数 | コメントの 1 行目 |
| ✋ の数と、そのうち本当に直したもの | コメントと、その後の commit |
| レーン c のレビューとの重なり（同じ指摘 / 片方だけ） | 両方のコメント |
| 起動しなかった PR（上限で捨てられた、filter の誤り） | タスクの History |
