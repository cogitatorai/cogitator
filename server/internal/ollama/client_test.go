package ollama

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestStatus(t *testing.T) {
	t.Run("running", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		defer srv.Close()

		c := New(srv.URL)
		if !c.Status(context.Background()) {
			t.Error("expected Status to return true when server responds OK")
		}
	})

	t.Run("not running", func(t *testing.T) {
		c := New("http://127.0.0.1:0")
		if c.Status(context.Background()) {
			t.Error("expected Status to return false when server is unreachable")
		}
	})
}

func TestListModels(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/tags" || r.Method != http.MethodGet {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{
			"models": []map[string]any{
				{
					"name": "llama3.2:latest",
					"size": 4_700_000_000,
					"details": map[string]any{
						"family":             "llama",
						"parameter_size":     "8B",
						"quantization_level": "Q4_K_M",
					},
				},
			},
		})
	}))
	defer srv.Close()

	c := New(srv.URL)
	models, err := c.ListModels(context.Background())
	if err != nil {
		t.Fatalf("ListModels: %v", err)
	}
	if len(models) != 1 {
		t.Fatalf("expected 1 model, got %d", len(models))
	}
	if models[0].Name != "llama3.2:latest" {
		t.Errorf("expected name llama3.2:latest, got %s", models[0].Name)
	}
	if models[0].Family != "llama" {
		t.Errorf("expected family llama, got %s", models[0].Family)
	}
	if models[0].Parameters != "8B" {
		t.Errorf("expected 8B parameters, got %s", models[0].Parameters)
	}
}

func TestPullModel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/pull" || r.Method != http.MethodPost {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if body["name"] != "llama3.2" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/x-ndjson")
		flusher, _ := w.(http.Flusher)
		for _, evt := range []PullProgress{
			{Status: "pulling manifest"},
			{Status: "downloading", Total: 1000, Completed: 500},
			{Status: "downloading", Total: 1000, Completed: 1000},
			{Status: "verifying"},
			{Status: "success"},
		} {
			data, _ := json.Marshal(evt)
			fmt.Fprintln(w, string(data))
			if flusher != nil {
				flusher.Flush()
			}
		}
	}))
	defer srv.Close()

	c := New(srv.URL)
	progress := make(chan PullProgress, 10)
	err := c.PullModel(context.Background(), "llama3.2", progress)
	if err != nil {
		t.Fatalf("PullModel: %v", err)
	}

	var events []PullProgress
	for p := range progress {
		events = append(events, p)
	}
	if len(events) < 2 {
		t.Fatalf("expected at least 2 progress events, got %d", len(events))
	}
	if events[len(events)-1].Status != "success" {
		t.Errorf("expected last event to be success, got %s", events[len(events)-1].Status)
	}
}

func TestDeleteModel(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/delete" || r.Method != http.MethodDelete {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		called = true
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := New(srv.URL)
	err := c.DeleteModel(context.Background(), "llama3.2")
	if err != nil {
		t.Fatalf("DeleteModel: %v", err)
	}
	if !called {
		t.Error("expected delete endpoint to be called")
	}
}
