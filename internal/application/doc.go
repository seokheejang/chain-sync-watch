// Package application hosts the verification use cases — the layer
// between the domain (internal/chain, source, verification, diff) and
// the infrastructure adapters (persistence, queue, HTTP).
//
// Every external dependency is a port: RunRepository, DiffRepository,
// SourceGateway, JobDispatcher, ChainHead, Clock, RateLimitBudget.
// Use cases receive ports via constructor injection; tests substitute
// the in-memory fakes in internal/testsupport. No adapter package is
// imported here — depguard enforces the boundary (see .golangci.yml
// "application-boundary" rule).
//
// The use cases are split one-per-file by the verb they expose:
//
//   - ScheduleRun  — create a Run, persist it, dispatch or register.
//   - ExecuteRun   — the engine: fan out fetches, compare, judge,
//     persist. Phase 5B.
//   - QueryRuns    — filtered list + single-record lookup.
//   - QueryDiffs   — filtered list of DiffRecords.
//   - ReplayDiff   — re-fetch a Discrepancy; mark resolved on match.
//     Phase 5C.
//
// DTOs (RunFilter, DiffFilter, DiffRecord, JobID, SchedulePayload)
// live in this package rather than the domain because they exist
// specifically at the use-case boundary — persistence and transport
// speak this shape, not the domain's.
package application
