-- summary of chair_locations for GET /api/owner/chairs (kept up to date by chairPostCoordinate);
-- rebuilt from the initial data on every /initialize
DROP TABLE IF EXISTS chair_distances;
CREATE TABLE chair_distances
(
  chair_id                  VARCHAR(26) NOT NULL,
  total_distance            INTEGER     NOT NULL,
  total_distance_updated_at DATETIME(6) NOT NULL,
  last_latitude             INTEGER     NOT NULL,
  last_longitude            INTEGER     NOT NULL,
  PRIMARY KEY (chair_id)
);

INSERT INTO chair_distances (chair_id, total_distance, total_distance_updated_at, last_latitude, last_longitude)
SELECT chair_id,
       SUM(IFNULL(distance, 0)),
       MAX(created_at),
       SUBSTRING_INDEX(GROUP_CONCAT(latitude ORDER BY created_at DESC, id DESC), ',', 1),
       SUBSTRING_INDEX(GROUP_CONCAT(longitude ORDER BY created_at DESC, id DESC), ',', 1)
FROM (SELECT id,
             chair_id,
             created_at,
             latitude,
             longitude,
             ABS(latitude - LAG(latitude) OVER (PARTITION BY chair_id ORDER BY created_at)) +
             ABS(longitude - LAG(longitude) OVER (PARTITION BY chair_id ORDER BY created_at)) AS distance
      FROM chair_locations) tmp
GROUP BY chair_id;
