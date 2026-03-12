package ratelimit

import (
	"net"
	"net/http"
	"sync"
	"time"
)

type bucket struct {
	tokens    float64
	lastCheck time.Time
	mu        sync.Mutex
}

// Limiter implements a per-IP token bucket rate limiter.
// Each IP gets its own bucket that refills at a steady rate
// up to a maximum burst size.
type Limiter struct {
	rate    float64  // tokens per second
	burst   int      // max tokens (also the initial token count)
	buckets sync.Map // map[string]*bucket
	stop    chan struct{}
}

// New creates a Limiter that allows requestsPerMinute sustained requests
// per IP, with a burst capacity equal to requestsPerMinute.
// Call Stop() when the limiter is no longer needed to release resources.
func New(requestsPerMinute int) *Limiter {
	l := &Limiter{
		rate:  float64(requestsPerMinute) / 60.0,
		burst: requestsPerMinute,
		stop:  make(chan struct{}),
	}
	go l.cleanupLoop()
	return l
}

// Stop shuts down the background cleanup goroutine.
func (l *Limiter) Stop() {
	close(l.stop)
}

// cleanupLoop periodically removes stale buckets that have not been
// accessed in the last 5 minutes.
func (l *Limiter) cleanupLoop() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			l.cleanup()
		case <-l.stop:
			return
		}
	}
}

// cleanup removes buckets that have not been checked in the last 5 minutes.
func (l *Limiter) cleanup() {
	threshold := time.Now().Add(-5 * time.Minute)
	l.buckets.Range(func(key, value any) bool {
		b := value.(*bucket)
		b.mu.Lock()
		stale := b.lastCheck.Before(threshold)
		b.mu.Unlock()
		if stale {
			l.buckets.Delete(key)
		}
		return true
	})
}

// Allow consumes one token for the given IP and reports whether the
// request should be permitted.
func (l *Limiter) Allow(ip string) bool {
	val, _ := l.buckets.LoadOrStore(ip, &bucket{
		tokens:    float64(l.burst),
		lastCheck: time.Now(),
	})
	b := val.(*bucket)

	b.mu.Lock()
	defer b.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(b.lastCheck).Seconds()
	b.lastCheck = now

	b.tokens += elapsed * l.rate
	if b.tokens > float64(l.burst) {
		b.tokens = float64(l.burst)
	}

	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// Middleware returns an HTTP middleware that rejects requests with
// 429 Too Many Requests when the per-IP rate limit is exceeded.
func (l *Limiter) Middleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip, _, _ := net.SplitHostPort(r.RemoteAddr)
			if ip == "" {
				ip = r.RemoteAddr
			}

			if !l.Allow(ip) {
				w.Header().Set("Retry-After", "1")
				http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
