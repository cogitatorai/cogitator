package provider

import "testing"

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
