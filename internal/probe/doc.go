// Package probe models the service-health world: how often we hit a
// target, what we measure when we do, and when those measurements
// breach a threshold long enough to count as an incident.
//
// It is the second bounded context in the project, sitting beside
// internal/verification + internal/diff. Where verification asks "do
// these N sources agree on the same value?", probe asks "is this one
// source healthy?". Different question, different domain — and the
// depguard rule keeps the probe package framework-free for the same
// reason it keeps verification clean.
//
// The primary types are:
//
//   - Probe — what to observe (Target), how often (Schedule), and
//     which thresholds turn a stream of Observations into incidents.
//   - Observation — a single, immutable measurement: latency, status
//     code, and an ErrorClass classification of the failure mode.
//     Body content is deliberately excluded; high-volume storage and
//     accidental PII are both unwelcome.
//   - Incident — a window-aggregate Breach that opens when a
//     threshold trips and closes when the metric recovers. Evidence
//     carries the most recent Observations so an operator can see
//     "what did the probe see when it tripped?".
//
// The package is import-narrow on purpose. Only the standard library
// and time-handling types are needed; persistence mappers and the
// asynq/HTTP-probe adapters live elsewhere and translate this domain
// model into their respective wire formats.
package probe
