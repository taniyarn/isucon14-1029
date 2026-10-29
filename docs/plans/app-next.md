# app-next — #11の次に効きそうな手（report 20261029-133401の上位endpointから）

決めてほしいこと: 次の`[TASK] 3`にどれを使うか。上から推奨順。どれも50行を超えそうなので設計（ルール6）が要る。

| # | 変わること・触る場所 | 見込み | リスク（猶予・再起動・2台） |
|---|---|---|---|
| 1 | `ownerGetChairs`の総走行距離を、`chairs`に足す要約列（`total_distance`, `total_distance_updated_at`）にする。更新は`chairPostCoordinate`のtxの中（前回位置との差を足す）、`/initialize`で既存の`chair_locations`から作り直す | owner/chairs = SQL時間の30%（#11で絞った後の残りを見て判断） | 猶予3秒あり。DBの列なので再起動OK・2台OK。init.shのスキーマ変更が要る（initializeの追従） |
| 2 | 椅子の最新位置を`chairs`に持つ（`latitude`, `longitude`、`chairPostCoordinate`で更新）。`appGetNearbyChairs`と`internalGetMatching`が読む | nearby avg 3947ms（#6・#11の後の残り次第）。matchingの近い椅子選び（レーンc）の材料にもなる | nearbyは3秒の猶予あり。DB列なので再起動・2台OK |
| 3 | `getChairStats`（ユーザー通知のポーリングから毎回）を`chairs`の要約列（`total_rides_count`, `total_evaluation`）にする。更新は`appPostRideEvaluatation`のtxの中 | 通知ポーリング1回あたりのSQLが1本減る | 猶予の明示なし→同じtxで更新して即時に保つ。DB列なので再起動・2台OK |
| 4 | 通知をSSEにする（`/api/app/notification`・`/api/chair/notification`）。マニュアル「通知エンドポイント」で許可 | chair/notification = リクエスト時間の43%（ポーリング自体が消える） | 全遷移を順に at least once が必須（落とすとFAIL）。2台なら通知元のホストを揃える必要。大きい |
| 5 | `chairPostCoordinate`の`SELECT * FROM chair_locations WHERE id = ?`（INSERT直後の読み戻し）をやめ、`created_at`をGoで決めてINSERTする | `POST /api/chair/coordinate`（書き込み最多）の往復1回減 | 時刻の精度（DATETIME(6)）とタイムゾーンを元と合わせる必要 |
