package browser

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"syscall"
	"time"
)

// DetectChromePath returns the path to a Chrome binary. If override is non-empty
// it is returned directly without any filesystem check. On macOS the standard
// application bundle path is probed. On Linux, exec.LookPath is tried for
// google-chrome then chromium-browser. An empty string is returned when no
// binary can be located.
func DetectChromePath(override string) string {
	if override != "" {
		return override
	}
	switch runtime.GOOS {
	case "darwin":
		const macPath = "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
		if _, err := os.Stat(macPath); err == nil {
			return macPath
		}
	case "linux":
		for _, candidate := range []string{"google-chrome", "chromium-browser"} {
			if p, err := exec.LookPath(candidate); err == nil {
				return p
			}
		}
	}
	return ""
}

// StartHeadless launches Chrome in headless mode with the remote debugging port
// bound to port. The process is started but not waited on; the caller owns the
// returned *exec.Cmd and must call StopProcess when done.
func StartHeadless(chromePath string, port int) (*exec.Cmd, error) {
	args := []string{
		"--headless=new",
		fmt.Sprintf("--remote-debugging-port=%d", port),
		"--no-first-run",
		"--disable-gpu",
		"--window-size=1920,1080",
	}
	cmd := exec.Command(chromePath, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("starting chrome: %w", err)
	}
	return cmd, nil
}

// StopProcess gracefully terminates the Chrome process started by StartHeadless.
// It sends SIGTERM and waits up to 5 seconds; if the process has not exited by
// then it is killed with SIGKILL.
func StopProcess(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}

	// Best-effort SIGTERM. On Windows Process.Signal returns an error for
	// signals other than os.Kill, so we fall back to Kill immediately there.
	if runtime.GOOS == "windows" {
		cmd.Process.Kill() //nolint:errcheck
		cmd.Wait()         //nolint:errcheck
		return
	}

	cmd.Process.Signal(syscall.SIGTERM) //nolint:errcheck

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	select {
	case <-done:
		// Process exited cleanly within the grace period.
	case <-time.After(5 * time.Second):
		cmd.Process.Kill() //nolint:errcheck
		<-done
	}
}
