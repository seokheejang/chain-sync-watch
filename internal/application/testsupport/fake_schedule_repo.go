package testsupport

import (
	"context"
	"sort"
	"sync"

	"github.com/seokheejang/chain-sync-watch/internal/application"
)

// FakeScheduleRepo is an in-memory ScheduleRepository. Deactivate
// flips Active to false in place rather than deleting the entry so
// tests can assert on cancellation audit trails the same way
// production does.
type FakeScheduleRepo struct {
	mu      sync.Mutex
	byID    map[application.JobID]application.ScheduleRecord
	SaveErr error
}

// NewFakeScheduleRepo returns an empty FakeScheduleRepo.
func NewFakeScheduleRepo() *FakeScheduleRepo {
	return &FakeScheduleRepo{byID: map[application.JobID]application.ScheduleRecord{}}
}

// Save upserts. Returns SaveErr when non-nil.
func (f *FakeScheduleRepo) Save(_ context.Context, s application.ScheduleRecord) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.SaveErr != nil {
		return f.SaveErr
	}
	f.byID[s.JobID] = s
	return nil
}

// Deactivate flips Active to false. Missing id is a no-op.
func (f *FakeScheduleRepo) Deactivate(_ context.Context, id application.JobID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	rec, ok := f.byID[id]
	if !ok {
		return nil
	}
	rec.Active = false
	f.byID[id] = rec
	return nil
}

// ListActive returns Active=true records sorted by CreatedAt ASC.
func (f *FakeScheduleRepo) ListActive(_ context.Context) ([]application.ScheduleRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]application.ScheduleRecord, 0, len(f.byID))
	for _, r := range f.byID {
		if r.Active {
			out = append(out, r)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}

// Count is a test helper — returns total records (active + inactive).
func (f *FakeScheduleRepo) Count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.byID)
}
