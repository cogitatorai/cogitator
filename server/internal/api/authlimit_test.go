package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/cogitatorai/cogitator/server/internal/user"
)

// --- failTracker unit tests (use injected clock, no sleeping) ---

func TestFailTracker_LockoutAfterThreshold(t *testing.T) {
	tr := newFailTracker()
	now := time.Unix(1000, 0)
	tr.now = func() time.Time { return now }

	const threshold = 3
	const window = 10 * time.Minute

	for i := 0; i < threshold-1; i++ {
		tr.fail("k", window)
		if blocked, _ := tr.blocked("k", threshold, window); blocked {
			t.Fatalf("should not be blocked after %d failures", i+1)
		}
	}
	tr.fail("k", window) // reaches threshold
	blocked, retry := tr.blocked("k", threshold, window)
	if !blocked {
		t.Fatal("expected blocked after reaching threshold")
	}
	if retry < 1 {
		t.Fatalf("expected retry-after >= 1, got %d", retry)
	}
}

func TestFailTracker_WindowExpiry(t *testing.T) {
	tr := newFailTracker()
	now := time.Unix(1000, 0)
	tr.now = func() time.Time { return now }

	const threshold = 2
	const window = 10 * time.Minute

	tr.fail("k", window)
	tr.fail("k", window)
	if blocked, _ := tr.blocked("k", threshold, window); !blocked {
		t.Fatal("expected blocked")
	}

	// Advance past the window: lockout expires.
	now = now.Add(window + time.Second)
	if blocked, _ := tr.blocked("k", threshold, window); blocked {
		t.Fatal("expected lockout to expire after window")
	}
}

func TestFailTracker_ResetClearsCounter(t *testing.T) {
	tr := newFailTracker()
	now := time.Unix(1000, 0)
	tr.now = func() time.Time { return now }

	const window = 10 * time.Minute
	tr.fail("k", window)
	tr.fail("k", window)
	tr.reset("k")
	if blocked, _ := tr.blocked("k", 2, window); blocked {
		t.Fatal("expected not blocked after reset")
	}
	// One more failure should not immediately re-lock at threshold 2.
	if c := tr.fail("k", window); c != 1 {
		t.Fatalf("expected count 1 after reset, got %d", c)
	}
}

func TestFailTracker_HardCapEvictsOldest(t *testing.T) {
	tr := newFailTracker()
	tr.max = 3
	base := time.Unix(1000, 0)
	cur := base
	tr.now = func() time.Time { return cur }

	const window = time.Hour

	// Insert keys at increasing times so "k0" is the oldest.
	for i := 0; i < 3; i++ {
		cur = base.Add(time.Duration(i) * time.Second)
		tr.fail("k"+strconv.Itoa(i), window)
	}
	// Insert a 4th; cap is 3 so the oldest (k0) is evicted.
	cur = base.Add(10 * time.Second)
	tr.fail("k3", window)

	tr.mu.Lock()
	defer tr.mu.Unlock()
	if len(tr.entries) != 3 {
		t.Fatalf("expected 3 entries after eviction, got %d", len(tr.entries))
	}
	if _, ok := tr.entries["k0"]; ok {
		t.Fatal("expected oldest entry k0 to be evicted")
	}
	if _, ok := tr.entries["k3"]; !ok {
		t.Fatal("expected newest entry k3 to remain")
	}
}

func TestFailTracker_PruneRemovesStale(t *testing.T) {
	tr := newFailTracker()
	now := time.Unix(1000, 0)
	tr.now = func() time.Time { return now }

	const window = 5 * time.Minute
	tr.fail("a", window)

	// Advance past window then touch a different key; prune should drop "a".
	now = now.Add(window + time.Second)
	tr.fail("b", window)

	tr.mu.Lock()
	defer tr.mu.Unlock()
	if _, ok := tr.entries["a"]; ok {
		t.Fatal("expected stale entry a to be pruned")
	}
	if _, ok := tr.entries["b"]; !ok {
		t.Fatal("expected fresh entry b to remain")
	}
}

func TestClientIP(t *testing.T) {
	t.Run("remote addr host part by default", func(t *testing.T) {
		r := httptest.NewRequest("POST", "/", nil)
		r.RemoteAddr = "203.0.113.5:54321"
		r.Header.Set("X-Forwarded-For", "1.2.3.4")
		if got := clientIP(r, false); got != "203.0.113.5" {
			t.Errorf("got %q, want 203.0.113.5 (XFF must be ignored when not trusting proxy)", got)
		}
	})
	t.Run("trusts rightmost XFF hop when behind proxy", func(t *testing.T) {
		// The leftmost hops are client-supplied and spoofable; the trusted
		// proxy appends the real client IP at the rightmost position.
		r := httptest.NewRequest("POST", "/", nil)
		r.RemoteAddr = "10.0.0.1:1111"
		r.Header.Set("X-Forwarded-For", "spoofed, 5.6.7.8")
		if got := clientIP(r, true); got != "5.6.7.8" {
			t.Errorf("got %q, want 5.6.7.8 (rightmost XFF hop, appended by trusted proxy)", got)
		}
	})
	t.Run("prefers Fly-Client-IP when behind proxy", func(t *testing.T) {
		r := httptest.NewRequest("POST", "/", nil)
		r.RemoteAddr = "10.0.0.1:1111"
		r.Header.Set("Fly-Client-IP", "9.9.9.9")
		r.Header.Set("X-Forwarded-For", "1.2.3.4, 9.9.9.9")
		if got := clientIP(r, true); got != "9.9.9.9" {
			t.Errorf("got %q, want 9.9.9.9 (Fly-Client-IP)", got)
		}
	})
	t.Run("Fly-Client-IP resists spoofed XFF", func(t *testing.T) {
		// An attacker controls the client-sent X-Forwarded-For but cannot
		// forge Fly-Client-IP through Fly's proxy. Fly-Client-IP must win.
		r := httptest.NewRequest("POST", "/", nil)
		r.RemoteAddr = "10.0.0.1:1111"
		r.Header.Set("X-Forwarded-For", "1.1.1.1, 2.2.2.2, 3.3.3.3")
		r.Header.Set("Fly-Client-IP", "8.8.8.8")
		if got := clientIP(r, true); got != "8.8.8.8" {
			t.Errorf("got %q, want 8.8.8.8 (Fly-Client-IP beats spoofed XFF)", got)
		}
	})
	t.Run("Fly-Client-IP ignored when not trusting proxy", func(t *testing.T) {
		r := httptest.NewRequest("POST", "/", nil)
		r.RemoteAddr = "203.0.113.5:54321"
		r.Header.Set("Fly-Client-IP", "9.9.9.9")
		if got := clientIP(r, false); got != "203.0.113.5" {
			t.Errorf("got %q, want 203.0.113.5 (Fly-Client-IP must be ignored when not trusting proxy)", got)
		}
	})
	t.Run("falls back to remote addr when no XFF", func(t *testing.T) {
		r := httptest.NewRequest("POST", "/", nil)
		r.RemoteAddr = "10.0.0.1:1111"
		if got := clientIP(r, true); got != "10.0.0.1" {
			t.Errorf("got %q, want 10.0.0.1", got)
		}
	})
}

// --- HTTP-level tests ---

// postJSON issues a POST with a JSON body from a given source IP and decodes
// the recorder.
func postJSON(t *testing.T, router *Router, path, ip string, body any) *httptest.ResponseRecorder {
	t.Helper()
	payload, _ := json.Marshal(body)
	req := httptest.NewRequest("POST", path, bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = ip + ":40000"
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

func decodeErr(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	var m map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &m); err != nil {
		t.Fatalf("decode error body %q: %v", w.Body.String(), err)
	}
	return m["error"]
}

func TestLogin_AccountLockout(t *testing.T) {
	router, store := setupAuthRouter(t)
	createTestUser(t, store, "alice@example.com", "secret123", user.RoleUser)

	// Fail loginAccountMaxFailures times from DISTINCT IPs so the IP limiter
	// does not trip first; only the account counter should lock.
	for i := 0; i < loginAccountMaxFailures; i++ {
		ip := "203.0.113." + strconv.Itoa(i+1)
		w := postJSON(t, router, "/api/auth/login", ip, loginRequest{Email: "alice@example.com", Password: "wrong"})
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d: expected 401, got %d", i, w.Code)
		}
	}

	// Next attempt (even from a fresh IP, even with the correct password) is
	// rejected because the account is locked.
	w := postJSON(t, router, "/api/auth/login", "203.0.113.250", loginRequest{Email: "alice@example.com", Password: "secret123"})
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d: %s", w.Code, w.Body.String())
	}
	if msg := decodeErr(t, w); msg != authFailMessage {
		t.Errorf("expected generic message %q, got %q", authFailMessage, msg)
	}
	if ra := w.Header().Get("Retry-After"); ra == "" {
		t.Error("expected Retry-After header")
	} else if n, err := strconv.Atoi(ra); err != nil || n < 1 {
		t.Errorf("Retry-After should be a positive int, got %q", ra)
	}
}

func TestLogin_AccountLockoutGenericMessage(t *testing.T) {
	router, store := setupAuthRouter(t)
	createTestUser(t, store, "real@example.com", "secret123", user.RoleUser)

	// Lock a real account.
	for i := 0; i < loginAccountMaxFailures; i++ {
		postJSON(t, router, "/api/auth/login", "198.51.100."+strconv.Itoa(i+1), loginRequest{Email: "real@example.com", Password: "wrong"})
	}
	realW := postJSON(t, router, "/api/auth/login", "198.51.100.250", loginRequest{Email: "real@example.com", Password: "wrong"})

	// Lock a NON-existent account.
	for i := 0; i < loginAccountMaxFailures; i++ {
		postJSON(t, router, "/api/auth/login", "198.51.100."+strconv.Itoa(i+1), loginRequest{Email: "ghost@example.com", Password: "wrong"})
	}
	ghostW := postJSON(t, router, "/api/auth/login", "198.51.100.251", loginRequest{Email: "ghost@example.com", Password: "wrong"})

	if realW.Code != http.StatusTooManyRequests || ghostW.Code != http.StatusTooManyRequests {
		t.Fatalf("expected both locked (429), got real=%d ghost=%d", realW.Code, ghostW.Code)
	}
	if decodeErr(t, realW) != decodeErr(t, ghostW) {
		t.Error("429 message differs between existing and non-existing account; leaks account existence")
	}
}

func TestLogin_SuccessResetsAccountCounter(t *testing.T) {
	router, store := setupAuthRouter(t)
	createTestUser(t, store, "alice@example.com", "secret123", user.RoleUser)

	// Fail one short of the account threshold (distinct IPs to avoid IP lock).
	for i := 0; i < loginAccountMaxFailures-1; i++ {
		postJSON(t, router, "/api/auth/login", "203.0.113."+strconv.Itoa(i+1), loginRequest{Email: "alice@example.com", Password: "wrong"})
	}

	// A successful login resets the account counter.
	okW := postJSON(t, router, "/api/auth/login", "203.0.113.100", loginRequest{Email: "alice@example.com", Password: "secret123"})
	if okW.Code != http.StatusOK {
		t.Fatalf("expected 200 on correct login, got %d", okW.Code)
	}

	// Now we can fail (threshold-1) more times without being locked.
	for i := 0; i < loginAccountMaxFailures-1; i++ {
		w := postJSON(t, router, "/api/auth/login", "203.0.113."+strconv.Itoa(i+1), loginRequest{Email: "alice@example.com", Password: "wrong"})
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("post-reset attempt %d: expected 401 (counter was reset), got %d", i, w.Code)
		}
	}
}

func TestLogin_IPLockout(t *testing.T) {
	router, store := setupAuthRouter(t)
	// Create enough distinct accounts so the per-account limiter never trips;
	// only the per-IP counter should lock.
	for i := 0; i < loginIPMaxFailures+1; i++ {
		createTestUser(t, store, "u"+strconv.Itoa(i)+"@example.com", "secret123", user.RoleUser)
	}

	const ip = "192.0.2.7"
	for i := 0; i < loginIPMaxFailures; i++ {
		email := "u" + strconv.Itoa(i) + "@example.com"
		w := postJSON(t, router, "/api/auth/login", ip, loginRequest{Email: email, Password: "wrong"})
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d: expected 401, got %d", i, w.Code)
		}
	}
	// Threshold reached for this IP: another account from same IP is blocked.
	w := postJSON(t, router, "/api/auth/login", ip, loginRequest{Email: "u" + strconv.Itoa(loginIPMaxFailures) + "@example.com", Password: "wrong"})
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 from IP lockout, got %d", w.Code)
	}

	// A different IP is unaffected.
	w2 := postJSON(t, router, "/api/auth/login", "192.0.2.8", loginRequest{Email: "u0@example.com", Password: "wrong"})
	if w2.Code != http.StatusUnauthorized {
		t.Fatalf("different IP should not be blocked, got %d", w2.Code)
	}
}

func TestLogin_SuccessDoesNotResetIPCounter(t *testing.T) {
	router, store := setupAuthRouter(t)
	createTestUser(t, store, "good@example.com", "secret123", user.RoleUser)
	for i := 0; i < loginIPMaxFailures; i++ {
		createTestUser(t, store, "v"+strconv.Itoa(i)+"@example.com", "secret123", user.RoleUser)
	}

	const ip = "192.0.2.9"
	// Fail (IP threshold - 1) times across distinct accounts.
	for i := 0; i < loginIPMaxFailures-1; i++ {
		postJSON(t, router, "/api/auth/login", ip, loginRequest{Email: "v" + strconv.Itoa(i) + "@example.com", Password: "wrong"})
	}
	// A successful login from this IP must NOT reset the IP counter.
	okW := postJSON(t, router, "/api/auth/login", ip, loginRequest{Email: "good@example.com", Password: "secret123"})
	if okW.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", okW.Code)
	}
	// One more failure reaches the IP threshold and locks.
	w := postJSON(t, router, "/api/auth/login", ip, loginRequest{Email: "v0@example.com", Password: "wrong"})
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("attempt reaching threshold should still be 401, got %d", w.Code)
	}
	blocked := postJSON(t, router, "/api/auth/login", ip, loginRequest{Email: "v1@example.com", Password: "wrong"})
	if blocked.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 (success did not reset IP counter), got %d", blocked.Code)
	}
}

func TestRegister_IPLockout(t *testing.T) {
	router, _ := setupAuthRouter(t)

	const ip = "192.0.2.20"
	for i := 0; i < registerIPMaxFailures; i++ {
		w := postJSON(t, router, "/api/auth/register", ip, registerRequest{
			Email:      "x" + strconv.Itoa(i) + "@example.com",
			Password:   "secret123",
			InviteCode: "BAD-CODE-HERE",
		})
		if w.Code != http.StatusBadRequest {
			t.Fatalf("attempt %d: expected 400 (bad invite), got %d", i, w.Code)
		}
	}
	w := postJSON(t, router, "/api/auth/register", ip, registerRequest{
		Email:      "blocked@example.com",
		Password:   "secret123",
		InviteCode: "BAD-CODE-HERE",
	})
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 after register IP threshold, got %d", w.Code)
	}
	if decodeErr(t, w) != authFailMessage {
		t.Errorf("expected generic message, got %q", decodeErr(t, w))
	}
}

func TestRefresh_IPLockout(t *testing.T) {
	router, _ := setupAuthRouter(t)

	const ip = "192.0.2.30"
	for i := 0; i < refreshIPMaxFailures; i++ {
		w := postJSON(t, router, "/api/auth/refresh", ip, refreshRequest{RefreshToken: "bogus-token"})
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d: expected 401, got %d", i, w.Code)
		}
	}
	w := postJSON(t, router, "/api/auth/refresh", ip, refreshRequest{RefreshToken: "bogus-token"})
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 after refresh IP threshold, got %d", w.Code)
	}
	if w.Header().Get("Retry-After") == "" {
		t.Error("expected Retry-After header")
	}
}
