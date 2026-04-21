package queue_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/require"

	"github.com/seokheejang/chain-sync-watch/internal/application"
	"github.com/seokheejang/chain-sync-watch/internal/chain"
	"github.com/seokheejang/chain-sync-watch/internal/infrastructure/queue"
	"github.com/seokheejang/chain-sync-watch/internal/verification"
)

func startMiniRedis(t *testing.T) (asynq.RedisConnOpt, *miniredis.Miniredis) {
	t.Helper()
	mr, err := miniredis.Run()
	require.NoError(t, err)
	t.Cleanup(mr.Close)
	return asynq.RedisClientOpt{Addr: mr.Addr()}, mr
}

func TestDispatcher_EnqueueRunExecution(t *testing.T) {
	opt, _ := startMiniRedis(t)
	d := queue.NewDispatcher(opt)
	t.Cleanup(func() { _ = d.Close() })

	require.NoError(t, d.EnqueueRunExecution(context.Background(), "rid-1"))

	// Inspect the default queue.
	insp := asynq.NewInspector(opt)
	t.Cleanup(func() { _ = insp.Close() })

	tasks, err := insp.ListPendingTasks(queue.QueueDefault)
	require.NoError(t, err)
	require.Len(t, tasks, 1)
	require.Equal(t, queue.TaskTypeExecuteRun, tasks[0].Type)

	payload, err := queue.UnmarshalExecuteRunPayload(tasks[0].Payload)
	require.NoError(t, err)
	require.Equal(t, "rid-1", payload.RunID)
}

func TestDispatcher_ScheduleRecurring_AddsToConfigProvider(t *testing.T) {
	opt, _ := startMiniRedis(t)
	d := queue.NewDispatcher(opt)
	t.Cleanup(func() { _ = d.Close() })

	sched, err := verification.NewSchedule("0 */6 * * *", "UTC")
	require.NoError(t, err)
	payload := application.SchedulePayload{
		ChainID:  chain.OptimismMainnet,
		Metrics:  []verification.Metric{verification.MetricBlockHash},
		Strategy: verification.LatestN{N: 5},
	}

	id, err := d.ScheduleRecurring(context.Background(), sched, payload)
	require.NoError(t, err)
	require.NotEmpty(t, id)

	cfgs, err := d.ConfigProvider().GetConfigs()
	require.NoError(t, err)
	require.Len(t, cfgs, 1)
	require.Equal(t, "0 */6 * * *", cfgs[0].Cronspec)
	require.Equal(t, queue.TaskTypeScheduledRun, cfgs[0].Task.Type())

	// Payload should round-trip into a ScheduledRunPayload.
	var got queue.ScheduledRunPayload
	require.NoError(t, json.Unmarshal(cfgs[0].Task.Payload(), &got))
	require.Equal(t, uint64(chain.OptimismMainnet), got.ChainID)
	require.Equal(t, verification.KindLatestN, got.StrategyKind)
	require.Equal(t, []string{"block.hash"}, got.MetricKeys)
	require.Equal(t, "0 */6 * * *", got.CronExpr)
}

func TestDispatcher_ScheduleRecurring_RejectsEmptySchedule(t *testing.T) {
	opt, _ := startMiniRedis(t)
	d := queue.NewDispatcher(opt)
	t.Cleanup(func() { _ = d.Close() })

	_, err := d.ScheduleRecurring(context.Background(), verification.Schedule{}, application.SchedulePayload{})
	require.Error(t, err)
}

func TestDispatcher_CancelScheduled(t *testing.T) {
	opt, _ := startMiniRedis(t)
	d := queue.NewDispatcher(opt)
	t.Cleanup(func() { _ = d.Close() })

	sched, _ := verification.NewSchedule("* * * * *", "UTC")
	id, err := d.ScheduleRecurring(context.Background(), sched, application.SchedulePayload{
		ChainID:  chain.OptimismMainnet,
		Metrics:  []verification.Metric{verification.MetricBlockHash},
		Strategy: verification.LatestN{N: 1},
	})
	require.NoError(t, err)

	require.NoError(t, d.CancelScheduled(context.Background(), id))

	cfgs, err := d.ConfigProvider().GetConfigs()
	require.NoError(t, err)
	require.Empty(t, cfgs)

	// Second cancel must fail — the id is no longer registered.
	require.Error(t, d.CancelScheduled(context.Background(), id))
}

func TestDispatcher_WithOptions_OverridesQueue(t *testing.T) {
	opt, _ := startMiniRedis(t)
	d := queue.NewDispatcher(opt).WithOptions(queue.EnqueueOptions{Queue: "custom"})
	t.Cleanup(func() { _ = d.Close() })

	require.NoError(t, d.EnqueueRunExecution(context.Background(), "rid-x"))

	insp := asynq.NewInspector(opt)
	t.Cleanup(func() { _ = insp.Close() })

	tasks, err := insp.ListPendingTasks("custom")
	require.NoError(t, err)
	require.Len(t, tasks, 1)
}
