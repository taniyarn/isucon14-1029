---
name: deep-dive
description: 後半で手詰まりになった時に、1 回のベンチの生データ（アクセスログ、スローログ、1 秒系列、アプリログ、DB スナップショット）を DuckDB で自由に問い合わせて律速を特定し、根拠付きの改善案を出す手順。
---

# deep-dive

## いつ使うか

report の Next actions を打ち切ってもスコアが伸びない、Verdict が「wait-bound（CPU が余っている）」になった、特定エンドポイントだけ遅い理由が分からない、ベンチの fail 理由が読めない、といった時。

## 準備（人がやる。1 コマンド）

```
task deepdive TS=<run の ts>        # ops から run を手元に取得し、export/*.csv を生成する
```

`measurements/<ts>/` に次が揃う:

| ファイル | 中身 |
|---|---|
| `summary.md` / `report.md` | 自動診断。最初に読む |
| `export/access.csv` | 1 行 1 リクエスト（時刻、正規化パス、status、reqtime、role=Cookie 名、sess、UA、接続番号、upstream） |
| `export/slow.csv` | 1 行 1 SQL（正規化文、tag=呼び出し元エンドポイント、loc=発行したコードの `file.go:行`（どちらも sqltag 使用時）、query_time、rows_examined…） |
| `export/sql-examples.csv` | 正規化文 → 生の例 |
| `export/timeline.csv` | 1 秒ごとの http / CPU / メモリ / ネット / MySQL の統合系列 |
| `export/procs.csv` | 1 秒ごとのプロセス別 CPU |
| `flows.md` / `flows.html` | ベンチのクライアント（セッション Cookie）ごとの最初の 8 ステップを段に展開した遷移（同じ endpoint の連続は 1 ステップ）。md は表（Claude が読む）、html は Sankey 図（人が見る。Mermaid を CDN から読むので手元で開く）。`task flows TS=… FROM='METHOD /path'` でループの途中から、`ROLE=<cookie>` で役割別に作り直せる |
| `<host>/app.log` | アプリの journald ログ（run の区間） |
| `<host>/nginx-error.log`, `mysql-error.log` | 同区間のエラーログ |
| `<host>/processlist.txt`, `innodb-status*.txt`, `db-catalog.txt` | 実行中クエリ、InnoDB の状態（デッドロック含む）、変数・テーブル行数・index 一覧 |
| `<host>/cpu.pb.gz`, `fgprof.pb.gz`, `goroutine.txt` … | pprof（アプリに入れていれば） |
| `note.json` | スコア、メモ、ベンチの出力（管制盤で入力したもの） |

アプリ知識は repo の `docs/app-map.md`（画面 → API → ハンドラ → テーブル）、マニュアル、`webapp/sql/*schema*.sql`。

## 手順（Claude がやる）

0. app-analysis の report をこの run で更新する（読むのは人。Claude は読まない）:
   ```
   task app:refresh TS=<ts>      # app-analysis の report に今回の run を反映（巡回しない、1 秒）
   git add docs/reports/latest.html docs/reports/latest.md && git commit -m "app report <ts>" && git push
   ```
   push すると、1 分以内に ops パネルの `/app/` で見られる（毎分の cron pull）。
1. `summary.md` と `note.json` を読む。最初に **スコアを生む流れ（シナリオ）と、その 1 周の時間の内訳** を確かめる（summary.md の **Flows** 節: Score loops の 1 周の時間と各ステップの応答時間、Transitions、Drop-off。足りなければ `flows.md`（どの段でどこへ分かれるか）と `queries.md` §10 の Q-scenario と Q-concurrency。Verdict の "Bench shape" が固定並列なら、1 周の中で一番長いステップを削るのがそのままスコア）。そのうえで、ベンチの fail 理由やスコア式に照らして、**仮説を 3 つ**書く（例:「通知の p99 が 500ms を超えた時点でユーザー増加が止まっている」「chair_locations の INSERT がロックを持ち続けている」）。
2. 仮説ごとに `queries.md` のクエリを起点に DuckDB で確かめる。生ログを `cat` しない。出力は集計と上位 N 行だけ。
   - 時間経過で遅くなる → index 欠落やデータ増加
   - CPU が余っていて p99 が高い → ロック、外部 API、プール上限、ポーリング間隔
   - 5xx / タイムアウト → `app.log` の同時刻を grep
   - セッションが増えない → 増加が止まった時刻の p99 とエラーを見る
3. 結論は次の形で書く。推測と事実を分ける。
   - **事実**: 数字（クエリ結果の要約）
   - **解釈**: 何が律速か
   - **提案**: 変更内容、触るファイルと関数（report の SQL 表の「code」列、slow.csv の `loc`、app-map 参照）、期待効果（例: SQL 時間の 23% を削る）、リスク（整合性の猶予、再起動後のデータ）
4. **結論をファイルに残す**: `docs/deepdive/<ts>.md`（40 行以内、上の 3 の形のまま）に書いて commit する（チャットだけだと他のセッションや後の振り返りから見えない。`measurements/` は gitignore なので CSV も残らない）。先頭に run の ts、スコア（分かれば）、使ったクエリの節番号、`flows.html` から読んだ流れの要点 2〜3 行（Sankey 図を見せたいときは `cp measurements/<ts>/flows.html docs/deepdive/<ts>-flows.html` して一緒に commit し、リンクを書く）。提案は 1 件ずつ「issue のタイトル 1 行 + 本文（根拠の数字、触るファイル、期待効果、リスク）」の形で書き、`gh issue create --title … --body …` にそのまま渡せるようにする。
5. 実装は別セッションで。issue を作るかは人に聞く。

## ベンチ挙動の推測の扱い（昇格のルール）

- §10 の結果は「推測」と書く。ベンチの実装を見たかのような断定はしない（isucandar 型を仮定しているだけ）。
- 練習の run では、ベンチ側のログ（`/tmp/contestant.log`、`result.json` のタグ別件数）と答え合わせをして、当たり外れを `docs/retro/` に残す。
- 2 回以上の練習で「当たった・判断に使えた」クエリだけ report（`tools/isumeasure/report.go`）に昇格する。今 report にあるのは Bench shape の 1 行、endpoint 表の 304% 列、管制盤の score/POST 列だけ。

## 予算

- summary.md（約 8KB）と note.json を読む以外、ファイルは読まない。CSV は DuckDB 経由。
- クエリは 1 回の出力を 40 行以内に。
- 1 回の deep-dive で 10〜15 クエリ、報告は 40 行以内を目安にする。

## 道具

- `duckdb`（手元: `brew install duckdb`）。`duckdb -c "SELECT ... FROM 'export/access.csv'"` で直接 CSV を読める。
- `go tool pprof -top -nodecount=30 <host>/cpu.pb.gz`、`go tool pprof -http=:8081 <host>/fgprof.pb.gz`。
- `grep -n "<HH:MM:SS>" <host>/app.log`。
