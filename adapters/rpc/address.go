package rpc

import (
	"context"
	"math/big"
	"time"

	"github.com/seokheejang/chain-sync-watch/internal/chain"
	"github.com/seokheejang/chain-sync-watch/internal/source"
)

// FetchAddressLatest returns balance / nonce / tx_count at the
// request's Anchor (zero value → "latest"). The nonce and tx_count
// are the same on EOAs (eth_getTransactionCount returns the send
// count, which equals the nonce for non-contract accounts); we fill
// both so callers don't have to re-derive one from the other.
func (a *Adapter) FetchAddressLatest(ctx context.Context, q source.AddressQuery) (source.AddressLatestResult, error) {
	tag := q.Anchor.String()

	bal, err := a.getBalance(ctx, q.Address, tag)
	if err != nil {
		return source.AddressLatestResult{SourceID: ID}, err
	}
	nonce, err := a.getTransactionCount(ctx, q.Address, tag)
	if err != nil {
		return source.AddressLatestResult{SourceID: ID}, err
	}

	out := source.AddressLatestResult{
		Balance:   bal,
		Nonce:     &nonce,
		TxCount:   &nonce,
		SourceID:  ID,
		FetchedAt: time.Now().UTC(),
	}
	// If the caller asked for a numeric anchor via AddressQuery, echo
	// it back as the reflected block so the comparison layer knows
	// exactly what the response pinned to.
	if q.Anchor.Kind() == source.BlockTagNumeric {
		n := q.Anchor.Number()
		out.ReflectedBlock = &n
	}
	return out, nil
}

// FetchAddressAtBlock returns historical account state. Requires
// WithArchive(true) — non-archive nodes return "missing trie node"
// for old heights and we refuse to paper over that by silently
// substituting latest.
func (a *Adapter) FetchAddressAtBlock(ctx context.Context, q source.AddressAtBlockQuery) (source.AddressAtBlockResult, error) {
	if !a.archive {
		return source.AddressAtBlockResult{SourceID: ID}, source.ErrUnsupported
	}
	tag := q.Block.Hex()

	bal, err := a.getBalance(ctx, q.Address, tag)
	if err != nil {
		return source.AddressAtBlockResult{SourceID: ID}, err
	}
	nonce, err := a.getTransactionCount(ctx, q.Address, tag)
	if err != nil {
		return source.AddressAtBlockResult{SourceID: ID}, err
	}

	refl := q.Block
	return source.AddressAtBlockResult{
		Balance:        bal,
		Nonce:          &nonce,
		Block:          q.Block,
		SourceID:       ID,
		FetchedAt:      time.Now().UTC(),
		ReflectedBlock: &refl,
	}, nil
}

// getBalance is the shared implementation for eth_getBalance.
func (a *Adapter) getBalance(ctx context.Context, addr chain.Address, tag string) (*big.Int, error) {
	var hex string
	if err := a.callRPC(ctx, "eth_getBalance", &hex, addr.Hex(), tag); err != nil {
		return nil, err
	}
	v, err := parseHexBigInt(hex)
	if err != nil {
		return nil, err
	}
	return v, nil
}

// getTransactionCount is the shared implementation for
// eth_getTransactionCount (nonce / send-count).
func (a *Adapter) getTransactionCount(ctx context.Context, addr chain.Address, tag string) (uint64, error) {
	var hex string
	if err := a.callRPC(ctx, "eth_getTransactionCount", &hex, addr.Hex(), tag); err != nil {
		return 0, err
	}
	return parseHexUint64(hex)
}
