package verification

import (
	"math/rand/v2"
	"sort"
	"time"

	"github.com/seokheejang/chain-sync-watch/internal/chain"
)

// SamplingContext carries the ambient facts a SamplingStrategy needs
// to compute block selection: the current tip and the current wall
// clock. Injecting these as a struct keeps SamplingStrategy.Blocks
// pure — it never touches an RPC endpoint or the real clock — which
// is what makes the strategy layer deterministically testable.
type SamplingContext struct {
	TipBlock chain.BlockNumber
	Now      time.Time
}

// SamplingStrategy picks the blocks a Run will verify. Kind() returns
// a stable identifier used for logging and persistence; Blocks()
// returns the selected block numbers in ascending order.
//
// Implementations must be deterministic: given the same receiver and
// the same SamplingContext, the output must be bit-for-bit identical.
// Randomised strategies (see Random) achieve this by carrying a seed
// on the receiver rather than sampling from a global source.
type SamplingStrategy interface {
	Kind() string
	Blocks(ctx SamplingContext) []chain.BlockNumber
}

// Strategy kind identifiers. These strings are persisted with each
// Run so log search and schema filters can find runs by sampling
// style; they must stay stable across versions.
const (
	KindFixedList   = "fixed_list"
	KindLatestN     = "latest_n"
	KindRandom      = "random"
	KindSparseSteps = "sparse_steps"
)

// FixedList samples an explicit set of block numbers. The list is
// returned verbatim (defensive copy) — callers that want filtering
// against the current tip must do it themselves before constructing
// the strategy.
type FixedList struct {
	Numbers []chain.BlockNumber
}

// Kind returns the stable identifier.
func (FixedList) Kind() string { return KindFixedList }

// Blocks returns a defensive copy of the configured list so mutations
// on the returned slice cannot leak back into the strategy value.
func (f FixedList) Blocks(_ SamplingContext) []chain.BlockNumber {
	if len(f.Numbers) == 0 {
		return nil
	}
	out := make([]chain.BlockNumber, len(f.Numbers))
	copy(out, f.Numbers)
	return out
}

// LatestN samples the last N blocks up to and including the tip.
// When the tip is smaller than N (shallow chain, test fixtures), the
// strategy clamps down to [0 .. tip] rather than underflowing.
type LatestN struct {
	N uint
}

// Kind returns the stable identifier.
func (LatestN) Kind() string { return KindLatestN }

// Blocks returns [tip-N+1 .. tip] in ascending order, clamped at 0.
func (l LatestN) Blocks(ctx SamplingContext) []chain.BlockNumber {
	if l.N == 0 {
		return nil
	}
	tip := ctx.TipBlock.Uint64()
	count := uint64(l.N)
	if count > tip+1 {
		count = tip + 1
	}
	start := tip - count + 1
	out := make([]chain.BlockNumber, 0, count)
	for n := start; n <= tip; n++ {
		out = append(out, chain.BlockNumber(n))
	}
	return out
}

// Random samples Count distinct block numbers from Range, using Seed
// to drive a deterministic PRNG. Seed is mandatory in spirit (zero is
// accepted, but runs with Seed=0 should be rare and deliberate).
// When Count >= Range.Len() the full range is returned.
type Random struct {
	Range chain.BlockRange
	Count uint
	Seed  int64
}

// Kind returns the stable identifier.
func (Random) Kind() string { return KindRandom }

// Blocks returns Count distinct block numbers from Range, sorted
// ascending. The output depends only on (Range, Count, Seed) — the
// SamplingContext is ignored — which is what makes the strategy
// deterministic across re-runs.
func (r Random) Blocks(_ SamplingContext) []chain.BlockNumber {
	if r.Count == 0 || r.Range.Len() == 0 {
		return nil
	}
	total := r.Range.Len()
	if uint64(r.Count) >= total {
		out := make([]chain.BlockNumber, 0, total)
		for n := r.Range.Start.Uint64(); n <= r.Range.End.Uint64(); n++ {
			out = append(out, chain.BlockNumber(n))
		}
		return out
	}

	// rand/v2 PCG is seeded with two uint64s; mirroring the signed
	// seed into both halves gives a full-period sequence that is
	// reproducible across Go versions as long as we stay on the same
	// PRNG algorithm. Reproducibility is the whole point here, so the
	// non-cryptographic PRNG (math/rand/v2) is exactly what we want —
	// a crypto/rand source would defeat the test determinism this
	// package is built on.
	//nolint:gosec // G115: intentional reinterpret of signed seed to uint64 halves.
	seedU := uint64(r.Seed)
	//nolint:gosec // G404: deterministic PRNG is required for reproducible sampling.
	rng := rand.New(rand.NewPCG(seedU, seedU^0x9E3779B97F4A7C15))

	picked := make(map[chain.BlockNumber]struct{}, r.Count)
	out := make([]chain.BlockNumber, 0, r.Count)
	start := r.Range.Start.Uint64()
	for uint(len(out)) < r.Count {
		delta := rng.Uint64N(total)
		n := chain.BlockNumber(start + delta)
		if _, dup := picked[n]; dup {
			continue
		}
		picked[n] = struct{}{}
		out = append(out, n)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// SparseSteps samples Range at fixed Step intervals starting at
// Range.Start. Step=0 is treated as "no samples" rather than an
// infinite loop — validation at construction time in the Run
// constructor will catch a Step=0 misconfiguration before it reaches
// this path.
type SparseSteps struct {
	Range chain.BlockRange
	Step  uint64
}

// Kind returns the stable identifier.
func (SparseSteps) Kind() string { return KindSparseSteps }

// Blocks returns [Start, Start+Step, Start+2*Step, ...] up to End
// inclusive. Overflow-safe: the loop breaks before a uint64 wrap.
func (s SparseSteps) Blocks(_ SamplingContext) []chain.BlockNumber {
	if s.Step == 0 {
		return nil
	}
	start := s.Range.Start.Uint64()
	end := s.Range.End.Uint64()
	var out []chain.BlockNumber
	for n := start; n <= end; {
		out = append(out, chain.BlockNumber(n))
		next := n + s.Step
		if next < n || next > end {
			break
		}
		n = next
	}
	return out
}
