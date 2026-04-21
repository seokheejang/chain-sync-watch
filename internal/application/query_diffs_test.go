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
	"github.com/seokheejang/chain-sync-watch/internal/verification"
)

func seedDiff(t *testing.T, repo *testsupport.FakeDiffRepo, runID verification.RunID, metric verification.Metric, block chain.BlockNumber, sev diff.Severity, detectedAt time.Time) application.DiffID {
	t.Helper()
	d, err := diff.NewDiscrepancy(
		runID,
		metric,
		block,
		diff.Subject{Type: diff.SubjectBlock},
		map[source.SourceID]diff.ValueSnapshot{
			"rpc":        {Raw: "0xA"},
			"blockscout": {Raw: "0xB"},
		},
		detectedAt,
	)
	require.NoError(t, err)
	j := diff.Judgement{Severity: sev, Reasoning: "test"}
	id, err := repo.Save(context.Background(), &d, j, application.SaveDiffMeta{})
	require.NoError(t, err)
	return id
}

func TestQueryDiffs_GetReturnsStored(t *testing.T) {
	repo := testsupport.NewFakeDiffRepo()
	uc := application.QueryDiffs{Diffs: repo}
	t0 := time.Date(2026, 4, 21, 0, 0, 0, 0, time.UTC)
	id := seedDiff(t, repo, "r1", verification.MetricBlockHash, 100, diff.SevCritical, t0)

	got, err := uc.Get(context.Background(), id)
	require.NoError(t, err)
	require.Equal(t, id, got.ID)
	require.Equal(t, diff.SevCritical, got.Judgement.Severity)
}

func TestQueryDiffs_GetMissingReturnsNotFound(t *testing.T) {
	repo := testsupport.NewFakeDiffRepo()
	uc := application.QueryDiffs{Diffs: repo}
	_, err := uc.Get(context.Background(), "missing")
	require.ErrorIs(t, err, application.ErrDiffNotFound)
}

func TestQueryDiffs_ByRun(t *testing.T) {
	repo := testsupport.NewFakeDiffRepo()
	uc := application.QueryDiffs{Diffs: repo}
	t0 := time.Date(2026, 4, 21, 0, 0, 0, 0, time.UTC)
	seedDiff(t, repo, "r1", verification.MetricBlockHash, 100, diff.SevCritical, t0)
	seedDiff(t, repo, "r1", verification.MetricBlockHash, 101, diff.SevCritical, t0.Add(time.Second))
	seedDiff(t, repo, "r2", verification.MetricBlockHash, 200, diff.SevCritical, t0.Add(2*time.Second))

	recs, err := uc.ByRun(context.Background(), "r1")
	require.NoError(t, err)
	require.Len(t, recs, 2)
}

func TestQueryDiffs_ListFiltersBySeverity(t *testing.T) {
	repo := testsupport.NewFakeDiffRepo()
	uc := application.QueryDiffs{Diffs: repo}
	t0 := time.Date(2026, 4, 21, 0, 0, 0, 0, time.UTC)
	seedDiff(t, repo, "r1", verification.MetricBlockHash, 100, diff.SevCritical, t0)
	seedDiff(t, repo, "r1", verification.MetricBalanceLatest, 101, diff.SevWarning, t0.Add(time.Second))
	seedDiff(t, repo, "r1", verification.MetricTotalTxCount, 102, diff.SevInfo, t0.Add(2*time.Second))

	sev := diff.SevWarning
	recs, total, err := uc.List(context.Background(), application.DiffFilter{Severity: &sev})
	require.NoError(t, err)
	require.Equal(t, 1, total)
	require.Equal(t, diff.SevWarning, recs[0].Judgement.Severity)
}

func TestQueryDiffs_ListFiltersByBlockRange(t *testing.T) {
	repo := testsupport.NewFakeDiffRepo()
	uc := application.QueryDiffs{Diffs: repo}
	t0 := time.Date(2026, 4, 21, 0, 0, 0, 0, time.UTC)
	seedDiff(t, repo, "r1", verification.MetricBlockHash, 100, diff.SevCritical, t0)
	seedDiff(t, repo, "r1", verification.MetricBlockHash, 200, diff.SevCritical, t0.Add(time.Second))
	seedDiff(t, repo, "r1", verification.MetricBlockHash, 300, diff.SevCritical, t0.Add(2*time.Second))

	br, err := chain.NewBlockRange(150, 250)
	require.NoError(t, err)
	recs, total, err := uc.List(context.Background(), application.DiffFilter{BlockRange: &br})
	require.NoError(t, err)
	require.Equal(t, 1, total)
	require.Equal(t, chain.BlockNumber(200), recs[0].Discrepancy.Block)
}

func TestQueryDiffs_ListPagination(t *testing.T) {
	repo := testsupport.NewFakeDiffRepo()
	uc := application.QueryDiffs{Diffs: repo}
	t0 := time.Date(2026, 4, 21, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 5; i++ {
		seedDiff(t, repo, "r1", verification.MetricBlockHash, chain.BlockNumber(100+i), diff.SevCritical, t0.Add(time.Duration(i)*time.Second))
	}
	recs, total, err := uc.List(context.Background(), application.DiffFilter{Limit: 2})
	require.NoError(t, err)
	require.Equal(t, 5, total)
	require.Len(t, recs, 2)
}
