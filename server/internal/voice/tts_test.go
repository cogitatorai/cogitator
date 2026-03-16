package voice_test

import (
	"bytes"
	"errors"
	"io"
	"testing"

	"github.com/cogitatorai/cogitator/server/internal/voice"
)

type mockTTS struct {
	data []byte
	err  error
}

func (m *mockTTS) Synthesize(text string, voiceName string) (io.Reader, error) {
	if m.err != nil {
		return nil, m.err
	}
	return bytes.NewReader(m.data), nil
}

func TestTTSInterface(t *testing.T) {
	audio := []byte("fake-aac-audio")
	var provider voice.TTSProvider = &mockTTS{data: audio}
	reader, err := provider.Synthesize("hello", "alloy")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got, _ := io.ReadAll(reader)
	if !bytes.Equal(got, audio) {
		t.Fatalf("audio mismatch")
	}
}

func TestTTSSynthesizeError(t *testing.T) {
	var provider voice.TTSProvider = &mockTTS{err: errors.New("provider down")}
	_, err := provider.Synthesize("hello", "alloy")
	if err == nil {
		t.Fatal("expected error")
	}
}
