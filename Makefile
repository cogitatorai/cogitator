-include .env

VERSION ?= $(shell git describe --tags --match 'v[0-9]*' --always --dirty 2>/dev/null || echo dev)

.PHONY: build test lint dashboard docker help

build: ## Build the server binary
	cd server && go build -trimpath \
		-ldflags "-s -w -X github.com/cogitatorai/cogitator/server/internal/version.Version=$(VERSION)" \
		-o ../cogitator ./cmd/cogitator/

test: ## Run all Go tests
	cd server && go test ./... -count=1

lint: ## Run go vet
	cd server && go vet ./...

dashboard: ## Build the dashboard
	cd dashboard && npm run build

docker: ## Build Docker image
	docker build -t cogitator .

help: ## Show this help
	@grep -hE '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  %-12s %s\n", $$1, $$2}'

.DEFAULT_GOAL := help
