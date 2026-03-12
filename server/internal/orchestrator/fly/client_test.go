package fly

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// helper: start a test server that records the last request and responds with
// the given status and JSON body.
func newTestServer(t *testing.T, status int, response any) (*httptest.Server, *http.Request, *[]byte) {
	t.Helper()
	var lastReq *http.Request
	var lastBody []byte

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lastReq = r.Clone(r.Context())
		if r.Body != nil {
			b, _ := io.ReadAll(r.Body)
			lastBody = b
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		if response != nil {
			json.NewEncoder(w).Encode(response)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, lastReq, &lastBody
}

func testClient(srv *httptest.Server) *Client {
	return &Client{
		token:   "test-token",
		appName: "test-app",
		baseURL: srv.URL,
		client:  srv.Client(),
	}
}

func TestCreateVolume(t *testing.T) {
	want := Volume{ID: "vol_123", Name: "data", SizeGB: 3, State: "created"}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/apps/test-app/volumes" {
			t.Errorf("path = %s, want /apps/test-app/volumes", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Errorf("missing or wrong auth header")
		}

		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)

		if body["name"] != "data" {
			t.Errorf("body name = %v, want data", body["name"])
		}
		if body["size_gb"] != float64(3) {
			t.Errorf("body size_gb = %v, want 3", body["size_gb"])
		}
		if body["region"] != "iad" {
			t.Errorf("body region = %v, want iad", body["region"])
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(want)
	}))
	t.Cleanup(srv.Close)

	c := testClient(srv)
	got, err := c.CreateVolume("data", 3, "iad")
	if err != nil {
		t.Fatalf("CreateVolume: %v", err)
	}
	if *got != want {
		t.Errorf("got %+v, want %+v", *got, want)
	}
}

func TestCreateMachine(t *testing.T) {
	want := Machine{ID: "m_abc", Name: "tenant-42", State: "started", Region: "iad"}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/apps/test-app/machines" {
			t.Errorf("path = %s, want /apps/test-app/machines", r.URL.Path)
		}

		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)

		if body["region"] != "iad" {
			t.Errorf("region = %v, want iad", body["region"])
		}

		cfg, _ := body["config"].(map[string]any)
		if cfg["image"] != "registry.fly.io/cogitator:v1" {
			t.Errorf("image = %v, want registry.fly.io/cogitator:v1", cfg["image"])
		}

		env, _ := cfg["env"].(map[string]any)
		if env["TENANT_ID"] != "42" {
			t.Errorf("env TENANT_ID = %v, want 42", env["TENANT_ID"])
		}

		services, _ := cfg["services"].([]any)
		if len(services) != 1 {
			t.Fatalf("services len = %d, want 1", len(services))
		}
		svc, _ := services[0].(map[string]any)
		if svc["internal_port"] != float64(8484) {
			t.Errorf("internal_port = %v, want 8484", svc["internal_port"])
		}

		mounts, _ := cfg["mounts"].([]any)
		if len(mounts) != 1 {
			t.Fatalf("mounts len = %d, want 1", len(mounts))
		}
		mount, _ := mounts[0].(map[string]any)
		if mount["volume"] != "vol_123" {
			t.Errorf("mount volume = %v, want vol_123", mount["volume"])
		}
		if mount["path"] != "/data" {
			t.Errorf("mount path = %v, want /data", mount["path"])
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(want)
	}))
	t.Cleanup(srv.Close)

	c := testClient(srv)
	got, err := c.CreateMachine(MachineConfig{
		Name:     "tenant-42",
		Image:    "registry.fly.io/cogitator:v1",
		CPUs:     1,
		MemoryMB: 256,
		Env:      map[string]string{"TENANT_ID": "42"},
		VolumeID: "vol_123",
	}, "iad")
	if err != nil {
		t.Fatalf("CreateMachine: %v", err)
	}
	if *got != want {
		t.Errorf("got %+v, want %+v", *got, want)
	}
}

func TestGetMachine(t *testing.T) {
	want := Machine{ID: "m_abc", Name: "tenant-42", State: "started", Region: "iad"}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", r.Method)
		}
		if r.URL.Path != "/apps/test-app/machines/m_abc" {
			t.Errorf("path = %s, want /apps/test-app/machines/m_abc", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(want)
	}))
	t.Cleanup(srv.Close)

	c := testClient(srv)
	got, err := c.GetMachine("m_abc")
	if err != nil {
		t.Fatalf("GetMachine: %v", err)
	}
	if *got != want {
		t.Errorf("got %+v, want %+v", *got, want)
	}
}

func TestDestroyMachine(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("method = %s, want DELETE", r.Method)
		}
		if r.URL.Path != "/apps/test-app/machines/m_abc" {
			t.Errorf("path = %s, want /apps/test-app/machines/m_abc", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	c := testClient(srv)
	if err := c.DestroyMachine("m_abc"); err != nil {
		t.Fatalf("DestroyMachine: %v", err)
	}
}

func TestErrorHandling(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"error":"machine not found"}`))
	}))
	t.Cleanup(srv.Close)

	c := testClient(srv)

	_, err := c.GetMachine("nonexistent")
	if err == nil {
		t.Fatal("expected error for 404 response")
	}
	if got := err.Error(); got == "" {
		t.Fatal("error message should not be empty")
	}
	// Verify the error contains the status code and the body content.
	want := "returned 404"
	if !contains(err.Error(), want) {
		t.Errorf("error %q should contain %q", err.Error(), want)
	}
	wantBody := "machine not found"
	if !contains(err.Error(), wantBody) {
		t.Errorf("error %q should contain response body %q", err.Error(), wantBody)
	}
}

func TestUpdateMachine(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/apps/test-app/machines/m_abc" {
			t.Errorf("path = %s, want /apps/test-app/machines/m_abc", r.URL.Path)
		}
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		cfg, _ := body["config"].(map[string]any)
		if cfg["image"] != "registry.fly.io/cogitator:v2" {
			t.Errorf("image = %v, want registry.fly.io/cogitator:v2", cfg["image"])
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	c := testClient(srv)
	if err := c.UpdateMachine("m_abc", "registry.fly.io/cogitator:v2"); err != nil {
		t.Fatalf("UpdateMachine: %v", err)
	}
}

func TestStartMachine(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/apps/test-app/machines/m_abc/start" {
			t.Errorf("path = %s, want /apps/test-app/machines/m_abc/start", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	c := testClient(srv)
	if err := c.StartMachine("m_abc"); err != nil {
		t.Fatalf("StartMachine: %v", err)
	}
}

func TestStopMachine(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/apps/test-app/machines/m_abc/stop" {
			t.Errorf("path = %s, want /apps/test-app/machines/m_abc/stop", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	c := testClient(srv)
	if err := c.StopMachine("m_abc"); err != nil {
		t.Fatalf("StopMachine: %v", err)
	}
}

func TestDeleteVolume(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("method = %s, want DELETE", r.Method)
		}
		if r.URL.Path != "/apps/test-app/volumes/vol_123" {
			t.Errorf("path = %s, want /apps/test-app/volumes/vol_123", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	c := testClient(srv)
	if err := c.DeleteVolume("vol_123"); err != nil {
		t.Fatalf("DeleteVolume: %v", err)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsAt(s, substr))
}

func containsAt(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
