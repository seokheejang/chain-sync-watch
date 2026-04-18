# chain-sync-watch

> N-way verification of blockchain indexer data against independent sources
> (RPC, Blockscout, Etherscan, custom adapters).

[![CI](https://github.com/seokheejang/chain-sync-watch/actions/workflows/ci.yml/badge.svg)](https://github.com/seokheejang/chain-sync-watch/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](./LICENSE)

**chain-sync-watch** is a Go server + Next.js dashboard that cross-checks
multiple independent sources for a chain indexer and surfaces
discrepancies by severity. Primary target is Optimism mainnet; the
architecture is multichain-ready.

## Why

Running a chain indexer and trusting its output is two different things.
This tool continuously answers "does our indexer agree with a public
explorer and with the raw RPC node?" for a configurable set of metrics
(block immutables, address balances, cumulative stats), and records the
cases where it doesn't.

## Architecture

```
internal/                    core — abstract only
  source/                    Source port + field-level Capability
  chain/                     value objects (Address, BlockNumber, ...)
  verification/ diff/        pure domain
  application/               use cases + ports
  infrastructure/            persistence / queue / httpapi

adapters/                    bundled Source implementations
  rpc/ blockscout/ etherscan/

examples/
  custom-graphql-adapter/    pattern for user-defined adapters
```

The core (`internal/source/`) imports zero concrete adapters. Users wire
up only the adapters they need in `cmd/csw-server/main.go`, the same
driver-registration pattern as `database/sql`.

See [docs/plans/README.md](docs/plans/README.md) for the full 12-phase
implementation plan and architectural decisions.

## Stack

| Layer | Choice |
|---|---|
| Language | Go 1.24 |
| HTTP | chi + huma (OpenAPI 3.1 auto-generated) |
| Queue | Redis 7.4 + asynq |
| Database | Postgres 17 + gorm |
| Config | koanf (embedded YAML + env overrides) |
| Logging | `log/slog` |
| Frontend | Next.js 15 (App Router, SSR) + shadcn/ui + Tailwind + TanStack Query |
| Node | 22 LTS + pnpm 10 |

## Quick start

**Prereqs**: Go 1.24+, Docker, Make.

```bash
cp .env.example .env              # fill in if you want Etherscan etc.
make up                           # postgres + redis via docker compose
make test                         # unit tests
```

Phase 0 (foundations) is the minimum set of files currently present.
Running `make run-server` works once Phase 8 (HTTP API) lands; see the
plan docs for progress.

## Project status

This project is under active development. See
[docs/plans/README.md](docs/plans/README.md) for the phase-by-phase
progress tracker.

**MVP scope** (Phases 0–10): single-chain (Optimism) 3-way verification
with bundled RPC + Blockscout + Etherscan adapters, HTTP API, dashboard
frontend, and docker-compose deployment.

**Phase 11** adds a Helm chart for production Kubernetes deployment.

## Contributing

1. Read [docs/plans/README.md](docs/plans/README.md) to understand the
   current phase.
2. Follow [Conventional Commits](https://www.conventionalcommits.org/)
   for messages (`feat:`, `fix:`, `docs:`, `refactor:`, `test:`,
   `chore:`, ...).
3. Respect the DDD boundaries — the `depguard` rules in `.golangci.yml`
   enforce them.
4. Never commit anything under `private/` (gitignored; for local-only
   custom adapters).

## License

[MIT](./LICENSE)
