package application

import (
	"context"

	"github.com/seokheejang/chain-sync-watch/internal/verification"
)

// QueryRuns is the read-side use case for Runs. It exists as a
// named type rather than a pair of loose functions so transport
// code can inject a single dependency and so policy additions
// (authorisation, rate limiting) can land in one place without
// touching every call site.
type QueryRuns struct {
	Runs RunRepository
}

// Get returns the Run with the given id or ErrRunNotFound.
func (uc QueryRuns) Get(ctx context.Context, id verification.RunID) (*verification.Run, error) {
	return uc.Runs.FindByID(ctx, id)
}

// List returns a filtered, paginated slice of Runs plus the total
// count of rows matching the filter (for UI pagination).
func (uc QueryRuns) List(ctx context.Context, f RunFilter) ([]*verification.Run, int, error) {
	return uc.Runs.List(ctx, f)
}
