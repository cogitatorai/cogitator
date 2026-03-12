package drain

import (
	"net/http"
	"strings"
	"sync/atomic"
)

// Manager tracks whether the instance is draining (preparing for shutdown or
// update). Once draining begins, non-essential HTTP requests are rejected with
// 503 so that clients retry against another instance.
type Manager struct {
	draining atomic.Bool
}

// New returns a Manager in the non-draining state.
func New() *Manager {
	return &Manager{}
}

// Start sets the drain flag. The operation is idempotent.
func (m *Manager) Start() {
	m.draining.Store(true)
}

// IsDraining reports whether the instance is draining.
func (m *Manager) IsDraining() bool {
	return m.draining.Load()
}

// Middleware returns HTTP middleware that rejects requests with 503 while
// draining. Internal endpoints (/api/internal/*) and the health check
// (/api/health) are always allowed through so that the orchestrator can
// continue communicating with the instance.
func (m *Manager) Middleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if m.draining.Load() {
				path := r.URL.Path
				if !strings.HasPrefix(path, "/api/internal/") && path != "/api/health" {
					w.Header().Set("Retry-After", "30")
					http.Error(w, "instance is updating, please try again shortly", http.StatusServiceUnavailable)
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}
