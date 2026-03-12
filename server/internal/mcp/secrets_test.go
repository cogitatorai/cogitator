package mcp

import (
	"testing"

	"github.com/cogitatorai/cogitator/server/internal/secretstore"
)

func TestLoadMCPSecrets_NonExistent(t *testing.T) {
	store := secretstore.NewFileStore(t.TempDir())
	secrets, err := LoadMCPSecrets(store)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if secrets == nil {
		t.Fatal("expected non-nil map, got nil")
	}
	if len(secrets) != 0 {
		t.Fatalf("expected empty map, got %d entries", len(secrets))
	}
}

func TestLoadMCPSecrets_WithEntries(t *testing.T) {
	store := secretstore.NewFileStore(t.TempDir())

	// Populate acme-api headers.
	acmeSecrets := &ServerSecrets{
		Headers: map[string]string{
			"Authorization": "Bearer tok-123",
			"X-Custom":      "value",
		},
	}
	corpSecrets := &ServerSecrets{
		OAuth: &OAuthSecrets{
			ClientID:     "cid-abc",
			ClientSecret: "csec-xyz",
			Scopes:       []string{"read", "write"},
			RedirectURI:  "http://localhost:9999/callback",
		},
	}
	if err := SaveMCPSecrets(store, map[string]*ServerSecrets{
		"acme-api": acmeSecrets,
		"corp-sso": corpSecrets,
	}); err != nil {
		t.Fatal(err)
	}

	secrets, err := LoadMCPSecrets(store)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify acme-api headers.
	acme, ok := secrets["acme-api"]
	if !ok {
		t.Fatal("missing acme-api entry")
	}
	if got := acme.Headers["Authorization"]; got != "Bearer tok-123" {
		t.Fatalf("Authorization header = %q, want %q", got, "Bearer tok-123")
	}
	if got := acme.Headers["X-Custom"]; got != "value" {
		t.Fatalf("X-Custom header = %q, want %q", got, "value")
	}

	// Verify corp-sso OAuth fields.
	corp, ok := secrets["corp-sso"]
	if !ok {
		t.Fatal("missing corp-sso entry")
	}
	if corp.OAuth == nil {
		t.Fatal("expected OAuth to be set for corp-sso")
	}
	if corp.OAuth.ClientID != "cid-abc" {
		t.Fatalf("ClientID = %q, want %q", corp.OAuth.ClientID, "cid-abc")
	}
	if corp.OAuth.ClientSecret != "csec-xyz" {
		t.Fatalf("ClientSecret = %q, want %q", corp.OAuth.ClientSecret, "csec-xyz")
	}
	if len(corp.OAuth.Scopes) != 2 || corp.OAuth.Scopes[0] != "read" || corp.OAuth.Scopes[1] != "write" {
		t.Fatalf("Scopes = %v, want [read write]", corp.OAuth.Scopes)
	}
	if corp.OAuth.RedirectURI != "http://localhost:9999/callback" {
		t.Fatalf("RedirectURI = %q, want %q", corp.OAuth.RedirectURI, "http://localhost:9999/callback")
	}
}

func TestSaveMCPSecrets_RoundTrip(t *testing.T) {
	store := secretstore.NewFileStore(t.TempDir())

	// Save MCP secrets.
	mcpSecrets := map[string]*ServerSecrets{
		"my-server": {
			Headers: map[string]string{"Authorization": "Bearer round-trip"},
			OAuth: &OAuthSecrets{
				ClientID:     "rt-cid",
				ClientSecret: "rt-csec",
				Scopes:       []string{"admin"},
			},
		},
	}
	if err := SaveMCPSecrets(store, mcpSecrets); err != nil {
		t.Fatalf("SaveMCPSecrets: %v", err)
	}

	// Load back and verify round-trip.
	loaded, err := LoadMCPSecrets(store)
	if err != nil {
		t.Fatalf("LoadMCPSecrets: %v", err)
	}
	srv, ok := loaded["my-server"]
	if !ok {
		t.Fatal("missing my-server after round-trip")
	}
	if got := srv.Headers["Authorization"]; got != "Bearer round-trip" {
		t.Fatalf("Authorization = %q, want %q", got, "Bearer round-trip")
	}
	if srv.OAuth == nil || srv.OAuth.ClientID != "rt-cid" {
		t.Fatal("OAuth data lost in round-trip")
	}
}

func TestSaveMCPSecrets_DeletesRemovedEntries(t *testing.T) {
	store := secretstore.NewFileStore(t.TempDir())

	// Save two entries.
	if err := SaveMCPSecrets(store, map[string]*ServerSecrets{
		"keep":   {Headers: map[string]string{"X": "1"}},
		"remove": {Headers: map[string]string{"X": "2"}},
	}); err != nil {
		t.Fatal(err)
	}

	// Save only one — the other must be pruned.
	if err := SaveMCPSecrets(store, map[string]*ServerSecrets{
		"keep": {Headers: map[string]string{"X": "1"}},
	}); err != nil {
		t.Fatal(err)
	}

	loaded, err := LoadMCPSecrets(store)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := loaded["remove"]; ok {
		t.Fatal("removed entry should not be present after save")
	}
	if _, ok := loaded["keep"]; !ok {
		t.Fatal("kept entry should still be present")
	}
}
