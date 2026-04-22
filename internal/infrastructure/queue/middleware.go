package queue

import (
	"context"
	"log/slog"
	"time"

	"github.com/hibiken/asynq"
)

// LoggingMiddleware returns an asynq middleware that records every
// task's type, duration, and outcome (success / error) via slog.
// Two goals:
//
//   - Ops visibility: operators get a per-task timeline in the
//     worker log without having to add print statements inside each
//     handler.
//   - Cheap metrics: a Prometheus exporter or log-pipeline can
//     aggregate on `task_type` + outcome without parsing the free
//     text — both fields are stable structured keys.
//
// A nil logger is a no-op pass-through — call sites do not need a
// conditional to opt out of middleware chaining.
//
// Design notes:
//
//   - We log AFTER the inner handler returns so success / error
//     classification is accurate. Errors from asynq (SkipRetry, etc)
//     come through as wrapped values; the middleware does not
//     unwrap them — that's the handler's contract.
//   - Duration is reported in milliseconds; tasks routinely run for
//     seconds-to-minutes and millisecond precision is enough for
//     histogramming. A future metrics exporter can reach in via the
//     same struct keys without re-instrumenting.
//   - The middleware uses slog.LevelInfo for success and LevelWarn
//     for error. Errors are expected-but-unusual (rate limits, transient
//     DB blips); they don't warrant LevelError, which we reserve for
//     panics and unrecoverable conditions at the server level.
func LoggingMiddleware(logger *slog.Logger) asynq.MiddlewareFunc {
	return func(next asynq.Handler) asynq.Handler {
		return asynq.HandlerFunc(func(ctx context.Context, t *asynq.Task) error {
			if logger == nil {
				return next.ProcessTask(ctx, t)
			}
			start := time.Now()
			err := next.ProcessTask(ctx, t)
			dur := time.Since(start)
			if err != nil {
				logger.WarnContext(ctx, "asynq task error",
					"task_type", t.Type(),
					"duration_ms", dur.Milliseconds(),
					"err", err.Error(),
				)
				return err
			}
			logger.InfoContext(ctx, "asynq task ok",
				"task_type", t.Type(),
				"duration_ms", dur.Milliseconds(),
			)
			return nil
		})
	}
}
