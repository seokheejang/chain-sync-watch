package source_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/seokheejang/chain-sync-watch/internal/source"
)

// Tier classifies how verification consumes a Capability.
//
//	A — RPC-canonical (authoritative, exhaustive).
//	B — Indexer-derived (cross-indexer sampling only).
//	C — Mixed (either path; per-metric policy).
//
// The labels are one character long on purpose so they are readable as
// table columns and metric labels.
func TestTier_StringLabel(t *testing.T) {
	cases := []struct {
		t    source.Tier
		want string
	}{
		{source.TierA, "A"},
		{source.TierB, "B"},
		{source.TierC, "C"},
	}
	for _, tc := range cases {
		require.Equal(t, tc.want, tc.t.String())
	}
	// Zero value must be distinguishable from a real tier so a mis-
	// categorised capability surfaces immediately in logs.
	require.Equal(t, "unknown", source.TierUnknown.String())
}

// Every Capability declared by the package must map to a known Tier.
// An unmapped capability is a bug — either the Capability was added
// without updating the Tier table, or the Tier taxonomy needs a new
// entry. We fail loudly here instead of returning TierUnknown silently.
func TestCapability_TierCoverage(t *testing.T) {
	for _, c := range source.AllCapabilities() {
		tier := c.Tier()
		require.NotEqualf(t, source.TierUnknown, tier,
			"capability %q has no Tier assignment", c)
	}
}

// Spot-check the taxonomy. These assertions match the 3-tier table in
// docs/research/external-api-coverage.md — updating the mapping there
// without updating this test (or vice-versa) breaks the build.
func TestCapability_TierAssignments(t *testing.T) {
	tierA := []source.Capability{
		// Block immutables are RPC-canonical by construction.
		source.CapBlockHash,
		source.CapBlockParentHash,
		source.CapBlockTimestamp,
		source.CapBlockTxCount,
		source.CapBlockGasUsed,
		source.CapBlockStateRoot,
		source.CapBlockReceiptsRoot,
		source.CapBlockTransactionsRoot,
		source.CapBlockMiner,
		// Address state at a specific block needs archive RPC — that
		// source is authoritative.
		source.CapBalanceAtLatest,
		source.CapNonceAtLatest,
		source.CapTxCountAtLatest,
		source.CapBalanceAtBlock,
		source.CapNonceAtBlock,
	}
	for _, c := range tierA {
		require.Equalf(t, source.TierA, c.Tier(), "%s should be TierA", c)
	}

	tierB := []source.Capability{
		// Chain-wide cumulative counts come from indexers; no RPC
		// reconstruction path cheap enough to treat as ground truth.
		source.CapTotalAddressCount,
		source.CapTotalTxCount,
		source.CapTotalContractCount,
		source.CapERC20TokenCount,
		// Full holdings list: RPC would require a Transfer-log scan;
		// indexers are canonical.
		source.CapERC20HoldingsAtLatest,
	}
	for _, c := range tierB {
		require.Equalf(t, source.TierB, c.Tier(), "%s should be TierB", c)
	}

	tierC := []source.Capability{
		// Specific token balance: eth_call on one side, indexer cache
		// on the other — operators pick per-metric.
		source.CapERC20BalanceAtLatest,
		// Trace data: debug_* RPC vs decoded indexer cache — same
		// tradeoff.
		source.CapInternalTxByTx,
		source.CapInternalTxByBlock,
	}
	for _, c := range tierC {
		require.Equalf(t, source.TierC, c.Tier(), "%s should be TierC", c)
	}
}
