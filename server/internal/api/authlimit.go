package api

import (
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Brute-force protection for the authentication endpoints.
//
// We deliberately do NOT reuse internal/ratelimit (a token-bucket limiter):
// token buckets refill continuously and model "sustained requests per second",
// which does not capture the semantics we need here:
//   - count CONSECUTIVE FAILURES (not all requests),
//   - RESET the counter on a successful login,
//   - apply a fixed LOCKOUT WINDOW once a threshold is crossed.
// A small dedicated failure tracker expresses those rules directly.
//
// The tracker is bounded the same way the rest of this hardening branch fixes
// unbounded maps: entries expire by TTL via on-access pruning (no background
// goroutine, so nothing to wire into the shutdown path) and a hard entry cap
// evicts the oldest entry when exceeded.

// Auth brute-force tuning. Package-level constants (not config) to keep scope
// tight; config plumbing is a follow-up if wanted.
const (
	// loginAccountWindow is the lockout window for a single account after too
	// many consecutive failed logins.
	loginAccountWindow = 15 * time.Minute
	// loginAccountMaxFailures is the number of consecutive failed logins for a
	// single account (lowercased email) before further attempts are rejected.
	loginAccountMaxFailures = 5

	// loginIPWindow is the rolling window over which per-IP login failures are
	// counted.
	loginIPWindow = 15 * time.Minute
	// loginIPMaxFailures is the number of login failures from a single source
	// IP within loginIPWindow before further attempts are rejected.
	loginIPMaxFailures = 20

	// registerIPWindow / registerIPMaxFailures bound failed registrations per
	// source IP.
	registerIPWindow      = 15 * time.Minute
	registerIPMaxFailures = 10

	// refreshIPWindow / refreshIPMaxFailures bound failed token refreshes per
	// source IP.
	refreshIPWindow      = 15 * time.Minute
	refreshIPMaxFailures = 30

	// authTrackerMaxEntries caps the number of tracked keys to bound memory.
	// When exceeded, the oldest entry (by last update) is evicted. This is a
	// safety valve against a flood of distinct keys (e.g. spoofed accounts);
	// TTL pruning handles the common case.
	authTrackerMaxEntries = 10000
)

// authFailMessage is the generic 429 body. It MUST NOT reveal whether an
// account exists, so it is identical for every key type.
const authFailMessage = "too many attempts, try again later"

// failEntry tracks consecutive/windowed failures for a single key.
type failEntry struct {
	count int
	first time.Time // start of the current window
	last  time.Time // most recent update (used for LRU eviction)
}

// failTracker counts authentication failures per key with TTL-based eviction
// and a hard entry cap. It is safe for concurrent use.
//
// Cross-key pruning correctness assumes all callers use the SAME window: prune
// runs against the calling path's window over the whole map, so a shorter
// window on one path would prematurely evict another path's live entries. If
// per-path windows ever diverge, store the window on each entry and prune
// against it.
type failTracker struct {
	mu      sync.Mutex
	entries map[string]*failEntry
	max     int
	now     func() time.Time // injectable clock for tests
}

func newFailTracker() *failTracker {
	return &failTracker{
		entries: make(map[string]*failEntry),
		max:     authTrackerMaxEntries,
		now:     time.Now,
	}
}

// blocked reports whether key is currently locked out for the given threshold
// and window. It prunes stale state for the key on access. retryAfter is the
// number of seconds until the lockout expires (>=1 when blocked, 0 otherwise).
func (t *failTracker) blocked(key string, threshold int, window time.Duration) (bool, int) {
	now := t.now()
	t.mu.Lock()
	defer t.mu.Unlock()

	t.pruneLocked(now, window)

	e, ok := t.entries[key]
	if !ok {
		return false, 0
	}
	// Window expired: the entry is stale, treat as not blocked.
	if now.Sub(e.first) >= window {
		delete(t.entries, key)
		return false, 0
	}
	if e.count < threshold {
		return false, 0
	}
	// Locked. Lockout expires at first+window.
	remaining := window - now.Sub(e.first)
	secs := int(remaining.Seconds())
	if secs < 1 {
		secs = 1
	}
	return true, secs
}

// fail records a failure for key within the given window and returns the new
// failure count.
func (t *failTracker) fail(key string, window time.Duration) int {
	now := t.now()
	t.mu.Lock()
	defer t.mu.Unlock()

	t.pruneLocked(now, window)

	e, ok := t.entries[key]
	if !ok || now.Sub(e.first) >= window {
		e = &failEntry{count: 0, first: now, last: now}
		t.entries[key] = e
		t.evictLocked()
	}
	e.count++
	e.last = now
	return e.count
}

// reset clears any tracked failures for key (called after a successful login
// to reset the per-account counter).
func (t *failTracker) reset(key string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.entries, key)
}

// pruneLocked removes entries whose window has elapsed. Caller must hold mu.
// window is the largest window we care about for the calling path; using the
// per-call window keeps pruning cheap and correct for that path.
func (t *failTracker) pruneLocked(now time.Time, window time.Duration) {
	for k, e := range t.entries {
		if now.Sub(e.first) >= window {
			delete(t.entries, k)
		}
	}
}

// evictLocked enforces the hard entry cap by removing the oldest entry (by
// last update) when the map exceeds max. Caller must hold mu.
func (t *failTracker) evictLocked() {
	if len(t.entries) <= t.max {
		return
	}
	var oldestKey string
	var oldest time.Time
	first := true
	for k, e := range t.entries {
		if first || e.last.Before(oldest) {
			oldestKey = k
			oldest = e.last
			first = false
		}
	}
	delete(t.entries, oldestKey)
}

// clientIP extracts the source IP for rate-limiting.
//
// By default (trustProxy=false) it uses only the host part of r.RemoteAddr;
// client-supplied headers are ignored because they are trivially spoofable.
//
// When trustProxy is set (the server runs behind a trusted reverse proxy, e.g.
// Fly.io in SaaS mode which terminates TLS) we must derive the IP that the
// trusted proxy itself observed, NOT any value the client could have injected.
// The trust model:
//
//   - Fly-Client-IP: Fly sets this to the real client IP. A client cannot forge
//     it through Fly's proxy (Fly overwrites it), so it is preferred when present.
//   - X-Forwarded-For: Fly APPENDS the observed client IP at the RIGHTMOST
//     position and preserves any client-sent XFF on the left. The leftmost hops
//     are therefore attacker-controlled; only the rightmost entry was added by
//     the trusted proxy. We take the rightmost entry, never the leftmost (which
//     would let an attacker rotate fabricated IPs to bypass per-IP limits or
//     poison a victim IP's failure counter).
//   - RemoteAddr: final fallback when no trusted header is present.
func clientIP(r *http.Request, trustProxy bool) string {
	if trustProxy {
		// Fly sets this to the real client IP and overwrites client-supplied
		// values, so it cannot be spoofed through the proxy.
		if fly := strings.TrimSpace(r.Header.Get("Fly-Client-IP")); fly != "" {
			return fly
		}
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			// The rightmost hop is the one appended by the trusted proxy;
			// everything to its left is client-supplied and spoofable.
			if i := strings.LastIndexByte(xff, ','); i >= 0 {
				xff = xff[i+1:]
			}
			if ip := strings.TrimSpace(xff); ip != "" {
				return ip
			}
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil || host == "" {
		return r.RemoteAddr
	}
	return host
}

// writeAuthThrottled writes a generic 429 response with a Retry-After header.
// The message is intentionally generic and identical across all key types so
// it never reveals whether an account exists.
func writeAuthThrottled(w http.ResponseWriter, retryAfterSecs int) {
	if retryAfterSecs < 1 {
		retryAfterSecs = 1
	}
	w.Header().Set("Retry-After", strconv.Itoa(retryAfterSecs))
	writeError(w, http.StatusTooManyRequests, authFailMessage)
}
