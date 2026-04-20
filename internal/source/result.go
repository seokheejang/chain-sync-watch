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
//
// ReflectedBlock is the block height this response actually reflects,
// when the upstream provides that metadata (Blockscout's
// `block_number_balance_updated_at` is the canonical example). A nil
// value means the source did not surface reflected-block information —
// anchor-window tolerance checks must discard or down-weight the
// sample in that case.
type AddressLatestResult struct {
	Balance *big.Int // *big.Int — balances exceed uint64
	Nonce   *uint64
	TxCount *uint64

	SourceID       SourceID
	FetchedAt      time.Time
	ReflectedBlock *chain.BlockNumber
	RawResponse    []byte
}

// AddressAtBlockResult is the archive-backed counterpart. Block echoes
// the requested height so callers do not have to thread it through
// their own state. ReflectedBlock should equal Block for adapters that
// honour the anchor strictly; a mismatch (or a nil ReflectedBlock from
// an adapter that exposes such metadata) signals a disagreement the
// Tolerance layer can surface.
type AddressAtBlockResult struct {
	Balance *big.Int
	Nonce   *uint64
	Block   chain.BlockNumber

	SourceID       SourceID
	FetchedAt      time.Time
	ReflectedBlock *chain.BlockNumber
	RawResponse    []byte
}

// SnapshotResult groups the "whole-chain" cumulative values sources can
// report. Every count is a pointer — no source serves all four, and a
// zeroed value cannot be distinguished from a truly-empty chain, so we
// force callers to handle "unknown" explicitly.
//
// SnapshotAt is whatever the source claims as the moment the numbers
// were taken. Adapters that don't surface such a timestamp leave it as
// the zero time and rely on FetchedAt. ReflectedBlock is the
// "last-indexed block" the source associates with the stats, when
// available; indexers that only return latest-only aggregates leave
// it nil and their data ends up in observation-only categories.
type SnapshotResult struct {
	TotalAddressCount  *uint64
	TotalTxCount       *uint64
	TotalContractCount *uint64
	ERC20TokenCount    *uint64

	SnapshotAt     time.Time
	SourceID       SourceID
	FetchedAt      time.Time
	ReflectedBlock *chain.BlockNumber
	RawResponse    []byte
}

// ERC20BalanceResult reports the balance of a single ERC-20 token for
// one address. Decimals and Symbol mirror the contract metadata when
// the source surfaces it; adapters that return only the raw balance
// leave them as zero values and the caller has to look up the token
// out of band.
type ERC20BalanceResult struct {
	Balance  *big.Int // nil when the source returns no value (treat as unsupported, not zero)
	Decimals uint8
	Symbol   string

	SourceID       SourceID
	FetchedAt      time.Time
	ReflectedBlock *chain.BlockNumber
	RawResponse    []byte
}

// TokenHolding is a single entry in an ERC-20 holdings list.
type TokenHolding struct {
	Contract chain.Address
	Name     string
	Symbol   string
	Decimals uint8
	Balance  *big.Int
}

// ERC20HoldingsResult groups every token an address holds. Tokens may
// be nil (source reports no holdings) or empty slice (address has no
// tokens) — callers should treat both the same.
type ERC20HoldingsResult struct {
	Tokens []TokenHolding

	SourceID       SourceID
	FetchedAt      time.Time
	ReflectedBlock *chain.BlockNumber
	RawResponse    []byte
}

// InternalTx is one decoded internal call from a trace.
//
// CallType takes the strings the chain exposes: "call", "delegatecall",
// "staticcall", "create", "create2", "selfdestruct". Error is the
// empty string for successful calls.
type InternalTx struct {
	From     chain.Address
	To       chain.Address
	Value    *big.Int
	GasUsed  uint64
	CallType string
	Error    string
}

// InternalTxResult is shared by the by-block and by-tx fetch methods —
// the payload shape is identical, only the query that produced it
// differs. ReflectedBlock is the block the trace belongs to (the
// requested block for by-block queries, the transaction's block for
// by-tx queries).
type InternalTxResult struct {
	Traces []InternalTx

	SourceID       SourceID
	FetchedAt      time.Time
	ReflectedBlock *chain.BlockNumber
	RawResponse    []byte
}
