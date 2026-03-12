package sandbox

import (
	"context"
	"testing"
)

func TestCapOutput(t *testing.T) {
	data := []byte("hello world, this is a test")

	// No cap.
	if got := capOutput(data, 0); got != string(data) {
		t.Errorf("capOutput(0) = %q, want full string", got)
	}

	// Cap larger than data.
	if got := capOutput(data, 1000); got != string(data) {
		t.Errorf("capOutput(1000) = %q, want full string", got)
	}

	// Cap at 5 bytes.
	got := capOutput(data, 5)
	if got[:5] != "hello" {
		t.Errorf("truncated prefix = %q, want 'hello'", got[:5])
	}
	if len(got) <= 5 {
		t.Error("expected truncation notice appended")
	}
}

func TestHostRunnerEcho(t *testing.T) {
	r := NewHostRunner(0, nil)
	if r.Mode() != "host" {
		t.Errorf("Mode() = %q, want 'host'", r.Mode())
	}

	res := r.Run(context.Background(), RunConfig{
		Command:    "echo hello",
		WorkingDir: t.TempDir(),
	})
	if res.Err != nil {
		t.Fatalf("unexpected error: %v", res.Err)
	}
	if res.Output != "hello" {
		t.Errorf("output = %q, want 'hello'", res.Output)
	}
	if res.ExitCode != 0 {
		t.Errorf("exit code = %d, want 0", res.ExitCode)
	}
}

func TestHostRunnerOutputCapping(t *testing.T) {
	r := NewHostRunner(10, nil)

	// Generate output longer than 10 bytes.
	res := r.Run(context.Background(), RunConfig{
		Command:    "echo 'this is a very long output string'",
		WorkingDir: t.TempDir(),
	})
	// The output should be capped and contain a truncation notice.
	if len(res.Output) > 200 {
		t.Errorf("output unexpectedly long: %d bytes", len(res.Output))
	}
	if res.ExitCode != 0 {
		t.Errorf("exit code = %d, want 0", res.ExitCode)
	}
}

func TestHostRunnerFailedCommand(t *testing.T) {
	r := NewHostRunner(0, nil)

	res := r.Run(context.Background(), RunConfig{
		Command:    "exit 42",
		WorkingDir: t.TempDir(),
	})
	if res.Err == nil {
		t.Fatal("expected error for non-zero exit")
	}
	if res.ExitCode != 42 {
		t.Errorf("exit code = %d, want 42", res.ExitCode)
	}
}

func TestHostRunnerEnvScrubbed(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "sk-leaked")

	r := NewHostRunner(0, nil)
	res := r.Run(context.Background(), RunConfig{
		Command:    "printenv OPENAI_API_KEY || true",
		WorkingDir: t.TempDir(),
	})
	if res.Output == "sk-leaked" {
		t.Error("OPENAI_API_KEY leaked through to shell command")
	}
}
