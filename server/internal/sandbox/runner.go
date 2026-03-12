package sandbox

import (
	"context"
	"fmt"
	"time"
)

// RunConfig describes how to execute a single shell command.
type RunConfig struct {
	Command    string
	WorkingDir string
	Timeout    time.Duration
	NeedsNet   bool // true only for allowed network commands
	MaxOutput  int  // 0 = use runner default
}

// Result holds the output and exit status of a command.
type Result struct {
	Output   string
	ExitCode int
	Err      error
}

// Runner abstracts the execution environment for shell commands.
type Runner interface {
	Run(ctx context.Context, cfg RunConfig) Result
	Mode() string // "host" or "docker"
}

// capOutput truncates data to max bytes, appending a notice if truncated.
func capOutput(data []byte, max int) string {
	if max <= 0 || len(data) <= max {
		return string(data)
	}
	return string(data[:max]) + fmt.Sprintf("\n\n[output truncated: %d bytes total, showing first %d]", len(data), max)
}
