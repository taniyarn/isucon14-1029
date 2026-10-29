# 他チームの計測・分析手法から取り込む候補（2026-10-17 調査）

原本（36 項目、出典 URL 付き）: `sources/measurement-practices.md`。ここでは **今の kit に無いもの** を、効果と工数で並べ替えた。採否は人が決める。工数 S = 半日以内、M = 1〜2 日、L = それ以上。

## 今すぐ入れる価値が高い（S、ユーザー行動分析に直結）

| # | 何を | なぜ（出典） | kit のどこに |
|---|---|---|---|
| M1 | **アプリがレスポンスヘッダにユーザー ID（の HMAC）を返し、nginx が `uid:$upstream_http_x_isu_userid` で記録** | NaruseJun（13 優勝）は per-user のログを Sankey にして「シナリオ推定に大分参考になった。ログを見てからマニュアルを読むと意図が分かる」。Mirrativ（11f）も同じ。今の kit は Cookie か接続番号で代用しており、**ログイン前後で鍵が変わる**（deep-dive §10 の注意書き）のがこれで消える | 計測パッチ（sqltag README）にミドルウェア 1 行、ltsv に 1 列、export の `sess` を uid 優先に |
| M3 | **点になるアクションの 1 秒あたり件数と、score ÷ 件数を run ごとに表示** | EkeMinusYou は 55 run を `score / PASS / evaluation-2xx / users / chairs` で表にして「完了ボーナスは 7.6% だけ、距離が支配」と判断。kit は score/POST 列まで入れた | report に per-second の rate、管制盤の timeline に列（alp.yml の `score_post`） |
| M4 | **ベンチ出力（stdout）を時系列表にして run に紐づける**（警告・エラー・「負荷レベル上昇」「ユーザーが増えませんでした」） | fukkatsuso（14）の後悔 1 位「ベンチのログを十分見なかった」。catatsuy（12q）は最後 10 分でエラーログを読まず 0 点 | ポータルの出力を人が貼る欄（note）に加え、`bench.log` を run ディレクトリに置けば report が節を作る |
| M19 | **`performance_schema.events_statements_summary_by_digest` を slow log の代わり/併用に** | takonomura（14 優勝）の `scripts/sql-performance.sh`。白金動物園「ON でもスコアに影響なし、TRUNCATE でリセット、NO_INDEX_USED 列が決定打」。**long_query_time=0 の slow log（44 MB gz）より軽い**。注: interpolateParams=true でないと prepared が digest されない | agent が run 開始で TRUNCATE、終了で SELECT → `perf-schema.txt`。report の SQL 表の列に `NO_INDEX_USED` / `QUANTILE_99` |
| M21 | **rows examined / sent の比を index の判定ルールに**（<5 倍は index 済み、100 倍超は無し、比 1 で絶対数が大きいなら LIMIT 漏れ） | NaruseJun の手順 2 番目。11q 公式攻略法の `Rows_sent=579.2` の例 | 既に列はある。report の色分け閾値（100/1000 倍）に「比 1 だが rows_sent > 100 → LIMIT 候補」を足す |
| M11 | alp のクエリ文字列を値マスクで残す（`/search?tag=` と `?name=` を分ける） | takonomura の alp.yml。「ベンチの動きはクセがあるのでクエリパラメータも見るべき」 | `isumeasure groups` / normalizer に `query_string` オプション |
| M34 | **計測のキルスイッチ 1 つ + 最終前チェックリスト** | NaruseJun は pprotein のログが残っていて最後の 1 時間で 277k → 468k。14 では VPC の計測インスタンスで失格 | `task hosts:measure-off` はある。アプリ側の `ENABLE_MEASURE=0` で sqltag/pprof/logger を一括停止、§8-2 に envcheck |

## 次に効きそう（S〜M）

| # | 何を | なぜ | kit のどこに |
|---|---|---|---|
| M2 | 遷移ごとの gap p50/p95 と **overlap**（前の応答前に次を送った数 = クライアント側の並列度）、署名でセッションを「シナリオ族」に分ける | mazrean は Node Graph で 3 つのユーザー像を読み取った。11f 講評「点になるリクエストは教員→学生の直列依存の裏にある」 | deep-dive §10 の Q-scenario に列追加、report の transitions 表に overlap |
| M5 | distinct ユーザー/秒と新規ユーザー/10 秒を 5xx・timeout と重ねる（負荷増加モデルの推定） | 12f は 10 秒ごとに並列を上げる、8 は timeout でユーザーを減らす、14 は満足度で増やす | timeline に 2 系列（M1 が前提） |
| M7 | 接続の再利用率（`connreq==1` の割合、再利用までの idle）と TLS handshake/秒 | EkeMinusYou は「10 秒以内の idle なら再利用」「SSE を ARRIVED で切ると通知 +63%」を導出。kawaemon は ephemeral port 枯渇 | ltsv に `ssl_reused:$ssl_session_reused`、report に列 |
| M13 | `db.Stats()`（WaitCount、WaitDuration、InUse）を 1 秒ごと | pool 待ちは slow log に出ない。MaxOpenConns の判断材料 | 計測パッチの expvar に `db` はある → agent が 1 秒ごとに取る（app.jsonl に列） |
| M16 | goroutine 数・GC 回数・heap を 1 秒ごと | SSE/ロングポーリングのリーク検出が最速 | 同上（expvar の memstats） |
| M23 | `$upstream_addr` / `$upstream_connect_time` / `$upstream_header_time` を ltsv に | app 2 台のとき「どっちが遅い」を access log だけで答える | ltsv に 3 列、report の host 別 upstream 表 |
| M25 | 負荷窓の中でのサービス別 CPU 秒（host × app/nginx/mysqld） | EkeMinusYou は「1 号機 Go 51.8 CPU 秒、2 号機 nginx 36.0」で役割を決めた | procs.jsonl から集計して summary.md に表 |
| M28 | run の計測品質（サンプル数、欠落、最大 gap）を badge にし、悪い run は比較に使わない | isutools の Collector Health | 管制盤の Runs に badge |
| M31 | run 同士の diff（endpoint の Δcount/Δsum/Δp99、SQL の消えた行/増えた行を赤） | 「減らしたものと増やしたものを両方見る」 | 管制盤に compare ビュー |
| M15 | ドメイン状態のゲージ（未割当件数、空き台数、最古待ち秒）を 1 秒ごと | 14 の「平均未割当 2,697 件、配車可能 251 台、最古待ち 9.29 秒」 | 当日実装: expvar に app 固有のゲージを足す規約を計測パッチに書く |
| M36 | 静的解析の CRUD マップ（関数 → テーブル → 操作） | キャッシュ可否は「誰が書くか」で決まる | codeindex に verb 抽出 → app-map の Data model タブ |

## 見送り・保留

- M18 分散トレーシング（OTel → Jaeger）: L。p1ass（13 4 位）は有効だったが、VPC 外の収集先と最終前の無効化が必須。kit の sqltag + pprof で代替できている間は見送り。
- M20 lock-wait の 50 ms サンプリング: M。processlist スナップショットで代替中。ロック律速が疑われる年に。
- M27 nginx の perf プロファイル: 必要になったら deep-dive で手動。
- M30 commit への結果コメント: 管制盤の timeline が commit を持っているので重複。
- M32 Grafana 風 annotation、M33 読む順序、M35 1 秒解像度: 既に kit の設計と一致。

## 提案する順序

1. M1（uid ヘッダ）+ M34（キルスイッチ）: 計測パッチの変更だけで済み、ユーザー行動分析の鍵が正しくなる。
2. M19 + M21: slow log の OOM 問題の根本対策にもなる。
3. M3 + M4: 「点になる行動」と「ベンチの言い分」を run に紐づける。
4. M13 + M16 + M23: ltsv と expvar の列追加。
5. M2 / M5 / M7: deep-dive のクエリ追加（M1 が前提）。
