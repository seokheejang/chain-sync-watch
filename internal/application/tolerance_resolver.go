package application

import (
	"github.com/seokheejang/chain-sync-watch/internal/diff"
	"github.com/seokheejang/chain-sync-watch/internal/verification"
)

// ToleranceResolver maps a Metric to the Tolerance that should
// judge disagreements on it. The resolver is a port so operators
// can inject per-deployment policy (tight thresholds for critical
// metrics, observational for noisy ones) without touching the
// ExecuteRun engine.
type ToleranceResolver interface {
	For(m verification.Metric) diff.Tolerance
}

// DefaultToleranceResolver applies the category-default mapping
// captured in docs/plans/phase-04-verification-diff-domain.md:
//
//   - BlockImmutable, AddressAtBlock → ExactMatch (any mismatch is
//     load-bearing, so strict equality is the right floor).
//   - AddressLatest → AnchorWindowed{ExactMatch, TolFwd≈64 blocks}
//     to absorb the race between Sources observing "latest" at
//     slightly different heights. 64 blocks ≈ 2 minutes on
//     Optimism; tune per chain via Overrides.
//   - Snapshot → Observational. Across-source semantics diverge
//     (spam filters, pending inclusion, indexer definitions), so
//     automatic judgement produces noise.
//
// Overrides lets callers pin a specific Metric to a non-default
// Tolerance — keyed on Metric.Key, the stable identifier — without
// reaching into the resolver's guts.
type DefaultToleranceResolver struct {
	Overrides map[string]diff.Tolerance
}

// For implements ToleranceResolver.
func (r DefaultToleranceResolver) For(m verification.Metric) diff.Tolerance {
	if r.Overrides != nil {
		if t, ok := r.Overrides[m.Key]; ok {
			return t
		}
	}
	switch m.Category {
	case verification.CatBlockImmutable, verification.CatAddressAtBlock:
		return diff.ExactMatch{}
	case verification.CatAddressLatest:
		return diff.AnchorWindowed{
			Inner:   diff.ExactMatch{},
			TolBack: 0,
			TolFwd:  64,
		}
	case verification.CatSnapshot:
		return diff.Observational{}
	}
	return diff.ExactMatch{}
}

// Compile-time assertion.
var _ ToleranceResolver = DefaultToleranceResolver{}
