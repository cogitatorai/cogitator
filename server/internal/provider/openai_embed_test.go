package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOpenAIEmbed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/embeddings" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		resp := openAIEmbeddingResponse{
			Data: []struct {
				Embedding []float32 `json:"embedding"`
				Index     int       `json:"index"`
			}{
				{Embedding: []float32{0.1, 0.2, 0.3}, Index: 0},
				{Embedding: []float32{0.4, 0.5, 0.6}, Index: 1},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	o := NewOpenAI(srv.URL, "test-key")
	vecs, err := o.Embed(context.Background(), []string{"hello", "world"}, "text-embedding-3-small")
	if err != nil {
		t.Fatalf("Embed returned error: %v", err)
	}
	if len(vecs) != 2 {
		t.Fatalf("expected 2 vectors, got %d", len(vecs))
	}
	if len(vecs[0]) != 3 {
		t.Errorf("expected vector[0] dim 3, got %d", len(vecs[0]))
	}
	if len(vecs[1]) != 3 {
		t.Errorf("expected vector[1] dim 3, got %d", len(vecs[1]))
	}
}

func TestOpenAIEmbedEmpty(t *testing.T) {
	// No server: if an HTTP call is made, the test will fail with a connection error.
	o := NewOpenAI("http://127.0.0.1:0", "test-key")
	vecs, err := o.Embed(context.Background(), []string{}, "text-embedding-3-small")
	if err != nil {
		t.Fatalf("expected nil error for empty input, got: %v", err)
	}
	if vecs != nil {
		t.Fatalf("expected nil result for empty input, got: %v", vecs)
	}
}
