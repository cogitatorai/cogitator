package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/cogitatorai/cogitator/server/internal/agent"
	"github.com/cogitatorai/cogitator/server/internal/bus"
	"github.com/cogitatorai/cogitator/server/internal/database"
	"github.com/cogitatorai/cogitator/server/internal/memory"
	"github.com/cogitatorai/cogitator/server/internal/notification"
	"github.com/cogitatorai/cogitator/server/internal/provider"
	"github.com/cogitatorai/cogitator/server/internal/session"
	"github.com/cogitatorai/cogitator/server/internal/task"
	"github.com/cogitatorai/cogitator/server/internal/tools"
)

func setupTestRouter(t *testing.T, responses ...provider.Response) *Router {
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
	eventBus := bus.New()
	t.Cleanup(func() { eventBus.Close() })

	a := agent.New(agent.Config{
		Provider:       mock,
		Sessions:       sessStore,
		ContextBuilder: agent.NewContextBuilder(profilePath),
		EventBus:       eventBus,
		Model:          "test",
	})

	taskStore := task.NewStore(db)
	notifStore := notification.NewStore(db)
	toolsReg := tools.NewRegistry("", nil)

	// Create an executor with a no-op agent func for testing.
	noopAgent := func(ctx context.Context, sessionKey, prompt, model, userID string) (string, error) {
		return "test result", nil
	}
	executor := task.NewExecutor(taskStore, noopAgent, nil, eventBus, nil)

	return NewRouter(RouterConfig{
		Agent:         a,
		Sessions:      sessStore,
		Memory:        memStore,
		Tasks:         taskStore,
		Tools:         toolsReg,
		TaskExecutor:  executor,
		Notifications: notifStore,
	})
}

func TestHealthEndpoint(t *testing.T) {
	router := setupTestRouter(t)

	req := httptest.NewRequest("GET", "/api/health", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	var body map[string]string
	json.NewDecoder(w.Body).Decode(&body)
	if body["status"] != "ok" {
		t.Errorf("expected status 'ok', got %q", body["status"])
	}
}

func TestChatEndpoint(t *testing.T) {
	router := setupTestRouter(t,
		provider.Response{Content: "Hello from agent"},
	)

	payload := `{"message": "Hi there", "channel": "web", "chat_id": "test1"}`
	req := httptest.NewRequest("POST", "/api/chat", bytes.NewBufferString(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp chatResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Content != "Hello from agent" {
		t.Errorf("expected 'Hello from agent', got %q", resp.Content)
	}
	if resp.SessionKey != "web:test1" {
		t.Errorf("expected session key 'web:test1', got %q", resp.SessionKey)
	}
}

func TestChatEndpointEmptyMessage(t *testing.T) {
	router := setupTestRouter(t)

	payload := `{"message": ""}`
	req := httptest.NewRequest("POST", "/api/chat", bytes.NewBufferString(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestChatEndpointInvalidJSON(t *testing.T) {
	router := setupTestRouter(t)

	req := httptest.NewRequest("POST", "/api/chat", bytes.NewBufferString("not json"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestChatEndpointDefaultSessionKey(t *testing.T) {
	router := setupTestRouter(t,
		provider.Response{Content: "default session"},
	)

	payload := `{"message": "Hello"}`
	req := httptest.NewRequest("POST", "/api/chat", bytes.NewBufferString(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp chatResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.SessionKey != "web:default" {
		t.Errorf("expected 'web:default', got %q", resp.SessionKey)
	}
}

func TestSessionEndpoints(t *testing.T) {
	router := setupTestRouter(t,
		provider.Response{Content: "Response 1"},
	)

	payload := `{"message": "Hello", "session_key": "test-sess", "channel": "web", "chat_id": "c1"}`
	chatReq := httptest.NewRequest("POST", "/api/chat", bytes.NewBufferString(payload))
	chatReq.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, chatReq)
	if w.Code != http.StatusOK {
		t.Fatalf("chat failed: %d %s", w.Code, w.Body.String())
	}

	listReq := httptest.NewRequest("GET", "/api/sessions", nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, listReq)
	if w.Code != http.StatusOK {
		t.Fatalf("list sessions failed: %d", w.Code)
	}

	var sessions []map[string]any
	json.NewDecoder(w.Body).Decode(&sessions)
	if len(sessions) == 0 {
		t.Fatal("expected at least one session")
	}

	getReq := httptest.NewRequest("GET", "/api/sessions/test-sess", nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, getReq)
	if w.Code != http.StatusOK {
		t.Fatalf("get session failed: %d", w.Code)
	}

	var detail map[string]any
	json.NewDecoder(w.Body).Decode(&detail)
	if detail["session"] == nil {
		t.Error("expected session in response")
	}
	if detail["messages"] == nil {
		t.Error("expected messages in response")
	}

	delReq := httptest.NewRequest("DELETE", "/api/sessions/test-sess", nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, delReq)
	if w.Code != http.StatusOK {
		t.Fatalf("delete session failed: %d", w.Code)
	}

	getReq = httptest.NewRequest("GET", "/api/sessions/test-sess", nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, getReq)
	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404 after delete, got %d", w.Code)
	}
}

func TestMemoryStats(t *testing.T) {
	router := setupTestRouter(t)

	req := httptest.NewRequest("GET", "/api/memory/stats", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var stats map[string]int
	json.NewDecoder(w.Body).Decode(&stats)
	if stats["total_nodes"] != 0 {
		t.Errorf("expected 0 nodes, got %d", stats["total_nodes"])
	}
}

func TestMemoryCreateAndGetNode(t *testing.T) {
	router := setupTestRouter(t)

	body := `{"type":"fact","title":"Test fact","confidence":0.8}`
	req := httptest.NewRequest("POST", "/api/memory/nodes", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var created memory.Node
	json.NewDecoder(w.Body).Decode(&created)
	if created.ID == "" {
		t.Fatal("expected non-empty ID")
	}
	if created.Title != "Test fact" {
		t.Errorf("expected 'Test fact', got %q", created.Title)
	}

	req = httptest.NewRequest("GET", "/api/memory/nodes/"+created.ID, nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestMemoryListNodes(t *testing.T) {
	router := setupTestRouter(t)

	// Create two fact nodes
	for _, title := range []string{"Fact A", "Fact B"} {
		body := `{"type":"fact","title":"` + title + `","confidence":0.5}`
		req := httptest.NewRequest("POST", "/api/memory/nodes", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
	}

	req := httptest.NewRequest("GET", "/api/memory/nodes?type=fact", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var nodes []memory.Node
	json.NewDecoder(w.Body).Decode(&nodes)
	if len(nodes) != 2 {
		t.Errorf("expected 2 nodes, got %d", len(nodes))
	}
}

func TestMemoryDeleteNode(t *testing.T) {
	router := setupTestRouter(t)

	body := `{"type":"fact","title":"Delete me","confidence":0.5}`
	req := httptest.NewRequest("POST", "/api/memory/nodes", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	var created memory.Node
	json.NewDecoder(w.Body).Decode(&created)

	req = httptest.NewRequest("DELETE", "/api/memory/nodes/"+created.ID, nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", w.Code)
	}

	req = httptest.NewRequest("GET", "/api/memory/nodes/"+created.ID, nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404 after delete, got %d", w.Code)
	}
}

func TestMemoryNodeNotFound(t *testing.T) {
	router := setupTestRouter(t)

	req := httptest.NewRequest("GET", "/api/memory/nodes/nonexistent", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

func TestTaskCRUD(t *testing.T) {
	router := setupTestRouter(t)

	// List (empty)
	req := httptest.NewRequest("GET", "/api/tasks", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list tasks: expected 200, got %d", w.Code)
	}
	var tasks []task.Task
	json.NewDecoder(w.Body).Decode(&tasks)
	if len(tasks) != 0 {
		t.Errorf("expected 0 tasks, got %d", len(tasks))
	}

	// Create
	body := `{"name":"daily-digest","prompt":"Summarize today","cron_expr":"@daily","enabled":true,"allow_manual":true}`
	req = httptest.NewRequest("POST", "/api/tasks", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create task: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var created task.Task
	json.NewDecoder(w.Body).Decode(&created)
	if created.ID == 0 {
		t.Fatal("expected non-zero task ID")
	}
	if created.Name != "daily-digest" {
		t.Errorf("expected 'daily-digest', got %q", created.Name)
	}

	// Get
	req = httptest.NewRequest("GET", "/api/tasks/"+strconv.FormatInt(created.ID, 10), nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("get task: expected 200, got %d", w.Code)
	}

	// Update
	updateBody := `{"name":"weekly-digest","prompt":"Summarize this week"}`
	req = httptest.NewRequest("PUT", "/api/tasks/"+strconv.FormatInt(created.ID, 10), bytes.NewBufferString(updateBody))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("update task: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var updated task.Task
	json.NewDecoder(w.Body).Decode(&updated)
	if updated.Name != "weekly-digest" {
		t.Errorf("expected 'weekly-digest', got %q", updated.Name)
	}

	// Delete
	req = httptest.NewRequest("DELETE", "/api/tasks/"+strconv.FormatInt(created.ID, 10), nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("delete task: expected 204, got %d", w.Code)
	}

	// Get after delete (not found)
	req = httptest.NewRequest("GET", "/api/tasks/"+strconv.FormatInt(created.ID, 10), nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404 after delete, got %d", w.Code)
	}
}

func TestTaskCreateValidation(t *testing.T) {
	router := setupTestRouter(t)

	body := `{"name":"","prompt":""}`
	req := httptest.NewRequest("POST", "/api/tasks", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestTaskTrigger(t *testing.T) {
	router := setupTestRouter(t)

	// Create a manually triggerable task
	body := `{"name":"manual-task","prompt":"do something","allow_manual":true}`
	req := httptest.NewRequest("POST", "/api/tasks", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d", w.Code)
	}
	var created task.Task
	json.NewDecoder(w.Body).Decode(&created)

	// Trigger it (async execution, returns 202)
	req = httptest.NewRequest("POST", "/api/tasks/"+strconv.FormatInt(created.ID, 10)+"/trigger", nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusAccepted {
		t.Fatalf("trigger: expected 202, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["status"] != "accepted" {
		t.Errorf("expected status 'accepted', got %q", resp["status"])
	}
}

func TestTaskTriggerNotAllowed(t *testing.T) {
	router := setupTestRouter(t)

	body := `{"name":"no-manual","prompt":"auto only","allow_manual":false}`
	req := httptest.NewRequest("POST", "/api/tasks", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	var created task.Task
	json.NewDecoder(w.Body).Decode(&created)

	req = httptest.NewRequest("POST", "/api/tasks/"+strconv.FormatInt(created.ID, 10)+"/trigger", nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", w.Code)
	}
}

func TestTaskRunEndpoints(t *testing.T) {
	router := setupTestRouter(t)

	// Create a task via API.
	body := `{"name":"run-test","prompt":"test","allow_manual":true}`
	req := httptest.NewRequest("POST", "/api/tasks", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	var created task.Task
	json.NewDecoder(w.Body).Decode(&created)
	taskID := strconv.FormatInt(created.ID, 10)

	// Create a run record directly (trigger is async so we test the read
	// endpoints with a known run).
	run := &task.Run{TaskID: &created.ID, Trigger: task.TriggerManual}
	runIDInt, err := router.tasks.CreateRun(run)
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	runID := strconv.FormatInt(runIDInt, 10)

	// List runs for task
	req = httptest.NewRequest("GET", "/api/tasks/"+taskID+"/runs", nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list runs: expected 200, got %d", w.Code)
	}
	var runs []task.Run
	json.NewDecoder(w.Body).Decode(&runs)
	if len(runs) != 1 {
		t.Errorf("expected 1 run, got %d", len(runs))
	}

	// Get specific run
	req = httptest.NewRequest("GET", "/api/tasks/"+taskID+"/runs/"+runID, nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("get run: expected 200, got %d", w.Code)
	}

	// Recent runs
	req = httptest.NewRequest("GET", "/api/runs/recent", nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("recent runs: expected 200, got %d", w.Code)
	}
	var recent []task.Run
	json.NewDecoder(w.Body).Decode(&recent)
	if len(recent) != 1 {
		t.Errorf("expected 1 recent run, got %d", len(recent))
	}
}

func TestSystemStatus(t *testing.T) {
	router := setupTestRouter(t)

	req := httptest.NewRequest("GET", "/api/status", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var status map[string]any
	json.NewDecoder(w.Body).Decode(&status)

	if status["go_version"] == nil {
		t.Error("expected go_version in status")
	}
	if status["goroutines"] == nil {
		t.Error("expected goroutines in status")
	}
	if status["memory"] == nil {
		t.Error("expected memory in status")
	}
	if status["components"] == nil {
		t.Error("expected components in status")
	}
}

func TestToolsEndpoints(t *testing.T) {
	router := setupTestRouter(t)

	// List tools (should have built-ins)
	req := httptest.NewRequest("GET", "/api/tools", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("list tools: expected 200, got %d", w.Code)
	}

	var toolList []tools.ToolDef
	json.NewDecoder(w.Body).Decode(&toolList)
	if len(toolList) < 4 {
		t.Errorf("expected at least 4 built-in tools, got %d", len(toolList))
	}

	// Get a specific tool
	req = httptest.NewRequest("GET", "/api/tools/read_file", nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("get tool: expected 200, got %d", w.Code)
	}

	// Get nonexistent tool
	req = httptest.NewRequest("GET", "/api/tools/nonexistent", nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}

	// Delete built-in tool should fail
	req = httptest.NewRequest("DELETE", "/api/tools/read_file", nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", w.Code)
	}
}
