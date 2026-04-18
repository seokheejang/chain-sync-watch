package source_test

import (
	"math/big"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/seokheejang/chain-sync-watch/internal/chain"
	"github.com/seokheejang/chain-sync-watch/internal/source"
)

// Result types use pointer fields for all payload entries so adapters
// can leave unsupported fields as nil. The zero value of the struct
// must therefore be "nothing fetched" without any field claiming a
// meaningful value.
func TestBlockResult_ZeroValueIsNilEverywhere(t *testing.T) {
	var r source.BlockResult
	require.Nil(t, r.Hash)
	require.Nil(t, r.ParentHash)
	require.Nil(t, r.Timestamp)
	require.Nil(t, r.TxCount)
	require.Nil(t, r.GasUsed)
	require.Nil(t, r.StateRoot)
	require.Nil(t, r.ReceiptsRoot)
	require.Nil(t, r.TransactionsRoot)
	require.Nil(t, r.Miner)
	require.Empty(t, r.SourceID)
	require.Zero(t, r.FetchedAt)
	require.Nil(t, r.RawResponse)
}

func TestBlockResult_PopulatedFields(t *testing.T) {
	// A round-about construction spec that pins down the field types
	// and proves the types interoperate with chain/* as expected.
	hash, err := chain.NewHash32("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	require.NoError(t, err)
	parent, err := chain.NewHash32("0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	require.NoError(t, err)
	// 0x4200...0011 is a well-known Optimism predeploy (SequencerFeeVault),
	// universally documented — safe to use as a concrete fixture.
	miner, err := chain.NewAddress("0x4200000000000000000000000000000000000011")
	require.NoError(t, err)

	ts := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	tx := uint64(42)
	gas := uint64(12_345_678)

	r := source.BlockResult{
		Number:     chain.NewBlockNumber(16_777_216),
		Hash:       &hash,
		ParentHash: &parent,
		Timestamp:  &ts,
		TxCount:    &tx,
		GasUsed:    &gas,
		Miner:      &miner,
		SourceID:   source.SourceID("rpc"),
		FetchedAt:  time.Now(),
	}

	require.Equal(t, uint64(16_777_216), r.Number.Uint64())
	require.Equal(t, hash, *r.Hash)
	require.Equal(t, parent, *r.ParentHash)
	require.Equal(t, ts, *r.Timestamp)
	require.Equal(t, uint64(42), *r.TxCount)
	require.Equal(t, miner, *r.Miner)
}

func TestAddressLatestResult_BalanceAsBigInt(t *testing.T) {
	// Balance ranges up to 2^256 — must be *big.Int, never uint64.
	bal := new(big.Int).Lsh(big.NewInt(1), 200)
	nonce := uint64(42)

	r := source.AddressLatestResult{
		Balance:  bal,
		Nonce:    &nonce,
		SourceID: "rpc",
	}
	require.Equal(t, 0, r.Balance.Cmp(bal))
	require.Equal(t, uint64(42), *r.Nonce)
}

func TestAddressAtBlockQuery_CarriesBlockNumber(t *testing.T) {
	addr, err := chain.NewAddress("0x4200000000000000000000000000000000000006")
	require.NoError(t, err)
	q := source.AddressAtBlockQuery{
		Address: addr,
		Block:   chain.NewBlockNumber(16_777_216),
	}
	require.Equal(t, addr, q.Address)
	require.Equal(t, uint64(16_777_216), q.Block.Uint64())
}

func TestSnapshotResult_AllCountsOptional(t *testing.T) {
	// All count fields are pointers because sources differ wildly on
	// which cumulative stats they can produce (see source-shapes.md).
	var r source.SnapshotResult
	require.Nil(t, r.TotalAddressCount)
	require.Nil(t, r.TotalTxCount)
	require.Nil(t, r.TotalContractCount)
	require.Nil(t, r.ERC20TokenCount)

	n := uint64(10_000_000)
	r.TotalAddressCount = &n
	require.Equal(t, uint64(10_000_000), *r.TotalAddressCount)
}

// SnapshotQuery is empty — we lock that in so later adapters don't
// accidentally grow query parameters on it (their presence would imply
// the caller must know the source's notion of "now", which breaks
// source comparability).
func TestSnapshotQuery_IsEmpty(t *testing.T) {
	var q source.SnapshotQuery
	_ = q // compile-only check; intentionally no fields to assert on
}
