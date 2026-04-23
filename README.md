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
cp .env.example .env              # edit: paste CSW_SECRET_KEY, any RPC overrides
make up                           # postgres + redis via docker compose
make migrate                      # apply schema
make seed                         # seed sources from defaults.yaml (one-shot)
make run-server                   # csw-server on :8080
make run-worker                   # csw-worker (separate shell)
make web-dev                      # frontend on :3000 (separate shell)
```

Generate the AES-GCM master key once before `make seed`:

```bash
echo "CSW_SECRET_KEY=$(openssl rand -base64 32)" >> .env
```

## Full stack via Docker Compose

Everything (server + worker + web + postgres + redis) in one command:

```bash
make stack-up                     # builds images, starts the whole stack
# → web on http://localhost:3000, API on :8080
make stack-logs                   # tail all services
make stack-down                   # stop and remove containers
```

The `app` profile pins the build so a fresh clone is one command from
a running dashboard. `make stack-auth` layers a Caddy reverse proxy
with HTTP basic auth in front — aimed at team-shared deployments
where LAN access isn't enough. Generate the credential hash with:

```bash
docker run --rm caddy:2.8-alpine caddy hash-password -p 'your-password'
# paste into .env as CSW_AUTH_HASH + CSW_AUTH_USER
```

## Security checklist

* **`CSW_SECRET_KEY`** is the AES-GCM master for every 3rd-party
  credential in the DB. Rotating it requires re-encrypting every
  `sources` row; losing it makes stored secrets unrecoverable. Store
  the value in a secrets manager for any non-local deployment.
* **Never commit `.env`.** Git already ignores it; the one-shot
  `.env.example` is the template.
* The Go HTTP API has **no authentication** on its own. Phase 10b's
  reverse-proxy profile (`make stack-auth`) is the MVP answer for
  team-shared deployments; Phase 10c (OIDC / oauth2-proxy) is
  tracked in the plan docs.
* `/sources` returns `has_secret: bool` — **never the ciphertext**.
  Inspect the database directly if you need to audit the encrypted
  payload.
* Database dumps are safe (ciphertext only), but pair them with the
  master key in a restore runbook — losing either half corrupts
  recovery.

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
