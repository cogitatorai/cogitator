package metrics

import (
	"net/http"
	"time"
)

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

// Middleware returns HTTP middleware that records every request's latency and
// status code into the provided ring buffer. It should be applied at the
// outermost layer so that all requests (including those rejected by auth)
// are captured.
func Middleware(ring *Ring) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rec := &statusRecorder{ResponseWriter: w, status: 200}
			next.ServeHTTP(rec, r)
			ring.Record(time.Since(start), rec.status)
		})
	}
}
