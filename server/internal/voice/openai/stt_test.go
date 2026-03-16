package openai_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cogitatorai/cogitator/server/internal/voice"
	vopenai "github.com/cogitatorai/cogitator/server/internal/voice/openai"
)

func TestWhisperTranscribe(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/audio/transcriptions" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.Method != "POST" {
			t.Fatalf("unexpected method: %s", r.Method)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Fatalf("unexpected auth header: %s", got)
		}
		ct := r.Header.Get("Content-Type")
		if ct == "" || ct[:len("multipart/form-data")] != "multipart/form-data" {
			t.Fatalf("expected multipart, got %s", ct)
		}
		json.NewEncoder(w).Encode(map[string]string{"text": "hello from whisper"})
	}))
	defer server.Close()

	client := vopenai.NewClient(server.URL, "test-key")
	stt := vopenai.NewSTT(client)

	var provider voice.STTProvider = stt
	text, err := provider.Transcribe(context.Background(), []byte("fake-audio-data"), "m4a")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if text != "hello from whisper" {
		t.Fatalf("got %q, want %q", text, "hello from whisper")
	}
}

func TestWhisperTranscribeEmpty(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{"text": ""})
	}))
	defer server.Close()

	client := vopenai.NewClient(server.URL, "test-key")
	stt := vopenai.NewSTT(client)

	_, err := stt.Transcribe(context.Background(), []byte("silence"), "m4a")
	if err != voice.ErrTranscriptionEmpty {
		t.Fatalf("got %v, want ErrTranscriptionEmpty", err)
	}
}
