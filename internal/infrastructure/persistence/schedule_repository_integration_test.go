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

func mkScheduleRecord(t *testing.T, jobID application.JobID, cron string, created time.Time) application.ScheduleRecord {
	t.Helper()
	s, err := verification.NewSchedule(cron, "UTC")
	require.NoError(t, err)
	return application.ScheduleRecord{
		JobID:     jobID,
		ChainID:   chain.OptimismMainnet,
		Schedule:  s,
		Strategy:  verification.LatestN{N: 3},
		Metrics:   []verification.Metric{verification.MetricBlockHash},
		CreatedAt: created,
		Active:    true,
	}
}

func TestIntegrationScheduleRepo_SaveAndListActive(t *testing.T) {
	resetDB(t)
	repo := persistence.NewScheduleRepo(testDB)
	ctx := context.Background()

	base := time.Date(2026, 4, 22, 10, 0, 0, 0, time.UTC)
	require.NoError(t, repo.Save(ctx, mkScheduleRecord(t, "job-1", "0 */6 * * *", base)))
	require.NoError(t, repo.Save(ctx, mkScheduleRecord(t, "job-2", "*/5 * * * *", base.Add(time.Hour))))

	records, err := repo.ListActive(ctx)
	require.NoError(t, err)
	require.Len(t, records, 2)
	require.Equal(t, application.JobID("job-1"), records[0].JobID, "CreatedAt-ascending order expected")
	require.Equal(t, application.JobID("job-2"), records[1].JobID)

	strat, ok := records[0].Strategy.(verification.LatestN)
	require.True(t, ok)
	require.Equal(t, uint(3), strat.N)
}

func TestIntegrationScheduleRepo_SaveIsUpsert(t *testing.T) {
	resetDB(t)
	repo := persistence.NewScheduleRepo(testDB)
	ctx := context.Background()

	base := time.Date(2026, 4, 22, 10, 0, 0, 0, time.UTC)
	require.NoError(t, repo.Save(ctx, mkScheduleRecord(t, "job-1", "0 */6 * * *", base)))

	// Re-save with a different cron — upsert by job_id.
	updated := mkScheduleRecord(t, "job-1", "*/30 * * * *", base)
	require.NoError(t, repo.Save(ctx, updated))

	records, err := repo.ListActive(ctx)
	require.NoError(t, err)
	require.Len(t, records, 1)
	require.Equal(t, "*/30 * * * *", records[0].Schedule.CronExpr())
}

func TestIntegrationScheduleRepo_DeactivateFiltersFromListActive(t *testing.T) {
	resetDB(t)
	repo := persistence.NewScheduleRepo(testDB)
	ctx := context.Background()

	base := time.Date(2026, 4, 22, 10, 0, 0, 0, time.UTC)
	require.NoError(t, repo.Save(ctx, mkScheduleRecord(t, "job-keep", "* * * * *", base)))
	require.NoError(t, repo.Save(ctx, mkScheduleRecord(t, "job-drop", "*/5 * * * *", base.Add(time.Hour))))

	require.NoError(t, repo.Deactivate(ctx, "job-drop"))

	records, err := repo.ListActive(ctx)
	require.NoError(t, err)
	require.Len(t, records, 1)
	require.Equal(t, application.JobID("job-keep"), records[0].JobID)
}

func TestIntegrationScheduleRepo_DeactivateUnknownIDIsNoOp(t *testing.T) {
	resetDB(t)
	repo := persistence.NewScheduleRepo(testDB)
	require.NoError(t, repo.Deactivate(context.Background(), "no-such-job"))
}

func TestIntegrationScheduleRepo_ListActiveEmpty(t *testing.T) {
	resetDB(t)
	repo := persistence.NewScheduleRepo(testDB)
	records, err := repo.ListActive(context.Background())
	require.NoError(t, err)
	require.Empty(t, records)
}

func TestIntegrationScheduleRepo_AddressPlansRoundTrip(t *testing.T) {
	resetDB(t)
	repo := persistence.NewScheduleRepo(testDB)
	ctx := context.Background()

	a := chain.MustAddress("0x0000000000000000000000000000000000000001")
	s, err := verification.NewSchedule("*/10 * * * *", "UTC")
	require.NoError(t, err)
	rec := application.ScheduleRecord{
		JobID:    "job-plans",
		ChainID:  chain.OptimismMainnet,
		Schedule: s,
		Strategy: verification.LatestN{N: 1},
		Metrics:  []verification.Metric{verification.MetricBalanceLatest},
		AddressPlans: []verification.AddressSamplingPlan{
			verification.KnownAddresses{Addresses: []chain.Address{a}},
			verification.TopNHolders{N: 20},
		},
		CreatedAt: time.Date(2026, 4, 22, 10, 0, 0, 0, time.UTC),
		Active:    true,
	}
	require.NoError(t, repo.Save(ctx, rec))

	records, err := repo.ListActive(ctx)
	require.NoError(t, err)
	require.Len(t, records, 1)
	require.Len(t, records[0].AddressPlans, 2)
	require.Equal(t, verification.KindKnownAddresses, records[0].AddressPlans[0].Kind())
	require.Equal(t, verification.KindTopNHolders, records[0].AddressPlans[1].Kind())
}

func TestIntegrationScheduleRepo_DefaultEmptyAddressPlans(t *testing.T) {
	resetDB(t)
	repo := persistence.NewScheduleRepo(testDB)
	ctx := context.Background()

	base := time.Date(2026, 4, 22, 10, 0, 0, 0, time.UTC)
	require.NoError(t, repo.Save(ctx, mkScheduleRecord(t, "job-no-plans", "* * * * *", base)))

	records, err := repo.ListActive(ctx)
	require.NoError(t, err)
	require.Len(t, records, 1)
	require.Nil(t, records[0].AddressPlans)
}

func TestIntegrationScheduleRepo_TimezoneRoundTrip(t *testing.T) {
	resetDB(t)
	repo := persistence.NewScheduleRepo(testDB)
	ctx := context.Background()

	s, err := verification.NewSchedule("0 12 * * *", "America/New_York")
	require.NoError(t, err)
	rec := application.ScheduleRecord{
		JobID:     "job-tz",
		ChainID:   chain.OptimismMainnet,
		Schedule:  s,
		Strategy:  verification.LatestN{N: 1},
		Metrics:   []verification.Metric{verification.MetricBlockHash},
		CreatedAt: time.Date(2026, 4, 22, 10, 0, 0, 0, time.UTC),
		Active:    true,
	}
	require.NoError(t, repo.Save(ctx, rec))

	records, err := repo.ListActive(ctx)
	require.NoError(t, err)
	require.Len(t, records, 1)
	require.Equal(t, "America/New_York", records[0].Schedule.Timezone().String())
}
