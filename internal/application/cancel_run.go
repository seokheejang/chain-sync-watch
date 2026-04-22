package application

import (
	"context"
	"fmt"

	"github.com/seokheejang/chain-sync-watch/internal/verification"
)

// CancelRun is the write-side use case for transitioning a Run to
// StatusCancelled. Splitting the cancellation flow into its own
// struct (rather than stuffing a method onto QueryRuns) keeps the
// read/write separation the other use cases follow and lets
// operator policy (audit, authorization) land here without
// cross-contaminating query paths.
//
// Behaviour:
//
//   - Loads the Run. ErrRunNotFound is propagated as-is so HTTP can
//     map it to 404.
//   - Calls Run.Cancel(now). Terminal-status Runs (completed / failed /
//     already-cancelled) surface the domain error wrapped with
//     ErrInvalidRun so HTTP can map to 409.
//   - Persists the updated aggregate.
type CancelRun struct {
	Runs  RunRepository
	Clock Clock
}

// Execute cancels the Run identified by id. Returns nil on success.
func (uc CancelRun) Execute(ctx context.Context, id verification.RunID) error {
	run, err := uc.Runs.FindByID(ctx, id)
	if err != nil {
		return err
	}
	if err := run.Cancel(uc.Clock.Now()); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidRun, err)
	}
	if err := uc.Runs.Save(ctx, run); err != nil {
		return fmt.Errorf("cancel run: save: %w", err)
	}
	return nil
}
