package source_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/seokheejang/chain-sync-watch/internal/chain"
	"github.com/seokheejang/chain-sync-watch/internal/source"
)

// The zero value of BlockTag is "latest". This matters for backward
// compatibility: existing Query types gain an Anchor field of type
// BlockTag, and leaving it unset in a struct literal must behave like
// the old "latest" semantics — not like an unintended "unknown".
func TestBlockTag_ZeroValueIsLatest(t *testing.T) {
	var zero source.BlockTag
	require.Equal(t, source.BlockTagLatest, zero.Kind())
	require.Equal(t, "latest", zero.String())
	require.True(t, zero.IsZero())
}

func TestBlockTag_LatestSafeFinalizedString(t *testing.T) {
	cases := []struct {
		tag  source.BlockTag
		want string
	}{
		{source.NewBlockTagLatest(), "latest"},
		{source.NewBlockTagSafe(), "safe"},
		{source.NewBlockTagFinalized(), "finalized"},
	}
	for _, tc := range cases {
		t.Run(tc.want, func(t *testing.T) {
			require.Equal(t, tc.want, tc.tag.String())
		})
	}
}

// A numeric tag serialises to the RPC-canonical hex form so adapters
// can forward it straight into eth_getBalance(..., tag) calls.
func TestBlockTag_NumericSerialisesAsHex(t *testing.T) {
	tag := source.BlockTagAt(chain.NewBlockNumber(0x2a))
	require.Equal(t, source.BlockTagNumeric, tag.Kind())
	require.Equal(t, uint64(0x2a), tag.Number().Uint64())
	require.Equal(t, "0x2a", tag.String())
}

// Named-tag constructors must not carry a meaningful block number.
// Adapters that accidentally read Number() on a Latest/Safe/Finalized
// tag should see the zero height, not a stale value.
func TestBlockTag_NamedTagsHaveZeroNumber(t *testing.T) {
	require.Equal(t, uint64(0), source.NewBlockTagSafe().Number().Uint64())
	require.Equal(t, uint64(0), source.NewBlockTagFinalized().Number().Uint64())
}

// IsZero distinguishes an explicit latest from an unset field only when
// the caller uses a non-default constructor; semantically, both mean
// "latest", which is the whole point of the zero-value design.
func TestBlockTag_IsZero(t *testing.T) {
	require.True(t, source.BlockTag{}.IsZero())
	require.True(t, source.NewBlockTagLatest().IsZero())
	require.False(t, source.NewBlockTagFinalized().IsZero())
	require.False(t, source.BlockTagAt(chain.NewBlockNumber(1)).IsZero())
}
