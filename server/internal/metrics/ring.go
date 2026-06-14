// Package metrics provides in-memory request telemetry, collected in every
// build mode. The ring buffer holds latency, status, and route data for the
// most recent N requests, enabling p95 latency and error rate calculation
// (global and per-route) without external dependencies or disk writes. In
// SaaS mode the heartbeat pushes a snapshot to the orchestrator; self-hosted
// and desktop builds surface it through /api/status.
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
	route   string // ServeMux pattern, e.g. "GET /api/chat"; "unmatched" when nothing matched
}

// RouteStats summarizes requests for a single route pattern.
type RouteStats struct {
	RequestCount int     `json:"request_count"`
	Count5xx     int     `json:"count_5xx"`
	P95LatencyMs float64 `json:"p95_latency_ms"`
}

// Snapshot holds a point-in-time summary of request metrics.
type Snapshot struct {
	RequestCount int                   `json:"request_count"`
	ErrorRate    float64               `json:"error_rate"`
	P95LatencyMs float64               `json:"p95_latency_ms"`
	Count2xx     int                   `json:"count_2xx"`
	Count4xx     int                   `json:"count_4xx"`
	Count5xx     int                   `json:"count_5xx"`
	Routes       map[string]RouteStats `json:"routes,omitempty"`
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
// route is the matched ServeMux pattern ("GET /api/chat"); empty is
// bucketed as "unmatched".
func (r *Ring) Record(latency time.Duration, statusCode int, route string) {
	if route == "" {
		route = "unmatched"
	}
	r.mu.Lock()
	r.entries[r.pos] = entry{latency: latency, status: statusCode, route: route}
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
	routeLat := make(map[string][]float64)
	route5xx := make(map[string]int)
	var count2xx, count4xx, count5xx int
	for i := 0; i < count; i++ {
		e := r.entries[i]
		ms := float64(e.latency) / float64(time.Millisecond)
		latencies[i] = ms
		routeLat[e.route] = append(routeLat[e.route], ms)
		switch {
		case e.status >= 500:
			count5xx++
			route5xx[e.route]++
		case e.status >= 400:
			count4xx++
		case e.status >= 200 && e.status < 300:
			count2xx++
		}
	}
	r.mu.Unlock()

	sort.Float64s(latencies)
	routes := make(map[string]RouteStats, len(routeLat))
	for route, lats := range routeLat {
		sort.Float64s(lats)
		routes[route] = RouteStats{
			RequestCount: len(lats),
			Count5xx:     route5xx[route],
			P95LatencyMs: lats[p95Index(len(lats))],
		}
	}

	return Snapshot{
		RequestCount: count,
		ErrorRate:    float64(count5xx) / float64(count),
		P95LatencyMs: latencies[p95Index(count)],
		Count2xx:     count2xx,
		Count4xx:     count4xx,
		Count5xx:     count5xx,
		Routes:       routes,
	}
}

// p95Index returns the index of the 95th percentile in a sorted slice of n items.
func p95Index(n int) int {
	idx := int(math.Ceil(float64(n)*0.95)) - 1
	if idx < 0 {
		return 0
	}
	return idx
}
