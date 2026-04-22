package verification

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/seokheejang/chain-sync-watch/internal/chain"
)

// RunID identifies one verification pass. Generation is the
// application layer's responsibility — the domain only requires a
// non-empty string, so persistence back-ends can supply whatever
// identifier format fits them (UUID, ULID, Snowflake, hex).
type RunID string

// NewRunID returns a 16-byte hex-encoded random id. Callers that
// already have an id (for example, rehydrating from the database)
// use the string constructor RunID("...") directly instead.
func NewRunID() (RunID, error) {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", fmt.Errorf("new run id: %w", err)
	}
	return RunID(hex.EncodeToString(buf[:])), nil
}

// Status is the lifecycle state of a Run. Transitions are enforced
// by the Run methods; invalid transitions return an error rather
// than panic so the application layer can surface the problem to
// users without the process dying.
type Status string

const (
	// StatusPending — the run has been created but not dispatched.
	StatusPending Status = "pending"
	// StatusRunning — the run is executing.
	StatusRunning Status = "running"
	// StatusCompleted — terminal; the run finished all its work.
	StatusCompleted Status = "completed"
	// StatusFailed — terminal; the run aborted with an error.
	StatusFailed Status = "failed"
	// StatusCancelled — terminal; operator stopped the run before it
	// could finish.
	StatusCancelled Status = "cancelled"
)

// IsTerminal reports whether the status represents a final state
// that no transition may leave.
func (s Status) IsTerminal() bool {
	switch s {
	case StatusCompleted, StatusFailed, StatusCancelled:
		return true
	}
	return false
}

// Run is one verification pass: a chain, a sampling strategy, the
// metrics to check, and the trigger that kicked it off. Fields are
// unexported because the lifecycle is state-machine-driven —
// mutating status directly would let an object skip transitions
// that need to record timestamps.
//
// Run is an aggregate root: the persistence layer rehydrates it and
// the application layer drives its state transitions. The domain
// package provides no clock of its own; callers pass the current
// time into each transition so tests remain deterministic.
//
// addressPlans is optional: a zero-length slice is valid and means
// "no address-stratum coverage". ExecuteRun uses it exclusively for
// AddressLatest / AddressAtBlock metrics; BlockImmutable passes
// require only the block-level SamplingStrategy.
type Run struct {
	id           RunID
	chainID      chain.ChainID
	strategy     SamplingStrategy
	addressPlans []AddressSamplingPlan
	metrics      []Metric
	trigger      Trigger
	status       Status
	createdAt    time.Time
	startedAt    *time.Time
	finishedAt   *time.Time
	errorMsg     string
}

// NewRun constructs a Run in the pending state after validating the
// required inputs:
//
//   - id must be non-empty
//   - cid must be non-zero
//   - strategy must be non-nil
//   - metrics must be non-empty
//   - trigger must be non-nil
//
// addressPlans is variadic and optional — zero plans is valid. Each
// plan must be non-nil. The metrics and addressPlans slices are
// copied so later mutations by the caller cannot reach the
// aggregate's internal state.
func NewRun(
	id RunID,
	cid chain.ChainID,
	strategy SamplingStrategy,
	metrics []Metric,
	trigger Trigger,
	createdAt time.Time,
	addressPlans ...AddressSamplingPlan,
) (*Run, error) {
	if id == "" {
		return nil, errors.New("run: id is empty")
	}
	if cid == 0 {
		return nil, errors.New("run: chain id is zero")
	}
	if strategy == nil {
		return nil, errors.New("run: sampling strategy is nil")
	}
	if len(metrics) == 0 {
		return nil, errors.New("run: metrics list is empty")
	}
	if trigger == nil {
		return nil, errors.New("run: trigger is nil")
	}
	for i, p := range addressPlans {
		if p == nil {
			return nil, fmt.Errorf("run: address plan %d is nil", i)
		}
	}
	m := make([]Metric, len(metrics))
	copy(m, metrics)
	var plans []AddressSamplingPlan
	if len(addressPlans) > 0 {
		plans = make([]AddressSamplingPlan, len(addressPlans))
		copy(plans, addressPlans)
	}
	return &Run{
		id:           id,
		chainID:      cid,
		strategy:     strategy,
		addressPlans: plans,
		metrics:      m,
		trigger:      trigger,
		status:       StatusPending,
		createdAt:    createdAt,
	}, nil
}

// ID returns the run's identifier.
func (r *Run) ID() RunID { return r.id }

// ChainID returns the chain this run verifies.
func (r *Run) ChainID() chain.ChainID { return r.chainID }

// Strategy returns the sampling strategy.
func (r *Run) Strategy() SamplingStrategy { return r.strategy }

// Trigger returns the trigger that produced the run.
func (r *Run) Trigger() Trigger { return r.trigger }

// Status returns the current lifecycle state.
func (r *Run) Status() Status { return r.status }

// CreatedAt returns the timestamp captured at construction.
func (r *Run) CreatedAt() time.Time { return r.createdAt }

// ErrorMessage returns the recorded failure message, empty if the
// run did not fail.
func (r *Run) ErrorMessage() string { return r.errorMsg }

// Metrics returns a defensive copy of the configured metric list so
// callers cannot mutate the aggregate's state by holding onto the
// slice.
func (r *Run) Metrics() []Metric {
	out := make([]Metric, len(r.metrics))
	copy(out, r.metrics)
	return out
}

// AddressPlans returns a defensive copy of the configured address
// sampling plans. A zero-length result means the Run does not cover
// any address-stratum metrics; ExecuteRun skips its address loop in
// that case.
func (r *Run) AddressPlans() []AddressSamplingPlan {
	if len(r.addressPlans) == 0 {
		return nil
	}
	out := make([]AddressSamplingPlan, len(r.addressPlans))
	copy(out, r.addressPlans)
	return out
}

// StartedAt returns a pointer to the started-at timestamp, or nil
// if the run has not left pending. The returned pointer is a copy;
// mutating it does not affect the aggregate.
func (r *Run) StartedAt() *time.Time {
	if r.startedAt == nil {
		return nil
	}
	t := *r.startedAt
	return &t
}

// FinishedAt returns a pointer to the finished-at timestamp, or
// nil if the run has not yet reached a terminal state.
func (r *Run) FinishedAt() *time.Time {
	if r.finishedAt == nil {
		return nil
	}
	t := *r.finishedAt
	return &t
}

// Start transitions pending → running. Returns an error if the
// current status forbids the transition (e.g., the run is already
// running or terminal).
func (r *Run) Start(now time.Time) error {
	if r.status != StatusPending {
		return fmt.Errorf("run: cannot start from status %q", r.status)
	}
	r.status = StatusRunning
	r.startedAt = &now
	return nil
}

// Complete transitions running → completed.
func (r *Run) Complete(now time.Time) error {
	if r.status != StatusRunning {
		return fmt.Errorf("run: cannot complete from status %q", r.status)
	}
	r.status = StatusCompleted
	r.finishedAt = &now
	return nil
}

// Fail transitions pending|running → failed, recording err.Message
// for the audit trail. The nil err case is allowed (operator
// marking a run as failed without a specific cause) but leaves
// ErrorMessage empty.
func (r *Run) Fail(now time.Time, err error) error {
	if r.status != StatusPending && r.status != StatusRunning {
		return fmt.Errorf("run: cannot fail from status %q", r.status)
	}
	r.status = StatusFailed
	r.finishedAt = &now
	if err != nil {
		r.errorMsg = err.Error()
	}
	return nil
}

// Cancel transitions pending|running → cancelled.
func (r *Run) Cancel(now time.Time) error {
	if r.status != StatusPending && r.status != StatusRunning {
		return fmt.Errorf("run: cannot cancel from status %q", r.status)
	}
	r.status = StatusCancelled
	r.finishedAt = &now
	return nil
}
