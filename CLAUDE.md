# Claude guidance for chain-sync-watch

This file is loaded automatically by Claude Code. Keep it short; link into
the plan docs for anything long.

## What this project is

A Go + Next.js tool that verifies a chain indexer's data by cross-checking
against multiple independent sources (RPC node, Blockscout, Etherscan,
user-provided custom adapters). N-way comparison; disagreements become
`Discrepancy` records that operators review via the dashboard.

Primary target chain: Optimism mainnet (chainid=10). Multichain-ready.

## Architecture in one paragraph

Go 1.24 + DDD + TDD, `database/sql`-driver pattern for source adapters.
Core abstractions live in `internal/source/` (pure port interface). Bundled
concrete adapters live in `adapters/{rpc,blockscout,etherscan}/` — each an
independent Go package so users import only what they need. User-specific
indexer adapters belong in `private/` (gitignored) or a separate private
repo. Wiring happens only at `cmd/csw-server/main.go`.

Stack: chi + huma (OpenAPI 3.1) / Postgres 17 + gorm / Redis 7.4 + asynq /
Next.js 15 + shadcn/ui + TanStack Query.

## Repository layout

```
internal/                    core — abstract only, no concrete adapters
  chain/                     value objects (Address, BlockNumber, ChainID…)
  source/                    Source port + field-level Capability + Query/Result
  verification/ diff/        pure domain
  application/               use cases + ports
  infrastructure/            persistence / queue / httpapi
  config/                    koanf-based loader + embedded defaults.yaml
  observability/             slog + context helpers
adapters/                    bundled Source implementations (phase 3)
examples/                    reference skeletons, incl. custom-graphql-adapter
private/                     gitignored; never commit
configs/                     config.example.yaml (template) + config.local.yaml (gitignored)
docs/plans/                  12-phase implementation plan (start here)
docs/research/               source-shapes.md (capability matrix)
```

Plan docs are the authoritative specification. When in doubt, read
[docs/plans/README.md](docs/plans/README.md) first.

## Common commands

Development flow uses Make throughout. The project's `.claude/settings.json`
already whitelists `go`, `make`, `docker`, `pnpm`, `npm`, and read-only git
for auto-execution.

```bash
make deps              # go mod download
make fmt               # gofumpt -l -w .
make fmt-check         # CI-style format check
make lint              # golangci-lint (v2 config)
make vet               # go vet ./...
make test              # unit tests + race + coverage
make test-integration  # testcontainers-go (tagged "integration")
make test-e2e          # tagged "e2e"
make build             # build all three binaries into ./bin/
make run-server        # go run ./cmd/csw-server
make run-worker        # go run ./cmd/csw-worker
make up                # docker compose up -d (postgres + redis)
make down              # docker compose down
make migrate           # csw migrate up
make openapi           # dump OpenAPI 3.1 to web/openapi.json
```

## Critical rules (do not violate)

1. **Never commit anything under `private/`.** This directory holds custom
   indexer adapters whose schema must not be published. `.gitignore`
   already excludes it; do not override. Prefer explicit `git add <paths>`
   over `git add -A` or `git add .` to prevent accidents.

2. **DDD boundaries.** The packages `internal/chain`, `internal/source`,
   `internal/verification`, `internal/diff` must not import frameworks
   (gorm, huma, asynq, ethclient, chi). `.golangci.yml` has a `depguard`
   rule that blocks these imports — do not disable it.

3. **No internal/sensitive data in the repo.** URLs, IPs, internal API
   schemas, observation snapshots that include real internal numbers —
   all must stay out of code, docs, tests, and fixtures. The
   `docs/research/source-shapes.md` file is checked specifically for this.

4. **OSS-friendly defaults.** This repo can be made public. Default
   endpoints in `internal/config/defaults.yaml` must be publicly
   accessible (e.g., `https://optimism-rpc.publicnode.com`,
   `https://optimism.blockscout.com`). Private overrides go through
   `configs/config.local.yaml` or `CSW_*` env vars.

5. **TDD order.** Domain tests first, then application (with fake ports),
   then infrastructure (with testcontainers for Postgres, miniredis for
   Redis). Black-box testing via `package xxx_test` keeps the domain
   boundary honest.

## Configuration precedence

Later layers win:

1. Embedded defaults — `internal/config/defaults.yaml` compiled into the
   binary via `go:embed`. Single source of truth.
2. Optional local override — `configs/config.local.yaml` (gitignored).
3. Environment variables with `CSW_` prefix, `__` between nested keys,
   e.g. `CSW_ADAPTERS__RPC__ENDPOINTS__10=https://...`.

Secrets (DB password, API keys) live in `.env`, never in YAML.

## Binary / directory name convention

Following Go's convention where the directory name becomes the binary
name:

- `cmd/csw-server/` → `csw-server` (HTTP API)
- `cmd/csw-worker/` → `csw-worker` (asynq worker)
- `cmd/csw/` → `csw` (CLI with `migrate`, `openapi-dump` subcommands)

## Where to find things

| Question | Look here |
|---|---|
| Project overview & phase plan | [docs/plans/README.md](docs/plans/README.md) |
| Source capability matrix | [docs/research/source-shapes.md](docs/research/source-shapes.md) |
| DDD boundary rules | [.golangci.yml](.golangci.yml) `depguard` section |
| Default configuration | [internal/config/defaults.yaml](internal/config/defaults.yaml) |
| Secrets / env overrides | [.env.example](.env.example) |
| Auto-approved shell commands | [.claude/settings.json](.claude/settings.json) |
