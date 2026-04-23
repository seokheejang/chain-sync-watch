# Custom GraphQL adapter — pattern guide

Bundled adapters cover the three public source types
(`rpc` / `blockscout` / `routescan`). If you run an **internal
indexer** with a private GraphQL schema, the `private/` + build-tag
pattern keeps your API contract out of the OSS repository while
still letting you plug into the verification engine.

This directory is a **template**, not a working adapter. Copy it
into `private/<yourname>/` (gitignored), edit the GraphQL queries
to match your real schema, and wire it through the build-tag hook.

## Layout

```
private/<yourname>/
├── adapter.go      Adapter struct + New + Supports + call helper
├── block.go        FetchBlock + Tip + (optional) Finalized
├── address.go      FetchAddressLatest / ERC-20 helpers / unsupported stubs
├── wire.go         //go:build private — Register(reg) called by csw-server
├── introspection.json    optional: dumped schema (also gitignored)
└── README.md       your operational notes
```

Fields / query strings / endpoints live **only** inside
`private/`. Never copy them into `examples/` or any other
tracked path.

## Step 1 — Dump your schema

With your VPN / proxy in place:

```bash
npx -y get-graphql-schema http://<internal>/graphql \
  > private/myindexer/schema.graphql
```

Or raw introspection JSON if you prefer parsing it programmatically
(see the `introspection.json` line in the template).

## Step 2 — Copy the template

```bash
cp -r examples/custom-graphql-adapter private/myindexer
cd private/myindexer
# Edit package name, GraphQL queries, response field names to match
# your real schema.
```

Key edits:

* Replace the `TypeName` constant (e.g. `"myindexer"` or
  `"internal-explorer"`). This is the string `/sources` CRUD stores
  in the `type` column and surfaces in the UI dropdown.
* Update `chainNameByID` if your indexer keys by chain slug rather
  than numeric ID (or drop the lookup entirely if it's single-chain).
* Replace the GraphQL query strings (`blockQuery`, `addressQuery`,
  etc.) with queries valid against your schema.
* Update the `raw` structs (`blockRaw`, `addressRaw`) to match your
  response field names.
* Implement only the capabilities your indexer can serve — leave
  `source.ErrUnsupported` for the rest.

## Step 3 — Wire the build tag

The public binary already contains the `!private` stubs. The
template's `wire.go` uses `//go:build private` so adding your
package to a `registerPrivateAdapters` call in
`cmd/csw-server/private_on.go` + `cmd/csw-worker/private_on.go`
is all the wiring needed. Those files ship with an example
`myindexer.Register(reg)` line — adapt the import path to your
package.

## Step 4 — Build

```bash
make build-private
```

Both binaries land in `bin/`, tagged with `-tags=private` so your
package is linked in. Public / CI builds use plain `make build`
and never see `private/`.

## Step 5 — Deploy

1. Add a `/sources` row via the UI (type = `myindexer`, endpoint =
   your GraphQL URL, paste api key if needed).
2. Create a run via `/runs/new` — the worker materialises your
   adapter alongside the public ones and produces a 4-way
   comparison.

## Public-template example (fake schema)

The `adapter.go` / `block.go` / `address.go` files in this
directory use a deliberately **made-up** schema:

```graphql
query Block($number: Int!) {
  block(number: $number) {
    hash parentHash timestamp transactionCount gasUsed miner
  }
}

query Address($address: String!) {
  accountByAddress(address: $address) {
    balanceWei nonce txCount
  }
}
```

Nothing in the above corresponds to any real deployment. The
template is here so a Go developer can see the wiring shape
without needing to reason from scratch about `source.Source` vs
GraphQL-over-HTTP.

## Dos and don'ts

* ✅ Keep every private field name / endpoint / auth header under
  `private/`. It is already gitignored.
* ✅ Return `source.ErrUnsupported` for capabilities your indexer
  can't serve — don't fudge data.
* ✅ Use `chain.NewAddress` / `chain.NewHash32` to normalise raw
  strings before assigning to result pointers — the comparator
  relies on canonical forms (EIP-55 / lowercase hex).
* ❌ Never paste your real schema into the public repo, PR
  descriptions, or tests.
* ❌ Don't log raw request/response bodies at Info — credential
  headers and internal IDs have a habit of leaking into shipped
  log aggregators. Use Debug at most.
