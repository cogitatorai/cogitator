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
		// Strip port if present (e.g. "starter.cogitator.cloud:443").
		if i := len(host) - 1; i > 0 && host[i] >= '0' && host[i] <= '9' {
			if h, _, err := net.SplitHostPort(host); err == nil {
				host = h
			}
		}
		if host != expectedHost {
			w.Header().Set("fly-replay", "elsewhere=true")
			w.WriteHeader(http.StatusConflict)
			return
		}
		next.ServeHTTP(w, r)
	})
}
