// Package persistence implements the RunRepository and
// DiffRepository ports against Postgres via gorm. It lives in the
// infrastructure layer because it wires the domain aggregates to
// their storage shape — gorm tags, JSONB marshalling, column
// constraints — none of which should leak into the domain.
//
// Boundary notes:
//
//   - Models (runModel, diffModel) are unexported. Callers interact
//     only with the domain aggregates handed back by the mappers.
//   - Mappers are the only place gorm.io/* and lib/pq types appear.
//     Everything beyond that boundary is plain verification.Run /
//     diff.Discrepancy / application.DiffRecord.
//   - Rehydration of Run uses verification.Rehydrate so the state
//     machine is not re-walked just to put the aggregate back into
//     its original status.
//
// Migrations live in the top-level migrations/ package; this
// package does not call AutoMigrate.
package persistence
