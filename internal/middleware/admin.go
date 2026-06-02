package middleware

import (
	"database/sql"
	"net/http"
	"strconv"
)

// AdminOnly requires the authenticated user to have role = 'admin'.
// Must be chained after Authenticate.
func AdminOnly(db *sql.DB) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			idStr, _ := r.Context().Value(UserIDKey).(string)
			id, _ := strconv.ParseInt(idStr, 10, 64)

			var role string
			err := db.QueryRowContext(r.Context(), `SELECT role FROM users WHERE id = $1`, id).Scan(&role)
			if err != nil || role != "admin" {
				http.Error(w, `{"error":"forbidden — admin access required"}`, http.StatusForbidden)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
