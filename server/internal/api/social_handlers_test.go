package api

import (
	"context"
	"net/http"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/cogitatorai/cogitator/server/internal/auth"
	"github.com/cogitatorai/cogitator/server/internal/database"
	"github.com/cogitatorai/cogitator/server/internal/social"
	"github.com/cogitatorai/cogitator/server/internal/user"
)

// mockSocialVerifier is a test double for SocialVerifier that returns a
// preconfigured result without performing any network requests.
type mockSocialVerifier struct {
	identity *social.VerifiedIdentity
	err      error
}

func (m *mockSocialVerifier) Verify(_ context.Context, _, _ string) (*social.VerifiedIdentity, error) {
	return m.identity, m.err
}

// setupSocialRouter creates a minimal Router with a real SQLite store, a JWT
// service, and the provided mock social verifier.
func setupSocialRouter(t *testing.T, verifier SocialVerifier) (*Router, *user.Store) {
	t.Helper()

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	db, err := database.Open(dbPath, database.Options{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	users := user.NewStore(db)
	jwtSvc := auth.NewJWTService("social-test-secret", 15*time.Minute, 7*24*time.Hour)

	router := NewRouter(RouterConfig{
		Users:          users,
		JWTService:     jwtSvc,
		SocialVerifier: verifier,
	})

	return router, users
}

// tokenForUser generates a fresh JWT access token for a user via the router's
// JWT service, bypassing HTTP login.
func tokenForUser(t *testing.T, r *Router, u *user.User) string {
	t.Helper()
	tok, err := r.jwtSvc.GenerateAccessToken(u.ID, string(u.Role))
	if err != nil {
		t.Fatalf("generate token for user %s: %v", u.ID, err)
	}
	return tok
}

// TestSocialAuth_NewUser_WithInvite exercises the full new-user registration
// path via social sign-in: verifier succeeds, invite code is valid, user is
// created and tokens are returned.
func TestSocialAuth_NewUser_WithInvite(t *testing.T) {
	verifier := &mockSocialVerifier{
		identity: &social.VerifiedIdentity{
			Provider: "google",
			Subject:  "google-sub-001",
			Email:    "alice@example.com",
			Name:     "Alice",
		},
	}
	router, store := setupSocialRouter(t, verifier)

	admin := createTestUser(t, store, "admin@test.com", "admin-pass", user.RoleAdmin)
	code := createTestInviteCode(t, store, admin.ID, user.RoleUser)

	rec := doRequest(t, router, "POST", "/api/auth/social", "", socialAuthRequest{
		Provider:   "google",
		IDToken:    "fake-id-token",
		InviteCode: code,
	})

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp authResponse
	decodeJSON(t, rec, &resp)

	if resp.User == nil {
		t.Fatal("expected user in response")
	}
	if resp.User.Email != "alice@example.com" {
		t.Errorf("expected email 'alice@example.com', got %q", resp.User.Email)
	}
	if resp.User.Role != user.RoleUser {
		t.Errorf("expected role 'user', got %q", resp.User.Role)
	}
	if resp.AccessToken == "" {
		t.Error("expected non-empty access_token")
	}
	if resp.RefreshToken == "" {
		t.Error("expected non-empty refresh_token")
	}
}

// TestSocialAuth_ExistingUser verifies that a returning social user whose
// provider identity is already linked receives tokens with HTTP 200.
func TestSocialAuth_ExistingUser(t *testing.T) {
	verifier := &mockSocialVerifier{
		identity: &social.VerifiedIdentity{
			Provider: "google",
			Subject:  "google-sub-002",
			Email:    "bob@example.com",
			Name:     "Bob",
		},
	}
	router, store := setupSocialRouter(t, verifier)

	// Pre-create the user and link the OAuth identity directly via the store.
	existing := createTestUser(t, store, "bob@example.com", "some-pass", user.RoleUser)
	if err := store.LinkOAuth(existing.ID, "google", "google-sub-002", "bob@example.com"); err != nil {
		t.Fatalf("link oauth: %v", err)
	}

	rec := doRequest(t, router, "POST", "/api/auth/social", "", socialAuthRequest{
		Provider: "google",
		IDToken:  "fake-id-token",
	})

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp authResponse
	decodeJSON(t, rec, &resp)

	if resp.User == nil {
		t.Fatal("expected user in response")
	}
	if resp.User.ID != existing.ID {
		t.Errorf("expected existing user ID %q, got %q", existing.ID, resp.User.ID)
	}
	if resp.AccessToken == "" {
		t.Error("expected non-empty access_token")
	}
}

// TestSocialAuth_NoInvite_Forbidden verifies that a new social identity without
// an invite code is rejected with HTTP 403.
func TestSocialAuth_NoInvite_Forbidden(t *testing.T) {
	verifier := &mockSocialVerifier{
		identity: &social.VerifiedIdentity{
			Provider: "google",
			Subject:  "google-sub-new",
			Email:    "newuser@example.com",
			Name:     "New User",
		},
	}
	router, _ := setupSocialRouter(t, verifier)

	rec := doRequest(t, router, "POST", "/api/auth/social", "", socialAuthRequest{
		Provider: "google",
		IDToken:  "fake-id-token",
		// No InviteCode.
	})

	if rec.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestSocialAuth_VerifierError verifies that a verifier failure returns 401.
func TestSocialAuth_VerifierError(t *testing.T) {
	verifier := &mockSocialVerifier{
		err: social.ErrInvalidToken,
	}
	router, _ := setupSocialRouter(t, verifier)

	rec := doRequest(t, router, "POST", "/api/auth/social", "", socialAuthRequest{
		Provider: "google",
		IDToken:  "bad-token",
	})

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestLinkOAuth verifies that an authenticated user can link a new OAuth
// provider identity to their account.
func TestLinkOAuth(t *testing.T) {
	verifier := &mockSocialVerifier{
		identity: &social.VerifiedIdentity{
			Provider: "google",
			Subject:  "google-sub-link",
			Email:    "carol@example.com",
			Name:     "Carol",
		},
	}
	router, store := setupSocialRouter(t, verifier)

	u := createTestUser(t, store, "carol@example.com", "carol-pass", user.RoleUser)
	tok := tokenForUser(t, router, u)

	rec := doRequest(t, router, "POST", "/api/account/link", tok, linkOAuthRequest{
		Provider: "google",
		IDToken:  "fake-id-token",
	})

	if rec.Code != http.StatusNoContent {
		t.Errorf("expected 204, got %d: %s", rec.Code, rec.Body.String())
	}

	// Confirm the link was actually persisted.
	links, err := store.ListOAuthLinks(u.ID)
	if err != nil {
		t.Fatalf("list links: %v", err)
	}
	if len(links) != 1 {
		t.Fatalf("expected 1 link, got %d", len(links))
	}
	if links[0].Provider != "google" {
		t.Errorf("expected provider 'google', got %q", links[0].Provider)
	}
}

// TestUnlinkOAuth_LastMethod_Blocked verifies that unlinking the only provider
// when there is no password is rejected with 400.
func TestUnlinkOAuth_LastMethod_Blocked(t *testing.T) {
	verifier := &mockSocialVerifier{}
	router, store := setupSocialRouter(t, verifier)

	// Create a social-only user (no password) by creating via CreateSocial.
	socialUser, err := store.CreateSocial("socialonly@example.com", "Social Only", user.RoleUser)
	if err != nil {
		t.Fatalf("create social user: %v", err)
	}
	if err := store.LinkOAuth(socialUser.ID, "google", "google-sub-solo", "solo@example.com"); err != nil {
		t.Fatalf("link oauth: %v", err)
	}

	tok := tokenForUser(t, router, socialUser)

	rec := doRequest(t, router, "DELETE", "/api/account/link/google", tok, nil)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestUnlinkOAuth_WithPassword_Allowed verifies that a user who has a password
// can unlink an OAuth provider even if it is their only linked provider.
func TestUnlinkOAuth_WithPassword_Allowed(t *testing.T) {
	verifier := &mockSocialVerifier{}
	router, store := setupSocialRouter(t, verifier)

	u := createTestUser(t, store, "dave@example.com", "dave-pass", user.RoleUser)
	if err := store.LinkOAuth(u.ID, "google", "google-sub-dave", "dave@example.com"); err != nil {
		t.Fatalf("link oauth: %v", err)
	}

	tok := tokenForUser(t, router, u)

	rec := doRequest(t, router, "DELETE", "/api/account/link/google", tok, nil)

	if rec.Code != http.StatusNoContent {
		t.Errorf("expected 204, got %d: %s", rec.Code, rec.Body.String())
	}

	// Confirm the link is gone.
	links, err := store.ListOAuthLinks(u.ID)
	if err != nil {
		t.Fatalf("list links: %v", err)
	}
	if len(links) != 0 {
		t.Errorf("expected 0 links after unlink, got %d", len(links))
	}
}

// TestListOAuthLinks verifies that listing links returns all linked providers
// for the authenticated user.
func TestListOAuthLinks(t *testing.T) {
	verifier := &mockSocialVerifier{}
	router, store := setupSocialRouter(t, verifier)

	u := createTestUser(t, store, "eve@example.com", "eve-pass", user.RoleUser)
	if err := store.LinkOAuth(u.ID, "google", "google-sub-eve", "eve@example.com"); err != nil {
		t.Fatalf("link google: %v", err)
	}
	if err := store.LinkOAuth(u.ID, "apple", "apple-sub-eve", "eve@icloud.com"); err != nil {
		t.Fatalf("link apple: %v", err)
	}

	tok := tokenForUser(t, router, u)

	rec := doRequest(t, router, "GET", "/api/account/links", tok, nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var links []user.OAuthLink
	decodeJSON(t, rec, &links)

	if len(links) != 2 {
		t.Errorf("expected 2 links, got %d", len(links))
	}

	// Verify both providers are present.
	providers := make(map[string]bool)
	for _, l := range links {
		providers[l.Provider] = true
	}
	if !providers["google"] {
		t.Error("expected 'google' in links")
	}
	if !providers["apple"] {
		t.Error("expected 'apple' in links")
	}
}

// TestListOAuthLinks_Empty verifies that a user with no links receives an empty
// array (not null) with HTTP 200.
func TestListOAuthLinks_Empty(t *testing.T) {
	verifier := &mockSocialVerifier{}
	router, store := setupSocialRouter(t, verifier)

	u := createTestUser(t, store, "frank@example.com", "frank-pass", user.RoleUser)
	tok := tokenForUser(t, router, u)

	rec := doRequest(t, router, "GET", "/api/account/links", tok, nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var links []user.OAuthLink
	decodeJSON(t, rec, &links)

	if links == nil {
		t.Error("expected empty array, got nil")
	}
	if len(links) != 0 {
		t.Errorf("expected 0 links, got %d", len(links))
	}
}

// --- OAuth state / pending-result store behavior (bounded TTL stores) ---

// TestAuthClaim_StoredResultClaimedOnce verifies the pending-result store's
// single-use semantics through the HTTP claim endpoint: the stored tokens are
// returned exactly once, and a second poll for the same id returns 202.
func TestAuthClaim_StoredResultClaimedOnce(t *testing.T) {
	router, _ := setupSocialRouter(t, &mockSocialVerifier{})

	router.pendingAuthResults.set("claim-1", &pendingAuthResult{
		AccessToken:  "access-abc",
		RefreshToken: "refresh-def",
	})

	rec := doRequest(t, router, "GET", "/api/auth/claim/claim-1", "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 on first claim, got %d: %s", rec.Code, rec.Body.String())
	}
	var got pendingAuthResult
	decodeJSON(t, rec, &got)
	if got.AccessToken != "access-abc" || got.RefreshToken != "refresh-def" {
		t.Fatalf("unexpected tokens: %+v", got)
	}

	// Single-use: the second poll finds nothing and returns 202.
	rec2 := doRequest(t, router, "GET", "/api/auth/claim/claim-1", "", nil)
	if rec2.Code != http.StatusAccepted {
		t.Fatalf("expected 202 after result already claimed, got %d", rec2.Code)
	}
}

// TestAuthClaim_ExpiredResultNotReturned verifies that a result past its TTL is
// treated as absent (202), exercising on-access expiry in the live flow.
func TestAuthClaim_ExpiredResultNotReturned(t *testing.T) {
	router, _ := setupSocialRouter(t, &mockSocialVerifier{})

	now := time.Unix(1000, 0)
	router.pendingAuthResults.now = func() time.Time { return now }

	router.pendingAuthResults.set("claim-exp", &pendingAuthResult{AccessToken: "tok"})

	// Advance past the result TTL: the entry must be rejected as expired.
	now = now.Add(pendingResultTTL + time.Second)

	rec := doRequest(t, router, "GET", "/api/auth/claim/claim-exp", "", nil)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202 for expired result, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestSocialState_ExpiredRejected verifies state expiry semantics: once a state
// nonce is past its TTL it is no longer retrievable, so the callback would
// reject it as "unknown or expired state".
func TestSocialState_ExpiredRejected(t *testing.T) {
	router, _ := setupSocialRouter(t, &mockSocialVerifier{})

	now := time.Unix(2000, 0)
	router.socialOAuthStates.now = func() time.Time { return now }

	router.socialOAuthStates.set("state-x", &pendingSocialAuth{purpose: "login"})

	now = now.Add(socialStateTTL + time.Second)
	if _, ok := router.socialOAuthStates.take("state-x"); ok {
		t.Fatal("expired state must be rejected (auth denied)")
	}
}

// TestSocialState_SingleUse verifies a state nonce is consumed on first take, so
// a replayed callback with the same state is rejected.
func TestSocialState_SingleUse(t *testing.T) {
	router, _ := setupSocialRouter(t, &mockSocialVerifier{})

	router.socialOAuthStates.set("state-once", &pendingSocialAuth{purpose: "login"})

	if _, ok := router.socialOAuthStates.take("state-once"); !ok {
		t.Fatal("first take of a valid state should succeed")
	}
	if _, ok := router.socialOAuthStates.take("state-once"); ok {
		t.Fatal("state nonce must be single-use; replay must be rejected")
	}
}

// TestSocialState_CapEvictsOldestNewestUsable verifies the hard cap: once the
// store is full, the oldest state is evicted while the newest remains fully
// usable (the common live flow for a recent user is unaffected).
func TestSocialState_CapEvictsOldestNewestUsable(t *testing.T) {
	router, _ := setupSocialRouter(t, &mockSocialVerifier{})

	base := time.Unix(3000, 0)
	cur := base
	router.socialOAuthStates.now = func() time.Time { return cur }

	max := router.socialOAuthStates.max
	// Fill to capacity; "s0" is the oldest.
	for i := 0; i < max; i++ {
		cur = base.Add(time.Duration(i) * time.Millisecond)
		router.socialOAuthStates.set("s"+strconv.Itoa(i), &pendingSocialAuth{purpose: "login"})
	}
	// One more distinct state evicts the oldest.
	cur = base.Add(time.Duration(max) * time.Millisecond)
	router.socialOAuthStates.set("newest", &pendingSocialAuth{purpose: "link"})

	if _, ok := router.socialOAuthStates.take("s0"); ok {
		t.Fatal("oldest state should have been evicted at cap")
	}
	got, ok := router.socialOAuthStates.take("newest")
	if !ok {
		t.Fatal("newest state must remain usable after eviction")
	}
	if got.purpose != "link" {
		t.Fatalf("expected newest state preserved, got purpose %q", got.purpose)
	}
}
