package reqctx

import (
	"context"
	"testing"
)

func TestRequestIDRoundTrip(t *testing.T) {
	ctx := WithRequestID(context.Background(), "req-123")
	if got := RequestID(ctx); got != "req-123" {
		t.Errorf("RequestID = %q, want req-123", got)
	}
}

func TestSessionKeyRoundTrip(t *testing.T) {
	ctx := WithSessionKey(context.Background(), "web:default")
	if got := SessionKey(ctx); got != "web:default" {
		t.Errorf("SessionKey = %q, want web:default", got)
	}
}

func TestEmptyContextDefaults(t *testing.T) {
	if got := RequestID(context.Background()); got != "" {
		t.Errorf("RequestID on empty ctx = %q, want empty", got)
	}
	if got := SessionKey(context.Background()); got != "" {
		t.Errorf("SessionKey on empty ctx = %q, want empty", got)
	}
}
