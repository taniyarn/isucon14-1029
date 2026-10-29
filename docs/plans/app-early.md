# app-early — コードから見える N+1・index不足（baseline前の下読み）

reportが来たら行と突き合わせる。「済」はPR #6（`integ/early`）に入れたもの。

| # | 場所 | 何が | 手 | 状態 |
|---|---|---|---|---|
| 1 | `getChairStats` app_handlers.go:763（`GET /api/app/notification`から毎回） | rideごとに`ride_statuses`をSELECT | rides JOIN ride_statusesの1本 | 済 #6 |
| 2 | `appGetNearbyChairs` app_handlers.go:834 | 全椅子 × rides × `getLatestRideStatus` | 「COMPLETEDの無いrideを持たないactive椅子」の1本 | 済 #6 |
| 3 | `appGetNearbyChairs`の最新位置 app_handlers.go:919 | 残った椅子ごとに`chair_locations` LIMIT 1 | 椅子に最新位置の列を持たせる（`chairPostCoordinate`で更新。設計が要る） | 未 |
| 4 | `ownerGetChairs` owner_handlers.go:198 | `chair_locations`全行にLAGの窓関数（毎回全件） | 椅子ごとの累計距離を`chairPostCoordinate`で更新（猶予3秒あり。設計が要る） | 未 |
| 5 | `appGetRides` app_handlers.go:214 | rideごとに`getLatestRideStatus` + `calculateDiscountedFare` + chairs + owners | chairs JOIN ownersを1本、statusは一括 | 未 |
| 6 | `ownerGetSales` owner_handlers.go:123 | 椅子ごとにrides JOIN ride_statuses | `chair_id IN (…)`の1本 | 未 |
| 7 | `appPostRides` app_handlers.go:323 | ユーザーの全rideに`getLatestRideStatus` | COMPLETEDの無いrideの`EXISTS`1本 | 未 |
| 8 | `internalGetMatching` internal_handlers.go:24 | ランダムな椅子を10回試す + 重い空き判定 | 空き椅子を一括で取って近い順に割り当て（matcherの間隔も自由。scanの型） | 未 |
| 9 | `coupons(used_by)` app_handlers.go:975 | `WHERE used_by = ?`がindex無し | `UPDATE … SET used_by`と同じtxでdeadlockの型に当たる。reportで重ければ別途 | 見送り |
| 10 | `chairs(access_token)` / `rides(user_id …)` などのindex | 認証・通知のWHERE | `early-indexes.sql` 6本 | 済 #6 |
