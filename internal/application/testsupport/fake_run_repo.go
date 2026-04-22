package testsupport

import (
	"context"
	"sort"
	"sync"

	"github.com/seokheejang/chain-sync-watch/internal/application"
	"github.com/seokheejang/chain-sync-watch/internal/verification"
)

// FakeRunRepo is an in-memory RunRepository. Save is upsert by
// design — state transitions call Save repeatedly for the same
// RunID. Duplicate detection for new runs is the ScheduleRun use
// case's responsibility (it checks FindByID first).
//
// SaveErr lets tests inject a transient Save failure (e.g., to
// verify retry classification on the scheduled-run handler path).
// When non-nil it is returned verbatim from every Save call and no
// mutation happens.
type FakeRunRepo struct {
	mu      sync.Mutex
	byID    map[verification.RunID]*verification.Run
	SaveErr error
}

// NewFakeRunRepo returns an empty FakeRunRepo.
func NewFakeRunRepo() *FakeRunRepo {
	return &FakeRunRepo{byID: map[verification.RunID]*verification.Run{}}
}

// Save persists r. Create or update; never errors on duplicate ID
// unless SaveErr is set.
func (f *FakeRunRepo) Save(_ context.Context, r *verification.Run) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.SaveErr != nil {
		return f.SaveErr
	}
	f.byID[r.ID()] = r
	return nil
}

// FindByID returns the stored Run or application.ErrRunNotFound.
func (f *FakeRunRepo) FindByID(_ context.Context, id verification.RunID) (*verification.Run, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, ok := f.byID[id]
	if !ok {
		return nil, application.ErrRunNotFound
	}
	return r, nil
}

// List returns stored Runs honouring the filter. Ordering is
// descending CreatedAt so the newest run appears first — matches
// the default the real repository uses.
func (f *FakeRunRepo) List(_ context.Context, flt application.RunFilter) ([]*verification.Run, int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]*verification.Run, 0, len(f.byID))
	for _, r := range f.byID {
		if flt.ChainID != nil && r.ChainID() != *flt.ChainID {
			continue
		}
		if flt.Status != nil && r.Status() != *flt.Status {
			continue
		}
		if flt.CreatedAt != nil {
			c := r.CreatedAt()
			if c.Before(flt.CreatedAt.From) || c.After(flt.CreatedAt.To) {
				continue
			}
		}
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].CreatedAt().After(out[j].CreatedAt())
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

// Count returns the number of stored runs. Test helper.
func (f *FakeRunRepo) Count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.byID)
}
