package probeapp_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	appTS "github.com/seokheejang/chain-sync-watch/internal/application/testsupport"

	probeapp "github.com/seokheejang/chain-sync-watch/internal/application/probe"
	probeTS "github.com/seokheejang/chain-sync-watch/internal/application/probe/testsupport"
	"github.com/seokheejang/chain-sync-watch/internal/probe"
)

func mustProbe(t *testing.T, id probe.ProbeID, enabled bool) probe.Probe {
	t.Helper()
	p, err := probe.NewProbe(
		id,
		probe.ProbeTarget{Kind: probe.TargetHTTP, URL: "https://api.example.com/health", Method: "GET"},
		probe.ProbeSchedule{Interval: 30 * time.Second},
		[]probe.Threshold{{Metric: probe.ThresholdLatencyP95Ms, Value: 1000, WindowSec: 300}},
		enabled,
	)
	require.NoError(t, err)
	return p
}

func TestRunProbe_PersistsObservation(t *testing.T) {
	now := time.Date(2026, 4, 27, 10, 0, 0, 0, time.UTC)
	probes := probeTS.NewFakeProbeRepo()
	obs := probeTS.NewFakeObservationRepo()
	prober := &probeTS.FakeHTTPProber{
		Static: &probeapp.ProbeResult{
			ElapsedMS:  123,
			StatusCode: 200,
			ErrorClass: probe.ErrorNone,
		},
	}
	clock := appTS.NewFakeClock(now)
	require.NoError(t, probes.Save(context.Background(), mustProbe(t, "p1", true)))

	uc := probeapp.RunProbe{Probes: probes, Observations: obs, Prober: prober, Clock: clock}
	got, err := uc.Execute(context.Background(), "p1")
	require.NoError(t, err)
	require.Equal(t, probe.ProbeID("p1"), got.ProbeID)
	require.Equal(t, now, got.At)
	require.EqualValues(t, 123, got.ElapsedMS)
	require.Equal(t, 1, obs.Len())
	require.Equal(t, 1, prober.Calls())
}

func TestRunProbe_NotFound(t *testing.T) {
	uc := probeapp.RunProbe{
		Probes:       probeTS.NewFakeProbeRepo(),
		Observations: probeTS.NewFakeObservationRepo(),
		Prober:       &probeTS.FakeHTTPProber{},
		Clock:        appTS.NewFakeClock(time.Now()),
	}
	_, err := uc.Execute(context.Background(), "missing")
	require.True(t, errors.Is(err, probeapp.ErrProbeNotFound))
}

func TestRunProbe_Disabled(t *testing.T) {
	probes := probeTS.NewFakeProbeRepo()
	require.NoError(t, probes.Save(context.Background(), mustProbe(t, "p1", false)))

	uc := probeapp.RunProbe{
		Probes:       probes,
		Observations: probeTS.NewFakeObservationRepo(),
		Prober:       &probeTS.FakeHTTPProber{},
		Clock:        appTS.NewFakeClock(time.Now()),
	}
	_, err := uc.Execute(context.Background(), "p1")
	require.True(t, errors.Is(err, probeapp.ErrProbeDisabled))
}

func TestRunProbe_DefaultTimeoutApplied(t *testing.T) {
	// We can't observe the timeout directly through the fake, but
	// the use case must not panic when Timeout is left zero.
	probes := probeTS.NewFakeProbeRepo()
	require.NoError(t, probes.Save(context.Background(), mustProbe(t, "p1", true)))

	uc := probeapp.RunProbe{
		Probes:       probes,
		Observations: probeTS.NewFakeObservationRepo(),
		Prober:       &probeTS.FakeHTTPProber{Static: &probeapp.ProbeResult{StatusCode: 200}},
		Clock:        appTS.NewFakeClock(time.Now()),
	}
	_, err := uc.Execute(context.Background(), "p1")
	require.NoError(t, err)
}

func TestRunProbe_RejectsInconsistentResult(t *testing.T) {
	// A prober that returns http_5xx with status 200 would generate
	// an invalid Observation. The use case must surface the error
	// rather than persist a bad row.
	probes := probeTS.NewFakeProbeRepo()
	require.NoError(t, probes.Save(context.Background(), mustProbe(t, "p1", true)))
	obs := probeTS.NewFakeObservationRepo()

	uc := probeapp.RunProbe{
		Probes:       probes,
		Observations: obs,
		Prober: &probeTS.FakeHTTPProber{Static: &probeapp.ProbeResult{
			StatusCode: 200,
			ErrorClass: probe.ErrorHTTP5xx,
		}},
		Clock: appTS.NewFakeClock(time.Now()),
	}
	_, err := uc.Execute(context.Background(), "p1")
	require.Error(t, err)
	require.Equal(t, 0, obs.Len())
}
