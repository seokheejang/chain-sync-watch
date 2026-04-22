//go:build integration

package persistence_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/seokheejang/chain-sync-watch/internal/application"
	"github.com/seokheejang/chain-sync-watch/internal/chain"
	"github.com/seokheejang/chain-sync-watch/internal/infrastructure/persistence"
	"github.com/seokheejang/chain-sync-watch/internal/verification"
)

func newTestRun(t *testing.T, id verification.RunID, cid chain.ChainID, created time.Time) *verification.Run {
	t.Helper()
	r, err := verification.NewRun(
		id,
		cid,
		verification.LatestN{N: 3},
		[]verification.Metric{verification.MetricBlockHash, verification.MetricBlockTimestamp},
		verification.ManualTrigger{User: "alice"},
		created,
	)
	require.NoError(t, err)
	return r
}

func TestIntegrationRunRepo_SaveAndFind(t *testing.T) {
	resetDB(t)
	repo := persistence.NewRunRepo(testDB)
	ctx := context.Background()

	created := time.Date(2026, 4, 21, 10, 0, 0, 0, time.UTC)
	run := newTestRun(t, "rid-1", chain.OptimismMainnet, created)
	require.NoError(t, repo.Save(ctx, run))

	got, err := repo.FindByID(ctx, "rid-1")
	require.NoError(t, err)
	require.Equal(t, run.ID(), got.ID())
	require.Equal(t, run.ChainID(), got.ChainID())
	require.Equal(t, verification.StatusPending, got.Status())
	require.WithinDuration(t, created, got.CreatedAt(), time.Second)
}

func TestIntegrationRunRepo_SaveUpsertsOnStateTransition(t *testing.T) {
	resetDB(t)
	repo := persistence.NewRunRepo(testDB)
	ctx := context.Background()

	created := time.Date(2026, 4, 21, 10, 0, 0, 0, time.UTC)
	run := newTestRun(t, "rid-2", chain.OptimismMainnet, created)
	require.NoError(t, repo.Save(ctx, run))

	require.NoError(t, run.Start(created.Add(time.Second)))
	require.NoError(t, repo.Save(ctx, run))

	require.NoError(t, run.Complete(created.Add(2*time.Second)))
	require.NoError(t, repo.Save(ctx, run))

	got, err := repo.FindByID(ctx, "rid-2")
	require.NoError(t, err)
	require.Equal(t, verification.StatusCompleted, got.Status())
	require.NotNil(t, got.StartedAt())
	require.NotNil(t, got.FinishedAt())
}

func TestIntegrationRunRepo_FindMissingReturnsNotFound(t *testing.T) {
	resetDB(t)
	repo := persistence.NewRunRepo(testDB)
	_, err := repo.FindByID(context.Background(), "no-such-run")
	require.ErrorIs(t, err, application.ErrRunNotFound)
}

func TestIntegrationRunRepo_ListFilterByChainAndStatus(t *testing.T) {
	resetDB(t)
	repo := persistence.NewRunRepo(testDB)
	ctx := context.Background()
	base := time.Date(2026, 4, 21, 10, 0, 0, 0, time.UTC)

	r1 := newTestRun(t, "opt-p", chain.OptimismMainnet, base)
	r2 := newTestRun(t, "opt-r", chain.OptimismMainnet, base.Add(time.Hour))
	require.NoError(t, r2.Start(base.Add(time.Hour)))
	r3 := newTestRun(t, "eth-p", chain.EthereumMainnet, base.Add(2*time.Hour))

	require.NoError(t, repo.Save(ctx, r1))
	require.NoError(t, repo.Save(ctx, r2))
	require.NoError(t, repo.Save(ctx, r3))

	cid := chain.OptimismMainnet
	rows, total, err := repo.List(ctx, application.RunFilter{ChainID: &cid})
	require.NoError(t, err)
	require.Equal(t, 2, total)
	require.Len(t, rows, 2)
	// Order: created_at DESC.
	require.Equal(t, verification.RunID("opt-r"), rows[0].ID())
	require.Equal(t, verification.RunID("opt-p"), rows[1].ID())

	st := verification.StatusRunning
	rows, total, err = repo.List(ctx, application.RunFilter{ChainID: &cid, Status: &st})
	require.NoError(t, err)
	require.Equal(t, 1, total)
	require.Equal(t, verification.RunID("opt-r"), rows[0].ID())
}

func TestIntegrationRunRepo_ListPagination(t *testing.T) {
	resetDB(t)
	repo := persistence.NewRunRepo(testDB)
	ctx := context.Background()
	base := time.Date(2026, 4, 21, 10, 0, 0, 0, time.UTC)

	for i := 0; i < 5; i++ {
		r := newTestRun(t, verification.RunID([]byte{'r', byte('0' + i)}), chain.OptimismMainnet, base.Add(time.Duration(i)*time.Hour))
		require.NoError(t, repo.Save(ctx, r))
	}

	rows, total, err := repo.List(ctx, application.RunFilter{Limit: 2})
	require.NoError(t, err)
	require.Equal(t, 5, total)
	require.Len(t, rows, 2)

	rows, total, err = repo.List(ctx, application.RunFilter{Limit: 2, Offset: 2})
	require.NoError(t, err)
	require.Equal(t, 5, total)
	require.Len(t, rows, 2)

	rows, total, err = repo.List(ctx, application.RunFilter{Limit: 2, Offset: 10})
	require.NoError(t, err)
	require.Equal(t, 5, total)
	require.Empty(t, rows)
}

func TestIntegrationRunRepo_PersistsEveryStrategyAndTrigger(t *testing.T) {
	resetDB(t)
	repo := persistence.NewRunRepo(testDB)
	ctx := context.Background()
	base := time.Date(2026, 4, 21, 10, 0, 0, 0, time.UTC)
	br, err := chain.NewBlockRange(10, 100)
	require.NoError(t, err)

	runs := []*verification.Run{
		newRunWithStrategyAndTrigger(t, "fixed-manual",
			verification.FixedList{Numbers: []chain.BlockNumber{1, 2, 3}},
			verification.ManualTrigger{User: "alice"},
			base),
		newRunWithStrategyAndTrigger(t, "latest-scheduled",
			verification.LatestN{N: 5},
			verification.ScheduledTrigger{CronExpr: "0 */6 * * *"},
			base.Add(time.Hour)),
		newRunWithStrategyAndTrigger(t, "random-realtime",
			verification.Random{Range: br, Count: 5, Seed: 42},
			verification.RealtimeTrigger{BlockNumber: 77},
			base.Add(2*time.Hour)),
		newRunWithStrategyAndTrigger(t, "sparse-manual",
			verification.SparseSteps{Range: br, Step: 10},
			verification.ManualTrigger{User: "bob"},
			base.Add(3*time.Hour)),
	}
	for _, r := range runs {
		require.NoError(t, repo.Save(ctx, r))
	}

	for _, r := range runs {
		got, err := repo.FindByID(ctx, r.ID())
		require.NoError(t, err)
		require.Equal(t, r.Strategy().Kind(), got.Strategy().Kind())
		require.Equal(t, r.Trigger().Kind(), got.Trigger().Kind())
	}
}

func TestIntegrationRunRepo_AddressPlansRoundTrip(t *testing.T) {
	resetDB(t)
	repo := persistence.NewRunRepo(testDB)
	ctx := context.Background()

	created := time.Date(2026, 4, 21, 10, 0, 0, 0, time.UTC)
	a := chain.MustAddress("0x0000000000000000000000000000000000000001")
	b := chain.MustAddress("0x0000000000000000000000000000000000000002")

	r, err := verification.NewRun(
		"rid-plans",
		chain.OptimismMainnet,
		verification.LatestN{N: 1},
		[]verification.Metric{verification.MetricBalanceLatest},
		verification.ManualTrigger{User: "u"},
		created,
		verification.KnownAddresses{Addresses: []chain.Address{a, b}},
		verification.TopNHolders{N: 25},
		verification.RandomAddresses{Count: 5, Seed: 7},
		verification.RecentlyActive{RecentBlocks: 200, Count: 10, Seed: 13},
	)
	require.NoError(t, err)
	require.NoError(t, repo.Save(ctx, r))

	got, err := repo.FindByID(ctx, "rid-plans")
	require.NoError(t, err)

	plans := got.AddressPlans()
	require.Len(t, plans, 4)
	require.Equal(t, verification.KindKnownAddresses, plans[0].Kind())
	require.Equal(t, verification.KindTopNHolders, plans[1].Kind())
	require.Equal(t, verification.KindRandomAddresses, plans[2].Kind())
	require.Equal(t, verification.KindRecentlyActive, plans[3].Kind())

	k := plans[0].(verification.KnownAddresses)
	require.Equal(t, []chain.Address{a, b}, k.Addresses)

	rn := plans[2].(verification.RandomAddresses)
	require.Equal(t, int64(7), rn.Seed)
}

func TestIntegrationRunRepo_NoAddressPlans_DefaultEmptyArray(t *testing.T) {
	resetDB(t)
	repo := persistence.NewRunRepo(testDB)
	ctx := context.Background()

	created := time.Date(2026, 4, 21, 10, 0, 0, 0, time.UTC)
	r := newTestRun(t, "rid-default", chain.OptimismMainnet, created)
	require.NoError(t, repo.Save(ctx, r))

	got, err := repo.FindByID(ctx, "rid-default")
	require.NoError(t, err)
	require.Nil(t, got.AddressPlans())
}

func newRunWithStrategyAndTrigger(t *testing.T, id verification.RunID, s verification.SamplingStrategy, tr verification.Trigger, created time.Time) *verification.Run {
	t.Helper()
	r, err := verification.NewRun(
		id,
		chain.OptimismMainnet,
		s,
		[]verification.Metric{verification.MetricBlockHash},
		tr,
		created,
	)
	require.NoError(t, err)
	return r
}
