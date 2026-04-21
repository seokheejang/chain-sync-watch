package application

import (
	"context"
	"errors"
	"fmt"

	"github.com/seokheejang/chain-sync-watch/internal/diff"
	"github.com/seokheejang/chain-sync-watch/internal/source"
	"github.com/seokheejang/chain-sync-watch/internal/verification"
)

// ReplayResult reports what ReplayDiff.Execute decided.
// Resolved=true means the original disagreement no longer holds and
// the DiffRecord was marked resolved. Resolved=false means the
// Sources still disagree and a new DiffRecord was written;
// NewDiffID points at it.
type ReplayResult struct {
	OriginalDiffID DiffID
	Resolved       bool
	NewDiffID      *DiffID
}

// ReplayDiff re-fetches every Source that originally participated in
// a DiffRecord, compares again at the same (block, metric), and
// either marks the record resolved (if values now agree) or
// persists a fresh Discrepancy tied to the same RunID (if they
// still disagree).
//
// Scope in Phase 5C:
//
//   - Only BlockImmutable metrics. Other categories return an error
//     — they need the address-sampling stage that lands in Phase 7.
//   - Replay does NOT re-transition the Run. The original Run
//     remains in whatever terminal state it reached; replays live
//     alongside it as additional DiffRecords.
type ReplayDiff struct {
	Diffs   DiffRepository
	Sources SourceGateway
	Clock   Clock
	Policy  diff.JudgementPolicy
}

// Execute re-runs the comparison for the given DiffID.
func (uc ReplayDiff) Execute(ctx context.Context, id DiffID) (ReplayResult, error) {
	rec, err := uc.Diffs.FindByID(ctx, id)
	if err != nil {
		return ReplayResult{}, err
	}
	if rec.Discrepancy.Metric.Category != verification.CatBlockImmutable {
		return ReplayResult{}, fmt.Errorf(
			"replay diff: category %q not supported in MVP",
			rec.Discrepancy.Metric.Category,
		)
	}

	sources := make([]source.Source, 0, len(rec.Discrepancy.Values))
	for sid := range rec.Discrepancy.Values {
		s, err := uc.Sources.Get(sid)
		if err != nil {
			return ReplayResult{}, fmt.Errorf("replay diff: resolve source %q: %w", sid, err)
		}
		sources = append(sources, s)
	}

	results := fetchBlockAll(ctx, sources, rec.Discrepancy.Block)
	snapshots := map[source.SourceID]diff.ValueSnapshot{}
	for _, fr := range results {
		if fr.err != nil {
			continue
		}
		if !fr.source.Supports(rec.Discrepancy.Metric.Capability) {
			continue
		}
		raw, ok := extractBlockField(rec.Discrepancy.Metric.Capability, fr.result)
		if !ok {
			continue
		}
		snapshots[fr.source.ID()] = diff.ValueSnapshot{
			Raw:       raw,
			FetchedAt: fr.result.FetchedAt,
		}
	}

	if len(snapshots) < 2 {
		return ReplayResult{}, errors.New("replay diff: fewer than 2 sources returned a value")
	}

	if allAgree(snapshots) {
		if err := uc.Diffs.MarkResolved(ctx, id, uc.Clock.Now()); err != nil {
			return ReplayResult{}, fmt.Errorf("replay diff: mark resolved: %w", err)
		}
		return ReplayResult{OriginalDiffID: id, Resolved: true}, nil
	}

	d, err := diff.NewDiscrepancy(
		rec.Discrepancy.RunID,
		rec.Discrepancy.Metric,
		rec.Discrepancy.Block,
		rec.Discrepancy.Subject,
		snapshots,
		uc.Clock.Now(),
	)
	if err != nil {
		return ReplayResult{}, fmt.Errorf("replay diff: build discrepancy: %w", err)
	}
	j := uc.Policy.Judge(d)
	newID, err := uc.Diffs.Save(ctx, &d, j)
	if err != nil {
		return ReplayResult{}, fmt.Errorf("replay diff: save: %w", err)
	}
	return ReplayResult{OriginalDiffID: id, Resolved: false, NewDiffID: &newID}, nil
}
