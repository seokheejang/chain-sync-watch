package application_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/seokheejang/chain-sync-watch/internal/application"
	"github.com/seokheejang/chain-sync-watch/internal/application/testsupport"
	"github.com/seokheejang/chain-sync-watch/internal/chain"
	"github.com/seokheejang/chain-sync-watch/internal/verification"
)

func seedRun(t *testing.T, repo *testsupport.FakeRunRepo, id verification.RunID, cid chain.ChainID, createdAt time.Time) *verification.Run {
	t.Helper()
	r, err := verification.NewRun(
		id,
		cid,
		verification.LatestN{N: 1},
		[]verification.Metric{verification.MetricBlockHash},
		verification.ManualTrigger{User: "u"},
		createdAt,
	)
	require.NoError(t, err)
	require.NoError(t, repo.Save(context.Background(), r))
	return r
}

func TestQueryRuns_GetReturnsStoredRun(t *testing.T) {
	repo := testsupport.NewFakeRunRepo()
	uc := application.QueryRuns{Runs: repo}
	t0 := time.Date(2026, 4, 21, 0, 0, 0, 0, time.UTC)
	seedRun(t, repo, "r1", chain.OptimismMainnet, t0)

	got, err := uc.Get(context.Background(), "r1")
	require.NoError(t, err)
	require.Equal(t, verification.RunID("r1"), got.ID())
}

func TestQueryRuns_GetMissingReturnsNotFound(t *testing.T) {
	repo := testsupport.NewFakeRunRepo()
	uc := application.QueryRuns{Runs: repo}
	_, err := uc.Get(context.Background(), "missing")
	require.ErrorIs(t, err, application.ErrRunNotFound)
}

func TestQueryRuns_ListFiltersByChainAndStatus(t *testing.T) {
	repo := testsupport.NewFakeRunRepo()
	uc := application.QueryRuns{Runs: repo}
	t0 := time.Date(2026, 4, 21, 0, 0, 0, 0, time.UTC)

	// Two on Optimism (one pending, one running), one on Ethereum.
	seedRun(t, repo, "opt-pending", chain.OptimismMainnet, t0)
	opt2 := seedRun(t, repo, "opt-running", chain.OptimismMainnet, t0.Add(time.Hour))
	require.NoError(t, opt2.Start(t0.Add(2*time.Hour)))
	require.NoError(t, repo.Save(context.Background(), opt2))
	seedRun(t, repo, "eth-1", chain.EthereumMainnet, t0.Add(3*time.Hour))

	cid := chain.OptimismMainnet
	runs, total, err := uc.List(context.Background(), application.RunFilter{ChainID: &cid})
	require.NoError(t, err)
	require.Equal(t, 2, total)
	require.Len(t, runs, 2)

	st := verification.StatusRunning
	runs, total, err = uc.List(context.Background(), application.RunFilter{
		ChainID: &cid,
		Status:  &st,
	})
	require.NoError(t, err)
	require.Equal(t, 1, total)
	require.Equal(t, verification.RunID("opt-running"), runs[0].ID())
}

func TestQueryRuns_ListPagination(t *testing.T) {
	repo := testsupport.NewFakeRunRepo()
	uc := application.QueryRuns{Runs: repo}
	t0 := time.Date(2026, 4, 21, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 5; i++ {
		seedRun(t, repo, verification.RunID([]byte{'r', byte('0' + i)}), chain.OptimismMainnet, t0.Add(time.Duration(i)*time.Hour))
	}

	runs, total, err := uc.List(context.Background(), application.RunFilter{Limit: 2})
	require.NoError(t, err)
	require.Equal(t, 5, total)
	require.Len(t, runs, 2)

	runs, total, err = uc.List(context.Background(), application.RunFilter{Limit: 2, Offset: 2})
	require.NoError(t, err)
	require.Equal(t, 5, total)
	require.Len(t, runs, 2)

	runs, total, err = uc.List(context.Background(), application.RunFilter{Limit: 2, Offset: 10})
	require.NoError(t, err)
	require.Equal(t, 5, total)
	require.Empty(t, runs)
}

func TestQueryRuns_ListFiltersByCreatedAt(t *testing.T) {
	repo := testsupport.NewFakeRunRepo()
	uc := application.QueryRuns{Runs: repo}
	t0 := time.Date(2026, 4, 21, 0, 0, 0, 0, time.UTC)
	seedRun(t, repo, "old", chain.OptimismMainnet, t0)
	seedRun(t, repo, "mid", chain.OptimismMainnet, t0.Add(time.Hour))
	seedRun(t, repo, "new", chain.OptimismMainnet, t0.Add(2*time.Hour))

	window := &application.TimeRange{From: t0.Add(30 * time.Minute), To: t0.Add(90 * time.Minute)}
	runs, total, err := uc.List(context.Background(), application.RunFilter{CreatedAt: window})
	require.NoError(t, err)
	require.Equal(t, 1, total)
	require.Equal(t, verification.RunID("mid"), runs[0].ID())
}
