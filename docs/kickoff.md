# kickoff — ISUCON14 ISURIDE（練習 2026-10-29）

phase 1（マニュアル）+ phase 2（repo・実機の設定）。サーバー: svc1 13.231.171.206 / 172.31.13.138（ベンチ対象）、svc2 176.32.88.158 / 172.31.6.124、svc3 13.158.31.40 / 172.31.6.139。今は3台ともapp + MySQL + nginx + matcherが動いている（DBは各ホスト別）。

| 項目 | 内容 | 根拠 |
|---|---|---|
| アプリ | ISURIDE（ライドチェア配車。user / chair / owner の 3 種類のクライアント） | app.md「ISURIDEとは」 |
| アクセス | `https://isuride.xiv.isucon.net`。ブラウザは hosts に `<svc1 public IP> isuride.xiv.isucon.net`。証明書は `*.xiv.isucon.net` の正規のもの（自己署名ではない） | day.md「動作確認のためのhostsファイル設定」「TLS証明書」 |
| ベンチ（練習） | セットアップ済み。bench 機 18.181.189.97 で `sudo -iu isucon` → `~/bench run --addr 172.31.13.138:443 --target https://isuride.xiv.isucon.net --payment-url http://172.31.12.206:12346 --payment-bind-port 12346`（`--addr` = svc1 の private IP、決済はベンチ機側） | 盤 t3 |
| ログイン | Cookie のセッション（`app_session` / `owner_session` / `chair_session`、値は sessionToken）。登録 API: `POST /api/app/users`・`/api/owner/owners`・`/api/chair/chairs` | app.md「アカウントプリセット」、day.md「負荷走行」 |
| 初期ユーザー | user `Miller6106` 他 / owner `Seat Revival` 他 / chair `QC-L13-8361`（token は app.md の表） | app.md「アカウントプリセット」 |
| 初期化 API | `POST /api/initialize`（30 秒でタイムアウト）。body `{"payment_server": "<URL>"}` を読む。応答の `language` は `go` のまま（言語賞） | day.md「負荷走行」「言語賞」、app.md「決済マイクロサービスのモックについて」 |
| 言語 / unit | Go が既定（`isuride-go.service`）。他に `isuride-matcher.service`（500ms ごとに `GET /api/internal/matching`）、`isuride-payment_mock.service`（:12345）。DB は MySQL `isuride` | day.md「リファレンス実装の切り替え方法」「データベース(MySQL)」、app.md「ライドのマッチング」 |
| 言語ディレクトリ | `webapp/go`（chi + net/http、sqlx）。規模は[docs/app-size.md](app-size.md): Go 2,088行 / ルート18 / テーブル10、`3-initial-data.sql.gz`はrepoに無い（サーバー上だけ） | `task app:size` |

## 前提の確認

| 前提 | 期待 | 見つけたもの | 根拠 | 判定 |
|---|---|---|---|---|
| DB | MySQL / MariaDB | MySQL（`isuride`） | server-info-ip-172-31-13-138.txt:76,84 `mysql running:yes` | ○ |
| 言語ディレクトリ | webapp/go（go.mod） | webapp/go | `ls webapp/go`（go.mod） | ○ |
| app の起動 | systemd のユニット | isuride-go.service（`Requires=mysql.service`、restart.conf drop-in済み） | etc/systemd/system/isuride-go.service | ○ |
| 前段 | nginx | nginx、`/api/` → `localhost:8080` | etc/nginx/sites-available/isuride.conf | ○ |
| 台数 | 3 | 3（c5.large） | day.md「1. テンプレートのダウンロード」 | ○ |
| ドメイン / ポート | マニュアルどおり | `server_name *.xiv.isucon.net`、443、証明書`_.xiv.isucon.net` | isuride.conf / day.md「動作確認のためのhostsファイル設定」 | ○ |
| initialize の制限時間 | 書いてある | 30秒 | day.md「負荷走行」 | ○ |
| 外部依存 | 無し | 決済マイクロサービス（本番・練習ともベンチ側、練習は`--payment-url http://172.31.12.206:12346`）。URLは`/initialize`のbodyで渡る | app.md「決済マイクロサービス」、盤 t3 | ✗ |

## 構成要素（どこにデータを置くか）

| 要素 | 役割 | データ / 読む設定 | 自前実装の候補 |
|---|---|---|---|
| `isuride-go`（:8080） | API全部 | MySQL `isuride`（env.shの`ISUCON_DB_*`）。`settings.payment_gateway_url`もDB。`/initialize`が`exec ../sql/init.sh`（main.go:131）→ `mysql`クライアントとenv.shのDBホストを使う | — |
| `isuride-matcher` | `while curl https://isuride.xiv.isucon.net/api/internal/matching; sleep $ISUCON_MATCHING_INTERVAL`（0.5秒、env.sh） | 無し。名前は`/etc/hosts`の`127.0.0.1`で自ホストのnginx → 自ホストのapp。`/api/internal/`はlocalhostだけ許可 | ○（仕様も間隔も自由。app内のgoroutineに置き換え可） |
| `isuride-payment_mock`（:12345） | 開発用の決済（常に成功） | 不明（`Requires=mysql.service`）。ベンチは使わない | — |
| MySQL | 全テーブル | `/var/lib/mysql`。init.shが`1-schema.sql` → `2-master-data.sql` → `3-initial-data.sql.gz` | — |
| nginx | TLS終端、静的ファイル（`/home/isucon/webapp/public/`） | isuride.conf | — |

## 初動の注意

- `1-schema.sql`は`DROP TABLE` / `CREATE TABLE`（index が `/initialize` のたびに消える）→ `init.sh`末尾に`bash "$(dirname "$0")/indexes/apply.sh"`があることを確認済み（追加不要）。
- `/initialize`はbodyの`payment_server`を`settings`に保存する（main.go:136）。空で叩くと決済URLが空になる。analyzerの`initializeBody`は`http://localhost:12345`。ベンチ後に手で触るときもこのbodyで叩き直す。
- matcherは3台ともenabled（server-info:74）。DBを共有したら1台だけで回す（ロック無しのマッチングが二重に走る。parallel-setup Aがdeploy.shで他を止める）。
- `isuride-go.service`の`Requires=mysql.service`: DBをsvc3に移すとapp側のmysql停止でappも止まる。分離時にこの行を外す（drop-inでは消せないのでユニットを直す）。
- 決済は`POST /api/app/rides/:ride_id/evaluation`のtxの中で外部HTTPを呼ぶ（app_handlers.go:608、commit 623）。決済サーバーが遅いとtxとコネクションを握ったまま待つ。
- 猶予: nearby-chairsの座標3秒、ownerの距離合計3秒、マッチ後の通知30秒、通知は状態変化から3秒。それ以外は即時。

## 先読みの手（マニュアル + コード。計測前）

| # | 手 | 根拠（マニュアルの見出し / file:line） | 期待効果 | リスク（猶予・クリティカル・再起動） | T# | 担当 |
|---|---|---|---|---|---|---|
| 1 | matchingを「待ち全件 × 空き椅子全件の近い順（速度も考慮）」に作り直す | app.md「ライドのマッチング」（自由に変更可）、「ライド」（評価に効く項目）/ internal_handlers.go:13 `ORDER BY created_at LIMIT 1`、:26 `ORDER BY RAND() LIMIT 1`、:34 椅子ごとの完了判定サブクエリ | 1回1件 → 1回で全件。待ち時間と迎車距離が縮み評価 → ユーザー増（推測） | 「長時間マッチングされない」「完了前に次のライド」はクリティカル。matcherが複数ホストで二重に動く | T34 | レーン c |
| 2 | `retry_after_ms` 30 → 100〜300ms | app.md「JSONレスポンス」（クライアントは指定ms後に再リクエスト）/ app_handlers.go:676,723、chair_handlers.go:204,264 | 通知ポーリングの件数が1/3〜1/10 | 通知は状態変化から3秒以内（それより十分短く） | T17 | レーン c |
| 3 | 欠けているindex: `rides(user_id, created_at)`、`rides(chair_id, updated_at)`、`ride_statuses(ride_id, created_at)`、`chair_locations(chair_id, created_at)`、`chairs(owner_id)`、`coupons(used_by)` | 1-schema.sql（PK / UNIQUEだけ）/ app_handlers.go:288,919、internal_handlers.go:34 | 全件走査 → 索引引き | replace-by-deleteは無し（要再確認）。ORDER BY付きか確認 | — | レーン 3 |
| 4 | N+1: rides一覧・nearby-chairs・椅子の統計・owner/chairs のループ内SELECT | app_handlers.go:214,779,886-898、owner_handlers.go:123,156 | ループ内のSELECTをIN / JOINで1回に | 並び順とtie-breakを保つ | — | レーン 3 |
| 5 | matcherの間隔（0.5秒）を短く、またはappのgoroutineに | app.md「ライドのマッチング」/ env.sh `ISUCON_MATCHING_INTERVAL=0.5`、isuride-matcher.service ExecStart | マッチまでの待ちが最大0.5秒 → 短く | 1台だけで回す。再起動後に自動で再開すること | T35 | レーン c |
| 6 | 決済をtxの外へ + `Idempotency-Key`で再送を冪等に（GET /paymentsの全件比較をやめる） | app.md「Idempotency-Key ヘッダを利用したリクエストの重複防止」/ payment_gateway.go:32-88（5回 × 100ms、キー無し）、app_handlers.go:608 | 評価のレイテンシとtx保持時間が短く | 二重請求・未払いはクリティカル。負荷終了後5秒以内に決済完了 | T14 | レーン c |
| 7 | owner/chairsの移動距離合計を書き込み時に集計 | day.md「猶予時間」（3秒の猶予）/ owner_handlers.go:216 `chair_locations`全件のウィンドウ関数 | 全件走査 → 1行 | 猶予3秒以内。再起動後もDBから読めること | T7 / T5 | レーン c |
| 8 | 通知をSSEに | app.md「SSE(Server-Sent Events)」 | ポーリングが消える（推測） | 接続直後に最新状態を送る、全遷移を順に。改修大 | T18 | 要確認 |

## スコア

- スコア = 「マッチ位置 → 乗車位置の移動距離合計 × 0.1」+「乗車位置 → 目的地の移動距離合計」+「ライド完了数 × 5」（day.md「スコアの計算」）。
- FAIL: 初期化・整合性チェックのエラー、クリティカルエラー（想定外の通知・状態遷移、長時間マッチしない、評価タイムアウト、未払い、誤った支払い）、ワーニング 200 件以上。負荷テスト終了時の決済は 5 秒以内に完了（day.md「負荷走行の打ち切り」）。
- 猶予: nearby-chairs の座標は直近 3 秒以内の coordinate と一致 / owner の移動距離合計は 3 秒 / マッチ後の通知は 30 秒 / 通知は状態変化から 3 秒以内（day.md「猶予時間」、app.md「通知エンドポイント」）。
- タイムアウト: initialize 30 秒、`/api/app/notification`・`/api/chair/notification` 60 秒、他 10 秒。
- **特別賞: 32,767 点を最初に超えた 1 チーム**（day.md「特別賞」）。

## 禁止・必須

- 禁止: `envcheck.service` 関連（`/etc/systemd/system/envcheck.service`、`/opt/isucon-env-checker`）、`isuadmin` ユーザー、`isucon` 以外のユーザーの鍵、SG の 22 / 443 の変更、インスタンスタイプの変更、テンプレート外の計算資源、スコアに効く外部資源（day.md「変更してはいけない点」「競技環境」「禁止事項」）。
- 必須（追試）: 3 台を再起動 → envcheck → ベンチ（FAIL か最終スコアの 75% 以下を 3 回で失格）→ 再起動 → **ベンチで書いたデータが読めること** → フロントの表示（day.md「追試」）。終了後は 3 台とも起動したまま触らない。
- 初期化は `POST /api/initialize` が行っている内容を漏れなく提供（データ構造を変えたら initialize も追従）（day.md「データベースのデータの初期化」）。

## 去年との差分（前回の練習 ISUCON13 との比較）

- DNS（PowerDNS）は無い。代わりに外部の**決済マイクロサービス**（本番はベンチ側。Idempotency-Key 対応、混むとエラー）を app が呼ぶ。
- 付随ワーカー `isuride-matcher`（内部 API をポーリング。仕様・間隔は自由に変えてよい）。
- 通知はポーリング（`retry_after_ms` 30ms）か SSE を選べる。
- ドメインは 1 つ（`isuride.xiv.isucon.net`、サブドメイン無し）。

## レビューで見ること

- 猶予: 座標 3 秒（nearby-chairs / owner の距離合計）、マッチ後の通知 30 秒、通知 3 秒。それ以外は即時。
- 通知は全状態遷移を発生順に at least once。ライドの状態遷移を飛ばす・逆転させる変更はクリティカルで FAIL。
- 決済: 二重請求・未払いはクリティカル。リトライは支払履歴か `Idempotency-Key` で冪等に。負荷テスト終了後 5 秒以内に完了。
- `POST /api/initialize` の `language` は `go` のまま、`payment_server` の設定を保存し続けること。
- envcheck 関連と `isuadmin` に触らない。

```analyzer
{
  "domain": "isuride.xiv.isucon.net",
  "initializeBody": { "payment_server": "http://localhost:12345" },
  "source": "app.md「アカウントプリセット」「決済マイクロサービスのモックについて」",
  "personas": [
    { "name": "user", "group": "app", "token": "d175bd9d34b9c4e0b3d9864a07ccee37f043b9df1e77ca20c78bc5878a537dc6", "cookie": "app_session", "seedPrefix": "client", "viewport": { "width": 390, "height": 844 }, "source": "app.md 利用者一覧 Miller6106" },
    { "name": "owner", "token": "7188f2fb45c7d81a6ba30d1572cfff37b3857e66f7b50513bbf89eb2c4bc0ac7", "cookie": "owner_session", "seedPrefix": "owner", "source": "app.md オーナー一覧 Seat Revival" },
    { "name": "chair", "token": "3013d5ec84e1b230f913a17d71ef27c8d09d777b1cce7a3c1e2ffd4040848411", "cookie": "chair_session", "source": "app.md シミュレーターで利用する椅子 QC-L13-8361" }
  ]
}
```
