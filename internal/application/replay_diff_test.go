package application_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/seokheejang/chain-sync-watch/internal/application"
	"github.com/seokheejang/chain-sync-watch/internal/application/testsupport"
	"github.com/seokheejang/chain-sync-watch/internal/chain"
	"github.com/seokheejang/chain-sync-watch/internal/diff"
	"github.com/seokheejang/chain-sync-watch/internal/source"
	"github.com/seokheejang/chain-sync-watch/internal/source/fake"
	"github.com/seokheejang/chain-sync-watch/internal/verification"
)

type replayFixture struct {
	diffs   *testsupport.FakeDiffRepo
	gateway *testsupport.FakeSourceGateway
	clock   *testsupport.FakeClock
	useCase application.ReplayDiff
	base    time.Time
}

func newReplayFixture() *replayFixture {
	base := time.Date(2026, 4, 21, 0, 0, 0, 0, time.UTC)
	diffs := testsupport.NewFakeDiffRepo()
	gateway := testsupport.NewFakeSourceGateway()
	clock := testsupport.NewFakeClock(base)
	return &replayFixture{
		diffs:   diffs,
		gateway: gateway,
		clock:   clock,
		base:    base,
		useCase: application.ReplayDiff{
			Diffs:   diffs,
			Sources: gateway,
			Clock:   clock,
			Policy:  diff.DefaultPolicy{SourceTrust: []source.SourceID{"rpc", "blockscout"}},
		},
	}
}

// seedDivergentDiff seeds a DiffRecord where rpc and blockscout
// originally disagreed on block hash at block 100.
func (f *replayFixture) seedDivergentDiff(t *testing.T) application.DiffID {
	t.Helper()
	d, err := diff.NewDiscrepancy(
		"run-orig",
		verification.MetricBlockHash,
		100,
		diff.Subject{Type: diff.SubjectBlock},
		map[source.SourceID]diff.ValueSnapshot{
			"rpc":        {Raw: hex32("aa")},
			"blockscout": {Raw: hex32("bb")},
		},
		f.base,
	)
	require.NoError(t, err)
	id, err := f.diffs.Save(context.Background(), &d, diff.Judgement{Severity: diff.SevCritical})
	require.NoError(t, err)
	return id
}

func TestReplayDiff_NotFound(t *testing.T) {
	f := newReplayFixture()
	_, err := f.useCase.Execute(context.Background(), "missing")
	require.ErrorIs(t, err, application.ErrDiffNotFound)
}

func TestReplayDiff_ResolvedWhenSourcesNowAgree(t *testing.T) {
	f := newReplayFixture()
	id := f.seedDivergentDiff(t)

	// Both sources now return the same hash.
	agree := mkBlockResult(t, hex32("cc"), hex32("00"), 1700000000, 42)
	rpc := fake.New("rpc", chain.OptimismMainnet, fake.WithCapabilities(source.CapBlockHash))
	bs := fake.New("blockscout", chain.OptimismMainnet, fake.WithCapabilities(source.CapBlockHash))
	rpc.SetBlockResponse(agree, nil)
	bs.SetBlockResponse(agree, nil)
	f.gateway.Register(rpc)
	f.gateway.Register(bs)

	res, err := f.useCase.Execute(context.Background(), id)
	require.NoError(t, err)
	require.True(t, res.Resolved)
	require.Nil(t, res.NewDiffID)

	// Original record now flagged resolved.
	rec, err := f.diffs.FindByID(context.Background(), id)
	require.NoError(t, err)
	require.True(t, rec.Resolved)
	require.NotNil(t, rec.ResolvedAt)
	require.Equal(t, f.base, *rec.ResolvedAt)
	// No new diff saved.
	require.Equal(t, 1, f.diffs.Count())
}

func TestReplayDiff_StillDivergentSavesNewDiff(t *testing.T) {
	f := newReplayFixture()
	id := f.seedDivergentDiff(t)

	rpc := fake.New("rpc", chain.OptimismMainnet, fake.WithCapabilities(source.CapBlockHash))
	bs := fake.New("blockscout", chain.OptimismMainnet, fake.WithCapabilities(source.CapBlockHash))
	rpc.SetBlockResponse(mkBlockResult(t, hex32("11"), hex32("00"), 1700000000, 42), nil)
	bs.SetBlockResponse(mkBlockResult(t, hex32("22"), hex32("00"), 1700000000, 42), nil)
	f.gateway.Register(rpc)
	f.gateway.Register(bs)

	res, err := f.useCase.Execute(context.Background(), id)
	require.NoError(t, err)
	require.False(t, res.Resolved)
	require.NotNil(t, res.NewDiffID)
	require.NotEqual(t, id, *res.NewDiffID)
	require.Equal(t, 2, f.diffs.Count())

	// Original untouched.
	rec, err := f.diffs.FindByID(context.Background(), id)
	require.NoError(t, err)
	require.False(t, rec.Resolved)
}

func TestReplayDiff_UnknownSourceFails(t *testing.T) {
	f := newReplayFixture()
	id := f.seedDivergentDiff(t)
	// Gateway has no sources registered — Get returns an error.

	_, err := f.useCase.Execute(context.Background(), id)
	require.Error(t, err)
	require.Contains(t, err.Error(), "resolve source")
}

func TestReplayDiff_InsufficientSnapshotsFails(t *testing.T) {
	f := newReplayFixture()
	id := f.seedDivergentDiff(t)

	rpc := fake.New("rpc", chain.OptimismMainnet, fake.WithCapabilities(source.CapBlockHash))
	// blockscout registered but will NOT return a usable hash
	// (empty BlockResult with no Hash set).
	bs := fake.New("blockscout", chain.OptimismMainnet, fake.WithCapabilities(source.CapBlockHash))
	rpc.SetBlockResponse(mkBlockResult(t, hex32("ab"), hex32("00"), 1700000000, 1), nil)
	bs.SetBlockResponse(source.BlockResult{FetchedAt: f.base}, nil)
	f.gateway.Register(rpc)
	f.gateway.Register(bs)

	_, err := f.useCase.Execute(context.Background(), id)
	require.Error(t, err)
	require.Contains(t, err.Error(), "fewer than 2 sources")
}

func TestReplayDiff_RejectsNonBlockImmutable(t *testing.T) {
	f := newReplayFixture()
	// Seed a snapshot-category diff (not supported in MVP replay).
	d, err := diff.NewDiscrepancy(
		"run-x",
		verification.MetricTotalTxCount,
		100,
		diff.Subject{Type: diff.SubjectChain},
		map[source.SourceID]diff.ValueSnapshot{
			"rpc":        {Raw: "1"},
			"blockscout": {Raw: "2"},
		},
		f.base,
	)
	require.NoError(t, err)
	id, err := f.diffs.Save(context.Background(), &d, diff.Judgement{Severity: diff.SevInfo})
	require.NoError(t, err)

	_, err = f.useCase.Execute(context.Background(), id)
	require.Error(t, err)
	require.Contains(t, err.Error(), "not supported")
}
