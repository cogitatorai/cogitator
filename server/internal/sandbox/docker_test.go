package sandbox

import (
	"strings"
	"testing"
)

func TestDockerRunnerBuildArgs(t *testing.T) {
	r := NewDockerRunner(0, nil)

	t.Run("basic command no network", func(t *testing.T) {
		args := r.BuildArgs(RunConfig{
			Command:    "echo hello",
			WorkingDir: "/tmp/work",
		})
		joined := strings.Join(args, " ")

		mustContain := []string{
			"--rm",
			"--memory 256m",
			"--pids-limit 64",
			"--security-opt no-new-privileges",
			"--read-only",
			"--network none",
			"-v /tmp/work:/work:rw",
			"-w /work",
			"alpine:3.19",
			"sh -c echo hello",
		}
		for _, want := range mustContain {
			if !strings.Contains(joined, want) {
				t.Errorf("args missing %q in: %s", want, joined)
			}
		}
	})

	t.Run("with network", func(t *testing.T) {
		args := r.BuildArgs(RunConfig{
			Command:    "curl https://example.com",
			WorkingDir: "/tmp/work",
			NeedsNet:   true,
		})
		joined := strings.Join(args, " ")

		if strings.Contains(joined, "--network none") {
			t.Error("--network none should be omitted when NeedsNet=true")
		}
	})

	t.Run("no working dir", func(t *testing.T) {
		args := r.BuildArgs(RunConfig{
			Command: "echo test",
		})
		joined := strings.Join(args, " ")

		if strings.Contains(joined, "-v") {
			t.Error("should not mount a volume when WorkingDir is empty")
		}
		if !strings.Contains(joined, "-w /tmp") {
			t.Error("should use /tmp as working dir when none specified")
		}
	})
}

func TestDockerRunnerMode(t *testing.T) {
	r := NewDockerRunner(0, nil)
	if r.Mode() != "docker" {
		t.Errorf("Mode() = %q, want 'docker'", r.Mode())
	}
}

func TestNewRunnerHostMode(t *testing.T) {
	r, err := NewRunner("host", 0, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r.Mode() != "host" {
		t.Errorf("Mode() = %q, want 'host'", r.Mode())
	}
}

func TestNewRunnerEmptyMode(t *testing.T) {
	r, err := NewRunner("", 0, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r.Mode() != "host" {
		t.Errorf("Mode() = %q, want 'host'", r.Mode())
	}
}

func TestNewRunnerInvalidMode(t *testing.T) {
	_, err := NewRunner("invalid", 0, nil)
	if err == nil {
		t.Fatal("expected error for invalid mode")
	}
}
