package application

import (
	"context"
	"time"

	"github.com/seokheejang/chain-sync-watch/internal/chain"
	"github.com/seokheejang/chain-sync-watch/internal/diff"
	"github.com/seokheejang/chain-sync-watch/internal/source"
	"github.com/seokheejang/chain-sync-watch/internal/verification"
)

// --- DTOs ------------------------------------------------------------
// These types exist at the use-case boundary, not in the domain.
// Persistence, queue, and HTTP layers all speak this shape.

// RunFilter constrains RunRepository.List queries. Pointer fields
// are optional filters — a nil pointer means "don't filter on this
// field". Limit = 0 uses the repository's default page size.
type RunFilter struct {
	ChainID   *chain.ChainID
	Status    *verification.Status
	CreatedAt *TimeRange
	Limit     int
	Offset    int
}

// TimeRange is an inclusive [From, To] interval for filter DTOs.
type TimeRange struct {
	From time.Time
	To   time.Time
}

// DiffFilter constrains DiffRepository.List queries.
type DiffFilter struct {
	RunID      *verification.RunID
	MetricKey  *string
	Severity   *diff.Severity
	Resolved   *bool
	BlockRange *chain.BlockRange
	Limit      int
	Offset     int
}

// DiffID is the persistence-assigned identifier for a stored
// Discrepancy + Judgement pair. The domain Discrepancy does not
// carry it — it is meaningful only after persistence has written
// the record.
type DiffID string

// DiffRecord is the read model the application hands back to
// transport and persistence layers: the raw Discrepancy, the
// rendered Judgement, resolution state, and the Tier / Anchor /
// SamplingSeed meta that lets operators audit or replay the
// comparison deterministically.
type DiffRecord struct {
	ID          DiffID
	Discrepancy diff.Discrepancy
	Judgement   diff.Judgement
	Resolved    bool
	ResolvedAt  *time.Time

	Tier         source.Tier
	AnchorBlock  chain.BlockNumber
	SamplingSeed *int64
}

// JobID identifies a dispatched or scheduled job in the underlying
// queue (Phase 7 wires it to asynq task IDs).
type JobID string

// SchedulePayload carries the configuration a recurring job needs
// to materialise a Run at firing time. The Trigger is filled in by
// the scheduler itself (ScheduledTrigger with the cron expression
// that fired).
type SchedulePayload struct {
	ChainID  chain.ChainID
	Metrics  []verification.Metric
	Strategy verification.SamplingStrategy
}

// --- Ports -----------------------------------------------------------

// RunRepository persists Run aggregates. Implementations must:
//
//   - Return ErrRunNotFound for missing ids so callers can match on
//     errors.Is without knowing the backing store.
//   - Return ErrDuplicateRun on Save when the RunID already exists.
//   - Be safe for concurrent use from multiple use-case invocations.
type RunRepository interface {
	Save(ctx context.Context, r *verification.Run) error
	FindByID(ctx context.Context, id verification.RunID) (*verification.Run, error)
	List(ctx context.Context, f RunFilter) (runs []*verification.Run, total int, err error)
}

// SaveDiffMeta carries the verification-time metadata that lives
// alongside a DiffRecord but is not part of the Discrepancy or
// Judgement domain objects themselves: the Tier of the metric at
// save time, the Run's anchor block, and (for Tier B sampled
// metrics) the seed that produced the address or block set.
//
// A zero value is legal — missing fields translate to NULL columns
// in persistence and an unresolved tier on the read model. Callers
// fill in what they can: ExecuteRun derives Tier from the metric's
// Capability and the anchor from ChainHead.Finalized; ReplayDiff
// carries the meta forward from the record it is re-verifying.
type SaveDiffMeta struct {
	Tier         source.Tier
	AnchorBlock  chain.BlockNumber
	SamplingSeed *int64
}

// DiffRepository persists Discrepancy + Judgement pairs as
// DiffRecords. Save returns the assigned DiffID so the caller can
// pair Runs with their diffs without a second query.
type DiffRepository interface {
	Save(ctx context.Context, d *diff.Discrepancy, j diff.Judgement, meta SaveDiffMeta) (DiffID, error)
	FindByRun(ctx context.Context, runID verification.RunID) ([]DiffRecord, error)
	FindByID(ctx context.Context, id DiffID) (*DiffRecord, error)
	List(ctx context.Context, f DiffFilter) (records []DiffRecord, total int, err error)
	MarkResolved(ctx context.Context, id DiffID, at time.Time) error
}

// SourceGateway resolves configured Sources for a chain. Used by
// ExecuteRun to fan out fetches across every registered Source and
// by ReplayDiff to re-query specific participants.
type SourceGateway interface {
	ForChain(chainID chain.ChainID) ([]source.Source, error)
	Get(sourceID source.SourceID) (source.Source, error)
}

// JobDispatcher enqueues or schedules Run-related work. The
// application layer knows nothing of the concrete queue; Phase 7
// binds this to asynq.
type JobDispatcher interface {
	EnqueueRunExecution(ctx context.Context, runID verification.RunID) error
	ScheduleRecurring(ctx context.Context, schedule verification.Schedule, payload SchedulePayload) (JobID, error)
	CancelScheduled(ctx context.Context, id JobID) error
}

// Clock is the time source used by use cases that need to stamp
// state transitions (Run.Start, Run.Complete, DiffRecord.ResolvedAt).
// Injecting the clock keeps use-case tests deterministic.
type Clock interface {
	Now() time.Time
}

// ChainHead reports chain-level heights. Tip is the current latest;
// Finalized is the canonical anchor for verification (Optimism
// supports the "finalized" tag natively). ExecuteRun resolves the
// anchor once at Run start and uses it across every comparison so
// all Sources are compared against the same snapshot.
type ChainHead interface {
	Tip(ctx context.Context, chainID chain.ChainID) (chain.BlockNumber, error)
	Finalized(ctx context.Context, chainID chain.ChainID) (chain.BlockNumber, error)
}

// RateLimitBudget gates Tier B (and some Tier C) fetches against
// per-source request budgets. Reserve returns ErrBudgetExhausted
// when no quota remains; Refund releases units when a reserved
// call was skipped for reasons unrelated to the limit. Phase 7
// ships the concrete implementation; Phase 5 uses only the port.
type RateLimitBudget interface {
	Reserve(ctx context.Context, sourceID source.SourceID, n int) error
	Refund(ctx context.Context, sourceID source.SourceID, n int) error
}
