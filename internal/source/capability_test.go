package source_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/seokheejang/chain-sync-watch/internal/source"
)

func TestSourceID_StringConversion(t *testing.T) {
	// SourceID is a named string so callers can get the raw form without
	// risking accidental mix-ups with arbitrary strings.
	id := source.SourceID("rpc")
	require.Equal(t, "rpc", string(id))
	require.Equal(t, "", string(source.SourceID("")))
}

func TestCapability_StringValues(t *testing.T) {
	// Values are stable, namespaced constants — adapters and tests
	// compare against them by string identity.
	cases := []struct {
		cap  source.Capability
		want string
	}{
		// Block-immutable fields
		{source.CapBlockHash, "block.hash"},
		{source.CapBlockParentHash, "block.parent_hash"},
		{source.CapBlockTimestamp, "block.timestamp"},
		{source.CapBlockTxCount, "block.tx_count"},
		{source.CapBlockGasUsed, "block.gas_used"},
		{source.CapBlockStateRoot, "block.state_root"},
		{source.CapBlockReceiptsRoot, "block.receipts_root"},
		{source.CapBlockTransactionsRoot, "block.transactions_root"},
		{source.CapBlockMiner, "block.miner"},

		// Address-at-latest
		{source.CapBalanceAtLatest, "address.balance_at_latest"},
		{source.CapNonceAtLatest, "address.nonce_at_latest"},
		{source.CapTxCountAtLatest, "address.tx_count_at_latest"},

		// Address-at-block (archive-node territory)
		{source.CapBalanceAtBlock, "address.balance_at_block"},
		{source.CapNonceAtBlock, "address.nonce_at_block"},

		// Snapshot / cumulative
		{source.CapTotalAddressCount, "snapshot.total_addresses"},
		{source.CapTotalTxCount, "snapshot.total_txs"},
		{source.CapERC20TokenCount, "snapshot.erc20_token_count"},
		{source.CapTotalContractCount, "snapshot.total_contracts"},

		// Per-address ERC-20 (Phase 2C)
		{source.CapERC20BalanceAtLatest, "address.erc20_balance_at_latest"},
		{source.CapERC20HoldingsAtLatest, "address.erc20_holdings_at_latest"},

		// Internal-tx traces (Phase 2C)
		{source.CapInternalTxByTx, "trace.internal_tx_by_tx"},
		{source.CapInternalTxByBlock, "trace.internal_tx_by_block"},
	}
	for _, tc := range cases {
		t.Run(tc.want, func(t *testing.T) {
			require.Equal(t, tc.want, string(tc.cap))
		})
	}
}

// AllCapabilities is the full set exposed by the package so that
// adapters can declare coverage and tests can iterate deterministically.
func TestCapability_AllKnown(t *testing.T) {
	all := source.AllCapabilities()
	// Must have no duplicates and match the count of the exported
	// constants. This test is brittle by design — adding a capability
	// without updating AllCapabilities is an easy miss we want to catch.
	seen := make(map[source.Capability]bool, len(all))
	for _, c := range all {
		require.False(t, seen[c], "duplicate capability: %s", c)
		seen[c] = true
	}
	require.Len(t, all, 22, "update this count when adding capabilities")
}
