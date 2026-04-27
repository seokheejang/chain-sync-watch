package probe_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/seokheejang/chain-sync-watch/internal/probe"
)

func validBreach() probe.Breach {
	return probe.Breach{
		Metric:    probe.ThresholdLatencyP95Ms,
		Threshold: 2000,
		Observed:  3450,
		WindowSec: 300,
	}
}

func TestBreach_Validate(t *testing.T) {
	t.Run("ok", func(t *testing.T) {
		require.NoError(t, validBreach().Validate())
	})
	t.Run("invalid metric", func(t *testing.T) {
		b := validBreach()
		b.Metric = ""
		require.Error(t, b.Validate())
	})
	t.Run("observed below threshold rejected", func(t *testing.T) {
		b := validBreach()
		b.Observed = b.Threshold - 1
		require.Error(t, b.Validate())
	})
	t.Run("missing window for window metric", func(t *testing.T) {
		b := validBreach()
		b.WindowSec = 0
		require.Error(t, b.Validate())
	})
	t.Run("consecutive failures allows zero window", func(t *testing.T) {
		b := probe.Breach{
			Metric:    probe.ThresholdConsecutiveFailures,
			Threshold: 5,
			Observed:  6,
			WindowSec: 0,
		}
		require.NoError(t, b.Validate())
	})
}

func TestNewIncident_Success(t *testing.T) {
	at := time.Date(2026, 4, 27, 10, 0, 0, 0, time.UTC)
	obs, err := probe.NewObservation("p1", at, 3500, 200, probe.ErrorNone, "")
	require.NoError(t, err)

	i, err := probe.NewIncident("inc-1", "p1", at, validBreach(), []probe.Observation{obs})
	require.NoError(t, err)
	require.Equal(t, probe.IncidentID("inc-1"), i.ID())
	require.Equal(t, probe.ProbeID("p1"), i.ProbeID())
	require.Equal(t, at, i.OpenedAt())
	require.Nil(t, i.ClosedAt())
	require.Equal(t, probe.StatusOpen, i.Status())
	require.Len(t, i.Evidence(), 1)
}

func TestNewIncident_RejectsInvariantBreaks(t *testing.T) {
	at := time.Now()
	t.Run("empty id", func(t *testing.T) {
		_, err := probe.NewIncident("", "p1", at, validBreach(), nil)
		require.Error(t, err)
	})
	t.Run("empty probe id", func(t *testing.T) {
		_, err := probe.NewIncident("inc", "", at, validBreach(), nil)
		require.Error(t, err)
	})
	t.Run("zero opened_at", func(t *testing.T) {
		_, err := probe.NewIncident("inc", "p1", time.Time{}, validBreach(), nil)
		require.Error(t, err)
	})
	t.Run("invalid breach", func(t *testing.T) {
		bad := validBreach()
		bad.Observed = bad.Threshold - 1
		_, err := probe.NewIncident("inc", "p1", at, bad, nil)
		require.Error(t, err)
	})
}

func TestIncident_Close(t *testing.T) {
	open := time.Date(2026, 4, 27, 10, 0, 0, 0, time.UTC)
	close := open.Add(5 * time.Minute)

	i, err := probe.NewIncident("inc-1", "p1", open, validBreach(), nil)
	require.NoError(t, err)

	require.NoError(t, i.Close(close))
	require.Equal(t, probe.StatusClosed, i.Status())
	require.NotNil(t, i.ClosedAt())
	require.Equal(t, close, *i.ClosedAt())

	// Defensive copy: caller mutating returned pointer must not
	// reach the aggregate.
	got := i.ClosedAt()
	*got = open
	require.Equal(t, close, *i.ClosedAt())
}

func TestIncident_CloseRejectsInvariantBreaks(t *testing.T) {
	at := time.Now()
	i, err := probe.NewIncident("inc-1", "p1", at, validBreach(), nil)
	require.NoError(t, err)

	t.Run("zero close time", func(t *testing.T) {
		require.Error(t, i.Close(time.Time{}))
	})
	t.Run("close before open", func(t *testing.T) {
		require.Error(t, i.Close(at.Add(-time.Second)))
	})
	t.Run("double close", func(t *testing.T) {
		require.NoError(t, i.Close(at.Add(time.Minute)))
		require.Error(t, i.Close(at.Add(2*time.Minute)))
	})
}

func TestIncident_Duration(t *testing.T) {
	open := time.Date(2026, 4, 27, 10, 0, 0, 0, time.UTC)
	i, err := probe.NewIncident("inc-1", "p1", open, validBreach(), nil)
	require.NoError(t, err)

	// Open: duration measured against caller-supplied "now".
	now := open.Add(2 * time.Minute)
	require.Equal(t, 2*time.Minute, i.Duration(now))

	// "Now" before open ⇒ zero (clock skew guard).
	require.Equal(t, time.Duration(0), i.Duration(open.Add(-time.Second)))

	// Closed: duration ignores "now" and uses closed_at.
	require.NoError(t, i.Close(open.Add(5*time.Minute)))
	require.Equal(t, 5*time.Minute, i.Duration(now))
}

func TestRehydrateIncident_Closed(t *testing.T) {
	open := time.Date(2026, 4, 27, 10, 0, 0, 0, time.UTC)
	close := open.Add(time.Hour)
	i := probe.RehydrateIncident("inc-1", "p1", open, &close, validBreach(), nil)
	require.Equal(t, probe.StatusClosed, i.Status())
	require.Equal(t, close, *i.ClosedAt())
}
