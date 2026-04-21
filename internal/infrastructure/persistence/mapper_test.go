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
