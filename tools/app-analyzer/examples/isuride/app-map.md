# ISURIDE app-map（ISUCON14 / 実機 https://xiv.isucon.net に対する分析サンプル）

- 入力: `out/20261011-125023/report.md`（Playwright 巡回 + 配車フロー）、公式リポジトリ `webapp/go/*.go`、`webapp/sql/1-schema.sql`、`docs/ISURIDE.md`、`docs/manual.md`
- 目的: 画面 → API → ハンドラ → テーブルの対応と、スコアに効く導線の仮説を 1 枚にする。仮説には「仮説」と明記する。

## 1. 画面一覧

| ペルソナ | 画面 | 役割 | 呼ぶ API（観測） | 備考 |
|---|---|---|---|---|
| guest | `/` | ランディング | なし | |
| guest | `/client/login` `/owner/login` | セッショントークンを Cookie に入れるだけ | なし | `app_session` / `owner_session`。ログイン API は存在しない |
| guest | `/client/register` `/owner/register` | 新規登録 | `POST /api/app/users`、`POST /api/owner/owners`（未観測・OpenAPI より） | 招待コード入力あり |
| user | `/client` | 地図で現在地・目的地を選び配車 | `GET /api/app/notification`（41ms 間隔ポーリング）、`GET /api/app/nearby-chairs`（位置選択時）、`POST /api/app/rides/estimated-fare`、`POST /api/app/rides` | モバイル想定の UI |
| user | `/client/history` | ライド履歴 | `GET /api/app/rides` + notification ポーリング | |
| user | `/client/user` | ユーザー情報・切替 | notification ポーリングのみ | |
| user | `/client/register-payment` | 決済トークン登録 | `POST /api/app/payment-methods` | PK が user_id のため 2 回目は 500（Duplicate entry）になるのを観測 |
| owner | `/owner` | 椅子一覧 | `GET /api/owner/chairs`（**437ms**、無負荷） | 総走行距離を毎回計算 |
| owner | `/owner/sales` | 売上（期間、椅子別 / モデル別） | `GET /api/owner/sales?since&until`（**600ms**、無負荷）、`GET /api/owner/chairs` | |
| guest | `/simulator` | 競技者用の椅子シミュレーター | `GET /api/chair/notification`（48ms 間隔）、`POST /api/chair/coordinate`、`POST /api/chair/activity` | ベンチの椅子クライアントの挙動を模したもの |

フロントは `EventSource` 対応コードを含む（SSE に切り替えればクライアント側はそのまま動く）。`setTimeout` のリテラルは 100 / 300ms。

## 2. エンドポイント表（API → ハンドラ → テーブル）

| method path | ハンドラ | 読む / 書くテーブル | 呼び元 | 無負荷の応答 | 所見 |
|---|---|---|---|---|---|
| POST /api/initialize | postInitialize | 全テーブル再作成（init.sh）、settings | ベンチ | - | 30 秒制限。スキーマ変更はここに同期 |
| POST /api/app/users | appPostUsers | users INSERT、coupons INSERT、coupons `FOR UPDATE`（招待コード）、users（invitation_code 検索） | 登録画面 / ベンチ初期化 | - | 招待は最大 3 人の制約をロックで実装 |
| POST /api/app/payment-methods | appPostPaymentMethods | payment_tokens INSERT | 決済登録画面 | 9ms | |
| GET /api/app/rides | appGetRides | rides（user）→ 各 ride: ride_statuses 最新、chairs、owners | 履歴 | 22ms | **N+1** |
| POST /api/app/rides | appPostRides | rides（user 全件）→ 各 ride の最新 status を確認、rides INSERT、ride_statuses INSERT(MATCHING)、coupons `FOR UPDATE` | 配車画面 | - | 「未完了ライドがあれば 400」の判定が N+1 |
| POST /api/app/rides/estimated-fare | appPostRidesEstimatedFare | coupons | 配車画面（目的地確定時） | - | 計算のみ |
| POST /api/app/rides/:id/evaluation | appPostRideEvaluatation | rides、ride_statuses INSERT(COMPLETED)、payment_tokens、settings、rides（user 全件 ASC）、**外部: 決済 GW** | 配車画面（到着後） | 83ms | 決済はリトライ + 履歴照合。Idempotency-Key 対応あり |
| GET /api/app/notification | appGetNotification | rides（user 最新）、ride_statuses（app_sent_at IS NULL の最古）、chairs、**getChairStats: rides(chair 全件) → 各 ride の ride_statuses**、ride_statuses UPDATE(app_sent_at) | 全 client 画面、**30ms 間隔**（`retry_after_ms: 30`） | 15ms | **呼び出し回数 × N+1 で最重量候補** |
| GET /api/app/nearby-chairs | appGetNearbyChairs | chairs 全件 → 各 chair: rides 全件 → 各 ride の statuses、chair_locations 最新 | 配車画面（位置選択） | 35ms | **二重 N+1 + chairs 全走査**。ベンチは「3 秒以内の座標」を要求 |
| POST /api/owner/owners | ownerPostOwners | owners INSERT | 登録画面 / ベンチ初期化 | - | |
| GET /api/owner/sales | ownerGetSales | chairs（owner）→ 各 chair: rides JOIN ride_statuses(COMPLETED, 期間) | 売上画面 | **600ms** | N+1 + JOIN。owner は「定期的に売上を確認して椅子を増やす」= ユーザー増加に効く |
| GET /api/owner/chairs | ownerGetChairs | chairs + chair_locations 全履歴から LAG で総距離をサブクエリ計算 | 椅子一覧・売上画面 | **437ms** | 3 秒の猶予あり → 座標更新時に加算して列に持つ |
| POST /api/chair/chairs | chairPostChairs | owners（chair_register_token）、chairs INSERT | ベンチ初期化 | - | |
| POST /api/chair/activity | chairPostActivity | chairs UPDATE(is_active) | 椅子クライアント | 19ms | |
| POST /api/chair/coordinate | chairPostCoordinate | chair_locations INSERT → 読み戻し、rides（chair 最新）、ride_statuses INSERT(PICKUP / ARRIVED 判定) | 椅子クライアント（移動のたび） | 23ms | **書き込み量最大**。「現在位置」列と「総距離」列をここで更新すれば読み側が軽くなる |
| GET /api/chair/notification | chairGetNotification | rides（chair 最新）、ride_statuses（chair_sent_at IS NULL）、users `FOR SHARE`、ride_statuses UPDATE | 椅子クライアント、30ms 間隔 | 15ms | app 側と同じ構造 |
| POST /api/chair/rides/:id/status | chairPostRideStatus | rides `FOR UPDATE`、ride_statuses INSERT(ENROUTE / CARRYING) | 椅子クライアント | 22ms | |
| GET /api/internal/matching | internalGetMatching | rides（chair_id IS NULL 最古 1 件）、chairs（is_active ランダム 1 件）、ride_statuses で空き判定、rides UPDATE | `isuride-matcher`（**500ms ごとに 1 件**） | - | **仕様変更自由**。距離も速度も見ないランダム割当 |
| 認証 middleware | app/owner/chairAuthMiddleware | users / owners / chairs を access_token で検索 | 全 API | - | 全リクエストで 1 クエリ。index とキャッシュを確認 |

## 3. データモデルと状態遷移

- `rides` は要求 1 件 1 行。**現在の状態は `ride_statuses` の最新行**（append-only）で表現され、`app_sent_at` / `chair_sent_at` で「通知済み」を管理する。
- 状態遷移: `MATCHING →(椅子が受理: chair/rides/:id/status ENROUTE)→ ENROUTE →(椅子の座標 = 乗車位置: chair/coordinate)→ PICKUP →(chair/rides/:id/status CARRYING)→ CARRYING →(座標 = 目的地)→ ARRIVED →(app/rides/:id/evaluation)→ COMPLETED`
- 実機で 1 ライドを通した結果（flow:ride-lifecycle）: マッチングは matcher 経由で約 0.3 秒、評価→決済→COMPLETED は 83ms。
- 通知は **発生順に at least once** で全遷移を返す必要がある（マニュアル）。キャッシュや SSE 化の際にこの順序保証が制約になる。
- `chair_locations` は append-only で、総距離・現在位置は毎回集計している（owner/chairs、nearby-chairs）。
- ユーザーは未完了ライドがあると新規要求不可、椅子は未完了ライドがあると受理不可。

## 4. スコアとユーザー増加に効く導線（仮説。ベンチのログで検証する）

- スコア = 椅子のマッチ位置→乗車位置距離 × 0.1 + 乗車→目的地距離 + 完了ライド数 × 5。評価（マッチまでの時間、椅子の近さ・速さ、乗車までの時間、移動時間）が高いとユーザーが増える正帰還。
- したがって **マッチングの質と速度**（500ms に 1 件のランダム割当 → 間隔短縮 + 複数件処理 + 近い / 速い椅子を選ぶ）が最大のレバー。`/api/internal/matching` と `isuride-matcher` は仕様変更自由と明記されている。
- 通知は `retry_after_ms` をクライアントが尊重する。値を上げればポーリング負荷は下がるが、通知遅延は評価に効く可能性がある。タイムアウトは notification のみ 60 秒なので **ロングポーリング / SSE** が選択肢。
- 決済は「負荷終了後 5 秒以内に完了」「評価済みで未払いがあれば FAIL」。非同期化するなら 5 秒以内に必ず吐き切る。
- 整合性の猶予: nearby-chairs の座標は 3 秒以内、owner/chairs の総距離は 3 秒、マッチ後の通知は 30 秒。**この猶予がキャッシュ設計の上限**。

## 5. 待ちが発生する箇所

- クライアント側ポーリング間隔（30ms）とサーバーの `retry_after_ms`
- `isuride-matcher` の 500ms 間隔と 1 件ずつの割当
- 決済マイクロサービス（不安定、リトライ、履歴照合）
- `FOR UPDATE` / `FOR SHARE`（rides、coupons、users）によるロック待ち

## 6. 疑わしいホットスポット（優先順）

1. `GET /api/app/notification` / `GET /api/chair/notification`: 回数が桁違い + getChairStats の N+1。まず stats の事前集計、次に retry_after_ms / SSE。
2. `GET /api/internal/matching`: アルゴリズムと間隔。スコアに直結。
3. `GET /api/app/nearby-chairs`: chairs 全走査 + 二重 N+1。chairs に現在位置と「空き」を列として持てば 1 クエリ。
4. `GET /api/owner/chairs`: 総距離の集計 → `POST /api/chair/coordinate` 時に加算。
5. `GET /api/owner/sales`: N+1 と期間 JOIN → index（rides.chair_id, updated_at）と集計の見直し。
6. `POST /api/chair/coordinate`: 書き込み量最大。バルク / 非同期化と、派生列の更新をここに寄せる。
7. 認証 middleware: access_token の index とインメモリキャッシュ。
8. `GET /api/app/rides` / `POST /api/app/rides`: user のライド全件走査。

## 7. 未確認・マニュアルやログで確かめること

- ベンチが実際に `retry_after_ms` をどこまで尊重するか、SSE に切り替えた時の挙動（アクセスログの間隔で確認）
- 椅子・ユーザーの数と、`POST /api/chair/coordinate` の秒間件数（書き込み設計の前提）
- `nearby-chairs` の呼び出し頻度（ベンチのシナリオでどのタイミングで呼ぶか）
- 評価値がユーザー増加にどう効くか（マニュアルの「評価に関係する項目」の重み）
- users / owners / chairs の access_token に index があるか（schema の UNIQUE 制約を確認）
- `payment_tokens` の PK が user_id であることをベンチがどう扱うか（再登録の有無）

## 8. 参照

- スクリーンショット: `out/20261011-125023/shots/`（client は 390×844、owner は 1280×800）
- レポート: `out/20261011-125023/report.html` / `report.md`
