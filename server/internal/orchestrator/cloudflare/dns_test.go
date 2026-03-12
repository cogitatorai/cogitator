package cloudflare

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// newTestClient spins up a local HTTP server and returns a Client pointed at it.
func newTestClient(t *testing.T, handler http.HandlerFunc) (*Client, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	c := NewClient("test-token", "zone-123")
	c.baseURL = srv.URL
	return c, srv
}

func TestAddCNAME(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/zones/zone-123/dns_records" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Fatal("missing or wrong Authorization header")
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Fatal("missing Content-Type header")
		}

		body, _ := io.ReadAll(r.Body)
		var req map[string]any
		if err := json.Unmarshal(body, &req); err != nil {
			t.Fatalf("bad request body: %v", err)
		}
		if req["type"] != "CNAME" {
			t.Fatalf("expected type CNAME, got %v", req["type"])
		}
		if req["name"] != "acme" {
			t.Fatalf("expected name acme, got %v", req["name"])
		}
		if req["content"] != "acme.fly.dev" {
			t.Fatalf("expected content acme.fly.dev, got %v", req["content"])
		}
		if req["proxied"] != true {
			t.Fatalf("expected proxied true, got %v", req["proxied"])
		}

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{
			"success": true,
			"result": map[string]any{
				"id":      "rec-1",
				"type":    "CNAME",
				"name":    "acme.example.com",
				"content": "acme.fly.dev",
				"proxied": true,
			},
		})
	})

	rec, err := c.AddCNAME("acme", "acme.fly.dev", true)
	if err != nil {
		t.Fatalf("AddCNAME: %v", err)
	}
	if rec.ID != "rec-1" {
		t.Fatalf("expected record ID rec-1, got %s", rec.ID)
	}
	if rec.Name != "acme.example.com" {
		t.Fatalf("expected name acme.example.com, got %s", rec.Name)
	}
}

func TestDeleteRecord(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Fatalf("expected DELETE, got %s", r.Method)
		}
		if r.URL.Path != "/zones/zone-123/dns_records/rec-42" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Fatal("missing or wrong Authorization header")
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"success":true}`))
	})

	if err := c.DeleteRecord("rec-42"); err != nil {
		t.Fatalf("DeleteRecord: %v", err)
	}
}

func TestFindRecord(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("expected GET, got %s", r.Method)
		}
		if got := r.URL.Query().Get("name"); got != "acme.example.com" {
			t.Fatalf("expected name query param acme.example.com, got %s", got)
		}

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{
			"success": true,
			"result": []map[string]any{
				{
					"id":      "rec-7",
					"type":    "CNAME",
					"name":    "acme.example.com",
					"content": "acme.fly.dev",
					"proxied": true,
				},
			},
		})
	})

	rec, err := c.FindRecord("acme.example.com")
	if err != nil {
		t.Fatalf("FindRecord: %v", err)
	}
	if rec == nil {
		t.Fatal("expected a record, got nil")
	}
	if rec.ID != "rec-7" {
		t.Fatalf("expected ID rec-7, got %s", rec.ID)
	}
}

func TestFindRecordNoResults(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{
			"success": true,
			"result":  []map[string]any{},
		})
	})

	rec, err := c.FindRecord("ghost.example.com")
	if err != nil {
		t.Fatalf("FindRecord: %v", err)
	}
	if rec != nil {
		t.Fatalf("expected nil, got %+v", rec)
	}
}

func TestErrorHandling(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"success":false,"errors":[{"code":9109,"message":"Invalid access token"}]}`))
	})

	_, err := c.AddCNAME("bad", "target.fly.dev", false)
	if err == nil {
		t.Fatal("expected error for 403 response")
	}
}

// Compile-time check that Client satisfies DNSAPI.
var _ DNSAPI = (*Client)(nil)
