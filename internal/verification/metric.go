package verification

import "github.com/seokheejang/chain-sync-watch/internal/source"

// MetricCategory classifies a Metric by how its values should be
// compared across Sources. The category is the primary input to the
// downstream JudgementPolicy: "what does disagreement mean?" depends
// more on the category than on the specific field.
//
// The four categories reflect hard distinctions we observed while
// mapping source shapes:
//
//   - BlockImmutable — block-anchored, on-chain immutable. Any
//     disagreement is a bug (or a reorg we failed to account for).
//   - AddressLatest  — account state at "latest". Disagreement is
//     normal within a short anchor window because Sources race each
//     other by one or two blocks; judgement uses AnchorWindowed.
//   - AddressAtBlock — historical state anchored to a specific block.
//     Archive-RPC-canonical; disagreement is Critical.
//   - Snapshot       — chain-wide cumulatives (total addresses, total
//     txs). Semantics differ across Sources (indexer definitions,
//     spam filters, pending inclusion), so judgement is observational
//     by default.
type MetricCategory string

const (
	// CatBlockImmutable — on-chain immutable block fields. ExactMatch,
	// any disagreement escalates to Critical.
	CatBlockImmutable MetricCategory = "block_immutable"

	// CatAddressLatest — address state at latest. AnchorWindowed
	// tolerance around the verification anchor; out-of-window samples
	// are discarded, not judged.
	CatAddressLatest MetricCategory = "address_latest"

	// CatAddressAtBlock — historical address state (archive RPC).
	// ExactMatch, RPC is trusted when present.
	CatAddressAtBlock MetricCategory = "address_at_block"

	// CatSnapshot — chain-wide cumulatives. Observational by default;
	// a Source that exposes reflected-block meta may opt into
	// AnchorWindowed cross-checks via its Capability entry.
	CatSnapshot MetricCategory = "snapshot"
)

// AllCategories returns every MetricCategory in declaration order.
// Tests that need exhaustive coverage iterate this list; adding a
// category without appending it here (or vice versa) fails
// TestMetricCategory_AllCategories.
func AllCategories() []MetricCategory {
	return []MetricCategory{
		CatBlockImmutable,
		CatAddressLatest,
		CatAddressAtBlock,
		CatSnapshot,
	}
}

// Metric names a single comparable field. Key is a stable identifier
// used in logs, metric labels, and persisted discrepancy rows;
// Category drives the judgement policy; Capability is the source-side
// contract — only Sources whose Supports(Capability) returns true can
// supply a value for this Metric.
//
// Metric is a plain value type by design: users may register their own
// metrics simply by constructing Metric literals that reference an
// existing source.Capability. No registry, no locking, no init-order
// coupling.
type Metric struct {
	Key        string
	Category   MetricCategory
	Capability source.Capability
}

// Built-in metric catalog. These cover every Capability declared in
// internal/source; adapter authors add new Capabilities in tandem with
// the corresponding Metric here so the verification layer has a default
// name/category mapping.
var (
	// --- Block-immutable fields -----------------------------------------

	MetricBlockHash             = Metric{Key: "block.hash", Category: CatBlockImmutable, Capability: source.CapBlockHash}
	MetricBlockParentHash       = Metric{Key: "block.parent_hash", Category: CatBlockImmutable, Capability: source.CapBlockParentHash}
	MetricBlockTimestamp        = Metric{Key: "block.timestamp", Category: CatBlockImmutable, Capability: source.CapBlockTimestamp}
	MetricBlockTxCount          = Metric{Key: "block.tx_count", Category: CatBlockImmutable, Capability: source.CapBlockTxCount}
	MetricBlockGasUsed          = Metric{Key: "block.gas_used", Category: CatBlockImmutable, Capability: source.CapBlockGasUsed}
	MetricBlockStateRoot        = Metric{Key: "block.state_root", Category: CatBlockImmutable, Capability: source.CapBlockStateRoot}
	MetricBlockReceiptsRoot     = Metric{Key: "block.receipts_root", Category: CatBlockImmutable, Capability: source.CapBlockReceiptsRoot}
	MetricBlockTransactionsRoot = Metric{Key: "block.transactions_root", Category: CatBlockImmutable, Capability: source.CapBlockTransactionsRoot}
	MetricBlockMiner            = Metric{Key: "block.miner", Category: CatBlockImmutable, Capability: source.CapBlockMiner}

	// --- Address state at latest ----------------------------------------

	MetricBalanceLatest = Metric{Key: "address.balance_latest", Category: CatAddressLatest, Capability: source.CapBalanceAtLatest}
	MetricNonceLatest   = Metric{Key: "address.nonce_latest", Category: CatAddressLatest, Capability: source.CapNonceAtLatest}
	MetricTxCountLatest = Metric{Key: "address.tx_count_latest", Category: CatAddressLatest, Capability: source.CapTxCountAtLatest}

	// --- Address state at a specific block ------------------------------

	MetricBalanceAtBlock = Metric{Key: "address.balance_at_block", Category: CatAddressAtBlock, Capability: source.CapBalanceAtBlock}
	MetricNonceAtBlock   = Metric{Key: "address.nonce_at_block", Category: CatAddressAtBlock, Capability: source.CapNonceAtBlock}

	// --- Chain-wide cumulatives -----------------------------------------

	MetricTotalAddressCount  = Metric{Key: "snapshot.total_addresses", Category: CatSnapshot, Capability: source.CapTotalAddressCount}
	MetricTotalTxCount       = Metric{Key: "snapshot.total_txs", Category: CatSnapshot, Capability: source.CapTotalTxCount}
	MetricTotalContractCount = Metric{Key: "snapshot.total_contracts", Category: CatSnapshot, Capability: source.CapTotalContractCount}
	// The "token_count" substring trips gosec G101 heuristics; the
	// value is a stable metric key, not a credential.
	MetricERC20TokenCount = Metric{Key: "snapshot.erc20_token_count", Category: CatSnapshot, Capability: source.CapERC20TokenCount} //nolint:gosec // G101: metric key, not credential

	// --- Per-address ERC-20 (Tier B/C) ----------------------------------

	MetricERC20BalanceLatest  = Metric{Key: "address.erc20_balance_latest", Category: CatAddressLatest, Capability: source.CapERC20BalanceAtLatest}
	MetricERC20HoldingsLatest = Metric{Key: "address.erc20_holdings_latest", Category: CatAddressLatest, Capability: source.CapERC20HoldingsAtLatest}

	// --- Internal transactions (Tier C) ---------------------------------
	// Classified under BlockImmutable because traces are anchored to
	// (block, tx) pairs and do not drift once finalized.

	MetricInternalTxByTx    = Metric{Key: "trace.internal_tx_by_tx", Category: CatBlockImmutable, Capability: source.CapInternalTxByTx}
	MetricInternalTxByBlock = Metric{Key: "trace.internal_tx_by_block", Category: CatBlockImmutable, Capability: source.CapInternalTxByBlock}
)

// AllMetrics returns every built-in Metric in stable declaration
// order. The catalog is the reference mapping between the source-layer
// Capability set and the verification-layer naming; it is not an enum
// — user code may register its own Metric values against any
// Capability.
func AllMetrics() []Metric {
	return []Metric{
		MetricBlockHash,
		MetricBlockParentHash,
		MetricBlockTimestamp,
		MetricBlockTxCount,
		MetricBlockGasUsed,
		MetricBlockStateRoot,
		MetricBlockReceiptsRoot,
		MetricBlockTransactionsRoot,
		MetricBlockMiner,
		MetricBalanceLatest,
		MetricNonceLatest,
		MetricTxCountLatest,
		MetricBalanceAtBlock,
		MetricNonceAtBlock,
		MetricTotalAddressCount,
		MetricTotalTxCount,
		MetricTotalContractCount,
		MetricERC20TokenCount,
		MetricERC20BalanceLatest,
		MetricERC20HoldingsLatest,
		MetricInternalTxByTx,
		MetricInternalTxByBlock,
	}
}
