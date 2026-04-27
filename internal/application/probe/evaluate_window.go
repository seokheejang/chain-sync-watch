package probeapp

import (
	"context"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/seokheejang/chain-sync-watch/internal/application"
	"github.com/seokheejang/chain-sync-watch/internal/probe"
)

// DefaultEvidenceMax caps how many Observations EvaluateWindow stores
// on a freshly opened Incident. The sample is for tooltips and root-
// cause sleuthing, not full reconstruction — a few dozen is plenty
// and keeps row sizes predictable.
const DefaultEvidenceMax = 20

// EvaluateWindow walks each Threshold on a Probe and decides whether
// to open or close an Incident.
//
// For each threshold:
//
//   - Pull observations covering the window [now - WindowSec, now]
//     (ConsecutiveFailures uses the most recent N regardless of time).
//   - Compute the metric (p95 / p99 / error rate / consecutive count).
//   - If the metric exceeds the threshold:
//   - existing open Incident → leave it alone (one Incident per
//     (Probe, Metric) until it closes).
//   - none → mint a new Incident with the trip evidence.
//   - If the metric is back below the threshold:
//   - existing open Incident → Close at "now".
//   - none → no-op.
//
// The use case is idempotent under repeated invocation: re-running
// it against the same window never opens a duplicate Incident, so
// the asynq scheduler can fire it on a coarse cadence (e.g. every
// 30s) without coordination.
type EvaluateWindow struct {
	Probes       ProbeRepository
	Observations ObservationRepository
	Incidents    IncidentRepository
	IDGen        IDGen
	Clock        application.Clock
	// EvidenceMax caps the Observation slice attached to a newly
	// opened Incident. Zero falls back to DefaultEvidenceMax.
	EvidenceMax int
}

// Execute evaluates every Threshold on the given Probe and persists
// any Incident state changes.
func (uc EvaluateWindow) Execute(ctx context.Context, probeID probe.ProbeID) error {
	p, err := uc.Probes.FindByID(ctx, probeID)
	if err != nil {
		return err
	}
	now := uc.Clock.Now()
	for _, th := range p.Thresholds() {
		if err := uc.evaluateThreshold(ctx, p.ID(), th, now); err != nil {
			return fmt.Errorf("probeapp: evaluate %s: %w", th.Metric, err)
		}
	}
	return nil
}

func (uc EvaluateWindow) evaluateThreshold(
	ctx context.Context,
	probeID probe.ProbeID,
	th probe.Threshold,
	now time.Time,
) error {
	// ConsecutiveFailures is count-based, not time-based. The hot
	// path still uses ListWindow; we just request a generous window
	// and slice the most recent N off the tail.
	since := now.Add(-time.Duration(th.WindowSec) * time.Second)
	if th.Metric == probe.ThresholdConsecutiveFailures {
		// Look back ~10× the threshold N at 1Hz. If the probe runs
		// slower the repository still returns whatever fits; the
		// classifier short-circuits when it can't see N rows.
		since = now.Add(-time.Duration(int(th.Value)*10) * time.Second)
		if th.WindowSec > 0 {
			since = now.Add(-time.Duration(th.WindowSec) * time.Second)
		}
	}

	obs, err := uc.Observations.ListWindow(ctx, probeID, since, now)
	if err != nil {
		return err
	}

	observed, ok := computeMetric(th.Metric, int(th.Value), obs)
	if !ok {
		// Insufficient data — no observations or fewer than N for
		// ConsecutiveFailures. Don't transition state on a partial
		// window; the next evaluation will pick it up.
		return nil
	}
	tripped := observed >= th.Value

	open, err := uc.Incidents.FindOpen(ctx, probeID, th.Metric)
	if err != nil {
		return err
	}

	switch {
	case tripped && open == nil:
		evidence := truncateObservations(obs, uc.evidenceMax())
		breach := probe.Breach{
			Metric:    th.Metric,
			Threshold: th.Value,
			Observed:  observed,
			WindowSec: th.WindowSec,
		}
		inc, err := probe.NewIncident(uc.IDGen.NewIncidentID(), probeID, now, breach, evidence)
		if err != nil {
			return fmt.Errorf("build incident: %w", err)
		}
		if err := uc.Incidents.Save(ctx, inc); err != nil {
			return err
		}
	case !tripped && open != nil:
		if err := uc.Incidents.CloseAt(ctx, open.ID(), now); err != nil {
			return err
		}
	}
	return nil
}

func (uc EvaluateWindow) evidenceMax() int {
	if uc.EvidenceMax > 0 {
		return uc.EvidenceMax
	}
	return DefaultEvidenceMax
}

// computeMetric returns (value, true) when there is enough data to
// produce a comparable measurement, else (0, false). Splitting the
// metrics into one helper keeps evaluateThreshold linear and lets
// tests cover each formula in isolation.
func computeMetric(metric probe.ThresholdMetric, n int, obs []probe.Observation) (float64, bool) {
	switch metric {
	case probe.ThresholdLatencyP95Ms:
		return percentileMS(obs, 0.95)
	case probe.ThresholdLatencyP99Ms:
		return percentileMS(obs, 0.99)
	case probe.ThresholdErrorRatePct:
		return errorRatePct(obs)
	case probe.ThresholdConsecutiveFailures:
		return consecutiveFailures(obs, n)
	}
	return 0, false
}

// percentileMS computes the latency percentile in milliseconds using
// the nearest-rank method. We sort, then pick rank ceil(p × n).
// nearest-rank is a stable, parameter-free choice that matches the
// percentile values most operators reach for in dashboards.
func percentileMS(obs []probe.Observation, p float64) (float64, bool) {
	if len(obs) == 0 {
		return 0, false
	}
	xs := make([]int64, 0, len(obs))
	for _, o := range obs {
		xs = append(xs, o.ElapsedMS)
	}
	sort.Slice(xs, func(i, j int) bool { return xs[i] < xs[j] })
	rank := int(math.Ceil(p * float64(len(xs))))
	if rank < 1 {
		rank = 1
	}
	if rank > len(xs) {
		rank = len(xs)
	}
	return float64(xs[rank-1]), true
}

// errorRatePct computes errors / total × 100 across the window. An
// empty window returns (0, false) — there is no rate to report and
// the caller must not transition state on no data.
func errorRatePct(obs []probe.Observation) (float64, bool) {
	if len(obs) == 0 {
		return 0, false
	}
	errs := 0
	for _, o := range obs {
		if o.ErrorClass.IsError() {
			errs++
		}
	}
	return float64(errs) / float64(len(obs)) * 100, true
}

// consecutiveFailures returns the run-length of trailing errors. It
// returns ok=false when the window has fewer than n observations,
// because "we haven't seen N samples yet" must not trip an Incident
// shortly after a probe is enabled.
func consecutiveFailures(obs []probe.Observation, n int) (float64, bool) {
	if n <= 0 {
		return 0, false
	}
	if len(obs) < n {
		return 0, false
	}
	// Sort by timestamp ascending so "trailing" means newest.
	sorted := make([]probe.Observation, len(obs))
	copy(sorted, obs)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].At.Before(sorted[j].At)
	})
	count := 0
	for i := len(sorted) - 1; i >= 0; i-- {
		if !sorted[i].ErrorClass.IsError() {
			break
		}
		count++
	}
	return float64(count), true
}

// truncateObservations keeps the most recent max entries, sorted by
// timestamp ascending so the dashboard renders them in chronological
// order. A zero or negative max returns the full slice unchanged.
func truncateObservations(obs []probe.Observation, max int) []probe.Observation {
	if max <= 0 || len(obs) <= max {
		out := make([]probe.Observation, len(obs))
		copy(out, obs)
		return out
	}
	sorted := make([]probe.Observation, len(obs))
	copy(sorted, obs)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].At.Before(sorted[j].At)
	})
	return sorted[len(sorted)-max:]
}
