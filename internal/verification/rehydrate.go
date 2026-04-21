package verification

import (
	"errors"
	"time"

	"github.com/seokheejang/chain-sync-watch/internal/chain"
)

// Rehydrate reconstructs a Run from its persisted form. Unlike
// NewRun, which is the application-layer constructor that creates a
// fresh Run in the pending state, Rehydrate is the persistence-
// layer constructor — it trusts the inputs (no state-machine
// validation) so a mapper can rebuild a Run at any Status with its
// original timestamps intact.
//
// Rehydrate still rejects structurally broken inputs: empty id,
// zero chain id, nil strategy, empty metrics, nil trigger. Those
// are schema-level invariants the database must uphold.
//
// Callers outside of repository mappers should not use Rehydrate —
// its purpose is to support the DDD boundary between the domain
// aggregate and its storage representation.
func Rehydrate(
	id RunID,
	cid chain.ChainID,
	strategy SamplingStrategy,
	metrics []Metric,
	trigger Trigger,
	status Status,
	createdAt time.Time,
	startedAt *time.Time,
	finishedAt *time.Time,
	errorMsg string,
) (*Run, error) {
	if id == "" {
		return nil, errors.New("rehydrate run: id is empty")
	}
	if cid == 0 {
		return nil, errors.New("rehydrate run: chain id is zero")
	}
	if strategy == nil {
		return nil, errors.New("rehydrate run: sampling strategy is nil")
	}
	if len(metrics) == 0 {
		return nil, errors.New("rehydrate run: metrics list is empty")
	}
	if trigger == nil {
		return nil, errors.New("rehydrate run: trigger is nil")
	}
	if status == "" {
		return nil, errors.New("rehydrate run: status is empty")
	}

	m := make([]Metric, len(metrics))
	copy(m, metrics)

	var startedCopy, finishedCopy *time.Time
	if startedAt != nil {
		t := *startedAt
		startedCopy = &t
	}
	if finishedAt != nil {
		t := *finishedAt
		finishedCopy = &t
	}

	return &Run{
		id:         id,
		chainID:    cid,
		strategy:   strategy,
		metrics:    m,
		trigger:    trigger,
		status:     status,
		createdAt:  createdAt,
		startedAt:  startedCopy,
		finishedAt: finishedCopy,
		errorMsg:   errorMsg,
	}, nil
}
