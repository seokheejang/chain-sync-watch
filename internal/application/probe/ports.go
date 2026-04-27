package probeapp

import (
	"context"
	"time"

	"github.com/seokheejang/chain-sync-watch/internal/probe"
)

// --- DTOs ------------------------------------------------------------

// ProbeFilter constrains ProbeRepository.List queries. EnabledOnly is
// the scheduler's normal path: it walks active probes only.
type ProbeFilter struct {
	EnabledOnly bool
	Limit       int
	Offset      int
}

// IncidentFilter constrains IncidentRepository.List queries.
type IncidentFilter struct {
	ProbeID    *probe.ProbeID
	Status     *probe.IncidentStatus
	OpenedFrom *time.Time
	OpenedTo   *time.Time
	Limit      int
	Offset     int
}

// ProbeResult is the raw measurement an HTTPProber returns. Use
// cases stitch this together with the probe ID and an authoritative
// timestamp from Clock to construct a probe.Observation. Keeping the
// timestamp out of the prober keeps the network adapter's mock surface
// small — tests don't need to stub a clock to get deterministic
// observation payloads.
type ProbeResult struct {
	ElapsedMS  int64
	StatusCode int
	ErrorClass probe.ErrorClass
	ErrorMsg   string
}

// --- Ports -----------------------------------------------------------

// ProbeRepository persists Probe aggregates. Save is upsert by
// design — operators tweak thresholds or schedules and resave the
// same ProbeID.
type ProbeRepository interface {
	Save(ctx context.Context, p probe.Probe) error
	FindByID(ctx context.Context, id probe.ProbeID) (*probe.Probe, error)
	List(ctx context.Context, f ProbeFilter) (probes []probe.Probe, total int, err error)
	Delete(ctx context.Context, id probe.ProbeID) error
}

// ObservationRepository is the high-volume table. The window query
// is the hot read path EvaluateWindow uses; PruneBefore enforces the
// retention policy on the cold tail.
//
// SaveBatch is offered alongside Save for the per-probe scheduler
// loop: a probe firing every second over an hour produces 3600 rows,
// and an INSERT fan-out per row is wasteful. Implementations that
// can't batch may forward to Save in a loop.
type ObservationRepository interface {
	Save(ctx context.Context, o probe.Observation) error
	SaveBatch(ctx context.Context, obs []probe.Observation) error
	ListWindow(
		ctx context.Context,
		probeID probe.ProbeID,
		since time.Time,
		until time.Time,
	) ([]probe.Observation, error)
	PruneBefore(ctx context.Context, cutoff time.Time) (int64, error)
}

// IncidentRepository persists Incidents. FindOpen is the trip-time
// idempotency check: if an Incident is already open for (probeID,
// metric), EvaluateWindow does nothing. CloseAt updates only the
// closure timestamp without rewriting the rest of the row.
type IncidentRepository interface {
	Save(ctx context.Context, i probe.Incident) error
	FindOpen(
		ctx context.Context,
		probeID probe.ProbeID,
		metric probe.ThresholdMetric,
	) (*probe.Incident, error)
	List(ctx context.Context, f IncidentFilter) (incidents []probe.Incident, total int, err error)
	CloseAt(ctx context.Context, id probe.IncidentID, at time.Time) error
}

// HTTPProber executes one Probe call. The httpprobe adapter
// (Phase 12.3) provides the concrete implementation; this port keeps
// the use case free of network code.
//
// Implementations are expected to never return an error — every
// failure mode (DNS, timeout, TLS, non-2xx, JSON-RPC errors) is
// captured in the ProbeResult.ErrorClass + ErrorMsg fields. That
// keeps the use case's control flow linear and avoids burying
// observable failures in `if err != nil` branches.
type HTTPProber interface {
	Probe(
		ctx context.Context,
		target probe.ProbeTarget,
		timeout time.Duration,
	) ProbeResult
}

// IDGen mints fresh IncidentIDs. The application layer doesn't pin
// down the encoding — a UUID v4 implementation lives in the
// infrastructure adapter; tests inject a counter-backed fake.
type IDGen interface {
	NewIncidentID() probe.IncidentID
}
