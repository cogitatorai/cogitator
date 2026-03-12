package sandbox

import (
	"context"
	"log/slog"
	"os/exec"
	"strings"
	"time"
)

// DefaultMaxOutput is the default output cap in bytes (64 KiB).
const DefaultMaxOutput = 65536

// DefaultTimeout is the fallback command timeout.
const DefaultTimeout = 30 * time.Second

// HostRunner executes commands directly on the host with a scrubbed
// environment and output capping.
type HostRunner struct {
	maxOutput int
	logger    *slog.Logger
}

// NewHostRunner creates a HostRunner. If maxOutput is 0, DefaultMaxOutput
// is used.
func NewHostRunner(maxOutput int, logger *slog.Logger) *HostRunner {
	if maxOutput <= 0 {
		maxOutput = DefaultMaxOutput
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &HostRunner{maxOutput: maxOutput, logger: logger}
}

func (r *HostRunner) Mode() string { return "host" }

func (r *HostRunner) Run(ctx context.Context, cfg RunConfig) Result {
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = DefaultTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-c", cfg.Command)
	cmd.Dir = cfg.WorkingDir
	cmd.Env = ScrubEnv()

	out, err := cmd.CombinedOutput()

	maxOut := cfg.MaxOutput
	if maxOut <= 0 {
		maxOut = r.maxOutput
	}
	output := strings.TrimSpace(capOutput(out, maxOut))

	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = -1
		}
	}

	return Result{
		Output:   output,
		ExitCode: exitCode,
		Err:      err,
	}
}
