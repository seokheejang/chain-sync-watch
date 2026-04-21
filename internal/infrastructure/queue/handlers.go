package queue

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/hibiken/asynq"

	"github.com/seokheejang/chain-sync-watch/internal/application"
	"github.com/seokheejang/chain-sync-watch/internal/verification"
)

// ExecuteRunUseCase is the narrow port the handler consumes. It
// exists so tests can inject a fake without instantiating the full
// application.ExecuteRun struct and its six port dependencies.
type ExecuteRunUseCase interface {
	Execute(ctx context.Context, id verification.RunID) error
}

// Handlers bridges asynq tasks to application use cases.
// Retry classification:
//
//   - Payload decode failures   → asynq.SkipRetry (permanent).
//   - application.ErrRunNotFound → asynq.SkipRetry (the Run was
//     deleted between enqueue and dispatch; retrying won't resurrect
//     it).
//   - Everything else           → propagated as-is so asynq's
//     MaxRetry + backoff policy kicks in.
type Handlers struct {
	ExecuteRun ExecuteRunUseCase
	Logger     *slog.Logger
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

// HandleScheduledRun is a Phase 7A stub. The cron scheduler enqueues
// tasks here; full wiring (decode payload → build Run → save →
// enqueue ExecuteRun) lands in Phase 7B/7C together with the durable
// schedule store. Returning SkipRetry prevents infinite re-fires on
// cron ticks until the real implementation lands.
func (h *Handlers) HandleScheduledRun(_ context.Context, t *asynq.Task) error {
	if _, err := UnmarshalScheduledRunPayload(t.Payload()); err != nil {
		h.logWarn("scheduled_run decode", "err", err)
		return fmt.Errorf("%w: %w", err, asynq.SkipRetry)
	}
	h.logWarn("scheduled_run: not yet implemented (Phase 7A stub)")
	return fmt.Errorf("scheduled_run not implemented: %w", asynq.SkipRetry)
}

func (h *Handlers) logWarn(msg string, args ...any) {
	if h.Logger != nil {
		h.Logger.Warn(msg, args...)
	}
}
