// Package rpc implements the source.Source contract against a
// JSON-RPC execution client (geth, erigon, op-geth, nethermind, ...).
//
// The adapter is the RPC-canonical backbone of the tier-A verification
// path: everything a client's execution node can serve as ground truth
// flows through here. We intentionally do not pull in go-ethereum —
// raw JSON-RPC over our own adapters/internal/httpx client keeps the
// binary small, inherits retry/rate-limit for free, and makes the
// wire contract easy to audit from the codebase alone.
//
// Archive and debug-trace capabilities are off by default. Operators
// flip them on via WithArchive / WithDebugTrace once they know their
// node supports the corresponding method set — auto-detection is
// unreliable across node implementations and we'd rather surface
// ErrUnsupported than a silent wrong answer.
package rpc

import (
	"context"
	"errors"
	"time"

	"github.com/seokheejang/chain-sync-watch/adapters/internal/httpx"
	"github.com/seokheejang/chain-sync-watch/internal/chain"
	"github.com/seokheejang/chain-sync-watch/internal/source"
)

// ID is the SourceID every rpc.Adapter reports.
const ID source.SourceID = "rpc"

// Adapter is the JSON-RPC source implementation.
type Adapter struct {
	chainID chain.ChainID
	url     string
	hc      *httpx.Client

	archive    bool
	debugTrace bool

	reqID uint64 // monotonic JSON-RPC request id; bumped atomically
}

// Option configures an Adapter at construction time.
type Option func(*Adapter)

// WithArchive declares that the upstream node serves historical state
// for every block (eth_getBalance at arbitrary heights, etc.). Only
// flip this on when the node is confirmed archive — non-archive nodes
// return "missing trie node" for old blocks, which we surface as an
// InvalidResponse rather than a silent wrong value.
func WithArchive(enabled bool) Option {
	return func(a *Adapter) { a.archive = enabled }
}

// WithDebugTrace enables debug_trace* methods. Public RPCs strip
// these; private nodes must be started with --http.api=debug,eth (or
// equivalent). If the upstream rejects the method, the capability is
// effectively false at runtime — the adapter will return
// ErrUnsupported wrapping the underlying RPC error.
func WithDebugTrace(enabled bool) Option {
	return func(a *Adapter) { a.debugTrace = enabled }
}

// WithHTTPX replaces the internal HTTP client. Useful for injecting a
// shared rate limiter across many adapters that point at the same
// node, or for test transports.
func WithHTTPX(c *httpx.Client) Option {
	return func(a *Adapter) {
		if c != nil {
			a.hc = c
		}
	}
}

// New constructs an Adapter. chainID identifies the chain (used in
// Source.ChainID and never forwarded to the node — JSON-RPC is
// chain-agnostic at the method level), url is the full RPC endpoint
// (e.g., "https://rpc.example.com" or a local "http://host:8545").
func New(chainID chain.ChainID, url string, opts ...Option) (*Adapter, error) {
	if chainID == 0 {
		return nil, errors.New("rpc: chain id is required")
	}
	if url == "" {
		return nil, errors.New("rpc: url is required")
	}
	a := &Adapter{
		chainID: chainID,
		url:     url,
		hc: httpx.New(
			httpx.WithTimeout(30*time.Second),
			httpx.WithRateLimit(20, 5),
		),
	}
	for _, opt := range opts {
		opt(a)
	}
	return a, nil
}

// --- source.Source identity ---------------------------------------------

// ID returns the stable source identifier.
func (a *Adapter) ID() source.SourceID { return ID }

// ChainID returns the chain this adapter is wired for.
func (a *Adapter) ChainID() chain.ChainID { return a.chainID }

// Supports reports whether this adapter can serve a Capability. The
// mapping reflects the RPC-canonical slice plus conditional archive /
// debug gates; chain-wide aggregates and ERC-20 holdings have no
// practical RPC implementation and always return false.
func (a *Adapter) Supports(c source.Capability) bool {
	switch c {
	// Block immutable fields — every node serves these.
	case source.CapBlockHash,
		source.CapBlockParentHash,
		source.CapBlockTimestamp,
		source.CapBlockTxCount,
		source.CapBlockGasUsed,
		source.CapBlockStateRoot,
		source.CapBlockReceiptsRoot,
		source.CapBlockTransactionsRoot,
		source.CapBlockMiner:
		return true

	// Address state at latest/finalized/safe — full nodes only.
	case source.CapBalanceAtLatest,
		source.CapNonceAtLatest,
		source.CapTxCountAtLatest:
		return true

	// Historical address state — archive only.
	case source.CapBalanceAtBlock,
		source.CapNonceAtBlock:
		return a.archive

	// ERC-20 balance of a specific token via eth_call — every node.
	case source.CapERC20BalanceAtLatest:
		return true

	// Trace endpoints — require debug_trace* activation.
	case source.CapInternalTxByTx,
		source.CapInternalTxByBlock:
		return a.debugTrace

	// Chain-wide cumulative + ERC-20 holdings list — RPC cannot serve
	// these without expensive reconstruction we refuse to do here.
	case source.CapTotalAddressCount,
		source.CapTotalTxCount,
		source.CapTotalContractCount,
		source.CapERC20TokenCount,
		source.CapERC20HoldingsAtLatest:
		return false
	}
	return false
}

// --- source.Source: unsupported methods ---------------------------------

// FetchSnapshot reports Unsupported: chain-wide cumulative aggregates
// are not discoverable from a single JSON-RPC node without an
// off-chain index.
func (a *Adapter) FetchSnapshot(_ context.Context, _ source.SnapshotQuery) (source.SnapshotResult, error) {
	return source.SnapshotResult{SourceID: ID}, source.ErrUnsupported
}

// FetchERC20Holdings reports Unsupported: listing every token an
// address holds would require a full Transfer-log scan across the
// chain, which is wildly impractical for per-run verification.
func (a *Adapter) FetchERC20Holdings(_ context.Context, _ source.ERC20HoldingsQuery) (source.ERC20HoldingsResult, error) {
	return source.ERC20HoldingsResult{SourceID: ID}, source.ErrUnsupported
}
