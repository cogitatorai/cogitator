package worker

import (
	"testing"

	"github.com/cogitatorai/cogitator/server/internal/session"
)

func TestDetectSignals(t *testing.T) {
	tests := []struct {
		name     string
		messages []session.Message
		wantType string
		wantConf float64
	}{
		{
			name: "correction",
			messages: []session.Message{
				{Role: "assistant", Content: "Here is the answer."},
				{Role: "user", Content: "That's wrong, I didn't ask for that."},
			},
			wantType: "correction",
			wantConf: 0.85,
		},
		{
			name: "refinement",
			messages: []session.Message{
				{Role: "assistant", Content: "Here is the plan."},
				{Role: "user", Content: "Actually, I meant something more like this."},
			},
			wantType: "refinement",
			wantConf: 0.85,
		},
		{
			name: "acknowledgment",
			messages: []session.Message{
				{Role: "assistant", Content: "Done."},
				{Role: "user", Content: "Perfect, that's exactly what I wanted!"},
			},
			wantType: "acknowledgment",
			wantConf: 0.85,
		},
		{
			name: "no signal",
			messages: []session.Message{
				{Role: "assistant", Content: "Here."},
				{Role: "user", Content: "Can you also check the weather?"},
			},
			wantType: "",
			wantConf: 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			signals := detectSignals(tt.messages)
			if tt.wantType == "" {
				if len(signals) != 0 {
					t.Errorf("expected no signals, got %d", len(signals))
				}
				return
			}
			if len(signals) == 0 {
				t.Fatal("expected at least one signal")
			}
			if signals[0].Type != tt.wantType {
				t.Errorf("type = %s, want %s", signals[0].Type, tt.wantType)
			}
			if signals[0].Confidence < tt.wantConf {
				t.Errorf("confidence = %f, want >= %f", signals[0].Confidence, tt.wantConf)
			}
		})
	}
}
