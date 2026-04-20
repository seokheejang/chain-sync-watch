package rpc_test

import (
	"context"
	"encoding/json"
	"math/big"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/seokheejang/chain-sync-watch/adapters/rpc"
	"github.com/seokheejang/chain-sync-watch/internal/chain"
	"github.com/seokheejang/chain-sync-watch/internal/source"
)

// ERC-20 balance resolution goes through eth_call against the token
// contract with balanceOf(address) calldata. The test locks in the
// exact wire format so an accidental change to the selector or
// padding would break an adapter used by every verification run.
func TestFetchERC20Balance_CallDataShape(t *testing.T) {
	const (
		holder = "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
		token  = "0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	)

	m := newMockRPC(t)
	m.Handle("eth_call", func(params []json.RawMessage) (any, *mockRPCError) {
		// Params: [callObject, tag]
		var call struct {
			To   string `json:"to"`
			Data string `json:"data"`
		}
		require.NoError(t, json.Unmarshal(params[0], &call))
		require.Equal(t, chain.MustAddress(token).Hex(), call.To)

		// balanceOf(address) selector is keccak256("balanceOf(address)")[:4]
		// = 0x70a08231. After the selector comes 12 zero bytes + the
		// 20-byte address, ABI-encoded to 32 bytes.
		require.True(t, strings.HasPrefix(call.Data, "0x70a08231"),
			"wrong selector; got %s", call.Data)
		// 4-byte selector + 32-byte padded address = 72 hex chars + "0x"
		require.Equal(t, 2+8+64, len(call.Data))
		// Address should be zero-padded to 32 bytes, big-endian.
		holderHex := strings.TrimPrefix(chain.MustAddress(holder).Hex(), "0x")
		require.True(t, strings.HasSuffix(strings.ToLower(call.Data), strings.ToLower(holderHex)))
		require.Equal(t, "latest", paramString(t, params, 1))

		// Return 32-byte big-endian encoding of 1 * 10^6 (1 USDC).
		return "0x00000000000000000000000000000000000000000000000000000000000f4240", nil
	})

	a, err := rpc.New(chain.OptimismMainnet, m.URL())
	require.NoError(t, err)

	res, err := a.FetchERC20Balance(context.Background(), source.ERC20BalanceQuery{
		Address: chain.MustAddress(holder),
		Token:   chain.MustAddress(token),
	})
	require.NoError(t, err)
	require.NotNil(t, res.Balance)
	require.Equal(t, 0, res.Balance.Cmp(big.NewInt(1_000_000)))
	require.Equal(t, rpc.ID, res.SourceID)
}

// Zero balance decodes correctly too (all-zero 32-byte word). This
// is also the common case for random holder/token pairs, so we want
// an explicit test rather than assuming big.Int.SetString handles it.
func TestFetchERC20Balance_ZeroBalance(t *testing.T) {
	m := newMockRPC(t)
	m.Handle("eth_call", func(_ []json.RawMessage) (any, *mockRPCError) {
		return "0x0000000000000000000000000000000000000000000000000000000000000000", nil
	})
	a, err := rpc.New(chain.OptimismMainnet, m.URL())
	require.NoError(t, err)
	res, err := a.FetchERC20Balance(context.Background(), source.ERC20BalanceQuery{
		Address: chain.MustAddress("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"),
		Token:   chain.MustAddress("0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"),
	})
	require.NoError(t, err)
	require.NotNil(t, res.Balance)
	require.Equal(t, 0, res.Balance.Cmp(big.NewInt(0)))
}

// FetchERC20Holdings is unconditionally unsupported on the RPC
// adapter — reconstructing the list would require a full log scan.
// The test is a guard against accidental removal of the short-circuit.
func TestFetchERC20Holdings_AlwaysUnsupported(t *testing.T) {
	a, err := rpc.New(chain.OptimismMainnet, "http://unused")
	require.NoError(t, err)
	require.False(t, a.Supports(source.CapERC20HoldingsAtLatest))

	_, err = a.FetchERC20Holdings(context.Background(), source.ERC20HoldingsQuery{
		Address: chain.MustAddress("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"),
	})
	require.ErrorIs(t, err, source.ErrUnsupported)
}
