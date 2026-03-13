package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cogitatorai/cogitator/server/internal/agent"
	"github.com/cogitatorai/cogitator/server/internal/auth"
	"github.com/cogitatorai/cogitator/server/internal/bus"
	"github.com/cogitatorai/cogitator/server/internal/database"
	"github.com/cogitatorai/cogitator/server/internal/memory"
	"github.com/cogitatorai/cogitator/server/internal/provider"
	"github.com/cogitatorai/cogitator/server/internal/session"
	"github.com/cogitatorai/cogitator/server/internal/task"
	"github.com/cogitatorai/cogitator/server/internal/tools"
	"github.com/cogitatorai/cogitator/server/internal/user"
)

// setupMultiUserRouter creates a fully wired router with auth, agent, sessions,
// memory, tasks, and user management. This is the integration test equivalent
// of the production server configuration.
func setupMultiUserRouter(t *testing.T, responses ...provider.Response) *Router {
	t.Helper()

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	db, err := database.Open(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	profilePath := filepath.Join(dir, "profile.md")
	os.WriteFile(profilePath, []byte(""), 0o644)

	mock := provider.NewMock(responses...)
	sessStore := session.NewStore(db)
	memStore := memory.NewStore(db)
	taskStore := task.NewStore(db)
	userStore := user.NewStore(db)
	jwtSvc := auth.NewJWTService("integration-test-secret", 15*time.Minute, 7*24*time.Hour)
	eventBus := bus.New()
	t.Cleanup(func() { eventBus.Close() })
	toolsReg := tools.NewRegistry("", nil)

	a := agent.New(agent.Config{
		Provider:       mock,
		Sessions:       sessStore,
		ContextBuilder: agent.NewContextBuilder(profilePath),
		EventBus:       eventBus,
		Model:          "test",
	})

	noopAgent := func(ctx context.Context, sessionKey, prompt, model, userID string) (string, error) {
		return "task result", nil
	}
	executor := task.NewExecutor(taskStore, noopAgent, nil, eventBus, nil)

	return NewRouter(RouterConfig{
		Agent:        a,
		Sessions:     sessStore,
		Memory:       memStore,
		Tasks:        taskStore,
		TaskExecutor: executor,
		Tools:        toolsReg,
		Users:        userStore,
		JWTService:   jwtSvc,
		DB:           db,
	})
}

// createUserWithToken creates a user directly via the store and returns it
// along with a valid JWT access token. This bypasses the registration flow
// for tests that need pre-existing users.
func createUserWithToken(t *testing.T, r *Router, email string, role user.Role) (*user.User, string) {
	t.Helper()
	u, err := r.users.Create(user.CreateUserInput{
		Email:    email,
		Name:     email,
		Password: "testpass",
		Role:     role,
	})
	if err != nil {
		t.Fatalf("create user %s: %v", email, err)
	}
	token, err := r.jwtSvc.GenerateAccessToken(u.ID, string(u.Role))
	if err != nil {
		t.Fatalf("generate token for %s: %v", email, err)
	}
	return u, token
}

// doRequest is a convenience helper that marshals a body (if non-nil), creates
// an httptest request with the given method/path/token, and returns the recorder.
func doRequest(t *testing.T, router *Router, method, path, token string, body interface{}) *httptest.ResponseRecorder {
	t.Helper()
	var bodyReader io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		bodyReader = bytes.NewReader(b)
	}
	req := httptest.NewRequest(method, path, bodyReader)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

// decodeJSON is a helper that decodes JSON from a recorder into the target.
func decodeJSON(t *testing.T, rec *httptest.ResponseRecorder, target interface{}) {
	t.Helper()
	if err := json.NewDecoder(rec.Body).Decode(target); err != nil {
		t.Fatalf("decode JSON: %v (body was: %s)", err, rec.Body.String())
	}
}

// TestMultiUser_FullRegistrationFlow exercises the complete registration lifecycle:
// bootstrap admin, login, create invite, register new user, chat, session isolation.
func TestMultiUser_FullRegistrationFlow(t *testing.T) {
	router := setupMultiUserRouter(t,
		provider.Response{Content: "reply for new user"},
		provider.Response{Content: "reply for admin"},
	)

	// Step 1: Bootstrap an admin user directly (simulating first-run bootstrap).
	admin, err := router.users.Create(user.CreateUserInput{
		Email:    "admin@test.com",
		Name:     "Admin",
		Password: "admin-pass",
		Role:     user.RoleAdmin,
	})
	if err != nil {
		t.Fatalf("bootstrap admin: %v", err)
	}

	// Step 2: Admin logs in via HTTP.
	t.Run("admin_login", func(t *testing.T) {
		rec := doRequest(t, router, "POST", "/api/auth/login", "", loginRequest{
			Email:    "admin@test.com",
			Password: "admin-pass",
		})
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
		var resp authResponse
		decodeJSON(t, rec, &resp)
		if resp.AccessToken == "" {
			t.Error("expected non-empty access token")
		}
	})

	// Get a fresh admin token for subsequent requests.
	adminToken, err := router.jwtSvc.GenerateAccessToken(admin.ID, string(admin.Role))
	if err != nil {
		t.Fatalf("generate admin token: %v", err)
	}

	// Step 3: Admin creates an invite code.
	var inviteCode string
	t.Run("admin_creates_invite", func(t *testing.T) {
		rec := doRequest(t, router, "POST", "/api/invite-codes", adminToken, createInviteCodeRequest{
			Role: "user",
		})
		if rec.Code != http.StatusCreated {
			t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
		}
		var ic user.InviteCode
		decodeJSON(t, rec, &ic)
		if ic.Code == "" {
			t.Fatal("expected non-empty invite code")
		}
		inviteCode = ic.Code
	})

	// Step 4: New user registers with the invite code.
	var userToken string
	t.Run("new_user_registers", func(t *testing.T) {
		rec := doRequest(t, router, "POST", "/api/auth/register", "", registerRequest{
			Email:      "alice@test.com",
			Name:       "Alice",
			Password:   "alice-pass",
			InviteCode: inviteCode,
		})
		if rec.Code != http.StatusCreated {
			t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
		}
		var resp authResponse
		decodeJSON(t, rec, &resp)
		if resp.User == nil || resp.User.Email != "alice@test.com" {
			t.Errorf("expected user alice@test.com, got %+v", resp.User)
		}
		userToken = resp.AccessToken
	})

	// Step 5: New user can chat.
	t.Run("new_user_can_chat", func(t *testing.T) {
		rec := doRequest(t, router, "POST", "/api/chat", userToken, chatRequest{
			Message: "Hello from Alice",
			Channel: "web",
			ChatID:  "alice-chat",
		})
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
		var resp chatResponse
		decodeJSON(t, rec, &resp)
		if resp.Content != "reply for new user" {
			t.Errorf("expected 'reply for new user', got %q", resp.Content)
		}
	})

	// Admin also chats to create a session.
	t.Run("admin_chats", func(t *testing.T) {
		rec := doRequest(t, router, "POST", "/api/chat", adminToken, chatRequest{
			Message: "Hello from Admin",
			Channel: "web",
			ChatID:  "admin-chat",
		})
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	// Step 6: User sees only their own sessions.
	t.Run("user_sees_own_sessions", func(t *testing.T) {
		rec := doRequest(t, router, "GET", "/api/sessions", userToken, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
		var sessions []map[string]any
		decodeJSON(t, rec, &sessions)
		if len(sessions) != 1 {
			t.Fatalf("expected 1 session for user, got %d", len(sessions))
		}
	})

	// Step 7: Admin sees only their own sessions.
	t.Run("admin_sees_own_sessions", func(t *testing.T) {
		rec := doRequest(t, router, "GET", "/api/sessions", adminToken, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
		var sessions []map[string]any
		decodeJSON(t, rec, &sessions)
		if len(sessions) != 1 {
			t.Fatalf("expected 1 session for admin, got %d", len(sessions))
		}
	})
}

// TestMultiUser_MemoryIsolation verifies that sessions created by different
// users are isolated; each user only sees their own.
func TestMultiUser_MemoryIsolation(t *testing.T) {
	router := setupMultiUserRouter(t,
		provider.Response{Content: "admin reply"},
		provider.Response{Content: "user reply"},
	)

	_, adminToken := createUserWithToken(t, router, "admin@test.com", user.RoleAdmin)
	_, userToken := createUserWithToken(t, router, "bob@test.com", user.RoleUser)

	// Admin sends a chat message, creating a session.
	t.Run("admin_chats", func(t *testing.T) {
		rec := doRequest(t, router, "POST", "/api/chat", adminToken, chatRequest{
			Message: "Admin message",
			Channel: "web",
			ChatID:  "admin-sess",
		})
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	// User sends a chat message, creating their own session.
	t.Run("user_chats", func(t *testing.T) {
		rec := doRequest(t, router, "POST", "/api/chat", userToken, chatRequest{
			Message: "User message",
			Channel: "web",
			ChatID:  "user-sess",
		})
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	// Verify session isolation.
	t.Run("admin_sessions_isolated", func(t *testing.T) {
		rec := doRequest(t, router, "GET", "/api/sessions", adminToken, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}
		var sessions []map[string]any
		decodeJSON(t, rec, &sessions)
		if len(sessions) != 1 {
			t.Errorf("expected 1 admin session, got %d", len(sessions))
		}
	})

	t.Run("user_sessions_isolated", func(t *testing.T) {
		rec := doRequest(t, router, "GET", "/api/sessions", userToken, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}
		var sessions []map[string]any
		decodeJSON(t, rec, &sessions)
		if len(sessions) != 1 {
			t.Errorf("expected 1 user session, got %d", len(sessions))
		}
	})
}

// TestMultiUser_RoleEnforcement verifies that endpoints enforce role-based
// access control correctly across admin, moderator, and regular user roles.
func TestMultiUser_RoleEnforcement(t *testing.T) {
	router := setupMultiUserRouter(t)

	_, adminToken := createUserWithToken(t, router, "admin@test.com", user.RoleAdmin)
	_, modToken := createUserWithToken(t, router, "mod@test.com", user.RoleModerator)
	_, userToken := createUserWithToken(t, router, "alice@test.com", user.RoleUser)

	t.Run("regular_user_cannot_list_users", func(t *testing.T) {
		rec := doRequest(t, router, "GET", "/api/users", userToken, nil)
		if rec.Code != http.StatusForbidden {
			t.Errorf("expected 403, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("regular_user_cannot_create_invite", func(t *testing.T) {
		rec := doRequest(t, router, "POST", "/api/invite-codes", userToken, createInviteCodeRequest{
			Role: "user",
		})
		if rec.Code != http.StatusForbidden {
			t.Errorf("expected 403, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("moderator_can_create_invite", func(t *testing.T) {
		rec := doRequest(t, router, "POST", "/api/invite-codes", modToken, createInviteCodeRequest{
			Role: "user",
		})
		if rec.Code != http.StatusCreated {
			t.Errorf("expected 201, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("admin_can_list_users", func(t *testing.T) {
		rec := doRequest(t, router, "GET", "/api/users", adminToken, nil)
		if rec.Code != http.StatusOK {
			t.Errorf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
		var resp struct {
			Users []user.User `json:"users"`
		}
		decodeJSON(t, rec, &resp)
		if len(resp.Users) != 3 {
			t.Errorf("expected 3 users, got %d", len(resp.Users))
		}
	})
}

// TestMultiUser_InviteCodeLifecycle tests invite code creation, single-use
// redemption, and expiry behavior.
func TestMultiUser_InviteCodeLifecycle(t *testing.T) {
	router := setupMultiUserRouter(t)

	admin, adminToken := createUserWithToken(t, router, "admin@test.com", user.RoleAdmin)

	// Step 1: Admin creates an invite code.
	var code string
	t.Run("create_invite_code", func(t *testing.T) {
		rec := doRequest(t, router, "POST", "/api/invite-codes", adminToken, createInviteCodeRequest{
			Role: "user",
		})
		if rec.Code != http.StatusCreated {
			t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
		}
		var ic user.InviteCode
		decodeJSON(t, rec, &ic)
		code = ic.Code
	})

	// Step 2: First user registers with the code.
	t.Run("first_registration_succeeds", func(t *testing.T) {
		rec := doRequest(t, router, "POST", "/api/auth/register", "", registerRequest{
			Email:      "first-user@test.com",
			Password:   "pass123",
			InviteCode: code,
		})
		if rec.Code != http.StatusCreated {
			t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	// Step 3: Second user tries to register with the same code.
	t.Run("second_registration_fails_already_redeemed", func(t *testing.T) {
		rec := doRequest(t, router, "POST", "/api/auth/register", "", registerRequest{
			Email:      "second-user@test.com",
			Password:   "pass123",
			InviteCode: code,
		})
		if rec.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	// Step 4: Admin creates an expired invite code.
	var expiredCode string
	t.Run("create_expired_invite", func(t *testing.T) {
		past := time.Now().Add(-1 * time.Hour)
		ic, err := router.users.CreateInviteCode(user.CreateInviteInput{
			CreatedBy: admin.ID,
			Role:      user.RoleUser,
			ExpiresAt: &past,
		})
		if err != nil {
			t.Fatalf("create expired invite: %v", err)
		}
		expiredCode = ic.Code
	})

	// Step 5: User tries to register with the expired code.
	t.Run("registration_with_expired_code_fails", func(t *testing.T) {
		rec := doRequest(t, router, "POST", "/api/auth/register", "", registerRequest{
			Email:      "third-user@test.com",
			Password:   "pass123",
			InviteCode: expiredCode,
		})
		if rec.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d: %s", rec.Code, rec.Body.String())
		}
	})
}

// TestMultiUser_ProfileOverrides verifies that a user's profile overrides
// default to empty and can be updated and retrieved.
func TestMultiUser_ProfileOverrides(t *testing.T) {
	router := setupMultiUserRouter(t)

	_, userToken := createUserWithToken(t, router, "alice@test.com", user.RoleUser)

	// Step 1: Default profile returns empty overrides.
	t.Run("default_profile_is_empty", func(t *testing.T) {
		rec := doRequest(t, router, "GET", "/api/me/profile", userToken, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
		var resp map[string]string
		decodeJSON(t, rec, &resp)
		if resp["overrides"] != "" && resp["overrides"] != "{}" {
			t.Errorf("expected empty or default '{}' overrides, got %q", resp["overrides"])
		}
	})

	// Step 2: Update profile overrides.
	overrides := `{"tone":"casual","language":"en"}`
	t.Run("update_profile_overrides", func(t *testing.T) {
		rec := doRequest(t, router, "PUT", "/api/me/profile", userToken, updateProfileRequest{
			Overrides: overrides,
		})
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
		var resp map[string]string
		decodeJSON(t, rec, &resp)
		if resp["overrides"] != overrides {
			t.Errorf("expected %q, got %q", overrides, resp["overrides"])
		}
	})

	// Step 3: Get profile returns the updated overrides.
	t.Run("get_profile_returns_updated", func(t *testing.T) {
		rec := doRequest(t, router, "GET", "/api/me/profile", userToken, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
		var resp map[string]string
		decodeJSON(t, rec, &resp)
		if resp["overrides"] != overrides {
			t.Errorf("expected %q, got %q", overrides, resp["overrides"])
		}
	})
}

// TestMultiUser_UserDeletion verifies that deleting a user cleans up their
// sessions, disables their tasks, and prevents further login.
func TestMultiUser_UserDeletion(t *testing.T) {
	router := setupMultiUserRouter(t,
		provider.Response{Content: "user reply"},
	)

	admin, adminToken := createUserWithToken(t, router, "admin@test.com", user.RoleAdmin)
	victim, victimToken := createUserWithToken(t, router, "victim@test.com", user.RoleUser)
	_ = admin

	// Victim creates a session via chat.
	t.Run("victim_creates_session", func(t *testing.T) {
		rec := doRequest(t, router, "POST", "/api/chat", victimToken, chatRequest{
			Message: "Hello",
			Channel: "web",
			ChatID:  "victim-chat",
		})
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	// Victim creates a task.
	var taskID int64
	t.Run("victim_creates_task", func(t *testing.T) {
		rec := doRequest(t, router, "POST", "/api/tasks", victimToken, map[string]interface{}{
			"name":         "victim-task",
			"prompt":       "do something",
			"allow_manual": true,
		})
		if rec.Code != http.StatusCreated {
			t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
		}
		var created task.Task
		decodeJSON(t, rec, &created)
		taskID = created.ID
	})

	// Admin deletes the victim user.
	t.Run("admin_deletes_user", func(t *testing.T) {
		rec := doRequest(t, router, "DELETE", "/api/users/"+victim.ID, adminToken, nil)
		if rec.Code != http.StatusNoContent {
			t.Fatalf("expected 204, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	// Verify: victim's sessions are gone.
	t.Run("victim_sessions_deleted", func(t *testing.T) {
		// Use admin token to list all sessions; the victim's should be gone.
		// Since session listing is user-scoped, we check directly via the store.
		sessions, err := router.sessions.List(victim.ID)
		if err != nil {
			t.Fatalf("list sessions: %v", err)
		}
		if len(sessions) != 0 {
			t.Errorf("expected 0 sessions for deleted user, got %d", len(sessions))
		}
	})

	// Verify: victim's task is disabled and reassigned.
	t.Run("victim_task_disabled", func(t *testing.T) {
		tk, err := router.tasks.GetTask(taskID)
		if err != nil {
			t.Fatalf("get task: %v", err)
		}
		if tk.Enabled {
			t.Error("expected task to be disabled after user deletion")
		}
		if tk.UserID != admin.ID {
			t.Errorf("expected task reassigned to admin %s, got %s", admin.ID, tk.UserID)
		}
	})

	// Verify: victim can no longer log in.
	t.Run("victim_cannot_login", func(t *testing.T) {
		rec := doRequest(t, router, "POST", "/api/auth/login", "", loginRequest{
			Email:    "victim@test.com",
			Password: "testpass",
		})
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("expected 401, got %d: %s", rec.Code, rec.Body.String())
		}
	})
}

// TestMultiUser_RunIsolation verifies that regular users can only see runs
// belonging to their own tasks when listing runs without a task_id filter.
func TestMultiUser_RunIsolation(t *testing.T) {
	router := setupMultiUserRouter(t)

	_, adminToken := createUserWithToken(t, router, "admin@test.com", user.RoleAdmin)
	alice, aliceToken := createUserWithToken(t, router, "alice@test.com", user.RoleUser)
	bob, bobToken := createUserWithToken(t, router, "bob@test.com", user.RoleUser)

	// Alice creates a task and a run.
	var aliceTaskID int64
	rec := doRequest(t, router, "POST", "/api/tasks", aliceToken, map[string]interface{}{
		"name": "alice-task", "prompt": "do alice stuff", "allow_manual": true,
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create alice task: %d %s", rec.Code, rec.Body.String())
	}
	var aliceTask task.Task
	decodeJSON(t, rec, &aliceTask)
	aliceTaskID = aliceTask.ID

	_ = alice
	aliceRunID, err := router.tasks.CreateRun(&task.Run{
		TaskID:  &aliceTaskID,
		Trigger: task.TriggerManual,
		Status:  task.RunStatusCompleted,
	})
	if err != nil {
		t.Fatalf("create alice run: %v", err)
	}

	// Bob creates a task and a run.
	var bobTaskID int64
	rec = doRequest(t, router, "POST", "/api/tasks", bobToken, map[string]interface{}{
		"name": "bob-task", "prompt": "do bob stuff", "allow_manual": true,
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create bob task: %d %s", rec.Code, rec.Body.String())
	}
	var bobTask task.Task
	decodeJSON(t, rec, &bobTask)
	bobTaskID = bobTask.ID

	_ = bob
	bobRunID, err := router.tasks.CreateRun(&task.Run{
		TaskID:  &bobTaskID,
		Trigger: task.TriggerManual,
		Status:  task.RunStatusCompleted,
	})
	if err != nil {
		t.Fatalf("create bob run: %v", err)
	}

	// Alice lists all runs (no task_id filter): should only see her own.
	t.Run("alice_sees_only_own_runs", func(t *testing.T) {
		rec := doRequest(t, router, "GET", "/api/runs", aliceToken, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
		var result task.RunListResult
		decodeJSON(t, rec, &result)
		if result.Total != 1 {
			t.Errorf("expected total=1 for alice, got %d", result.Total)
		}
		if len(result.Runs) != 1 {
			t.Fatalf("expected 1 run for alice, got %d", len(result.Runs))
		}
		if result.Runs[0].ID != aliceRunID {
			t.Errorf("expected alice's run %d, got %d", aliceRunID, result.Runs[0].ID)
		}
	})

	// Bob lists all runs: should only see his own.
	t.Run("bob_sees_only_own_runs", func(t *testing.T) {
		rec := doRequest(t, router, "GET", "/api/runs", bobToken, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
		var result task.RunListResult
		decodeJSON(t, rec, &result)
		if result.Total != 1 {
			t.Errorf("expected total=1 for bob, got %d", result.Total)
		}
		if len(result.Runs) != 1 {
			t.Fatalf("expected 1 run for bob, got %d", len(result.Runs))
		}
		if result.Runs[0].ID != bobRunID {
			t.Errorf("expected bob's run %d, got %d", bobRunID, result.Runs[0].ID)
		}
	})

	// Admin sees all runs.
	t.Run("admin_sees_all_runs", func(t *testing.T) {
		rec := doRequest(t, router, "GET", "/api/runs", adminToken, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
		var result task.RunListResult
		decodeJSON(t, rec, &result)
		if result.Total != 2 {
			t.Errorf("expected total=2 for admin, got %d", result.Total)
		}
		if len(result.Runs) != 2 {
			t.Fatalf("expected 2 runs for admin, got %d", len(result.Runs))
		}
	})
}

// TestMultiUser_AdminSeesAllTasks verifies that admins can see tasks created
// by other users, while regular users only see their own.
func TestMultiUser_AdminSeesAllTasks(t *testing.T) {
	router := setupMultiUserRouter(t)

	_, adminToken := createUserWithToken(t, router, "admin@test.com", user.RoleAdmin)
	_, aliceToken := createUserWithToken(t, router, "alice@test.com", user.RoleUser)
	_, bobToken := createUserWithToken(t, router, "bob@test.com", user.RoleUser)

	// Alice creates a task.
	rec := doRequest(t, router, "POST", "/api/tasks", aliceToken, map[string]any{
		"name": "alice-task", "prompt": "alice prompt", "allow_manual": true,
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create alice task: %d %s", rec.Code, rec.Body.String())
	}

	// Bob creates a task.
	rec = doRequest(t, router, "POST", "/api/tasks", bobToken, map[string]any{
		"name": "bob-task", "prompt": "bob prompt", "allow_manual": true,
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create bob task: %d %s", rec.Code, rec.Body.String())
	}

	// Alice sees only her own task.
	t.Run("alice_sees_own_tasks", func(t *testing.T) {
		rec := doRequest(t, router, "GET", "/api/tasks", aliceToken, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}
		var tasks []task.Task
		decodeJSON(t, rec, &tasks)
		if len(tasks) != 1 {
			t.Fatalf("expected 1 task for alice, got %d", len(tasks))
		}
		if tasks[0].Name != "alice-task" {
			t.Errorf("expected 'alice-task', got %q", tasks[0].Name)
		}
	})

	// Bob sees only his own task.
	t.Run("bob_sees_own_tasks", func(t *testing.T) {
		rec := doRequest(t, router, "GET", "/api/tasks", bobToken, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}
		var tasks []task.Task
		decodeJSON(t, rec, &tasks)
		if len(tasks) != 1 {
			t.Fatalf("expected 1 task for bob, got %d", len(tasks))
		}
		if tasks[0].Name != "bob-task" {
			t.Errorf("expected 'bob-task', got %q", tasks[0].Name)
		}
	})

	// Admin sees all tasks with owner names populated.
	t.Run("admin_sees_all_tasks", func(t *testing.T) {
		rec := doRequest(t, router, "GET", "/api/tasks", adminToken, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}
		var tasks []task.Task
		decodeJSON(t, rec, &tasks)
		if len(tasks) != 2 {
			t.Fatalf("expected 2 tasks for admin, got %d", len(tasks))
		}
		for _, tk := range tasks {
			if tk.OwnerName == "" {
				t.Errorf("expected owner_name to be populated for task %q, got empty", tk.Name)
			}
		}
	})
}
