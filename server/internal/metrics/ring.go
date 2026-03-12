// Package metrics provides in-memory request telemetry for SaaS health
// monitoring. The ring buffer collects latency and status data for the most
// recent N requests, enabling p95 latency and error rate calculation without
// external dependencies or disk writes.
package metrics

import (
	"math"
	"sort"
	"sync"
	"time"
)

type entry struct {
	latency time.Duration
	status  int
}

// Snapshot holds a point-in-time summary of request metrics.
type Snapshot struct {
	RequestCount int     `json:"request_count"`
	ErrorRate    float64 `json:"error_rate"`
	P95LatencyMs float64 `json:"p95_latency_ms"`
}

// Ring is a fixed-size, thread-safe circular buffer of request entries.
// When full, new entries overwrite the oldest. All operations are O(1)
// except Snapshot which is O(n log n) due to percentile sorting.
type Ring struct {
	mu      sync.Mutex
	entries []entry
	pos     int
	full    bool
	size    int
}

// NewRing creates a ring buffer that retains the last size requests.
func NewRing(size int) *Ring {
	return &Ring{
		entries: make([]entry, size),
		size:    size,
	}
}

// Record adds a request observation to the buffer. Thread-safe.
func (r *Ring) Record(latency time.Duration, statusCode int) {
	r.mu.Lock()
	r.entries[r.pos] = entry{latency: latency, status: statusCode}
	r.pos++
	if r.pos >= r.size {
		r.pos = 0
		r.full = true
	}
	r.mu.Unlock()
}

// Snapshot returns a point-in-time summary of all entries currently in the
// buffer. Returns zero values when the buffer is empty.
func (r *Ring) Snapshot() Snapshot {
	r.mu.Lock()
	count := r.pos
	if r.full {
		count = r.size
	}
	if count == 0 {
		r.mu.Unlock()
		return Snapshot{}
	}

	latencies := make([]float64, count)
	errors := 0
	for i := 0; i < count; i++ {
		e := r.entries[i]
		latencies[i] = float64(e.latency) / float64(time.Millisecond)
		if e.status >= 500 {
			errors++
		}
	}
	r.mu.Unlock()

	sort.Float64s(latencies)
	p95Idx := int(math.Ceil(float64(count)*0.95)) - 1
	if p95Idx < 0 {
		p95Idx = 0
	}

	return Snapshot{
		RequestCount: count,
		ErrorRate:    float64(errors) / float64(count),
		P95LatencyMs: latencies[p95Idx],
	}
}
