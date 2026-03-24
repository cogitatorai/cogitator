package main

import (
	"flag"
	"log"
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
