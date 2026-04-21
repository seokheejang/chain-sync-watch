package diff_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/seokheejang/chain-sync-watch/internal/chain"
	"github.com/seokheejang/chain-sync-watch/internal/diff"
	"github.com/seokheejang/chain-sync-watch/internal/source"
	"github.com/seokheejang/chain-sync-watch/internal/verification"
)

func TestValueSnapshot_IsZero(t *testing.T) {
	var v diff.ValueSnapshot
	require.True(t, v.IsZero())

	v.Raw = "0x1"
	require.False(t, v.IsZero())
}

func TestNewDiscrepancy_Success(t *testing.T) {
	addr, err := chain.NewAddress("0x" + string(hexA(40)))
	require.NoError(t, err)

	values := map[source.SourceID]diff.ValueSnapshot{
		"rpc":        {Raw: "0xabc", FetchedAt: time.Now()},
		"blockscout": {Raw: "0xabd", FetchedAt: time.Now()},
	}
	d, err := diff.NewDiscrepancy(
		"rid-1",
		verification.MetricBalanceLatest,
		chain.BlockNumber(100),
		diff.Subject{Type: diff.SubjectAddress, Address: &addr},
		values,
		time.Date(2026, 4, 21, 0, 0, 0, 0, time.UTC),
	)
	require.NoError(t, err)
	require.Equal(t, verification.RunID("rid-1"), d.RunID)
	require.Equal(t, chain.BlockNumber(100), d.Block)
	require.Equal(t, verification.MetricBalanceLatest, d.Metric)
	require.Equal(t, diff.SubjectAddress, d.Subject.Type)
	require.Equal(t, &addr, d.Subject.Address)
	require.Len(t, d.Values, 2)
}

func TestNewDiscrepancy_DefensiveCopy(t *testing.T) {
	values := map[source.SourceID]diff.ValueSnapshot{
		"rpc":        {Raw: "0xabc"},
		"blockscout": {Raw: "0xabd"},
	}
	d, err := diff.NewDiscrepancy(
		"rid",
		verification.MetricBlockHash,
		100,
		diff.Subject{Type: diff.SubjectBlock},
		values,
		time.Now(),
	)
	require.NoError(t, err)

	values["rpc"] = diff.ValueSnapshot{Raw: "MUTATED"}
	require.Equal(t, "0xabc", d.Values["rpc"].Raw)
}

func TestNewDiscrepancy_ValidationErrors(t *testing.T) {
	good := map[source.SourceID]diff.ValueSnapshot{
		"rpc":        {Raw: "0x1"},
		"blockscout": {Raw: "0x2"},
	}
	cases := []struct {
		name   string
		runID  verification.RunID
		sub    diff.Subject
		values map[source.SourceID]diff.ValueSnapshot
	}{
		{
			"empty run id",
			"",
			diff.Subject{Type: diff.SubjectBlock},
			good,
		},
		{
			"empty subject type",
			"rid",
			diff.Subject{},
			good,
		},
		{
			"single source value",
			"rid",
			diff.Subject{Type: diff.SubjectBlock},
			map[source.SourceID]diff.ValueSnapshot{"rpc": {Raw: "0x1"}},
		},
		{
			"empty source id",
			"rid",
			diff.Subject{Type: diff.SubjectBlock},
			map[source.SourceID]diff.ValueSnapshot{
				"":    {Raw: "0x1"},
				"rpc": {Raw: "0x2"},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := diff.NewDiscrepancy(
				tc.runID,
				verification.MetricBlockHash,
				0,
				tc.sub,
				tc.values,
				time.Now(),
			)
			require.Error(t, err)
		})
	}
}

func TestSubject_AddressNilForBlockAndChain(t *testing.T) {
	// Subject is just a value type; we don't reject nil Address
	// for block/chain, but tests document the intent: consumers
	// treat nil as "not applicable".
	subBlock := diff.Subject{Type: diff.SubjectBlock}
	subChain := diff.Subject{Type: diff.SubjectChain}
	require.Nil(t, subBlock.Address)
	require.Nil(t, subChain.Address)
}

func hexA(n int) []byte {
	out := make([]byte, n)
	for i := range out {
		out[i] = 'a'
	}
	return out
}
