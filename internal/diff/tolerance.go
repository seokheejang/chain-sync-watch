package diff

import (
	"math/big"
	"strings"

	"github.com/seokheejang/chain-sync-watch/internal/chain"
	"github.com/seokheejang/chain-sync-watch/internal/source"
	"github.com/seokheejang/chain-sync-watch/internal/verification"
)

// CompareContext carries the ambient facts a Tolerance needs beyond
// the two ValueSnapshots: the Run's anchor (tag form + numeric
// resolution) and the per-side reflected blocks extracted from the
// responses. Reflected blocks are duplicated here (they also live on
// ValueSnapshot) so the application layer can override them — useful
// when the adapter omits the meta but the caller can compute one
// from context (tip minus a known lag).
type CompareContext struct {
	Anchor      source.BlockTag
	AnchorBlock chain.BlockNumber
	ReflectedA  *chain.BlockNumber
	ReflectedB  *chain.BlockNumber
}

// Tolerance decides whether two ValueSnapshots are "close enough"
// to call equal for a given metric and compare context. The return
// values split two concerns:
//
//   - ok: values are equivalent under this tolerance (no
//     Discrepancy needs to be raised).
//   - needDiscard: the sample is outside the tolerance's region of
//     applicability (e.g., reflected block falls outside the
//     anchor window) and should not be compared at all — no
//     Judgement, no persisted Discrepancy.
//
// Implementations must be pure: given the same inputs, the same
// outputs. No clock, no randomness, no side effects.
type Tolerance interface {
	Judge(a, b ValueSnapshot, metric verification.Metric, ctx CompareContext) (ok bool, needDiscard bool)
}

// ExactMatch treats two snapshots as equal iff their Raw strings
// are byte-identical. Adapters normalise Raw to a canonical form
// (lower-case hex for hashes, decimal for counts) so string
// comparison is the cheap and sufficient default.
type ExactMatch struct{}

// Judge implements Tolerance.
func (ExactMatch) Judge(a, b ValueSnapshot, _ verification.Metric, _ CompareContext) (bool, bool) {
	return a.Raw == b.Raw, false
}

// NumericTolerance compares two snapshots as arbitrary-precision
// integers and declares them equal when the absolute difference
// satisfies either AbsoluteMax or RelativePPM (whichever is
// configured, OR-combined). When both fields are zero the
// tolerance degenerates to exact numeric equality — useful when
// the caller wants numeric parsing but not slack.
//
// Raw is parsed as decimal or 0x-prefixed hex; if either side
// fails to parse, Judge returns ok=false with needDiscard=false so
// the caller still produces a Discrepancy on the raw string form.
type NumericTolerance struct {
	// AbsoluteMax: |a - b| <= AbsoluteMax -> ok.
	AbsoluteMax *big.Int
	// RelativePPM: |a - b| * 1e6 <= max(|a|, |b|) * RelativePPM -> ok.
	RelativePPM uint
}

// Judge implements Tolerance.
func (nt NumericTolerance) Judge(a, b ValueSnapshot, _ verification.Metric, _ CompareContext) (bool, bool) {
	av, okA := parseBigInt(a.Raw)
	bv, okB := parseBigInt(b.Raw)
	if !okA || !okB {
		return false, false
	}
	diff := new(big.Int).Sub(av, bv)
	diff.Abs(diff)

	if nt.AbsoluteMax == nil && nt.RelativePPM == 0 {
		return diff.Sign() == 0, false
	}
	if nt.AbsoluteMax != nil && diff.Cmp(nt.AbsoluteMax) <= 0 {
		return true, false
	}
	if nt.RelativePPM > 0 {
		maxAbs := new(big.Int).Abs(av)
		absB := new(big.Int).Abs(bv)
		if absB.Cmp(maxAbs) > 0 {
			maxAbs = absB
		}
		lhs := new(big.Int).Mul(diff, big.NewInt(1_000_000))
		rhs := new(big.Int).Mul(maxAbs, new(big.Int).SetUint64(uint64(nt.RelativePPM)))
		if lhs.Cmp(rhs) <= 0 {
			return true, false
		}
	}
	return false, false
}

// AnchorWindowed wraps an Inner Tolerance with an anchor-window
// check against the reflected blocks on each side. If either
// reflected block is non-nil and falls outside
// [AnchorBlock - TolBack, AnchorBlock + TolFwd], Judge returns
// needDiscard=true and the sample is dropped before Inner is
// called. A nil reflected block is permissive — the Source did
// not expose the meta, and the default policy handles that by
// penalising trust rather than discarding.
type AnchorWindowed struct {
	Inner   Tolerance
	TolBack uint64
	TolFwd  uint64
}

// Judge implements Tolerance.
func (aw AnchorWindowed) Judge(a, b ValueSnapshot, metric verification.Metric, ctx CompareContext) (bool, bool) {
	anchor := ctx.AnchorBlock.Uint64()
	var lo uint64
	if aw.TolBack > anchor {
		lo = 0
	} else {
		lo = anchor - aw.TolBack
	}
	hi := anchor + aw.TolFwd
	if hi < anchor { // overflow guard; treat as unbounded upward
		hi = ^uint64(0)
	}

	for _, rb := range []*chain.BlockNumber{ctx.ReflectedA, ctx.ReflectedB} {
		if rb == nil {
			continue
		}
		n := rb.Uint64()
		if n < lo || n > hi {
			return false, true
		}
	}
	if aw.Inner == nil {
		return ExactMatch{}.Judge(a, b, metric, ctx)
	}
	return aw.Inner.Judge(a, b, metric, ctx)
}

// Observational always returns (true, false). It is the right
// tolerance for metrics where the semantics diverge across Sources
// so strongly that automatic judgement would only produce noise
// (Snapshot category: totalAddressCount, totalTxCount). The metric
// still gets persisted and rendered; it just never escalates to a
// Discrepancy.
type Observational struct{}

// Judge implements Tolerance.
func (Observational) Judge(_, _ ValueSnapshot, _ verification.Metric, _ CompareContext) (bool, bool) {
	return true, false
}

// parseBigInt accepts either a decimal integer ("12345") or a
// 0x-prefixed hex integer ("0x3039"). It returns (*big.Int, true)
// on success and (nil, false) on any parse failure.
func parseBigInt(s string) (*big.Int, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, false
	}
	if strings.HasPrefix(s, "0x") || strings.HasPrefix(s, "0X") {
		n, ok := new(big.Int).SetString(s[2:], 16)
		return n, ok
	}
	n, ok := new(big.Int).SetString(s, 10)
	return n, ok
}
