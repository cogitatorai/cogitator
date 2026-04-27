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

// TestOpenAI_CacheTokens_OpenAI verifies that prompt_tokens_details.cached_tokens
// (the OpenAI shape) is decoded into CacheReadTokens and CacheCreationTokens
// stays zero when the field is absent.
func TestOpenAI_CacheTokens_OpenAI(t *testing.T) {
	const body = `{
		"choices":[{"message":{"role":"assistant","content":"hi"}}],
		"usage":{
			"prompt_tokens":200,
			"completion_tokens":50,
			"prompt_tokens_details":{"cached_tokens":150}
		}
	}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	p := NewOpenAI(srv.URL, "")
	resp, err := p.Chat(context.Background(), []Message{{Role: "user", Content: "hi"}}, nil, "test", nil)
	if err != nil {
		t.Fatalf("Chat error: %v", err)
	}
	if resp.Usage.InputTokens != 200 {
		t.Errorf("InputTokens = %d, want 200", resp.Usage.InputTokens)
	}
	if resp.Usage.OutputTokens != 50 {
		t.Errorf("OutputTokens = %d, want 50", resp.Usage.OutputTokens)
	}
	if resp.Usage.CacheReadTokens != 150 {
		t.Errorf("CacheReadTokens = %d, want 150", resp.Usage.CacheReadTokens)
	}
	if resp.Usage.CacheCreationTokens != 0 {
		t.Errorf("CacheCreationTokens = %d, want 0", resp.Usage.CacheCreationTokens)
	}
}

// TestOpenAI_CacheTokens_AnthropicCompat verifies that cache_creation_input_tokens
// (the Anthropic compat shape) is decoded into CacheCreationTokens.
func TestOpenAI_CacheTokens_AnthropicCompat(t *testing.T) {
	const body = `{
		"choices":[{"message":{"role":"assistant","content":"hi"}}],
		"usage":{
			"prompt_tokens":300,
			"completion_tokens":40,
			"cache_creation_input_tokens":300
		}
	}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	p := NewOpenAI(srv.URL, "")
	resp, err := p.Chat(context.Background(), []Message{{Role: "user", Content: "hi"}}, nil, "test", nil)
	if err != nil {
		t.Fatalf("Chat error: %v", err)
	}
	if resp.Usage.CacheCreationTokens != 300 {
		t.Errorf("CacheCreationTokens = %d, want 300", resp.Usage.CacheCreationTokens)
	}
	if resp.Usage.CacheReadTokens != 0 {
		t.Errorf("CacheReadTokens = %d, want 0", resp.Usage.CacheReadTokens)
	}
}

// TestOpenAI_CacheTokens_NoCacheFields verifies that a response without any
// cache fields leaves CacheReadTokens and CacheCreationTokens at zero.
func TestOpenAI_CacheTokens_NoCacheFields(t *testing.T) {
	const body = `{"choices":[{"message":{"role":"assistant","content":"ok"}}],"usage":{"prompt_tokens":10,"completion_tokens":5}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	p := NewOpenAI(srv.URL, "")
	resp, err := p.Chat(context.Background(), []Message{{Role: "user", Content: "hi"}}, nil, "test", nil)
	if err != nil {
		t.Fatalf("Chat error: %v", err)
	}
	if resp.Usage.CacheReadTokens != 0 {
		t.Errorf("CacheReadTokens = %d, want 0", resp.Usage.CacheReadTokens)
	}
	if resp.Usage.CacheCreationTokens != 0 {
		t.Errorf("CacheCreationTokens = %d, want 0", resp.Usage.CacheCreationTokens)
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
