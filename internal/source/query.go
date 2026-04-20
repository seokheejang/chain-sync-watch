package source

import "github.com/seokheejang/chain-sync-watch/internal/chain"

// BlockQuery requests block-level data by height.
type BlockQuery struct {
	Number chain.BlockNumber
}

// AddressQuery requests the on-chain state of an account at an anchor.
//
// Anchor is a BlockTag; the zero value is "latest", so existing call
// sites that only set Address keep their pre-2C semantics. Callers who
// want reorg-safe results should set Anchor to a finalized tag (either
// NewBlockTagFinalized() or an explicit BlockTagAt(n) resolved via
// ChainHead.Finalized).
//
// For numeric historical queries, prefer AddressAtBlockQuery —
// FetchAddressAtBlock is the dedicated "archive read" method and
// distinguishes adapters that cannot serve archive data at all.
type AddressQuery struct {
	Address chain.Address
	Anchor  BlockTag
}

// AddressAtBlockQuery requests historical state — only archive-capable
// sources can serve this.
type AddressAtBlockQuery struct {
	Address chain.Address
	Block   chain.BlockNumber
}

// SnapshotQuery intentionally carries no parameters. Cumulative stats
// are point-in-time reports from whichever source is consulted; making
// callers pick a timestamp would imply precision these sources don't
// share with each other.
type SnapshotQuery struct{}

// ERC20BalanceQuery requests the balance of a specific ERC-20 token
// held by an address. Anchor follows the same zero-value-is-latest
// convention as AddressQuery.
type ERC20BalanceQuery struct {
	Address chain.Address
	Token   chain.Address
	Anchor  BlockTag
}

// ERC20HoldingsQuery lists every ERC-20 balance an address holds at
// Anchor. Reconstructing this from RPC alone is impractical (requires
// a Transfer-log scan across all blocks), so adapters backed by an
// indexer are the canonical servers.
type ERC20HoldingsQuery struct {
	Address chain.Address
	Anchor  BlockTag
}

// InternalTxByTxQuery requests every internal call recorded within a
// single transaction. TxHash is the transaction whose trace we want.
type InternalTxByTxQuery struct {
	TxHash chain.TxHash
}

// InternalTxByBlockQuery requests every internal call recorded across
// every transaction in a block. Block is the height of interest; the
// result's ReflectedBlock should equal Block for strictly-indexed
// sources.
type InternalTxByBlockQuery struct {
	Block chain.BlockNumber
}
