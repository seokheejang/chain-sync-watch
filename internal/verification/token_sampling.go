package verification

import (
	"github.com/seokheejang/chain-sync-watch/internal/chain"
)

// TokenSamplingPlan picks the ERC-20 contract set a Run will verify
// for per-address / per-token balance metrics (CapERC20BalanceAtLatest).
// The shape mirrors AddressSamplingPlan on purpose: three of the
// four stratums we care about need external queries (top tokens
// by holder count, random token selection from a live candidate
// pool, derivation from a concurrent Holdings fetch), so plan
// execution is deferred to the TokenSampler port.
//
// This iteration ships the Known stratum only. The others are
// registered as follow-ups in the plan docs; their Plan types will
// slot in without changing the interface.
//
// The combination with AddressSamplingPlan drives a cartesian
// (address × token) fan-out in the ERC20 Balance pass, so a Run
// that carries N addresses and M tokens issues N*M
// FetchERC20Balance calls per source.
type TokenSamplingPlan interface {
	// Kind returns the stable identifier persisted alongside every
	// Run that uses this plan. Like AddressSamplingPlan.Kind(), the
	// string form is what logs, schema filters, and dashboards key
	// off of.
	Kind() string
}

// Plan kind identifiers. Persisted with each Run — must stay stable
// across versions (rename breaks historical Run records).
const (
	KindKnownTokens = "known_tokens"
	// Future: KindTopNTokens, KindRandomTokens, KindFromHoldings.
)

// KnownTokens enumerates a hand-picked ERC-20 contract list.
// Typical uses: USDC / USDT / WETH / the chain's canonical governance
// token — the ones where a silent balance discrepancy would trigger
// immediate operational attention.
//
// Following the KnownAddresses precedent, KnownTokens is the one
// stratum the domain can evaluate without external help; fakes and
// unit tests can call Resolve directly. The full TokenSampler port
// exists so the more-complex stratums fit the same shape.
type KnownTokens struct {
	// Tokens is the set of ERC-20 contract addresses to verify.
	// Deduplication and ordering are the sampler's concern, not the
	// plan's.
	Tokens []chain.Address
}

// Kind returns the stable identifier.
func (KnownTokens) Kind() string { return KindKnownTokens }

// Resolve returns a defensive copy so downstream mutation cannot
// leak back into the plan.
func (k KnownTokens) Resolve() []chain.Address {
	if len(k.Tokens) == 0 {
		return nil
	}
	out := make([]chain.Address, len(k.Tokens))
	copy(out, k.Tokens)
	return out
}
