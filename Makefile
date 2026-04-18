.DEFAULT_GOAL := help
SHELL := /bin/bash

# Binary output directory
BIN_DIR := bin

# Go tooling
GO              := go
GOFUMPT         := gofumpt
GOLANGCI_LINT   := golangci-lint

# Docker compose (v2)
COMPOSE := docker compose

# Build targets
BINARIES := csw-server csw-worker csw

.PHONY: help
help: ## Show this help
	@awk 'BEGIN {FS = ":.*##"; printf "Usage: make <target>\n\nTargets:\n"} /^[a-zA-Z_-]+:.*?##/ { printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2 }' $(MAKEFILE_LIST)

# -------------------- Dependencies --------------------

.PHONY: deps
deps: ## Download Go modules
	$(GO) mod download

.PHONY: tidy
tidy: ## Tidy go.mod / go.sum
	$(GO) mod tidy

# -------------------- Formatting & Linting --------------------

.PHONY: fmt
fmt: ## Format Go source with gofumpt
	$(GOFUMPT) -l -w .

.PHONY: fmt-check
fmt-check: ## Check formatting (CI)
	@diff=$$($(GOFUMPT) -l .); \
	if [ -n "$$diff" ]; then \
	  echo "Files need formatting:"; echo "$$diff"; exit 1; \
	fi

.PHONY: lint
lint: ## Run golangci-lint
	$(GOLANGCI_LINT) run

.PHONY: vet
vet: ## Run go vet
	$(GO) vet ./...

# -------------------- Testing --------------------

.PHONY: test
test: ## Run unit tests with race detector + coverage
	$(GO) test ./... -race -coverprofile=coverage.out -covermode=atomic

.PHONY: test-integration
test-integration: ## Run integration tests (testcontainers)
	$(GO) test ./... -tags=integration -race

.PHONY: test-e2e
test-e2e: ## Run end-to-end tests
	$(GO) test ./... -tags=e2e -race

.PHONY: coverage-html
coverage-html: test ## Generate HTML coverage report
	$(GO) tool cover -html=coverage.out -o coverage.html
	@echo "Report: coverage.html"

# -------------------- Build --------------------

.PHONY: build
build: $(addprefix build-,$(BINARIES)) ## Build all binaries

.PHONY: build-%
build-%: ## Build a single binary (e.g. make build-csw-server)
	@mkdir -p $(BIN_DIR)
	CGO_ENABLED=0 $(GO) build -trimpath -ldflags='-s -w' -o $(BIN_DIR)/$* ./cmd/$*

.PHONY: clean
clean: ## Remove build artifacts
	rm -rf $(BIN_DIR) coverage.out coverage.html

# -------------------- Run --------------------

.PHONY: run-server
run-server: ## Run csw-server locally
	$(GO) run ./cmd/csw-server

.PHONY: run-worker
run-worker: ## Run csw-worker locally
	$(GO) run ./cmd/csw-worker

# -------------------- Database / migrations --------------------

.PHONY: migrate
migrate: ## Apply pending DB migrations (csw migrate up)
	$(GO) run ./cmd/csw migrate up

.PHONY: migrate-down
migrate-down: ## Roll back last DB migration
	$(GO) run ./cmd/csw migrate down

.PHONY: migrate-status
migrate-status: ## Show migration status
	$(GO) run ./cmd/csw migrate status

# -------------------- OpenAPI --------------------

.PHONY: openapi
openapi: ## Dump OpenAPI spec to web/openapi.json
	@mkdir -p web
	$(GO) run ./cmd/csw openapi-dump > web/openapi.json
	@echo "Wrote web/openapi.json"

# -------------------- Docker compose --------------------

.PHONY: up
up: ## Start local infra (postgres, redis)
	$(COMPOSE) up -d

.PHONY: down
down: ## Stop local infra
	$(COMPOSE) down

.PHONY: logs
logs: ## Tail docker compose logs
	$(COMPOSE) logs -f

.PHONY: ps
ps: ## List docker compose services
	$(COMPOSE) ps
