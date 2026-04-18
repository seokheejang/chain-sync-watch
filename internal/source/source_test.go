package source_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/seokheejang/chain-sync-watch/internal/chain"
	"github.com/seokheejang/chain-sync-watch/internal/source"
)

// dummySource is a minimal no-op implementation used purely to lock in
// the Source interface shape — if its method set diverges from the
// interface, this file fails to compile. That's the point.
type dummySource struct{}

func (dummySource) ID() source.SourceID       { return "dummy" }
func (dummySource) ChainID() chain.ChainID    { return chain.OptimismMainnet }
func (dummySource) Supports(source.Capability) bool { return false }

func (dummySource) FetchBlock(context.Context, source.BlockQuery) (source.BlockResult, error) {
	return source.BlockResult{}, source.ErrUnsupported
}

func (dummySource) FetchAddressLatest(context.Context, source.AddressQuery) (source.AddressLatestResult, error) {
	return source.AddressLatestResult{}, source.ErrUnsupported
}

func (dummySource) FetchAddressAtBlock(context.Context, source.AddressAtBlockQuery) (source.AddressAtBlockResult, error) {
	return source.AddressAtBlockResult{}, source.ErrUnsupported
}

func (dummySource) FetchSnapshot(context.Context, source.SnapshotQuery) (source.SnapshotResult, error) {
	return source.SnapshotResult{}, source.ErrUnsupported
}

// Compile-time check. If Source grows or loses a method, this line
// stops compiling and surfaces the mismatch immediately.
var _ source.Source = dummySource{}

func TestSource_InterfaceSatisfied(t *testing.T) {
	var s source.Source = dummySource{}
	require.Equal(t, source.SourceID("dummy"), s.ID())
	require.Equal(t, chain.OptimismMainnet, s.ChainID())
	require.False(t, s.Supports(source.CapBlockHash))

	_, err := s.FetchBlock(context.Background(), source.BlockQuery{})
	require.ErrorIs(t, err, source.ErrUnsupported)
}
