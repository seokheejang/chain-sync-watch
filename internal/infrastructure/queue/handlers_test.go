package queue_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/require"

	"github.com/seokheejang/chain-sync-watch/internal/application"
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

func TestHandleScheduledRun_Phase7AStubRejects(t *testing.T) {
	h := &queue.Handlers{}
	body, err := queue.ScheduledRunPayload{
		ChainID:      10,
		StrategyKind: "latest_n",
		StrategyData: []byte(`{"n":1}`),
		MetricKeys:   []string{"block.hash"},
		CronExpr:     "* * * * *",
	}.Marshal()
	require.NoError(t, err)
	task := asynq.NewTask(queue.TaskTypeScheduledRun, body)

	err = h.HandleScheduledRun(context.Background(), task)
	require.Error(t, err)
	require.ErrorIs(t, err, asynq.SkipRetry)
	require.Contains(t, err.Error(), "not implemented")
}

func TestHandleScheduledRun_InvalidPayloadSkipsRetry(t *testing.T) {
	h := &queue.Handlers{}
	bad := asynq.NewTask(queue.TaskTypeScheduledRun, []byte(`not json`))
	err := h.HandleScheduledRun(context.Background(), bad)
	require.Error(t, err)
	require.ErrorIs(t, err, asynq.SkipRetry)
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
