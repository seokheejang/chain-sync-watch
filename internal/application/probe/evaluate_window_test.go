package probeapp_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	appTS "github.com/seokheejang/chain-sync-watch/internal/application/testsupport"

	probeapp "github.com/seokheejang/chain-sync-watch/internal/application/probe"
	probeTS "github.com/seokheejang/chain-sync-watch/internal/application/probe/testsupport"
	"github.com/seokheejang/chain-sync-watch/internal/probe"
)

// probeWith builds a Probe carrying the given thresholds. Helper
// avoids repeating the boilerplate target/schedule across every test.
func probeWith(t *testing.T, id probe.ProbeID, ths []probe.Threshold) probe.Probe {
	t.Helper()
	p, err := probe.NewProbe(
		id,
		probe.ProbeTarget{Kind: probe.TargetHTTP, URL: "https://api.example.com/h", Method: "GET"},
		probe.ProbeSchedule{Interval: 30 * time.Second},
		ths,
		true,
	)
	require.NoError(t, err)
	return p
}

func mustObs(t *testing.T, id probe.ProbeID, at time.Time, elapsed int64, code int, class probe.ErrorClass) probe.Observation {
	t.Helper()
	o, err := probe.NewObservation(id, at, elapsed, code, class, "")
	require.NoError(t, err)
	return o
}

func newEvaluator(t *testing.T, now time.Time) (
	probeapp.EvaluateWindow,
	*probeTS.FakeProbeRepo,
	*probeTS.FakeObservationRepo,
	*probeTS.FakeIncidentRepo,
) {
	t.Helper()
	probes := probeTS.NewFakeProbeRepo()
	obs := probeTS.NewFakeObservationRepo()
	incidents := probeTS.NewFakeIncidentRepo()
	uc := probeapp.EvaluateWindow{
		Probes:       probes,
		Observations: obs,
		Incidents:    incidents,
		IDGen:        &probeTS.FakeIDGen{},
		Clock:        appTS.NewFakeClock(now),
	}
	return uc, probes, obs, incidents
}

// ---------- percentile metric ---------------------------------------

func TestEvaluateWindow_LatencyP95_OpensIncidentWhenExceeded(t *testing.T) {
	now := time.Date(2026, 4, 27, 10, 5, 0, 0, time.UTC)
	uc, probes, obs, incidents := newEvaluator(t, now)
	threshold := probe.Threshold{Metric: probe.ThresholdLatencyP95Ms, Value: 1000, WindowSec: 300}
	require.NoError(t, probes.Save(context.Background(), probeWith(t, "p1", []probe.Threshold{threshold})))

	// 20 fast samples (100ms) + 1 outlier (3500ms) → p95 (rank ceil(0.95*21) = 20-th
	// after sort) lands on a fast value of 100ms, which is BELOW 1000ms. We need
	// enough samples skewed high to exceed the threshold.
	// Use 10 samples at 1500ms — p95 is the 10th-rank value, all 1500.
	for i := 0; i < 10; i++ {
		require.NoError(t, obs.Save(context.Background(),
			mustObs(t, "p1", now.Add(-time.Duration(i)*time.Second), 1500, 200, probe.ErrorNone)))
	}

	require.NoError(t, uc.Execute(context.Background(), "p1"))

	open, err := incidents.FindOpen(context.Background(), "p1", probe.ThresholdLatencyP95Ms)
	require.NoError(t, err)
	require.NotNil(t, open)
	require.Equal(t, probe.ThresholdLatencyP95Ms, open.Breach().Metric)
	require.EqualValues(t, 1500, open.Breach().Observed)
	require.EqualValues(t, 1000, open.Breach().Threshold)
}

func TestEvaluateWindow_LatencyP95_NoIncidentBelowThreshold(t *testing.T) {
	now := time.Date(2026, 4, 27, 10, 5, 0, 0, time.UTC)
	uc, probes, obs, incidents := newEvaluator(t, now)
	threshold := probe.Threshold{Metric: probe.ThresholdLatencyP95Ms, Value: 1000, WindowSec: 300}
	require.NoError(t, probes.Save(context.Background(), probeWith(t, "p1", []probe.Threshold{threshold})))

	for i := 0; i < 10; i++ {
		require.NoError(t, obs.Save(context.Background(),
			mustObs(t, "p1", now.Add(-time.Duration(i)*time.Second), 100, 200, probe.ErrorNone)))
	}
	require.NoError(t, uc.Execute(context.Background(), "p1"))

	open, _ := incidents.FindOpen(context.Background(), "p1", probe.ThresholdLatencyP95Ms)
	require.Nil(t, open)
}

func TestEvaluateWindow_RecoveryClosesIncident(t *testing.T) {
	now := time.Date(2026, 4, 27, 10, 5, 0, 0, time.UTC)
	uc, probes, obs, incidents := newEvaluator(t, now)
	threshold := probe.Threshold{Metric: probe.ThresholdLatencyP95Ms, Value: 1000, WindowSec: 300}
	require.NoError(t, probes.Save(context.Background(), probeWith(t, "p1", []probe.Threshold{threshold})))

	// Trip first.
	for i := 0; i < 10; i++ {
		require.NoError(t, obs.Save(context.Background(),
			mustObs(t, "p1", now.Add(-time.Duration(60+i)*time.Second), 1500, 200, probe.ErrorNone)))
	}
	require.NoError(t, uc.Execute(context.Background(), "p1"))
	open, _ := incidents.FindOpen(context.Background(), "p1", probe.ThresholdLatencyP95Ms)
	require.NotNil(t, open)
	openedAt := open.OpenedAt()

	// Recovery: fresh fast samples within the window.
	_, err := obs.PruneBefore(context.Background(), now)
	require.NoError(t, err)
	for i := 0; i < 10; i++ {
		require.NoError(t, obs.Save(context.Background(),
			mustObs(t, "p1", now.Add(-time.Duration(i)*time.Second), 100, 200, probe.ErrorNone)))
	}
	require.NoError(t, uc.Execute(context.Background(), "p1"))

	stillOpen, _ := incidents.FindOpen(context.Background(), "p1", probe.ThresholdLatencyP95Ms)
	require.Nil(t, stillOpen, "trip should have been closed on recovery")

	closed := openedAt
	_ = closed
}

func TestEvaluateWindow_Idempotent_DoesNotDuplicate(t *testing.T) {
	now := time.Date(2026, 4, 27, 10, 5, 0, 0, time.UTC)
	uc, probes, obs, incidents := newEvaluator(t, now)
	threshold := probe.Threshold{Metric: probe.ThresholdLatencyP95Ms, Value: 1000, WindowSec: 300}
	require.NoError(t, probes.Save(context.Background(), probeWith(t, "p1", []probe.Threshold{threshold})))

	for i := 0; i < 10; i++ {
		require.NoError(t, obs.Save(context.Background(),
			mustObs(t, "p1", now.Add(-time.Duration(i)*time.Second), 1500, 200, probe.ErrorNone)))
	}
	require.NoError(t, uc.Execute(context.Background(), "p1"))
	require.NoError(t, uc.Execute(context.Background(), "p1"))
	require.NoError(t, uc.Execute(context.Background(), "p1"))

	all, _, err := incidents.List(context.Background(), probeapp.IncidentFilter{})
	require.NoError(t, err)
	require.Len(t, all, 1, "repeated evaluation must not open duplicate incidents")
}

// ---------- error-rate metric ---------------------------------------

func TestEvaluateWindow_ErrorRate_TripsAtThreshold(t *testing.T) {
	now := time.Date(2026, 4, 27, 10, 5, 0, 0, time.UTC)
	uc, probes, obs, incidents := newEvaluator(t, now)
	threshold := probe.Threshold{Metric: probe.ThresholdErrorRatePct, Value: 10, WindowSec: 300}
	require.NoError(t, probes.Save(context.Background(), probeWith(t, "p1", []probe.Threshold{threshold})))

	// 9 successes + 1 failure → 10% exactly. The implementation uses
	// >= so 10% trips the 10% threshold.
	for i := 0; i < 9; i++ {
		require.NoError(t, obs.Save(context.Background(),
			mustObs(t, "p1", now.Add(-time.Duration(i)*time.Second), 50, 200, probe.ErrorNone)))
	}
	require.NoError(t, obs.Save(context.Background(),
		mustObs(t, "p1", now.Add(-10*time.Second), 0, 503, probe.ErrorHTTP5xx)))

	require.NoError(t, uc.Execute(context.Background(), "p1"))
	open, _ := incidents.FindOpen(context.Background(), "p1", probe.ThresholdErrorRatePct)
	require.NotNil(t, open)
	require.InDelta(t, 10.0, open.Breach().Observed, 0.0001)
}

// ---------- consecutive failures -------------------------------------

func TestEvaluateWindow_ConsecutiveFailures_Trips(t *testing.T) {
	now := time.Date(2026, 4, 27, 10, 5, 0, 0, time.UTC)
	uc, probes, obs, incidents := newEvaluator(t, now)
	threshold := probe.Threshold{Metric: probe.ThresholdConsecutiveFailures, Value: 3, WindowSec: 60}
	require.NoError(t, probes.Save(context.Background(), probeWith(t, "p1", []probe.Threshold{threshold})))

	// Two old failures + four more recent failures → trailing 4 are
	// all errors → run-length 4 ≥ 3 → trip.
	for i := 0; i < 6; i++ {
		require.NoError(t, obs.Save(context.Background(),
			mustObs(t, "p1", now.Add(-time.Duration(50-i*5)*time.Second), 0, 0, probe.ErrorNetwork)))
	}
	require.NoError(t, uc.Execute(context.Background(), "p1"))
	open, _ := incidents.FindOpen(context.Background(), "p1", probe.ThresholdConsecutiveFailures)
	require.NotNil(t, open)
}

func TestEvaluateWindow_ConsecutiveFailures_NotEnoughData(t *testing.T) {
	now := time.Date(2026, 4, 27, 10, 5, 0, 0, time.UTC)
	uc, probes, obs, incidents := newEvaluator(t, now)
	threshold := probe.Threshold{Metric: probe.ThresholdConsecutiveFailures, Value: 5, WindowSec: 60}
	require.NoError(t, probes.Save(context.Background(), probeWith(t, "p1", []probe.Threshold{threshold})))

	// Only 2 failures < N=5 → must not trip.
	for i := 0; i < 2; i++ {
		require.NoError(t, obs.Save(context.Background(),
			mustObs(t, "p1", now.Add(-time.Duration(i)*time.Second), 0, 0, probe.ErrorNetwork)))
	}
	require.NoError(t, uc.Execute(context.Background(), "p1"))
	open, _ := incidents.FindOpen(context.Background(), "p1", probe.ThresholdConsecutiveFailures)
	require.Nil(t, open)
}

func TestEvaluateWindow_ConsecutiveFailures_BrokenStreak(t *testing.T) {
	now := time.Date(2026, 4, 27, 10, 5, 0, 0, time.UTC)
	uc, probes, obs, incidents := newEvaluator(t, now)
	threshold := probe.Threshold{Metric: probe.ThresholdConsecutiveFailures, Value: 3, WindowSec: 60}
	require.NoError(t, probes.Save(context.Background(), probeWith(t, "p1", []probe.Threshold{threshold})))

	// 3 errors then a success at the tail → trailing run-length is 0.
	for i := 0; i < 3; i++ {
		require.NoError(t, obs.Save(context.Background(),
			mustObs(t, "p1", now.Add(-time.Duration(40-i*5)*time.Second), 0, 0, probe.ErrorNetwork)))
	}
	require.NoError(t, obs.Save(context.Background(),
		mustObs(t, "p1", now.Add(-1*time.Second), 50, 200, probe.ErrorNone)))

	require.NoError(t, uc.Execute(context.Background(), "p1"))
	open, _ := incidents.FindOpen(context.Background(), "p1", probe.ThresholdConsecutiveFailures)
	require.Nil(t, open)
}

// ---------- multi-threshold per probe ---------------------------------

func TestEvaluateWindow_TripsTwoIndependentThresholds(t *testing.T) {
	now := time.Date(2026, 4, 27, 10, 5, 0, 0, time.UTC)
	uc, probes, obs, incidents := newEvaluator(t, now)
	thresholds := []probe.Threshold{
		{Metric: probe.ThresholdLatencyP95Ms, Value: 1000, WindowSec: 300},
		{Metric: probe.ThresholdErrorRatePct, Value: 5, WindowSec: 300},
	}
	require.NoError(t, probes.Save(context.Background(), probeWith(t, "p1", thresholds)))

	// 10 high-latency 5xx samples → both thresholds trip.
	for i := 0; i < 10; i++ {
		require.NoError(t, obs.Save(context.Background(),
			mustObs(t, "p1", now.Add(-time.Duration(i)*time.Second), 1500, 503, probe.ErrorHTTP5xx)))
	}
	require.NoError(t, uc.Execute(context.Background(), "p1"))

	all, _, err := incidents.List(context.Background(), probeapp.IncidentFilter{})
	require.NoError(t, err)
	require.Len(t, all, 2)
}

// ---------- empty window does not transition state ------------------

func TestEvaluateWindow_EmptyWindowIsNoOp(t *testing.T) {
	now := time.Date(2026, 4, 27, 10, 5, 0, 0, time.UTC)
	uc, probes, _, incidents := newEvaluator(t, now)
	threshold := probe.Threshold{Metric: probe.ThresholdLatencyP95Ms, Value: 100, WindowSec: 300}
	require.NoError(t, probes.Save(context.Background(), probeWith(t, "p1", []probe.Threshold{threshold})))

	require.NoError(t, uc.Execute(context.Background(), "p1"))
	all, _, _ := incidents.List(context.Background(), probeapp.IncidentFilter{})
	require.Empty(t, all)
}

// ---------- evidence cap --------------------------------------------

func TestEvaluateWindow_EvidenceTrimmedToMax(t *testing.T) {
	now := time.Date(2026, 4, 27, 10, 5, 0, 0, time.UTC)
	probes := probeTS.NewFakeProbeRepo()
	obs := probeTS.NewFakeObservationRepo()
	incidents := probeTS.NewFakeIncidentRepo()
	uc := probeapp.EvaluateWindow{
		Probes:       probes,
		Observations: obs,
		Incidents:    incidents,
		IDGen:        &probeTS.FakeIDGen{},
		Clock:        appTS.NewFakeClock(now),
		EvidenceMax:  3,
	}
	threshold := probe.Threshold{Metric: probe.ThresholdLatencyP95Ms, Value: 1000, WindowSec: 300}
	require.NoError(t, probes.Save(context.Background(), probeWith(t, "p1", []probe.Threshold{threshold})))

	for i := 0; i < 10; i++ {
		require.NoError(t, obs.Save(context.Background(),
			mustObs(t, "p1", now.Add(-time.Duration(i)*time.Second), 1500, 200, probe.ErrorNone)))
	}
	require.NoError(t, uc.Execute(context.Background(), "p1"))
	open, _ := incidents.FindOpen(context.Background(), "p1", probe.ThresholdLatencyP95Ms)
	require.NotNil(t, open)
	require.LessOrEqual(t, len(open.Evidence()), 3)
}
