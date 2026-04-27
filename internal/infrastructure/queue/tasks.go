package queue

import (
	"encoding/json"
	"fmt"
)

// Task type strings. Persisted inside Redis as part of every
// queued job, so changing them would orphan in-flight tasks on
// running deployments. Add new constants here rather than renaming.
const (
	TaskTypeExecuteRun   = "verification:execute_run"
	TaskTypeScheduledRun = "verification:scheduled_run"
	TaskTypePruneOldRuns = "maintenance:prune_old_runs"
)

// Queue names. Tier-aware weights land in Phase 7D; for now every
// run lives on the default queue.
const (
	QueueDefault = "default"
)

// ExecuteRunPayload is the asynq payload for a one-off Run
// execution. The payload is intentionally minimal — the worker
// rehydrates everything else from the RunRepository so we never
// ship a stale snapshot of the Run through Redis.
type ExecuteRunPayload struct {
	RunID string `json:"run_id"`
}

// Marshal serialises the payload. Returning ([]byte, error)
// matches the asynq Task constructor signature.
func (p ExecuteRunPayload) Marshal() ([]byte, error) {
	b, err := json.Marshal(p)
	if err != nil {
		return nil, fmt.Errorf("queue: marshal execute_run payload: %w", err)
	}
	return b, nil
}

// UnmarshalExecuteRunPayload decodes a payload from a task body.
func UnmarshalExecuteRunPayload(data []byte) (ExecuteRunPayload, error) {
	var p ExecuteRunPayload
	if err := json.Unmarshal(data, &p); err != nil {
		return ExecuteRunPayload{}, fmt.Errorf("queue: decode execute_run payload: %w", err)
	}
	if p.RunID == "" {
		return ExecuteRunPayload{}, fmt.Errorf("queue: execute_run payload missing run_id")
	}
	return p, nil
}

// ScheduledRunPayload is the asynq payload for a recurring,
// scheduler-fired Run. The scheduler creates these periodically
// from a stored PeriodicTaskConfig; the worker-side handler
// constructs a fresh Run and invokes the execution pipeline.
//
// StrategyData / AddressPlansData / TokenPlansData are opaque JSON
// blobs — the handler decodes them via the same serialise helpers
// the persistence layer uses, which keeps the queue package free
// of domain-specific unmarshal logic.
//
// AddressPlansData / TokenPlansData may each be empty (nil / "[]")
// — that signals "no address stratum" or "no token stratum" and is
// the default for cron-scheduled Runs that only check
// block-immutable fields.
type ScheduledRunPayload struct {
	ChainID          uint64   `json:"chain_id"`
	StrategyKind     string   `json:"strategy_kind"`
	StrategyData     []byte   `json:"strategy_data"`
	MetricKeys       []string `json:"metric_keys"`
	AddressPlansData []byte   `json:"address_plans_data,omitempty"`
	TokenPlansData   []byte   `json:"token_plans_data,omitempty"`
	CronExpr         string   `json:"cron_expr"`
}

// Marshal serialises the payload.
func (p ScheduledRunPayload) Marshal() ([]byte, error) {
	b, err := json.Marshal(p)
	if err != nil {
		return nil, fmt.Errorf("queue: marshal scheduled_run payload: %w", err)
	}
	return b, nil
}

// UnmarshalScheduledRunPayload decodes a scheduled-run payload and
// enforces the minimum structural invariants (non-zero chain, non-
// empty strategy kind, at least one metric key).
func UnmarshalScheduledRunPayload(data []byte) (ScheduledRunPayload, error) {
	var p ScheduledRunPayload
	if err := json.Unmarshal(data, &p); err != nil {
		return ScheduledRunPayload{}, fmt.Errorf("queue: decode scheduled_run payload: %w", err)
	}
	if p.ChainID == 0 {
		return ScheduledRunPayload{}, fmt.Errorf("queue: scheduled_run payload missing chain_id")
	}
	if p.StrategyKind == "" {
		return ScheduledRunPayload{}, fmt.Errorf("queue: scheduled_run payload missing strategy_kind")
	}
	if len(p.MetricKeys) == 0 {
		return ScheduledRunPayload{}, fmt.Errorf("queue: scheduled_run payload missing metric_keys")
	}
	return p, nil
}

// PruneOldRunsPayload is the body for the retention sweep. Days is
// the age threshold: runs with finished_at older than that get
// deleted (CASCADE cleans discrepancies).
type PruneOldRunsPayload struct {
	Days int `json:"days"`
}

// Marshal serialises the payload.
func (p PruneOldRunsPayload) Marshal() ([]byte, error) {
	b, err := json.Marshal(p)
	if err != nil {
		return nil, fmt.Errorf("queue: marshal prune_old_runs payload: %w", err)
	}
	return b, nil
}

// UnmarshalPruneOldRunsPayload decodes the sweep payload and rejects
// non-positive Days — a 0-day threshold would wipe every terminal
// run and is almost certainly a config mistake.
func UnmarshalPruneOldRunsPayload(data []byte) (PruneOldRunsPayload, error) {
	var p PruneOldRunsPayload
	if err := json.Unmarshal(data, &p); err != nil {
		return PruneOldRunsPayload{}, fmt.Errorf("queue: decode prune_old_runs payload: %w", err)
	}
	if p.Days <= 0 {
		return PruneOldRunsPayload{}, fmt.Errorf("queue: prune_old_runs days must be > 0 (got %d)", p.Days)
	}
	return p, nil
}
