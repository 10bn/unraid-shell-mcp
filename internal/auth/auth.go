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

// PathTokenMiddleware authenticates requests using a token embedded directly
// in the URL path (via r.PathValue("token")) instead of an Authorization
// header. It exists for MCP clients whose UI only accepts a URL, with no way
// to configure a custom header — the token substitutes for the header in
// that case. It carries the same weight as the bearer token (in fact it's
// the same secret) and is checked with the same constant-time comparison;
// the difference is purely which part of the request carries it. Because
// URL paths are more likely than headers to end up in proxy/server access
// logs or browser history, prefer the header-based Middleware when the
// client supports it.
func PathTokenMiddleware(token string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !validPathToken(token, r.PathValue("token")) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func validPathToken(configured, presented string) bool {
	if configured == "" || presented == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(presented), []byte(configured)) == 1
}
