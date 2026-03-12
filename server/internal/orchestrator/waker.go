package orchestrator

import (
	"fmt"
	"log"
	"net/http"
	"time"
)

// Waker is a background loop that wakes tenant machines whose scheduled
// wake time has arrived. It queries the wake_schedule table every minute
// and hits each due tenant's health endpoint (which triggers Fly auto-start).
type Waker struct {
	db         *OrchestratorDB
	client     *http.Client
	baseURLFmt string // default: "https://%s.cogitator.cloud"
	retryDelay time.Duration
	stop       chan struct{}
	done       chan struct{}
}

// NewWaker creates a Waker backed by the given database.
func NewWaker(db *OrchestratorDB) *Waker {
	return &Waker{
		db:         db,
		client:     &http.Client{Timeout: 10 * time.Second},
		baseURLFmt: "https://%s.cogitator.cloud",
		retryDelay: 5 * time.Second,
		stop:       make(chan struct{}),
		done:       make(chan struct{}),
	}
}

// Start launches the waker loop in a background goroutine.
func (w *Waker) Start() {
	go w.run()
}

// Stop signals the waker loop to exit and waits for it to finish.
func (w *Waker) Stop() {
	close(w.stop)
	<-w.done
}

func (w *Waker) run() {
	defer close(w.done)

	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	// Run immediately on start, then on each tick.
	w.tick()

	for {
		select {
		case <-w.stop:
			return
		case <-ticker.C:
			w.tick()
		}
	}
}

type wakeTarget struct {
	tenantID string
	slug     string
}

func (w *Waker) tick() {
	rows, err := w.db.db.Query(
		`SELECT ws.tenant_id, t.slug
		 FROM wake_schedule ws
		 JOIN tenants t ON t.id = ws.tenant_id
		 WHERE ws.wake_at <= datetime('now')`,
	)
	if err != nil {
		log.Printf("waker: query wake_schedule: %v", err)
		return
	}
	defer rows.Close()

	var targets []wakeTarget
	for rows.Next() {
		var wt wakeTarget
		if err := rows.Scan(&wt.tenantID, &wt.slug); err != nil {
			log.Printf("waker: scan row: %v", err)
			continue
		}
		targets = append(targets, wt)
	}
	if err := rows.Err(); err != nil {
		log.Printf("waker: iterate rows: %v", err)
		return
	}

	for _, target := range targets {
		w.wake(target)
	}
}

func (w *Waker) wake(target wakeTarget) {
	url := fmt.Sprintf(w.baseURLFmt, target.slug) + "/api/health"

	const maxAttempts = 3
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		resp, err := w.client.Get(url)
		if err != nil {
			log.Printf("waker: %s (attempt %d/%d): %v", target.slug, attempt, maxAttempts, err)
			if attempt < maxAttempts {
				time.Sleep(w.retryDelay)
			}
			continue
		}
		resp.Body.Close()

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			if _, err := w.db.db.Exec(
				`DELETE FROM wake_schedule WHERE tenant_id = ?`, target.tenantID,
			); err != nil {
				log.Printf("waker: delete wake_schedule for %s: %v", target.slug, err)
			}
			return
		}

		log.Printf("waker: %s (attempt %d/%d): status %d", target.slug, attempt, maxAttempts, resp.StatusCode)
		if attempt < maxAttempts {
			time.Sleep(w.retryDelay)
		}
	}

	log.Printf("waker: %s failed after %d attempts, will retry next tick", target.slug, maxAttempts)
}
