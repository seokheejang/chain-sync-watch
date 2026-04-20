package source

import (
	"context"

	"github.com/seokheejang/chain-sync-watch/internal/chain"
)

// Source is the abstract contract every adapter implements. The four
// Fetch* methods are split by "query shape" rather than by Capability:
// a single call packs every field the source can serve into one trip,
// minimising round-trips to upstream services. The caller consults
// Supports(...) to learn which result fields will actually be
// populated.
//
// Conventions all adapters must follow:
//
//   - Any transport-layer error MUST be normalised to one of the
//     sentinel errors in errors.go via errors.Is-friendly wrapping
//     (fmt.Errorf("adapter X: %w", source.ErrRateLimited)).
//   - Unsupported capabilities MUST either return ErrUnsupported from
//     the relevant Fetch call, or leave the corresponding result
//     fields nil while still returning nil error. Both are valid; the
//     former is preferred when the entire query type is unreachable
//     (e.g., RPC adapter on a non-archive node asked for
//     AddressAtBlock).
//   - Context cancellation MUST be honoured promptly. Adapters that
//     fan out should wrap downstream calls with context.WithCancel and
//     return ctx.Err() when the parent cancels.
type Source interface {
	// ID is a stable identifier for logs, metrics, and diff attribution.
	// Each adapter constructor sets it.
	ID() SourceID

	// ChainID is the chain this instance is configured for. One adapter
	// serves one chain; multi-chain setups wire up multiple instances.
	ChainID() chain.ChainID

	// Supports reports whether a Capability is (a) implemented by the
	// adapter's code AND (b) available on the underlying service
	// (archive-mode RPC, PRO-tier API, etc.). Callers must consult this
	// before trusting any optional field on the corresponding Result.
	Supports(Capability) bool

	// FetchBlock retrieves block-level metadata by height. Returns
	// ErrNotFound for a height the source has not indexed.
	FetchBlock(ctx context.Context, q BlockQuery) (BlockResult, error)

	// FetchAddressLatest retrieves account state at the source's
	// current notion of "latest". Two sources observed at the same wall
	// clock may race each other by a block or two.
	FetchAddressLatest(ctx context.Context, q AddressQuery) (AddressLatestResult, error)

	// FetchAddressAtBlock retrieves historical account state. Adapters
	// backed by non-archive nodes must return ErrUnsupported.
	FetchAddressAtBlock(ctx context.Context, q AddressAtBlockQuery) (AddressAtBlockResult, error)

	// FetchSnapshot retrieves chain-wide cumulative counters. The
	// semantics differ per source (see docs/research/source-shapes.md);
	// judgement over snapshots is observational by default.
	FetchSnapshot(ctx context.Context, q SnapshotQuery) (SnapshotResult, error)

	// FetchERC20Balance returns a single-token balance for an address
	// at the query's Anchor. Tier C — RPC adapters serve it via
	// eth_call to balanceOf(), 3rd-party indexers via cached reads.
	// Adapters that cannot honour a numeric anchor on this call must
	// return ErrUnsupported rather than silently fall back to latest.
	FetchERC20Balance(ctx context.Context, q ERC20BalanceQuery) (ERC20BalanceResult, error)

	// FetchERC20Holdings returns every ERC-20 balance held by an
	// address at the query's Anchor. Tier B — canonical only from
	// indexers; RPC-backed adapters return ErrUnsupported.
	FetchERC20Holdings(ctx context.Context, q ERC20HoldingsQuery) (ERC20HoldingsResult, error)

	// FetchInternalTxByTx returns the internal-call trace for one
	// transaction. Tier C — RPC via debug_traceTransaction (archive
	// required), indexers via their trace cache.
	FetchInternalTxByTx(ctx context.Context, q InternalTxByTxQuery) (InternalTxResult, error)

	// FetchInternalTxByBlock returns every internal call recorded in a
	// block across all of its transactions. Tier C.
	FetchInternalTxByBlock(ctx context.Context, q InternalTxByBlockQuery) (InternalTxResult, error)
}
