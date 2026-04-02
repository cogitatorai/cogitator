package api

import (
	"net"
	"net/http"
)

// flyReplayMiddleware checks if the incoming request's Host matches the
// expected hostname for this tenant. If not, it responds with the
// fly-replay header so Fly's proxy retries the request on another instance.
// This is required because all tenant machines share one Fly app and Fly
// cannot route by subdomain without application-level help.
func flyReplayMiddleware(expectedHost string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := r.Host
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}
		if host != expectedHost {
			// Fly's proxy retries the request on another instance when it
			// sees any fly-replay header. "elsewhere=true" is not a documented
			// directive but is treated as a generic retry signal.
			w.Header().Set("fly-replay", "elsewhere=true")
			w.WriteHeader(http.StatusConflict)
			return
		}
		next.ServeHTTP(w, r)
	})
}
