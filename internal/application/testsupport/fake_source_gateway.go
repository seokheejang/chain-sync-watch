package testsupport

import (
	"fmt"
	"sync"

	"github.com/seokheejang/chain-sync-watch/internal/chain"
	"github.com/seokheejang/chain-sync-watch/internal/source"
)

// FakeSourceGateway returns configured Source instances per chain.
// Tests build Sources with the source/fake package and register
// them here.
type FakeSourceGateway struct {
	mu      sync.Mutex
	byChain map[chain.ChainID][]source.Source
	byID    map[source.SourceID]source.Source
}

// NewFakeSourceGateway returns an empty gateway.
func NewFakeSourceGateway() *FakeSourceGateway {
	return &FakeSourceGateway{
		byChain: map[chain.ChainID][]source.Source{},
		byID:    map[source.SourceID]source.Source{},
	}
}

// Register indexes src under its ChainID() and ID(). Multiple
// Sources may share a ChainID; a SourceID must be unique.
func (f *FakeSourceGateway) Register(src source.Source) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.byChain[src.ChainID()] = append(f.byChain[src.ChainID()], src)
	f.byID[src.ID()] = src
}

// ForChain returns every Source registered under chainID. An
// unregistered chain returns an empty slice, not an error —
// ExecuteRun's "no sources configured" path must be reachable
// without a special sentinel here.
func (f *FakeSourceGateway) ForChain(chainID chain.ChainID) ([]source.Source, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]source.Source, len(f.byChain[chainID]))
	copy(out, f.byChain[chainID])
	return out, nil
}

// Get returns the Source with the given ID or an error.
func (f *FakeSourceGateway) Get(id source.SourceID) (source.Source, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, ok := f.byID[id]
	if !ok {
		return nil, fmt.Errorf("fake source gateway: unknown source %q", id)
	}
	return s, nil
}
