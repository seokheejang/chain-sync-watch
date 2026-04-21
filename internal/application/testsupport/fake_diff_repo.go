package testsupport

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/seokheejang/chain-sync-watch/internal/application"
	"github.com/seokheejang/chain-sync-watch/internal/diff"
	"github.com/seokheejang/chain-sync-watch/internal/verification"
)

// FakeDiffRepo is an in-memory DiffRepository. Save assigns a
// monotonic "fake-N" DiffID so tests can assert on the order of
// persistence.
type FakeDiffRepo struct {
	mu    sync.Mutex
	seq   int
	byID  map[application.DiffID]*application.DiffRecord
	byRun map[verification.RunID][]application.DiffID
	recs  []*application.DiffRecord
}

// NewFakeDiffRepo returns an empty FakeDiffRepo.
func NewFakeDiffRepo() *FakeDiffRepo {
	return &FakeDiffRepo{
		byID:  map[application.DiffID]*application.DiffRecord{},
		byRun: map[verification.RunID][]application.DiffID{},
	}
}

// Save persists d + j and returns the assigned DiffID. The meta
// fields land on the resulting DiffRecord for callers that want to
// assert on Tier / AnchorBlock / SamplingSeed without going
// through the Postgres mapper.
func (f *FakeDiffRepo) Save(_ context.Context, d *diff.Discrepancy, j diff.Judgement, meta application.SaveDiffMeta) (application.DiffID, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.seq++
	id := application.DiffID(fmt.Sprintf("fake-%d", f.seq))
	rec := &application.DiffRecord{
		ID:           id,
		Discrepancy:  *d,
		Judgement:    j,
		Tier:         meta.Tier,
		AnchorBlock:  meta.AnchorBlock,
		SamplingSeed: meta.SamplingSeed,
	}
	f.byID[id] = rec
	f.byRun[d.RunID] = append(f.byRun[d.RunID], id)
	f.recs = append(f.recs, rec)
	return id, nil
}

// FindByRun returns every DiffRecord saved under runID, in
// insertion order.
func (f *FakeDiffRepo) FindByRun(_ context.Context, runID verification.RunID) ([]application.DiffRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	ids := f.byRun[runID]
	out := make([]application.DiffRecord, 0, len(ids))
	for _, id := range ids {
		out = append(out, *f.byID[id])
	}
	return out, nil
}

// FindByID returns the record or application.ErrDiffNotFound.
func (f *FakeDiffRepo) FindByID(_ context.Context, id application.DiffID) (*application.DiffRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, ok := f.byID[id]
	if !ok {
		return nil, application.ErrDiffNotFound
	}
	cp := *r
	return &cp, nil
}

// List applies the filter and returns matching records in
// descending DetectedAt order.
func (f *FakeDiffRepo) List(_ context.Context, flt application.DiffFilter) ([]application.DiffRecord, int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]application.DiffRecord, 0, len(f.recs))
	for _, r := range f.recs {
		if flt.RunID != nil && r.Discrepancy.RunID != *flt.RunID {
			continue
		}
		if flt.MetricKey != nil && r.Discrepancy.Metric.Key != *flt.MetricKey {
			continue
		}
		if flt.Severity != nil && r.Judgement.Severity != *flt.Severity {
			continue
		}
		if flt.Resolved != nil && r.Resolved != *flt.Resolved {
			continue
		}
		if flt.BlockRange != nil && !flt.BlockRange.Contains(r.Discrepancy.Block) {
			continue
		}
		out = append(out, *r)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Discrepancy.DetectedAt.After(out[j].Discrepancy.DetectedAt)
	})
	total := len(out)
	if flt.Offset > len(out) {
		return nil, total, nil
	}
	out = out[flt.Offset:]
	if flt.Limit > 0 && flt.Limit < len(out) {
		out = out[:flt.Limit]
	}
	return out, total, nil
}

// MarkResolved flips the record's resolved flag and stamps
// ResolvedAt.
func (f *FakeDiffRepo) MarkResolved(_ context.Context, id application.DiffID, at time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, ok := f.byID[id]
	if !ok {
		return application.ErrDiffNotFound
	}
	r.Resolved = true
	t := at
	r.ResolvedAt = &t
	return nil
}

// Count returns the number of records stored. Test helper.
func (f *FakeDiffRepo) Count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.recs)
}
