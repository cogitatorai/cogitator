package openai_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cogitatorai/cogitator/server/internal/voice"
	vopenai "github.com/cogitatorai/cogitator/server/internal/voice/openai"
)

func TestTTSSynthesize(t *testing.T) {
	fakeAudio := []byte("fake-aac-audio-stream")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/audio/speech" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.Method != "POST" {
			t.Fatalf("unexpected method: %s", r.Method)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Fatalf("unexpected auth: %s", got)
		}
		w.Header().Set("Content-Type", "audio/aac")
		w.Write(fakeAudio)
	}))
	defer server.Close()

	client := vopenai.NewClient(server.URL, "test-key")
	tts := vopenai.NewTTS(client)

	var provider voice.TTSProvider = tts
	reader, err := provider.Synthesize("hello world", "alloy")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read error: %v", err)
	}
	if string(got) != string(fakeAudio) {
		t.Fatalf("audio mismatch: got %d bytes, want %d", len(got), len(fakeAudio))
	}
}

func TestTTSSynthesizeAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error": "server error"}`))
	}))
	defer server.Close()

	client := vopenai.NewClient(server.URL, "test-key")
	tts := vopenai.NewTTS(client)

	_, err := tts.Synthesize("hello", "alloy")
	if err == nil {
		t.Fatal("expected error")
	}
}
