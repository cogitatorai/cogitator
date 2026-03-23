package connector

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/oauth2"
	"github.com/cogitatorai/cogitator/server/internal/secretstore"
)

// TokenInfo holds persisted OAuth tokens for a user+connector pair.
type TokenInfo struct {
	AccessToken  string    `yaml:"access_token" json:"access_token"`
	RefreshToken string    `yaml:"refresh_token" json:"refresh_token"`
	TokenType    string    `yaml:"token_type" json:"token_type"`
	Expiry       time.Time `yaml:"expiry,omitempty" json:"expiry,omitempty"`
	ClientID     string    `yaml:"client_id,omitempty" json:"client_id,omitempty"`
	ClientSecret string    `yaml:"client_secret,omitempty" json:"client_secret,omitempty"`
}

// pendingAuth tracks an in-flight OAuth flow.
type pendingAuth struct {
	connectorName  string
	userID         string
	config         *oauth2.Config
	redirectScheme string
	source         string // "web" when initiated from the browser dashboard
	origin         string // scheme+host for redirect-back
}

// OAuthRuntime manages OAuth2 flows and token storage for all connectors.
// Tokens are keyed by "{connectorName}:{userID}".
type OAuthRuntime struct {
	mu         sync.RWMutex
	tokens     map[string]*TokenInfo   // "connector:userID" -> token
	authErrors map[string]string       // "connector:userID" -> last auth error
	states     map[string]*pendingAuth // state -> pending auth
	store      secretstore.SecretStore
	port       int
}

// NewOAuthRuntime creates a runtime that persists tokens via the given SecretStore.
func NewOAuthRuntime(store secretstore.SecretStore, port int) *OAuthRuntime {
	o := &OAuthRuntime{
		tokens:     make(map[string]*TokenInfo),
		authErrors: make(map[string]string),
		states:     make(map[string]*pendingAuth),
		store:      store,
		port:       port,
	}
	_ = o.loadTokens()
	return o
}

func tokenKey(connectorName, userID string) string {
	return connectorName + ":" + userID
}

// StartAuth begins an OAuth flow. Returns the provider consent URL.
// callbackURL is the full URL for the OAuth callback (derived from the request by the caller).
func (o *OAuthRuntime) StartAuth(connectorName, userID string, auth AuthConfig, clientID, clientSecret, redirectScheme, source, origin, callbackURL string) (string, error) {
	state, err := randomState()
	if err != nil {
		return "", err
	}

	if callbackURL == "" {
		callbackURL = fmt.Sprintf("http://localhost:%d/api/connectors/callback", o.port)
	}

	cfg := &oauth2.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		Scopes:       auth.Scopes,
		Endpoint: oauth2.Endpoint{
			AuthURL:  auth.AuthURL,
			TokenURL: auth.TokenURL,
		},
		RedirectURL: callbackURL,
	}

	o.mu.Lock()
	o.states[state] = &pendingAuth{
		connectorName:  connectorName,
		userID:         userID,
		config:         cfg,
		redirectScheme: redirectScheme,
		source:         source,
		origin:         origin,
	}
	o.mu.Unlock()

	url := cfg.AuthCodeURL(state, oauth2.AccessTypeOffline, oauth2.ApprovalForce)
	return url, nil
}

// HandleCallback exchanges the authorization code for tokens.
// Returns (connectorName, userID, redirectScheme, source, origin, error).
func (o *OAuthRuntime) HandleCallback(code, state string) (string, string, string, string, string, error) {
	o.mu.Lock()
	pending, ok := o.states[state]
	if ok {
		delete(o.states, state)
	}
	o.mu.Unlock()

	if !ok {
		return "", "", "", "", "", errors.New("unknown or expired OAuth state")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	token, err := pending.config.Exchange(ctx, code)
	if err != nil {
		return "", "", "", "", "", fmt.Errorf("token exchange: %w", err)
	}

	info := &TokenInfo{
		AccessToken:  token.AccessToken,
		RefreshToken: token.RefreshToken,
		TokenType:    token.TokenType,
		Expiry:       token.Expiry,
		ClientID:     pending.config.ClientID,
		ClientSecret: pending.config.ClientSecret,
	}

	key := tokenKey(pending.connectorName, pending.userID)
	o.mu.Lock()
	o.tokens[key] = info
	delete(o.authErrors, key)
	o.mu.Unlock()

	if err := o.saveToken(key, info); err != nil {
		return "", "", "", "", "", fmt.Errorf("persist tokens: %w", err)
	}
	return pending.connectorName, pending.userID, pending.redirectScheme, pending.source, pending.origin, nil
}

// Status returns whether a user has a connector connected.
func (o *OAuthRuntime) Status(connectorName, userID string) bool {
	o.mu.RLock()
	defer o.mu.RUnlock()
	info := o.tokens[tokenKey(connectorName, userID)]
	return info != nil && info.RefreshToken != ""
}

// StatusDetail returns connection status and any auth error for a connector.
// An auth error is set when a token refresh fails (e.g. revoked credentials)
// and cleared on successful refresh or reconnect.
func (o *OAuthRuntime) StatusDetail(connectorName, userID string) (connected bool, authError string) {
	o.mu.RLock()
	defer o.mu.RUnlock()
	key := tokenKey(connectorName, userID)
	info := o.tokens[key]
	connected = info != nil && info.RefreshToken != ""
	authError = o.authErrors[key]
	return
}

// Client returns an authenticated HTTP client for a connector+user.
// Automatically refreshes expired tokens and persists the new ones.
// Client credentials are read from the stored token info (saved during auth).
func (o *OAuthRuntime) Client(connectorName, userID string, auth AuthConfig) (*http.Client, error) {
	key := tokenKey(connectorName, userID)
	o.mu.RLock()
	info := o.tokens[key]
	o.mu.RUnlock()

	if info == nil || info.RefreshToken == "" {
		return nil, fmt.Errorf("%s not connected. Ask the user to connect %s in Connectors", connectorName, connectorName)
	}

	cfg := &oauth2.Config{
		ClientID:     info.ClientID,
		ClientSecret: info.ClientSecret,
		Scopes:       auth.Scopes,
		Endpoint: oauth2.Endpoint{
			AuthURL:  auth.AuthURL,
			TokenURL: auth.TokenURL,
		},
	}

	token := &oauth2.Token{
		AccessToken:  info.AccessToken,
		RefreshToken: info.RefreshToken,
		TokenType:    info.TokenType,
		Expiry:       info.Expiry,
	}

	base := cfg.TokenSource(context.Background(), token)
	saving := &savingTokenSource{
		base:    base,
		runtime: o,
		key:     key,
	}
	return oauth2.NewClient(context.Background(), saving), nil
}

// Revoke disconnects a connector for a user.
func (o *OAuthRuntime) Revoke(connectorName, userID string) error {
	key := tokenKey(connectorName, userID)
	o.mu.Lock()
	delete(o.tokens, key)
	delete(o.authErrors, key)
	o.mu.Unlock()
	return o.deleteToken(key)
}

// ConnectedConnectors returns the connector names a user has connected.
func (o *OAuthRuntime) ConnectedConnectors(userID string) []string {
	o.mu.RLock()
	defer o.mu.RUnlock()
	suffix := ":" + userID
	var names []string
	for key, info := range o.tokens {
		if strings.HasSuffix(key, suffix) && info.RefreshToken != "" {
			names = append(names, strings.TrimSuffix(key, suffix))
		}
	}
	return names
}

// savingTokenSource wraps a token source to persist refreshed tokens.
type savingTokenSource struct {
	base    oauth2.TokenSource
	runtime *OAuthRuntime
	key     string
	mu      sync.Mutex
}

func (s *savingTokenSource) Token() (*oauth2.Token, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	token, err := s.base.Token()
	if err != nil {
		// Record a generic auth error so status checks can surface it.
		// Log the full error for debugging; do not expose raw oauth2 errors
		// in the API response as they may contain token endpoint details.
		slog.Warn("oauth token refresh failed", "key", s.key, "error", err)
		s.runtime.mu.Lock()
		s.runtime.authErrors[s.key] = "token refresh failed"
		s.runtime.mu.Unlock()
		return nil, err
	}

	s.runtime.mu.Lock()
	// Clear any previous auth error on successful refresh.
	delete(s.runtime.authErrors, s.key)
	info := s.runtime.tokens[s.key]
	if info != nil && info.AccessToken != token.AccessToken {
		info.AccessToken = token.AccessToken
		info.TokenType = token.TokenType
		info.Expiry = token.Expiry
		if token.RefreshToken != "" {
			info.RefreshToken = token.RefreshToken
		}
	}
	var infoCopy *TokenInfo
	if info != nil {
		cp := *info
		infoCopy = &cp
	}
	s.runtime.mu.Unlock()

	if infoCopy != nil {
		go func() { _ = s.runtime.saveToken(s.key, infoCopy) }()
	}
	return token, nil
}

func (o *OAuthRuntime) loadTokens() error {
	keys, err := o.store.List("connector")
	if err != nil {
		return err
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	for _, key := range keys {
		val, err := o.store.Get("connector", key)
		if err != nil {
			continue
		}
		var info TokenInfo
		if err := json.Unmarshal([]byte(val), &info); err != nil {
			continue
		}
		o.tokens[key] = &info
	}
	return nil
}

func (o *OAuthRuntime) saveToken(key string, info *TokenInfo) error {
	data, err := json.Marshal(info)
	if err != nil {
		return err
	}
	return o.store.Set("connector", key, string(data))
}

func (o *OAuthRuntime) deleteToken(key string) error {
	return o.store.Delete("connector", key)
}

func randomState() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
