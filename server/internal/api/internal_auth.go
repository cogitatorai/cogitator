package api

import (
	"crypto/subtle"
	"net/http"
)

// internalAuth returns middleware that gates access using a shared secret.
// The orchestrator must send the secret via the X-Internal-Secret header.
// Comparison uses constant-time equality to prevent timing attacks.
func internalAuth(secret string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			provided := r.Header.Get("X-Internal-Secret")
			if subtle.ConstantTimeCompare([]byte(provided), []byte(secret)) != 1 {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
