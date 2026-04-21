// Package queue wires the application's JobDispatcher port to
// asynq (hibiken/asynq) on top of Redis. It owns:
//
//   - Dispatcher — the JobDispatcher implementation. Enqueues
//     one-off ExecuteRun tasks and registers recurring cron jobs
//     through asynq's PeriodicTaskManager.
//   - Handlers — asynq task handlers. HandleExecuteRun decodes a
//     payload and invokes the ExecuteRun use case, mapping
//     application-level errors to asynq retry semantics
//     (transient → retry, permanent → SkipRetry).
//   - Scheduler — the thin ConfigProvider backing
//     PeriodicTaskManager. Recurring jobs are stored in-memory and
//     reconciled on every sync tick; CancelScheduled drops them.
//   - Health — a small HTTP server the worker exposes for
//     /healthz (liveness) and /readyz (Redis ping + server ready).
//
// This package deliberately depends on asynq/miniredis (redis-side
// drivers land via transitive deps) but not on any other part of
// the infrastructure layer — everything the handlers need arrives
// through constructor injection as application ports.
package queue
