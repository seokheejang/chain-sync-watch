package queue

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/hibiken/asynq"

	"github.com/seokheejang/chain-sync-watch/internal/application"
	"github.com/seokheejang/chain-sync-watch/internal/verification"
)

// EnqueueOptions captures the knobs operators tune per task.
// Defaults are conservative — three retries, thirty-minute timeout,
// seven-day retention for archived/completed tasks.
type EnqueueOptions struct {
	MaxRetry  int
	Timeout   time.Duration
	Retention time.Duration
	Queue     string
}

func defaultEnqueueOptions() EnqueueOptions {
	return EnqueueOptions{
		MaxRetry:  3,
		Timeout:   30 * time.Minute,
		Retention: 7 * 24 * time.Hour,
		Queue:     QueueDefault,
	}
}

// Dispatcher wires the JobDispatcher port to asynq. Recurring
// jobs live in an in-memory store the scheduler polls; CancelScheduled
// simply drops the entry and the PeriodicTaskManager stops firing
// at the next sync tick.
//
// Limitation (Phase 7A): the recurring-job store is process-local.
// Running csw-server and csw-worker as separate processes means
// ScheduleRecurring from the server will not propagate to the
// worker without a shared persistence layer. Phase 7B/7C revisits
// this with a Postgres-backed ConfigProvider.
type Dispatcher struct {
	client *asynq.Client
	store  *scheduleStore
	opts   EnqueueOptions
}

// NewDispatcher builds a Dispatcher around the given Redis
// connection options.
func NewDispatcher(opt asynq.RedisConnOpt) *Dispatcher {
	return &Dispatcher{
		client: asynq.NewClient(opt),
		store:  newScheduleStore(),
		opts:   defaultEnqueueOptions(),
	}
}

// WithOptions overrides the default enqueue options. Chainable.
func (d *Dispatcher) WithOptions(o EnqueueOptions) *Dispatcher {
	if o.MaxRetry > 0 {
		d.opts.MaxRetry = o.MaxRetry
	}
	if o.Timeout > 0 {
		d.opts.Timeout = o.Timeout
	}
	if o.Retention > 0 {
		d.opts.Retention = o.Retention
	}
	if o.Queue != "" {
		d.opts.Queue = o.Queue
	}
	return d
}

// Close releases the underlying asynq client.
func (d *Dispatcher) Close() error { return d.client.Close() }

// ConfigProvider exposes the in-memory schedule store so the
// PeriodicTaskManager can poll it. The worker wires this up during
// startup.
func (d *Dispatcher) ConfigProvider() asynq.PeriodicTaskConfigProvider {
	return d.store
}

// EnqueueRunExecution implements application.JobDispatcher.
func (d *Dispatcher) EnqueueRunExecution(ctx context.Context, runID verification.RunID) error {
	payload, err := ExecuteRunPayload{RunID: string(runID)}.Marshal()
	if err != nil {
		return err
	}
	task := asynq.NewTask(TaskTypeExecuteRun, payload, d.taskOptions()...)
	if _, err := d.client.EnqueueContext(ctx, task); err != nil {
		return fmt.Errorf("queue: enqueue execute_run: %w", err)
	}
	return nil
}

// ScheduleRecurring registers a cron-driven job with the in-memory
// ConfigProvider. The worker's PeriodicTaskManager picks it up on
// the next sync tick.
func (d *Dispatcher) ScheduleRecurring(
	ctx context.Context,
	schedule verification.Schedule,
	payload application.SchedulePayload,
) (application.JobID, error) {
	if schedule.IsZero() {
		return "", fmt.Errorf("queue: schedule_recurring: empty schedule")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}

	metricKeys := make([]string, len(payload.Metrics))
	for i, m := range payload.Metrics {
		metricKeys[i] = m.Key
	}
	strategyKind := payload.Strategy.Kind()
	// The strategy body is serialised as an opaque blob; the
	// handler will decode it via the same persistence helpers the
	// repository uses.
	strategyData, err := json.Marshal(payload.Strategy)
	if err != nil {
		return "", fmt.Errorf("queue: encode strategy: %w", err)
	}

	body, err := ScheduledRunPayload{
		ChainID:      payload.ChainID.Uint64(),
		StrategyKind: strategyKind,
		StrategyData: strategyData,
		MetricKeys:   metricKeys,
		CronExpr:     schedule.CronExpr(),
	}.Marshal()
	if err != nil {
		return "", err
	}

	task := asynq.NewTask(TaskTypeScheduledRun, body, d.taskOptions()...)
	id, err := newJobID()
	if err != nil {
		return "", err
	}
	d.store.add(id, &asynq.PeriodicTaskConfig{
		Cronspec: schedule.CronExpr(),
		Task:     task,
	})
	return id, nil
}

// CancelScheduled drops a recurring job from the ConfigProvider.
// Already-fired tasks continue to process to completion.
func (d *Dispatcher) CancelScheduled(_ context.Context, id application.JobID) error {
	if !d.store.remove(id) {
		return fmt.Errorf("queue: cancel_scheduled: unknown job id %q", id)
	}
	return nil
}

func (d *Dispatcher) taskOptions() []asynq.Option {
	return []asynq.Option{
		asynq.MaxRetry(d.opts.MaxRetry),
		asynq.Timeout(d.opts.Timeout),
		asynq.Retention(d.opts.Retention),
		asynq.Queue(d.opts.Queue),
	}
}

// --- scheduleStore -------------------------------------------------

// scheduleStore is an in-memory map of JobID → PeriodicTaskConfig.
// It satisfies asynq.PeriodicTaskConfigProvider so the worker can
// feed it straight into a PeriodicTaskManager.
type scheduleStore struct {
	mu      sync.RWMutex
	configs map[application.JobID]*asynq.PeriodicTaskConfig
}

func newScheduleStore() *scheduleStore {
	return &scheduleStore{configs: map[application.JobID]*asynq.PeriodicTaskConfig{}}
}

func (s *scheduleStore) add(id application.JobID, cfg *asynq.PeriodicTaskConfig) {
	s.mu.Lock()
	s.configs[id] = cfg
	s.mu.Unlock()
}

func (s *scheduleStore) remove(id application.JobID) bool {
	s.mu.Lock()
	_, ok := s.configs[id]
	delete(s.configs, id)
	s.mu.Unlock()
	return ok
}

// GetConfigs satisfies asynq.PeriodicTaskConfigProvider.
func (s *scheduleStore) GetConfigs() ([]*asynq.PeriodicTaskConfig, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*asynq.PeriodicTaskConfig, 0, len(s.configs))
	for _, c := range s.configs {
		out = append(out, c)
	}
	return out, nil
}

// --- JobID generator -----------------------------------------------

// newJobID returns a 16-byte hex-encoded random JobID. The string
// form is safe to embed in log lines and URLs.
func newJobID() (application.JobID, error) {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", fmt.Errorf("queue: generate job id: %w", err)
	}
	return application.JobID(hex.EncodeToString(buf[:])), nil
}

// Compile-time assertion.
var _ application.JobDispatcher = (*Dispatcher)(nil)
