package diff

import (
	"fmt"
	"sort"
	"strings"

	"github.com/seokheejang/chain-sync-watch/internal/source"
	"github.com/seokheejang/chain-sync-watch/internal/verification"
)

// Severity classifies a Judgement by the action it warrants. The
// four levels map cleanly onto existing monitoring taxonomies so
// downstream alerting can route on the string label directly.
type Severity string

const (
	// SevInfo — informational record; no action required.
	SevInfo Severity = "info"
	// SevWarning — worth a human look, not an incident.
	SevWarning Severity = "warning"
	// SevError — something is broken but the system still serves.
	SevError Severity = "error"
	// SevCritical — ground-truth disagreement on immutable data.
	// Pager-worthy.
	SevCritical Severity = "critical"
)

// Judgement is the verdict a JudgementPolicy renders on a
// Discrepancy: how severe is the disagreement, which Sources are
// most likely telling the truth, and a short human-readable
// reasoning string for debugging and dashboards.
type Judgement struct {
	Severity       Severity
	TrustedSources []source.SourceID
	Reasoning      string
}

// JudgementPolicy decides how to interpret a Discrepancy. It is a
// port: the MVP ships with DefaultPolicy, but operators can
// override with their own rules (SLO-specific severities,
// organisation-internal trust rankings) without touching the
// domain.
type JudgementPolicy interface {
	Judge(d Discrepancy) Judgement
}

// DefaultPolicy is the out-of-the-box JudgementPolicy. It drives
// severity from MetricCategory (Critical for BlockImmutable /
// AddressAtBlock, Warning for AddressLatest, Info for Snapshot)
// and picks the trusted cluster by preferring whichever agreeing
// group contains the highest-ranked Source in SourceTrust.
//
// When no ranked Source is present in any cluster, the fallback is
// "largest cluster wins", with ties broken by the lexicographic
// order of the cluster's Raw value — that tiebreaker is arbitrary
// but stable, so the same Discrepancy always produces the same
// Judgement across re-runs.
type DefaultPolicy struct {
	// SourceTrust lists SourceIDs in descending priority. The
	// cluster containing the highest-priority present Source is
	// trusted.
	SourceTrust []source.SourceID
}

// Judge implements JudgementPolicy.
func (p DefaultPolicy) Judge(d Discrepancy) Judgement {
	groups := groupByRaw(d.Values)
	severity := severityFor(d.Metric.Category)

	if len(groups) <= 1 {
		ids := sortedIDs(d.Values)
		return Judgement{
			Severity:       SevInfo,
			TrustedSources: ids,
			Reasoning:      "all sources agree",
		}
	}

	trusted := p.pickTrustedCluster(groups)
	sort.Slice(trusted, func(i, j int) bool { return trusted[i] < trusted[j] })

	return Judgement{
		Severity:       severity,
		TrustedSources: trusted,
		Reasoning:      reasoning(groups, trusted),
	}
}

// severityFor maps a MetricCategory to the default Severity. The
// mapping reflects the observational choices documented in
// docs/plans/phase-04-verification-diff-domain.md.
func severityFor(c verification.MetricCategory) Severity {
	switch c {
	case verification.CatBlockImmutable, verification.CatAddressAtBlock:
		return SevCritical
	case verification.CatAddressLatest:
		return SevWarning
	case verification.CatSnapshot:
		return SevInfo
	}
	return SevWarning
}

// groupByRaw clusters SourceIDs whose snapshots share the same Raw
// string. The Raw value is the stable, normalised form; clustering
// on it is how we decide "these Sources agree with each other".
func groupByRaw(values map[source.SourceID]ValueSnapshot) map[string][]source.SourceID {
	groups := map[string][]source.SourceID{}
	for sid, v := range values {
		groups[v.Raw] = append(groups[v.Raw], sid)
	}
	for k := range groups {
		sort.Slice(groups[k], func(i, j int) bool { return groups[k][i] < groups[k][j] })
	}
	return groups
}

// pickTrustedCluster implements the two-tier selection: first the
// cluster containing the highest-priority Source in SourceTrust,
// otherwise the largest cluster with a lexicographic tiebreaker on
// the Raw key.
func (p DefaultPolicy) pickTrustedCluster(groups map[string][]source.SourceID) []source.SourceID {
	trustRank := map[source.SourceID]int{}
	for i, sid := range p.SourceTrust {
		trustRank[sid] = i
	}

	bestRank := -1
	var bestCluster []source.SourceID
	for _, g := range groups {
		for _, sid := range g {
			r, ok := trustRank[sid]
			if !ok {
				continue
			}
			if bestCluster == nil || r < bestRank {
				bestRank = r
				bestCluster = g
			}
		}
	}
	if bestCluster != nil {
		return bestCluster
	}

	// No ranked Source: largest cluster, lexicographic tiebreaker.
	type entry struct {
		key string
		ids []source.SourceID
	}
	entries := make([]entry, 0, len(groups))
	for k, v := range groups {
		entries = append(entries, entry{k, v})
	}
	sort.Slice(entries, func(i, j int) bool {
		if len(entries[i].ids) != len(entries[j].ids) {
			return len(entries[i].ids) > len(entries[j].ids)
		}
		return entries[i].key < entries[j].key
	})
	return entries[0].ids
}

// sortedIDs returns the source IDs of values in stable order. Used
// for the all-agree Info judgement so Trusted is deterministic.
func sortedIDs(values map[source.SourceID]ValueSnapshot) []source.SourceID {
	out := make([]source.SourceID, 0, len(values))
	for sid := range values {
		out = append(out, sid)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// reasoning composes a short human-readable explanation of the
// judgement. The format is intentionally terse — dashboards render
// this inline next to the severity badge, so it needs to fit on one
// line without markdown.
func reasoning(groups map[string][]source.SourceID, trusted []source.SourceID) string {
	parts := make([]string, 0, len(groups))
	for raw, ids := range groups {
		sorted := make([]string, len(ids))
		for i, id := range ids {
			sorted[i] = string(id)
		}
		sort.Strings(sorted)
		parts = append(parts, fmt.Sprintf("[%s]=%s", strings.Join(sorted, ","), raw))
	}
	sort.Strings(parts)
	trustedStr := make([]string, len(trusted))
	for i, id := range trusted {
		trustedStr[i] = string(id)
	}
	return fmt.Sprintf("clusters %s; trusted=[%s]", strings.Join(parts, " "), strings.Join(trustedStr, ","))
}
