package verification_test

import (
	"sort"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/seokheejang/chain-sync-watch/internal/chain"
	"github.com/seokheejang/chain-sync-watch/internal/verification"
)

func TestSamplingStrategy_InterfaceCompliance(t *testing.T) {
	var _ verification.SamplingStrategy = verification.FixedList{}
	var _ verification.SamplingStrategy = verification.LatestN{}
	var _ verification.SamplingStrategy = verification.Random{}
	var _ verification.SamplingStrategy = verification.SparseSteps{}
}

func TestFixedList_Blocks(t *testing.T) {
	s := verification.FixedList{
		Numbers: []chain.BlockNumber{1, 5, 100, 1_000_000},
	}
	require.Equal(t, verification.KindFixedList, s.Kind())
	got := s.Blocks(verification.SamplingContext{TipBlock: 2_000_000})
	require.Equal(t, []chain.BlockNumber{1, 5, 100, 1_000_000}, got)
}

func TestFixedList_ReturnsDefensiveCopy(t *testing.T) {
	s := verification.FixedList{Numbers: []chain.BlockNumber{1, 2, 3}}
	got := s.Blocks(verification.SamplingContext{})
	got[0] = 999
	again := s.Blocks(verification.SamplingContext{})
	require.Equal(t, chain.BlockNumber(1), again[0])
}

func TestFixedList_Empty(t *testing.T) {
	require.Nil(t, verification.FixedList{}.Blocks(verification.SamplingContext{}))
}

func TestLatestN_Blocks(t *testing.T) {
	s := verification.LatestN{N: 3}
	require.Equal(t, verification.KindLatestN, s.Kind())
	got := s.Blocks(verification.SamplingContext{TipBlock: 10})
	require.Equal(t, []chain.BlockNumber{8, 9, 10}, got)
}

func TestLatestN_TipSmallerThanN(t *testing.T) {
	s := verification.LatestN{N: 5}
	got := s.Blocks(verification.SamplingContext{TipBlock: 2})
	require.Equal(t, []chain.BlockNumber{0, 1, 2}, got)
}

func TestLatestN_ZeroN(t *testing.T) {
	s := verification.LatestN{N: 0}
	require.Nil(t, s.Blocks(verification.SamplingContext{TipBlock: 10}))
}

func TestRandom_DeterministicWithSeed(t *testing.T) {
	r, err := chain.NewBlockRange(1, 100)
	require.NoError(t, err)
	s := verification.Random{Range: r, Count: 5, Seed: 42}

	require.Equal(t, verification.KindRandom, s.Kind())
	a := s.Blocks(verification.SamplingContext{})
	b := s.Blocks(verification.SamplingContext{})
	require.Equal(t, a, b, "same seed must reproduce the same blocks")
	require.Len(t, a, 5)

	// Output must be in ascending order and within the range.
	require.True(t, sort.SliceIsSorted(a, func(i, j int) bool { return a[i] < a[j] }))
	for _, n := range a {
		require.True(t, r.Contains(n))
	}

	// No duplicates.
	seen := map[chain.BlockNumber]bool{}
	for _, n := range a {
		require.Falsef(t, seen[n], "duplicate block %d", n)
		seen[n] = true
	}
}

func TestRandom_DifferentSeedDifferentResult(t *testing.T) {
	r, err := chain.NewBlockRange(1, 1000)
	require.NoError(t, err)
	a := verification.Random{Range: r, Count: 10, Seed: 1}.Blocks(verification.SamplingContext{})
	b := verification.Random{Range: r, Count: 10, Seed: 2}.Blocks(verification.SamplingContext{})
	require.NotEqual(t, a, b)
}

func TestRandom_CountExceedsRange(t *testing.T) {
	r, err := chain.NewBlockRange(5, 10)
	require.NoError(t, err)
	s := verification.Random{Range: r, Count: 100, Seed: 1}
	got := s.Blocks(verification.SamplingContext{})
	require.Equal(t, []chain.BlockNumber{5, 6, 7, 8, 9, 10}, got)
}

func TestRandom_ZeroCount(t *testing.T) {
	r, err := chain.NewBlockRange(1, 100)
	require.NoError(t, err)
	require.Nil(t, verification.Random{Range: r, Count: 0, Seed: 1}.Blocks(verification.SamplingContext{}))
}

func TestSparseSteps_Blocks(t *testing.T) {
	r, err := chain.NewBlockRange(0, 10)
	require.NoError(t, err)
	s := verification.SparseSteps{Range: r, Step: 3}
	require.Equal(t, verification.KindSparseSteps, s.Kind())
	got := s.Blocks(verification.SamplingContext{})
	require.Equal(t, []chain.BlockNumber{0, 3, 6, 9}, got)
}

func TestSparseSteps_StepLargerThanRange(t *testing.T) {
	r, err := chain.NewBlockRange(5, 10)
	require.NoError(t, err)
	s := verification.SparseSteps{Range: r, Step: 100}
	got := s.Blocks(verification.SamplingContext{})
	require.Equal(t, []chain.BlockNumber{5}, got)
}

func TestSparseSteps_ZeroStep(t *testing.T) {
	r, err := chain.NewBlockRange(0, 10)
	require.NoError(t, err)
	s := verification.SparseSteps{Range: r, Step: 0}
	require.Nil(t, s.Blocks(verification.SamplingContext{}))
}

func TestSparseSteps_SingleBlockRange(t *testing.T) {
	r, err := chain.NewBlockRange(42, 42)
	require.NoError(t, err)
	s := verification.SparseSteps{Range: r, Step: 1}
	got := s.Blocks(verification.SamplingContext{})
	require.Equal(t, []chain.BlockNumber{42}, got)
}
