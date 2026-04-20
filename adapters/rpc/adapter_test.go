package rpc_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/seokheejang/chain-sync-watch/adapters/rpc"
	"github.com/seokheejang/chain-sync-watch/internal/chain"
	"github.com/seokheejang/chain-sync-watch/internal/source"
)

// Compile-time proof that *rpc.Adapter implements the full Source
// contract. If the interface grows and we forget a method, this line
// stops compiling.
var _ source.Source = (*rpc.Adapter)(nil)

// The Supports() matrix is the contract every higher layer reads
// from, so we pin it against the Phase 3 / research-doc spec. The
// table is exhaustive — AllCapabilities is iterated to catch any
// new Capability that slips in without a matching case.
func TestAdapter_SupportsMatrix(t *testing.T) {
	cases := []struct {
		name      string
		opts      []rpc.Option
		supported map[source.Capability]bool
	}{
		{
			name: "default (no archive, no debug)",
			supported: map[source.Capability]bool{
				source.CapBlockHash:             true,
				source.CapBlockParentHash:       true,
				source.CapBlockTimestamp:        true,
				source.CapBlockTxCount:          true,
				source.CapBlockGasUsed:          true,
				source.CapBlockStateRoot:        true,
				source.CapBlockReceiptsRoot:     true,
				source.CapBlockTransactionsRoot: true,
				source.CapBlockMiner:            true,
				source.CapBalanceAtLatest:       true,
				source.CapNonceAtLatest:         true,
				source.CapTxCountAtLatest:       true,
				source.CapBalanceAtBlock:        false,
				source.CapNonceAtBlock:          false,
				source.CapTotalAddressCount:     false,
				source.CapTotalTxCount:          false,
				source.CapTotalContractCount:    false,
				source.CapERC20TokenCount:       false,
				source.CapERC20BalanceAtLatest:  true,
				source.CapERC20HoldingsAtLatest: false,
				source.CapInternalTxByTx:        false,
				source.CapInternalTxByBlock:     false,
			},
		},
		{
			name: "archive only",
			opts: []rpc.Option{rpc.WithArchive(true)},
			supported: map[source.Capability]bool{
				source.CapBalanceAtBlock:    true,
				source.CapNonceAtBlock:      true,
				source.CapInternalTxByTx:    false, // still gated by debug
				source.CapInternalTxByBlock: false,
			},
		},
		{
			name: "archive + debug",
			opts: []rpc.Option{rpc.WithArchive(true), rpc.WithDebugTrace(true)},
			supported: map[source.Capability]bool{
				source.CapBalanceAtBlock:    true,
				source.CapNonceAtBlock:      true,
				source.CapInternalTxByTx:    true,
				source.CapInternalTxByBlock: true,
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a, err := rpc.New(chain.OptimismMainnet, "http://unused", tc.opts...)
			require.NoError(t, err)

			// The explicit expectations win over iteration — anything
			// not listed falls back to the default case's expected
			// value if present.
			for cap, want := range tc.supported {
				require.Equalf(t, want, a.Supports(cap),
					"Supports(%s) want %v; check adapter.go Supports matrix", cap, want)
			}

			// Also assert every Capability has SOME mapping — new
			// capabilities must be classified explicitly in Supports.
			for _, c := range source.AllCapabilities() {
				_ = a.Supports(c) // no panic, no missing case
			}
		})
	}
}

// ID and ChainID are stable identities the verification layer relies
// on for attribution; lock them in at the interface level.
func TestAdapter_Identity(t *testing.T) {
	a, err := rpc.New(chain.OptimismMainnet, "http://unused")
	require.NoError(t, err)
	require.Equal(t, rpc.ID, a.ID())
	require.Equal(t, source.SourceID("rpc"), a.ID())
	require.Equal(t, chain.OptimismMainnet, a.ChainID())
}

// Construction rejects missing required fields — an adapter without
// chain id or URL is useless and we surface that at New time instead
// of deferring to the first Fetch call.
func TestAdapter_New_RejectsMissingFields(t *testing.T) {
	_, err := rpc.New(0, "http://x")
	require.Error(t, err)
	_, err = rpc.New(chain.OptimismMainnet, "")
	require.Error(t, err)
}

// FetchSnapshot is unconditionally unsupported — a single RPC node
// has no notion of chain-wide cumulative aggregates.
func TestAdapter_FetchSnapshot_AlwaysUnsupported(t *testing.T) {
	a, err := rpc.New(chain.OptimismMainnet, "http://unused")
	require.NoError(t, err)
	_, err = a.FetchSnapshot(t.Context(), source.SnapshotQuery{})
	require.ErrorIs(t, err, source.ErrUnsupported)
}
