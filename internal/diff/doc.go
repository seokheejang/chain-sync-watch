// Package diff models the disagreement world: when Sources disagree
// about a metric, this package captures the snapshot, decides
// tolerance, and classifies severity.
//
// Three layers, narrow interfaces:
//
//   - Discrepancy / ValueSnapshot — the raw record of "Source A said
//     X, Source B said Y". No comparison logic, just transport.
//   - Tolerance — the first-pass comparator. Returns (ok,
//     needDiscard): ok means values are close enough to call equal,
//     needDiscard means the sample is outside an anchor window and
//     should not produce a Judgement at all. Implementations:
//     ExactMatch, NumericTolerance, AnchorWindowed, Observational.
//   - JudgementPolicy — when values actually diverge, decide
//     severity and nominate trusted sources. DefaultPolicy applies
//     the MetricCategory → Severity table and trusts whichever
//     cluster carries the highest-ranked SourceID.
//
// Like internal/verification, this package is pure domain: stdlib
// plus internal/chain, internal/source, and internal/verification —
// enforced by the depguard rule in .golangci.yml.
package diff
