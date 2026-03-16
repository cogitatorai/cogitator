package voice_test

import (
	"context"
	"testing"

	"github.com/cogitatorai/cogitator/server/internal/voice"
)

type mockSTT struct {
	text string
	err  error
}

func (m *mockSTT) Transcribe(ctx context.Context, audio []byte, format string) (string, error) {
	return m.text, m.err
}

func TestSTTInterface(t *testing.T) {
	var provider voice.STTProvider = &mockSTT{text: "hello world"}
	text, err := provider.Transcribe(context.Background(), []byte("fake-audio"), "m4a")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if text != "hello world" {
		t.Fatalf("got %q, want %q", text, "hello world")
	}
}

func TestSTTError(t *testing.T) {
	var provider voice.STTProvider = &mockSTT{err: voice.ErrTranscriptionEmpty}
	_, err := provider.Transcribe(context.Background(), []byte("silence"), "m4a")
	if err != voice.ErrTranscriptionEmpty {
		t.Fatalf("got %v, want ErrTranscriptionEmpty", err)
	}
}

func TestRegistryResolveSTT(t *testing.T) {
	reg := voice.NewRegistry()
	mock := &mockSTT{text: "registered"}
	reg.RegisterSTT("mock", mock)
	got, err := reg.STT("mock")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	text, _ := got.Transcribe(context.Background(), nil, "")
	if text != "registered" {
		t.Fatalf("got %q, want %q", text, "registered")
	}
}

func TestRegistrySTTNotFound(t *testing.T) {
	reg := voice.NewRegistry()
	_, err := reg.STT("nonexistent")
	if err == nil {
		t.Fatal("expected error for unknown provider")
	}
}

func TestRegistryResolveTTS(t *testing.T) {
	reg := voice.NewRegistry()
	mock := &mockTTS{data: []byte("audio")}
	reg.RegisterTTS("mock", mock)
	got, err := reg.TTS("mock")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil provider")
	}
}
