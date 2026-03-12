package api

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/cogitatorai/cogitator/server/internal/auth"
	"github.com/cogitatorai/cogitator/server/internal/database"
	"github.com/cogitatorai/cogitator/server/internal/user"
)

// setupAuthRouter creates a minimal router wired with a real SQLite-backed
// user store and JWT service. No other subsystems are needed for auth tests.
func setupAuthRouter(t *testing.T) (*Router, *user.Store) {
	t.Helper()

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	db, err := database.Open(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	users := user.NewStore(db)
	jwtSvc := auth.NewJWTService("test-secret-key-for-auth", 15*time.Minute, 7*24*time.Hour)

	router := NewRouter(RouterConfig{
		Users:      users,
		JWTService: jwtSvc,
	})

	return router, users
}

// createTestInviteCode creates an invite code via the store directly.
func createTestInviteCode(t *testing.T, store *user.Store, createdBy string, role user.Role) string {
	t.Helper()
	ic, err := store.CreateInviteCode(user.CreateInviteInput{
		CreatedBy: createdBy,
		Role:      role,
	})
	if err != nil {
		t.Fatalf("create invite code: %v", err)
	}
	return ic.Code
}

// createTestUser creates a user via the store directly and returns it.
func createTestUser(t *testing.T, store *user.Store, email, password string, role user.Role) *user.User {
	t.Helper()
	u, err := store.Create(user.CreateUserInput{
		Email:    email,
		Name:     email,
		Password: password,
		Role:     role,
	})
	if err != nil {
		t.Fatalf("create test user: %v", err)
	}
	return u
}

func TestRegister_HappyPath(t *testing.T) {
	router, store := setupAuthRouter(t)

	// Create an admin user to own the invite code.
	admin := createTestUser(t, store, "admin@test.com", "admin-pass", user.RoleAdmin)
	code := createTestInviteCode(t, store, admin.ID, user.RoleUser)

	payload, _ := json.Marshal(registerRequest{
		Email:      "alice@example.com",
		Name:       "Alice",
		Password:   "secret123",
		InviteCode: code,
	})

	req := httptest.NewRequest("POST", "/api/auth/register", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var resp authResponse
	json.NewDecoder(w.Body).Decode(&resp)

	if resp.User == nil {
		t.Fatal("expected user in response")
	}
	if resp.User.Email != "alice@example.com" {
		t.Errorf("expected email 'alice@example.com', got %q", resp.User.Email)
	}
	if resp.User.Name != "Alice" {
		t.Errorf("expected name 'Alice', got %q", resp.User.Name)
	}
	if resp.User.Role != user.RoleUser {
		t.Errorf("expected role 'user', got %q", resp.User.Role)
	}
	if resp.AccessToken == "" {
		t.Error("expected non-empty access_token")
	}
	if resp.RefreshToken == "" {
		t.Error("expected non-empty refresh_token")
	}
}

func TestRegister_AdminRole(t *testing.T) {
	router, store := setupAuthRouter(t)

	admin := createTestUser(t, store, "admin@test.com", "admin-pass", user.RoleAdmin)
	code := createTestInviteCode(t, store, admin.ID, user.RoleAdmin)

	payload, _ := json.Marshal(registerRequest{
		Email:      "bob@example.com",
		Password:   "secret123",
		InviteCode: code,
	})

	req := httptest.NewRequest("POST", "/api/auth/register", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var resp authResponse
	json.NewDecoder(w.Body).Decode(&resp)

	if resp.User.Role != user.RoleAdmin {
		t.Errorf("expected role 'admin', got %q", resp.User.Role)
	}
}

func TestRegister_InvalidInviteCode(t *testing.T) {
	router, _ := setupAuthRouter(t)

	payload, _ := json.Marshal(registerRequest{
		Email:      "alice@example.com",
		Password:   "secret123",
		InviteCode: "XXXX-YYYY-ZZZZ",
	})

	req := httptest.NewRequest("POST", "/api/auth/register", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestRegister_DuplicateEmail(t *testing.T) {
	router, store := setupAuthRouter(t)

	admin := createTestUser(t, store, "admin@test.com", "admin-pass", user.RoleAdmin)
	createTestUser(t, store, "alice@example.com", "pass1", user.RoleUser)
	code := createTestInviteCode(t, store, admin.ID, user.RoleUser)

	payload, _ := json.Marshal(registerRequest{
		Email:      "alice@example.com",
		Password:   "pass2",
		InviteCode: code,
	})

	req := httptest.NewRequest("POST", "/api/auth/register", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Errorf("expected 409, got %d: %s", w.Code, w.Body.String())
	}
}

func TestRegister_MissingFields(t *testing.T) {
	router, _ := setupAuthRouter(t)

	cases := []struct {
		name    string
		payload registerRequest
	}{
		{"missing email", registerRequest{Password: "pass", InviteCode: "CODE"}},
		{"missing password", registerRequest{Email: "bob@example.com", InviteCode: "CODE"}},
		{"missing invite_code", registerRequest{Email: "bob@example.com", Password: "pass"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body, _ := json.Marshal(tc.payload)
			req := httptest.NewRequest("POST", "/api/auth/register", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			if w.Code != http.StatusBadRequest {
				t.Errorf("expected 400, got %d: %s", w.Code, w.Body.String())
			}
		})
	}
}

func TestRegister_Base64InviteCode(t *testing.T) {
	router, store := setupAuthRouter(t)

	admin := createTestUser(t, store, "admin@test.com", "admin-pass", user.RoleAdmin)
	code := createTestInviteCode(t, store, admin.ID, user.RoleUser)

	// Wrap the raw code in the base64 format used by mobile clients: base64("url|code").
	mobileCode := base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("http://localhost:8888|%s", code)))

	payload, _ := json.Marshal(registerRequest{
		Email:      "carol@example.com",
		Name:       "Carol",
		Password:   "secret123",
		InviteCode: mobileCode,
	})

	req := httptest.NewRequest("POST", "/api/auth/register", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var resp authResponse
	json.NewDecoder(w.Body).Decode(&resp)

	if resp.User == nil {
		t.Fatal("expected user in response")
	}
	if resp.User.Email != "carol@example.com" {
		t.Errorf("expected email 'carol@example.com', got %q", resp.User.Email)
	}
}

func TestParseInviteCode(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{"raw code", "53C4-B1A3-74B0", "53C4-B1A3-74B0"},
		{"base64 with url", base64.StdEncoding.EncodeToString([]byte("http://localhost:8888|53C4-B1A3-74B0")), "53C4-B1A3-74B0"},
		{"base64 with https url", base64.StdEncoding.EncodeToString([]byte("https://my.server.com|ABCD-EFGH-IJKL")), "ABCD-EFGH-IJKL"},
		{"invalid base64", "not-valid-base64!!!", "not-valid-base64!!!"},
		{"base64 without pipe returns original", base64.StdEncoding.EncodeToString([]byte("nopipe")), base64.StdEncoding.EncodeToString([]byte("nopipe"))},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := parseInviteCode(tc.input)
			if got != tc.want {
				t.Errorf("parseInviteCode(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestLogin_HappyPath(t *testing.T) {
	router, store := setupAuthRouter(t)
	createTestUser(t, store, "alice@example.com", "secret123", user.RoleUser)

	payload, _ := json.Marshal(loginRequest{
		Email:    "alice@example.com",
		Password: "secret123",
	})

	req := httptest.NewRequest("POST", "/api/auth/login", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp authResponse
	json.NewDecoder(w.Body).Decode(&resp)

	if resp.User == nil {
		t.Fatal("expected user in response")
	}
	if resp.User.Email != "alice@example.com" {
		t.Errorf("expected email 'alice@example.com', got %q", resp.User.Email)
	}
	if resp.AccessToken == "" {
		t.Error("expected non-empty access_token")
	}
	if resp.RefreshToken == "" {
		t.Error("expected non-empty refresh_token")
	}
}

func TestLogin_BadPassword(t *testing.T) {
	router, store := setupAuthRouter(t)
	createTestUser(t, store, "alice@example.com", "secret123", user.RoleUser)

	payload, _ := json.Marshal(loginRequest{
		Email:    "alice@example.com",
		Password: "wrong-password",
	})

	req := httptest.NewRequest("POST", "/api/auth/login", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d: %s", w.Code, w.Body.String())
	}
}

func TestLogin_UnknownUser(t *testing.T) {
	router, _ := setupAuthRouter(t)

	payload, _ := json.Marshal(loginRequest{
		Email:    "nobody@example.com",
		Password: "secret123",
	})

	req := httptest.NewRequest("POST", "/api/auth/login", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d: %s", w.Code, w.Body.String())
	}
}

func TestRefresh_HappyPath(t *testing.T) {
	router, store := setupAuthRouter(t)
	createTestUser(t, store, "alice@example.com", "secret123", user.RoleUser)

	// Login first to get a refresh token.
	loginPayload, _ := json.Marshal(loginRequest{
		Email:    "alice@example.com",
		Password: "secret123",
	})
	loginReq := httptest.NewRequest("POST", "/api/auth/login", bytes.NewReader(loginPayload))
	loginReq.Header.Set("Content-Type", "application/json")
	loginW := httptest.NewRecorder()
	router.ServeHTTP(loginW, loginReq)

	var loginResp authResponse
	json.NewDecoder(loginW.Body).Decode(&loginResp)

	// Use the refresh token to get new tokens.
	refreshPayload, _ := json.Marshal(refreshRequest{
		RefreshToken: loginResp.RefreshToken,
	})
	req := httptest.NewRequest("POST", "/api/auth/refresh", bytes.NewReader(refreshPayload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp authResponse
	json.NewDecoder(w.Body).Decode(&resp)

	if resp.AccessToken == "" {
		t.Error("expected non-empty access_token")
	}
	if resp.RefreshToken == "" {
		t.Error("expected non-empty refresh_token")
	}
	// New refresh token should differ from the original (rotation).
	if resp.RefreshToken == loginResp.RefreshToken {
		t.Error("expected rotated refresh token to be different from original")
	}
}

func TestRefresh_InvalidToken(t *testing.T) {
	router, _ := setupAuthRouter(t)

	payload, _ := json.Marshal(refreshRequest{
		RefreshToken: "not-a-real-token",
	})

	req := httptest.NewRequest("POST", "/api/auth/refresh", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d: %s", w.Code, w.Body.String())
	}
}

func TestRefresh_ReusedTokenFails(t *testing.T) {
	router, store := setupAuthRouter(t)
	createTestUser(t, store, "alice@example.com", "secret123", user.RoleUser)

	// Login to get a refresh token.
	loginPayload, _ := json.Marshal(loginRequest{Email: "alice@example.com", Password: "secret123"})
	loginReq := httptest.NewRequest("POST", "/api/auth/login", bytes.NewReader(loginPayload))
	loginReq.Header.Set("Content-Type", "application/json")
	loginW := httptest.NewRecorder()
	router.ServeHTTP(loginW, loginReq)

	var loginResp authResponse
	json.NewDecoder(loginW.Body).Decode(&loginResp)

	// First refresh: should succeed.
	refreshPayload, _ := json.Marshal(refreshRequest{RefreshToken: loginResp.RefreshToken})
	req := httptest.NewRequest("POST", "/api/auth/refresh", bytes.NewReader(refreshPayload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("first refresh: expected 200, got %d", w.Code)
	}

	// Second refresh with the same token: should fail (token was revoked).
	refreshPayload2, _ := json.Marshal(refreshRequest{RefreshToken: loginResp.RefreshToken})
	req2 := httptest.NewRequest("POST", "/api/auth/refresh", bytes.NewReader(refreshPayload2))
	req2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, req2)

	if w2.Code != http.StatusUnauthorized {
		t.Errorf("reused token: expected 401, got %d: %s", w2.Code, w2.Body.String())
	}
}

func TestLogout_HappyPath(t *testing.T) {
	router, store := setupAuthRouter(t)
	createTestUser(t, store, "alice@example.com", "secret123", user.RoleUser)

	// Login first.
	loginPayload, _ := json.Marshal(loginRequest{Email: "alice@example.com", Password: "secret123"})
	loginReq := httptest.NewRequest("POST", "/api/auth/login", bytes.NewReader(loginPayload))
	loginReq.Header.Set("Content-Type", "application/json")
	loginW := httptest.NewRecorder()
	router.ServeHTTP(loginW, loginReq)

	var loginResp authResponse
	json.NewDecoder(loginW.Body).Decode(&loginResp)

	// Logout with the refresh token.
	logoutPayload, _ := json.Marshal(logoutRequest{RefreshToken: loginResp.RefreshToken})
	req := httptest.NewRequest("POST", "/api/auth/logout", bytes.NewReader(logoutPayload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("expected 204, got %d: %s", w.Code, w.Body.String())
	}

	// Attempting to refresh with the revoked token should fail.
	refreshPayload, _ := json.Marshal(refreshRequest{RefreshToken: loginResp.RefreshToken})
	refreshReq := httptest.NewRequest("POST", "/api/auth/refresh", bytes.NewReader(refreshPayload))
	refreshReq.Header.Set("Content-Type", "application/json")
	refreshW := httptest.NewRecorder()
	router.ServeHTTP(refreshW, refreshReq)

	if refreshW.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 after logout, got %d", refreshW.Code)
	}
}
