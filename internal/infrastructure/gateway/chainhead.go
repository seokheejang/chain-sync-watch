package gateway

import (
	"context"
	"errors"
	"fmt"

	"github.com/seokheejang/chain-sync-watch/adapters/rpc"
	"github.com/seokheejang/chain-sync-watch/internal/application"
	"github.com/seokheejang/chain-sync-watch/internal/chain"
)

// RPCChainHead implements application.ChainHead by reading the
// sources table for the target chain, finding the first enabled
// rpc-type row, and calling its Tip / Finalized JSON-RPC methods.
//
// The implementation is deliberately lean: no caching, no failover
// across multiple RPC rows (UNIQUE(type, chain_id) caps the MVP at
// one anyway). When the multi-instance redundancy story lands, this
// is where load-balancing / quorum logic would plug in.
type RPCChainHead struct {
	repo application.SourceConfigRepository
}

// NewRPCChainHead wires the ChainHead around a SourceConfigRepository.
// chainhead is stateless beyond that reference.
func NewRPCChainHead(repo application.SourceConfigRepository) *RPCChainHead {
	return &RPCChainHead{repo: repo}
}

// ErrNoRPCSource surfaces when a chain has no enabled rpc row. The
// verification engine treats this as a fatal misconfiguration
// rather than a transient error.
var ErrNoRPCSource = errors.New("chainhead: no enabled rpc source for chain")

// Tip returns the current head block height for the chain.
func (h *RPCChainHead) Tip(ctx context.Context, chainID chain.ChainID) (chain.BlockNumber, error) {
	adapter, err := h.adapterFor(ctx, chainID)
	if err != nil {
		return 0, err
	}
	return adapter.Tip(ctx)
}

// Finalized returns the latest finalized block for the chain.
// Optimism has native `finalized` tag support; other chains may
// need a policy layer that falls back to Tip - N.
func (h *RPCChainHead) Finalized(ctx context.Context, chainID chain.ChainID) (chain.BlockNumber, error) {
	adapter, err := h.adapterFor(ctx, chainID)
	if err != nil {
		return 0, err
	}
	return adapter.Finalized(ctx)
}

// adapterFor loads the rpc source config for chainID and constructs
// a fresh *rpc.Adapter. A new adapter per call keeps the struct
// stateless and avoids concurrent-use concerns — the cost is a
// struct alloc which is negligible next to the HTTP round-trip.
func (h *RPCChainHead) adapterFor(ctx context.Context, chainID chain.ChainID) (*rpc.Adapter, error) {
	rows, err := h.repo.ListByChain(ctx, chainID, true)
	if err != nil {
		return nil, fmt.Errorf("chainhead: list by chain %d: %w", chainID, err)
	}
	for _, cfg := range rows {
		if cfg.Type != TypeRPC {
			continue
		}
		var opts []rpc.Option
		if b, ok := cfg.Options["archive"].(bool); ok && b {
			opts = append(opts, rpc.WithArchive(true))
		}
		adapter, err := rpc.New(cfg.ChainID, cfg.Endpoint, opts...)
		if err != nil {
			return nil, fmt.Errorf("chainhead: build rpc %q: %w", cfg.ID, err)
		}
		return adapter, nil
	}
	return nil, fmt.Errorf("%w: chain %d", ErrNoRPCSource, chainID)
}

// Compile-time assertion.
var _ application.ChainHead = (*RPCChainHead)(nil)
