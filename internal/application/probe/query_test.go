package probeapp_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	probeapp "github.com/seokheejang/chain-sync-watch/internal/application/probe"
	probeTS "github.com/seokheejang/chain-sync-watch/internal/application/probe/testsupport"
	"github.com/seokheejang/chain-sync-watch/internal/probe"
)

func TestQueryProbes_GetAndList(t *testing.T) {
	repo := probeTS.NewFakeProbeRepo()
	require.NoError(t, repo.Save(context.Background(), mustProbe(t, "p1", true)))
	require.NoError(t, repo.Save(context.Background(), mustProbe(t, "p2", false)))

	uc := probeapp.QueryProbes{Probes: repo}

	got, err := uc.Get(context.Background(), "p1")
	require.NoError(t, err)
	require.Equal(t, probe.ProbeID("p1"), got.ID())

	all, total, err := uc.List(context.Background(), probeapp.ProbeFilter{})
	require.NoError(t, err)
	require.Equal(t, 2, total)
	require.Len(t, all, 2)

	enabled, total, err := uc.List(context.Background(), probeapp.ProbeFilter{EnabledOnly: true})
	require.NoError(t, err)
	require.Equal(t, 1, total)
	require.Len(t, enabled, 1)
	require.Equal(t, probe.ProbeID("p1"), enabled[0].ID())
}

func TestQueryIncidents_FilterByStatus(t *testing.T) {
	repo := probeTS.NewFakeIncidentRepo()
	openInc, err := probe.NewIncident(
		"inc-open", "p1", time.Now(),
		probe.Breach{Metric: probe.ThresholdLatencyP95Ms, Threshold: 1000, Observed: 1500, WindowSec: 60},
		nil,
	)
	require.NoError(t, err)
	closedInc, err := probe.NewIncident(
		"inc-closed", "p1", time.Now().Add(-time.Hour),
		probe.Breach{Metric: probe.ThresholdLatencyP95Ms, Threshold: 1000, Observed: 1500, WindowSec: 60},
		nil,
	)
	require.NoError(t, err)
	require.NoError(t, repo.Save(context.Background(), openInc))
	require.NoError(t, repo.Save(context.Background(), closedInc))
	require.NoError(t, repo.CloseAt(context.Background(), "inc-closed", time.Now()))

	uc := probeapp.QueryIncidents{Incidents: repo}

	openStatus := probe.StatusOpen
	openOnly, total, err := uc.List(context.Background(), probeapp.IncidentFilter{Status: &openStatus})
	require.NoError(t, err)
	require.Equal(t, 1, total)
	require.Equal(t, probe.IncidentID("inc-open"), openOnly[0].ID())

	closedStatus := probe.StatusClosed
	closedOnly, total, err := uc.List(context.Background(), probeapp.IncidentFilter{Status: &closedStatus})
	require.NoError(t, err)
	require.Equal(t, 1, total)
	require.Equal(t, probe.IncidentID("inc-closed"), closedOnly[0].ID())
}
