package auth

import (
	"context"
	"testing"
	"time"
)

const testSecret = "super-secret-key-for-testing-only"

func newTestService() *JWTService {
	return NewJWTService(testSecret, 15*time.Minute, 7*24*time.Hour)
}

func TestGenerateAccessToken(t *testing.T) {
	svc := newTestService()
	token, err := svc.GenerateAccessToken("user-123", "admin")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if token == "" {
		t.Fatal("expected non-empty token")
	}

	// Validate the token and inspect claims.
	claims, err := svc.ValidateAccessToken(token)
	if err != nil {
		t.Fatalf("generated token should be valid: %v", err)
	}
	if claims.UserID != "user-123" {
		t.Errorf("UserID = %q, want %q", claims.UserID, "user-123")
	}
	if claims.Role != "admin" {
		t.Errorf("Role = %q, want %q", claims.Role, "admin")
	}
	if claims.ExpiresAt == nil {
		t.Fatal("ExpiresAt should be set")
	}
	if claims.IssuedAt == nil {
		t.Fatal("IssuedAt should be set")
	}
	// Expiry should be roughly 15 minutes from now.
	expiry := claims.ExpiresAt.Time
	if expiry.Before(time.Now()) {
		t.Error("token should not already be expired")
	}
	if expiry.After(time.Now().Add(16 * time.Minute)) {
		t.Error("token expiry is too far in the future")
	}
}

func TestValidateAccessToken(t *testing.T) {
	svc := newTestService()

	token, err := svc.GenerateAccessToken("u-42", "viewer")
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	claims, err := svc.ValidateAccessToken(token)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if claims.UserID != "u-42" {
		t.Errorf("UserID = %q, want %q", claims.UserID, "u-42")
	}
	if claims.Role != "viewer" {
		t.Errorf("Role = %q, want %q", claims.Role, "viewer")
	}
}

func TestValidateAccessToken_Expired(t *testing.T) {
	// Use a negative TTL so the token is born expired.
	svc := NewJWTService(testSecret, -1*time.Second, time.Hour)

	token, err := svc.GenerateAccessToken("u-expired", "admin")
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	_, err = svc.ValidateAccessToken(token)
	if err == nil {
		t.Fatal("expected error for expired token, got nil")
	}
}

func TestValidateAccessToken_WrongSecret(t *testing.T) {
	svc := newTestService()
	token, err := svc.GenerateAccessToken("u-1", "admin")
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	other := NewJWTService("completely-different-secret", 15*time.Minute, time.Hour)
	_, err = other.ValidateAccessToken(token)
	if err == nil {
		t.Fatal("expected error when validating with wrong secret, got nil")
	}
}

func TestGenerateRefreshToken(t *testing.T) {
	svc := newTestService()
	raw, hash, expiresAt, err := svc.GenerateRefreshToken()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if raw == "" {
		t.Error("raw token should not be empty")
	}
	if hash == "" {
		t.Error("hash should not be empty")
	}
	if raw == hash {
		t.Error("raw and hash should differ")
	}
	if expiresAt.Before(time.Now()) {
		t.Error("expiresAt should be in the future")
	}
	// Verify the hash matches what HashToken produces.
	if got := svc.HashToken(raw); got != hash {
		t.Errorf("HashToken(raw) = %q, want %q", got, hash)
	}
}

func TestHashToken(t *testing.T) {
	svc := newTestService()

	h1 := svc.HashToken("deterministic-input")
	h2 := svc.HashToken("deterministic-input")
	if h1 != h2 {
		t.Errorf("HashToken is not deterministic: %q != %q", h1, h2)
	}

	h3 := svc.HashToken("different-input")
	if h1 == h3 {
		t.Error("different inputs should produce different hashes")
	}
}

func TestContextUser(t *testing.T) {
	ctx := context.Background()

	// Before setting, should return false.
	_, ok := UserFromContext(ctx)
	if ok {
		t.Error("expected no user in empty context")
	}

	// Round-trip.
	u := ContextUser{ID: "u-ctx", Role: "editor"}
	ctx = WithUser(ctx, u)

	got, ok := UserFromContext(ctx)
	if !ok {
		t.Fatal("expected user in context")
	}
	if got.ID != u.ID {
		t.Errorf("ID = %q, want %q", got.ID, u.ID)
	}
	if got.Role != u.Role {
		t.Errorf("Role = %q, want %q", got.Role, u.Role)
	}
}

func TestMustUserFromContext_Panics(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic from MustUserFromContext on empty context")
		}
	}()
	MustUserFromContext(context.Background())
}

func TestRefreshTokenTTL(t *testing.T) {
	ttl := 7 * 24 * time.Hour
	svc := NewJWTService(testSecret, time.Minute, ttl)
	if got := svc.RefreshTokenTTL(); got != ttl {
		t.Errorf("RefreshTokenTTL() = %v, want %v", got, ttl)
	}
}
