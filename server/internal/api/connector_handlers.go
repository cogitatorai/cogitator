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
	StartAuth(connectorName, userID, clientID, clientSecret, redirectScheme, source, origin, callbackURL string) (string, error)
	HandleCallback(code, state string) (string, string, string, string, string, error)
	Revoke(connectorName, userID string) error
	ConnectorStatuses(userID string) map[string]bool
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
	statuses := r.connectors.ConnectorStatuses(userID)

	type connectorResponse struct {
		Name        string `json:"name"`
		DisplayName string `json:"display_name"`
		Description string `json:"description"`
		Version     string `json:"version"`
		HasAuth     bool   `json:"has_auth"`
		Connected   bool   `json:"connected"`
		Trusted     bool   `json:"trusted"`
	}

	result := make([]connectorResponse, 0, len(connectors))
	for _, c := range connectors {
		result = append(result, connectorResponse{
			Name:        c.Name,
			DisplayName: c.DisplayName,
			Description: c.Description,
			Version:     c.Version,
			HasAuth:     c.HasAuth,
			Connected:   statuses[c.Name],
			Trusted:     c.Trusted,
		})
	}
	writeJSON(w, http.StatusOK, result)
}

func (r *Router) handleConnectorStatus(w http.ResponseWriter, req *http.Request) {
	name := req.PathValue("name")
	if r.connectors == nil {
		writeJSON(w, http.StatusOK, map[string]bool{"connected": false})
		return
	}
	userID := userIDFromRequest(req)
	connected := r.connectors.Status(name, userID)
	writeJSON(w, http.StatusOK, map[string]bool{"connected": connected})
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
	oauthBrandedPage(w, connectorName+" connected successfully.", "You can close this window.")
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

func (r *Router) handleBrowserSettings(w http.ResponseWriter, req *http.Request) {
	if !requireAdmin(w, req) {
		return
	}
	if r.browserConnector == nil {
		writeError(w, http.StatusServiceUnavailable, "browser connector not available")
		return
	}
	var body struct {
		Port int `json:"port"`
	}
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := r.browserConnector.UpdateConfig(body.Port); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, r.browserConnector.Status())
}

func (r *Router) handleBrowserEnable(w http.ResponseWriter, req *http.Request) {
	if !requireAdmin(w, req) {
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
	if !requireAdmin(w, req) {
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
