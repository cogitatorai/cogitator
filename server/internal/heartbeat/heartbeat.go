// Package heartbeat pushes periodic metrics snapshots from a SaaS tenant
// machine to the orchestrator. It runs a background goroutine that fires
// every Interval (default 5 minutes), serialises the current metrics ring
// buffer snapshot, and POSTs it to the orchestrator's internal heartbeat
// endpoint. The goroutine is started with Start and cleanly shut down
// with Stop.
package heartbeat

import (
	"bytes"
	"encoding/json"
	"net/http"
	"time"

	"github.com/cogitatorai/cogitator/server/internal/metrics"
)

// Config holds everything the heartbeat goroutine needs to reach the
// orchestrator and identify itself.
type Config struct {
	OrchestratorURL string
	TenantID        string
	InternalSecret  string
	Ring            *metrics.Ring
	Interval        time.Duration
}

// Heartbeat manages the background push loop.
type Heartbeat struct {
	cfg    Config
	stop   chan struct{}
	done   chan struct{}
	client *http.Client
}

// New creates a Heartbeat ready to Start. If Interval is zero it defaults
// to 5 minutes.
func New(cfg Config) *Heartbeat {
	if cfg.Interval == 0 {
		cfg.Interval = 5 * time.Minute
	}
	return &Heartbeat{
		cfg:    cfg,
		stop:   make(chan struct{}),
		done:   make(chan struct{}),
		client: &http.Client{Timeout: 10 * time.Second},
	}
}

// Start launches the background goroutine. It sends one heartbeat
// immediately, then repeats on every tick.
func (h *Heartbeat) Start() {
	go h.run()
}

// Stop signals the goroutine to exit and blocks until it has finished.
func (h *Heartbeat) Stop() {
	close(h.stop)
	<-h.done
}

func (h *Heartbeat) run() {
	defer close(h.done)
	ticker := time.NewTicker(h.cfg.Interval)
	defer ticker.Stop()

	h.send() // send immediately on start
	for {
		select {
		case <-ticker.C:
			h.send()
		case <-h.stop:
			return
		}
	}
}

func (h *Heartbeat) send() {
	snap := h.cfg.Ring.Snapshot()
	payload := map[string]any{
		"tenant_id":      h.cfg.TenantID,
		"request_count":  snap.RequestCount,
		"error_rate":     snap.ErrorRate,
		"p95_latency_ms": snap.P95LatencyMs,
		"count_2xx":      snap.Count2xx,
		"count_4xx":      snap.Count4xx,
		"count_5xx":      snap.Count5xx,
	}

	body, _ := json.Marshal(payload)
	req, err := http.NewRequest("POST", h.cfg.OrchestratorURL+"/api/internal/heartbeat", bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Internal-Secret", h.cfg.InternalSecret)

	resp, err := h.client.Do(req)
	if err != nil {
		return
	}
	resp.Body.Close()
}
