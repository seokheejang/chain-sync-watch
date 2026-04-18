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
	}
}
