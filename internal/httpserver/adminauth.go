package httpserver

import (
	"crypto/subtle"
	"net/http"
	"strings"
)

func RequireAdminToken(adminToken string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		const prefix = "Bearer "
		if !strings.HasPrefix(authHeader, prefix) {
			writeJSONError(w, http.StatusUnauthorized, "unauthorized", "missing or malformed Authorization header")
			return
		}
		provided := strings.TrimPrefix(authHeader, prefix)

		if subtle.ConstantTimeCompare([]byte(provided), []byte(adminToken)) != 1 {
			writeJSONError(w, http.StatusUnauthorized, "unauthorized", "invalid admin token")
			return
		}

		next.ServeHTTP(w, r)
	})
}