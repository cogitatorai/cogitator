package main

import (
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"
	_ "time/tzdata" // Embed timezone database so TZ env var works in minimal containers (Alpine).

	"github.com/cogitatorai/cogitator/server/internal/app"
)

func main() {
	var cfgPath string
	flag.StringVar(&cfgPath, "config", "", "path to cogitator.yaml")
	flag.Parse()

	srv, err := app.New(app.Options{ConfigPath: cfgPath})
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
