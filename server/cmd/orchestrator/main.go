package main

import (
	"log"
	"os"

	"github.com/cogitatorai/cogitator/server/internal/orchestrator"
)

func main() {
	cfg := orchestrator.LoadConfig()
	srv, err := orchestrator.NewServer(cfg)
	if err != nil {
		log.Fatalf("failed to start orchestrator: %v", err)
	}
	if err := srv.Run(); err != nil {
		log.Fatalf("orchestrator error: %v", err)
		os.Exit(1)
	}
}
