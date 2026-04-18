package source

import (
	"math/big"
	"time"

	"github.com/seokheejang/chain-sync-watch/internal/chain"
)

// BlockResult holds every block-level field a source might populate.
//
// All payload fields are pointers so the zero value of the struct means
// "nothing fetched" — adapters leave fields they cannot serve as nil,
// and callers check Source.Supports(cap) before trusting a given field.
// SourceID, FetchedAt, and RawResponse are metadata, not payload, so
// they are plain values.
type BlockResult struct {
	Number           chain.BlockNumber
	Hash             *chain.BlockHash
	ParentHash       *chain.BlockHash
	Timestamp        *time.Time
	TxCount          *uint64
	GasUsed          *uint64
	StateRoot        *chain.Hash32
	ReceiptsRoot     *chain.Hash32
	TransactionsRoot *chain.Hash32
	Miner            *chain.Address

	SourceID    SourceID
	FetchedAt   time.Time
	RawResponse []byte // populated only when config raw_response.persist=true
}

// AddressLatestResult reports account state as observed at the source's
// current tip. Two sources asked for "latest" at roughly the same wall
// clock may observe different heights; callers should treat small
// nonce/tx-count divergences accordingly.
type AddressLatestResult struct {
	Balance *big.Int // *big.Int — balances exceed uint64
	Nonce   *uint64
	TxCount *uint64

	SourceID    SourceID
	FetchedAt   time.Time
	RawResponse []byte
}

// AddressAtBlockResult is the archive-backed counterpart. Block echoes
// the requested height so callers do not have to thread it through
// their own state.
type AddressAtBlockResult struct {
	Balance *big.Int
	Nonce   *uint64
	Block   chain.BlockNumber

	SourceID    SourceID
	FetchedAt   time.Time
	RawResponse []byte
}

// SnapshotResult groups the "whole-chain" cumulative values sources can
// report. Every count is a pointer — no source serves all four, and a
// zeroed value cannot be distinguished from a truly-empty chain, so we
// force callers to handle "unknown" explicitly.
//
// SnapshotAt is whatever the source claims as the moment the numbers
// were taken. Adapters that don't surface such a timestamp leave it as
// the zero time and rely on FetchedAt.
type SnapshotResult struct {
	TotalAddressCount  *uint64
	TotalTxCount       *uint64
	TotalContractCount *uint64
	ERC20TokenCount    *uint64

	SnapshotAt  time.Time
	SourceID    SourceID
	FetchedAt   time.Time
	RawResponse []byte
}
