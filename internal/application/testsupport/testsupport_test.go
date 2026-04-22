package testsupport_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/seokheejang/chain-sync-watch/internal/application/testsupport"
	"github.com/seokheejang/chain-sync-watch/internal/chain"
	"github.com/seokheejang/chain-sync-watch/internal/source"
	"github.com/seokheejang/chain-sync-watch/internal/verification"
)

func TestFakeClock_AdvanceAndSet(t *testing.T) {
	base := time.Date(2026, 4, 21, 0, 0, 0, 0, time.UTC)
	c := testsupport.NewFakeClock(base)
	require.Equal(t, base, c.Now())

	c.Advance(time.Hour)
	require.Equal(t, base.Add(time.Hour), c.Now())

	c.Set(base)
	require.Equal(t, base, c.Now())
}

func TestFakeChainHead_MissingConfigReturnsError(t *testing.T) {
	h := testsupport.NewFakeChainHead()
	_, err := h.Tip(context.Background(), chain.OptimismMainnet)
	require.Error(t, err)

	h.SetTip(chain.OptimismMainnet, 100)
	got, err := h.Tip(context.Background(), chain.OptimismMainnet)
	require.NoError(t, err)
	require.Equal(t, chain.BlockNumber(100), got)
}

func TestFakeRateLimitBudget_ReserveExhaustion(t *testing.T) {
	b := testsupport.NewFakeRateLimitBudget(map[source.SourceID]int{"rpc": 3})
	require.NoError(t, b.Reserve(context.Background(), "rpc", 2))
	require.Equal(t, 1, b.Remaining("rpc"))

	err := b.Reserve(context.Background(), "rpc", 2)
	require.Error(t, err)

	require.NoError(t, b.Refund(context.Background(), "rpc", 1))
	require.Equal(t, 2, b.Remaining("rpc"))
}

func TestFakeAddressSampler_SampleReturnsConfiguredList(t *testing.T) {
	a := chain.MustAddress("0x0000000000000000000000000000000000000001")
	b := chain.MustAddress("0x0000000000000000000000000000000000000002")
	s := testsupport.NewFakeAddressSampler()
	s.Results[verification.KindTopNHolders] = []chain.Address{a, b}

	got, err := s.Sample(context.Background(), chain.OptimismMainnet, verification.TopNHolders{N: 10}, 100)
	require.NoError(t, err)
	require.Equal(t, []chain.Address{a, b}, got)

	require.Len(t, s.Calls, 1)
	require.Equal(t, verification.KindTopNHolders, s.Calls[0].Kind)
	require.Equal(t, chain.BlockNumber(100), s.Calls[0].At)
}

func TestFakeAddressSampler_MissingKindReturnsNil(t *testing.T) {
	s := testsupport.NewFakeAddressSampler()
	got, err := s.Sample(context.Background(), chain.OptimismMainnet, verification.KnownAddresses{}, 0)
	require.NoError(t, err)
	require.Nil(t, got)
}

func TestFakeAddressSampler_ErrorInjection(t *testing.T) {
	sentinel := errors.New("boom")
	s := testsupport.NewFakeAddressSampler()
	s.Err = sentinel

	_, err := s.Sample(context.Background(), chain.OptimismMainnet, verification.RandomAddresses{Count: 5, Seed: 1}, 0)
	require.ErrorIs(t, err, sentinel)
}

func TestFakeAddressSampler_ReturnsDefensiveCopy(t *testing.T) {
	a := chain.MustAddress("0x0000000000000000000000000000000000000001")
	s := testsupport.NewFakeAddressSampler()
	s.Results[verification.KindKnownAddresses] = []chain.Address{a}

	got, err := s.Sample(context.Background(), chain.OptimismMainnet, verification.KnownAddresses{}, 0)
	require.NoError(t, err)
	got[0] = chain.Address{}

	again, err := s.Sample(context.Background(), chain.OptimismMainnet, verification.KnownAddresses{}, 0)
	require.NoError(t, err)
	require.Equal(t, a, again[0])
}
