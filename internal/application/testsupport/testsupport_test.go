package testsupport_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/seokheejang/chain-sync-watch/internal/application/testsupport"
	"github.com/seokheejang/chain-sync-watch/internal/chain"
	"github.com/seokheejang/chain-sync-watch/internal/source"
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
