package verification_test

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/seokheejang/chain-sync-watch/internal/chain"
	"github.com/seokheejang/chain-sync-watch/internal/verification"
)

func TestStatus_IsTerminal(t *testing.T) {
	require.False(t, verification.StatusPending.IsTerminal())
	require.False(t, verification.StatusRunning.IsTerminal())
	require.True(t, verification.StatusCompleted.IsTerminal())
	require.True(t, verification.StatusFailed.IsTerminal())
	require.True(t, verification.StatusCancelled.IsTerminal())
}

func TestNewRunID_HexForm(t *testing.T) {
	id, err := verification.NewRunID()
	require.NoError(t, err)
	require.Len(t, string(id), 32)

	id2, err := verification.NewRunID()
	require.NoError(t, err)
	require.NotEqual(t, id, id2, "two generated ids must not collide")
}

func TestNewRun_Success(t *testing.T) {
	now := time.Date(2026, 4, 21, 0, 0, 0, 0, time.UTC)
	r, err := verification.NewRun(
		"rid-1",
		chain.OptimismMainnet,
		verification.LatestN{N: 3},
		[]verification.Metric{verification.MetricBlockHash, verification.MetricBlockTimestamp},
		verification.ManualTrigger{User: "alice"},
		now,
	)
	require.NoError(t, err)
	require.Equal(t, verification.RunID("rid-1"), r.ID())
	require.Equal(t, chain.OptimismMainnet, r.ChainID())
	require.Equal(t, verification.StatusPending, r.Status())
	require.Equal(t, now, r.CreatedAt())
	require.Nil(t, r.StartedAt())
	require.Nil(t, r.FinishedAt())
	require.Equal(t, "", r.ErrorMessage())
	require.Len(t, r.Metrics(), 2)
	require.Equal(t, verification.ManualTrigger{User: "alice"}, r.Trigger())
	require.Nil(t, r.AddressPlans())
}

func TestNewRun_WithAddressPlans(t *testing.T) {
	now := time.Date(2026, 4, 21, 0, 0, 0, 0, time.UTC)
	addr := chain.MustAddress("0x0000000000000000000000000000000000000001")
	plans := []verification.AddressSamplingPlan{
		verification.KnownAddresses{Addresses: []chain.Address{addr}},
		verification.TopNHolders{N: 50},
	}
	r, err := verification.NewRun(
		"rid-2",
		chain.OptimismMainnet,
		verification.LatestN{N: 1},
		[]verification.Metric{verification.MetricBalanceLatest},
		verification.ManualTrigger{User: "alice"},
		now,
		plans...,
	)
	require.NoError(t, err)

	got := r.AddressPlans()
	require.Len(t, got, 2)
	require.Equal(t, verification.KindKnownAddresses, got[0].Kind())
	require.Equal(t, verification.KindTopNHolders, got[1].Kind())
}

func TestNewRun_RejectsNilAddressPlan(t *testing.T) {
	now := time.Date(2026, 4, 21, 0, 0, 0, 0, time.UTC)
	_, err := verification.NewRun(
		"rid-3",
		chain.OptimismMainnet,
		verification.LatestN{N: 1},
		[]verification.Metric{verification.MetricBalanceLatest},
		verification.ManualTrigger{User: "alice"},
		now,
		nil,
	)
	require.Error(t, err)
}

func TestRun_AddressPlans_ReturnsDefensiveCopy(t *testing.T) {
	now := time.Date(2026, 4, 21, 0, 0, 0, 0, time.UTC)
	r, err := verification.NewRun(
		"rid-4",
		chain.OptimismMainnet,
		verification.LatestN{N: 1},
		[]verification.Metric{verification.MetricBalanceLatest},
		verification.ManualTrigger{User: "alice"},
		now,
		verification.TopNHolders{N: 50},
	)
	require.NoError(t, err)

	got := r.AddressPlans()
	got[0] = verification.KnownAddresses{}

	again := r.AddressPlans()
	require.Equal(t, verification.KindTopNHolders, again[0].Kind())
}

func TestNewRun_ValidationErrors(t *testing.T) {
	now := time.Now()
	goodStrategy := verification.LatestN{N: 10}
	goodMetrics := []verification.Metric{verification.MetricBlockHash}
	goodTrigger := verification.ManualTrigger{User: "u"}

	cases := []struct {
		name     string
		id       verification.RunID
		cid      chain.ChainID
		strategy verification.SamplingStrategy
		metrics  []verification.Metric
		trigger  verification.Trigger
	}{
		{"empty id", "", chain.OptimismMainnet, goodStrategy, goodMetrics, goodTrigger},
		{"zero chain id", "rid", 0, goodStrategy, goodMetrics, goodTrigger},
		{"nil strategy", "rid", chain.OptimismMainnet, nil, goodMetrics, goodTrigger},
		{"empty metrics", "rid", chain.OptimismMainnet, goodStrategy, nil, goodTrigger},
		{"nil trigger", "rid", chain.OptimismMainnet, goodStrategy, goodMetrics, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := verification.NewRun(tc.id, tc.cid, tc.strategy, tc.metrics, tc.trigger, now)
			require.Error(t, err)
		})
	}
}

func TestRun_MetricsDefensiveCopy(t *testing.T) {
	r, _ := newTestRun(t)
	m := r.Metrics()
	m[0] = verification.MetricBlockGasUsed
	require.Equal(t, verification.MetricBlockHash, r.Metrics()[0])
}

func TestRun_InputMetricsNotAliased(t *testing.T) {
	// Mutating the caller's slice after construction must not
	// reach into the aggregate.
	input := []verification.Metric{verification.MetricBlockHash}
	r, err := verification.NewRun(
		"rid",
		chain.OptimismMainnet,
		verification.LatestN{N: 1},
		input,
		verification.ManualTrigger{User: "u"},
		time.Now(),
	)
	require.NoError(t, err)
	input[0] = verification.MetricBlockGasUsed
	require.Equal(t, verification.MetricBlockHash, r.Metrics()[0])
}

func TestRun_HappyPath(t *testing.T) {
	r, start := newTestRun(t)

	require.Equal(t, verification.StatusPending, r.Status())
	t1 := start.Add(time.Second)
	require.NoError(t, r.Start(t1))
	require.Equal(t, verification.StatusRunning, r.Status())
	require.NotNil(t, r.StartedAt())
	require.Equal(t, t1, *r.StartedAt())

	t2 := start.Add(2 * time.Second)
	require.NoError(t, r.Complete(t2))
	require.Equal(t, verification.StatusCompleted, r.Status())
	require.NotNil(t, r.FinishedAt())
	require.Equal(t, t2, *r.FinishedAt())
}

func TestRun_CannotStartFromRunning(t *testing.T) {
	r, start := newTestRun(t)
	require.NoError(t, r.Start(start))
	require.Error(t, r.Start(start))
}

func TestRun_CannotCompleteFromPending(t *testing.T) {
	r, _ := newTestRun(t)
	require.Error(t, r.Complete(time.Now()))
}

func TestRun_TerminalBlocksTransitions(t *testing.T) {
	terminalCases := []struct {
		name    string
		reach   func(t *testing.T, r *verification.Run, now time.Time)
		wantSev verification.Status
	}{
		{
			name: "completed",
			reach: func(t *testing.T, r *verification.Run, now time.Time) {
				require.NoError(t, r.Start(now))
				require.NoError(t, r.Complete(now))
			},
			wantSev: verification.StatusCompleted,
		},
		{
			name: "failed",
			reach: func(t *testing.T, r *verification.Run, now time.Time) {
				require.NoError(t, r.Start(now))
				require.NoError(t, r.Fail(now, errors.New("boom")))
			},
			wantSev: verification.StatusFailed,
		},
		{
			name: "cancelled",
			reach: func(t *testing.T, r *verification.Run, now time.Time) {
				require.NoError(t, r.Cancel(now))
			},
			wantSev: verification.StatusCancelled,
		},
	}
	for _, tc := range terminalCases {
		t.Run(tc.name, func(t *testing.T) {
			r, now := newTestRun(t)
			tc.reach(t, r, now)
			require.Equal(t, tc.wantSev, r.Status())
			require.True(t, r.Status().IsTerminal())
			require.Error(t, r.Start(now))
			require.Error(t, r.Complete(now))
			require.Error(t, r.Fail(now, errors.New("late")))
			require.Error(t, r.Cancel(now))
		})
	}
}

func TestRun_FailRecordsMessage(t *testing.T) {
	r, s := newTestRun(t)
	require.NoError(t, r.Start(s))
	require.NoError(t, r.Fail(s, errors.New("kaboom")))
	require.Equal(t, verification.StatusFailed, r.Status())
	require.Equal(t, "kaboom", r.ErrorMessage())
	require.NotNil(t, r.FinishedAt())
}

func TestRun_FailWithNilErrorLeavesMessageEmpty(t *testing.T) {
	r, s := newTestRun(t)
	require.NoError(t, r.Start(s))
	require.NoError(t, r.Fail(s, nil))
	require.Equal(t, verification.StatusFailed, r.Status())
	require.Equal(t, "", r.ErrorMessage())
}

func TestRun_CancelFromPending(t *testing.T) {
	r, s := newTestRun(t)
	require.NoError(t, r.Cancel(s))
	require.Equal(t, verification.StatusCancelled, r.Status())
	require.NotNil(t, r.FinishedAt())
}

func TestRun_FailFromPending(t *testing.T) {
	r, s := newTestRun(t)
	require.NoError(t, r.Fail(s, errors.New("pre-run")))
	require.Equal(t, verification.StatusFailed, r.Status())
	require.Equal(t, "pre-run", r.ErrorMessage())
}

func TestRun_StartedAtIsDefensiveCopy(t *testing.T) {
	r, s := newTestRun(t)
	require.NoError(t, r.Start(s))
	got := r.StartedAt()
	*got = s.Add(99 * time.Hour)
	require.Equal(t, s, *r.StartedAt())
}

func newTestRun(t *testing.T) (*verification.Run, time.Time) {
	t.Helper()
	now := time.Date(2026, 4, 21, 0, 0, 0, 0, time.UTC)
	r, err := verification.NewRun(
		"rid",
		chain.OptimismMainnet,
		verification.LatestN{N: 3},
		[]verification.Metric{verification.MetricBlockHash},
		verification.ManualTrigger{User: "u"},
		now,
	)
	require.NoError(t, err)
	return r, now
}
