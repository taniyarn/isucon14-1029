-- report 20261029-133401 indexes not already in early-indexes.sql
-- chairGetNotification: WHERE ride_id = ? AND chair_sent_at IS NULL ORDER BY created_at (chair_handlers.go:212)
ALTER TABLE ride_statuses ADD INDEX idx_ride_statuses_ride_id_chair_sent_at_created_at (ride_id, chair_sent_at, created_at);
-- appGetNotification: WHERE ride_id = ? AND app_sent_at IS NULL ORDER BY created_at (app_handlers.go:686)
ALTER TABLE ride_statuses ADD INDEX idx_ride_statuses_ride_id_app_sent_at_created_at (ride_id, app_sent_at, created_at);
-- internalGetMatching: WHERE chair_id IS NULL ORDER BY created_at (internal_handlers.go:14); nearby rides by chair_id ORDER BY created_at
ALTER TABLE rides ADD INDEX idx_rides_chair_id_created_at (chair_id, created_at);
