package social

import (
	"context"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const (
	googleJWKSURL = "https://www.googleapis.com/oauth2/v3/certs"
	appleJWKSURL  = "https://appleid.apple.com/auth/keys"

	googleIssuer1 = "https://accounts.google.com"
	googleIssuer2 = "accounts.google.com"
	appleIssuer   = "https://appleid.apple.com"

	jwksCacheTTL = time.Hour
)

var (
	// ErrInvalidToken is returned when the token fails signature or claims validation.
	ErrInvalidToken = errors.New("social: invalid token")
	// ErrUnsupported is returned when an unknown provider name is given.
	ErrUnsupported = errors.New("social: unsupported provider")
	// ErrNotConfigured is returned when the relevant client ID has not been set.
	ErrNotConfigured = errors.New("social: provider not configured")
)

// VerifiedIdentity holds the claims extracted from a valid social ID token.
type VerifiedIdentity struct {
	Provider string
	Subject  string
	Email    string
	Name     string
}

// jwksCache holds RSA public keys fetched from a JWKS endpoint, with a TTL.
type jwksCache struct {
	mu      sync.RWMutex
	keys    map[string]*rsa.PublicKey
	fetched time.Time
	ttl     time.Duration
}

func newJWKSCache(ttl time.Duration) *jwksCache {
	return &jwksCache{
		keys: make(map[string]*rsa.PublicKey),
		ttl:  ttl,
	}
}

// get returns the key for kid from the cache, or (nil, false) if stale/missing.
func (c *jwksCache) get(kid string) (*rsa.PublicKey, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if time.Since(c.fetched) > c.ttl {
		return nil, false
	}
	k, ok := c.keys[kid]
	return k, ok
}

// set replaces the entire key set and resets the fetch timestamp.
func (c *jwksCache) set(keys map[string]*rsa.PublicKey) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.keys = keys
	c.fetched = time.Now()
}

// jwksResponse is the JSON structure returned by JWKS endpoints.
type jwksResponse struct {
	Keys []jwkKey `json:"keys"`
}

type jwkKey struct {
	Kty string `json:"kty"`
	Kid string `json:"kid"`
	N   string `json:"n"`
	E   string `json:"e"`
}

// Verifier verifies Google and Apple ID tokens using JWKS public keys.
type Verifier struct {
	googleClientID    string
	appleAudiences    []string
	httpClient        *http.Client
	googleJWKSCache   *jwksCache
	appleJWKSCache    *jwksCache
}

// NewVerifier creates a Verifier for the given client IDs.
// appleAudiences accepts multiple valid audience values (e.g. Services ID
// for web/desktop and bundle ID for mobile).
func NewVerifier(googleClientID string, appleAudiences ...string) *Verifier {
	// Filter out empty strings.
	var auds []string
	for _, a := range appleAudiences {
		if a != "" {
			auds = append(auds, a)
		}
	}
	return &Verifier{
		googleClientID:  googleClientID,
		appleAudiences:  auds,
		httpClient:      &http.Client{Timeout: 10 * time.Second},
		googleJWKSCache: newJWKSCache(jwksCacheTTL),
		appleJWKSCache:  newJWKSCache(jwksCacheTTL),
	}
}

// Verify validates provider (google|apple) and returns the verified identity.
func (v *Verifier) Verify(ctx context.Context, provider, idToken string) (*VerifiedIdentity, error) {
	switch provider {
	case "google":
		return v.verifyGoogle(ctx, idToken, googleJWKSURL)
	case "apple":
		return v.verifyApple(ctx, idToken, appleJWKSURL)
	default:
		return nil, ErrUnsupported
	}
}

// verifyWithJWKSURL validates the token using the supplied JWKS URL instead of
// the hardcoded production endpoints. Used by tests to inject a local server.
func (v *Verifier) verifyWithJWKSURL(ctx context.Context, provider, idToken, jwksURL string) (*VerifiedIdentity, error) {
	switch provider {
	case "google":
		return v.verifyGoogle(ctx, idToken, jwksURL)
	case "apple":
		return v.verifyApple(ctx, idToken, jwksURL)
	default:
		return nil, ErrUnsupported
	}
}

// verifyGoogle validates a Google ID token and checks issuer/audience claims.
func (v *Verifier) verifyGoogle(ctx context.Context, idToken, jwksURL string) (*VerifiedIdentity, error) {
	if v.googleClientID == "" {
		return nil, ErrNotConfigured
	}
	claims, err := v.parseAndVerify(ctx, idToken, jwksURL, v.googleJWKSCache)
	if err != nil {
		return nil, err
	}

	iss, _ := claims["iss"].(string)
	if iss != googleIssuer1 && iss != googleIssuer2 {
		return nil, fmt.Errorf("%w: unexpected issuer %q", ErrInvalidToken, iss)
	}

	aud, err := extractAudience(claims)
	if err != nil || aud != v.googleClientID {
		return nil, fmt.Errorf("%w: audience mismatch", ErrInvalidToken)
	}

	return identityFromClaims("google", claims), nil
}

// verifyApple validates an Apple ID token and checks issuer/audience claims.
func (v *Verifier) verifyApple(ctx context.Context, idToken, jwksURL string) (*VerifiedIdentity, error) {
	if len(v.appleAudiences) == 0 {
		return nil, ErrNotConfigured
	}
	claims, err := v.parseAndVerify(ctx, idToken, jwksURL, v.appleJWKSCache)
	if err != nil {
		return nil, err
	}

	iss, _ := claims["iss"].(string)
	if iss != appleIssuer {
		return nil, fmt.Errorf("%w: unexpected issuer %q", ErrInvalidToken, iss)
	}

	aud, err := extractAudience(claims)
	if err != nil {
		return nil, fmt.Errorf("%w: audience mismatch", ErrInvalidToken)
	}
	audOK := false
	for _, a := range v.appleAudiences {
		if aud == a {
			audOK = true
			break
		}
	}
	if !audOK {
		return nil, fmt.Errorf("%w: audience mismatch", ErrInvalidToken)
	}

	return identityFromClaims("apple", claims), nil
}

// parseAndVerify fetches the JWKS (using cache), finds the matching key by kid,
// and validates the RS256 JWT signature and standard time-based claims.
func (v *Verifier) parseAndVerify(ctx context.Context, idToken, jwksURL string, cache *jwksCache) (jwt.MapClaims, error) {
	// Peek at the header to get kid without full parse.
	unverified, _, err := jwt.NewParser().ParseUnverified(idToken, jwt.MapClaims{})
	if err != nil {
		return nil, fmt.Errorf("%w: malformed token", ErrInvalidToken)
	}
	kid, _ := unverified.Header["kid"].(string)

	key, ok := cache.get(kid)
	if !ok {
		// Cache miss or stale: re-fetch JWKS.
		fetched, fetchErr := v.fetchJWKS(ctx, jwksURL)
		if fetchErr != nil {
			return nil, fmt.Errorf("%w: failed to fetch JWKS: %v", ErrInvalidToken, fetchErr)
		}
		cache.set(fetched)
		key, ok = fetched[kid]
		if !ok {
			return nil, fmt.Errorf("%w: unknown key id %q", ErrInvalidToken, kid)
		}
	}

	rsaKey := key // capture for closure
	token, err := jwt.ParseWithClaims(idToken, jwt.MapClaims{}, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodRSA); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return rsaKey, nil
	})
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidToken, err)
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok || !token.Valid {
		return nil, fmt.Errorf("%w: invalid claims", ErrInvalidToken)
	}
	return claims, nil
}

// fetchJWKS downloads the JWKS from the given URL and parses RSA public keys.
func (v *Verifier) fetchJWKS(ctx context.Context, url string) (map[string]*rsa.PublicKey, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := v.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var jwks jwksResponse
	if err := json.Unmarshal(body, &jwks); err != nil {
		return nil, fmt.Errorf("malformed JWKS response: %w", err)
	}

	keys := make(map[string]*rsa.PublicKey, len(jwks.Keys))
	for _, k := range jwks.Keys {
		if k.Kty != "RSA" {
			continue
		}
		pub, err := rsaPublicKeyFromJWK(k.N, k.E)
		if err != nil {
			return nil, fmt.Errorf("key %q: %w", k.Kid, err)
		}
		keys[k.Kid] = pub
	}
	return keys, nil
}

// rsaPublicKeyFromJWK constructs an *rsa.PublicKey from base64url-encoded n and e.
func rsaPublicKeyFromJWK(nB64, eB64 string) (*rsa.PublicKey, error) {
	nBytes, err := base64.RawURLEncoding.DecodeString(nB64)
	if err != nil {
		return nil, fmt.Errorf("decode n: %w", err)
	}
	eBytes, err := base64.RawURLEncoding.DecodeString(eB64)
	if err != nil {
		return nil, fmt.Errorf("decode e: %w", err)
	}

	n := new(big.Int).SetBytes(nBytes)
	e := new(big.Int).SetBytes(eBytes)

	return &rsa.PublicKey{N: n, E: int(e.Int64())}, nil
}

// extractAudience pulls the "aud" claim as a single string, tolerating both
// string and []interface{} representations.
func extractAudience(claims jwt.MapClaims) (string, error) {
	raw, ok := claims["aud"]
	if !ok {
		return "", errors.New("missing aud claim")
	}
	switch v := raw.(type) {
	case string:
		return v, nil
	case []any:
		if len(v) == 1 {
			if s, ok := v[0].(string); ok {
				return s, nil
			}
		}
		return "", errors.New("aud contains multiple values")
	default:
		return "", fmt.Errorf("unexpected aud type %T", raw)
	}
}

// identityFromClaims builds a VerifiedIdentity from MapClaims.
func identityFromClaims(provider string, claims jwt.MapClaims) *VerifiedIdentity {
	str := func(key string) string {
		v, _ := claims[key].(string)
		return v
	}
	return &VerifiedIdentity{
		Provider: provider,
		Subject:  str("sub"),
		Email:    str("email"),
		Name:     str("name"),
	}
}
