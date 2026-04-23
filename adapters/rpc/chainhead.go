package rpc

import (
	"context"
	"fmt"

	"github.com/seokheejang/chain-sync-watch/internal/chain"
)

// Tip reports the current head block via `eth_blockNumber`. Used by
// application.ChainHead implementations as the "latest known" anchor
// when a verification run needs a recent block without the stricter
// finality guarantees of Finalized().
func (a *Adapter) Tip(ctx context.Context) (chain.BlockNumber, error) {
	var raw string
	if err := a.callRPC(ctx, "eth_blockNumber", &raw); err != nil {
		return 0, fmt.Errorf("rpc: eth_blockNumber: %w", err)
	}
	n, err := parseHexUint64(raw)
	if err != nil {
		return 0, fmt.Errorf("rpc: parse tip: %w", err)
	}
	return chain.BlockNumber(n), nil
}

// Finalized reports the most recent block the node considers final
// (`eth_getBlockByNumber("finalized", false)`). Optimism supports
// the "finalized" tag natively — callers should fall back to Tip()
// only if the node errors with "unknown tag". That fallback is the
// caller's policy rather than this adapter's because different
// chains finalize at very different cadences.
func (a *Adapter) Finalized(ctx context.Context) (chain.BlockNumber, error) {
	var raw *rawBlock
	if err := a.callRPC(ctx, "eth_getBlockByNumber", &raw, "finalized", false); err != nil {
		return 0, fmt.Errorf("rpc: finalized: %w", err)
	}
	if raw == nil {
		return 0, fmt.Errorf("rpc: finalized returned null")
	}
	n, err := parseHexUint64(raw.Number)
	if err != nil {
		return 0, fmt.Errorf("rpc: parse finalized number: %w", err)
	}
	return chain.BlockNumber(n), nil
}
