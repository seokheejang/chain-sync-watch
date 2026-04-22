package persistence

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/seokheejang/chain-sync-watch/internal/application"
	"github.com/seokheejang/chain-sync-watch/internal/chain"
	"github.com/seokheejang/chain-sync-watch/internal/diff"
	"github.com/seokheejang/chain-sync-watch/internal/source"
	"github.com/seokheejang/chain-sync-watch/internal/verification"
)

func TestRunRoundTrip_ManualTrigger_FixedList(t *testing.T) {
	now := time.Date(2026, 4, 21, 0, 0, 0, 0, time.UTC)
	orig, err := verification.NewRun(
		"rid-1",
		chain.OptimismMainnet,
		verification.FixedList{Numbers: []chain.BlockNumber{100, 200, 300}},
		[]verification.Metric{verification.MetricBlockHash, verification.MetricBlockTimestamp},
		verification.ManualTrigger{User: "alice"},
		now,
	)
	require.NoError(t, err)

	m, err := toRunModel(orig)
	require.NoError(t, err)

	got, err := toRun(m)
	require.NoError(t, err)

	require.Equal(t, orig.ID(), got.ID())
	require.Equal(t, orig.ChainID(), got.ChainID())
	require.Equal(t, orig.Status(), got.Status())
	require.Equal(t, verification.KindFixedList, got.Strategy().Kind())
	require.Equal(t, verification.ManualTrigger{User: "alice"}, got.Trigger())
	require.Equal(t, orig.CreatedAt(), got.CreatedAt())
	require.Equal(t, len(orig.Metrics()), len(got.Metrics()))
}

func TestRunRoundTrip_ScheduledTrigger_LatestN(t *testing.T) {
	now := time.Date(2026, 4, 21, 0, 0, 0, 0, time.UTC)
	r, err := verification.NewRun(
		"rid",
		chain.OptimismMainnet,
		verification.LatestN{N: 7},
		[]verification.Metric{verification.MetricBlockHash},
		verification.ScheduledTrigger{CronExpr: "0 */6 * * *"},
		now,
	)
	require.NoError(t, err)

	m, err := toRunModel(r)
	require.NoError(t, err)
	require.Equal(t, "scheduled", m.TriggerType)
	require.Equal(t, "latest_n", m.StrategyKind)

	got, err := toRun(m)
	require.NoError(t, err)
	require.Equal(t, verification.ScheduledTrigger{CronExpr: "0 */6 * * *"}, got.Trigger())
	strategy, ok := got.Strategy().(verification.LatestN)
	require.True(t, ok)
	require.Equal(t, uint(7), strategy.N)
}

func TestRunRoundTrip_RealtimeTrigger_Random(t *testing.T) {
	now := time.Date(2026, 4, 21, 0, 0, 0, 0, time.UTC)
	br, err := chain.NewBlockRange(10, 100)
	require.NoError(t, err)
	r, err := verification.NewRun(
		"rid",
		chain.OptimismMainnet,
		verification.Random{Range: br, Count: 5, Seed: 42},
		[]verification.Metric{verification.MetricBlockHash},
		verification.RealtimeTrigger{BlockNumber: 777},
		now,
	)
	require.NoError(t, err)

	m, err := toRunModel(r)
	require.NoError(t, err)

	got, err := toRun(m)
	require.NoError(t, err)
	rt, ok := got.Trigger().(verification.RealtimeTrigger)
	require.True(t, ok)
	require.Equal(t, chain.BlockNumber(777), rt.BlockNumber)
	rnd, ok := got.Strategy().(verification.Random)
	require.True(t, ok)
	require.Equal(t, int64(42), rnd.Seed)
	require.Equal(t, chain.BlockNumber(10), rnd.Range.Start)
	require.Equal(t, chain.BlockNumber(100), rnd.Range.End)
}

func TestRunRoundTrip_SparseSteps(t *testing.T) {
	now := time.Date(2026, 4, 21, 0, 0, 0, 0, time.UTC)
	br, err := chain.NewBlockRange(0, 1000)
	require.NoError(t, err)
	r, err := verification.NewRun(
		"rid",
		chain.OptimismMainnet,
		verification.SparseSteps{Range: br, Step: 100},
		[]verification.Metric{verification.MetricBlockHash},
		verification.ManualTrigger{User: "u"},
		now,
	)
	require.NoError(t, err)
	m, err := toRunModel(r)
	require.NoError(t, err)
	got, err := toRun(m)
	require.NoError(t, err)
	ss, ok := got.Strategy().(verification.SparseSteps)
	require.True(t, ok)
	require.Equal(t, uint64(100), ss.Step)
}

func TestRunRoundTrip_PreservesTerminalState(t *testing.T) {
	created := time.Date(2026, 4, 21, 0, 0, 0, 0, time.UTC)
	r, err := verification.NewRun(
		"rid",
		chain.OptimismMainnet,
		verification.LatestN{N: 1},
		[]verification.Metric{verification.MetricBlockHash},
		verification.ManualTrigger{User: "u"},
		created,
	)
	require.NoError(t, err)
	require.NoError(t, r.Start(created.Add(time.Second)))
	require.NoError(t, r.Complete(created.Add(2*time.Second)))

	m, err := toRunModel(r)
	require.NoError(t, err)
	require.Equal(t, string(verification.StatusCompleted), m.Status)
	require.NotNil(t, m.StartedAt)
	require.NotNil(t, m.FinishedAt)

	got, err := toRun(m)
	require.NoError(t, err)
	require.Equal(t, verification.StatusCompleted, got.Status())
	require.Equal(t, created.Add(time.Second), *got.StartedAt())
	require.Equal(t, created.Add(2*time.Second), *got.FinishedAt())
}

func TestRunRoundTrip_AddressPlans_AllFour(t *testing.T) {
	now := time.Date(2026, 4, 21, 0, 0, 0, 0, time.UTC)
	a := chain.MustAddress("0x0000000000000000000000000000000000000001")
	b := chain.MustAddress("0x0000000000000000000000000000000000000002")
	plans := []verification.AddressSamplingPlan{
		verification.KnownAddresses{Addresses: []chain.Address{a, b}},
		verification.TopNHolders{N: 50},
		verification.RandomAddresses{Count: 10, Seed: 42},
		verification.RecentlyActive{RecentBlocks: 500, Count: 20, Seed: 7},
	}
	r, err := verification.NewRun(
		"rid-plans",
		chain.OptimismMainnet,
		verification.LatestN{N: 1},
		[]verification.Metric{verification.MetricBalanceLatest},
		verification.ManualTrigger{User: "u"},
		now,
		plans...,
	)
	require.NoError(t, err)

	m, err := toRunModel(r)
	require.NoError(t, err)
	require.NotNil(t, m.AddressPlans)
	require.NotEqual(t, "[]", string(m.AddressPlans))

	got, err := toRun(m)
	require.NoError(t, err)

	gotPlans := got.AddressPlans()
	require.Len(t, gotPlans, 4)

	k, ok := gotPlans[0].(verification.KnownAddresses)
	require.True(t, ok)
	require.Equal(t, []chain.Address{a, b}, k.Addresses)

	tn, ok := gotPlans[1].(verification.TopNHolders)
	require.True(t, ok)
	require.Equal(t, uint(50), tn.N)

	rn, ok := gotPlans[2].(verification.RandomAddresses)
	require.True(t, ok)
	require.Equal(t, uint(10), rn.Count)
	require.Equal(t, int64(42), rn.Seed)

	ra, ok := gotPlans[3].(verification.RecentlyActive)
	require.True(t, ok)
	require.Equal(t, uint(500), ra.RecentBlocks)
	require.Equal(t, uint(20), ra.Count)
	require.Equal(t, int64(7), ra.Seed)
}

func TestRunRoundTrip_NoAddressPlans_EmptyArray(t *testing.T) {
	now := time.Date(2026, 4, 21, 0, 0, 0, 0, time.UTC)
	r, err := verification.NewRun(
		"rid-no-plans",
		chain.OptimismMainnet,
		verification.LatestN{N: 1},
		[]verification.Metric{verification.MetricBlockHash},
		verification.ManualTrigger{User: "u"},
		now,
	)
	require.NoError(t, err)

	m, err := toRunModel(r)
	require.NoError(t, err)
	require.Equal(t, "[]", string(m.AddressPlans))

	got, err := toRun(m)
	require.NoError(t, err)
	require.Nil(t, got.AddressPlans())
}

func TestRunRoundTrip_AddressPlans_NullTolerated(t *testing.T) {
	// Rows written before migration 002 may arrive with NULL / nil bytes
	// in address_plans; toRun must treat that the same as an empty list
	// so older rows stay rehydrateable.
	m := runModel{
		ID:           "rid",
		ChainID:      10,
		Status:       "pending",
		TriggerType:  "manual",
		TriggerData:  []byte(`{"user":"u"}`),
		StrategyKind: "latest_n",
		StrategyData: []byte(`{"n":1}`),
		AddressPlans: nil,
		Metrics:      []string{verification.MetricBlockHash.Key},
		CreatedAt:    time.Date(2026, 4, 21, 0, 0, 0, 0, time.UTC),
	}
	got, err := toRun(m)
	require.NoError(t, err)
	require.Nil(t, got.AddressPlans())
}

func TestRunRoundTrip_RejectsUnknownMetricKey(t *testing.T) {
	m := runModel{
		ID:           "rid",
		ChainID:      10,
		Status:       "pending",
		TriggerType:  "manual",
		TriggerData:  []byte(`{"user":"u"}`),
		StrategyKind: "latest_n",
		StrategyData: []byte(`{"n":1}`),
		Metrics:      []string{"custom.unknown"},
		CreatedAt:    time.Now(),
	}
	_, err := toRun(m)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unknown metric key")
}

func TestScheduleRoundTrip_BasicFields(t *testing.T) {
	schedule, err := verification.NewSchedule("0 */6 * * *", "America/New_York")
	require.NoError(t, err)
	created := time.Date(2026, 4, 22, 10, 0, 0, 0, time.UTC)

	orig := application.ScheduleRecord{
		JobID:     "job-1",
		ChainID:   chain.OptimismMainnet,
		Schedule:  schedule,
		Strategy:  verification.LatestN{N: 5},
		Metrics:   []verification.Metric{verification.MetricBlockHash, verification.MetricBlockTimestamp},
		CreatedAt: created,
		Active:    true,
	}

	m, err := toScheduleModel(orig)
	require.NoError(t, err)
	require.Equal(t, "job-1", m.JobID)
	require.Equal(t, uint64(chain.OptimismMainnet), m.ChainID)
	require.Equal(t, "0 */6 * * *", m.CronExpr)
	require.Equal(t, "America/New_York", m.Timezone)
	require.Equal(t, verification.KindLatestN, m.StrategyKind)
	require.Equal(t, []string{"block.hash", "block.timestamp"}, []string(m.Metrics))
	require.True(t, m.Active)

	got, err := toScheduleRecord(m)
	require.NoError(t, err)
	require.Equal(t, orig.JobID, got.JobID)
	require.Equal(t, orig.ChainID, got.ChainID)
	require.Equal(t, orig.Schedule.CronExpr(), got.Schedule.CronExpr())
	require.Equal(t, "America/New_York", got.Schedule.Timezone().String())
	require.Equal(t, orig.Active, got.Active)

	strategy, ok := got.Strategy.(verification.LatestN)
	require.True(t, ok)
	require.Equal(t, uint(5), strategy.N)

	require.Len(t, got.Metrics, 2)
	require.Equal(t, verification.MetricBlockHash.Key, got.Metrics[0].Key)
	require.Equal(t, verification.MetricBlockTimestamp.Key, got.Metrics[1].Key)
}

func TestScheduleRoundTrip_DeactivatedPreserved(t *testing.T) {
	schedule, err := verification.NewSchedule("* * * * *", "UTC")
	require.NoError(t, err)
	orig := application.ScheduleRecord{
		JobID:     "job-cancelled",
		ChainID:   chain.OptimismMainnet,
		Schedule:  schedule,
		Strategy:  verification.LatestN{N: 1},
		Metrics:   []verification.Metric{verification.MetricBlockHash},
		CreatedAt: time.Now().UTC(),
		Active:    false,
	}

	m, err := toScheduleModel(orig)
	require.NoError(t, err)
	require.False(t, m.Active)

	got, err := toScheduleRecord(m)
	require.NoError(t, err)
	require.False(t, got.Active)
}

func TestScheduleRoundTrip_AddressPlans(t *testing.T) {
	schedule, err := verification.NewSchedule("* * * * *", "UTC")
	require.NoError(t, err)
	a := chain.MustAddress("0x0000000000000000000000000000000000000001")
	b := chain.MustAddress("0x0000000000000000000000000000000000000002")
	plans := []verification.AddressSamplingPlan{
		verification.KnownAddresses{Addresses: []chain.Address{a, b}},
		verification.RandomAddresses{Count: 10, Seed: 99},
	}
	orig := application.ScheduleRecord{
		JobID:        "job-plans",
		ChainID:      chain.OptimismMainnet,
		Schedule:     schedule,
		Strategy:     verification.LatestN{N: 3},
		Metrics:      []verification.Metric{verification.MetricBalanceLatest},
		AddressPlans: plans,
		CreatedAt:    time.Now().UTC(),
		Active:       true,
	}

	m, err := toScheduleModel(orig)
	require.NoError(t, err)
	require.NotEqual(t, "[]", string(m.AddressPlans))

	got, err := toScheduleRecord(m)
	require.NoError(t, err)
	require.Len(t, got.AddressPlans, 2)
	k, ok := got.AddressPlans[0].(verification.KnownAddresses)
	require.True(t, ok)
	require.Equal(t, []chain.Address{a, b}, k.Addresses)

	rnd, ok := got.AddressPlans[1].(verification.RandomAddresses)
	require.True(t, ok)
	require.Equal(t, int64(99), rnd.Seed)
}

func TestScheduleRoundTrip_NoPlansEmptyArray(t *testing.T) {
	schedule, err := verification.NewSchedule("* * * * *", "UTC")
	require.NoError(t, err)
	orig := application.ScheduleRecord{
		JobID:     "job-none",
		ChainID:   chain.OptimismMainnet,
		Schedule:  schedule,
		Strategy:  verification.LatestN{N: 1},
		Metrics:   []verification.Metric{verification.MetricBlockHash},
		CreatedAt: time.Now().UTC(),
		Active:    true,
	}
	m, err := toScheduleModel(orig)
	require.NoError(t, err)
	require.Equal(t, "[]", string(m.AddressPlans))

	got, err := toScheduleRecord(m)
	require.NoError(t, err)
	require.Nil(t, got.AddressPlans)
}

func TestScheduleRoundTrip_RejectsUnknownStrategyKind(t *testing.T) {
	m := scheduleModel{
		JobID:        "j",
		ChainID:      10,
		CronExpr:     "* * * * *",
		Timezone:     "UTC",
		StrategyKind: "not_a_real_kind",
		StrategyData: []byte(`{}`),
		Metrics:      []string{verification.MetricBlockHash.Key},
		Active:       true,
		CreatedAt:    time.Now().UTC(),
	}
	_, err := toScheduleRecord(m)
	require.Error(t, err)
}

func TestDiffRoundTrip_BlockSubject(t *testing.T) {
	now := time.Date(2026, 4, 21, 0, 0, 0, 0, time.UTC)
	rb := chain.BlockNumber(990)
	values := map[source.SourceID]diff.ValueSnapshot{
		"rpc":        {Raw: "0xabc", FetchedAt: now},
		"blockscout": {Raw: "0xabd", FetchedAt: now, ReflectedBlock: &rb},
	}
	d, err := diff.NewDiscrepancy(
		"rid",
		verification.MetricBlockHash,
		100,
		diff.Subject{Type: diff.SubjectBlock},
		values,
		now,
	)
	require.NoError(t, err)
	j := diff.Judgement{
		Severity:       diff.SevCritical,
		TrustedSources: []source.SourceID{"rpc"},
		Reasoning:      "blockscout diverged",
	}

	m, err := toDiffModel(&d, j, application.SaveDiffMeta{
		Tier:        source.TierA,
		AnchorBlock: 990,
	})
	require.NoError(t, err)
	require.Equal(t, "rid", m.RunID)
	require.Equal(t, "block.hash", m.MetricKey)
	require.Equal(t, int64(100), m.BlockNumber)
	require.Nil(t, m.SubjectAddr)
	require.NotNil(t, m.Tier)
	require.Equal(t, int16(source.TierA), *m.Tier)
	require.NotNil(t, m.AnchorBlock)
	require.Equal(t, int64(990), *m.AnchorBlock)

	// Simulate DB assigning an id.
	m.ID = 42
	rec, err := toDiffRecord(m)
	require.NoError(t, err)
	require.Equal(t, "42", string(rec.ID))
	require.Equal(t, verification.MetricBlockHash, rec.Discrepancy.Metric)
	require.Equal(t, chain.BlockNumber(100), rec.Discrepancy.Block)
	require.Equal(t, diff.SevCritical, rec.Judgement.Severity)
	require.Equal(t, []source.SourceID{"rpc"}, rec.Judgement.TrustedSources)
	require.Equal(t, source.TierA, rec.Tier)
	require.Equal(t, "0xabc", rec.Discrepancy.Values["rpc"].Raw)
	require.NotNil(t, rec.Discrepancy.Values["blockscout"].ReflectedBlock)
	require.Equal(t, chain.BlockNumber(990), *rec.Discrepancy.Values["blockscout"].ReflectedBlock)
}

func TestDiffRoundTrip_AddressSubject(t *testing.T) {
	now := time.Date(2026, 4, 21, 0, 0, 0, 0, time.UTC)
	addr, err := chain.NewAddress("0x" + string(fortyHex('a')))
	require.NoError(t, err)
	values := map[source.SourceID]diff.ValueSnapshot{
		"rpc":        {Raw: "100", FetchedAt: now},
		"blockscout": {Raw: "200", FetchedAt: now},
	}
	d, err := diff.NewDiscrepancy(
		"rid",
		verification.MetricBalanceLatest,
		100,
		diff.Subject{Type: diff.SubjectAddress, Address: &addr},
		values,
		now,
	)
	require.NoError(t, err)

	m, err := toDiffModel(&d, diff.Judgement{Severity: diff.SevWarning}, application.SaveDiffMeta{})
	require.NoError(t, err)
	require.Len(t, m.SubjectAddr, 20)

	m.ID = 1
	rec, err := toDiffRecord(m)
	require.NoError(t, err)
	require.NotNil(t, rec.Discrepancy.Subject.Address)
	require.Equal(t, addr, *rec.Discrepancy.Subject.Address)
}

func fortyHex(b byte) []byte {
	out := make([]byte, 40)
	for i := range out {
		out[i] = b
	}
	return out
}
