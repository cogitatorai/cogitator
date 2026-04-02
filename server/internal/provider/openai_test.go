package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOpenAI_ExtraHeaders(t *testing.T) {
	const validResponse = `{"choices":[{"message":{"role":"assistant","content":"ok"}}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`

	var gotTenantID, gotSecret string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotTenantID = r.Header.Get("X-Tenant-ID")
		gotSecret = r.Header.Get("X-Internal-Secret")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(validResponse))
	}))
	defer srv.Close()

	p := NewOpenAI(srv.URL, "")
	p.ExtraHeaders = map[string]string{
		"X-Tenant-ID":       "tenant-123",
		"X-Internal-Secret": "secret-abc",
	}

	messages := []Message{{Role: "user", Content: "hi"}}
	if _, err := p.Chat(context.Background(), messages, nil, "test", nil); err != nil {
		t.Fatalf("Chat returned error: %v", err)
	}

	if gotTenantID != "tenant-123" {
		t.Errorf("X-Tenant-ID = %q, want %q", gotTenantID, "tenant-123")
	}
	if gotSecret != "secret-abc" {
		t.Errorf("X-Internal-Secret = %q, want %q", gotSecret, "secret-abc")
	}
}

func TestOpenAI_Capabilities_StreamCancel(t *testing.T) {
	tests := []struct {
		name         string
		provider     string
		streamCancel bool
	}{
		{"openai", "openai", true},
		{"anthropic", "anthropic", true},
		{"ollama", "ollama", false},
		{"groq", "groq", false},
		{"together", "together", false},
		{"openrouter", "openrouter", false},
		{"custom-url", "https://custom.example.com/v1", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var p Provider = NewOpenAI(tt.provider, "test-key")
			cp, ok := p.(CapabilityProvider)
			if !ok {
				t.Fatal("OpenAI does not implement CapabilityProvider")
			}
			if got := cp.Capabilities().StreamCancel; got != tt.streamCancel {
				t.Errorf("StreamCancel = %v, want %v", got, tt.streamCancel)
			}
		})
	}
}
