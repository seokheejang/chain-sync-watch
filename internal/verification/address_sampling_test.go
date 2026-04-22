package verification_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/seokheejang/chain-sync-watch/internal/chain"
	"github.com/seokheejang/chain-sync-watch/internal/verification"
)

func TestAddressSamplingPlan_InterfaceCompliance(t *testing.T) {
	var _ verification.AddressSamplingPlan = verification.KnownAddresses{}
	var _ verification.AddressSamplingPlan = verification.TopNHolders{}
	var _ verification.AddressSamplingPlan = verification.RandomAddresses{}
	var _ verification.AddressSamplingPlan = verification.RecentlyActive{}
}

func TestAddressSamplingPlan_Kind(t *testing.T) {
	cases := []struct {
		plan verification.AddressSamplingPlan
		want string
	}{
		{verification.KnownAddresses{}, verification.KindKnownAddresses},
		{verification.TopNHolders{N: 10}, verification.KindTopNHolders},
		{verification.RandomAddresses{Count: 5, Seed: 1}, verification.KindRandomAddresses},
		{verification.RecentlyActive{RecentBlocks: 100, Count: 5, Seed: 1}, verification.KindRecentlyActive},
	}
	for _, c := range cases {
		require.Equal(t, c.want, c.plan.Kind())
	}
}

func TestKnownAddresses_Resolve(t *testing.T) {
	a := chain.MustAddress("0x0000000000000000000000000000000000000001")
	b := chain.MustAddress("0x0000000000000000000000000000000000000002")
	k := verification.KnownAddresses{Addresses: []chain.Address{a, b}}
	got := k.Resolve()
	require.Equal(t, []chain.Address{a, b}, got)
}

func TestKnownAddresses_Resolve_Empty(t *testing.T) {
	require.Nil(t, verification.KnownAddresses{}.Resolve())
}

func TestKnownAddresses_Resolve_ReturnsDefensiveCopy(t *testing.T) {
	a := chain.MustAddress("0x0000000000000000000000000000000000000001")
	b := chain.MustAddress("0x0000000000000000000000000000000000000002")
	k := verification.KnownAddresses{Addresses: []chain.Address{a, b}}

	got := k.Resolve()
	zero := chain.Address{}
	got[0] = zero

	again := k.Resolve()
	require.Equal(t, a, again[0])
}
