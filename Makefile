# Faraday Makefile — single static binary by default.
SHELL := /bin/bash
VERSION ?= 0.1.0-dev
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
DATE    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -s -w \
	-X github.com/faraday-stack/faraday/internal/version.Version=$(VERSION) \
	-X github.com/faraday-stack/faraday/internal/version.Commit=$(COMMIT) \
	-X github.com/faraday-stack/faraday/internal/version.Date=$(DATE)

.PHONY: build
build: ## Build the single static binary
	CGO_ENABLED=0 go build -tags "netgo osusergo" -ldflags '$(LDFLAGS)' -o bin/faraday ./cmd/faraday

.PHONY: build-linux
build-linux: ## Cross-build static linux/amd64 + linux/arm64
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -tags "netgo osusergo" -ldflags '$(LDFLAGS)' -o bin/faraday-linux-amd64 ./cmd/faraday
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -tags "netgo osusergo" -ldflags '$(LDFLAGS)' -o bin/faraday-linux-arm64 ./cmd/faraday

.PHONY: test
test: ## Run Go unit tests
	go test ./...

.PHONY: e2e
e2e: build ## Run the end-to-end slice-1 flow (CPU, offline)
	go test -tags e2e -count=1 ./test/e2e/...

.PHONY: vet
vet: ## go vet
	go vet ./...

.PHONY: tidy
tidy: ## go mod tidy
	go mod tidy

.PHONY: schema
schema: build ## Print the config JSON schema
	./bin/faraday config schema

.PHONY: openspec
openspec: ## Validate OpenSpec artifacts
	openspec validate --changes --strict

.PHONY: help
help:
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  %-14s %s\n", $$1, $$2}'
