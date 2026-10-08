package middleware

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"net/http"
)

type deviceContextKey string

const DeviceIDKey deviceContextKey = "deviceID"
const DeviceUserIDKey deviceContextKey = "deviceUserID"

// DeviceAuth looks up which device the X-Device-Key header belongs to and
// attaches that device's id and owning user's id to the request context.
// Each device has its own key (see Handler.CreateDevice) instead of one
// shared secret, so a reading can be attributed to the right user's account.
func DeviceAuth(db *sql.DB) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := r.Header.Get("X-Device-Key")
			if key == "" {
				http.Error(w, `{"error":"invalid device key"}`, http.StatusUnauthorized)
				return
			}
			sum := sha256.Sum256([]byte(key))
			keyHash := hex.EncodeToString(sum[:])

			var deviceID, userID int64
			err := db.QueryRowContext(r.Context(),
				`SELECT id, user_id FROM devices WHERE key_hash = $1`, keyHash,
			).Scan(&deviceID, &userID)
			if err != nil {
				http.Error(w, `{"error":"invalid device key"}`, http.StatusUnauthorized)
				return
			}

			db.ExecContext(r.Context(), `UPDATE devices SET last_seen_at = NOW() WHERE id = $1`, deviceID)

			ctx := context.WithValue(r.Context(), DeviceIDKey, deviceID)
			ctx = context.WithValue(ctx, DeviceUserIDKey, userID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
