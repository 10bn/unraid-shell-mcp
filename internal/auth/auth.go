// Package auth provides bearer-token HTTP middleware.
package auth

import (
	"crypto/subtle"
	"net/http"
	"strings"
)

// Middleware wraps next, requiring "Authorization: Bearer <token>" to match
// the configured token exactly. Responds 401 on any mismatch, including a
// missing header or an empty configured token (never authenticate-by-default).
func Middleware(token string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !validToken(token, r.Header.Get("Authorization")) {
			w.Header().Set("WWW-Authenticate", `Bearer realm="unraid-shell-mcp"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func validToken(configured, header string) bool {
	if configured == "" {
		return false
	}
	const prefix = "Bearer "
	if !strings.HasPrefix(header, prefix) {
		return false
	}
	presented := strings.TrimPrefix(header, prefix)
	return subtle.ConstantTimeCompare([]byte(presented), []byte(configured)) == 1
}
