package chain_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/seokheejang/chain-sync-watch/internal/chain"
)

func TestChainID_Uint64(t *testing.T) {
	require.Equal(t, uint64(1), chain.EthereumMainnet.Uint64())
	require.Equal(t, uint64(10), chain.OptimismMainnet.Uint64())
	require.Equal(t, uint64(999), chain.ChainID(999).Uint64())
}

func TestChainID_IsKnown(t *testing.T) {
	require.True(t, chain.EthereumMainnet.IsKnown())
	require.True(t, chain.OptimismMainnet.IsKnown())
	require.False(t, chain.ChainID(0).IsKnown())
	require.False(t, chain.ChainID(999).IsKnown())
}

func TestChainID_Slug(t *testing.T) {
	tests := []struct {
		id   chain.ChainID
		want string
	}{
		{chain.EthereumMainnet, "ethereum"},
		{chain.OptimismMainnet, "optimism"},
		{chain.ChainID(999), ""}, // unknown chain — empty signals "not mapped"
		{chain.ChainID(0), ""},
	}
	for _, tc := range tests {
		t.Run(fmt.Sprint(tc.id.Uint64()), func(t *testing.T) {
			require.Equal(t, tc.want, tc.id.Slug())
		})
	}
}

func TestChainID_DisplayName(t *testing.T) {
	tests := []struct {
		id   chain.ChainID
		want string
	}{
		{chain.EthereumMainnet, "Ethereum"},
		{chain.OptimismMainnet, "Optimism"},
		{chain.ChainID(999), ""},
		{chain.ChainID(0), ""},
	}
	for _, tc := range tests {
		t.Run(fmt.Sprint(tc.id.Uint64()), func(t *testing.T) {
			require.Equal(t, tc.want, tc.id.DisplayName())
		})
	}
}

func TestChainID_String(t *testing.T) {
	// Known chain: String returns the slug for human-readable logs.
	require.Equal(t, "optimism", chain.OptimismMainnet.String())
	require.Equal(t, "ethereum", chain.EthereumMainnet.String())

	// Unknown chain: fmt-friendly fallback so logs don't silently swallow the id.
	require.Equal(t, "chain:999", chain.ChainID(999).String())
	require.Equal(t, "chain:0", chain.ChainID(0).String())
}

// Stringer interface satisfaction — ensures fmt.Stringer is wired
// correctly at compile time.
func TestChainID_ImplementsStringer(t *testing.T) {
	var _ fmt.Stringer = chain.OptimismMainnet
}
