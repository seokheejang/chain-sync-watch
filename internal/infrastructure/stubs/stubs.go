// Package stubs hosts minimal no-op implementations of application
// ports for bootstrapping CLIs / binaries before the adapter layer
// is wired. They return empty lists or ErrNotConfigured-style errors
// so callers get clear feedback that the real plumbing is missing —
// never a silent happy path.
//
// These are scaffolding. Phase 10 will introduce config-driven
// factories that build concrete gateways from adapters/* packages;
// at that point these stubs move out of every binary that wires
// them. Until then they keep csw-server / csw-worker / the
// openapi-dump CLI from each re-declaring the same placeholders.
package stubs

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/seokheejang/chain-sync-watch/internal/chain"
	"github.com/seokheejang/chain-sync-watch/internal/source"
)

// NullGateway is the zero-source implementation of
// application.SourceGateway. ForChain returns an empty slice
// (applications that demand ≥2 sources will fail cleanly); Get
// always errors because a stub has no sources to name.
type NullGateway struct{}

// ForChain returns an empty source slice.
func (NullGateway) ForChain(chain.ChainID) ([]source.Source, error) {
	return nil, nil
}

// Get errors for any requested SourceID.
func (NullGateway) Get(id source.SourceID) (source.Source, error) {
	return nil, fmt.Errorf("no sources configured for %q", id)
}

// NullChainHead errors on every read. ExecuteRun / ReplayDiff will
// surface the error through their normal path — operators see a
// "chain head not configured" message and know the binary is
// running in stub mode.
type NullChainHead struct{}

// Tip errors.
func (NullChainHead) Tip(context.Context, chain.ChainID) (chain.BlockNumber, error) {
	return 0, errors.New("chain head not configured")
}

// Finalized errors.
func (NullChainHead) Finalized(context.Context, chain.ChainID) (chain.BlockNumber, error) {
	return 0, errors.New("chain head not configured")
}

// SystemClock implements application.Clock on top of time.Now. The
// zero value is usable; production binaries inject this, tests
// inject a FakeClock from testsupport.
type SystemClock struct{}

// Now returns the current wall-clock time.
func (SystemClock) Now() time.Time { return time.Now() }
