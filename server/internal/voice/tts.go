package voice

import "io"

type TTSProvider interface {
	Synthesize(text string, voice string) (io.Reader, error)
}
