package queue

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/hibiken/asynq"

	"github.com/seokheejang/chain-sync-watch/internal/application"
	"github.com/seokheejang/chain-sync-watch/internal/infrastructure/persistence"
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

// providerTimeout bounds the GetConfigs() query the asynq scheduler
// issues every SyncInterval (default 10s). A Postgres round-trip on
// a healthy cluster finishes in well under a second; this is the
// belt-and-braces ceiling to avoid blocking the scheduler loop on
// an unresponsive DB.
const providerTimeout = 5 * time.Second

// Dispatcher wires the JobDispatcher port to asynq. Recurring jobs
// are persisted via ScheduleRepository so csw-server writes and
// csw-worker reads converge on the same view even across restarts —
// the DB replaces the in-memory scheduleStore Phase 7A used for
// prototyping.
type Dispatcher struct {
	client    *asynq.Client
	schedules application.ScheduleRepository
	opts      EnqueueOptions
	clock     func() time.Time
}

// NewDispatcher builds a Dispatcher around the given Redis
// connection options. ScheduleRepository is required — passing nil
// disables ScheduleRecurring (documented behaviour to catch mis-
// configured tests; production always supplies a real repo).
func NewDispatcher(opt asynq.RedisConnOpt, schedules application.ScheduleRepository) *Dispatcher {
	return &Dispatcher{
		client:    asynq.NewClient(opt),
		schedules: schedules,
		opts:      defaultEnqueueOptions(),
		clock:     time.Now,
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

// WithClock overrides the wall clock used when stamping
// ScheduleRecord.CreatedAt. Tests use it to make CreatedAt
// deterministic; production leaves the default time.Now.
func (d *Dispatcher) WithClock(now func() time.Time) *Dispatcher {
	if now != nil {
		d.clock = now
	}
	return d
}

// Close releases the underlying asynq client.
func (d *Dispatcher) Close() error { return d.client.Close() }

// ConfigProvider returns an asynq.PeriodicTaskConfigProvider backed
// by the injected ScheduleRepository. The Scheduler polls this on
// every SyncInterval; on each poll we translate active records into
// asynq.PeriodicTaskConfig entries.
func (d *Dispatcher) ConfigProvider() asynq.PeriodicTaskConfigProvider {
	return &dbConfigProvider{
		schedules: d.schedules,
		opts:      d.opts,
	}
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

// ScheduleRecurring persists a ScheduleRecord so the ConfigProvider
// picks it up on the next sync tick. The returned JobID is the
// primary key — callers can later pass it to CancelScheduled.
func (d *Dispatcher) ScheduleRecurring(
	ctx context.Context,
	schedule verification.Schedule,
	payload application.SchedulePayload,
) (application.JobID, error) {
	if schedule.IsZero() {
		return "", fmt.Errorf("queue: schedule_recurring: empty schedule")
	}
	if d.schedules == nil {
		return "", fmt.Errorf("queue: schedule_recurring: no schedule repository wired")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}

	id, err := newJobID()
	if err != nil {
		return "", err
	}
	record := application.ScheduleRecord{
		JobID:        id,
		ChainID:      payload.ChainID,
		Schedule:     schedule,
		Strategy:     payload.Strategy,
		Metrics:      payload.Metrics,
		AddressPlans: payload.AddressPlans,
		TokenPlans:   payload.TokenPlans,
		CreatedAt:    d.clock(),
		Active:       true,
	}
	if err := d.schedules.Save(ctx, record); err != nil {
		return "", fmt.Errorf("queue: schedule_recurring save: %w", err)
	}
	return id, nil
}

// CancelScheduled soft-deletes the schedule. The PeriodicTaskManager
// stops emitting the task at the next sync tick; rows already in
// flight complete normally.
func (d *Dispatcher) CancelScheduled(ctx context.Context, id application.JobID) error {
	if d.schedules == nil {
		return fmt.Errorf("queue: cancel_scheduled: no schedule repository wired")
	}
	if err := d.schedules.Deactivate(ctx, id); err != nil {
		return fmt.Errorf("queue: cancel_scheduled: %w", err)
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

// --- dbConfigProvider ----------------------------------------------

// dbConfigProvider satisfies asynq.PeriodicTaskConfigProvider by
// reading active ScheduleRecords from the repository on each call
// and rendering them as asynq.PeriodicTaskConfig entries.
//
// GetConfigs runs on the scheduler's polling cadence (SyncInterval
// — 10s default) so the cost is dominated by one indexed SELECT
// per poll. The provider is stateless: a new process reconstructs
// the full schedule view from the first GetConfigs call.
type dbConfigProvider struct {
	schedules application.ScheduleRepository
	opts      EnqueueOptions
}

// GetConfigs queries ListActive and renders the result as asynq
// periodic task configs. Records whose payload cannot be rebuilt
// (unknown strategy kind, dropped metric catalog entry) are skipped
// — surfacing them as a scheduler-loop failure would block every
// healthy schedule. The persistence layer already enforces schema
// shape, so malformed records here indicate a deliberate rename
// the operator needs to reconcile.
func (p *dbConfigProvider) GetConfigs() ([]*asynq.PeriodicTaskConfig, error) {
	if p.schedules == nil {
		return nil, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), providerTimeout)
	defer cancel()
	records, err := p.schedules.ListActive(ctx)
	if err != nil {
		return nil, fmt.Errorf("queue: provider list_active: %w", err)
	}
	out := make([]*asynq.PeriodicTaskConfig, 0, len(records))
	for _, r := range records {
		task, err := buildScheduledRunTask(r, p.opts)
		if err != nil {
			continue
		}
		out = append(out, &asynq.PeriodicTaskConfig{
			Cronspec: r.Schedule.CronExpr(),
			Task:     task,
		})
	}
	return out, nil
}

// buildScheduledRunTask renders a ScheduleRecord into the asynq
// Task the PeriodicTaskManager will enqueue on every fire. The
// output lives inside asynq's Redis representation so the payload
// format must round-trip through the same serialise helpers the
// runs table uses — callers decode with persistence.UnmarshalStrategy,
// persistence.UnmarshalAddressPlans, and persistence.UnmarshalTokenPlans.
func buildScheduledRunTask(r application.ScheduleRecord, opts EnqueueOptions) (*asynq.Task, error) {
	stratData, err := persistence.MarshalStrategy(r.Strategy)
	if err != nil {
		return nil, fmt.Errorf("queue: build scheduled_run strategy: %w", err)
	}
	addressPlanData, err := persistence.MarshalAddressPlans(r.AddressPlans)
	if err != nil {
		return nil, fmt.Errorf("queue: build scheduled_run address plans: %w", err)
	}
	tokenPlanData, err := persistence.MarshalTokenPlans(r.TokenPlans)
	if err != nil {
		return nil, fmt.Errorf("queue: build scheduled_run token plans: %w", err)
	}
	metricKeys := make([]string, len(r.Metrics))
	for i, m := range r.Metrics {
		metricKeys[i] = m.Key
	}
	body, err := ScheduledRunPayload{
		ChainID:          r.ChainID.Uint64(),
		StrategyKind:     r.Strategy.Kind(),
		StrategyData:     stratData,
		MetricKeys:       metricKeys,
		AddressPlansData: addressPlanData,
		TokenPlansData:   tokenPlanData,
		CronExpr:         r.Schedule.CronExpr(),
	}.Marshal()
	if err != nil {
		return nil, fmt.Errorf("queue: build scheduled_run payload: %w", err)
	}
	return asynq.NewTask(TaskTypeScheduledRun, body, taskOptionsFrom(opts)...), nil
}

func taskOptionsFrom(o EnqueueOptions) []asynq.Option {
	return []asynq.Option{
		asynq.MaxRetry(o.MaxRetry),
		asynq.Timeout(o.Timeout),
		asynq.Retention(o.Retention),
		asynq.Queue(o.Queue),
	}
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
