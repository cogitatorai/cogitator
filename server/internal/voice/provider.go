package voice

import "fmt"

type Registry struct {
	stt map[string]STTProvider
	tts map[string]TTSProvider
}

func NewRegistry() *Registry {
	return &Registry{
		stt: make(map[string]STTProvider),
		tts: make(map[string]TTSProvider),
	}
}

func (r *Registry) RegisterSTT(name string, p STTProvider) { r.stt[name] = p }
func (r *Registry) RegisterTTS(name string, p TTSProvider) { r.tts[name] = p }

func (r *Registry) STT(name string) (STTProvider, error) {
	p, ok := r.stt[name]
	if !ok {
		return nil, fmt.Errorf("voice: unknown STT provider %q", name)
	}
	return p, nil
}

func (r *Registry) TTS(name string) (TTSProvider, error) {
	p, ok := r.tts[name]
	if !ok {
		return nil, fmt.Errorf("voice: unknown TTS provider %q", name)
	}
	return p, nil
}
