package application_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/seokheejang/chain-sync-watch/internal/application"
	"github.com/seokheejang/chain-sync-watch/internal/diff"
	"github.com/seokheejang/chain-sync-watch/internal/verification"
)

func TestDefaultToleranceResolver_CategoryDefaults(t *testing.T) {
	r := application.DefaultToleranceResolver{}
	cases := []struct {
		name   string
		metric verification.Metric
		want   diff.Tolerance
	}{
		{"block immutable", verification.MetricBlockHash, diff.ExactMatch{}},
		{"address at block", verification.MetricBalanceAtBlock, diff.ExactMatch{}},
		{
			"address latest",
			verification.MetricBalanceLatest,
			diff.AnchorWindowed{Inner: diff.ExactMatch{}, TolBack: 0, TolFwd: 64},
		},
		{"snapshot", verification.MetricTotalTxCount, diff.Observational{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, r.For(tc.metric))
		})
	}
}

func TestDefaultToleranceResolver_OverrideByMetricKey(t *testing.T) {
	r := application.DefaultToleranceResolver{
		Overrides: map[string]diff.Tolerance{
			"block.timestamp": diff.Observational{},
		},
	}
	// Timestamp is BlockImmutable by category but caller overrode it.
	require.Equal(t, diff.Observational{}, r.For(verification.MetricBlockTimestamp))
	// Non-overridden metric still takes the category default.
	require.Equal(t, diff.ExactMatch{}, r.For(verification.MetricBlockHash))
}
