package probe

import (
	"errors"
	"fmt"
	"time"
)

// IncidentID is the storage-stable handle for an Incident. The
// application layer mints it (UUID typically); the domain treats it
// as opaque.
type IncidentID string

// String returns the underlying identifier.
func (i IncidentID) String() string { return string(i) }

// Breach captures which Threshold tripped and the value the
// evaluator measured at trip time. Persisting the observed value
// alongside the threshold lets dashboards render "p95 was 3450ms vs
// 2000ms target" without re-running the aggregation.
type Breach struct {
	Metric    ThresholdMetric
	Threshold float64
	Observed  float64
	WindowSec int
}

// Validate sanity-checks the breach payload. The domain rejects an
// empty metric (storage corruption) and Observed values that fail
// the "this exceeded the threshold" invariant — opening an Incident
// for a recovered metric is a programming error worth catching.
func (b Breach) Validate() error {
	if !b.Metric.Valid() {
		return fmt.Errorf("breach: invalid metric %q", b.Metric)
	}
	if b.Threshold < 0 {
		return fmt.Errorf("breach: threshold negative: %v", b.Threshold)
	}
	if b.Metric != ThresholdConsecutiveFailures && b.WindowSec <= 0 {
		return errors.New("breach: window_sec must be > 0")
	}
	if b.Observed < b.Threshold {
		return fmt.Errorf("breach: observed %v does not exceed threshold %v", b.Observed, b.Threshold)
	}
	return nil
}

// IncidentStatus is the state machine of an Incident. Open means the
// Breach is still tripped at the most recent evaluation; Closed means
// the metric recovered and the evaluator paired the close with a
// concrete recovery time.
type IncidentStatus string

const (
	// StatusOpen — the Incident is currently active.
	StatusOpen IncidentStatus = "open"
	// StatusClosed — the metric returned below the threshold and the
	// evaluator stamped a closure time.
	StatusClosed IncidentStatus = "closed"
)

// Incident is the window-aggregate result of a tripped Threshold.
// Lifecycle: Open via NewIncident, transition to Closed via Close.
// Re-opening is intentionally not a domain operation — the evaluator
// mints a new Incident if the metric trips again.
//
// Evidence carries a small slice of the most recent Observations at
// trip time so a dashboard tooltip can display them without an extra
// query. The application layer caps the slice size; the domain only
// enforces non-emptiness.
type Incident struct {
	id       IncidentID
	probeID  ProbeID
	openedAt time.Time
	closedAt *time.Time
	breach   Breach
	evidence []Observation
}

// NewIncident opens a new Incident. evidence may be empty if the
// caller hasn't sampled any Observations yet, but that's a curious
// state — most evaluators have at least the trip-causing batch.
func NewIncident(
	id IncidentID,
	probeID ProbeID,
	openedAt time.Time,
	breach Breach,
	evidence []Observation,
) (Incident, error) {
	if id == "" {
		return Incident{}, errors.New("incident: id is empty")
	}
	if probeID == "" {
		return Incident{}, errors.New("incident: probe id is empty")
	}
	if openedAt.IsZero() {
		return Incident{}, errors.New("incident: opened_at is zero")
	}
	if err := breach.Validate(); err != nil {
		return Incident{}, fmt.Errorf("incident: %w", err)
	}
	ev := make([]Observation, len(evidence))
	copy(ev, evidence)
	return Incident{
		id:       id,
		probeID:  probeID,
		openedAt: openedAt,
		breach:   breach,
		evidence: ev,
	}, nil
}

// Rehydrate rebuilds an Incident from persisted state without
// re-running validation. Same rationale as Probe.Rehydrate — the
// mapper is the source of truth for storage consistency.
func RehydrateIncident(
	id IncidentID,
	probeID ProbeID,
	openedAt time.Time,
	closedAt *time.Time,
	breach Breach,
	evidence []Observation,
) Incident {
	ev := make([]Observation, len(evidence))
	copy(ev, evidence)
	var ca *time.Time
	if closedAt != nil {
		v := *closedAt
		ca = &v
	}
	return Incident{
		id:       id,
		probeID:  probeID,
		openedAt: openedAt,
		closedAt: ca,
		breach:   breach,
		evidence: ev,
	}
}

// Close transitions Open → Closed. Calling Close on an already-closed
// Incident is a programming error (the evaluator should mint a new
// Incident on a re-trip), so it returns an error rather than silently
// updating closedAt.
func (i *Incident) Close(at time.Time) error {
	if i.closedAt != nil {
		return errors.New("incident: already closed")
	}
	if at.IsZero() {
		return errors.New("incident: closed_at is zero")
	}
	if at.Before(i.openedAt) {
		return fmt.Errorf("incident: closed_at %s precedes opened_at %s", at, i.openedAt)
	}
	v := at
	i.closedAt = &v
	return nil
}

// ID returns the storage handle.
func (i Incident) ID() IncidentID { return i.id }

// ProbeID returns the Probe this Incident belongs to.
func (i Incident) ProbeID() ProbeID { return i.probeID }

// OpenedAt returns the trip time.
func (i Incident) OpenedAt() time.Time { return i.openedAt }

// ClosedAt returns the recovery time, or nil if still open.
func (i Incident) ClosedAt() *time.Time {
	if i.closedAt == nil {
		return nil
	}
	v := *i.closedAt
	return &v
}

// Status reports Open or Closed.
func (i Incident) Status() IncidentStatus {
	if i.closedAt == nil {
		return StatusOpen
	}
	return StatusClosed
}

// Breach returns the threshold/observed pair that opened this
// Incident.
func (i Incident) Breach() Breach { return i.breach }

// Evidence returns a defensive copy of the captured Observations.
func (i Incident) Evidence() []Observation {
	out := make([]Observation, len(i.evidence))
	copy(out, i.evidence)
	return out
}

// Duration reports how long the Incident lasted. For open incidents
// the second arg ("now") is used as the upper bound, which keeps the
// helper pure (no global clock).
func (i Incident) Duration(now time.Time) time.Duration {
	end := now
	if i.closedAt != nil {
		end = *i.closedAt
	}
	if end.Before(i.openedAt) {
		return 0
	}
	return end.Sub(i.openedAt)
}
