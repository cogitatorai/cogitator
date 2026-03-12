package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/cogitatorai/cogitator/server/internal/auth"
	"github.com/cogitatorai/cogitator/server/internal/social"
	"github.com/cogitatorai/cogitator/server/internal/user"
)

// pendingSocialAuth tracks an in-flight social OAuth flow.
type pendingSocialAuth struct {
	returnTo       string // URL fragment to return to after auth (e.g., "account", "login")
	purpose        string // "login" or "link"
	token          string // JWT access token for the "link" purpose (user already authenticated)
	claimID        string // client-generated ID used to poll for auth result
	inviteCode     string // invite code for registration via social login
	redirectScheme string // custom URL scheme for mobile app redirects (e.g., "cogitator")
}

// socialOAuthStates stores pending social OAuth state parameters.
var socialOAuthStates = struct {
	sync.Mutex
	m map[string]*pendingSocialAuth
}{m: make(map[string]*pendingSocialAuth)}

// pendingAuthResult holds tokens waiting to be claimed by the client.
type pendingAuthResult struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	Error        string `json:"error,omitempty"`
}

// pendingAuthResults stores auth results keyed by claim ID.
var pendingAuthResults = struct {
	sync.Mutex
	m map[string]*pendingAuthResult
}{m: make(map[string]*pendingAuthResult)}

// socialAuthRequest is the JSON body for POST /api/auth/social.
type socialAuthRequest struct {
	Provider   string `json:"provider"`
	IDToken    string `json:"id_token"`
	InviteCode string `json:"invite_code,omitempty"`
	Name       string `json:"name,omitempty"`
}

// linkOAuthRequest is the JSON body for POST /api/account/link.
type linkOAuthRequest struct {
	Provider string `json:"provider"`
	IDToken  string `json:"id_token"`
}

// issueTokens generates an access/refresh token pair for u, stores the refresh
// token, and writes an authResponse with the given HTTP status code.
// If any step fails it writes an error response and returns; callers must not
// write to w after calling this.
func (r *Router) issueTokens(w http.ResponseWriter, u *user.User, status int) {
	accessToken, err := r.jwtSvc.GenerateAccessToken(u.ID, string(u.Role))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate access token")
		return
	}
	rawRefresh, hashRefresh, expiresAt, err := r.jwtSvc.GenerateRefreshToken()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate refresh token")
		return
	}
	if err := r.users.StoreRefreshToken(hashRefresh, u.ID, expiresAt); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to store refresh token")
		return
	}
	writeJSON(w, status, authResponse{
		User:         u,
		AccessToken:  accessToken,
		RefreshToken: rawRefresh,
	})
}

// handleSocialAuth handles POST /api/auth/social.
// It verifies an ID token from Google or Apple, then either logs in an
// existing user or creates a new one (requiring an invite code).
func (r *Router) handleSocialAuth(w http.ResponseWriter, req *http.Request) {
	var body socialAuthRequest
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if body.Provider == "" || body.IDToken == "" {
		writeError(w, http.StatusBadRequest, "provider and id_token are required")
		return
	}

	identity, err := r.socialVerifier.Verify(req.Context(), body.Provider, body.IDToken)
	if err != nil {
		switch {
		case errors.Is(err, social.ErrUnsupported):
			writeError(w, http.StatusBadRequest, "unsupported provider")
		case errors.Is(err, social.ErrNotConfigured):
			writeError(w, http.StatusBadRequest, "provider not configured")
		default:
			writeError(w, http.StatusUnauthorized, "invalid id token")
		}
		return
	}

	// Existing user: just issue tokens.
	u, err := r.users.GetByOAuthLink(identity.Provider, identity.Subject)
	if err == nil {
		r.issueTokens(w, u, http.StatusOK)
		return
	}
	if !errors.Is(err, user.ErrNotFound) {
		writeError(w, http.StatusInternalServerError, "failed to look up user")
		return
	}

	// New user: require an invite code.
	if body.InviteCode == "" {
		writeError(w, http.StatusForbidden, "invite_code is required to create an account")
		return
	}

	// Check if an account with this email already exists (non-OAuth).
	if _, err := r.users.GetByEmail(identity.Email); err == nil {
		writeError(w, http.StatusConflict,
			"an account with this email already exists; log in with your password and link your social account from settings")
		return
	}

	name := body.Name
	if name == "" {
		name = identity.Name
	}
	if name == "" {
		name = identity.Email
	}

	newUser, err := r.users.CreateSocial(identity.Email, name, user.RoleUser)
	if err != nil {
		if errors.Is(err, user.ErrDuplicateUser) {
			writeError(w, http.StatusConflict,
				"an account with this email already exists; log in with your password and link your social account from settings")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to create user")
		return
	}

	// Redeem the invite code.
	ic, err := r.users.RedeemInviteCode(parseInviteCode(body.InviteCode), newUser.ID)
	if err != nil {
		_ = r.users.Delete(newUser.ID)
		switch {
		case errors.Is(err, user.ErrCodeNotFound):
			writeError(w, http.StatusBadRequest, "invalid invite code")
		case errors.Is(err, user.ErrCodeRedeemed):
			writeError(w, http.StatusBadRequest, "invite code already redeemed")
		case errors.Is(err, user.ErrCodeExpired):
			writeError(w, http.StatusBadRequest, "invite code expired")
		default:
			writeError(w, http.StatusInternalServerError, "failed to redeem invite code")
		}
		return
	}

	// Apply the role from the invite code if it differs from the default.
	if ic.Role != newUser.Role {
		if err := r.users.UpdateRole(newUser.ID, ic.Role); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to update user role")
			return
		}
		newUser.Role = ic.Role
	}

	// Link the OAuth identity to the new user.
	if err := r.users.LinkOAuth(newUser.ID, identity.Provider, identity.Subject, identity.Email); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to link oauth identity")
		return
	}

	r.issueTokens(w, newUser, http.StatusCreated)
}

// handleLinkOAuth handles POST /api/account/link (authenticated).
// It verifies an ID token and links that provider identity to the caller.
func (r *Router) handleLinkOAuth(w http.ResponseWriter, req *http.Request) {
	caller, ok := auth.UserFromContext(req.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var body linkOAuthRequest
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if body.Provider == "" || body.IDToken == "" {
		writeError(w, http.StatusBadRequest, "provider and id_token are required")
		return
	}

	identity, err := r.socialVerifier.Verify(req.Context(), body.Provider, body.IDToken)
	if err != nil {
		switch {
		case errors.Is(err, social.ErrUnsupported):
			writeError(w, http.StatusBadRequest, "unsupported provider")
		case errors.Is(err, social.ErrNotConfigured):
			writeError(w, http.StatusBadRequest, "provider not configured")
		default:
			writeError(w, http.StatusUnauthorized, "invalid id token")
		}
		return
	}

	// Check the identity is not already linked to a different account.
	existing, err := r.users.GetByOAuthLink(identity.Provider, identity.Subject)
	if err != nil && !errors.Is(err, user.ErrNotFound) {
		writeError(w, http.StatusInternalServerError, "failed to check existing link")
		return
	}
	if err == nil && existing.ID != caller.ID {
		writeError(w, http.StatusConflict, "provider identity is already linked to another account")
		return
	}
	if err == nil && existing.ID == caller.ID {
		// Already linked to this user; nothing to do.
		w.WriteHeader(http.StatusNoContent)
		return
	}

	if err := r.users.LinkOAuth(caller.ID, identity.Provider, identity.Subject, identity.Email); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to link oauth identity")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// handleUnlinkOAuth handles DELETE /api/account/link/{provider} (authenticated).
// It prevents the caller from being locked out: they must retain either a
// password or at least one other linked provider.
func (r *Router) handleUnlinkOAuth(w http.ResponseWriter, req *http.Request) {
	caller, ok := auth.UserFromContext(req.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	provider := req.PathValue("provider")
	if provider == "" {
		writeError(w, http.StatusBadRequest, "provider is required")
		return
	}

	hasPassword, err := r.users.HasPassword(caller.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to check password")
		return
	}

	links, err := r.users.ListOAuthLinks(caller.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list oauth links")
		return
	}

	// Count links excluding the one being removed.
	otherLinks := 0
	for _, l := range links {
		if l.Provider != provider {
			otherLinks++
		}
	}

	if !hasPassword && otherLinks == 0 {
		writeError(w, http.StatusBadRequest, "cannot unlink: account has no password and no other linked providers")
		return
	}

	if err := r.users.UnlinkOAuth(caller.ID, provider); err != nil {
		if errors.Is(err, user.ErrLinkNotFound) {
			writeError(w, http.StatusNotFound, "provider link not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to unlink oauth identity")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// handleListOAuthLinks handles GET /api/account/links (authenticated).
// It returns all OAuth links for the authenticated user.
func (r *Router) handleListOAuthLinks(w http.ResponseWriter, req *http.Request) {
	caller, ok := auth.UserFromContext(req.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	links, err := r.users.ListOAuthLinks(caller.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list oauth links")
		return
	}

	if links == nil {
		links = []user.OAuthLink{}
	}
	writeJSON(w, http.StatusOK, links)
}

// handleAuthProviders handles GET /api/auth/providers (public).
// It returns which social providers are configured so the frontend can decide
// which sign-in buttons to show.
func (r *Router) handleAuthProviders(w http.ResponseWriter, _ *http.Request) {
	type providerInfo struct {
		Google   bool   `json:"google"`
		GoogleID string `json:"google_client_id,omitempty"`
		Apple    bool   `json:"apple"`
	}
	info := providerInfo{}
	if r.socialVerifier != nil {
		if r.googleClientID != "" {
			info.Google = true
			info.GoogleID = r.googleClientID
		}
		if r.appleServicesID != "" {
			info.Apple = true
		}
	}
	writeJSON(w, http.StatusOK, info)
}

// googleCallbackURI determines the redirect URI for Google OAuth based on the
// request. It uses the Origin header if present, otherwise derives from the
// Host header, falling back to localhost.
func (r *Router) googleCallbackURI(req *http.Request) string {
	// Dashboard/local requests include an Origin header; use it directly.
	if origin := req.Header.Get("Origin"); origin != "" {
		return origin + "/api/auth/google/callback"
	}
	if host := req.Host; host != "" {
		scheme := "http"
		if req.TLS != nil {
			scheme = "https"
		}
		if fwd := req.Header.Get("X-Forwarded-Proto"); fwd != "" {
			scheme = fwd
		}
		return fmt.Sprintf("%s://%s/api/auth/google/callback", scheme, host)
	}
	return fmt.Sprintf("http://127.0.0.1:%d/api/auth/google/callback", r.serverPort)
}

// handleGoogleAuthStart handles GET /api/auth/google/start.
// It builds a Google OAuth2 authorization URL and redirects the user there.
// Query params: return_to (URL fragment), purpose ("login"|"link"), token (JWT for link purpose).
func (r *Router) handleGoogleAuthStart(w http.ResponseWriter, req *http.Request) {
	if r.googleClientID == "" {
		writeError(w, http.StatusBadRequest, "Google sign-in not configured")
		return
	}

	stateBytes := make([]byte, 16)
	_, _ = rand.Read(stateBytes)
	state := hex.EncodeToString(stateBytes)

	returnTo := req.URL.Query().Get("return_to")
	purpose := req.URL.Query().Get("purpose")
	token := req.URL.Query().Get("token")
	claimID := req.URL.Query().Get("claim_id")
	inviteCode := req.URL.Query().Get("invite_code")
	redirectScheme := req.URL.Query().Get("redirect_scheme")
	if purpose == "" {
		purpose = "login"
	}

	socialOAuthStates.Lock()
	socialOAuthStates.m[state] = &pendingSocialAuth{
		returnTo:       returnTo,
		purpose:        purpose,
		token:          token,
		claimID:        claimID,
		inviteCode:     inviteCode,
		redirectScheme: redirectScheme,
	}
	socialOAuthStates.Unlock()

	// Clean up stale states after 10 minutes.
	go func() {
		time.Sleep(10 * time.Minute)
		socialOAuthStates.Lock()
		delete(socialOAuthStates.m, state)
		socialOAuthStates.Unlock()
	}()

	redirectURI := r.googleCallbackURI(req)

	params := fmt.Sprintf(
		"client_id=%s&redirect_uri=%s&response_type=code&scope=openid+email+profile&state=%s&access_type=online&prompt=select_account",
		url.QueryEscape(r.googleClientID),
		url.QueryEscape(redirectURI),
		url.QueryEscape(state),
	)
	http.Redirect(w, req, "https://accounts.google.com/o/oauth2/v2/auth?"+params, http.StatusFound)
}

// handleGoogleCallback handles GET /api/auth/google/callback.
// Google redirects here with an authorization code. We exchange it for an
// id_token, then redirect back to the dashboard with the token.
func (r *Router) handleGoogleCallback(w http.ResponseWriter, req *http.Request) {
	code := req.URL.Query().Get("code")
	state := req.URL.Query().Get("state")
	if code == "" || state == "" {
		writeError(w, http.StatusBadRequest, "missing code or state")
		return
	}

	socialOAuthStates.Lock()
	pending, ok := socialOAuthStates.m[state]
	if ok {
		delete(socialOAuthStates.m, state)
	}
	socialOAuthStates.Unlock()

	if !ok {
		writeError(w, http.StatusBadRequest, "unknown or expired state")
		return
	}

	// Exchange the code for tokens (including id_token).
	redirectURI := r.googleCallbackURI(req)

	idToken, err := r.exchangeGoogleCode(req.Context(), code, redirectURI)
	if err != nil {
		socialErrorPage(w, "Failed to exchange authorization code: "+err.Error(), pending.returnTo)
		return
	}

	// For "link" purpose with a stored JWT, complete the linking server-side
	// so the client doesn't need to be authenticated when it receives the redirect.
	if pending.purpose == "link" && pending.token != "" {
		claims, err := r.jwtSvc.ValidateAccessToken(pending.token)
		if err != nil {
			socialErrorPage(w, "Session expired. Please sign in again and retry.", pending.returnTo)
			return
		}

		identity, err := r.socialVerifier.Verify(req.Context(), "google", idToken)
		if err != nil {
			socialErrorPage(w, "Failed to verify Google identity: "+err.Error(), pending.returnTo)
			return
		}

		// Check the identity is not already linked to a different account.
		existing, err := r.users.GetByOAuthLink(identity.Provider, identity.Subject)
		if err != nil && !errors.Is(err, user.ErrNotFound) {
			socialErrorPage(w, "Failed to check existing link", pending.returnTo)
			return
		}
		if err == nil && existing.ID != claims.UserID {
			socialErrorPage(w, "This Google account is already linked to another user.", pending.returnTo)
			return
		}

		// Link (or skip if already linked to the same user).
		if errors.Is(err, user.ErrNotFound) {
			if linkErr := r.users.LinkOAuth(claims.UserID, identity.Provider, identity.Subject, identity.Email); linkErr != nil {
				socialErrorPage(w, "Failed to link Google account.", pending.returnTo)
				return
			}
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `<!DOCTYPE html>
<html><head><title>Connected</title></head>
<body style="font-family:system-ui;display:flex;align-items:center;justify-content:center;height:100vh;margin:0;background:#18181b;color:#e4e4e7">
<p>Google account connected successfully. You can close this window.</p>
</body></html>`)
		return
	}

	// For "login" purpose with a claim ID, complete authentication server-side
	// and store the tokens for the client to claim on redirect.
	if pending.claimID != "" {
		result := r.completeSocialLogin(req.Context(), "google", idToken, pending.inviteCode)
		pendingAuthResults.Lock()
		pendingAuthResults.m[pending.claimID] = result
		pendingAuthResults.Unlock()

		// Auto-expire after 5 minutes.
		go func() {
			time.Sleep(5 * time.Minute)
			pendingAuthResults.Lock()
			delete(pendingAuthResults.m, pending.claimID)
			pendingAuthResults.Unlock()
		}()

		// Mobile app: redirect to the custom URL scheme so the in-app browser
		// closes automatically and the app can claim the tokens.
		if pending.redirectScheme != "" {
			status := "success"
			if result.Error != "" {
				status = "error"
			}
			redirectURL := fmt.Sprintf("%s://auth-callback?claim_id=%s&status=%s",
				pending.redirectScheme, pending.claimID, status)
			http.Redirect(w, req, redirectURL, http.StatusFound)
			return
		}

		// Desktop: show a static success page.
		msg := "Signed in successfully. You can close this window."
		if result.Error != "" {
			msg = result.Error
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, `<!DOCTYPE html>
<html><head><title>Sign In</title></head>
<body style="font-family:system-ui;display:flex;align-items:center;justify-content:center;height:100vh;margin:0;background:#18181b;color:#e4e4e7">
<p>%s</p>
</body></html>`, msg)
		return
	}

	// Fallback: redirect back with the id_token in the hash fragment.
	fragment := pending.returnTo
	if fragment == "" {
		fragment = "login"
	}
	redirectURL := fmt.Sprintf("/#%s?google_id_token=%s&purpose=%s", fragment, idToken, pending.purpose)
	http.Redirect(w, req, redirectURL, http.StatusFound)
}

// exchangeGoogleCode exchanges an authorization code for an id_token using
// Google's token endpoint.
func (r *Router) exchangeGoogleCode(ctx context.Context, code, redirectURI string) (string, error) {
	body := fmt.Sprintf(
		"code=%s&client_id=%s&client_secret=%s&redirect_uri=%s&grant_type=authorization_code",
		url.QueryEscape(code), url.QueryEscape(r.googleClientID), url.QueryEscape(r.googleClientSecret), url.QueryEscape(redirectURI),
	)

	reqCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	httpReq, err := http.NewRequestWithContext(reqCtx, "POST", "https://oauth2.googleapis.com/token", strings.NewReader(body))
	if err != nil {
		return "", err
	}
	httpReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("token endpoint returned %d: %s", resp.StatusCode, string(respBody))
	}

	var tokenResp struct {
		IDToken string `json:"id_token"`
	}
	if err := json.Unmarshal(respBody, &tokenResp); err != nil {
		return "", err
	}
	if tokenResp.IDToken == "" {
		return "", fmt.Errorf("no id_token in response")
	}
	return tokenResp.IDToken, nil
}

// socialErrorPage renders a simple error page that redirects back to the dashboard.
func socialErrorPage(w http.ResponseWriter, msg, returnTo string) {
	if returnTo == "" {
		returnTo = "login"
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, `<!DOCTYPE html>
<html><head><title>Sign-in Error</title></head>
<body style="font-family:system-ui;display:flex;align-items:center;justify-content:center;height:100vh;margin:0;background:#18181b;color:#e4e4e7;flex-direction:column">
<p>%s</p>
<a href="/#%s" style="color:#ea580c;margin-top:1rem">Go back</a>
</body></html>`, msg, returnTo)
}

// completeSocialLogin verifies the id_token, finds or creates the user, and
// returns auth tokens (or an error) as a pendingAuthResult.
func (r *Router) completeSocialLogin(ctx context.Context, provider, idToken, inviteCode string) *pendingAuthResult {
	identity, err := r.socialVerifier.Verify(ctx, provider, idToken)
	if err != nil {
		return &pendingAuthResult{Error: "Failed to verify identity"}
	}

	// Existing user: issue tokens.
	u, err := r.users.GetByOAuthLink(identity.Provider, identity.Subject)
	if err == nil {
		return r.generateAuthResult(u)
	}
	if !errors.Is(err, user.ErrNotFound) {
		return &pendingAuthResult{Error: "Failed to look up user"}
	}

	// New user: if this is initial setup (no users), skip invite code and grant admin.
	// Otherwise, require an invite code.
	userCount, countErr := r.users.Count()
	if countErr != nil {
		return &pendingAuthResult{Error: "Failed to check setup status"}
	}

	isSetup := userCount == 0

	if !isSetup && inviteCode == "" {
		return &pendingAuthResult{Error: "An invite code is required to create an account. Please register first."}
	}

	// Check if an account with this email already exists (non-OAuth).
	if _, err := r.users.GetByEmail(identity.Email); err == nil {
		return &pendingAuthResult{Error: "An account with this email already exists. Log in with your password and link your social account from settings."}
	}

	name := identity.Name
	if name == "" {
		name = identity.Email
	}

	role := user.RoleUser
	if isSetup {
		role = user.RoleAdmin
	}

	newUser, err := r.users.CreateSocial(identity.Email, name, role)
	if err != nil {
		if errors.Is(err, user.ErrDuplicateUser) {
			return &pendingAuthResult{Error: "An account with this email already exists. Log in with your password and link your social account from settings."}
		}
		return &pendingAuthResult{Error: "Failed to create user"}
	}

	if !isSetup {
		ic, err := r.users.RedeemInviteCode(parseInviteCode(inviteCode), newUser.ID)
		if err != nil {
			_ = r.users.Delete(newUser.ID)
			switch {
			case errors.Is(err, user.ErrCodeNotFound):
				return &pendingAuthResult{Error: "Invalid invite code"}
			case errors.Is(err, user.ErrCodeRedeemed):
				return &pendingAuthResult{Error: "Invite code already redeemed"}
			case errors.Is(err, user.ErrCodeExpired):
				return &pendingAuthResult{Error: "Invite code expired"}
			default:
				return &pendingAuthResult{Error: "Failed to redeem invite code"}
			}
		}

		if ic.Role != newUser.Role {
			if err := r.users.UpdateRole(newUser.ID, ic.Role); err != nil {
				return &pendingAuthResult{Error: "Failed to update user role"}
			}
			newUser.Role = ic.Role
		}
	}

	if err := r.users.LinkOAuth(newUser.ID, identity.Provider, identity.Subject, identity.Email); err != nil {
		return &pendingAuthResult{Error: "Failed to link identity"}
	}

	return r.generateAuthResult(newUser)
}

// generateAuthResult creates JWT tokens for a user.
func (r *Router) generateAuthResult(u *user.User) *pendingAuthResult {
	accessToken, err := r.jwtSvc.GenerateAccessToken(u.ID, string(u.Role))
	if err != nil {
		return &pendingAuthResult{Error: "Failed to generate access token"}
	}
	rawRefresh, hashRefresh, expiresAt, err := r.jwtSvc.GenerateRefreshToken()
	if err != nil {
		return &pendingAuthResult{Error: "Failed to generate refresh token"}
	}
	if err := r.users.StoreRefreshToken(hashRefresh, u.ID, expiresAt); err != nil {
		return &pendingAuthResult{Error: "Failed to store refresh token"}
	}
	return &pendingAuthResult{
		AccessToken:  accessToken,
		RefreshToken: rawRefresh,
	}
}

// handleAuthClaim handles GET /api/auth/claim/{id} (public).
// The client polls this endpoint after starting a Google OAuth flow.
// Returns 202 if the result is not yet available, or 200 with the tokens.
func (r *Router) handleAuthClaim(w http.ResponseWriter, req *http.Request) {
	id := req.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "claim id is required")
		return
	}

	pendingAuthResults.Lock()
	result, ok := pendingAuthResults.m[id]
	if ok {
		delete(pendingAuthResults.m, id)
	}
	pendingAuthResults.Unlock()

	if !ok {
		w.WriteHeader(http.StatusAccepted)
		return
	}

	if result.Error != "" {
		writeError(w, http.StatusBadRequest, result.Error)
		return
	}

	writeJSON(w, http.StatusOK, result)
}

