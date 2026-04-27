package probe

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// ProbeID is the stable, human-readable identifier for a Probe. It is
// supplied by the operator (config-driven, like a metric key) so the
// same Probe definition survives restarts and migrations.
type ProbeID string

// String returns the underlying identifier.
func (p ProbeID) String() string { return string(p) }

// TargetKind enumerates how a Probe interrogates its target. HTTP is
// the only kind needed for Phase 12A (self indexer); RPC and GraphQL
// are reserved so future adapters can extend the package without
// breaking persistence or API consumers.
type TargetKind string

const (
	// TargetHTTP — plain HTTP(S) probe. Method + URL + optional headers.
	TargetHTTP TargetKind = "http"
	// TargetRPC — JSON-RPC method invocation. Reserved for Phase 12B.
	TargetRPC TargetKind = "rpc"
	// TargetGraphQL — GraphQL query string. Reserved for Phase 12B.
	TargetGraphQL TargetKind = "graphql"
)

// Valid reports whether k is one of the recognised TargetKind constants.
func (k TargetKind) Valid() bool {
	switch k {
	case TargetHTTP, TargetRPC, TargetGraphQL:
		return true
	}
	return false
}

// ProbeTarget describes what to call. Kind discriminates the rest of
// the fields:
//
//   - TargetHTTP    — Method ∈ {GET, POST, ...}; URL is the absolute
//     endpoint; Body is the request payload (optional).
//   - TargetRPC     — Method is the JSON-RPC method name; URL is the
//     RPC endpoint; Body holds JSON-encoded params.
//   - TargetGraphQL — Method is unused; URL points at the GraphQL
//     endpoint; Body carries the query document.
//
// Headers apply across all kinds. Bodies are kept as []byte so the
// adapter can pass them through without forcing a string round-trip
// on binary payloads.
type ProbeTarget struct {
	Kind    TargetKind
	URL     string
	Method  string
	Headers map[string]string
	Body    []byte
}

// Validate checks the invariants the rest of the system relies on.
// The probe adapter trusts these have already been enforced — every
// constructor that yields a Probe must run them.
func (t ProbeTarget) Validate() error {
	if !t.Kind.Valid() {
		return fmt.Errorf("probe target: invalid kind %q", t.Kind)
	}
	if strings.TrimSpace(t.URL) == "" {
		return errors.New("probe target: url is empty")
	}
	u, err := url.Parse(t.URL)
	if err != nil {
		return fmt.Errorf("probe target: parse url: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("probe target: unsupported scheme %q", u.Scheme)
	}
	if u.Host == "" {
		return errors.New("probe target: url has no host")
	}
	if t.Kind == TargetHTTP && strings.TrimSpace(t.Method) == "" {
		return errors.New("probe target: http kind requires a method")
	}
	if t.Kind == TargetRPC && strings.TrimSpace(t.Method) == "" {
		return errors.New("probe target: rpc kind requires a method name")
	}
	return nil
}

// ProbeSchedule describes how often a Probe runs. Either a cron
// expression (5-field, asynq scheduler form — same constraint as
// verification.Schedule) or a fixed interval. Exactly one must be set.
type ProbeSchedule struct {
	CronExpr string
	Interval time.Duration
}

// Validate enforces the "exactly one" rule. An empty schedule is also
// rejected: a Probe that never runs is a configuration mistake worth
// surfacing at construction time.
func (s ProbeSchedule) Validate() error {
	hasCron := strings.TrimSpace(s.CronExpr) != ""
	hasInterval := s.Interval > 0
	switch {
	case !hasCron && !hasInterval:
		return errors.New("probe schedule: must set either cron_expr or interval")
	case hasCron && hasInterval:
		return errors.New("probe schedule: cron_expr and interval are mutually exclusive")
	case hasInterval && s.Interval < time.Second:
		return fmt.Errorf("probe schedule: interval too short: %s (min 1s)", s.Interval)
	}
	return nil
}

// ThresholdMetric names the rolled-up window metric a Threshold
// applies to. Each constant pairs with an EvaluateWindow code path in
// the application layer; adding a new metric here is a coordinated
// change with the evaluator.
type ThresholdMetric string

const (
	// ThresholdLatencyP95Ms — p95 latency over the window, in
	// milliseconds. Trips when the p95 exceeds Value.
	ThresholdLatencyP95Ms ThresholdMetric = "latency_p95_ms"
	// ThresholdLatencyP99Ms — p99 latency over the window.
	ThresholdLatencyP99Ms ThresholdMetric = "latency_p99_ms"
	// ThresholdErrorRatePct — error count / total count × 100. Trips
	// when the error rate exceeds Value (0..100).
	ThresholdErrorRatePct ThresholdMetric = "error_rate_pct"
	// ThresholdConsecutiveFailures — fires when the most recent N
	// observations are all errors. Value is N (integer-valued).
	ThresholdConsecutiveFailures ThresholdMetric = "consecutive_failures"
)

// Valid reports whether m is one of the recognised ThresholdMetric
// constants.
func (m ThresholdMetric) Valid() bool {
	switch m {
	case ThresholdLatencyP95Ms, ThresholdLatencyP99Ms,
		ThresholdErrorRatePct, ThresholdConsecutiveFailures:
		return true
	}
	return false
}

// Threshold expresses "if Metric over WindowSec exceeds Value, treat
// it as a Breach". Window is unused for ConsecutiveFailures (the
// metric is a count, not a rate), but keeping a single Threshold
// shape avoids splitting evaluation logic across two types.
type Threshold struct {
	Metric    ThresholdMetric
	Value     float64
	WindowSec int
	// Optional human-readable label. Surfaced in the API and UI so
	// operators can name a threshold ("p95<2s SLO") without having
	// to read the metric/value tuple.
	Label string
}

// Validate enforces type-specific invariants:
//
//   - Metric must be recognised.
//   - Value must be non-negative; latency thresholds reject zero
//     (a 0ms ceiling is meaningless).
//   - WindowSec must be positive for window metrics; ignored for
//     ConsecutiveFailures (which uses Value as N).
//   - For ErrorRatePct, Value lives in [0, 100].
//   - For ConsecutiveFailures, Value must be a positive integer.
func (t Threshold) Validate() error {
	if !t.Metric.Valid() {
		return fmt.Errorf("threshold: invalid metric %q", t.Metric)
	}
	if t.Value < 0 {
		return fmt.Errorf("threshold: value cannot be negative: %v", t.Value)
	}
	switch t.Metric {
	case ThresholdLatencyP95Ms, ThresholdLatencyP99Ms:
		if t.Value == 0 {
			return errors.New("threshold: latency value must be > 0")
		}
		if t.WindowSec <= 0 {
			return errors.New("threshold: latency thresholds require window_sec > 0")
		}
	case ThresholdErrorRatePct:
		if t.Value > 100 {
			return fmt.Errorf("threshold: error_rate_pct out of range: %v", t.Value)
		}
		if t.WindowSec <= 0 {
			return errors.New("threshold: error_rate_pct requires window_sec > 0")
		}
	case ThresholdConsecutiveFailures:
		if t.Value <= 0 || t.Value != float64(int(t.Value)) {
			return fmt.Errorf("threshold: consecutive_failures requires a positive integer, got %v", t.Value)
		}
	}
	return nil
}

// Probe is the aggregate root of the package: a stable identifier, a
// description of what to observe, a cadence, and the Thresholds that
// promote Observations into Incidents.
//
// Probes are immutable once constructed via NewProbe. The persistence
// layer hydrates them through Rehydrate (sibling of verification's
// Rehydrate) so it can rebuild a Probe from storage without going
// through the validation gate again — adapters trust that storage
// only ever holds valid records, and constructors enforce the gate
// for fresh instances.
type Probe struct {
	id         ProbeID
	target     ProbeTarget
	schedule   ProbeSchedule
	thresholds []Threshold
	enabled    bool
}

// NewProbe constructs a Probe and validates every component. It
// returns the first invariant failure rather than aggregating, since
// most failures are configuration typos resolved one at a time.
func NewProbe(
	id ProbeID,
	target ProbeTarget,
	schedule ProbeSchedule,
	thresholds []Threshold,
	enabled bool,
) (Probe, error) {
	if strings.TrimSpace(string(id)) == "" {
		return Probe{}, errors.New("probe: id is empty")
	}
	if err := target.Validate(); err != nil {
		return Probe{}, err
	}
	if err := schedule.Validate(); err != nil {
		return Probe{}, err
	}
	if len(thresholds) == 0 {
		return Probe{}, errors.New("probe: at least one threshold is required")
	}
	seen := make(map[ThresholdMetric]struct{}, len(thresholds))
	for i, th := range thresholds {
		if err := th.Validate(); err != nil {
			return Probe{}, fmt.Errorf("probe: threshold[%d]: %w", i, err)
		}
		if _, dup := seen[th.Metric]; dup {
			return Probe{}, fmt.Errorf("probe: duplicate threshold metric %q", th.Metric)
		}
		seen[th.Metric] = struct{}{}
	}
	// Defensive copies — callers that mutate slices/maps after
	// construction must not corrupt the stored aggregate.
	tCopy := make([]Threshold, len(thresholds))
	copy(tCopy, thresholds)
	if target.Headers != nil {
		hCopy := make(map[string]string, len(target.Headers))
		for k, v := range target.Headers {
			hCopy[k] = v
		}
		target.Headers = hCopy
	}
	if target.Body != nil {
		bCopy := make([]byte, len(target.Body))
		copy(bCopy, target.Body)
		target.Body = bCopy
	}
	return Probe{
		id:         id,
		target:     target,
		schedule:   schedule,
		thresholds: tCopy,
		enabled:    enabled,
	}, nil
}

// Rehydrate rebuilds a Probe from persisted state without re-running
// the validation gate. Persistence already round-tripped a valid
// instance; running the constructor would force the storage layer to
// either re-encode rejected fields (bad) or duplicate the validation
// logic (worse). The mapper alone is responsible for keeping rows
// consistent with the domain — see internal/infrastructure/persistence.
func Rehydrate(
	id ProbeID,
	target ProbeTarget,
	schedule ProbeSchedule,
	thresholds []Threshold,
	enabled bool,
) Probe {
	tCopy := make([]Threshold, len(thresholds))
	copy(tCopy, thresholds)
	return Probe{
		id:         id,
		target:     target,
		schedule:   schedule,
		thresholds: tCopy,
		enabled:    enabled,
	}
}

// ID returns the stable identifier.
func (p Probe) ID() ProbeID { return p.id }

// Target returns a defensive copy of the probe target.
func (p Probe) Target() ProbeTarget {
	t := p.target
	if t.Headers != nil {
		hCopy := make(map[string]string, len(t.Headers))
		for k, v := range t.Headers {
			hCopy[k] = v
		}
		t.Headers = hCopy
	}
	if t.Body != nil {
		bCopy := make([]byte, len(t.Body))
		copy(bCopy, t.Body)
		t.Body = bCopy
	}
	return t
}

// Schedule returns the run cadence.
func (p Probe) Schedule() ProbeSchedule { return p.schedule }

// Thresholds returns a defensive copy of the configured thresholds.
func (p Probe) Thresholds() []Threshold {
	out := make([]Threshold, len(p.thresholds))
	copy(out, p.thresholds)
	return out
}

// Enabled reports whether this Probe should be scheduled. Disabled
// Probes survive in storage and the API but the scheduler skips them.
func (p Probe) Enabled() bool { return p.enabled }
