package connector

import (
	"path/filepath"
	"testing"

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
