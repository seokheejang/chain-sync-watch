package fake_test

import (
	"context"
	"errors"
	"math/big"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/seokheejang/chain-sync-watch/internal/chain"
	"github.com/seokheejang/chain-sync-watch/internal/source"
	"github.com/seokheejang/chain-sync-watch/internal/source/fake"
)

// The fake must behave like a Source at the type level.
var _ source.Source = (*fake.Source)(nil)

func TestFake_ID_And_ChainID(t *testing.T) {
	f := fake.New("rpc", chain.OptimismMainnet)
	require.Equal(t, source.SourceID("rpc"), f.ID())
	require.Equal(t, chain.OptimismMainnet, f.ChainID())
}

func TestFake_DefaultSupportsNothing(t *testing.T) {
	f := fake.New("x", chain.OptimismMainnet)
	for _, c := range source.AllCapabilities() {
		require.Falsef(t, f.Supports(c), "new fake must advertise no capability; found %s", c)
	}
}

func TestFake_WithCapabilities(t *testing.T) {
	f := fake.New("x", chain.OptimismMainnet,
		fake.WithCapabilities(source.CapBlockHash, source.CapBalanceAtLatest),
	)
	require.True(t, f.Supports(source.CapBlockHash))
	require.True(t, f.Supports(source.CapBalanceAtLatest))
	require.False(t, f.Supports(source.CapBlockStateRoot))
}

// ----- Fetch methods default to ErrUnsupported when no response is set -----

func TestFake_FetchBlock_DefaultsToUnsupported(t *testing.T) {
	f := fake.New("x", chain.OptimismMainnet)
	_, err := f.FetchBlock(context.Background(), source.BlockQuery{Number: chain.NewBlockNumber(1)})
	require.ErrorIs(t, err, source.ErrUnsupported)
}

func TestFake_FetchAddressLatest_DefaultsToUnsupported(t *testing.T) {
	f := fake.New("x", chain.OptimismMainnet)
	_, err := f.FetchAddressLatest(context.Background(), source.AddressQuery{})
	require.ErrorIs(t, err, source.ErrUnsupported)
}

func TestFake_FetchAddressAtBlock_DefaultsToUnsupported(t *testing.T) {
	f := fake.New("x", chain.OptimismMainnet)
	_, err := f.FetchAddressAtBlock(context.Background(), source.AddressAtBlockQuery{})
	require.ErrorIs(t, err, source.ErrUnsupported)
}

func TestFake_FetchSnapshot_DefaultsToUnsupported(t *testing.T) {
	f := fake.New("x", chain.OptimismMainnet)
	_, err := f.FetchSnapshot(context.Background(), source.SnapshotQuery{})
	require.ErrorIs(t, err, source.ErrUnsupported)
}

// ----- SetBlockResponse & friends return the configured value --------------

func TestFake_SetBlockResponse(t *testing.T) {
	hash, _ := chain.NewHash32("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	want := source.BlockResult{
		Number:   chain.NewBlockNumber(42),
		Hash:     &hash,
		SourceID: "x",
	}

	f := fake.New("x", chain.OptimismMainnet, fake.WithCapabilities(source.CapBlockHash))
	f.SetBlockResponse(want, nil)

	got, err := f.FetchBlock(context.Background(), source.BlockQuery{Number: chain.NewBlockNumber(42)})
	require.NoError(t, err)
	require.Equal(t, want, got)
}

func TestFake_SetBlockResponse_InjectError(t *testing.T) {
	f := fake.New("x", chain.OptimismMainnet, fake.WithCapabilities(source.CapBlockHash))
	f.SetBlockResponse(source.BlockResult{}, source.ErrRateLimited)

	_, err := f.FetchBlock(context.Background(), source.BlockQuery{})
	require.ErrorIs(t, err, source.ErrRateLimited)
}

// ----- Per-query custom handler overrides any static response --------------

func TestFake_SetBlockHandler_PerQuery(t *testing.T) {
	f := fake.New("x", chain.OptimismMainnet, fake.WithCapabilities(source.CapBlockHash))
	f.SetBlockHandler(func(_ context.Context, q source.BlockQuery) (source.BlockResult, error) {
		if q.Number.Uint64() == 100 {
			return source.BlockResult{}, source.ErrNotFound
		}
		return source.BlockResult{Number: q.Number, SourceID: "x"}, nil
	})

	r, err := f.FetchBlock(context.Background(), source.BlockQuery{Number: chain.NewBlockNumber(42)})
	require.NoError(t, err)
	require.Equal(t, uint64(42), r.Number.Uint64())

	_, err = f.FetchBlock(context.Background(), source.BlockQuery{Number: chain.NewBlockNumber(100)})
	require.ErrorIs(t, err, source.ErrNotFound)
}

// ----- Call recording ------------------------------------------------------

func TestFake_CallsRecorded(t *testing.T) {
	f := fake.New("x", chain.OptimismMainnet, fake.WithCapabilities(source.CapBlockHash))
	f.SetBlockResponse(source.BlockResult{}, nil)

	ctx := context.Background()
	_, _ = f.FetchBlock(ctx, source.BlockQuery{Number: chain.NewBlockNumber(1)})
	_, _ = f.FetchBlock(ctx, source.BlockQuery{Number: chain.NewBlockNumber(2)})
	_, _ = f.FetchAddressLatest(ctx, source.AddressQuery{}) // errs, still recorded

	calls := f.Calls()
	require.Len(t, calls, 3)
	require.Equal(t, "FetchBlock", calls[0].Method)
	require.Equal(t, "FetchBlock", calls[1].Method)
	require.Equal(t, "FetchAddressLatest", calls[2].Method)

	// BlockQuery arg captured by value — mutating the source doesn't
	// affect recorded history.
	q0, ok := calls[0].Args.(source.BlockQuery)
	require.True(t, ok)
	require.Equal(t, uint64(1), q0.Number.Uint64())
}

func TestFake_Reset_ClearsCalls(t *testing.T) {
	f := fake.New("x", chain.OptimismMainnet)
	_, _ = f.FetchBlock(context.Background(), source.BlockQuery{})
	require.Len(t, f.Calls(), 1)

	f.Reset()
	require.Empty(t, f.Calls())
}

// ----- Context cancellation is honoured ------------------------------------

func TestFake_FetchBlock_RespectsCancelledContext(t *testing.T) {
	f := fake.New("x", chain.OptimismMainnet, fake.WithCapabilities(source.CapBlockHash))
	f.SetBlockResponse(source.BlockResult{}, nil)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	_, err := f.FetchBlock(ctx, source.BlockQuery{})
	require.Error(t, err)
	require.True(t, errors.Is(err, context.Canceled),
		"cancelled context must surface through the fake; got %v", err)
}

// ----- Phase 2C additions: 4 new Fetch methods -----------------------------

func TestFake_FetchERC20Balance_DefaultsToUnsupported(t *testing.T) {
	f := fake.New("x", chain.OptimismMainnet)
	_, err := f.FetchERC20Balance(context.Background(), source.ERC20BalanceQuery{})
	require.ErrorIs(t, err, source.ErrUnsupported)
}

func TestFake_FetchERC20Holdings_DefaultsToUnsupported(t *testing.T) {
	f := fake.New("x", chain.OptimismMainnet)
	_, err := f.FetchERC20Holdings(context.Background(), source.ERC20HoldingsQuery{})
	require.ErrorIs(t, err, source.ErrUnsupported)
}

func TestFake_FetchInternalTxByTx_DefaultsToUnsupported(t *testing.T) {
	f := fake.New("x", chain.OptimismMainnet)
	_, err := f.FetchInternalTxByTx(context.Background(), source.InternalTxByTxQuery{})
	require.ErrorIs(t, err, source.ErrUnsupported)
}

func TestFake_FetchInternalTxByBlock_DefaultsToUnsupported(t *testing.T) {
	f := fake.New("x", chain.OptimismMainnet)
	_, err := f.FetchInternalTxByBlock(context.Background(), source.InternalTxByBlockQuery{})
	require.ErrorIs(t, err, source.ErrUnsupported)
}

func TestFake_SetERC20BalanceResponse(t *testing.T) {
	f := fake.New("x", chain.OptimismMainnet, fake.WithCapabilities(source.CapERC20BalanceAtLatest))
	want := source.ERC20BalanceResult{
		Balance:  new(big.Int).SetUint64(2_004_458),
		Decimals: 6,
		Symbol:   "USDC",
		SourceID: "x",
	}
	f.SetERC20BalanceResponse(want, nil)

	got, err := f.FetchERC20Balance(context.Background(), source.ERC20BalanceQuery{})
	require.NoError(t, err)
	require.Equal(t, want, got)
}

func TestFake_SetERC20HoldingsHandler_PerQuery(t *testing.T) {
	f := fake.New("x", chain.OptimismMainnet, fake.WithCapabilities(source.CapERC20HoldingsAtLatest))
	f.SetERC20HoldingsHandler(func(_ context.Context, q source.ERC20HoldingsQuery) (source.ERC20HoldingsResult, error) {
		if q.Anchor.Kind() == source.BlockTagNumeric {
			return source.ERC20HoldingsResult{}, source.ErrUnsupported
		}
		return source.ERC20HoldingsResult{
			Tokens:   []source.TokenHolding{{Symbol: "USDC", Balance: big.NewInt(100)}},
			SourceID: "x",
		}, nil
	})

	// Latest anchor — handler returns tokens.
	r, err := f.FetchERC20Holdings(context.Background(), source.ERC20HoldingsQuery{})
	require.NoError(t, err)
	require.Len(t, r.Tokens, 1)
	require.Equal(t, "USDC", r.Tokens[0].Symbol)

	// Numeric anchor — handler vetoes with ErrUnsupported.
	_, err = f.FetchERC20Holdings(context.Background(), source.ERC20HoldingsQuery{
		Anchor: source.BlockTagAt(chain.NewBlockNumber(100)),
	})
	require.ErrorIs(t, err, source.ErrUnsupported)
}

func TestFake_SetInternalTxByTxResponse_WithReflectedBlock(t *testing.T) {
	refl := chain.NewBlockNumber(150_000_000)
	want := source.InternalTxResult{
		Traces: []source.InternalTx{
			{CallType: "call", GasUsed: 21000, Value: big.NewInt(0)},
		},
		SourceID:       "x",
		ReflectedBlock: &refl,
	}

	f := fake.New("x", chain.OptimismMainnet, fake.WithCapabilities(source.CapInternalTxByTx))
	f.SetInternalTxByTxResponse(want, nil)

	got, err := f.FetchInternalTxByTx(context.Background(), source.InternalTxByTxQuery{})
	require.NoError(t, err)
	require.NotNil(t, got.ReflectedBlock)
	require.Equal(t, uint64(150_000_000), got.ReflectedBlock.Uint64())
	require.Len(t, got.Traces, 1)
}

// Call recording covers the new methods too — the fake's Calls slice
// is the "journal" test bench every Phase 4/5 test relies on.
func TestFake_CallsRecorded_IncludesNewMethods(t *testing.T) {
	f := fake.New("x", chain.OptimismMainnet)
	ctx := context.Background()

	_, _ = f.FetchERC20Balance(ctx, source.ERC20BalanceQuery{})
	_, _ = f.FetchERC20Holdings(ctx, source.ERC20HoldingsQuery{})
	_, _ = f.FetchInternalTxByTx(ctx, source.InternalTxByTxQuery{})
	_, _ = f.FetchInternalTxByBlock(ctx, source.InternalTxByBlockQuery{})

	calls := f.Calls()
	require.Len(t, calls, 4)
	names := []string{calls[0].Method, calls[1].Method, calls[2].Method, calls[3].Method}
	require.Equal(t, []string{
		"FetchERC20Balance",
		"FetchERC20Holdings",
		"FetchInternalTxByTx",
		"FetchInternalTxByBlock",
	}, names)
}
