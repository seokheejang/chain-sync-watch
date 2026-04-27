package probeapp

import (
	"context"
	"fmt"
	"time"

	"github.com/seokheejang/chain-sync-watch/internal/application"
	"github.com/seokheejang/chain-sync-watch/internal/probe"
)

// DefaultProbeTimeout is the per-call ceiling RunProbe applies when
// Timeout is left zero. 10s sits between an aggressive SLO probe and
// an indexer query that may legitimately fan out a lot of work
// internally.
const DefaultProbeTimeout = 10 * time.Second

// RunProbe executes one Probe call: load the aggregate, run the
// network call through HTTPProber, stamp the result with an
// authoritative timestamp from Clock, persist the resulting
// Observation, and return it.
//
// Implementations of HTTPProber don't surface errors via Go's error
// channel — every failure mode is encoded in the ProbeResult — so
// the only `error` returns from Execute are repository / not-found
// failures. The Observation is returned to the caller (the asynq
// handler) so logs can include the latency without a re-read.
type RunProbe struct {
	Probes       ProbeRepository
	Observations ObservationRepository
	Prober       HTTPProber
	Clock        application.Clock
	// Timeout overrides DefaultProbeTimeout when non-zero. Per-probe
	// timeouts could live on the Probe aggregate later; for MVP a
	// single application-wide value is sufficient.
	Timeout time.Duration
}

// Execute runs one probe and persists its observation.
func (uc RunProbe) Execute(ctx context.Context, id probe.ProbeID) (probe.Observation, error) {
	p, err := uc.Probes.FindByID(ctx, id)
	if err != nil {
		return probe.Observation{}, err
	}
	if !p.Enabled() {
		return probe.Observation{}, ErrProbeDisabled
	}

	timeout := uc.Timeout
	if timeout == 0 {
		timeout = DefaultProbeTimeout
	}

	result := uc.Prober.Probe(ctx, p.Target(), timeout)
	obs, err := probe.NewObservation(
		p.ID(),
		uc.Clock.Now(),
		result.ElapsedMS,
		result.StatusCode,
		result.ErrorClass,
		result.ErrorMsg,
	)
	if err != nil {
		// A prober that returns an inconsistent ProbeResult is a
		// programming error in the adapter — surface it loudly so the
		// reviewer notices in CI rather than persisting a bogus row.
		return probe.Observation{}, fmt.Errorf("probeapp: build observation: %w", err)
	}
	if err := uc.Observations.Save(ctx, obs); err != nil {
		return obs, fmt.Errorf("probeapp: save observation: %w", err)
	}
	return obs, nil
}
