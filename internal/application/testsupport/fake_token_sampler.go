package testsupport

import (
	"context"
	"sync"

	"github.com/seokheejang/chain-sync-watch/internal/chain"
	"github.com/seokheejang/chain-sync-watch/internal/verification"
)

// FakeTokenSampler is the token counterpart to FakeAddressSampler —
// same shape (per-Kind preconfigured lists, optional Err, recorded
// Calls) so test setups look familiar across the two axes.
type FakeTokenSampler struct {
	mu      sync.Mutex
	Results map[string][]chain.Address
	Err     error
	Calls   []FakeTokenSamplerCall
}

// FakeTokenSamplerCall records one Sample invocation.
type FakeTokenSamplerCall struct {
	ChainID chain.ChainID
	Kind    string
	At      chain.BlockNumber
}

// NewFakeTokenSampler returns an empty sampler.
func NewFakeTokenSampler() *FakeTokenSampler {
	return &FakeTokenSampler{Results: map[string][]chain.Address{}}
}

// Sample looks up the preconfigured list for plan.Kind() and returns
// a defensive copy.
func (f *FakeTokenSampler) Sample(
	_ context.Context,
	chainID chain.ChainID,
	plan verification.TokenSamplingPlan,
	at chain.BlockNumber,
) ([]chain.Address, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Calls = append(f.Calls, FakeTokenSamplerCall{
		ChainID: chainID,
		Kind:    plan.Kind(),
		At:      at,
	})
	if f.Err != nil {
		return nil, f.Err
	}
	src := f.Results[plan.Kind()]
	if len(src) == 0 {
		return nil, nil
	}
	out := make([]chain.Address, len(src))
	copy(out, src)
	return out, nil
}
