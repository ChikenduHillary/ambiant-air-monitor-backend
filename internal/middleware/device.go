package middleware

import "net/http"

// DeviceAuth restricts a route to callers presenting the shared device API
// key via the X-Device-Key header. It fails closed: if no key is configured
// server-side, every request is rejected rather than left open.
func DeviceAuth(apiKey string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if apiKey == "" || r.Header.Get("X-Device-Key") != apiKey {
				http.Error(w, `{"error":"invalid device key"}`, http.StatusUnauthorized)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
