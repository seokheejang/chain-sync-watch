package rpc

import (
	"context"
	"encoding/hex"
	"fmt"
	"math/big"
	"time"

	"github.com/seokheejang/chain-sync-watch/internal/chain"
	"github.com/seokheejang/chain-sync-watch/internal/source"
)

// ERC-20 balanceOf(address) function selector. Computed as
// keccak256("balanceOf(address)")[:4]; hard-coded here because the
// value is stable across every ERC-20 contract and embedding it
// avoids dragging a keccak helper into the package for one use.
const balanceOfSelector = "70a08231"

// FetchERC20Balance returns the balance of a specific ERC-20 token
// held by an address. We implement this via eth_call against the
// token contract's balanceOf(address) method — supported on every
// full node, works at any block tag (including numeric for
// archive-enabled nodes).
func (a *Adapter) FetchERC20Balance(ctx context.Context, q source.ERC20BalanceQuery) (source.ERC20BalanceResult, error) {
	tag := q.Anchor.String()
	if q.Anchor.Kind() == source.BlockTagNumeric && !a.archive {
		// Historical eth_call against a specific block requires
		// archive state — refuse rather than misrepresent a
		// latest-state answer.
		return source.ERC20BalanceResult{SourceID: ID}, source.ErrUnsupported
	}

	callData, err := encodeBalanceOf(q.Address)
	if err != nil {
		return source.ERC20BalanceResult{SourceID: ID}, err
	}

	// eth_call expects a CallObject {to, data} + a block tag.
	callObj := map[string]string{
		"to":   q.Token.Hex(),
		"data": callData,
	}

	var retHex string
	if err := a.callRPC(ctx, "eth_call", &retHex, callObj, tag); err != nil {
		return source.ERC20BalanceResult{SourceID: ID}, err
	}

	bal, err := decodeUint256(retHex)
	if err != nil {
		return source.ERC20BalanceResult{SourceID: ID}, err
	}

	out := source.ERC20BalanceResult{
		Balance:   bal,
		SourceID:  ID,
		FetchedAt: time.Now().UTC(),
	}
	if q.Anchor.Kind() == source.BlockTagNumeric {
		n := q.Anchor.Number()
		out.ReflectedBlock = &n
	}
	return out, nil
}

// encodeBalanceOf builds the 4+32 byte calldata for
// balanceOf(address). The selector is constant; the address is
// ABI-encoded by left-padding to 32 bytes with zeros.
func encodeBalanceOf(addr chain.Address) (string, error) {
	// 4-byte selector + 32-byte zero-padded address.
	// Total hex length: 8 + 64 = 72 chars, with a "0x" prefix.
	var buf [36]byte
	selBytes, err := hex.DecodeString(balanceOfSelector)
	if err != nil {
		return "", fmt.Errorf("rpc: encode selector: %w", err)
	}
	copy(buf[0:4], selBytes)
	// Address occupies the lowest 20 bytes of the 32-byte slot.
	copy(buf[16:36], addr.Bytes())
	return "0x" + hex.EncodeToString(buf[:]), nil
}

// decodeUint256 parses a 32-byte hex-encoded integer (big-endian) as
// returned by an ERC-20 balanceOf call. The value range fits any
// supported token — including tokens with 18 decimals at trillion-
// unit supplies.
func decodeUint256(s string) (*big.Int, error) {
	b, err := hexBytes(s)
	if err != nil {
		return nil, err
	}
	if len(b) == 0 {
		// Empty return data means the contract didn't exist or the
		// method reverted without data. Treat the result as
		// effectively "no value" so the caller can skip the
		// comparison rather than record a false zero.
		return nil, fmt.Errorf("%w: eth_call returned empty data", source.ErrInvalidResponse)
	}
	return new(big.Int).SetBytes(b), nil
}
