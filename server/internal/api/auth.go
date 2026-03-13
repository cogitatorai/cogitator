package api

import (
	"net/http"
	"strings"

	"github.com/cogitatorai/cogitator/server/internal/auth"
)

// jwtAuthMiddleware enforces JWT-based authentication on API and WebSocket
// endpoints. Public endpoints and static assets pass through without auth.
//
// Token sources (checked in order):
//  1. Authorization: Bearer <token> header
//  2. ?token=<token> query parameter (WebSocket handshake fallback)
func jwtAuthMiddleware(jwtSvc *auth.JWTService, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path

		// Static assets are always public.
		if !strings.HasPrefix(path, "/api/") && !strings.HasPrefix(path, "/ws") {
			next.ServeHTTP(w, r)
			return
		}

		// Public endpoints that don't need auth.
		if isPublicEndpoint(r.Method, path) {
			next.ServeHTTP(w, r)
			return
		}

		// Extract token from Authorization header or query param.
		tokenStr := extractBearerToken(r)
		if tokenStr == "" {
			writeError(w, http.StatusUnauthorized, "missing auth token")
			return
		}

		claims, err := jwtSvc.ValidateAccessToken(tokenStr)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "invalid or expired token")
			return
		}

		ctx := auth.WithUser(r.Context(), auth.ContextUser{
			ID:   claims.UserID,
			Role: claims.Role,
		})
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// isPublicEndpoint returns true for paths that do not require authentication.
func isPublicEndpoint(method, path string) bool {
	// Health check.
	if method == http.MethodGet && path == "/api/health" {
		return true
	}
	// Auth endpoints (login, register, refresh, etc.).
	if strings.HasPrefix(path, "/api/auth/") {
		return true
	}
	// OAuth connector callback (redirected from external provider, no token).
	if method == http.MethodGet && path == "/api/connectors/callback" {
		return true
	}
	// Version info (read-only, used by login/register pages to display current version).
	if method == http.MethodGet && path == "/api/version" {
		return true
	}
	return false
}

// extractBearerToken pulls a bearer token from the Authorization header,
// falling back to the "token" query parameter for WebSocket upgrades.
func extractBearerToken(r *http.Request) string {
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		return strings.TrimPrefix(h, "Bearer ")
	}
	return r.URL.Query().Get("token")
}

// requireRole creates middleware that checks the authenticated user's role
// against a whitelist. Requests from users whose role is not in the allowed
// set receive a 403 Forbidden response.
func requireRole(roles ...string) func(http.Handler) http.Handler {
	allowed := make(map[string]bool, len(roles))
	for _, r := range roles {
		allowed[r] = true
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			u, ok := auth.UserFromContext(r.Context())
			if !ok || !allowed[u.Role] {
				writeError(w, http.StatusForbidden, "forbidden")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// corsMiddleware handles CORS for browser cross-origin requests. Because all
// authentication uses Bearer tokens (not cookies), CORS is not a security
// boundary; the JWT itself gates access. Any origin is allowed so that
// remote dashboards can operate in client-mode against this server.
func corsMiddleware(_ int, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")

		if origin != "" {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")

			if r.Method == http.MethodOptions {
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
				w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
				w.Header().Set("Access-Control-Max-Age", "3600")
				w.WriteHeader(http.StatusNoContent)
				return
			}
		}

		next.ServeHTTP(w, r)
	})
}
