//go:build integration

package persistence_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/seokheejang/chain-sync-watch/internal/application"
	"github.com/seokheejang/chain-sync-watch/internal/chain"
	"github.com/seokheejang/chain-sync-watch/internal/diff"
	"github.com/seokheejang/chain-sync-watch/internal/infrastructure/persistence"
	"github.com/seokheejang/chain-sync-watch/internal/source"
	"github.com/seokheejang/chain-sync-watch/internal/verification"
)

// seedParentRun inserts a Run so the FK constraint on
// discrepancies.run_id is satisfiable.
func seedParentRun(t *testing.T, id verification.RunID) {
	t.Helper()
	ctx := context.Background()
	runRepo := persistence.NewRunRepo(testDB)
	base := time.Date(2026, 4, 21, 10, 0, 0, 0, time.UTC)
	r, err := verification.NewRun(
		id,
		chain.OptimismMainnet,
		verification.LatestN{N: 1},
		[]verification.Metric{verification.MetricBlockHash},
		verification.ManualTrigger{User: "u"},
		base,
	)
	require.NoError(t, err)
	require.NoError(t, runRepo.Save(ctx, r))
}

func mkDiscrepancy(t *testing.T, runID verification.RunID, metric verification.Metric, block chain.BlockNumber, detectedAt time.Time) diff.Discrepancy {
	t.Helper()
	d, err := diff.NewDiscrepancy(
		runID,
		metric,
		block,
		diff.Subject{Type: diff.SubjectBlock},
		map[source.SourceID]diff.ValueSnapshot{
			"rpc":        {Raw: "0xAA", FetchedAt: detectedAt},
			"blockscout": {Raw: "0xBB", FetchedAt: detectedAt},
		},
		detectedAt,
	)
	require.NoError(t, err)
	return d
}

func TestIntegrationDiffRepo_SaveAndFindByID(t *testing.T) {
	resetDB(t)
	seedParentRun(t, "rid")
	repo := persistence.NewDiffRepo(testDB)
	ctx := context.Background()

	d := mkDiscrepancy(t, "rid", verification.MetricBlockHash, 100, time.Now().UTC())
	j := diff.Judgement{
		Severity:       diff.SevCritical,
		TrustedSources: []source.SourceID{"rpc"},
		Reasoning:      "blockscout diverged",
	}
	id, err := repo.Save(ctx, &d, j, application.SaveDiffMeta{Tier: source.TierA, AnchorBlock: 100})
	require.NoError(t, err)
	require.NotEmpty(t, id)

	got, err := repo.FindByID(ctx, id)
	require.NoError(t, err)
	require.Equal(t, id, got.ID)
	require.Equal(t, chain.BlockNumber(100), got.Discrepancy.Block)
	require.Equal(t, diff.SevCritical, got.Judgement.Severity)
	require.Equal(t, []source.SourceID{"rpc"}, got.Judgement.TrustedSources)
	require.Equal(t, source.TierA, got.Tier)
	require.Equal(t, "0xAA", got.Discrepancy.Values["rpc"].Raw)
}

func TestIntegrationDiffRepo_FindByRun_OrdersByDetectedAt(t *testing.T) {
	resetDB(t)
	seedParentRun(t, "rid")
	repo := persistence.NewDiffRepo(testDB)
	ctx := context.Background()

	base := time.Date(2026, 4, 21, 10, 0, 0, 0, time.UTC)
	for i, block := range []chain.BlockNumber{100, 101, 102} {
		d := mkDiscrepancy(t, "rid", verification.MetricBlockHash, block, base.Add(time.Duration(i)*time.Second))
		_, err := repo.Save(ctx, &d, diff.Judgement{Severity: diff.SevCritical}, application.SaveDiffMeta{})
		require.NoError(t, err)
	}
	recs, err := repo.FindByRun(ctx, "rid")
	require.NoError(t, err)
	require.Len(t, recs, 3)
	require.Equal(t, chain.BlockNumber(100), recs[0].Discrepancy.Block)
	require.Equal(t, chain.BlockNumber(102), recs[2].Discrepancy.Block)
}

func TestIntegrationDiffRepo_FindByIDNotFound(t *testing.T) {
	resetDB(t)
	repo := persistence.NewDiffRepo(testDB)
	_, err := repo.FindByID(context.Background(), "999999")
	require.ErrorIs(t, err, application.ErrDiffNotFound)

	_, err = repo.FindByID(context.Background(), "not-a-number")
	require.ErrorIs(t, err, application.ErrDiffNotFound)
}

func TestIntegrationDiffRepo_ListFilterBySeverity(t *testing.T) {
	resetDB(t)
	seedParentRun(t, "rid")
	repo := persistence.NewDiffRepo(testDB)
	ctx := context.Background()
	base := time.Date(2026, 4, 21, 10, 0, 0, 0, time.UTC)

	sevs := []diff.Severity{diff.SevCritical, diff.SevWarning, diff.SevInfo}
	for i, s := range sevs {
		d := mkDiscrepancy(t, "rid", verification.MetricBlockHash, chain.BlockNumber(100+i), base.Add(time.Duration(i)*time.Second))
		_, err := repo.Save(ctx, &d, diff.Judgement{Severity: s}, application.SaveDiffMeta{})
		require.NoError(t, err)
	}

	warn := diff.SevWarning
	recs, total, err := repo.List(ctx, application.DiffFilter{Severity: &warn})
	require.NoError(t, err)
	require.Equal(t, 1, total)
	require.Equal(t, diff.SevWarning, recs[0].Judgement.Severity)
}

func TestIntegrationDiffRepo_ListFilterByBlockRange(t *testing.T) {
	resetDB(t)
	seedParentRun(t, "rid")
	repo := persistence.NewDiffRepo(testDB)
	ctx := context.Background()
	base := time.Date(2026, 4, 21, 10, 0, 0, 0, time.UTC)

	for _, b := range []chain.BlockNumber{100, 200, 300, 400} {
		d := mkDiscrepancy(t, "rid", verification.MetricBlockHash, b, base)
		_, err := repo.Save(ctx, &d, diff.Judgement{Severity: diff.SevCritical}, application.SaveDiffMeta{})
		require.NoError(t, err)
	}

	br, err := chain.NewBlockRange(150, 350)
	require.NoError(t, err)
	recs, total, err := repo.List(ctx, application.DiffFilter{BlockRange: &br})
	require.NoError(t, err)
	require.Equal(t, 2, total)
	blocks := []chain.BlockNumber{recs[0].Discrepancy.Block, recs[1].Discrepancy.Block}
	require.Contains(t, blocks, chain.BlockNumber(200))
	require.Contains(t, blocks, chain.BlockNumber(300))
}

func TestIntegrationDiffRepo_MarkResolved(t *testing.T) {
	resetDB(t)
	seedParentRun(t, "rid")
	repo := persistence.NewDiffRepo(testDB)
	ctx := context.Background()

	d := mkDiscrepancy(t, "rid", verification.MetricBlockHash, 100, time.Now().UTC())
	id, err := repo.Save(ctx, &d, diff.Judgement{Severity: diff.SevCritical}, application.SaveDiffMeta{})
	require.NoError(t, err)

	resolvedAt := time.Date(2026, 4, 22, 0, 0, 0, 0, time.UTC)
	require.NoError(t, repo.MarkResolved(ctx, id, resolvedAt))

	got, err := repo.FindByID(ctx, id)
	require.NoError(t, err)
	require.True(t, got.Resolved)
	require.NotNil(t, got.ResolvedAt)
	require.Equal(t, resolvedAt.Unix(), got.ResolvedAt.Unix())
}

func TestIntegrationDiffRepo_MarkResolvedMissing(t *testing.T) {
	resetDB(t)
	repo := persistence.NewDiffRepo(testDB)
	err := repo.MarkResolved(context.Background(), "999999", time.Now())
	require.ErrorIs(t, err, application.ErrDiffNotFound)
}

func TestIntegrationDiffRepo_CascadeDeleteOnRun(t *testing.T) {
	resetDB(t)
	seedParentRun(t, "rid-cascade")
	repo := persistence.NewDiffRepo(testDB)
	ctx := context.Background()

	d := mkDiscrepancy(t, "rid-cascade", verification.MetricBlockHash, 100, time.Now().UTC())
	_, err := repo.Save(ctx, &d, diff.Judgement{Severity: diff.SevCritical}, application.SaveDiffMeta{})
	require.NoError(t, err)

	// Deleting the parent run must cascade.
	require.NoError(t, testDB.Exec("DELETE FROM runs WHERE id = ?", "rid-cascade").Error)

	recs, err := repo.FindByRun(ctx, "rid-cascade")
	require.NoError(t, err)
	require.Empty(t, recs)
}
