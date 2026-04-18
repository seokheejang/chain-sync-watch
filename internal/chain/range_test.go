package chain_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/seokheejang/chain-sync-watch/internal/chain"
)

func TestBlockRange_NewBlockRange_Valid(t *testing.T) {
	cases := []struct {
		name    string
		start   uint64
		end     uint64
		wantLen uint64
	}{
		{"single block", 100, 100, 1},
		{"small range", 100, 102, 3},
		{"from zero", 0, 10, 11},
		{"large range", 0, 150449305, 150449306},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, err := chain.NewBlockRange(chain.NewBlockNumber(tc.start), chain.NewBlockNumber(tc.end))
			require.NoError(t, err)
			require.Equal(t, chain.NewBlockNumber(tc.start), r.Start)
			require.Equal(t, chain.NewBlockNumber(tc.end), r.End)
			require.Equal(t, tc.wantLen, r.Len())
		})
	}
}

func TestBlockRange_NewBlockRange_Invalid(t *testing.T) {
	// start > end is rejected — allows the Contains / Len logic to
	// assume a well-formed range without per-call guards.
	_, err := chain.NewBlockRange(chain.NewBlockNumber(10), chain.NewBlockNumber(5))
	require.Error(t, err)
}

func TestBlockRange_Contains(t *testing.T) {
	r, err := chain.NewBlockRange(chain.NewBlockNumber(100), chain.NewBlockNumber(200))
	require.NoError(t, err)

	// Inclusive bounds.
	require.True(t, r.Contains(chain.NewBlockNumber(100)))
	require.True(t, r.Contains(chain.NewBlockNumber(150)))
	require.True(t, r.Contains(chain.NewBlockNumber(200)))

	// Outside.
	require.False(t, r.Contains(chain.NewBlockNumber(99)))
	require.False(t, r.Contains(chain.NewBlockNumber(201)))
	require.False(t, r.Contains(chain.NewBlockNumber(0)))
}

func TestBlockRange_Len_Inclusive(t *testing.T) {
	// [0, 0] has 1 block, [0, 1] has 2, etc. Sampling strategies depend
	// on this inclusive semantics.
	r, _ := chain.NewBlockRange(chain.NewBlockNumber(5), chain.NewBlockNumber(5))
	require.Equal(t, uint64(1), r.Len())

	r, _ = chain.NewBlockRange(chain.NewBlockNumber(5), chain.NewBlockNumber(15))
	require.Equal(t, uint64(11), r.Len())
}

func TestBlockRange_String(t *testing.T) {
	r, err := chain.NewBlockRange(chain.NewBlockNumber(100), chain.NewBlockNumber(200))
	require.NoError(t, err)
	require.Equal(t, "[100..200]", r.String())
}
