package connector

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/oauth2"

	"github.com/cogitatorai/cogitator/server/internal/secretstore"
)

func TestOAuth_StartAuth(t *testing.T) {
	dir := t.TempDir()
	o := NewOAuthRuntime(secretstore.NewFileStore(dir), 8484)

	cfg := AuthConfig{
		Type:     "oauth2",
		AuthURL:  "https://accounts.google.com/o/oauth2/auth",
		TokenURL: "https://oauth2.googleapis.com/token",
		Scopes:   []string{"calendar.readonly"},
	}

	url, err := o.StartAuth("google", "user1", cfg, "test-client-id", "test-client-secret", "", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if url == "" {
		t.Fatal("expected non-empty auth URL")
	}
}

func TestOAuth_Status_Disconnected(t *testing.T) {
	dir := t.TempDir()
	o := NewOAuthRuntime(secretstore.NewFileStore(dir), 8484)
	if o.Status("google", "user1") {
		t.Fatal("expected disconnected")
	}
}

func TestOAuth_HandleCallback_InvalidState(t *testing.T) {
	dir := t.TempDir()
	o := NewOAuthRuntime(secretstore.NewFileStore(dir), 8484)
	_, _, _, _, _, err := o.HandleCallback("somecode", "badstate")
	if err == nil {
		t.Fatal("expected error for invalid state")
	}
}

func TestOAuth_Revoke_Noop(t *testing.T) {
	dir := t.TempDir()
	o := NewOAuthRuntime(secretstore.NewFileStore(dir), 8484)
	if err := o.Revoke("google", "user1"); err != nil {
		t.Fatal(err)
	}
}

func TestOAuth_TokenPersistence(t *testing.T) {
	dir := t.TempDir()
	store := secretstore.NewFileStore(dir)
	o := NewOAuthRuntime(store, 8484)

	// Manually inject a token.
	o.mu.Lock()
	info := &TokenInfo{
		AccessToken:  "access123",
		RefreshToken: "refresh456",
	}
	o.tokens["google:user1"] = info
	o.mu.Unlock()

	if err := o.saveToken("google:user1", info); err != nil {
		t.Fatal(err)
	}

	// Reload into a new runtime using the same store.
	o2 := NewOAuthRuntime(store, 8484)
	if !o2.Status("google", "user1") {
		t.Fatal("expected connected after reload")
	}
}

func TestOAuth_ConnectorNames(t *testing.T) {
	dir := t.TempDir()
	o := NewOAuthRuntime(secretstore.NewFileStore(dir), 8484)

	o.mu.Lock()
	o.tokens["google:user1"] = &TokenInfo{RefreshToken: "r1"}
	o.tokens["slack:user1"] = &TokenInfo{RefreshToken: "r2"}
	o.tokens["google:user2"] = &TokenInfo{RefreshToken: "r3"}
	o.mu.Unlock()

	names := o.ConnectedConnectors("user1")
	if len(names) != 2 {
		t.Fatalf("expected 2 connectors, got %d", len(names))
	}
}

func TestOAuth_LoadTokens_FileNotExist(t *testing.T) {
	dir := t.TempDir()
	// Use a subdirectory that does not exist; NewFileStore handles missing dirs gracefully.
	o := NewOAuthRuntime(secretstore.NewFileStore(filepath.Join(dir, "nonexistent")), 8484)
	if len(o.tokens) != 0 {
		t.Fatal("expected empty tokens for nonexistent store dir")
	}
}

// failingTokenSource simulates a token source that returns an error (e.g. revoked refresh token).
type failingTokenSource struct{}

func (f *failingTokenSource) Token() (*oauth2.Token, error) {
	return nil, errors.New("oauth2: token expired and refresh token is not set")
}

// succeedingTokenSource simulates a successful token refresh.
type succeedingTokenSource struct{}

func (s *succeedingTokenSource) Token() (*oauth2.Token, error) {
	return &oauth2.Token{
		AccessToken:  "new-access-token",
		RefreshToken: "new-refresh-token",
		TokenType:    "Bearer",
		Expiry:       time.Now().Add(1 * time.Hour),
	}, nil
}

func TestOAuth_AuthError_TrackedOnRefreshFailure(t *testing.T) {
	dir := t.TempDir()
	o := NewOAuthRuntime(secretstore.NewFileStore(dir), 8484)

	// Inject a token so the connector appears connected.
	key := "google:user1"
	o.mu.Lock()
	o.tokens[key] = &TokenInfo{
		AccessToken:  "expired-access",
		RefreshToken: "revoked-refresh",
		Expiry:       time.Now().Add(-1 * time.Hour),
	}
	o.mu.Unlock()

	// Status should report connected (refresh token is present).
	if !o.Status("google", "user1") {
		t.Fatal("expected connected before refresh failure")
	}

	// Simulate a failed token refresh via savingTokenSource.
	sts := &savingTokenSource{
		base:    &failingTokenSource{},
		runtime: o,
		key:     key,
	}
	_, err := sts.Token()
	if err == nil {
		t.Fatal("expected error from failing token source")
	}

	// StatusDetail should now report the auth error (sanitized, not raw).
	connected, authErr := o.StatusDetail("google", "user1")
	if !connected {
		t.Fatal("expected still structurally connected (refresh token present)")
	}
	if authErr != "token refresh failed" {
		t.Fatalf("expected sanitized auth error, got: %q", authErr)
	}
}

func TestOAuth_AuthError_ClearedOnSuccessfulRefresh(t *testing.T) {
	dir := t.TempDir()
	o := NewOAuthRuntime(secretstore.NewFileStore(dir), 8484)

	key := "google:user1"
	o.mu.Lock()
	o.tokens[key] = &TokenInfo{
		AccessToken:  "expired-access",
		RefreshToken: "valid-refresh",
		Expiry:       time.Now().Add(-1 * time.Hour),
	}
	o.mu.Unlock()

	// Simulate a failed refresh to set the error.
	failSts := &savingTokenSource{
		base:    &failingTokenSource{},
		runtime: o,
		key:     key,
	}
	failSts.Token()

	// Confirm error is set.
	_, authErr := o.StatusDetail("google", "user1")
	if authErr == "" {
		t.Fatal("expected auth error after failure")
	}

	// Now simulate a successful refresh.
	successSts := &savingTokenSource{
		base:    &succeedingTokenSource{},
		runtime: o,
		key:     key,
	}
	_, err := successSts.Token()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Error should be cleared.
	_, authErr = o.StatusDetail("google", "user1")
	if authErr != "" {
		t.Fatalf("expected auth error to be cleared after successful refresh, got: %s", authErr)
	}

	// Let the async token-save goroutine finish before TempDir cleanup.
	time.Sleep(50 * time.Millisecond)
}

func TestOAuth_AuthError_ClearedOnRevoke(t *testing.T) {
	dir := t.TempDir()
	o := NewOAuthRuntime(secretstore.NewFileStore(dir), 8484)

	key := "google:user1"
	o.mu.Lock()
	o.tokens[key] = &TokenInfo{
		AccessToken:  "expired",
		RefreshToken: "revoked",
		Expiry:       time.Now().Add(-1 * time.Hour),
	}
	o.mu.Unlock()

	// Set an auth error.
	failSts := &savingTokenSource{
		base:    &failingTokenSource{},
		runtime: o,
		key:     key,
	}
	failSts.Token()

	// Revoke (disconnect) should clear the error.
	if err := o.Revoke("google", "user1"); err != nil {
		t.Fatal(err)
	}

	_, authErr := o.StatusDetail("google", "user1")
	if authErr != "" {
		t.Fatalf("expected auth error to be cleared after revoke, got: %s", authErr)
	}
}

func TestOAuth_SaveTokens_CreatesFile(t *testing.T) {
	dir := t.TempDir()
	store := secretstore.NewFileStore(dir)
	o := NewOAuthRuntime(store, 8484)

	info := &TokenInfo{
		AccessToken:  "a",
		RefreshToken: "r",
	}
	o.mu.Lock()
	o.tokens["test:user1"] = info
	o.mu.Unlock()

	if err := o.saveToken("test:user1", info); err != nil {
		t.Fatal(err)
	}

	// Verify data was persisted by reading it back.
	val, err := store.Get("connector", "test:user1")
	if err != nil {
		t.Fatalf("expected stored token, got error: %v", err)
	}
	if val == "" {
		t.Fatal("expected non-empty stored value")
	}
}
