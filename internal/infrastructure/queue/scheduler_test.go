package queue_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/seokheejang/chain-sync-watch/internal/application"
	"github.com/seokheejang/chain-sync-watch/internal/chain"
	"github.com/seokheejang/chain-sync-watch/internal/infrastructure/queue"
	"github.com/seokheejang/chain-sync-watch/internal/verification"
)

func TestScheduler_NilProviderRejected(t *testing.T) {
	opt, _ := startMiniRedis(t)
	_, err := queue.NewScheduler(opt, nil, queue.SchedulerOptions{})
	require.Error(t, err)
}

func TestScheduler_StartShutdown(t *testing.T) {
	opt, _ := startMiniRedis(t)
	disp := queue.NewDispatcher(opt)
	t.Cleanup(func() { _ = disp.Close() })

	sched, err := verification.NewSchedule("*/5 * * * *", "UTC")
	require.NoError(t, err)
	_, err = disp.ScheduleRecurring(context.Background(), sched, application.SchedulePayload{
		ChainID:  chain.OptimismMainnet,
		Metrics:  []verification.Metric{verification.MetricBlockHash},
		Strategy: verification.LatestN{N: 1},
	})
	require.NoError(t, err)

	s, err := queue.NewScheduler(opt, disp.ConfigProvider(), queue.SchedulerOptions{})
	require.NoError(t, err)
	require.NoError(t, s.Start())
	s.Shutdown()
}

func TestScheduler_CompilesAgainstAsynqProvider(t *testing.T) {
	// Type-assertion compile-check: the store returned by
	// Dispatcher.ConfigProvider() must satisfy the interface asynq
	// expects from a PeriodicTaskManager.
	opt, _ := startMiniRedis(t)
	disp := queue.NewDispatcher(opt)
	t.Cleanup(func() { _ = disp.Close() })

	provider := disp.ConfigProvider()
	_, err := provider.GetConfigs()
	require.NoError(t, err)
}
