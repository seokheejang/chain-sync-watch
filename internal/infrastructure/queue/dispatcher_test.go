package queue_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/require"

	"github.com/seokheejang/chain-sync-watch/internal/application"
	"github.com/seokheejang/chain-sync-watch/internal/application/testsupport"
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

func newDispatcherWithFakeRepo(t *testing.T) (*queue.Dispatcher, *testsupport.FakeScheduleRepo) {
	t.Helper()
	opt, _ := startMiniRedis(t)
	repo := testsupport.NewFakeScheduleRepo()
	d := queue.NewDispatcher(opt, repo)
	t.Cleanup(func() { _ = d.Close() })
	return d, repo
}

func TestDispatcher_EnqueueRunExecution(t *testing.T) {
	opt, _ := startMiniRedis(t)
	d := queue.NewDispatcher(opt, testsupport.NewFakeScheduleRepo())
	t.Cleanup(func() { _ = d.Close() })

	require.NoError(t, d.EnqueueRunExecution(context.Background(), "rid-1"))

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

func TestDispatcher_ScheduleRecurring_PersistsRecord(t *testing.T) {
	d, repo := newDispatcherWithFakeRepo(t)

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

	records, err := repo.ListActive(context.Background())
	require.NoError(t, err)
	require.Len(t, records, 1)
	require.Equal(t, id, records[0].JobID)
	require.Equal(t, chain.OptimismMainnet, records[0].ChainID)
	require.Equal(t, "0 */6 * * *", records[0].Schedule.CronExpr())
	require.True(t, records[0].Active)
}

func TestDispatcher_ConfigProvider_RendersActiveSchedules(t *testing.T) {
	d, _ := newDispatcherWithFakeRepo(t)

	sched, err := verification.NewSchedule("0 */6 * * *", "UTC")
	require.NoError(t, err)
	payload := application.SchedulePayload{
		ChainID:  chain.OptimismMainnet,
		Metrics:  []verification.Metric{verification.MetricBlockHash},
		Strategy: verification.LatestN{N: 5},
	}
	_, err = d.ScheduleRecurring(context.Background(), sched, payload)
	require.NoError(t, err)

	cfgs, err := d.ConfigProvider().GetConfigs()
	require.NoError(t, err)
	require.Len(t, cfgs, 1)
	require.Equal(t, "0 */6 * * *", cfgs[0].Cronspec)
	require.Equal(t, queue.TaskTypeScheduledRun, cfgs[0].Task.Type())

	var got queue.ScheduledRunPayload
	require.NoError(t, json.Unmarshal(cfgs[0].Task.Payload(), &got))
	require.Equal(t, uint64(chain.OptimismMainnet), got.ChainID)
	require.Equal(t, verification.KindLatestN, got.StrategyKind)
	require.Equal(t, []string{"block.hash"}, got.MetricKeys)
	require.Equal(t, "0 */6 * * *", got.CronExpr)
}

func TestDispatcher_ConfigProvider_SkipsDeactivated(t *testing.T) {
	d, repo := newDispatcherWithFakeRepo(t)

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
	require.Empty(t, cfgs, "deactivated schedules must not show up in the provider view")

	require.Equal(t, 1, repo.Count(), "cancelled row stays for audit, just Active=false")
}

func TestDispatcher_CancelScheduled_UnknownIDIsNoOp(t *testing.T) {
	d, _ := newDispatcherWithFakeRepo(t)

	// Defensive double-cancel (e.g. after a crash-recovery loop) must
	// be safe. The previous in-memory store errored on unknown ids;
	// the durable contract treats it as a no-op.
	require.NoError(t, d.CancelScheduled(context.Background(), "no-such-job"))
}

func TestDispatcher_ScheduleRecurring_RejectsEmptySchedule(t *testing.T) {
	d, _ := newDispatcherWithFakeRepo(t)
	_, err := d.ScheduleRecurring(context.Background(), verification.Schedule{}, application.SchedulePayload{})
	require.Error(t, err)
}

func TestDispatcher_ScheduleRecurring_RejectsNilRepository(t *testing.T) {
	opt, _ := startMiniRedis(t)
	d := queue.NewDispatcher(opt, nil)
	t.Cleanup(func() { _ = d.Close() })

	sched, _ := verification.NewSchedule("* * * * *", "UTC")
	_, err := d.ScheduleRecurring(context.Background(), sched, application.SchedulePayload{
		ChainID:  chain.OptimismMainnet,
		Metrics:  []verification.Metric{verification.MetricBlockHash},
		Strategy: verification.LatestN{N: 1},
	})
	require.Error(t, err)
}

func TestDispatcher_WithOptions_OverridesQueue(t *testing.T) {
	opt, _ := startMiniRedis(t)
	d := queue.NewDispatcher(opt, testsupport.NewFakeScheduleRepo()).
		WithOptions(queue.EnqueueOptions{Queue: "custom"})
	t.Cleanup(func() { _ = d.Close() })

	require.NoError(t, d.EnqueueRunExecution(context.Background(), "rid-x"))

	insp := asynq.NewInspector(opt)
	t.Cleanup(func() { _ = insp.Close() })

	tasks, err := insp.ListPendingTasks("custom")
	require.NoError(t, err)
	require.Len(t, tasks, 1)
}

func TestDispatcher_ScheduleRecurring_PersistsAddressPlans(t *testing.T) {
	d, repo := newDispatcherWithFakeRepo(t)

	addr := chain.MustAddress("0x0000000000000000000000000000000000000001")
	sched, _ := verification.NewSchedule("* * * * *", "UTC")
	_, err := d.ScheduleRecurring(context.Background(), sched, application.SchedulePayload{
		ChainID:  chain.OptimismMainnet,
		Metrics:  []verification.Metric{verification.MetricBalanceLatest},
		Strategy: verification.LatestN{N: 1},
		AddressPlans: []verification.AddressSamplingPlan{
			verification.KnownAddresses{Addresses: []chain.Address{addr}},
			verification.TopNHolders{N: 25},
		},
	})
	require.NoError(t, err)

	records, err := repo.ListActive(context.Background())
	require.NoError(t, err)
	require.Len(t, records, 1)
	require.Len(t, records[0].AddressPlans, 2)
	require.Equal(t, verification.KindKnownAddresses, records[0].AddressPlans[0].Kind())
	require.Equal(t, verification.KindTopNHolders, records[0].AddressPlans[1].Kind())
}

func TestDispatcher_ConfigProvider_RendersPlansIntoPayload(t *testing.T) {
	d, _ := newDispatcherWithFakeRepo(t)

	addr := chain.MustAddress("0x0000000000000000000000000000000000000001")
	sched, _ := verification.NewSchedule("* * * * *", "UTC")
	_, err := d.ScheduleRecurring(context.Background(), sched, application.SchedulePayload{
		ChainID:  chain.OptimismMainnet,
		Metrics:  []verification.Metric{verification.MetricBalanceLatest},
		Strategy: verification.LatestN{N: 1},
		AddressPlans: []verification.AddressSamplingPlan{
			verification.KnownAddresses{Addresses: []chain.Address{addr}},
		},
	})
	require.NoError(t, err)

	cfgs, err := d.ConfigProvider().GetConfigs()
	require.NoError(t, err)
	require.Len(t, cfgs, 1)

	var got queue.ScheduledRunPayload
	require.NoError(t, json.Unmarshal(cfgs[0].Task.Payload(), &got))
	require.NotEmpty(t, got.AddressPlansData)
	require.NotEqual(t, "[]", string(got.AddressPlansData))
}

func TestDispatcher_WithClock_StampsCreatedAt(t *testing.T) {
	d, repo := newDispatcherWithFakeRepo(t)
	fixed := time.Date(2026, 4, 22, 12, 0, 0, 0, time.UTC)
	d.WithClock(func() time.Time { return fixed })

	sched, _ := verification.NewSchedule("* * * * *", "UTC")
	_, err := d.ScheduleRecurring(context.Background(), sched, application.SchedulePayload{
		ChainID:  chain.OptimismMainnet,
		Metrics:  []verification.Metric{verification.MetricBlockHash},
		Strategy: verification.LatestN{N: 1},
	})
	require.NoError(t, err)

	records, err := repo.ListActive(context.Background())
	require.NoError(t, err)
	require.Len(t, records, 1)
	require.Equal(t, fixed, records[0].CreatedAt)
}
