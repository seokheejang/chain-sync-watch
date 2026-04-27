package testsupport

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	probeapp "github.com/seokheejang/chain-sync-watch/internal/application/probe"
	"github.com/seokheejang/chain-sync-watch/internal/probe"
)

// --- ProbeRepo --------------------------------------------------------

// FakeProbeRepo is an in-memory ProbeRepository.
type FakeProbeRepo struct {
	mu   sync.Mutex
	byID map[probe.ProbeID]probe.Probe
}

// NewFakeProbeRepo returns an empty FakeProbeRepo.
func NewFakeProbeRepo() *FakeProbeRepo {
	return &FakeProbeRepo{byID: map[probe.ProbeID]probe.Probe{}}
}

// Save upserts the Probe.
func (f *FakeProbeRepo) Save(_ context.Context, p probe.Probe) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.byID[p.ID()] = p
	return nil
}

// FindByID returns the stored Probe or ErrProbeNotFound.
func (f *FakeProbeRepo) FindByID(_ context.Context, id probe.ProbeID) (*probe.Probe, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p, ok := f.byID[id]
	if !ok {
		return nil, probeapp.ErrProbeNotFound
	}
	return &p, nil
}

// List returns all stored probes honouring EnabledOnly + pagination.
func (f *FakeProbeRepo) List(_ context.Context, flt probeapp.ProbeFilter) ([]probe.Probe, int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	all := make([]probe.Probe, 0, len(f.byID))
	for _, p := range f.byID {
		if flt.EnabledOnly && !p.Enabled() {
			continue
		}
		all = append(all, p)
	}
	sort.Slice(all, func(i, j int) bool { return all[i].ID() < all[j].ID() })
	total := len(all)
	if flt.Offset > 0 {
		if flt.Offset >= len(all) {
			return nil, total, nil
		}
		all = all[flt.Offset:]
	}
	if flt.Limit > 0 && flt.Limit < len(all) {
		all = all[:flt.Limit]
	}
	return all, total, nil
}

// Delete removes the Probe by id. Missing ids are no-ops, matching
// the real repository's idempotent contract.
func (f *FakeProbeRepo) Delete(_ context.Context, id probe.ProbeID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.byID, id)
	return nil
}

// --- ObservationRepo --------------------------------------------------

// FakeObservationRepo is an in-memory ObservationRepository. Storage
// is a flat slice — fine at MVP test volumes.
type FakeObservationRepo struct {
	mu   sync.Mutex
	rows []probe.Observation
}

// NewFakeObservationRepo returns an empty FakeObservationRepo.
func NewFakeObservationRepo() *FakeObservationRepo {
	return &FakeObservationRepo{}
}

// Save appends the observation.
func (f *FakeObservationRepo) Save(_ context.Context, o probe.Observation) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rows = append(f.rows, o)
	return nil
}

// SaveBatch appends every observation atomically (under the mu lock).
func (f *FakeObservationRepo) SaveBatch(_ context.Context, obs []probe.Observation) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rows = append(f.rows, obs...)
	return nil
}

// ListWindow returns observations whose At ∈ [since, until] for the
// given probeID, sorted by timestamp ascending.
func (f *FakeObservationRepo) ListWindow(
	_ context.Context,
	probeID probe.ProbeID,
	since time.Time,
	until time.Time,
) ([]probe.Observation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]probe.Observation, 0, len(f.rows))
	for _, o := range f.rows {
		if o.ProbeID != probeID {
			continue
		}
		if o.At.Before(since) || o.At.After(until) {
			continue
		}
		out = append(out, o)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].At.Before(out[j].At) })
	return out, nil
}

// PruneBefore drops every observation with At < cutoff and returns
// the affected count.
func (f *FakeObservationRepo) PruneBefore(_ context.Context, cutoff time.Time) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	kept := f.rows[:0]
	pruned := int64(0)
	for _, o := range f.rows {
		if o.At.Before(cutoff) {
			pruned++
			continue
		}
		kept = append(kept, o)
	}
	f.rows = kept
	return pruned, nil
}

// Len reports the number of stored observations. Test helper.
func (f *FakeObservationRepo) Len() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.rows)
}

// --- IncidentRepo -----------------------------------------------------

// FakeIncidentRepo is an in-memory IncidentRepository keyed by
// IncidentID with a secondary index on (probeID, metric, status=open)
// rebuilt on every FindOpen call.
type FakeIncidentRepo struct {
	mu   sync.Mutex
	byID map[probe.IncidentID]probe.Incident
}

// NewFakeIncidentRepo returns an empty FakeIncidentRepo.
func NewFakeIncidentRepo() *FakeIncidentRepo {
	return &FakeIncidentRepo{byID: map[probe.IncidentID]probe.Incident{}}
}

// Save upserts the Incident.
func (f *FakeIncidentRepo) Save(_ context.Context, i probe.Incident) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.byID[i.ID()] = i
	return nil
}

// FindOpen returns the open Incident matching (probeID, metric) or
// nil if none. Implementations are free to assume at most one open
// row per pair — EvaluateWindow's idempotency depends on it.
func (f *FakeIncidentRepo) FindOpen(
	_ context.Context,
	probeID probe.ProbeID,
	metric probe.ThresholdMetric,
) (*probe.Incident, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, i := range f.byID {
		if i.ProbeID() != probeID {
			continue
		}
		if i.Breach().Metric != metric {
			continue
		}
		if i.Status() != probe.StatusOpen {
			continue
		}
		v := i
		return &v, nil
	}
	return nil, nil
}

// List returns Incidents honouring the filter.
func (f *FakeIncidentRepo) List(
	_ context.Context,
	flt probeapp.IncidentFilter,
) ([]probe.Incident, int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]probe.Incident, 0, len(f.byID))
	for _, i := range f.byID {
		if flt.ProbeID != nil && i.ProbeID() != *flt.ProbeID {
			continue
		}
		if flt.Status != nil && i.Status() != *flt.Status {
			continue
		}
		if flt.OpenedFrom != nil && i.OpenedAt().Before(*flt.OpenedFrom) {
			continue
		}
		if flt.OpenedTo != nil && i.OpenedAt().After(*flt.OpenedTo) {
			continue
		}
		out = append(out, i)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].OpenedAt().After(out[j].OpenedAt()) })
	total := len(out)
	if flt.Offset > 0 {
		if flt.Offset >= len(out) {
			return nil, total, nil
		}
		out = out[flt.Offset:]
	}
	if flt.Limit > 0 && flt.Limit < len(out) {
		out = out[:flt.Limit]
	}
	return out, total, nil
}

// CloseAt closes the Incident with id, stamping at as the closure
// time. Re-applies the domain Close method so the same lifecycle
// rules (no double-close, monotonic times) hold for the fake.
func (f *FakeIncidentRepo) CloseAt(
	_ context.Context,
	id probe.IncidentID,
	at time.Time,
) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	i, ok := f.byID[id]
	if !ok {
		return probeapp.ErrIncidentNotFound
	}
	if err := i.Close(at); err != nil {
		return err
	}
	f.byID[id] = i
	return nil
}

// --- HTTPProber -------------------------------------------------------

// FakeHTTPProber returns a ProbeResult from a configurable Sequence,
// looping back to the start when callers exhaust it. Simpler tests
// can leave Sequence empty and set Static instead — every Probe()
// call then returns the same result.
type FakeHTTPProber struct {
	mu       sync.Mutex
	Static   *probeapp.ProbeResult
	Sequence []probeapp.ProbeResult
	idx      int
	calls    int
}

// Probe implements HTTPProber.
func (f *FakeHTTPProber) Probe(
	_ context.Context,
	_ probe.ProbeTarget,
	_ time.Duration,
) probeapp.ProbeResult {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.Static != nil {
		return *f.Static
	}
	if len(f.Sequence) == 0 {
		return probeapp.ProbeResult{StatusCode: 200, ErrorClass: probe.ErrorNone}
	}
	r := f.Sequence[f.idx%len(f.Sequence)]
	f.idx++
	return r
}

// Calls reports how many times Probe was invoked.
func (f *FakeHTTPProber) Calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// --- IDGen ------------------------------------------------------------

// FakeIDGen mints monotonically increasing IncidentIDs of the form
// "inc-N". Tests can predict the next id without injecting a clock.
type FakeIDGen struct {
	mu sync.Mutex
	n  int
}

// NewIncidentID implements IDGen.
func (f *FakeIDGen) NewIncidentID() probe.IncidentID {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.n++
	return probe.IncidentID(fmt.Sprintf("inc-%d", f.n))
}
