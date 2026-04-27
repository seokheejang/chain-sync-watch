// Package probeapp hosts the probe-context use cases — the layer
// between the probe domain (internal/probe) and the infrastructure
// adapters (httpprobe, queue, persistence, HTTP).
//
// It mirrors internal/application's structure but keeps a separate
// surface so the verification and probe contexts don't accidentally
// share aggregates: a RunRepository and a ProbeRepository are both
// "the persistence port" for their respective worlds, and conflating
// them invites cross-context coupling. depguard's
// application-boundary rule covers both packages identically.
//
// Use cases:
//
//   - RunProbe       — execute one Probe call, persist the
//     Observation, return it for the caller's logs.
//   - EvaluateWindow — aggregate recent Observations into per-
//     threshold metrics (p95, p99, error rate, consecutive failures)
//     and open or close Incidents accordingly.
//   - QueryProbes    — read-side: list / get a Probe.
//   - QueryIncidents — read-side: list active or historical Incidents.
//
// Ports (ProbeRepository, ObservationRepository, IncidentRepository,
// HTTPProber, IDGen) live alongside the use cases. Reuses
// application.Clock from the parent package for time-of-day stamping.
package probeapp
