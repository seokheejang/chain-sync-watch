package application

import (
	"context"

	"github.com/seokheejang/chain-sync-watch/internal/verification"
)

// QueryDiffs is the read-side use case for DiffRecords.
type QueryDiffs struct {
	Diffs DiffRepository
}

// Get returns the record with the given id or ErrDiffNotFound.
func (uc QueryDiffs) Get(ctx context.Context, id DiffID) (*DiffRecord, error) {
	return uc.Diffs.FindByID(ctx, id)
}

// List returns filtered, paginated DiffRecords plus the total row
// count.
func (uc QueryDiffs) List(ctx context.Context, f DiffFilter) ([]DiffRecord, int, error) {
	return uc.Diffs.List(ctx, f)
}

// ByRun returns every DiffRecord produced by a single Run.
func (uc QueryDiffs) ByRun(ctx context.Context, runID verification.RunID) ([]DiffRecord, error) {
	return uc.Diffs.FindByRun(ctx, runID)
}
