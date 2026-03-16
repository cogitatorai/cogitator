package voice

import (
	"context"
	"errors"
)

var ErrTranscriptionEmpty = errors.New("voice: transcription returned empty text")

type STTProvider interface {
	Transcribe(ctx context.Context, audio []byte, format string) (string, error)
}
