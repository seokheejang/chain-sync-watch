package diff_test

import (
	"math/big"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/seokheejang/chain-sync-watch/internal/chain"
	"github.com/seokheejang/chain-sync-watch/internal/diff"
	"github.com/seokheejang/chain-sync-watch/internal/verification"
)

func TestExactMatch_Equal(t *testing.T) {
	ok, discard := diff.ExactMatch{}.Judge(
		diff.ValueSnapshot{Raw: "0xabc"},
		diff.ValueSnapshot{Raw: "0xabc"},
		verification.MetricBlockHash,
		diff.CompareContext{},
	)
	require.True(t, ok)
	require.False(t, discard)
}

func TestExactMatch_Unequal(t *testing.T) {
	ok, discard := diff.ExactMatch{}.Judge(
		diff.ValueSnapshot{Raw: "0xabc"},
		diff.ValueSnapshot{Raw: "0xabd"},
		verification.MetricBlockHash,
		diff.CompareContext{},
	)
	require.False(t, ok)
	require.False(t, discard)
}

func TestNumericTolerance_ExactMatchMode(t *testing.T) {
	// No tolerance configured -> exact integer equality.
	nt := diff.NumericTolerance{}
	ok, _ := nt.Judge(
		diff.ValueSnapshot{Raw: "1000000000"},
		diff.ValueSnapshot{Raw: "1000000000"},
		verification.MetricBalanceLatest,
		diff.CompareContext{},
	)
	require.True(t, ok)

	ok, _ = nt.Judge(
		diff.ValueSnapshot{Raw: "1000000000"},
		diff.ValueSnapshot{Raw: "1000000001"},
		verification.MetricBalanceLatest,
		diff.CompareContext{},
	)
	require.False(t, ok)
}

func TestNumericTolerance_AbsoluteMax(t *testing.T) {
	nt := diff.NumericTolerance{AbsoluteMax: big.NewInt(100)}
	cases := []struct {
		a, b string
		want bool
	}{
		{"1000", "1050", true},  // diff=50 <= 100
		{"1000", "1100", true},  // diff=100 <= 100 (boundary)
		{"1000", "1101", false}, // diff=101 > 100
		{"1000", "900", true},   // diff=100
	}
	for _, tc := range cases {
		ok, _ := nt.Judge(
			diff.ValueSnapshot{Raw: tc.a},
			diff.ValueSnapshot{Raw: tc.b},
			verification.MetricBalanceLatest,
			diff.CompareContext{},
		)
		require.Equalf(t, tc.want, ok, "%s vs %s", tc.a, tc.b)
	}
}

func TestNumericTolerance_RelativePPM(t *testing.T) {
	// 1000 PPM = 0.1% relative tolerance.
	nt := diff.NumericTolerance{RelativePPM: 1000}
	cases := []struct {
		a, b string
		want bool
	}{
		// Tolerance formula: |a-b|*1e6 <= max(|a|,|b|) * PPM.
		// With PPM=1000 this is "diff / max <= 0.1%".
		{"1000000", "1000500", true},  // 0.05% relative
		{"1000000", "1001000", true},  // 0.0999% relative, inside window
		{"1000000", "1002000", false}, // 0.2% relative, outside window
	}
	for _, tc := range cases {
		ok, _ := nt.Judge(
			diff.ValueSnapshot{Raw: tc.a},
			diff.ValueSnapshot{Raw: tc.b},
			verification.MetricBalanceLatest,
			diff.CompareContext{},
		)
		require.Equalf(t, tc.want, ok, "%s vs %s", tc.a, tc.b)
	}
}

func TestNumericTolerance_HexInput(t *testing.T) {
	nt := diff.NumericTolerance{AbsoluteMax: big.NewInt(16)}
	ok, _ := nt.Judge(
		diff.ValueSnapshot{Raw: "0x100"}, // 256
		diff.ValueSnapshot{Raw: "0x110"}, // 272  diff=16
		verification.MetricBalanceLatest,
		diff.CompareContext{},
	)
	require.True(t, ok)
}

func TestNumericTolerance_UnparseableInput(t *testing.T) {
	nt := diff.NumericTolerance{AbsoluteMax: big.NewInt(100)}
	ok, discard := nt.Judge(
		diff.ValueSnapshot{Raw: "not a number"},
		diff.ValueSnapshot{Raw: "12345"},
		verification.MetricBalanceLatest,
		diff.CompareContext{},
	)
	require.False(t, ok)
	require.False(t, discard)
}

func TestAnchorWindowed_BothInWindow(t *testing.T) {
	aw := diff.AnchorWindowed{
		Inner:   diff.ExactMatch{},
		TolBack: 10,
		TolFwd:  10,
	}
	reflA := chain.BlockNumber(995)
	reflB := chain.BlockNumber(1005)
	ok, discard := aw.Judge(
		diff.ValueSnapshot{Raw: "0xabc"},
		diff.ValueSnapshot{Raw: "0xabc"},
		verification.MetricBalanceLatest,
		diff.CompareContext{
			AnchorBlock: 1000,
			ReflectedA:  &reflA,
			ReflectedB:  &reflB,
		},
	)
	require.True(t, ok)
	require.False(t, discard)
}

func TestAnchorWindowed_OutsideWindow(t *testing.T) {
	aw := diff.AnchorWindowed{
		Inner:   diff.ExactMatch{},
		TolBack: 10,
		TolFwd:  10,
	}
	reflA := chain.BlockNumber(900) // 100 back, outside window
	reflB := chain.BlockNumber(1000)
	ok, discard := aw.Judge(
		diff.ValueSnapshot{Raw: "0xabc"},
		diff.ValueSnapshot{Raw: "0xabc"},
		verification.MetricBalanceLatest,
		diff.CompareContext{
			AnchorBlock: 1000,
			ReflectedA:  &reflA,
			ReflectedB:  &reflB,
		},
	)
	require.False(t, ok)
	require.True(t, discard)
}

func TestAnchorWindowed_NilReflectedIsPermissive(t *testing.T) {
	aw := diff.AnchorWindowed{
		Inner:   diff.ExactMatch{},
		TolBack: 10,
		TolFwd:  10,
	}
	// A nil reflected block means the Source did not expose the
	// meta; AnchorWindowed must not discard on that alone.
	ok, discard := aw.Judge(
		diff.ValueSnapshot{Raw: "0xabc"},
		diff.ValueSnapshot{Raw: "0xabc"},
		verification.MetricBalanceLatest,
		diff.CompareContext{
			AnchorBlock: 1000,
			ReflectedA:  nil,
			ReflectedB:  nil,
		},
	)
	require.True(t, ok)
	require.False(t, discard)
}

func TestAnchorWindowed_BoundaryCases(t *testing.T) {
	aw := diff.AnchorWindowed{
		Inner:   diff.ExactMatch{},
		TolBack: 10,
		TolFwd:  10,
	}
	// Exactly at the lower boundary is in-window.
	reflA := chain.BlockNumber(990)
	reflB := chain.BlockNumber(1010)
	ok, discard := aw.Judge(
		diff.ValueSnapshot{Raw: "x"},
		diff.ValueSnapshot{Raw: "x"},
		verification.MetricBalanceLatest,
		diff.CompareContext{
			AnchorBlock: 1000,
			ReflectedA:  &reflA,
			ReflectedB:  &reflB,
		},
	)
	require.True(t, ok)
	require.False(t, discard)

	// Off by one on the upper edge discards.
	reflB = chain.BlockNumber(1011)
	_, discard = aw.Judge(
		diff.ValueSnapshot{Raw: "x"},
		diff.ValueSnapshot{Raw: "x"},
		verification.MetricBalanceLatest,
		diff.CompareContext{
			AnchorBlock: 1000,
			ReflectedA:  &reflA,
			ReflectedB:  &reflB,
		},
	)
	require.True(t, discard)
}

func TestAnchorWindowed_AnchorUnderflow(t *testing.T) {
	// When anchor is small and TolBack is large, the lower bound
	// should clamp to 0 instead of underflowing.
	aw := diff.AnchorWindowed{
		Inner:   diff.ExactMatch{},
		TolBack: 1000,
		TolFwd:  10,
	}
	reflA := chain.BlockNumber(0)
	ok, discard := aw.Judge(
		diff.ValueSnapshot{Raw: "x"},
		diff.ValueSnapshot{Raw: "x"},
		verification.MetricBalanceLatest,
		diff.CompareContext{
			AnchorBlock: 5,
			ReflectedA:  &reflA,
		},
	)
	require.True(t, ok)
	require.False(t, discard)
}

func TestAnchorWindowed_NilInnerDefaultsToExactMatch(t *testing.T) {
	aw := diff.AnchorWindowed{TolBack: 10, TolFwd: 10}
	reflA := chain.BlockNumber(1000)
	reflB := chain.BlockNumber(1000)
	ok, _ := aw.Judge(
		diff.ValueSnapshot{Raw: "0xabc"},
		diff.ValueSnapshot{Raw: "0xabc"},
		verification.MetricBlockHash,
		diff.CompareContext{
			AnchorBlock: 1000,
			ReflectedA:  &reflA,
			ReflectedB:  &reflB,
		},
	)
	require.True(t, ok)
}

func TestObservational_AlwaysOk(t *testing.T) {
	ok, discard := diff.Observational{}.Judge(
		diff.ValueSnapshot{Raw: "1"},
		diff.ValueSnapshot{Raw: "999999"},
		verification.MetricTotalTxCount,
		diff.CompareContext{},
	)
	require.True(t, ok)
	require.False(t, discard)
}

func TestTolerance_InterfaceCompliance(t *testing.T) {
	var _ diff.Tolerance = diff.ExactMatch{}
	var _ diff.Tolerance = diff.NumericTolerance{}
	var _ diff.Tolerance = diff.AnchorWindowed{}
	var _ diff.Tolerance = diff.Observational{}
}
