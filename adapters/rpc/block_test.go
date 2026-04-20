package rpc_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/seokheejang/chain-sync-watch/adapters/rpc"
	"github.com/seokheejang/chain-sync-watch/internal/chain"
	"github.com/seokheejang/chain-sync-watch/internal/source"
)

// Synthetic block header. The values are deliberately bogus (all 0xAA
// bytes) so the test locks in parsing paths without implying any
// real observation.
func fixtureBlockResponse() map[string]any {
	return map[string]any{
		"number":           "0x150534f", // 22_041_423
		"hash":             "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"parentHash":       "0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		"stateRoot":        "0xcccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
		"transactionsRoot": "0xdddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd",
		"receiptsRoot":     "0xeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee",
		"miner":            "0x4200000000000000000000000000000000000011",
		"gasUsed":          "0xbc614e",   // 12_345_678
		"timestamp":        "0x68054310", // arbitrary unix
		"transactions": []string{
			"0x1111111111111111111111111111111111111111111111111111111111111111",
			"0x2222222222222222222222222222222222222222222222222222222222222222",
			"0x3333333333333333333333333333333333333333333333333333333333333333",
		},
	}
}

func TestFetchBlock_PopulatesAllFields(t *testing.T) {
	m := newMockRPC(t)
	m.Handle("eth_getBlockByNumber", func(params []json.RawMessage) (any, *mockRPCError) {
		// Params: [blockTag (hex), include-full-tx-objects].
		require.Equal(t, "0x150534f", paramString(t, params, 0))
		require.False(t, paramBool(t, params, 1), "fetcher must request tx hashes only (false)")
		return fixtureBlockResponse(), nil
	})

	a, err := rpc.New(chain.OptimismMainnet, m.URL())
	require.NoError(t, err)

	res, err := a.FetchBlock(context.Background(), source.BlockQuery{
		Number: chain.NewBlockNumber(0x150534f),
	})
	require.NoError(t, err)

	require.Equal(t, uint64(0x150534f), res.Number.Uint64())
	require.NotNil(t, res.Hash)
	require.Equal(t, "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", res.Hash.Hex())
	require.NotNil(t, res.ParentHash)
	require.Equal(t, "0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", res.ParentHash.Hex())
	require.NotNil(t, res.StateRoot)
	require.NotNil(t, res.ReceiptsRoot)
	require.NotNil(t, res.TransactionsRoot)
	require.NotNil(t, res.Miner)
	// EIP-55 checksum for the predeploy address.
	require.Equal(t, "0x4200000000000000000000000000000000000011", res.Miner.Hex())
	require.NotNil(t, res.TxCount)
	require.Equal(t, uint64(3), *res.TxCount)
	require.NotNil(t, res.GasUsed)
	require.Equal(t, uint64(12_345_678), *res.GasUsed)
	require.NotNil(t, res.Timestamp)
	require.NotZero(t, res.FetchedAt)
	require.Equal(t, rpc.ID, res.SourceID)
}

// ErrNotFound surfaces when the node returns a JSON "null" result —
// the node understood the request but has no block at that height.
func TestFetchBlock_NullResultYieldsNotFound(t *testing.T) {
	m := newMockRPC(t)
	m.Handle("eth_getBlockByNumber", func(_ []json.RawMessage) (any, *mockRPCError) {
		return nil, nil // result: null
	})

	a, err := rpc.New(chain.OptimismMainnet, m.URL())
	require.NoError(t, err)

	_, err = a.FetchBlock(context.Background(), source.BlockQuery{
		Number: chain.NewBlockNumber(999_999_999),
	})
	require.ErrorIs(t, err, source.ErrNotFound)
}

// Method-not-found from the node must surface as ErrUnsupported so
// the use case can skip the (source, capability) combination rather
// than treat it as a failure.
func TestFetchBlock_MethodNotFoundMapsToUnsupported(t *testing.T) {
	m := newMockRPC(t) // note: no handler registered → server emits -32601

	a, err := rpc.New(chain.OptimismMainnet, m.URL())
	require.NoError(t, err)

	_, err = a.FetchBlock(context.Background(), source.BlockQuery{
		Number: chain.NewBlockNumber(1),
	})
	require.ErrorIs(t, err, source.ErrUnsupported)
}
