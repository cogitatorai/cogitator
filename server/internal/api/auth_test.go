package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/cogitatorai/cogitator/server/internal/auth"
)

// ok200 is a simple handler that always returns 200.
var ok200 = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
})

func newTestJWTService() *auth.JWTService {
	return auth.NewJWTService("test-secret-key-for-unit-tests", 15*time.Minute, 24*time.Hour)
}

func TestJWTMiddleware_ValidToken(t *testing.T) {
	jwtSvc := newTestJWTService()
	token, err := jwtSvc.GenerateAccessToken("user-42", "member")
	if err != nil {
		t.Fatalf("generating token: %v", err)
	}

	// Handler that verifies the user was injected into context.
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, ok := auth.UserFromContext(r.Context())
		if !ok {
			t.Error("expected user in context, got none")
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		if u.ID != "user-42" {
			t.Errorf("user ID = %q, want %q", u.ID, "user-42")
		}
		if u.Role != "member" {
			t.Errorf("user role = %q, want %q", u.Role, "member")
		}
		w.WriteHeader(http.StatusOK)
	})

	handler := jwtAuthMiddleware(jwtSvc, false, inner)
	req := httptest.NewRequest("GET", "/api/status", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("got status %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestJWTMiddleware_ExpiredToken(t *testing.T) {
	// Create a service with a tiny TTL so the token is already expired.
	jwtSvc := auth.NewJWTService("test-secret", -1*time.Second, 24*time.Hour)
	token, err := jwtSvc.GenerateAccessToken("user-1", "member")
	if err != nil {
		t.Fatalf("generating token: %v", err)
	}

	// Use the "real" service to validate (it will see the token as expired).
	validatorSvc := auth.NewJWTService("test-secret", 15*time.Minute, 24*time.Hour)

	handler := jwtAuthMiddleware(validatorSvc, false, ok200)
	req := httptest.NewRequest("GET", "/api/status", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("got status %d, want %d", rec.Code, http.StatusUnauthorized)
	}
	assertJSONError(t, rec, "invalid or expired token")
}

func TestJWTMiddleware_NoToken(t *testing.T) {
	jwtSvc := newTestJWTService()
	handler := jwtAuthMiddleware(jwtSvc, false, ok200)

	req := httptest.NewRequest("GET", "/api/status", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("got status %d, want %d", rec.Code, http.StatusUnauthorized)
	}
	assertJSONError(t, rec, "missing auth token")
}

func TestJWTMiddleware_PublicEndpoints(t *testing.T) {
	jwtSvc := newTestJWTService()
	handler := jwtAuthMiddleware(jwtSvc, false, ok200)

	endpoints := []struct {
		method string
		path   string
	}{
		{"GET", "/api/health"},
		{"POST", "/api/auth/login"},
		{"POST", "/api/auth/register"},
		{"POST", "/api/auth/refresh"},
	}

	for _, ep := range endpoints {
		t.Run(ep.method+" "+ep.path, func(t *testing.T) {
			req := httptest.NewRequest(ep.method, ep.path, nil)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Errorf("got status %d, want %d", rec.Code, http.StatusOK)
			}
		})
	}
}

func TestJWTMiddleware_StaticAssets(t *testing.T) {
	jwtSvc := newTestJWTService()
	handler := jwtAuthMiddleware(jwtSvc, false, ok200)

	paths := []string{"/", "/assets/app.js", "/index.html", "/favicon.ico"}

	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest("GET", path, nil)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Errorf("got status %d, want %d", rec.Code, http.StatusOK)
			}
		})
	}
}

func TestJWTMiddleware_QueryParamToken(t *testing.T) {
	jwtSvc := newTestJWTService()
	token, err := jwtSvc.GenerateAccessToken("ws-user", "member")
	if err != nil {
		t.Fatalf("generating token: %v", err)
	}

	handler := jwtAuthMiddleware(jwtSvc, false, ok200)
	req := httptest.NewRequest("GET", "/ws?token="+token, nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("got status %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestJWTMiddleware_InvalidSignature(t *testing.T) {
	// Generate with one secret, validate with another.
	issuer := auth.NewJWTService("secret-a", 15*time.Minute, 24*time.Hour)
	validator := auth.NewJWTService("secret-b", 15*time.Minute, 24*time.Hour)

	token, err := issuer.GenerateAccessToken("user-1", "member")
	if err != nil {
		t.Fatalf("generating token: %v", err)
	}

	handler := jwtAuthMiddleware(validator, false, ok200)
	req := httptest.NewRequest("GET", "/api/status", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("got status %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestJWTMiddleware_PostHealthRequiresAuth(t *testing.T) {
	jwtSvc := newTestJWTService()
	handler := jwtAuthMiddleware(jwtSvc, false, ok200)

	req := httptest.NewRequest("POST", "/api/health", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("POST /api/health should require auth, got status %d", rec.Code)
	}
}

func TestJWTMiddleware_WSWithoutTokenReturns401(t *testing.T) {
	jwtSvc := newTestJWTService()
	handler := jwtAuthMiddleware(jwtSvc, false, ok200)

	req := httptest.NewRequest("GET", "/ws", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("got status %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestCORSMiddleware(t *testing.T) {
	const port = 8484
	allowedOrigin := "http://127.0.0.1:8484"
	localhostOrigin := "http://localhost:8484"
	disallowedOrigin := "http://evil.example.com"

	t.Run("allowed origin gets CORS headers", func(t *testing.T) {
		handler := corsMiddleware(port, ok200)
		req := httptest.NewRequest("GET", "/api/status", nil)
		req.Header.Set("Origin", allowedOrigin)

		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if got := rec.Header().Get("Access-Control-Allow-Origin"); got != allowedOrigin {
			t.Errorf("Access-Control-Allow-Origin = %q, want %q", got, allowedOrigin)
		}
		if got := rec.Header().Get("Vary"); got != "Origin" {
			t.Errorf("Vary = %q, want %q", got, "Origin")
		}
		if rec.Code != 200 {
			t.Errorf("got status %d, want 200", rec.Code)
		}
	})

	t.Run("localhost origin gets CORS headers", func(t *testing.T) {
		handler := corsMiddleware(port, ok200)
		req := httptest.NewRequest("GET", "/api/status", nil)
		req.Header.Set("Origin", localhostOrigin)

		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if got := rec.Header().Get("Access-Control-Allow-Origin"); got != localhostOrigin {
			t.Errorf("Access-Control-Allow-Origin = %q, want %q", got, localhostOrigin)
		}
	})

	t.Run("any origin gets CORS headers", func(t *testing.T) {
		handler := corsMiddleware(port, ok200)
		req := httptest.NewRequest("GET", "/api/status", nil)
		req.Header.Set("Origin", disallowedOrigin)

		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if got := rec.Header().Get("Access-Control-Allow-Origin"); got != disallowedOrigin {
			t.Errorf("Access-Control-Allow-Origin = %q, want %q", got, disallowedOrigin)
		}
		if rec.Code != 200 {
			t.Errorf("got status %d, want 200", rec.Code)
		}
	})

	t.Run("no origin header passes through without CORS headers", func(t *testing.T) {
		handler := corsMiddleware(port, ok200)
		req := httptest.NewRequest("GET", "/api/status", nil)

		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
			t.Errorf("Access-Control-Allow-Origin should be empty, got %q", got)
		}
		if rec.Code != 200 {
			t.Errorf("got status %d, want 200", rec.Code)
		}
	})

	t.Run("OPTIONS preflight from allowed origin returns 204", func(t *testing.T) {
		handler := corsMiddleware(port, ok200)
		req := httptest.NewRequest("OPTIONS", "/api/status", nil)
		req.Header.Set("Origin", allowedOrigin)

		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != 204 {
			t.Errorf("got status %d, want 204", rec.Code)
		}
		if got := rec.Header().Get("Access-Control-Allow-Methods"); got == "" {
			t.Error("Access-Control-Allow-Methods should be set on preflight")
		}
		if got := rec.Header().Get("Access-Control-Allow-Headers"); got == "" {
			t.Error("Access-Control-Allow-Headers should be set on preflight")
		}
		if got := rec.Header().Get("Access-Control-Max-Age"); got != "3600" {
			t.Errorf("Access-Control-Max-Age = %q, want %q", got, "3600")
		}
	})

	t.Run("OPTIONS preflight from any origin returns 204", func(t *testing.T) {
		handler := corsMiddleware(port, ok200)
		req := httptest.NewRequest("OPTIONS", "/api/status", nil)
		req.Header.Set("Origin", disallowedOrigin)

		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != 204 {
			t.Errorf("got status %d, want 204", rec.Code)
		}
		if got := rec.Header().Get("Access-Control-Allow-Origin"); got != disallowedOrigin {
			t.Errorf("Access-Control-Allow-Origin = %q, want %q", got, disallowedOrigin)
		}
	})
}

// assertJSONError decodes the response body and checks the "error" field.
func assertJSONError(t *testing.T, rec *httptest.ResponseRecorder, want string) {
	t.Helper()
	var body map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response body: %v", err)
	}
	if body["error"] != want {
		t.Errorf("error = %q, want %q", body["error"], want)
	}
}
