// Package httpprobe implements probeapp.HTTPProber for plain
// HTTP(S) targets. One probe call = one HTTP request, no retries —
// the whole point of a probe is to capture the *first* response
// (including its failure mode), so any retry layer would mask the
// signal the operator subscribed to.
//
// Errors don't surface via Go's error channel: every failure mode
// (DNS, TCP, TLS, deadline, non-2xx, etc.) is encoded into a
// probeapp.ProbeResult with the correct probe.ErrorClass. That keeps
// the use-case control flow linear (no `if err != nil` branches that
// hide observable failures) and matches the prober contract defined
// at the application layer.
//
// The adapter sits at adapters/httpprobe/ rather than under
// adapters/internal/ because external packages — the cmd wiring,
// integration tests, and (eventually) operators using us as a
// library — need to construct it. Adapters never import domain
// packages by anything but the public API of internal/probe and
// internal/application/probe.
package httpprobe
