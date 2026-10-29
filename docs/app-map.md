# ISURIDE app-map（ISUCON14 練習 2026-10-29）

- 入力: `tools/app-analyzer/out/20261029-132906/report.md`（巡回 14画面 + flow `ride-lifecycle`、code 6484a0d）、`webapp/go/*.go`、`webapp/sql/1-schema.sql`、`docs/manual/app.md`・`day.md`、`docs/kickoff.md`
- アプリの挙動とデータの地図。性能の所見はbenchのreport（`measurements/<ts>/summary.md`）を見る。仮説には「仮説」と書く。
- 前提: ベンチはブラウザを使わずAPIを直接叩く。下の呼び出し頻度は画面の挙動で、ベンチの挙動ではない。

## 1. 画面一覧

| ペルソナ | 画面 | 役割 | 呼ぶAPI（観測） | 備考 |
|---|---|---|---|---|
| guest | `/` | ランディング（client / owner / simulatorへのリンク） | なし | |
| guest | `/client/login` `/owner/login` | プリセットのsessionTokenをCookieに入れるだけ | なし | ログインAPIは無い。`app_session` / `owner_session` / `chair_session` |
| guest | `/client/register` `/owner/register` | 新規登録（userは招待コード入力あり） | `POST /api/app/users`、`POST /api/owner/owners`（巡回では未観測、OpenAPIより） | 未ログインで`/client`を開くと`/client/register`へリダイレクト |
| user | `/client` | 地図で現在地・目的地を選び配車、進行中のライド表示、到着後に評価 | `GET /api/app/notification`（ポーリング）、`GET /api/app/nearby-chairs`、`POST /api/app/rides/estimated-fare`、`POST /api/app/rides`、`POST /api/app/rides/:id/evaluation` | 画面の実体はライドの状態（3節） |
| user | `/client/history` | ライド履歴 | `GET /api/app/rides` + notificationポーリング | |
| user | `/client/user` | ユーザー情報・切替 | notificationポーリングのみ | |
| user | `/client/register-payment` | 決済トークン登録 | `POST /api/app/payment-methods` | 登録済みユーザーでは500（PKがuser_idで重複）を観測 |
| owner | `/owner` | 椅子一覧（総走行距離つき） | `GET /api/owner/chairs` | |
| owner | `/owner/sales` | 期間の売上（椅子別 / モデル別） | `GET /api/owner/sales?since&until`、`GET /api/owner/chairs` | ownerはこれを見て椅子を増やす（=ベンチの椅子の増加） |
| chair | `/simulator` | 競技者用の椅子シミュレーター（1秒ごとに位置・状態を更新） | `GET /api/chair/notification`（ポーリング）、`POST /api/chair/coordinate`、`POST /api/chair/activity`、`POST /api/chair/rides/:id/status` | ベンチの椅子クライアントの模倣 |

フロントのコード走査ではsetInterval / EventSourceは見つからず（バンドル未取得）。ポーリング間隔はサーバーが返す`retry_after_ms: 30`に従う。

## 2. エンドポイント表（API → ハンドラ → テーブル）

| method path | ハンドラ | 読む / 書くテーブル | 呼び元 | 所見（挙動） |
|---|---|---|---|---|
| POST /api/initialize | postInitialize (main.go:123) | `init.sh`で全テーブル再作成 + `indexes/apply.sh`、settings UPDATE | ベンチ | **exec**（`../sql/init.sh`）。`payment_server`をsettingsに保存、応答`{"language":"go"}` |
| POST /api/app/users | appPostUsers (app_handlers.go:28) | users INSERT、coupons INSERT（CP_NEW2024）、招待時: coupons `FOR UPDATE`・users（invitation_code）・coupons INSERT×2 | 登録画面 / ベンチ初期 | 招待は最大3人をロックで実装。Cookie `app_session`を発行 |
| POST /api/app/payment-methods | appPostPaymentMethods | payment_tokens INSERT | 決済登録画面 | 重複は500（DBエラー文がそのまま返る） |
| GET /api/app/rides | appGetRides | rides（user）、ride_statuses、chairs、owners、coupons（運賃） | 履歴画面 | COMPLETEDのライドだけ返す |
| POST /api/app/rides | appPostRides | rides（user全件）+ 各最新status、rides INSERT、ride_statuses INSERT(MATCHING)、coupons `FOR UPDATE`/UPDATE(used_by) | 配車画面 | 未完了ライドがあれば409。初回はCP_NEW2024、以降は古い順にクーポン消費。202 |
| POST /api/app/rides/estimated-fare | appPostRidesEstimatedFare | coupons（読むだけ） | 配車画面 | 運賃計算のみ |
| POST /api/app/rides/:id/evaluation | appPostRideEvaluatation (app_handlers.go:508) | rides UPDATE(evaluation)、ride_statuses INSERT(COMPLETED)、payment_tokens、settings、coupons、rides（user全件） | 配車画面（ARRIVED後） | **外部HTTP: 決済GW**（`POST/GET {payment_gateway_url}/payments`）をtxの中で呼ぶ。失敗時は100ms × 最大5回リトライ、支払履歴の件数で重複判定。`Idempotency-Key`は未使用 |
| GET /api/app/notification | appGetNotification (app_handlers.go:661) | rides（user最新）、ride_statuses（app_sent_at IS NULL最古）、chairs、getChairStats（rides・ride_statuses）、ride_statuses UPDATE(app_sent_at) | 全client画面 | **書き込みあり**（未送信statusを送信済みにする）。`retry_after_ms: 30` |
| GET /api/app/nearby-chairs | appGetNearbyChairs (app_handlers.go:834) | chairs、rides、ride_statuses、chair_locations（最新） | 配車画面 | 空いている椅子の現在位置を返す。座標は3秒の猶予 |
| POST /api/owner/owners | ownerPostOwners | owners INSERT | 登録画面 / ベンチ初期 | |
| GET /api/owner/sales | ownerGetSales (owner_handlers.go:82) | chairs（owner）、rides JOIN ride_statuses（COMPLETED、期間） | 売上画面 | 椅子別・モデル別に集計 |
| GET /api/owner/chairs | ownerGetChairs (owner_handlers.go:193) | chairs + chair_locations全履歴から総距離を計算 | 椅子一覧・売上画面 | 総距離は3秒の猶予 |
| POST /api/chair/chairs | chairPostChairs | owners（chair_register_token）、chairs INSERT | ベンチ初期 | |
| POST /api/chair/activity | chairPostActivity | chairs UPDATE(is_active) | 椅子 | 204 |
| POST /api/chair/coordinate | chairPostCoordinate (chair_handlers.go:97) | chair_locations INSERT → 読み戻し、rides（chair最新）、ride_statuses INSERT(PICKUP / ARRIVED) | 椅子（移動のたび） | 座標が乗車位置 / 目的地に一致したら状態を進める。椅子は成功を確認するまで動かない |
| GET /api/chair/notification | chairGetNotification (chair_handlers.go:187) | rides（chair最新）、ride_statuses（chair_sent_at IS NULL）、users `FOR SHARE`、ride_statuses UPDATE(chair_sent_at) | 椅子 | **書き込みあり**。`retry_after_ms: 30` |
| POST /api/chair/rides/:id/status | chairPostRideStatus (chair_handlers.go:272) | rides `FOR UPDATE`、ride_statuses INSERT(ENROUTE / CARRYING) | 椅子 | 204 |
| GET /api/internal/matching | internalGetMatching (internal_handlers.go:10) | rides（chair_id IS NULL最古1件）、chairs（is_activeからRAND 1件、最大10回）、ride_statuses（空き判定）、rides UPDATE(chair_id) | `isuride-matcher`（`ISUCON_MATCHING_INTERVAL`ごと、既定500ms） | **仕様変更自由**（app.md「ライドのマッチング」）。1回で1件だけ、距離・速度を見ない |
| 認証middleware | app/owner/chairAuthMiddleware (middlewares.go) | users / owners / chairsをaccess_tokenで検索 | 認証付き全API | Cookieが無ければ401 |

## 3. データモデルと状態

| テーブル | 主キー | 参照 | 性質 |
|---|---|---|---|
| settings | name | | `payment_gateway_url`の1行。initializeで更新 |
| chair_models | name | | マスタ（モデル名 → speed）。`2-master-data.sql` |
| owners | id | | 登録で増える。access_token / chair_register_token |
| chairs | id | owner_id → owners | `is_active`が配車受付の状態。現在位置の列は無い |
| chair_locations | id | chair_id → chairs | **append-only**の位置履歴。現在位置 = 最新行、総距離 = 全履歴の差分合計 |
| users | id | | invitation_codeを持つ |
| payment_tokens | user_id | user_id → users | 1ユーザー1トークン |
| rides | id | user_id → users、chair_id → chairs | 要求1件1行。マッチで`chair_id`、評価で`evaluation`を更新 |
| ride_statuses | id | ride_id → rides | **append-only**。現在の状態 = 最新行。`app_sent_at` / `chair_sent_at`で通知済みを管理 |
| coupons | (user_id, code) | user_id → users | `used_by`（ride id）で使用済み。CP_NEW2024 / INV_<code> / RWD_<code>_<ms> |

行数は未取得（ベンチ後に`task db:snapshot LABEL=latest`）。初期データは`3-initial-data.sql.gz`（init.shが読む）。

状態遷移（app.md「ライド」、flowで観測）:

| from → to | 進めるAPI | アクター |
|---|---|---|
| (なし / COMPLETED) → MATCHING | `POST /api/app/rides` | user |
| MATCHING（chair_id付与） | `GET /api/internal/matching` | matcher（状態は変えない、rides.chair_idだけ） |
| MATCHING → ENROUTE | `POST /api/chair/rides/:id/status`（ENROUTE） | chair |
| ENROUTE → PICKUP | `POST /api/chair/coordinate`（乗車位置に到着） | chair |
| PICKUP → CARRYING | `POST /api/chair/rides/:id/status`（CARRYING） | chair |
| CARRYING → ARRIVED | `POST /api/chair/coordinate`（目的地に到着） | chair |
| ARRIVED → COMPLETED | `POST /api/app/rides/:id/evaluation`（決済つき） | user |

- 各状態はnotificationが`ride_statuses`の未送信行を古い順に1件ずつ返す。全遷移を発生順にat least once（app.md「通知の順序」）。
- userは未完了ライドがあると新規要求不可、椅子は未完了ライドがあると受理不可。matcherの「空き」判定は`chair_sent_at`が6件そろったか。

## 4. スコアとユーザー増加に効く導線

- スコア = マッチ位置 → 乗車位置の距離 × 0.1 + 乗車位置 → 目的地の距離 + 完了ライド数 × 5（day.md「スコアの計算」）。点が付くのはライドが完了まで進んだときで、進めるAPIは3節の表。
- 評価（マッチまでの時間、近い / 速い椅子か、乗車までの時間、移動時間）が高いとユーザーが増える（app.md「ライド」）。仮説: マッチングの質と間隔がユーザー増加 = スコアの伸びに直結する。matcherと`/api/internal/matching`は仕様変更自由。
- ownerは売上を見て椅子を増やす（app.md「オーナー」）。仮説: `GET /api/owner/sales`が返らないと椅子が増えない。
- 猶予: nearby-chairsの座標は過去3秒、owner/chairsの総距離は3秒、マッチ後の通知は30秒、通知は状態変化から3秒以内（day.md「猶予時間」、app.md「通知エンドポイント」）。それ以外は即時。
- タイムアウト: initialize 30秒、notification 2本は60秒、他10秒。登録APIは処理全体20秒（day.md「負荷走行」「ベンチマーカーのタイムアウト」）。
- FAIL: 想定外の通知・状態遷移、長時間マッチしない、評価タイムアウト、未払い、誤った支払い、ワーニング200件以上。負荷終了後5秒以内に決済完了（day.md「負荷走行の打ち切り」）。
- 通知はJSONポーリング（`retry_after_ms`）かSSE（`text/event-stream`、接続直後に最新状態を送る）を選べる（app.md「SSE」）。

## 5. 待ちが発生する箇所

- クライアントのポーリング: notification 2本、サーバーの`retry_after_ms: 30`に従う（巡回で約50〜70ms間隔）。
- `isuride-matcher`: `while true; curl -s https://isuride.xiv.isucon.net/api/internal/matching; sleep $ISUCON_MATCHING_INTERVAL`。公開ドメイン経由でnginx → localhost:8080に入る（ホスト依存: svc1のnginxが受ける）。
- 決済GW: 評価のtxの中で外部HTTP、失敗で100ms sleep × 最大5回。本番の決済はベンチ機側（`--payment-url`）。
- ロック: coupons `FOR UPDATE`（登録・配車）、rides `FOR UPDATE`（椅子の状態更新）、users `FOR SHARE`（椅子の通知）。
- 常駐プロセス: `isuride-matcher.service`、`isuride-payment_mock.service`（:12345、開発用）。initializeは`exec`で`init.sh`を走らせる（DB接続は`env.sh`）。

## 6. 未確認・ログで確かめること

- ベンチの椅子・ユーザー・ownerの数の推移と、`POST /api/chair/coordinate`の秒間件数（アクセスログ）
- ベンチが`retry_after_ms`をどこまで尊重するか（notificationの間隔をアクセスログで）
- `GET /api/app/nearby-chairs`と`GET /api/owner/sales`をベンチがどのタイミング・頻度で呼ぶか
- 評価値の分布とユーザー増加の関係（マニュアルに重みは無い）
- 決済GWの失敗率と502の件数、ベンチの決済サーバーが`Idempotency-Key`を前提にしているか（`webapp/payment_mock/openapi.yaml`）
- reportの「OpenAPI not observed」の`POST /api/chair/notification`はOpenAPI（`GET`のみ）にもGoのルートにも無い。analyzerの誤検出と見る（仮説）
- matcherの間隔を変えたときのマッチ数（`env.sh`の`ISUCON_MATCHING_INTERVAL`）

## 7. 参照

- レポート: `tools/app-analyzer/out/20261029-132906/report.md` / `report.html`、`docs/reports/latest.html`
- スクリーンショット: `tools/app-analyzer/out/20261029-132906/shots/`（guest-00〜06、owner-00〜01、chair-00、flowのステップ）
- annotations: `docs/app-annotations.json`
