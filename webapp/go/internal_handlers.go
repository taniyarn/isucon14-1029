package main

import (
	"net/http"
)

// matchingCandidate is a free chair with its latest position and model speed.
type matchingCandidate struct {
	ID        string `db:"id"`
	Latitude  int    `db:"latitude"`
	Longitude int    `db:"longitude"`
	Speed     int    `db:"speed"`
}

// このAPIをインスタンス内から一定間隔で叩かせることで、椅子とライドをマッチングさせる
// 1 回の呼び出しで、待っているライド全件を、空いている椅子の中から「乗車位置までの到着が早い順」
// (マンハッタン距離 ÷ モデルの速度) に古いライドから割り当てる。
func internalGetMatching(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	rides := []Ride{}
	if err := db.SelectContext(ctx, &rides, `SELECT * FROM rides WHERE chair_id IS NULL ORDER BY created_at`); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if len(rides) == 0 {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	// free = active, and every ride ever assigned to the chair has had its COMPLETED status sent to the chair
	// (same as the reference's COUNT(chair_sent_at) = 6: statuses are inserted and sent in order).
	// Chairs without any location are not candidates (the reference's nearby-chairs skips them too).
	// Latest position: chair_distances.last_* (one row per chair, maintained by chairPostCoordinate, rebuilt on /initialize)
	// instead of the per-chair ORDER BY created_at DESC LIMIT 1 over chair_locations (151 ms/call in run 135253).
	candidates := []matchingCandidate{}
	if err := db.SelectContext(ctx, &candidates, `
SELECT c.id, cd.last_latitude AS latitude, cd.last_longitude AS longitude, cm.speed
FROM chairs c
JOIN chair_models cm ON cm.name = c.model
JOIN chair_distances cd ON cd.chair_id = c.id
WHERE c.is_active = TRUE
  AND NOT EXISTS (
    SELECT 1 FROM rides r
    WHERE r.chair_id = c.id
      AND NOT EXISTS (
        SELECT 1 FROM ride_statuses rs
        WHERE rs.ride_id = r.id AND rs.status = 'COMPLETED' AND rs.chair_sent_at IS NOT NULL
      )
  )`); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	for _, ride := range rides {
		if len(candidates) == 0 {
			break
		}
		best, bestCost := -1, 0.0
		for i, c := range candidates {
			speed := c.Speed
			if speed <= 0 {
				speed = 1
			}
			cost := float64(calculateDistance(c.Latitude, c.Longitude, ride.PickupLatitude, ride.PickupLongitude)) / float64(speed)
			if best < 0 || cost < bestCost {
				best, bestCost = i, cost
			}
		}
		if _, err := db.ExecContext(ctx, "UPDATE rides SET chair_id = ? WHERE id = ? AND chair_id IS NULL", candidates[best].ID, ride.ID); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		candidates = append(candidates[:best], candidates[best+1:]...)
	}

	w.WriteHeader(http.StatusNoContent)
}
