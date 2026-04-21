package queue_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	"github.com/seokheejang/chain-sync-watch/internal/application"
	"github.com/seokheejang/chain-sync-watch/internal/infrastructure/queue"
	"github.com/seokheejang/chain-sync-watch/internal/source"
)

func budgetFixture(t *testing.T, policies map[source.SourceID]queue.BudgetPolicy) (*queue.RedisBudget, *miniredis.Miniredis) {
	t.Helper()
	mr, err := miniredis.Run()
	require.NoError(t, err)
	t.Cleanup(mr.Close)

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close() })

	return queue.NewRedisBudget(client, queue.BudgetConfig{Policies: policies}), mr
}

func TestRedisBudget_ReserveWithinLimit(t *testing.T) {
	b, _ := budgetFixture(t, map[source.SourceID]queue.BudgetPolicy{
		"routescan": {Window: 1 * time.Second, Limit: 5},
	})
	ctx := context.Background()

	require.NoError(t, b.Reserve(ctx, "routescan", 3))
	remaining, err := b.Remaining(ctx, "routescan")
	require.NoError(t, err)
	require.Equal(t, 2, remaining)
}

func TestRedisBudget_ReserveExceedsLimit(t *testing.T) {
	b, _ := budgetFixture(t, map[source.SourceID]queue.BudgetPolicy{
		"rpc": {Window: 1 * time.Second, Limit: 3},
	})
	ctx := context.Background()

	require.NoError(t, b.Reserve(ctx, "rpc", 3))
	err := b.Reserve(ctx, "rpc", 1)
	require.ErrorIs(t, err, application.ErrBudgetExhausted)

	// Remaining stayed at zero — the failed reserve did not leak
	// units.
	remaining, err := b.Remaining(ctx, "rpc")
	require.NoError(t, err)
	require.Equal(t, 0, remaining)
}

func TestRedisBudget_Refund(t *testing.T) {
	b, _ := budgetFixture(t, map[source.SourceID]queue.BudgetPolicy{
		"blockscout": {Window: 1 * time.Second, Limit: 5},
	})
	ctx := context.Background()

	require.NoError(t, b.Reserve(ctx, "blockscout", 4))
	require.NoError(t, b.Refund(ctx, "blockscout", 2))

	remaining, err := b.Remaining(ctx, "blockscout")
	require.NoError(t, err)
	require.Equal(t, 3, remaining)

	// Refund below zero stays at zero (key is deleted).
	require.NoError(t, b.Refund(ctx, "blockscout", 99))
	remaining, err = b.Remaining(ctx, "blockscout")
	require.NoError(t, err)
	require.Equal(t, 5, remaining)
}

func TestRedisBudget_UnconfiguredSourceIsUnlimited(t *testing.T) {
	b, _ := budgetFixture(t, map[source.SourceID]queue.BudgetPolicy{
		"rpc": {Window: 1 * time.Second, Limit: 1},
	})
	ctx := context.Background()

	// Any n against an unknown source succeeds without touching
	// Redis.
	for i := 0; i < 100; i++ {
		require.NoError(t, b.Reserve(ctx, "unknown", 1))
	}
	remaining, err := b.Remaining(ctx, "unknown")
	require.NoError(t, err)
	require.Equal(t, -1, remaining, "-1 signals 'no policy configured'")
}

func TestRedisBudget_WindowExpiry(t *testing.T) {
	b, mr := budgetFixture(t, map[source.SourceID]queue.BudgetPolicy{
		"rpc": {Window: 500 * time.Millisecond, Limit: 2},
	})
	ctx := context.Background()

	require.NoError(t, b.Reserve(ctx, "rpc", 2))
	require.ErrorIs(t, b.Reserve(ctx, "rpc", 1), application.ErrBudgetExhausted)

	// miniredis requires explicit time fast-forward; advance past
	// both the window + grace TTL.
	mr.FastForward(2 * time.Second)

	// Fresh window — reservation should succeed again.
	require.NoError(t, b.Reserve(ctx, "rpc", 2))
}

func TestRedisBudget_ZeroOrNegativeNIsNoop(t *testing.T) {
	b, _ := budgetFixture(t, map[source.SourceID]queue.BudgetPolicy{
		"rpc": {Window: 1 * time.Second, Limit: 1},
	})
	ctx := context.Background()
	require.NoError(t, b.Reserve(ctx, "rpc", 0))
	require.NoError(t, b.Reserve(ctx, "rpc", -1))
	require.NoError(t, b.Refund(ctx, "rpc", 0))

	remaining, err := b.Remaining(ctx, "rpc")
	require.NoError(t, err)
	require.Equal(t, 1, remaining)
}

func TestRedisBudget_RejectsEmptySourceID(t *testing.T) {
	b, _ := budgetFixture(t, map[source.SourceID]queue.BudgetPolicy{})
	ctx := context.Background()
	require.Error(t, b.Reserve(ctx, "", 1))
	require.Error(t, b.Refund(ctx, "", 1))
}

func TestRedisBudget_ConcurrentReserves(t *testing.T) {
	b, _ := budgetFixture(t, map[source.SourceID]queue.BudgetPolicy{
		"rpc": {Window: 5 * time.Second, Limit: 10},
	})
	ctx := context.Background()

	// 20 goroutines racing to reserve 1 each; exactly 10 should
	// succeed.
	var wg sync.WaitGroup
	var successes, exhaustions int
	var mu sync.Mutex
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := b.Reserve(ctx, "rpc", 1)
			mu.Lock()
			defer mu.Unlock()
			switch err {
			case nil:
				successes++
			case application.ErrBudgetExhausted:
				exhaustions++
			default:
				t.Errorf("unexpected error: %v", err)
			}
		}()
	}
	wg.Wait()
	require.Equal(t, 10, successes)
	require.Equal(t, 10, exhaustions)
}

func TestRedisBudget_PolicyWithZeroLimitTreatedAsUnlimited(t *testing.T) {
	// A BudgetPolicy{} zero value is treated as "not configured"
	// to protect against accidental "zero limit = deny all" typos.
	b, _ := budgetFixture(t, map[source.SourceID]queue.BudgetPolicy{
		"rpc": {},
	})
	ctx := context.Background()
	for i := 0; i < 10; i++ {
		require.NoError(t, b.Reserve(ctx, "rpc", 1))
	}
}
