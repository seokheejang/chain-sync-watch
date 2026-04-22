package testsupport

import (
	"context"
	"sync"

	"github.com/seokheejang/chain-sync-watch/internal/chain"
	"github.com/seokheejang/chain-sync-watch/internal/verification"
)

// FakeAddressSampler returns preconfigured address lists keyed by
// plan Kind. Tests populate Results for the stratums they care
// about; missing keys return a nil slice (treated as "skip this
// stratum"). An optional Err is returned from every Sample call so
// tests can drive the error path.
type FakeAddressSampler struct {
	mu      sync.Mutex
	Results map[string][]chain.Address
	Err     error
	Calls   []FakeAddressSamplerCall
}

// FakeAddressSamplerCall records one Sample invocation for test
// assertions (ChainID, Kind, anchor block).
type FakeAddressSamplerCall struct {
	ChainID chain.ChainID
	Kind    string
	At      chain.BlockNumber
}

// NewFakeAddressSampler returns an empty sampler. Populate Results
// before passing it into the use case under test.
func NewFakeAddressSampler() *FakeAddressSampler {
	return &FakeAddressSampler{Results: map[string][]chain.Address{}}
}

// Sample looks up the preconfigured list for plan.Kind() and
// returns a defensive copy so mutations by the use case cannot leak
// into subsequent invocations.
func (f *FakeAddressSampler) Sample(
	_ context.Context,
	chainID chain.ChainID,
	plan verification.AddressSamplingPlan,
	at chain.BlockNumber,
) ([]chain.Address, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Calls = append(f.Calls, FakeAddressSamplerCall{
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
