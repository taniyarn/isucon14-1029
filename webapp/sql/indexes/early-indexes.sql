-- early-fix: WHERE / ORDER BY columns without an index (schema 1-schema.sql vs handler SQL)
-- chairAuthMiddleware: WHERE access_token = ? (middlewares.go:69)
ALTER TABLE chairs ADD INDEX idx_chairs_access_token (access_token);
-- ownerGetSales / ownerGetChairs: WHERE owner_id = ? (owner_handlers.go:113)
ALTER TABLE chairs ADD INDEX idx_chairs_owner_id (owner_id);
-- appGetNearbyChairs / chairPostCoordinate: WHERE chair_id = ? ORDER BY created_at DESC LIMIT 1 (app_handlers.go:919)
ALTER TABLE chair_locations ADD INDEX idx_chair_locations_chair_id_created_at (chair_id, created_at);
-- chairPostCoordinate / chairGetNotification / getChairStats: WHERE chair_id = ? ORDER BY updated_at DESC (chair_handlers.go:131, 201; app_handlers.go:770)
-- internalGetMatching: WHERE chair_id IS NULL (internal_handlers.go:14)
ALTER TABLE rides ADD INDEX idx_rides_chair_id_updated_at (chair_id, updated_at);
-- appGetNotification / appGetRides / appPostRides: WHERE user_id = ? ORDER BY created_at (app_handlers.go:206, 317, 673)
ALTER TABLE rides ADD INDEX idx_rides_user_id_created_at (user_id, created_at);
-- getLatestRideStatus / notifications / getChairStats: WHERE ride_id = ? ORDER BY created_at (app_handlers.go:288, 686, 784; chair_handlers.go:212)
ALTER TABLE ride_statuses ADD INDEX idx_ride_statuses_ride_id_created_at (ride_id, created_at);
