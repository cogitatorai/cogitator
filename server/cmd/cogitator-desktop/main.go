//go:build desktop

package main

import (
	"log"
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
		log.Fatalf("startup: %v", err)
	}

	if err := srv.Start(); err != nil {
		log.Fatalf("listen: %v", err)
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	log.Println("shutting down...")
	srv.ShutdownWithTimeout(5 * time.Second)
}
