package verification

import (
	"github.com/seokheejang/chain-sync-watch/internal/chain"
)

// AddressSamplingPlan picks the address set a Run will verify for
// AddressLatest / AddressAtBlock metrics. Unlike SamplingStrategy
// (block selection), plan execution cannot be done purely in the
// domain: three of the four stratums need external queries (top
// holders by balance, recent chain activity, random sampling over a
// live candidate pool). The domain therefore defines only the plan
// — parameters + a stable Kind — and defers Resolve to the
// AddressSampler port in the application layer.
//
// The four stratums form a deliberate, complementary mix:
//
//   - Known: hand-picked addresses where a silent discrepancy would
//     be a high-impact incident (bridges, DEXs, treasuries).
//   - TopNHolders: weight-proportional coverage — the largest
//     balances matter most for total-supply-style sanity checks.
//   - RandomAddresses: uniform coverage so indexers cannot game
//     the checker by keeping the popular addresses correct.
//   - RecentlyActive: time-proximate coverage — catches regressions
//     on the path data is flowing through right now. Candidates are
//     derived from RPC blocks (not from any indexer's address list)
//     so the sampling itself cannot be biased by the thing being
//     verified.
type AddressSamplingPlan interface {
	// Kind returns the stable identifier persisted alongside every
	// Run that uses this plan. Sampler implementations type-switch
	// on the concrete plan value, but log search, schema filters,
	// and dashboards key off the string form.
	Kind() string
}

// Plan kind identifiers. These strings are persisted with each Run
// and must stay stable across versions — rename breaks historical
// Run records.
const (
	KindKnownAddresses  = "known"
	KindTopNHolders     = "top_n"
	KindRandomAddresses = "random"
	KindRecentlyActive  = "recently_active"
)

// KnownAddresses enumerates a hand-picked address list. Typically
// populated from config (bridges, DEXs, treasuries, team wallets).
// The sampler returns the list verbatim — no external query — which
// makes this stratum both the cheapest and the highest-signal.
type KnownAddresses struct {
	// Addresses is the set to verify. Deduplication and ordering are
	// the sampler's responsibility, not the plan's; callers pass
	// whatever their config yields.
	Addresses []chain.Address
}

// Kind returns the stable identifier.
func (KnownAddresses) Kind() string { return KindKnownAddresses }

// Resolve returns a defensive copy so downstream mutations on the
// returned slice cannot leak back into the plan. Kept on the domain
// value (not only on the port) because Known is the one stratum the
// domain can evaluate without external help — fakes and unit tests
// can call it directly without instantiating a sampler.
func (k KnownAddresses) Resolve() []chain.Address {
	if len(k.Addresses) == 0 {
		return nil
	}
	out := make([]chain.Address, len(k.Addresses))
	copy(out, k.Addresses)
	return out
}

// TopNHolders asks the sampler for the N largest balance holders on
// the chain at the Run's anchor block. N's practical upper bound is
// adapter-dependent (Blockscout exposes 50 per page, Routescan
// 100); the plan itself does not enforce one so sampler impls can
// page and combine.
//
// N = 0 is a legal "skip this stratum" signal — the sampler returns
// an empty slice rather than erroring.
type TopNHolders struct {
	N uint
}

// Kind returns the stable identifier.
func (TopNHolders) Kind() string { return KindTopNHolders }

// RandomAddresses asks the sampler for Count uniformly-distributed
// addresses. The candidate pool is sampler-dependent (typically a
// union of recent chain activity and known lists) — the plan stays
// agnostic so different deployments can tune the pool without
// reshuffling the domain.
//
// Seed drives the random selection. A Run persists Seed so an
// operator can replay the exact same sample set months later
// (forensic / reproducibility concern).
type RandomAddresses struct {
	Count uint
	Seed  int64
}

// Kind returns the stable identifier.
func (RandomAddresses) Kind() string { return KindRandomAddresses }

// RecentlyActive asks the sampler for addresses that appeared as
// sender or recipient in the last RecentBlocks blocks. When the
// scan yields more than Count candidates, Seed-driven uniform
// sub-sampling trims it down. The scan uses RPC blocks directly —
// never an indexer's address list — so the sampling cannot be
// biased by the very thing being verified.
//
// RecentBlocks = 0 or Count = 0 both degenerate to "skip this
// stratum" — the sampler returns an empty slice.
type RecentlyActive struct {
	RecentBlocks uint
	Count        uint
	Seed         int64
}

// Kind returns the stable identifier.
func (RecentlyActive) Kind() string { return KindRecentlyActive }
