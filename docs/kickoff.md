# kickoff — ISUCON14 ISURIDE（練習 2026-10-29）

phase 1 の下書き（マニュアルだけから）。phase 2 で 前提の確認 / 構成要素 / 初動の注意 / 先読みの手 / 言語ディレクトリ を埋める。

| 項目 | 内容 | 根拠 |
|---|---|---|
| アプリ | ISURIDE（ライドチェア配車。user / chair / owner の 3 種類のクライアント） | app.md「ISURIDEとは」 |
| アクセス | `https://isuride.xiv.isucon.net`。ブラウザは hosts に `<svc1 public IP> isuride.xiv.isucon.net`。証明書は `*.xiv.isucon.net` の正規のもの（自己署名ではない） | day.md「動作確認のためのhostsファイル設定」「TLS証明書」 |
| ベンチ（練習） | セットアップ済み。bench 機 18.181.189.97 で `sudo -iu isucon` → `~/bench run --addr 172.31.13.138:443 --target https://isuride.xiv.isucon.net --payment-url http://172.31.12.206:12346 --payment-bind-port 12346`（`--addr` = svc1 の private IP、決済はベンチ機側） | 盤 t3 |
| ログイン | Cookie のセッション（`app_session` / `owner_session` / `chair_session`、値は sessionToken）。登録 API: `POST /api/app/users`・`/api/owner/owners`・`/api/chair/chairs` | app.md「アカウントプリセット」、day.md「負荷走行」 |
| 初期ユーザー | user `Miller6106` 他 / owner `Seat Revival` 他 / chair `QC-L13-8361`（token は app.md の表） | app.md「アカウントプリセット」 |
| 初期化 API | `POST /api/initialize`（30 秒でタイムアウト）。body `{"payment_server": "<URL>"}` を読む。応答の `language` は `go` のまま（言語賞） | day.md「負荷走行」「言語賞」、app.md「決済マイクロサービスのモックについて」 |
| 言語 / unit | Go が既定（`isuride-go.service`）。他に `isuride-matcher.service`（500ms ごとに `GET /api/internal/matching`）、`isuride-payment_mock.service`（:12345）。DB は MySQL `isuride` | day.md「リファレンス実装の切り替え方法」「データベース(MySQL)」、app.md「ライドのマッチング」 |

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
