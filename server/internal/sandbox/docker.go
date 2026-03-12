package sandbox

import (
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
	"time"
)

// DockerRunner executes commands inside throwaway Docker containers.
type DockerRunner struct {
	maxOutput int
	image     string
	logger    *slog.Logger
}

// NewDockerRunner creates a DockerRunner. If maxOutput is 0,
// DefaultMaxOutput is used.
func NewDockerRunner(maxOutput int, logger *slog.Logger) *DockerRunner {
	if maxOutput <= 0 {
		maxOutput = DefaultMaxOutput
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &DockerRunner{
		maxOutput: maxOutput,
		image:     "alpine:3.19",
		logger:    logger,
	}
}

func (r *DockerRunner) Mode() string { return "docker" }

// BuildArgs constructs the docker run argument list for the given config.
// Exported for testing without requiring Docker.
func (r *DockerRunner) BuildArgs(cfg RunConfig) []string {
	args := []string{
		"run", "--rm",
		"--memory", "256m",
		"--pids-limit", "64",
		"--security-opt", "no-new-privileges",
		"--read-only",
		"--tmpfs", "/tmp:rw,noexec,size=32m",
	}

	if !cfg.NeedsNet {
		args = append(args, "--network", "none")
	}

	if cfg.WorkingDir != "" {
		args = append(args, "-v", cfg.WorkingDir+":/work:rw", "-w", "/work")
	} else {
		args = append(args, "-w", "/tmp")
	}

	args = append(args, r.image, "sh", "-c", cfg.Command)
	return args
}

func (r *DockerRunner) Run(ctx context.Context, cfg RunConfig) Result {
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = DefaultTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	dockerArgs := r.BuildArgs(cfg)
	r.logger.Info("docker run", "args", dockerArgs)

	cmd := exec.CommandContext(ctx, "docker", dockerArgs...)
	// Docker commands don't need a working directory on the host.
	// The container handles its own working directory via -w.

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
		r.logger.Warn("docker run completed with error",
			"command", cfg.Command,
			"exit_code", exitCode,
			"output_bytes", len(out),
			"error", err,
		)
	} else {
		r.logger.Info("docker run completed",
			"command", cfg.Command,
			"exit_code", exitCode,
			"output_bytes", len(out),
		)
	}

	return Result{
		Output:   output,
		ExitCode: exitCode,
		Err:      err,
	}
}

// DockerAvailable checks whether Docker is installed and responsive.
func DockerAvailable() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return exec.CommandContext(ctx, "docker", "info").Run() == nil
}

// NewRunner creates a Runner based on the mode string.
// "auto" selects Docker if available, otherwise host.
// "docker" requires Docker and returns an error if unavailable.
// "host" (or empty) always uses host execution.
func NewRunner(mode string, maxOutput int, logger *slog.Logger) (Runner, error) {
	switch strings.ToLower(mode) {
	case "docker":
		if !DockerAvailable() {
			return nil, fmt.Errorf("sandbox mode is 'docker' but Docker is not available")
		}
		return NewDockerRunner(maxOutput, logger), nil
	case "auto":
		if DockerAvailable() {
			logger.Info("sandbox: Docker detected, using container isolation")
			return NewDockerRunner(maxOutput, logger), nil
		}
		logger.Info("sandbox: Docker not available, using host execution")
		return NewHostRunner(maxOutput, logger), nil
	case "host", "":
		return NewHostRunner(maxOutput, logger), nil
	default:
		return nil, fmt.Errorf("unknown sandbox mode: %q (valid: auto, docker, host)", mode)
	}
}
