package testsupport

import (
	"context"
	"fmt"
	"sync"

	"github.com/seokheejang/chain-sync-watch/internal/chain"
)

// FakeChainHead is a configurable ChainHead. Per-chain values live
// in the Tips / Finalized maps; missing entries surface as errors
// so tests that forget to seed the chain catch it loudly.
type FakeChainHead struct {
	mu   sync.Mutex
	tips map[chain.ChainID]chain.BlockNumber
	fin  map[chain.ChainID]chain.BlockNumber
}

// NewFakeChainHead returns an empty FakeChainHead.
func NewFakeChainHead() *FakeChainHead {
	return &FakeChainHead{
		tips: map[chain.ChainID]chain.BlockNumber{},
		fin:  map[chain.ChainID]chain.BlockNumber{},
	}
}

// SetTip configures the tip block for chainID.
func (f *FakeChainHead) SetTip(chainID chain.ChainID, n chain.BlockNumber) {
	f.mu.Lock()
	f.tips[chainID] = n
	f.mu.Unlock()
}

// SetFinalized configures the finalized block for chainID.
func (f *FakeChainHead) SetFinalized(chainID chain.ChainID, n chain.BlockNumber) {
	f.mu.Lock()
	f.fin[chainID] = n
	f.mu.Unlock()
}

// Tip returns the configured tip or an error if unset.
func (f *FakeChainHead) Tip(_ context.Context, chainID chain.ChainID) (chain.BlockNumber, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	n, ok := f.tips[chainID]
	if !ok {
		return 0, fmt.Errorf("fake chain head: no tip configured for %s", chainID)
	}
	return n, nil
}

// Finalized returns the configured finalized block or an error if
// unset.
func (f *FakeChainHead) Finalized(_ context.Context, chainID chain.ChainID) (chain.BlockNumber, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	n, ok := f.fin[chainID]
	if !ok {
		return 0, fmt.Errorf("fake chain head: no finalized block configured for %s", chainID)
	}
	return n, nil
}
