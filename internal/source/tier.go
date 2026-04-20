package source

// Tier classifies a Capability by how verification can consume it:
//
//	TierA — RPC-canonical. Ground truth lives on an archive node; every
//	        finalized block can be verified exhaustively at zero API
//	        cost. Adapters backed by a 3rd-party indexer still carry
//	        TierA capabilities, but they are cross-checked against the
//	        RPC adapter, which acts as the oracle.
//
//	TierB — Indexer-derived. No single source of truth — only cross-
//	        source comparison between independent indexers. Subject to
//	        per-source rate-limit budgets and therefore sampled rather
//	        than verified exhaustively. Snapshot-style aggregates
//	        (total_addresses, total_txs) live here.
//
//	TierC — Mixed. RPC can reconstruct the value but at meaningful
//	        compute cost (log scans, archive balanceOf loops, trace
//	        replays); 3rd-party indexers serve cached copies. Operators
//	        pick the path per metric.
//
// See docs/research/external-api-coverage.md for the tier rationale
// and per-capability matrix.
type Tier uint8

const (
	// TierUnknown is the zero value and MUST NOT appear in production.
	// A Tier returning Unknown means a Capability was added without
	// being classified — see TestCapability_TierCoverage.
	TierUnknown Tier = iota
	TierA
	TierB
	TierC
)

// String returns the single-letter label used in logs, metric labels,
// and user-facing matrices. The Unknown case renders as "unknown" so a
// missing classification never silently masquerades as a real tier.
func (t Tier) String() string {
	switch t {
	case TierA:
		return "A"
	case TierB:
		return "B"
	case TierC:
		return "C"
	default:
		return "unknown"
	}
}

// Tier returns the verification tier assigned to this Capability. The
// assignment is a property of the Capability itself (not of a specific
// adapter) — every adapter that serves CapBlockHash participates in
// TierA verification for that field.
//
// Adding a new Capability requires adding it here too; the unit test
// TestCapability_TierCoverage iterates AllCapabilities() and fails on
// any TierUnknown result.
func (c Capability) Tier() Tier {
	switch c {
	// Block immutables — RPC oracle.
	case CapBlockHash,
		CapBlockParentHash,
		CapBlockTimestamp,
		CapBlockTxCount,
		CapBlockGasUsed,
		CapBlockStateRoot,
		CapBlockReceiptsRoot,
		CapBlockTransactionsRoot,
		CapBlockMiner:
		return TierA

	// Address state, latest and historical — RPC oracle (archive node
	// for the "at block" variants).
	case CapBalanceAtLatest,
		CapNonceAtLatest,
		CapTxCountAtLatest,
		CapBalanceAtBlock,
		CapNonceAtBlock:
		return TierA

	// Chain-wide cumulative counters — indexer territory.
	case CapTotalAddressCount,
		CapTotalTxCount,
		CapTotalContractCount,
		CapERC20TokenCount:
		return TierB

	// Full ERC-20 holdings list per address — indexer-cached; RPC
	// reconstruction requires log scans too expensive to run per
	// verification.
	case CapERC20HoldingsAtLatest:
		return TierB

	// Specific ERC-20 balance: RPC can balanceOf() cheaply, indexers
	// serve it faster. Per-metric policy decides the path.
	case CapERC20BalanceAtLatest:
		return TierC

	// Internal-transaction data: archive RPC can debug_trace; indexers
	// serve decoded copies. Tier C — mixed.
	case CapInternalTxByTx,
		CapInternalTxByBlock:
		return TierC
	}
	return TierUnknown
}
