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

.PHONY: build-private
build-private: ## Build csw-server + csw-worker with private/* adapters (`-tags=private`). Requires non-empty private/.
	@mkdir -p $(BIN_DIR)
	CGO_ENABLED=0 $(GO) build -tags=private -trimpath -ldflags='-s -w' -o $(BIN_DIR)/csw-server ./cmd/csw-server
	CGO_ENABLED=0 $(GO) build -tags=private -trimpath -ldflags='-s -w' -o $(BIN_DIR)/csw-worker ./cmd/csw-worker
	@echo "Built private-tagged binaries in $(BIN_DIR)/"

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

.PHONY: seed
seed: ## Seed sources table from defaults.yaml (one-shot; fails if already populated)
	$(GO) run ./cmd/csw migrate seed

# -------------------- OpenAPI --------------------

.PHONY: openapi
openapi: ## Dump OpenAPI spec to web/openapi.json
	@mkdir -p web
	$(GO) run ./cmd/csw openapi-dump > web/openapi.json
	@echo "Wrote web/openapi.json"

# -------------------- Frontend --------------------

.PHONY: web-deps
web-deps: ## Install frontend dependencies (pnpm install)
	cd web && pnpm install

.PHONY: web-gen
web-gen: openapi ## Regenerate frontend API types from the fresh spec
	cd web && pnpm gen:api

.PHONY: web-dev
web-dev: ## Run the Next.js dev server (http://localhost:3000)
	cd web && pnpm dev

.PHONY: web-build
web-build: ## Production build of the frontend
	cd web && pnpm build

.PHONY: web-lint
web-lint: ## Biome check (frontend)
	cd web && pnpm lint

# -------------------- Docker compose --------------------

.PHONY: up
up: ## Start local infra (postgres, redis)
	$(COMPOSE) up -d

.PHONY: down
down: ## Stop local infra
	$(COMPOSE) down

.PHONY: stack-up
stack-up: ## Full stack via compose (postgres + redis + server + worker + web)
	$(COMPOSE) --profile app up -d --build

.PHONY: stack-down
stack-down: ## Stop the full stack
	$(COMPOSE) --profile app --profile auth --profile tools down

.PHONY: stack-logs
stack-logs: ## Tail logs for the full stack
	$(COMPOSE) --profile app logs -f

.PHONY: stack-auth
stack-auth: ## Full stack with caddy basic-auth reverse proxy in front
	$(COMPOSE) --profile app --profile auth up -d --build

.PHONY: logs
logs: ## Tail docker compose logs
	$(COMPOSE) logs -f

.PHONY: ps
ps: ## List docker compose services
	$(COMPOSE) ps

# -------------------- Helm / kind --------------------

KIND_CLUSTER ?= csw-smoke

.PHONY: helm-deps
helm-deps: ## helm dependency update for the bundled chart
	helm dependency update deploy/helm/chain-sync-watch

.PHONY: helm-lint
helm-lint: helm-deps ## helm lint with each environment overlay
	@for env in dev staging prod; do \
		echo "=== $$env ==="; \
		helm lint deploy/helm/chain-sync-watch \
			-f deploy/helm/chain-sync-watch/environments/values.$$env.yaml \
			--set secrets.CSW_SECRET_KEY=ci-dummy || exit 1; \
	done

.PHONY: helm-template
helm-template: helm-deps ## helm template the prod overlay (read-only render)
	helm template csw deploy/helm/chain-sync-watch \
		-f deploy/helm/chain-sync-watch/environments/values.prod.yaml

.PHONY: kind-smoke
kind-smoke: ## Spin up kind, install chart, hit /healthz (full smoke test)
	deploy/scripts/kind-smoke.sh

.PHONY: kind-smoke-down
kind-smoke-down: ## Tear down the kind smoke cluster
	kind delete cluster --name $(KIND_CLUSTER)

# -------------------- Private indexer tunnel --------------------
# The proxy bridges localhost:19999 to an internal indexer reachable
# only via VPN. Script + hard-coded target IP live under private/
# (gitignored) — the Makefile targets stay public but skip silently
# if the script is absent (fresh clones / CI).

TUNNEL_SCRIPT := private/scripts/csw-tunnel.py
TUNNEL_PID    := private/scripts/csw-tunnel.pid
TUNNEL_LOG    := private/scripts/csw-tunnel.log

.PHONY: tunnel-up
tunnel-up: ## Start the private indexer tunnel in the background
	@test -f $(TUNNEL_SCRIPT) || { echo "missing $(TUNNEL_SCRIPT) — skipping"; exit 0; }
	@if [ -f $(TUNNEL_PID) ] && kill -0 $$(cat $(TUNNEL_PID)) 2>/dev/null; then \
	  echo "tunnel already running (pid $$(cat $(TUNNEL_PID)))"; exit 0; \
	fi; \
	port=$${CSW_TUNNEL_LISTEN##*:}; port=$${port:-19999}; \
	if lsof -nP -iTCP:$$port -sTCP:LISTEN >/dev/null 2>&1; then \
	  echo "port $$port already bound (external tunnel?) — skipping"; \
	  lsof -nP -iTCP:$$port -sTCP:LISTEN | tail -n +1 | head -5; exit 0; \
	fi; \
	nohup python3 $(TUNNEL_SCRIPT) >$(TUNNEL_LOG) 2>&1 & pid=$$!; echo $$pid > $(TUNNEL_PID); \
	sleep 0.3; \
	if kill -0 $$pid 2>/dev/null; then \
	  echo "tunnel started (pid $$pid, log $(TUNNEL_LOG))"; \
	else \
	  rm -f $(TUNNEL_PID); \
	  echo "tunnel failed to stay up — last log lines:"; \
	  tail -5 $(TUNNEL_LOG); exit 1; \
	fi

.PHONY: tunnel-down
tunnel-down: ## Stop the private indexer tunnel
	@if [ -f $(TUNNEL_PID) ] && kill -0 $$(cat $(TUNNEL_PID)) 2>/dev/null; then \
	  kill $$(cat $(TUNNEL_PID)); rm -f $(TUNNEL_PID); \
	  echo "tunnel stopped"; \
	else \
	  rm -f $(TUNNEL_PID); echo "tunnel not running"; \
	fi

.PHONY: tunnel-status
tunnel-status: ## Show tunnel state
	@if [ -f $(TUNNEL_PID) ] && kill -0 $$(cat $(TUNNEL_PID)) 2>/dev/null; then \
	  echo "tunnel running (pid $$(cat $(TUNNEL_PID)))"; \
	else \
	  echo "tunnel not running"; \
	fi

.PHONY: tunnel-logs
tunnel-logs: ## Tail the tunnel log
	@test -f $(TUNNEL_LOG) && tail -f $(TUNNEL_LOG) || echo "no log yet ($(TUNNEL_LOG))"
