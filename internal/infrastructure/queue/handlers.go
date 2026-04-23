package queue

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/hibiken/asynq"

	"github.com/seokheejang/chain-sync-watch/internal/application"
	"github.com/seokheejang/chain-sync-watch/internal/chain"
	"github.com/seokheejang/chain-sync-watch/internal/infrastructure/persistence"
	"github.com/seokheejang/chain-sync-watch/internal/verification"
)

// ExecuteRunUseCase is the narrow port the handler consumes. It
// exists so tests can inject a fake without instantiating the full
// application.ExecuteRun struct and its six port dependencies.
type ExecuteRunUseCase interface {
	Execute(ctx context.Context, id verification.RunID) error
}

// RunEnqueuer is the one-method slice of application.JobDispatcher
// HandleScheduledRun needs: kick off a fresh ExecuteRun task for a
// Run just materialised from a cron fire. Narrowing to this port
// keeps handler tests from having to satisfy ScheduleRecurring and
// CancelScheduled too.
type RunEnqueuer interface {
	EnqueueRunExecution(ctx context.Context, runID verification.RunID) error
}

// Handlers bridges asynq tasks to application use cases.
// Retry classification:
//
//   - Payload decode failures   → asynq.SkipRetry (permanent).
//   - application.ErrRunNotFound → asynq.SkipRetry (the Run was
//     deleted between enqueue and dispatch; retrying won't resurrect
//     it).
//   - Unknown strategy kind / metric key in a scheduled-run payload
//     → asynq.SkipRetry (permanent; the payload was built from a
//     stale enum or a rename — no amount of retry fixes it).
//   - Run-repo Save errors and enqueue errors on the scheduled-run
//     path → propagated as-is so asynq's MaxRetry + backoff policy
//     kicks in. A transient DB blip retries; a permanent corruption
//     ends up in asynq's archived queue after MaxRetry.
//   - Everything else           → propagated as-is.
type Handlers struct {
	ExecuteRun ExecuteRunUseCase

	// Scheduled-run path dependencies. All three are required when
	// the worker is set up to process TaskTypeScheduledRun; a nil
	// here surfaces as a handler error on the first fire so the
	// problem shows up in logs rather than silently dropping runs.
	Runs     application.RunRepository
	Enqueuer RunEnqueuer
	Clock    application.Clock

	Logger *slog.Logger
}

// Register attaches every handler to mux. Call this once during
// worker startup.
func (h *Handlers) Register(mux *asynq.ServeMux) {
	mux.HandleFunc(TaskTypeExecuteRun, h.HandleExecuteRun)
	mux.HandleFunc(TaskTypeScheduledRun, h.HandleScheduledRun)
}

// HandleExecuteRun processes a one-off ExecuteRun task.
func (h *Handlers) HandleExecuteRun(ctx context.Context, t *asynq.Task) error {
	p, err := UnmarshalExecuteRunPayload(t.Payload())
	if err != nil {
		h.logWarn("execute_run decode", "err", err)
		return fmt.Errorf("%w: %w", err, asynq.SkipRetry)
	}
	if err := h.ExecuteRun.Execute(ctx, verification.RunID(p.RunID)); err != nil {
		if errors.Is(err, application.ErrRunNotFound) {
			h.logWarn("execute_run: run not found", "run_id", p.RunID)
			return fmt.Errorf("%w: %w", err, asynq.SkipRetry)
		}
		h.logWarn("execute_run: use case error", "run_id", p.RunID, "err", err)
		return err
	}
	return nil
}

// HandleScheduledRun decodes a cron-fired ScheduledRunPayload,
// materialises a fresh Run in the repository, and hands it to the
// enqueuer so the ExecuteRun pipeline processes it asynchronously.
//
// The separation of "schedule fires" (this handler) from "run
// executes" (ExecuteRun handler) is deliberate — it keeps the
// scheduler loop cheap (persist + enqueue only) and lets ExecuteRun
// retry its expensive pipeline independently.
//
// Classification:
//
//   - Payload decode errors / unknown strategy kind / unknown
//     metric key → SkipRetry. These indicate a payload built
//     against a different code version; retrying cannot fix it.
//   - verification.NewRun validation errors → SkipRetry. A malformed
//     payload won't pass on re-delivery either.
//   - Runs.Save and Enqueuer errors → transient, no SkipRetry. Let
//     asynq's MaxRetry / backoff smooth over database or Redis
//     blips.
func (h *Handlers) HandleScheduledRun(ctx context.Context, t *asynq.Task) error {
	p, err := UnmarshalScheduledRunPayload(t.Payload())
	if err != nil {
		h.logWarn("scheduled_run decode", "err", err)
		return fmt.Errorf("%w: %w", err, asynq.SkipRetry)
	}

	if h.Runs == nil || h.Enqueuer == nil || h.Clock == nil {
		// A worker wired without these cannot process scheduled
		// runs. Surface loudly on every fire so the operator sees
		// it immediately; do NOT SkipRetry because a config reload
		// could fix the wiring in place.
		h.logWarn("scheduled_run: handler missing Runs/Enqueuer/Clock wiring")
		return errors.New("scheduled_run: handler not fully wired")
	}

	strategy, err := persistence.UnmarshalStrategy(p.StrategyKind, p.StrategyData)
	if err != nil {
		h.logWarn("scheduled_run decode strategy", "kind", p.StrategyKind, "err", err)
		return fmt.Errorf("%w: %w", err, asynq.SkipRetry)
	}

	addressPlans, err := persistence.UnmarshalAddressPlans(p.AddressPlansData)
	if err != nil {
		h.logWarn("scheduled_run decode address plans", "err", err)
		return fmt.Errorf("%w: %w", err, asynq.SkipRetry)
	}
	tokenPlans, err := persistence.UnmarshalTokenPlans(p.TokenPlansData)
	if err != nil {
		h.logWarn("scheduled_run decode token plans", "err", err)
		return fmt.Errorf("%w: %w", err, asynq.SkipRetry)
	}

	metrics := make([]verification.Metric, 0, len(p.MetricKeys))
	for _, k := range p.MetricKeys {
		m, ok := persistence.MetricByKey(k)
		if !ok {
			h.logWarn("scheduled_run unknown metric", "key", k)
			return fmt.Errorf("scheduled_run: unknown metric key %q: %w", k, asynq.SkipRetry)
		}
		metrics = append(metrics, m)
	}

	runID, err := verification.NewRunID()
	if err != nil {
		return fmt.Errorf("scheduled_run: generate run id: %w", err)
	}

	run, err := verification.NewRun(
		runID,
		chain.ChainID(p.ChainID),
		strategy,
		metrics,
		verification.ScheduledTrigger{CronExpr: p.CronExpr},
		h.Clock.Now(),
		addressPlans...,
	)
	if err != nil {
		h.logWarn("scheduled_run build run", "err", err)
		return fmt.Errorf("%w: %w", err, asynq.SkipRetry)
	}
	if len(tokenPlans) > 0 {
		if err := run.SetTokenPlans(tokenPlans...); err != nil {
			h.logWarn("scheduled_run set token plans", "err", err)
			return fmt.Errorf("%w: %w", err, asynq.SkipRetry)
		}
	}

	if err := h.Runs.Save(ctx, run); err != nil {
		h.logWarn("scheduled_run save", "run_id", runID, "err", err)
		return fmt.Errorf("scheduled_run: save run: %w", err)
	}

	if err := h.Enqueuer.EnqueueRunExecution(ctx, runID); err != nil {
		h.logWarn("scheduled_run enqueue", "run_id", runID, "err", err)
		// The Run is saved in pending state; if enqueue keeps
		// failing until MaxRetry, operators can manually re-enqueue
		// via the API (Phase 8). For now propagate so asynq retries.
		return fmt.Errorf("scheduled_run: enqueue execute: %w", err)
	}

	return nil
}

func (h *Handlers) logWarn(msg string, args ...any) {
	if h.Logger != nil {
		h.Logger.Warn(msg, args...)
	}
}
