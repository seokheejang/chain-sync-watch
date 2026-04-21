package verification_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/seokheejang/chain-sync-watch/internal/source"
	"github.com/seokheejang/chain-sync-watch/internal/verification"
)

func TestMetricCategory_AllCategories(t *testing.T) {
	want := []verification.MetricCategory{
		verification.CatBlockImmutable,
		verification.CatAddressLatest,
		verification.CatAddressAtBlock,
		verification.CatSnapshot,
	}
	require.Equal(t, want, verification.AllCategories())
}

func TestMetric_CatalogHasEveryCategory(t *testing.T) {
	byCategory := map[verification.MetricCategory]int{}
	for _, m := range verification.AllMetrics() {
		byCategory[m.Category]++
	}
	for _, c := range verification.AllCategories() {
		require.Positivef(t, byCategory[c],
			"category %q has no metrics in the built-in catalog", c)
	}
}

func TestMetric_CapabilityReferencesAreKnown(t *testing.T) {
	known := map[source.Capability]bool{}
	for _, c := range source.AllCapabilities() {
		known[c] = true
	}
	for _, m := range verification.AllMetrics() {
		require.Truef(t, known[m.Capability],
			"metric %q references unknown capability %q", m.Key, m.Capability)
	}
}

func TestMetric_KeysAreUnique(t *testing.T) {
	seen := map[string]bool{}
	for _, m := range verification.AllMetrics() {
		require.Falsef(t, seen[m.Key], "duplicate metric key %q", m.Key)
		seen[m.Key] = true
	}
}

func TestMetric_CategoryAssignments(t *testing.T) {
	cases := []struct {
		metric verification.Metric
		want   verification.MetricCategory
	}{
		{verification.MetricBlockHash, verification.CatBlockImmutable},
		{verification.MetricBlockTimestamp, verification.CatBlockImmutable},
		{verification.MetricBalanceLatest, verification.CatAddressLatest},
		{verification.MetricNonceLatest, verification.CatAddressLatest},
		{verification.MetricBalanceAtBlock, verification.CatAddressAtBlock},
		{verification.MetricNonceAtBlock, verification.CatAddressAtBlock},
		{verification.MetricTotalAddressCount, verification.CatSnapshot},
		{verification.MetricTotalTxCount, verification.CatSnapshot},
		{verification.MetricERC20TokenCount, verification.CatSnapshot},
	}
	for _, tc := range cases {
		require.Equalf(t, tc.want, tc.metric.Category,
			"metric %q expected category %q", tc.metric.Key, tc.want)
	}
}

func TestMetric_UserDefinedConstruction(t *testing.T) {
	// Users may define their own Metric values against an existing
	// Capability; no registry required, Metric is a plain value type.
	custom := verification.Metric{
		Key:        "custom.my_field",
		Category:   verification.CatSnapshot,
		Capability: source.CapTotalTxCount,
	}
	require.Equal(t, "custom.my_field", custom.Key)
	require.Equal(t, verification.CatSnapshot, custom.Category)
	require.Equal(t, source.CapTotalTxCount, custom.Capability)
}
