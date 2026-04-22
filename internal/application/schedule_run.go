package application

import (
	"context"
	"errors"
	"fmt"

	"github.com/seokheejang/chain-sync-watch/internal/chain"
	"github.com/seokheejang/chain-sync-watch/internal/verification"
)

// ScheduleRunInput carries the arguments to ScheduleRun.Execute.
// ID is optional — an empty value asks the use case to generate a
// fresh RunID. Schedule is required only when Trigger is a
// ScheduledTrigger; it is ignored for ManualTrigger and
// RealtimeTrigger. AddressPlans is optional — zero plans disables
// the AddressLatest stratum for the resulting Run.
type ScheduleRunInput struct {
	ID           verification.RunID
	ChainID      chain.ChainID
	Strategy     verification.SamplingStrategy
	Metrics      []verification.Metric
	Trigger      verification.Trigger
	Schedule     verification.Schedule
	AddressPlans []verification.AddressSamplingPlan
}

// ScheduleRunResult is the successful return of ScheduleRun.
// JobID is populated only for ScheduledTrigger dispatches.
type ScheduleRunResult struct {
	RunID verification.RunID
	JobID *JobID
}

// ScheduleRun creates a Run, persists it in pending state, and
// dispatches work to the JobDispatcher based on the Trigger kind:
//
//   - ManualTrigger    → EnqueueRunExecution immediately.
//   - ScheduledTrigger → ScheduleRecurring with the given Schedule.
//   - RealtimeTrigger  → persist only (streaming path lands
//     post-MVP).
//
// All external dependencies are ports; this struct is a thin
// coordinator. Validation failures surface as ErrInvalidRun;
// duplicate RunIDs surface as ErrDuplicateRun.
type ScheduleRun struct {
	Runs       RunRepository
	Dispatcher JobDispatcher
	Clock      Clock
}

// Execute runs the use case. See the struct doc for behaviour.
func (uc ScheduleRun) Execute(ctx context.Context, in ScheduleRunInput) (ScheduleRunResult, error) {
	// Pair-validate Trigger and Schedule up-front so nothing is
	// persisted on a bad request. verification.NewRun handles the
	// rest of the structural validation below.
	if _, ok := in.Trigger.(verification.ScheduledTrigger); ok && in.Schedule.IsZero() {
		return ScheduleRunResult{}, errors.New("schedule run: scheduled trigger requires a Schedule")
	}

	id := in.ID
	if id == "" {
		generated, err := verification.NewRunID()
		if err != nil {
			return ScheduleRunResult{}, fmt.Errorf("schedule run: generate id: %w", err)
		}
		id = generated
	}

	// Reject duplicates. FindByID returns ErrRunNotFound when the
	// id is free; any other error is propagated as-is.
	if _, err := uc.Runs.FindByID(ctx, id); err == nil {
		return ScheduleRunResult{}, fmt.Errorf("%w: %s", ErrDuplicateRun, id)
	} else if !errors.Is(err, ErrRunNotFound) {
		return ScheduleRunResult{}, fmt.Errorf("schedule run: check existing: %w", err)
	}

	now := uc.Clock.Now()
	r, err := verification.NewRun(id, in.ChainID, in.Strategy, in.Metrics, in.Trigger, now, in.AddressPlans...)
	if err != nil {
		return ScheduleRunResult{}, fmt.Errorf("%w: %w", ErrInvalidRun, err)
	}

	if err := uc.Runs.Save(ctx, r); err != nil {
		return ScheduleRunResult{}, fmt.Errorf("schedule run: save: %w", err)
	}

	switch in.Trigger.(type) {
	case verification.ManualTrigger:
		if err := uc.Dispatcher.EnqueueRunExecution(ctx, id); err != nil {
			return ScheduleRunResult{}, fmt.Errorf("schedule run: enqueue: %w", err)
		}
		return ScheduleRunResult{RunID: id}, nil

	case verification.ScheduledTrigger:
		jobID, err := uc.Dispatcher.ScheduleRecurring(ctx, in.Schedule, SchedulePayload{
			ChainID:      in.ChainID,
			Metrics:      in.Metrics,
			Strategy:     in.Strategy,
			AddressPlans: in.AddressPlans,
		})
		if err != nil {
			return ScheduleRunResult{}, fmt.Errorf("schedule run: schedule: %w", err)
		}
		return ScheduleRunResult{RunID: id, JobID: &jobID}, nil

	case verification.RealtimeTrigger:
		// Streaming path lands post-MVP — the Run is persisted so
		// operators can observe its pending state, but no dispatch
		// happens here.
		return ScheduleRunResult{RunID: id}, nil

	default:
		return ScheduleRunResult{}, fmt.Errorf("schedule run: unknown trigger type %T", in.Trigger)
	}
}
