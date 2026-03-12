package social

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const testKID = "test-kid"

// testJWKS generates a fresh RSA key pair and starts an httptest.Server that
// serves a JWKS containing the public key under kid "test-kid".
func testJWKS(t *testing.T) (*rsa.PrivateKey, *httptest.Server) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate RSA key: %v", err)
	}

	nBytes := key.PublicKey.N.Bytes()
	eVal := big.NewInt(int64(key.PublicKey.E))
	eBytes := eVal.Bytes()

	jwks := map[string]any{
		"keys": []map[string]any{
			{
				"kty": "RSA",
				"kid": testKID,
				"n":   base64.RawURLEncoding.EncodeToString(nBytes),
				"e":   base64.RawURLEncoding.EncodeToString(eBytes),
			},
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(jwks)
	}))
	t.Cleanup(srv.Close)

	return key, srv
}

// signToken creates a signed RS256 JWT with the given claims and the test kid header.
func signToken(t *testing.T, key *rsa.PrivateKey, claims jwt.MapClaims) string {
	t.Helper()
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = testKID
	signed, err := token.SignedString(key)
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}
	return signed
}

func TestVerifyGoogle_Valid(t *testing.T) {
	key, srv := testJWKS(t)
	v := NewVerifier("my-google-client-id", "")

	claims := jwt.MapClaims{
		"iss":   "https://accounts.google.com",
		"aud":   "my-google-client-id",
		"sub":   "1234567890",
		"email": "user@example.com",
		"name":  "Test User",
		"exp":   time.Now().Add(time.Hour).Unix(),
		"iat":   time.Now().Unix(),
	}
	token := signToken(t, key, claims)

	id, err := v.verifyWithJWKSURL(context.Background(), "google", token, srv.URL)
	if err != nil {
		t.Fatalf("expected valid token, got error: %v", err)
	}
	if id.Subject != "1234567890" {
		t.Errorf("subject: got %q, want %q", id.Subject, "1234567890")
	}
	if id.Email != "user@example.com" {
		t.Errorf("email: got %q, want %q", id.Email, "user@example.com")
	}
	if id.Name != "Test User" {
		t.Errorf("name: got %q, want %q", id.Name, "Test User")
	}
	if id.Provider != "google" {
		t.Errorf("provider: got %q, want %q", id.Provider, "google")
	}
}

func TestVerifyGoogle_BadAudience(t *testing.T) {
	key, srv := testJWKS(t)
	v := NewVerifier("my-google-client-id", "")

	claims := jwt.MapClaims{
		"iss":   "https://accounts.google.com",
		"aud":   "wrong-client-id",
		"sub":   "1234567890",
		"email": "user@example.com",
		"exp":   time.Now().Add(time.Hour).Unix(),
		"iat":   time.Now().Unix(),
	}
	token := signToken(t, key, claims)

	_, err := v.verifyWithJWKSURL(context.Background(), "google", token, srv.URL)
	if err == nil {
		t.Fatal("expected error for bad audience, got nil")
	}
}

func TestVerifyGoogle_Expired(t *testing.T) {
	key, srv := testJWKS(t)
	v := NewVerifier("my-google-client-id", "")

	claims := jwt.MapClaims{
		"iss":   "https://accounts.google.com",
		"aud":   "my-google-client-id",
		"sub":   "1234567890",
		"email": "user@example.com",
		"exp":   time.Now().Add(-time.Hour).Unix(),
		"iat":   time.Now().Add(-2 * time.Hour).Unix(),
	}
	token := signToken(t, key, claims)

	_, err := v.verifyWithJWKSURL(context.Background(), "google", token, srv.URL)
	if err == nil {
		t.Fatal("expected error for expired token, got nil")
	}
}

func TestVerify_UnsupportedProvider(t *testing.T) {
	v := NewVerifier("my-google-client-id", "")

	_, err := v.Verify(context.Background(), "facebook", "some-token")
	if err != ErrUnsupported {
		t.Errorf("expected ErrUnsupported, got: %v", err)
	}
}

func TestVerify_NotConfigured(t *testing.T) {
	v := NewVerifier("", "")

	_, err := v.verifyWithJWKSURL(context.Background(), "google", "some-token", "http://localhost")
	if err != ErrNotConfigured {
		t.Errorf("expected ErrNotConfigured, got: %v", err)
	}
}
