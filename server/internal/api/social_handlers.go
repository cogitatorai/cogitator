package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html"
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
	source         string // "web" when initiated from the browser dashboard (enables redirect-back)
	origin         string // scheme+host of the initiating page (for redirect-back to the correct host)
	redirectURI    string // OAuth redirect_uri used in the authorization request (reused in token exchange)
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

// googleCallbackURI determines the redirect URI for Google OAuth. The callback
// always targets the Go server itself (req.Host) so that the server receives
// the authorization code directly, regardless of where the dashboard is hosted.
// This is essential for client-mode where the dashboard runs on a different
// origin. After processing the callback, the server redirects the user back to
// the dashboard using the stored origin from the pending state.
func (r *Router) googleCallbackURI(req *http.Request) string {
	if r.publicURL != "" {
		return r.publicURL + "/api/auth/google/callback"
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
	source := req.URL.Query().Get("source")
	if purpose == "" {
		purpose = "login"
	}

	// Capture the origin of the initiating page so we can redirect back to
	// the correct host after the OAuth callback (important when the dashboard
	// dev server and Go server run on different ports).
	origin := requestOrigin(req)

	redirectURI := r.googleCallbackURI(req)

	socialOAuthStates.Lock()
	socialOAuthStates.m[state] = &pendingSocialAuth{
		returnTo:       returnTo,
		purpose:        purpose,
		token:          token,
		claimID:        claimID,
		inviteCode:     inviteCode,
		redirectScheme: redirectScheme,
		source:         source,
		origin:         origin,
		redirectURI:    redirectURI,
	}
	socialOAuthStates.Unlock()

	// Clean up stale states after 10 minutes.
	go func() {
		time.Sleep(10 * time.Minute)
		socialOAuthStates.Lock()
		delete(socialOAuthStates.m, state)
		socialOAuthStates.Unlock()
	}()

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
	// Reuse the redirect_uri from the authorization request; recomputing it
	// from the callback request would produce a different value when a dev
	// proxy sits between the browser and the server.
	idToken, err := r.exchangeGoogleCode(req.Context(), code, pending.redirectURI)
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

		// Web dashboard: redirect back to the app.
		if pending.source == "web" {
			returnTo := pending.returnTo
			if returnTo == "" {
				returnTo = "account"
			}
			http.Redirect(w, req, pending.origin+"/#"+returnTo, http.StatusFound)
			return
		}

		oauthBrandedPage(w, "Google account connected successfully.", "You can close this window.")
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

		// Web dashboard: redirect back to the app so claim polling picks up tokens.
		if pending.source == "web" {
			returnTo := pending.returnTo
			if returnTo == "" {
				returnTo = "login"
			}
			http.Redirect(w, req, pending.origin+"/#"+returnTo, http.StatusFound)
			return
		}

		// Desktop: show a branded page the user can close.
		if result.Error != "" {
			oauthBrandedPage(w, "Sign-in failed.", result.Error)
		} else {
			oauthBrandedPage(w, "Signed in successfully.", "You can close this window.")
		}
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
	fmt.Fprintf(w, oauthPageTemplate,
		"Sign-in Error",
		fmt.Sprintf(`<p style="color:#c0beb9">%s</p>
<a href="/#%s" style="color:#ea580c;margin-top:1rem;text-decoration:none">Go back</a>`, msg, returnTo))
}

// oauthBrandedPage renders a Cogitator-branded page for OAuth callbacks that
// cannot redirect (e.g. desktop app flows where the browser is external).
// An optional iconSVG (inline SVG markup) is displayed next to the heading.
func oauthBrandedPage(w http.ResponseWriter, heading, detail string, iconSVG ...string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)

	safeHeading := html.EscapeString(heading)
	safeDetail := html.EscapeString(detail)

	var headingHTML string
	if len(iconSVG) > 0 && iconSVG[0] != "" {
		headingHTML = fmt.Sprintf(`<div style="display:flex;align-items:center;justify-content:center;gap:0.5rem">%s<span style="font-size:1.1rem;color:#f5f4f1">%s</span></div>`, iconSVG[0], safeHeading)
	} else {
		headingHTML = fmt.Sprintf(`<p style="font-size:1.1rem;color:#f5f4f1">%s</p>`, safeHeading)
	}

	fmt.Fprintf(w, oauthPageTemplate,
		"Cogitator",
		fmt.Sprintf(`%s
<p style="color:#9b9894;margin-top:0.25rem">%s</p>`, headingHTML, safeDetail))
}

// requestOrigin extracts the scheme+host the request originated from.
// Checks Origin, then Referer, then falls back to Host.
func requestOrigin(req *http.Request) string {
	if origin := req.Header.Get("Origin"); origin != "" {
		return origin
	}
	if ref := req.Header.Get("Referer"); ref != "" {
		if u, err := url.Parse(ref); err == nil && u.Host != "" {
			return u.Scheme + "://" + u.Host
		}
	}
	if host := req.Host; host != "" {
		scheme := "http"
		if req.TLS != nil {
			scheme = "https"
		}
		if fwd := req.Header.Get("X-Forwarded-Proto"); fwd != "" {
			scheme = fwd
		}
		return scheme + "://" + host
	}
	return ""
}

// oauthPageTemplate is a minimal branded HTML shell used for OAuth callback pages.
// It accepts two format args: page title and inner HTML content.
const oauthPageTemplate = `<!DOCTYPE html>
<html><head><meta charset="utf-8"><title>%s</title>
<link href="https://fonts.googleapis.com/css2?family=Rajdhani:wght@600&display=swap" rel="stylesheet">
</head>
<body style="font-family:system-ui,-apple-system,sans-serif;display:flex;align-items:center;justify-content:center;height:100vh;margin:0;background:#141413;color:#d4d2cd;flex-direction:column">
<div style="text-align:center">
<p style="font-family:'Rajdhani',system-ui,sans-serif;font-weight:600;font-size:1.5rem;letter-spacing:0.15em;text-transform:uppercase;color:#f5f4f1;margin-bottom:1.5rem">COGITATOR</p>
%s
</div>
</body></html>`

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

