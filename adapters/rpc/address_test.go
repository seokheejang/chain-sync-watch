package rpc_test

import (
	"context"
	"encoding/json"
	"math/big"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/seokheejang/chain-sync-watch/adapters/rpc"
	"github.com/seokheejang/chain-sync-watch/internal/chain"
	"github.com/seokheejang/chain-sync-watch/internal/source"
)

const (
	// Synthetic address — every byte is 0xaa. We use the lowercase
	// form so EIP-55 treats it as "unchecked" and accepts it without
	// us having to embed a canonical checksum literal.
	fixtureAddr = "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
)

// Latest balance/nonce/tx-count must fill all three pointer fields
// when the node responds successfully. The Anchor is left at its
// zero value (BlockTagLatest) to exercise the default path.
func TestFetchAddressLatest_PopulatesAllFields(t *testing.T) {
	m := newMockRPC(t)
	m.Handle("eth_getBalance", func(params []json.RawMessage) (any, *mockRPCError) {
		// paramString returns what the adapter actually sent; the
		// lowercase fixture round-trips to its EIP-55 form via
		// chain.Address.Hex, so compare against that.
		wantAddr := chain.MustAddress(fixtureAddr).Hex()
		require.Equal(t, wantAddr, paramString(t, params, 0))
		require.Equal(t, "latest", paramString(t, params, 1))
		return "0x1bc16d674ec80000", nil // 2e18 wei (= 2 ETH)
	})
	m.Handle("eth_getTransactionCount", func(params []json.RawMessage) (any, *mockRPCError) {
		require.Equal(t, "latest", paramString(t, params, 1))
		return "0x2a", nil // 42
	})

	a, err := rpc.New(chain.OptimismMainnet, m.URL())
	require.NoError(t, err)

	addr, err := chain.NewAddress(fixtureAddr)
	require.NoError(t, err)

	res, err := a.FetchAddressLatest(context.Background(), source.AddressQuery{Address: addr})
	require.NoError(t, err)

	require.NotNil(t, res.Balance)
	require.Equal(t, 0, res.Balance.Cmp(new(big.Int).Mul(big.NewInt(2), big.NewInt(1_000_000_000_000_000_000))))
	require.NotNil(t, res.Nonce)
	require.Equal(t, uint64(42), *res.Nonce)
	// TxCount equals the nonce on EOAs — we surface both so callers
	// don't have to re-derive one from the other.
	require.NotNil(t, res.TxCount)
	require.Equal(t, uint64(42), *res.TxCount)
	require.Equal(t, rpc.ID, res.SourceID)
	require.NotZero(t, res.FetchedAt)
}

// When the caller supplies an explicit Anchor (finalized / safe /
// numeric), the block tag passed to both eth_getBalance and
// eth_getTransactionCount must mirror it — the whole point of the
// anchor field is that the adapter forwards it verbatim.
func TestFetchAddressLatest_ForwardsAnchor(t *testing.T) {
	m := newMockRPC(t)
	m.Handle("eth_getBalance", func(params []json.RawMessage) (any, *mockRPCError) {
		require.Equal(t, "finalized", paramString(t, params, 1))
		return "0x0", nil
	})
	m.Handle("eth_getTransactionCount", func(params []json.RawMessage) (any, *mockRPCError) {
		require.Equal(t, "finalized", paramString(t, params, 1))
		return "0x0", nil
	})

	a, err := rpc.New(chain.OptimismMainnet, m.URL())
	require.NoError(t, err)

	addr, _ := chain.NewAddress(fixtureAddr)
	_, err = a.FetchAddressLatest(context.Background(), source.AddressQuery{
		Address: addr,
		Anchor:  source.NewBlockTagFinalized(),
	})
	require.NoError(t, err)
}

// AtBlock is gated by the archive option. Without it the adapter
// must refuse the call outright so callers know to route the query
// elsewhere.
func TestFetchAddressAtBlock_NonArchiveReturnsUnsupported(t *testing.T) {
	a, err := rpc.New(chain.OptimismMainnet, "http://unused")
	require.NoError(t, err)
	require.False(t, a.Supports(source.CapBalanceAtBlock))

	_, err = a.FetchAddressAtBlock(context.Background(), source.AddressAtBlockQuery{
		Address: chain.MustAddress(fixtureAddr),
		Block:   chain.NewBlockNumber(1_000_000),
	})
	require.ErrorIs(t, err, source.ErrUnsupported)
}

// With archive enabled the adapter forwards the numeric block as the
// tag on both RPC calls and returns populated fields.
func TestFetchAddressAtBlock_ArchiveForwardsBlock(t *testing.T) {
	m := newMockRPC(t)
	const wantTag = "0xf4240" // 1_000_000
	m.Handle("eth_getBalance", func(params []json.RawMessage) (any, *mockRPCError) {
		require.Equal(t, wantTag, paramString(t, params, 1))
		return "0xde0b6b3a7640000", nil // 1 ETH
	})
	m.Handle("eth_getTransactionCount", func(params []json.RawMessage) (any, *mockRPCError) {
		require.Equal(t, wantTag, paramString(t, params, 1))
		return "0x7", nil
	})

	a, err := rpc.New(chain.OptimismMainnet, m.URL(), rpc.WithArchive(true))
	require.NoError(t, err)
	require.True(t, a.Supports(source.CapBalanceAtBlock))

	res, err := a.FetchAddressAtBlock(context.Background(), source.AddressAtBlockQuery{
		Address: chain.MustAddress(fixtureAddr),
		Block:   chain.NewBlockNumber(1_000_000),
	})
	require.NoError(t, err)

	require.Equal(t, uint64(1_000_000), res.Block.Uint64())
	require.NotNil(t, res.Balance)
	require.Equal(t, 0, res.Balance.Cmp(new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil)))
	require.NotNil(t, res.Nonce)
	require.Equal(t, uint64(7), *res.Nonce)
	// ReflectedBlock echoes the requested block — RPC is the oracle
	// for the tier and must report "I answered at exactly the height
	// you asked".
	require.NotNil(t, res.ReflectedBlock)
	require.Equal(t, uint64(1_000_000), res.ReflectedBlock.Uint64())
}
