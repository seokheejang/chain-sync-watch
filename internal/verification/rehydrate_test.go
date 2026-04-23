package verification_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/seokheejang/chain-sync-watch/internal/chain"
	"github.com/seokheejang/chain-sync-watch/internal/verification"
)

func TestRehydrate_PreservesTerminalStateAndTimestamps(t *testing.T) {
	created := time.Date(2026, 4, 21, 0, 0, 0, 0, time.UTC)
	started := created.Add(time.Second)
	finished := created.Add(2 * time.Second)

	r, err := verification.Rehydrate(
		"rid",
		chain.OptimismMainnet,
		verification.LatestN{N: 3},
		[]verification.Metric{verification.MetricBlockHash},
		verification.ManualTrigger{User: "alice"},
		verification.StatusCompleted,
		created,
		&started,
		&finished,
		"",
		nil,
		nil,
	)
	require.NoError(t, err)
	require.Equal(t, verification.StatusCompleted, r.Status())
	require.Equal(t, created, r.CreatedAt())
	require.Equal(t, started, *r.StartedAt())
	require.Equal(t, finished, *r.FinishedAt())
	require.True(t, r.Status().IsTerminal())
}

func TestRehydrate_RejectsStructuralErrors(t *testing.T) {
	base := time.Now()
	goodStrategy := verification.LatestN{N: 1}
	goodMetrics := []verification.Metric{verification.MetricBlockHash}
	goodTrigger := verification.ManualTrigger{User: "u"}

	cases := []struct {
		name     string
		id       verification.RunID
		cid      chain.ChainID
		strategy verification.SamplingStrategy
		metrics  []verification.Metric
		trigger  verification.Trigger
		status   verification.Status
	}{
		{"empty id", "", chain.OptimismMainnet, goodStrategy, goodMetrics, goodTrigger, verification.StatusPending},
		{"zero chain", "rid", 0, goodStrategy, goodMetrics, goodTrigger, verification.StatusPending},
		{"nil strategy", "rid", chain.OptimismMainnet, nil, goodMetrics, goodTrigger, verification.StatusPending},
		{"empty metrics", "rid", chain.OptimismMainnet, goodStrategy, nil, goodTrigger, verification.StatusPending},
		{"nil trigger", "rid", chain.OptimismMainnet, goodStrategy, goodMetrics, nil, verification.StatusPending},
		{"empty status", "rid", chain.OptimismMainnet, goodStrategy, goodMetrics, goodTrigger, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := verification.Rehydrate(tc.id, tc.cid, tc.strategy, tc.metrics, tc.trigger, tc.status, base, nil, nil, "", nil, nil)
			require.Error(t, err)
		})
	}
}

func TestRehydrate_CopiesTimestampsDefensively(t *testing.T) {
	created := time.Date(2026, 4, 21, 0, 0, 0, 0, time.UTC)
	started := created.Add(time.Second)
	finished := created.Add(2 * time.Second)

	r, err := verification.Rehydrate(
		"rid",
		chain.OptimismMainnet,
		verification.LatestN{N: 1},
		[]verification.Metric{verification.MetricBlockHash},
		verification.ManualTrigger{User: "u"},
		verification.StatusCompleted,
		created,
		&started,
		&finished,
		"",
		nil,
		nil,
	)
	require.NoError(t, err)
	// Mutating caller's variables must not reach the aggregate.
	started = started.Add(time.Hour)
	finished = finished.Add(time.Hour)
	require.Equal(t, created.Add(time.Second), *r.StartedAt())
	require.Equal(t, created.Add(2*time.Second), *r.FinishedAt())
}
