package queue_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/require"

	"github.com/seokheejang/chain-sync-watch/internal/application"
	"github.com/seokheejang/chain-sync-watch/internal/application/testsupport"
	"github.com/seokheejang/chain-sync-watch/internal/chain"
	"github.com/seokheejang/chain-sync-watch/internal/infrastructure/persistence"
	"github.com/seokheejang/chain-sync-watch/internal/infrastructure/queue"
	"github.com/seokheejang/chain-sync-watch/internal/verification"
)

// fakeExecuteRun records invocations and returns a configurable
// error. Implements queue.ExecuteRunUseCase.
type fakeExecuteRun struct {
	mu      sync.Mutex
	calls   []verification.RunID
	nextErr error
}

func (f *fakeExecuteRun) Execute(_ context.Context, id verification.RunID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, id)
	return f.nextErr
}

func (f *fakeExecuteRun) Calls() []verification.RunID {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]verification.RunID, len(f.calls))
	copy(out, f.calls)
	return out
}

func taskFor(t *testing.T, p queue.ExecuteRunPayload) *asynq.Task {
	t.Helper()
	body, err := p.Marshal()
	require.NoError(t, err)
	return asynq.NewTask(queue.TaskTypeExecuteRun, body)
}

func TestHandleExecuteRun_Success(t *testing.T) {
	fake := &fakeExecuteRun{}
	h := &queue.Handlers{ExecuteRun: fake}

	err := h.HandleExecuteRun(context.Background(), taskFor(t, queue.ExecuteRunPayload{RunID: "rid-1"}))
	require.NoError(t, err)
	require.Equal(t, []verification.RunID{"rid-1"}, fake.Calls())
}

func TestHandleExecuteRun_InvalidPayloadSkipsRetry(t *testing.T) {
	fake := &fakeExecuteRun{}
	h := &queue.Handlers{ExecuteRun: fake}

	bad := asynq.NewTask(queue.TaskTypeExecuteRun, []byte("not json"))
	err := h.HandleExecuteRun(context.Background(), bad)
	require.Error(t, err)
	require.ErrorIs(t, err, asynq.SkipRetry)
	require.Empty(t, fake.Calls())
}

func TestHandleExecuteRun_EmptyRunIDSkipsRetry(t *testing.T) {
	fake := &fakeExecuteRun{}
	h := &queue.Handlers{ExecuteRun: fake}

	bad := asynq.NewTask(queue.TaskTypeExecuteRun, []byte(`{"run_id":""}`))
	err := h.HandleExecuteRun(context.Background(), bad)
	require.Error(t, err)
	require.ErrorIs(t, err, asynq.SkipRetry)
}

func TestHandleExecuteRun_RunNotFoundSkipsRetry(t *testing.T) {
	fake := &fakeExecuteRun{nextErr: application.ErrRunNotFound}
	h := &queue.Handlers{ExecuteRun: fake}

	err := h.HandleExecuteRun(context.Background(), taskFor(t, queue.ExecuteRunPayload{RunID: "gone"}))
	require.Error(t, err)
	require.ErrorIs(t, err, asynq.SkipRetry)
}

func TestHandleExecuteRun_TransientErrorPropagates(t *testing.T) {
	fake := &fakeExecuteRun{nextErr: errors.New("upstream flaky")}
	h := &queue.Handlers{ExecuteRun: fake}

	err := h.HandleExecuteRun(context.Background(), taskFor(t, queue.ExecuteRunPayload{RunID: "rid-2"}))
	require.Error(t, err)
	require.NotErrorIs(t, err, asynq.SkipRetry, "transient errors must remain retryable")
}

// --- scheduled_run handler ----------------------------------------

// fakeEnqueuer records invocations and returns a configurable error.
// Implements queue.RunEnqueuer.
type fakeEnqueuer struct {
	mu      sync.Mutex
	calls   []verification.RunID
	nextErr error
}

func (f *fakeEnqueuer) EnqueueRunExecution(_ context.Context, id verification.RunID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, id)
	return f.nextErr
}

func (f *fakeEnqueuer) Calls() []verification.RunID {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]verification.RunID, len(f.calls))
	copy(out, f.calls)
	return out
}

func validScheduledPayload(t *testing.T) []byte {
	t.Helper()
	stratData, err := persistence.MarshalStrategy(verification.LatestN{N: 5})
	require.NoError(t, err)
	body, err := queue.ScheduledRunPayload{
		ChainID:      uint64(chain.OptimismMainnet),
		StrategyKind: verification.KindLatestN,
		StrategyData: stratData,
		MetricKeys:   []string{verification.MetricBlockHash.Key},
		CronExpr:     "0 */6 * * *",
	}.Marshal()
	require.NoError(t, err)
	return body
}

func scheduledFixture(t *testing.T) (*queue.Handlers, *testsupport.FakeRunRepo, *fakeEnqueuer, *testsupport.FakeClock) {
	t.Helper()
	runs := testsupport.NewFakeRunRepo()
	enq := &fakeEnqueuer{}
	clock := testsupport.NewFakeClock(time.Date(2026, 4, 22, 0, 0, 0, 0, time.UTC))
	return &queue.Handlers{Runs: runs, Enqueuer: enq, Clock: clock}, runs, enq, clock
}

func TestHandleScheduledRun_SuccessSavesRunAndEnqueues(t *testing.T) {
	h, runs, enq, clock := scheduledFixture(t)

	task := asynq.NewTask(queue.TaskTypeScheduledRun, validScheduledPayload(t))
	require.NoError(t, h.HandleScheduledRun(context.Background(), task))

	require.Len(t, enq.Calls(), 1)
	runID := enq.Calls()[0]

	saved, err := runs.FindByID(context.Background(), runID)
	require.NoError(t, err)
	require.Equal(t, chain.OptimismMainnet, saved.ChainID())
	require.Equal(t, verification.StatusPending, saved.Status())
	require.Equal(t, clock.Now(), saved.CreatedAt())

	trig, ok := saved.Trigger().(verification.ScheduledTrigger)
	require.True(t, ok)
	require.Equal(t, "0 */6 * * *", trig.CronExpr)

	strategy, ok := saved.Strategy().(verification.LatestN)
	require.True(t, ok)
	require.Equal(t, uint(5), strategy.N)

	require.Len(t, saved.Metrics(), 1)
	require.Equal(t, verification.MetricBlockHash.Key, saved.Metrics()[0].Key)
}

func TestHandleScheduledRun_InvalidPayloadSkipsRetry(t *testing.T) {
	h, _, _, _ := scheduledFixture(t)
	bad := asynq.NewTask(queue.TaskTypeScheduledRun, []byte(`not json`))
	err := h.HandleScheduledRun(context.Background(), bad)
	require.Error(t, err)
	require.ErrorIs(t, err, asynq.SkipRetry)
}

func TestHandleScheduledRun_UnknownStrategyKindSkipsRetry(t *testing.T) {
	h, _, enq, _ := scheduledFixture(t)
	body, err := queue.ScheduledRunPayload{
		ChainID:      uint64(chain.OptimismMainnet),
		StrategyKind: "not_a_real_kind",
		StrategyData: []byte(`{}`),
		MetricKeys:   []string{verification.MetricBlockHash.Key},
		CronExpr:     "* * * * *",
	}.Marshal()
	require.NoError(t, err)

	err = h.HandleScheduledRun(context.Background(), asynq.NewTask(queue.TaskTypeScheduledRun, body))
	require.Error(t, err)
	require.ErrorIs(t, err, asynq.SkipRetry)
	require.Empty(t, enq.Calls())
}

func TestHandleScheduledRun_UnknownMetricKeySkipsRetry(t *testing.T) {
	h, _, enq, _ := scheduledFixture(t)
	stratData, err := persistence.MarshalStrategy(verification.LatestN{N: 1})
	require.NoError(t, err)
	body, err := queue.ScheduledRunPayload{
		ChainID:      uint64(chain.OptimismMainnet),
		StrategyKind: verification.KindLatestN,
		StrategyData: stratData,
		MetricKeys:   []string{"no.such.metric"},
		CronExpr:     "* * * * *",
	}.Marshal()
	require.NoError(t, err)

	err = h.HandleScheduledRun(context.Background(), asynq.NewTask(queue.TaskTypeScheduledRun, body))
	require.Error(t, err)
	require.ErrorIs(t, err, asynq.SkipRetry)
	require.Empty(t, enq.Calls())
}

func TestHandleScheduledRun_SaveErrorIsTransient(t *testing.T) {
	h, runs, enq, _ := scheduledFixture(t)
	runs.SaveErr = errors.New("db blip")

	task := asynq.NewTask(queue.TaskTypeScheduledRun, validScheduledPayload(t))
	err := h.HandleScheduledRun(context.Background(), task)
	require.Error(t, err)
	require.NotErrorIs(t, err, asynq.SkipRetry, "save errors must stay retryable")
	require.Empty(t, enq.Calls())
}

func TestHandleScheduledRun_EnqueueErrorIsTransient(t *testing.T) {
	h, _, enq, _ := scheduledFixture(t)
	enq.nextErr = errors.New("redis blip")

	task := asynq.NewTask(queue.TaskTypeScheduledRun, validScheduledPayload(t))
	err := h.HandleScheduledRun(context.Background(), task)
	require.Error(t, err)
	require.NotErrorIs(t, err, asynq.SkipRetry, "enqueue errors must stay retryable")
}

func TestHandleScheduledRun_PropagatesAddressPlansToRun(t *testing.T) {
	h, runs, enq, _ := scheduledFixture(t)

	addr := chain.MustAddress("0x0000000000000000000000000000000000000001")
	plans := []verification.AddressSamplingPlan{
		verification.KnownAddresses{Addresses: []chain.Address{addr}},
		verification.TopNHolders{N: 10},
	}
	planData, err := persistence.MarshalAddressPlans(plans)
	require.NoError(t, err)
	stratData, err := persistence.MarshalStrategy(verification.LatestN{N: 1})
	require.NoError(t, err)

	body, err := queue.ScheduledRunPayload{
		ChainID:          uint64(chain.OptimismMainnet),
		StrategyKind:     verification.KindLatestN,
		StrategyData:     stratData,
		MetricKeys:       []string{verification.MetricBalanceLatest.Key},
		AddressPlansData: planData,
		CronExpr:         "* * * * *",
	}.Marshal()
	require.NoError(t, err)

	require.NoError(t, h.HandleScheduledRun(context.Background(), asynq.NewTask(queue.TaskTypeScheduledRun, body)))
	require.Len(t, enq.Calls(), 1)

	saved, err := runs.FindByID(context.Background(), enq.Calls()[0])
	require.NoError(t, err)
	got := saved.AddressPlans()
	require.Len(t, got, 2)
	require.Equal(t, verification.KindKnownAddresses, got[0].Kind())
	require.Equal(t, verification.KindTopNHolders, got[1].Kind())
}

func TestHandleScheduledRun_EmptyPlansDataOK(t *testing.T) {
	h, runs, enq, _ := scheduledFixture(t)

	// Omitted plans (nil data) is the default shape for cron-only
	// verifications that just check block-immutable fields.
	task := asynq.NewTask(queue.TaskTypeScheduledRun, validScheduledPayload(t))
	require.NoError(t, h.HandleScheduledRun(context.Background(), task))
	require.Len(t, enq.Calls(), 1)

	saved, err := runs.FindByID(context.Background(), enq.Calls()[0])
	require.NoError(t, err)
	require.Nil(t, saved.AddressPlans())
}

func TestHandleScheduledRun_MissingWiringFailsLoud(t *testing.T) {
	// Runs/Enqueuer/Clock unset — handler must refuse without
	// SkipRetry so a config-reload can fix it in place.
	h := &queue.Handlers{}
	task := asynq.NewTask(queue.TaskTypeScheduledRun, validScheduledPayload(t))
	err := h.HandleScheduledRun(context.Background(), task)
	require.Error(t, err)
	require.NotErrorIs(t, err, asynq.SkipRetry)
}

func TestHandlers_Register(t *testing.T) {
	// Mux.Handler lookup isn't exported; registering twice should
	// panic. Just assert Register does not panic on first call.
	h := &queue.Handlers{ExecuteRun: &fakeExecuteRun{}}
	mux := asynq.NewServeMux()
	h.Register(mux)
	// Register on the same mux is idempotent? asynq.ServeMux
	// panics on duplicate — we don't call it twice.
	_ = mux
}
