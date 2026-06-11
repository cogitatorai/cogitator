package api

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/mail"
	"strings"

	"github.com/cogitatorai/cogitator/server/internal/user"
)

// parseInviteCode extracts the raw invite code from a value that may be
// either the raw code itself (e.g. "53C4-B1A3-74B0") or a base64-encoded
// composite string used by mobile clients (e.g. base64("https://host|53C4-B1A3-74B0")).
// If decoding fails or no "|" separator is found, the original value is returned.
func parseInviteCode(v string) string {
	decoded, err := base64.StdEncoding.DecodeString(v)
	if err != nil {
		return v
	}
	if i := strings.LastIndex(string(decoded), "|"); i >= 0 {
		return string(decoded[i+1:])
	}
	return v
}

// registerRequest is the JSON body for POST /api/auth/register.
type registerRequest struct {
	Email      string `json:"email"`
	Name       string `json:"name"`
	Password   string `json:"password"`
	InviteCode string `json:"invite_code"`
}

// loginRequest is the JSON body for POST /api/auth/login.
type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// refreshRequest is the JSON body for POST /api/auth/refresh.
type refreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

// logoutRequest is the JSON body for POST /api/auth/logout.
type logoutRequest struct {
	RefreshToken string `json:"refresh_token"`
}

// setupRequest is the JSON body for POST /api/auth/setup.
type setupRequest struct {
	Email    string `json:"email"`
	Name     string `json:"name"`
	Password string `json:"password"`
}

// authResponse is the JSON body returned by register, login, and refresh.
type authResponse struct {
	User         *user.User `json:"user,omitempty"`
	AccessToken  string     `json:"access_token"`
	RefreshToken string     `json:"refresh_token"`
}

// handleRegister creates a new user account via an invite code.
func (r *Router) handleRegister(w http.ResponseWriter, req *http.Request) {
	var body registerRequest
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	if body.Email == "" || body.Password == "" || body.InviteCode == "" {
		writeError(w, http.StatusBadRequest, "email, password, and invite_code are required")
		return
	}

	if _, err := mail.ParseAddress(body.Email); err != nil {
		writeError(w, http.StatusBadRequest, "invalid email address")
		return
	}

	// Brute-force protection: per-IP only. A flood of registration attempts
	// (e.g. invite-code guessing) from one source is throttled.
	ipKey := "register:ip:" + clientIP(req, r.isSaaS)
	if blocked, retry := r.authFailures.blocked(ipKey, registerIPMaxFailures, registerIPWindow); blocked {
		writeAuthThrottled(w, retry)
		return
	}

	// Step 1: Create the user with a temporary "user" role.
	u, err := r.users.Create(user.CreateUserInput{
		Email:    body.Email,
		Name:     body.Name,
		Password: body.Password,
		Role:     user.RoleUser,
	})
	if err != nil {
		if errors.Is(err, user.ErrDuplicateUser) {
			r.authFailures.fail(ipKey, registerIPWindow)
			writeError(w, http.StatusConflict, "an account with this email already exists")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to create user")
		return
	}

	// Step 2: Redeem the invite code (validates and marks as used).
	ic, err := r.users.RedeemInviteCode(parseInviteCode(body.InviteCode), u.ID)
	if err != nil {
		// Cleanup the user we just created.
		_ = r.users.Delete(u.ID)
		r.authFailures.fail(ipKey, registerIPWindow)
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

	// Step 3: Update the user's role to match the invite code if needed.
	if ic.Role != u.Role {
		if err := r.users.UpdateRole(u.ID, ic.Role); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to update user role")
			return
		}
		u.Role = ic.Role
	}

	// Step 4: Generate and issue tokens.
	r.issueTokens(w, u, http.StatusCreated)
}

// handleLogin authenticates a user with email and password.
func (r *Router) handleLogin(w http.ResponseWriter, req *http.Request) {
	var body loginRequest
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	if body.Email == "" || body.Password == "" {
		writeError(w, http.StatusBadRequest, "email and password are required")
		return
	}

	// Brute-force protection: reject if either this account or this source IP
	// has crossed the failure threshold. The 429 message is generic and
	// identical for both cases so it never reveals whether the account exists.
	accountKey := "login:account:" + strings.ToLower(strings.TrimSpace(body.Email))
	ipKey := "login:ip:" + clientIP(req, r.isSaaS)
	if blocked, retry := r.authFailures.blocked(accountKey, loginAccountMaxFailures, loginAccountWindow); blocked {
		writeAuthThrottled(w, retry)
		return
	}
	if blocked, retry := r.authFailures.blocked(ipKey, loginIPMaxFailures, loginIPWindow); blocked {
		writeAuthThrottled(w, retry)
		return
	}

	u, err := r.users.Authenticate(body.Email, body.Password)
	if err != nil {
		// Failed attempt increments both the per-account and per-IP counters.
		r.authFailures.fail(accountKey, loginAccountWindow)
		r.authFailures.fail(ipKey, loginIPWindow)
		writeError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}

	// Successful login resets the per-account counter (NOT the per-IP counter,
	// so credential stuffing across many accounts from one IP is still bounded).
	r.authFailures.reset(accountKey)

	r.issueTokens(w, u, http.StatusOK)
}

// handleRefresh rotates the refresh token and issues a new access token.
func (r *Router) handleRefresh(w http.ResponseWriter, req *http.Request) {
	var body refreshRequest
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	if body.RefreshToken == "" {
		writeError(w, http.StatusBadRequest, "refresh_token is required")
		return
	}

	// Brute-force protection: per-IP only. Guards against refresh-token
	// guessing from a single source.
	ipKey := "refresh:ip:" + clientIP(req, r.isSaaS)
	if blocked, retry := r.authFailures.blocked(ipKey, refreshIPMaxFailures, refreshIPWindow); blocked {
		writeAuthThrottled(w, retry)
		return
	}

	tokenHash := r.jwtSvc.HashToken(body.RefreshToken)

	userID, err := r.users.ValidateRefreshToken(tokenHash)
	if err != nil {
		r.authFailures.fail(ipKey, refreshIPWindow)
		writeError(w, http.StatusUnauthorized, "invalid or expired refresh token")
		return
	}

	// Revoke the old refresh token (single-use rotation).
	_ = r.users.RevokeRefreshToken(tokenHash)

	u, err := r.users.Get(userID)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "user not found")
		return
	}

	r.issueTokens(w, u, http.StatusOK)
}

// handleLogout revokes a refresh token.
func (r *Router) handleLogout(w http.ResponseWriter, req *http.Request) {
	var body logoutRequest
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	if body.RefreshToken == "" {
		writeError(w, http.StatusBadRequest, "refresh_token is required")
		return
	}

	tokenHash := r.jwtSvc.HashToken(body.RefreshToken)
	_ = r.users.RevokeRefreshToken(tokenHash)

	w.WriteHeader(http.StatusNoContent)
}

// handleNeedsSetup returns whether the server needs initial setup (no users exist).
func (r *Router) handleNeedsSetup(w http.ResponseWriter, req *http.Request) {
	count, err := r.users.Count()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to check setup status")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"needs_setup": count == 0})
}

// handleSetup creates the first admin account. Only works when no users exist.
func (r *Router) handleSetup(w http.ResponseWriter, req *http.Request) {
	// Guard: only allow setup when no users exist.
	count, err := r.users.Count()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to check setup status")
		return
	}
	if count > 0 {
		writeError(w, http.StatusConflict, "setup already completed")
		return
	}

	var body setupRequest
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	if body.Email == "" || body.Password == "" {
		writeError(w, http.StatusBadRequest, "email and password are required")
		return
	}

	if _, err := mail.ParseAddress(body.Email); err != nil {
		writeError(w, http.StatusBadRequest, "invalid email address")
		return
	}

	if len(body.Password) < 6 {
		writeError(w, http.StatusBadRequest, "password must be at least 6 characters")
		return
	}

	u, err := r.users.Create(user.CreateUserInput{
		Email:    body.Email,
		Name:     body.Name,
		Password: body.Password,
		Role:     user.RoleAdmin,
	})
	if err != nil {
		if errors.Is(err, user.ErrDuplicateUser) {
			writeError(w, http.StatusConflict, "an account with this email already exists")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to create user")
		return
	}

	r.issueTokens(w, u, http.StatusCreated)
}
