package probeapp

import "errors"

// Sentinel errors. Mirrors internal/application's pattern so HTTP
// handlers and queue workers can errors.Is to a stable set without
// reaching into infrastructure.
var (
	// ErrProbeNotFound — no Probe with the given ProbeID exists.
	ErrProbeNotFound = errors.New("probeapp: probe not found")

	// ErrIncidentNotFound — no Incident with the given IncidentID exists.
	ErrIncidentNotFound = errors.New("probeapp: incident not found")

	// ErrProbeDisabled — RunProbe was invoked against a Probe whose
	// Enabled flag is false. The scheduler filters these out before
	// dispatch; surfacing the error here catches direct CLI/manual
	// invocations against a deactivated probe.
	ErrProbeDisabled = errors.New("probeapp: probe is disabled")

	// ErrInvalidProbe — Probe construction rejected the inputs.
	// The underlying probe-domain error is wrapped.
	ErrInvalidProbe = errors.New("probeapp: invalid probe")
)
