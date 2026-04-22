package verification_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/seokheejang/chain-sync-watch/internal/chain"
	"github.com/seokheejang/chain-sync-watch/internal/verification"
)

func TestTokenSamplingPlan_InterfaceCompliance(t *testing.T) {
	var _ verification.TokenSamplingPlan = verification.KnownTokens{}
}

func TestTokenSamplingPlan_Kind(t *testing.T) {
	require.Equal(t, verification.KindKnownTokens, verification.KnownTokens{}.Kind())
}

func TestKnownTokens_Resolve(t *testing.T) {
	a := chain.MustAddress("0x0000000000000000000000000000000000000aaa")
	b := chain.MustAddress("0x0000000000000000000000000000000000000bbb")
	k := verification.KnownTokens{Tokens: []chain.Address{a, b}}
	require.Equal(t, []chain.Address{a, b}, k.Resolve())
}

func TestKnownTokens_Resolve_Empty(t *testing.T) {
	require.Nil(t, verification.KnownTokens{}.Resolve())
}

func TestKnownTokens_Resolve_ReturnsDefensiveCopy(t *testing.T) {
	a := chain.MustAddress("0x0000000000000000000000000000000000000aaa")
	b := chain.MustAddress("0x0000000000000000000000000000000000000bbb")
	k := verification.KnownTokens{Tokens: []chain.Address{a, b}}

	got := k.Resolve()
	got[0] = chain.Address{}

	again := k.Resolve()
	require.Equal(t, a, again[0])
}
