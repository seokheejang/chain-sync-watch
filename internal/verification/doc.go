// Package verification models the verification world: what we check,
// when we check it, and how samples are selected.
//
// It is a pure-domain package — no I/O, no frameworks, stdlib plus
// internal/chain and internal/source only. That constraint is enforced
// by the depguard rule in .golangci.yml; keep imports minimal so the
// domain stays buildable without any adapter or transport dependency.
//
// The primary types are:
//
//   - Metric — a comparable field, classified by MetricCategory. The
//     category drives the downstream comparison policy (strict equality
//     vs observational); the Capability it wraps determines which
//     Sources can actually serve it.
//   - SamplingStrategy — deterministic block selection (FixedList,
//     LatestN, Random, SparseSteps). Sampling is pure: the caller
//     injects a Context carrying the current tip, so unit tests need no
//     clock or RPC.
//   - Trigger and Schedule — what kicks a Run off (manual / scheduled /
//     realtime). Trigger uses Go's sealed-type idiom so callers can
//     exhaustively switch without the risk of an unknown implementation
//     slipping in from outside the package.
//   - Run — one instance of a verification pass. It owns a Trigger, a
//     SamplingStrategy, a list of Metrics, and a lifecycle state
//     machine (pending → running → completed/failed/cancelled).
//
// The package deliberately returns values, not ORM rows: persistence
// mappers live in internal/infrastructure and translate between this
// domain model and their storage representation.
package verification
