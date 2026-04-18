package source

import "github.com/seokheejang/chain-sync-watch/internal/chain"

// BlockQuery requests block-level data by height.
type BlockQuery struct {
	Number chain.BlockNumber
}

// AddressQuery requests the latest on-chain state of an account.
type AddressQuery struct {
	Address chain.Address
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
