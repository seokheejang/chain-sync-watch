package source

// Capability names a single field a Source may be able to serve. The
// string form is stable and used in logs, metrics labels, and the
// user-facing Capability matrix. We keep the set flat (no groups) so
// callers never need to understand a hierarchy to check support.
type Capability string

const (
	// --- Block-immutable fields (anchored by block number) ---
	CapBlockHash             Capability = "block.hash"
	CapBlockParentHash       Capability = "block.parent_hash"
	CapBlockTimestamp        Capability = "block.timestamp"
	CapBlockTxCount          Capability = "block.tx_count"
	CapBlockGasUsed          Capability = "block.gas_used"
	CapBlockStateRoot        Capability = "block.state_root"
	CapBlockReceiptsRoot     Capability = "block.receipts_root"
	CapBlockTransactionsRoot Capability = "block.transactions_root"
	CapBlockMiner            Capability = "block.miner"

	// --- Address snapshot at the current latest block ---
	CapBalanceAtLatest Capability = "address.balance_at_latest"
	CapNonceAtLatest   Capability = "address.nonce_at_latest"
	CapTxCountAtLatest Capability = "address.tx_count_at_latest"

	// --- Address snapshot at a specific historical block ---
	// Requires archive-node support on RPC-backed adapters.
	CapBalanceAtBlock Capability = "address.balance_at_block"
	CapNonceAtBlock   Capability = "address.nonce_at_block"

	// --- Chain-wide cumulative snapshots (not anchored to a block) ---
	// Semantically fuzzy across sources; judgments over these should be
	// observational only.
	CapTotalAddressCount  Capability = "snapshot.total_addresses"
	CapTotalTxCount       Capability = "snapshot.total_txs"
	CapERC20TokenCount    Capability = "snapshot.erc20_token_count"
	CapTotalContractCount Capability = "snapshot.total_contracts"

	// --- Per-address ERC-20 (Phase 2C) ---
	// Balance of a specific ERC-20 token held by an address. RPC can
	// serve this via eth_call to balanceOf(); 3rd-party indexers
	// cache it. Tier C — mixed.
	CapERC20BalanceAtLatest Capability = "address.erc20_balance_at_latest"
	// The full list of tokens held by an address. Reconstructing this
	// from RPC requires scanning Transfer logs (expensive); indexers
	// serve it directly. Tier B — indexer-derived.
	CapERC20HoldingsAtLatest Capability = "address.erc20_holdings_at_latest"

	// --- Internal transactions / traces (Phase 2C) ---
	// Internal calls within a single transaction or all internal calls
	// recorded in a block. Archive RPC can replay traces via
	// debug_traceTransaction / debug_traceBlockByNumber; indexers
	// cache decoded traces. Tier C — mixed.
	CapInternalTxByTx    Capability = "trace.internal_tx_by_tx"
	CapInternalTxByBlock Capability = "trace.internal_tx_by_block"
)

// AllCapabilities returns every capability the core exposes. Callers
// iterate this to build coverage matrices and to drive exhaustive
// tests. Order is stable (by declaration) so test output stays
// diff-friendly.
func AllCapabilities() []Capability {
	return []Capability{
		CapBlockHash,
		CapBlockParentHash,
		CapBlockTimestamp,
		CapBlockTxCount,
		CapBlockGasUsed,
		CapBlockStateRoot,
		CapBlockReceiptsRoot,
		CapBlockTransactionsRoot,
		CapBlockMiner,
		CapBalanceAtLatest,
		CapNonceAtLatest,
		CapTxCountAtLatest,
		CapBalanceAtBlock,
		CapNonceAtBlock,
		CapTotalAddressCount,
		CapTotalTxCount,
		CapERC20TokenCount,
		CapTotalContractCount,
		CapERC20BalanceAtLatest,
		CapERC20HoldingsAtLatest,
		CapInternalTxByTx,
		CapInternalTxByBlock,
	}
}
