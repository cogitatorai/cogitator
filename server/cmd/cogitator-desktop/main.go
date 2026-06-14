//go:build desktop

package main

import (
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/cogitatorai/cogitator/server/internal/app"
	"github.com/cogitatorai/cogitator/server/internal/dashboard"
)

func main() {
	// Use a fixed workspace in the user's home directory so data persists
	// across launches regardless of the working directory.
	home, _ := os.UserHomeDir()
	wsPath := filepath.Join(home, ".cogitator")

	srv, err := app.New(app.Options{
		DashboardFS:   dashboard.FS(),
		WorkspacePath: wsPath,
	})
	if err != nil {
		slog.Error("startup failed", "error", err)
		os.Exit(1)
	}

	if err := srv.Start(); err != nil {
		slog.Error("listen failed", "error", err)
		os.Exit(1)
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	slog.Info("shutting down")
	srv.ShutdownWithTimeout(5 * time.Second)
}
