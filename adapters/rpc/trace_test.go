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

// A fixture callTracer output with one nested sub-call. The
// structure mirrors geth's callTracer JSON exactly — we rely on
// that format being stable across geth / op-geth / erigon, which it
// has been since the tracer was upstreamed.
func callTracerFixture() map[string]any {
	return map[string]any{
		"type":    "CALL",
		"from":    "0x4200000000000000000000000000000000000010",
		"to":      "0x4200000000000000000000000000000000000006",
		"value":   "0x0",
		"gas":     "0x5208",
		"gasUsed": "0x5208",
		"input":   "0x",
		"output":  "0x",
		"calls": []any{
			map[string]any{
				"type":    "STATICCALL",
				"from":    "0x4200000000000000000000000000000000000006",
				"to":      "0x4200000000000000000000000000000000000011",
				"gas":     "0x1000",
				"gasUsed": "0x100",
				"input":   "0x",
				"output":  "0x",
			},
		},
	}
}

// Trace methods are gated by WithDebugTrace — without it the adapter
// must refuse rather than try the call and surface a "method not
// found" error from the node.
func TestFetchInternalTxByTx_WithoutDebugUnsupported(t *testing.T) {
	a, err := rpc.New(chain.OptimismMainnet, "http://unused")
	require.NoError(t, err)
	require.False(t, a.Supports(source.CapInternalTxByTx))

	_, err = a.FetchInternalTxByTx(context.Background(), source.InternalTxByTxQuery{
		TxHash: chain.Hash32{},
	})
	require.ErrorIs(t, err, source.ErrUnsupported)
}

// With debug enabled, FetchInternalTxByTx should flatten the trace
// tree into a linear slice (parent first, then children in DFS
// order), decoding types and hex values along the way.
func TestFetchInternalTxByTx_FlattensCallTree(t *testing.T) {
	m := newMockRPC(t)
	m.Handle("debug_traceTransaction", func(params []json.RawMessage) (any, *mockRPCError) {
		// Second param must carry the callTracer selector — the
		// tracer name change would silently break parsing, so the
		// test pins it explicitly.
		var tracerCfg map[string]string
		require.NoError(t, json.Unmarshal(params[1], &tracerCfg))
		require.Equal(t, "callTracer", tracerCfg["tracer"])
		return callTracerFixture(), nil
	})

	a, err := rpc.New(chain.OptimismMainnet, m.URL(), rpc.WithDebugTrace(true))
	require.NoError(t, err)

	txh, _ := chain.NewHash32("0xab" + repeat("0", 62))
	res, err := a.FetchInternalTxByTx(context.Background(), source.InternalTxByTxQuery{TxHash: txh})
	require.NoError(t, err)

	// Expect 2 traces: the root CALL and the nested STATICCALL.
	require.Len(t, res.Traces, 2)
	require.Equal(t, "CALL", res.Traces[0].CallType)
	require.Equal(t, "STATICCALL", res.Traces[1].CallType)
	require.Equal(t, uint64(0x5208), res.Traces[0].GasUsed)
	require.Equal(t, uint64(0x100), res.Traces[1].GasUsed)
}

func repeat(s string, n int) string {
	out := make([]byte, 0, len(s)*n)
	for range n {
		out = append(out, s...)
	}
	return string(out)
}

// by-block returns a list of traces (one per transaction in the
// block). Each entry has a .result field in geth's response shape.
func TestFetchInternalTxByBlock_FlattensAllTransactions(t *testing.T) {
	m := newMockRPC(t)
	m.Handle("debug_traceBlockByNumber", func(params []json.RawMessage) (any, *mockRPCError) {
		require.Equal(t, "0x64", paramString(t, params, 0))
		return []any{
			map[string]any{"result": callTracerFixture()},
			map[string]any{"result": map[string]any{
				"type":    "CREATE",
				"from":    "0x4200000000000000000000000000000000000006",
				"to":      "0x4200000000000000000000000000000000000007",
				"value":   "0x0",
				"gas":     "0x1",
				"gasUsed": "0x1",
				"input":   "0x",
				"output":  "0x",
			}},
		}, nil
	})

	a, err := rpc.New(chain.OptimismMainnet, m.URL(), rpc.WithDebugTrace(true))
	require.NoError(t, err)

	res, err := a.FetchInternalTxByBlock(context.Background(), source.InternalTxByBlockQuery{
		Block: chain.NewBlockNumber(100),
	})
	require.NoError(t, err)
	// Two root calls (one with a nested STATICCALL) = 3 flattened
	// entries total.
	require.Len(t, res.Traces, 3)
	require.Equal(t, "CALL", res.Traces[0].CallType)
	require.Equal(t, "STATICCALL", res.Traces[1].CallType)
	require.Equal(t, "CREATE", res.Traces[2].CallType)
	require.NotNil(t, res.ReflectedBlock)
	require.Equal(t, uint64(100), res.ReflectedBlock.Uint64())
}
