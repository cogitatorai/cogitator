package connector

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRESTExecutor_SimpleRequest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("q") != "meeting" {
			t.Errorf("query q = %q, want meeting", r.URL.Query().Get("q"))
		}
		json.NewEncoder(w).Encode(map[string]any{
			"items": []map[string]any{
				{"id": "1", "summary": "Team standup", "start": map[string]string{"dateTime": "2026-03-05T09:00:00Z"}},
			},
		})
	}))
	defer srv.Close()

	tool := ToolManifest{
		Name: "calendar_search",
		Parameters: []ParamDef{
			{Name: "query", Type: "string", Required: true},
		},
		Request: RequestDef{
			Method: "GET",
			URL:    srv.URL + "/calendar/events",
			Query:  map[string]string{"q": "{{.query}}"},
		},
		Response: ResponseDef{
			Root: ".items",
			Fields: map[string]string{
				"id":      ".id",
				"summary": ".summary",
			},
		},
		connectorName: "google",
	}

	exec := NewRESTExecutor()
	result, err := exec.Execute(srv.Client(), tool, map[string]any{"query": "meeting"})
	if err != nil {
		t.Fatal(err)
	}

	var items []map[string]any
	if err := json.Unmarshal([]byte(result), &items); err != nil {
		t.Fatalf("result not valid JSON array: %s", result)
	}
	if len(items) != 1 {
		t.Fatalf("items = %d, want 1", len(items))
	}
	if items[0]["summary"] != "Team standup" {
		t.Fatalf("summary = %v", items[0]["summary"])
	}
}

func TestRESTExecutor_FetchEach(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/messages" {
			json.NewEncoder(w).Encode(map[string]any{
				"messages": []map[string]string{
					{"id": "msg1"},
					{"id": "msg2"},
				},
			})
			return
		}
		// Individual message fetch.
		id := r.URL.Path[len("/messages/"):]
		json.NewEncoder(w).Encode(map[string]any{
			"id":      id,
			"snippet": "Hello from " + id,
			"payload": map[string]any{
				"headers": []map[string]string{
					{"name": "Subject", "value": "Re: " + id},
				},
			},
		})
	}))
	defer srv.Close()

	tool := ToolManifest{
		Name: "email_search",
		Parameters: []ParamDef{
			{Name: "query", Type: "string", Required: true},
		},
		Request: RequestDef{
			Method: "GET",
			URL:    srv.URL + "/messages",
			Query:  map[string]string{"q": "{{.query}}"},
		},
		FetchEach: &FetchEachDef{
			IDPath: ".messages[].id",
			Request: RequestDef{
				Method: "GET",
				URL:    srv.URL + "/messages/{{.id}}",
			},
			Response: ResponseDef{
				Fields: map[string]string{
					"id":      ".id",
					"snippet": ".snippet",
					"subject": `.payload.headers[] | select(.name=="Subject") | .value`,
				},
			},
		},
		connectorName: "google",
	}

	exec := NewRESTExecutor()
	result, err := exec.Execute(srv.Client(), tool, map[string]any{"query": "test"})
	if err != nil {
		t.Fatal(err)
	}

	var items []map[string]any
	if err := json.Unmarshal([]byte(result), &items); err != nil {
		t.Fatalf("result not valid JSON array: %s", result)
	}
	if len(items) != 2 {
		t.Fatalf("items = %d, want 2", len(items))
	}
	if items[0]["subject"] != "Re: msg1" {
		t.Fatalf("subject = %v", items[0]["subject"])
	}
}

func TestRESTExecutor_TemplateRendering(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("maxResults") != "10" {
			t.Errorf("maxResults = %q, want 10", r.URL.Query().Get("maxResults"))
		}
		json.NewEncoder(w).Encode(map[string]any{"result": "ok"})
	}))
	defer srv.Close()

	tool := ToolManifest{
		Name: "test_tool",
		Parameters: []ParamDef{
			{Name: "max_results", Type: "integer"},
		},
		Request: RequestDef{
			Method: "GET",
			URL:    srv.URL + "/test",
			Query:  map[string]string{"maxResults": `{{.max_results | default 10}}`},
		},
		Response:      ResponseDef{},
		connectorName: "test",
	}

	exec := NewRESTExecutor()
	// max_results not provided; should use default.
	result, err := exec.Execute(srv.Client(), tool, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if result == "" {
		t.Fatal("expected non-empty result")
	}
}
