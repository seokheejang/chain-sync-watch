package testsupport

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/seokheejang/chain-sync-watch/internal/application"
	"github.com/seokheejang/chain-sync-watch/internal/verification"
)

// EnqueuedRun is one recorded EnqueueRunExecution call.
type EnqueuedRun struct {
	RunID verification.RunID
}

// ScheduledJob is one recorded ScheduleRecurring call.
type ScheduledJob struct {
	JobID    application.JobID
	Schedule verification.Schedule
	Payload  application.SchedulePayload
	Cancel   bool
}

// FakeDispatcher records every call so tests can assert on the
// sequence of dispatches. It never blocks on the queue — enqueues
// and schedule calls return immediately.
type FakeDispatcher struct {
	mu        sync.Mutex
	enqueued  []EnqueuedRun
	scheduled map[application.JobID]*ScheduledJob
	seq       int

	// EnqueueErr, if set, makes EnqueueRunExecution fail. Useful
	// for the "dispatcher down" scenario.
	EnqueueErr error
}

// NewFakeDispatcher returns an empty FakeDispatcher.
func NewFakeDispatcher() *FakeDispatcher {
	return &FakeDispatcher{scheduled: map[application.JobID]*ScheduledJob{}}
}

// EnqueueRunExecution records an enqueue. Returns EnqueueErr when
// set.
func (f *FakeDispatcher) EnqueueRunExecution(_ context.Context, id verification.RunID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.EnqueueErr != nil {
		return f.EnqueueErr
	}
	f.enqueued = append(f.enqueued, EnqueuedRun{RunID: id})
	return nil
}

// ScheduleRecurring records a schedule and returns a synthetic
// JobID.
func (f *FakeDispatcher) ScheduleRecurring(_ context.Context, s verification.Schedule, p application.SchedulePayload) (application.JobID, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.seq++
	id := application.JobID(fmt.Sprintf("job-%d", f.seq))
	f.scheduled[id] = &ScheduledJob{JobID: id, Schedule: s, Payload: p}
	return id, nil
}

// CancelScheduled flags the recorded job as cancelled. Returns an
// error if the id is unknown.
func (f *FakeDispatcher) CancelScheduled(_ context.Context, id application.JobID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	j, ok := f.scheduled[id]
	if !ok {
		return errors.New("fake dispatcher: unknown job id")
	}
	j.Cancel = true
	return nil
}

// Enqueued returns a copy of the enqueued list.
func (f *FakeDispatcher) Enqueued() []EnqueuedRun {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]EnqueuedRun, len(f.enqueued))
	copy(out, f.enqueued)
	return out
}

// Scheduled returns a copy of every registered recurring job.
func (f *FakeDispatcher) Scheduled() []ScheduledJob {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]ScheduledJob, 0, len(f.scheduled))
	for _, j := range f.scheduled {
		out = append(out, *j)
	}
	return out
}
