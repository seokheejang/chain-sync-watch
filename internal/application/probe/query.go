package probeapp

import (
	"context"

	"github.com/seokheejang/chain-sync-watch/internal/probe"
)

// QueryProbes is the read-side use case for Probes. Mirrors
// internal/application's QueryRuns shape so HTTP handlers and CLI
// callers see the same surface for both contexts.
type QueryProbes struct {
	Probes ProbeRepository
}

// Get returns the Probe with the given id or ErrProbeNotFound.
func (uc QueryProbes) Get(ctx context.Context, id probe.ProbeID) (*probe.Probe, error) {
	return uc.Probes.FindByID(ctx, id)
}

// List returns a filtered, paginated slice of Probes plus the total
// count of rows matching the filter.
func (uc QueryProbes) List(ctx context.Context, f ProbeFilter) ([]probe.Probe, int, error) {
	return uc.Probes.List(ctx, f)
}

// QueryIncidents is the read-side use case for Incidents.
type QueryIncidents struct {
	Incidents IncidentRepository
}

// List returns a filtered, paginated slice of Incidents plus the
// total count.
func (uc QueryIncidents) List(ctx context.Context, f IncidentFilter) ([]probe.Incident, int, error) {
	return uc.Incidents.List(ctx, f)
}
