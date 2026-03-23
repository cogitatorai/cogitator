package api

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"

	"github.com/cogitatorai/cogitator/server/internal/connector"
)

// validScheme matches a valid URL scheme (lowercase letters, digits, +, -, .).
var validScheme = regexp.MustCompile(`^[a-z][a-z0-9+.\-]*$`)

// builtinCredentials maps connector names to build-time injected credentials.
var builtinCredentials = map[string][2]string{
	"google": {connector.GoogleClientID, connector.GoogleClientSecret},
}

// ConnectorManager is the interface the router needs from connector.Manager.
type ConnectorManager interface {
	List() []connector.ConnectorInfo
	Status(connectorName, userID string) bool
	StatusDetail(connectorName, userID string) (bool, string)
	StartAuth(connectorName, userID, clientID, clientSecret, redirectScheme, source, origin, callbackURL string) (string, error)
	HandleCallback(code, state string) (string, string, string, string, string, error)
	Revoke(connectorName, userID string) error
	ConnectorStatuses(userID string) map[string]bool
	ConnectorDetailedStatuses(userID string) map[string]connector.ConnectorDetailedStatus
	Settings() *connector.SettingsStore
	FetchCalendarList(connectorName, userID string) ([]connector.CalendarEntry, error)
}

func (r *Router) handleConnectorsList(w http.ResponseWriter, req *http.Request) {
	if r.connectors == nil {
		writeJSON(w, http.StatusOK, []any{})
		return
	}
	userID := userIDFromRequest(req)
	connectors := r.connectors.List()
	statuses := r.connectors.ConnectorDetailedStatuses(userID)

	type connectorResponse struct {
		Name        string `json:"name"`
		DisplayName string `json:"display_name"`
		Description string `json:"description"`
		Version     string `json:"version"`
		HasAuth     bool   `json:"has_auth"`
		Connected   bool   `json:"connected"`
		Trusted     bool   `json:"trusted"`
		AuthError   string `json:"auth_error,omitempty"`
	}

	result := make([]connectorResponse, 0, len(connectors))
	for _, c := range connectors {
		s := statuses[c.Name]
		result = append(result, connectorResponse{
			Name:        c.Name,
			DisplayName: c.DisplayName,
			Description: c.Description,
			Version:     c.Version,
			HasAuth:     c.HasAuth,
			Connected:   s.Connected,
			Trusted:     c.Trusted,
			AuthError:   s.AuthError,
		})
	}
	writeJSON(w, http.StatusOK, result)
}

func (r *Router) handleConnectorStatus(w http.ResponseWriter, req *http.Request) {
	name := req.PathValue("name")
	if r.connectors == nil {
		writeJSON(w, http.StatusOK, map[string]any{"connected": false})
		return
	}
	userID := userIDFromRequest(req)
	connected, authErr := r.connectors.StatusDetail(name, userID)
	resp := map[string]any{"connected": connected}
	if authErr != "" {
		resp["auth_error"] = authErr
	}
	writeJSON(w, http.StatusOK, resp)
}

func (r *Router) handleConnectorAuthStart(w http.ResponseWriter, req *http.Request) {
	if r.connectors == nil {
		writeError(w, http.StatusServiceUnavailable, "connectors not configured")
		return
	}
	name := req.PathValue("name")
	userID := userIDFromRequest(req)

	// Look up OAuth client credentials from environment.
	clientID, clientSecret := connectorCredentials(name)
	if clientID == "" {
		writeError(w, http.StatusBadRequest, "no OAuth credentials configured for "+name)
		return
	}

	redirectScheme := req.URL.Query().Get("redirect_scheme")
	// Validate redirect_scheme to prevent open redirect via arbitrary URL schemes.
	if redirectScheme != "" && !validScheme.MatchString(redirectScheme) {
		writeError(w, http.StatusBadRequest, "invalid redirect_scheme")
		return
	}
	source := req.URL.Query().Get("source")
	origin := requestOrigin(req)
	// Only derive callback URL from request for mobile (redirect_scheme set).
	// Desktop uses the default localhost URL which is already registered with Google.
	var callbackURL string
	if redirectScheme != "" {
		callbackURL = r.connectorCallbackURL(req)
	}
	url, err := r.connectors.StartAuth(name, userID, clientID, clientSecret, redirectScheme, source, origin, callbackURL)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"url": url})
}

func (r *Router) handleConnectorCallback(w http.ResponseWriter, req *http.Request) {
	if r.connectors == nil {
		writeError(w, http.StatusServiceUnavailable, "connectors not configured")
		return
	}
	code := req.URL.Query().Get("code")
	state := req.URL.Query().Get("state")
	if code == "" || state == "" {
		writeError(w, http.StatusBadRequest, "missing code or state")
		return
	}
	connectorName, userID, redirectScheme, source, origin, err := r.connectors.HandleCallback(code, state)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Fetch calendar list in background so settings are ready when the user opens the modal.
	go func() {
		if _, err := r.connectors.FetchCalendarList(connectorName, userID); err != nil {
			slog.Warn("auto-fetch calendar list failed", "connector", connectorName, "error", err)
		}
	}()

	if redirectScheme != "" {
		redirectURL := fmt.Sprintf("%s://connectors-callback?status=success&connector=%s", redirectScheme, url.QueryEscape(connectorName))
		http.Redirect(w, req, redirectURL, http.StatusFound)
		return
	}

	// Web dashboard: redirect back to connectors page.
	if source == "web" {
		http.Redirect(w, req, origin+"/#connectors", http.StatusFound)
		return
	}

	// Desktop: show a branded page the user can close.
	displayName := r.connectorDisplayName(connectorName)
	icon := connectorIconSVG(connectorName)
	oauthBrandedPage(w, displayName+" connected successfully.", "You can close this window.", icon)
}

func (r *Router) handleConnectorDisconnect(w http.ResponseWriter, req *http.Request) {
	if r.connectors == nil {
		writeError(w, http.StatusServiceUnavailable, "connectors not configured")
		return
	}
	name := req.PathValue("name")
	userID := userIDFromRequest(req)
	if err := r.connectors.Revoke(name, userID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"disconnected": true})
}

func (r *Router) handleConnectorSettings(w http.ResponseWriter, req *http.Request) {
	if r.connectors == nil {
		writeJSON(w, http.StatusOK, map[string]any{"calendars": []any{}, "enabled_calendar_ids": []string{}})
		return
	}
	name := req.PathValue("name")
	userID := userIDFromRequest(req)
	settings := r.connectors.Settings()

	calendars := settings.GetCalendars(name, userID)
	if calendars == nil {
		calendars = []connector.CalendarEntry{}
	}
	enabled := settings.GetEnabledCalendarIDs(name, userID)
	if enabled == nil {
		enabled = []string{}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"calendars":            calendars,
		"enabled_calendar_ids": enabled,
	})
}

func (r *Router) handleConnectorSettingsUpdate(w http.ResponseWriter, req *http.Request) {
	if r.connectors == nil {
		writeError(w, http.StatusServiceUnavailable, "connectors not configured")
		return
	}
	name := req.PathValue("name")
	userID := userIDFromRequest(req)

	var body struct {
		EnabledCalendarIDs []string `json:"enabled_calendar_ids"`
	}
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if err := r.connectors.Settings().SetEnabledCalendarIDs(name, userID, body.EnabledCalendarIDs); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (r *Router) handleConnectorSettingsRefresh(w http.ResponseWriter, req *http.Request) {
	if r.connectors == nil {
		writeError(w, http.StatusServiceUnavailable, "connectors not configured")
		return
	}
	name := req.PathValue("name")
	userID := userIDFromRequest(req)

	if !r.connectors.Status(name, userID) {
		writeError(w, http.StatusBadRequest, "connector not connected")
		return
	}

	calendars, err := r.connectors.FetchCalendarList(name, userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"calendars":            calendars,
		"enabled_calendar_ids": r.connectors.Settings().GetEnabledCalendarIDs(name, userID),
	})
}

// Browser connector handlers.

func (r *Router) handleBrowserStatus(w http.ResponseWriter, req *http.Request) {
	if r.browserConnector == nil {
		writeJSON(w, http.StatusOK, map[string]any{"enabled": false, "connected": false})
		return
	}
	writeJSON(w, http.StatusOK, r.browserConnector.Status())
}

func (r *Router) handleBrowserEnable(w http.ResponseWriter, req *http.Request) {
	if r.dashboardFS == nil {
		writeError(w, http.StatusForbidden, "browser connector is only available in desktop mode")
		return
	}
	if r.browserConnector == nil {
		writeError(w, http.StatusServiceUnavailable, "browser connector not available")
		return
	}
	if err := r.browserConnector.Enable(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, r.browserConnector.Status())
}

func (r *Router) handleBrowserDisable(w http.ResponseWriter, req *http.Request) {
	if r.dashboardFS == nil {
		writeError(w, http.StatusForbidden, "browser connector is only available in desktop mode")
		return
	}
	if r.browserConnector == nil {
		writeError(w, http.StatusServiceUnavailable, "browser connector not available")
		return
	}
	if err := r.browserConnector.Disable(); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, r.browserConnector.Status())
}

// connectorCallbackURL derives the connector OAuth callback URL from the request,
// using Origin or Host headers so it works from both localhost and remote/mobile clients.
func (r *Router) connectorCallbackURL(req *http.Request) string {
	if origin := req.Header.Get("Origin"); origin != "" {
		return origin + "/api/connectors/callback"
	}
	if host := req.Host; host != "" {
		scheme := "http"
		if req.TLS != nil {
			scheme = "https"
		}
		if fwd := req.Header.Get("X-Forwarded-Proto"); fwd != "" {
			scheme = fwd
		}
		return fmt.Sprintf("%s://%s/api/connectors/callback", scheme, host)
	}
	return fmt.Sprintf("http://127.0.0.1:%d/api/connectors/callback", r.serverPort)
}

// connectorCredentials returns OAuth client_id and client_secret for a connector.
// It checks build-time injected credentials first, then falls back to environment
// variables (COGITATOR_CONNECTOR_{NAME}_CLIENT_ID / _CLIENT_SECRET).
func connectorCredentials(name string) (string, string) {
	if creds, ok := builtinCredentials[name]; ok && creds[0] != "" {
		return creds[0], creds[1]
	}
	envPrefix := "COGITATOR_CONNECTOR_" + strings.ToUpper(name) + "_"
	clientID := os.Getenv(envPrefix + "CLIENT_ID")
	clientSecret := os.Getenv(envPrefix + "CLIENT_SECRET")
	return clientID, clientSecret
}

// connectorIconSVG returns an inline SVG icon for known connectors.
func connectorIconSVG(name string) string {
	switch name {
	case "google":
		return `<svg width="28" height="28" viewBox="0 0 48 48"><path fill="#EA4335" d="M24 9.5c3.54 0 6.71 1.22 9.21 3.6l6.85-6.85C35.9 2.38 30.47 0 24 0 14.62 0 6.51 5.38 2.56 13.22l7.98 6.19C12.43 13.72 17.74 9.5 24 9.5z"/><path fill="#4285F4" d="M46.98 24.55c0-1.57-.15-3.09-.38-4.55H24v9.02h12.94c-.58 2.96-2.26 5.48-4.78 7.18l7.73 6c4.51-4.18 7.09-10.36 7.09-17.65z"/><path fill="#FBBC05" d="M10.53 28.59a14.5 14.5 0 0 1 0-9.18l-7.98-6.19a24.0 24.0 0 0 0 0 21.56l7.98-6.19z"/><path fill="#34A853" d="M24 48c6.48 0 11.93-2.13 15.89-5.81l-7.73-6c-2.15 1.45-4.92 2.3-8.16 2.3-6.26 0-11.57-4.22-13.47-9.91l-7.98 6.19C6.51 42.62 14.62 48 24 48z"/></svg>`
	default:
		return ""
	}
}

// connectorDisplayName returns the display_name for a connector, falling back to
// the internal name if not found.
func (r *Router) connectorDisplayName(name string) string {
	if r.connectors == nil {
		return name
	}
	for _, c := range r.connectors.List() {
		if c.Name == name {
			return c.DisplayName
		}
	}
	return name
}
