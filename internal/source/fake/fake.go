// Package fake provides a test-only implementation of source.Source
// that records every call and returns configured responses. It is the
// substrate every Phase 4/5 test sits on top of — Application use
// cases build against the Source port, and this fake is how those
// tests drive the port without touching any real network.
//
// The fake deliberately lives in its own subpackage so it can only be
// imported from test files in other packages (by convention) and so
// the core source package stays free of test helpers.
package fake

import (
	"context"
	"sync"

	"github.com/seokheejang/chain-sync-watch/internal/chain"
	"github.com/seokheejang/chain-sync-watch/internal/source"
)

// Call is one recorded invocation. Method is the Fetch name, Args is
// the query value (copied by value so mutating the source afterwards
// cannot corrupt history).
type Call struct {
	Method string
	Args   any
}

// Source is a configurable, call-recording source.Source. All fields
// are guarded by mu so tests can exercise concurrent Fetch calls.
type Source struct {
	mu sync.Mutex

	id      source.SourceID
	chainID chain.ChainID
	caps    map[source.Capability]bool

	// Static responses. Nil handler + zero response means "return
	// ErrUnsupported" so tests that forget to configure a method get a
	// clear signal instead of an empty success.
	staticBlock             *blockEnvelope
	staticAddressLatest     *addressLatestEnvelope
	staticAddressAtBlock    *addressAtBlockEnvelope
	staticSnapshot          *snapshotEnvelope
	staticERC20Balance      *erc20BalanceEnvelope
	staticERC20Holdings     *erc20HoldingsEnvelope
	staticInternalTxByTx    *internalTxEnvelope
	staticInternalTxByBlock *internalTxEnvelope

	// Dynamic handlers — when set, override any static response for
	// that method.
	blockHandler             func(context.Context, source.BlockQuery) (source.BlockResult, error)
	addressLatestHandler     func(context.Context, source.AddressQuery) (source.AddressLatestResult, error)
	addressAtBlockHandler    func(context.Context, source.AddressAtBlockQuery) (source.AddressAtBlockResult, error)
	snapshotHandler          func(context.Context, source.SnapshotQuery) (source.SnapshotResult, error)
	erc20BalanceHandler      func(context.Context, source.ERC20BalanceQuery) (source.ERC20BalanceResult, error)
	erc20HoldingsHandler     func(context.Context, source.ERC20HoldingsQuery) (source.ERC20HoldingsResult, error)
	internalTxByTxHandler    func(context.Context, source.InternalTxByTxQuery) (source.InternalTxResult, error)
	internalTxByBlockHandler func(context.Context, source.InternalTxByBlockQuery) (source.InternalTxResult, error)

	calls []Call
}

// Envelopes bundle "happy-path value + injected error" because tests
// frequently want to assert "method errored with X" without having to
// set a zero-value result.
type (
	blockEnvelope struct {
		r   source.BlockResult
		err error
	}
	addressLatestEnvelope struct {
		r   source.AddressLatestResult
		err error
	}
	addressAtBlockEnvelope struct {
		r   source.AddressAtBlockResult
		err error
	}
	snapshotEnvelope struct {
		r   source.SnapshotResult
		err error
	}
	erc20BalanceEnvelope struct {
		r   source.ERC20BalanceResult
		err error
	}
	erc20HoldingsEnvelope struct {
		r   source.ERC20HoldingsResult
		err error
	}
	internalTxEnvelope struct {
		r   source.InternalTxResult
		err error
	}
)

// Option configures a Source at construction time.
type Option func(*Source)

// WithCapabilities declares which Capability values Supports() returns
// true for. Defaults to none.
func WithCapabilities(caps ...source.Capability) Option {
	return func(s *Source) {
		for _, c := range caps {
			s.caps[c] = true
		}
	}
}

// New builds a fake with the given id, chain, and options.
func New(id source.SourceID, chainID chain.ChainID, opts ...Option) *Source {
	s := &Source{
		id:      id,
		chainID: chainID,
		caps:    make(map[source.Capability]bool),
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// --- source.Source implementation ----------------------------------------

func (s *Source) ID() source.SourceID    { return s.id }
func (s *Source) ChainID() chain.ChainID { return s.chainID }

func (s *Source) Supports(c source.Capability) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.caps[c]
}

func (s *Source) FetchBlock(ctx context.Context, q source.BlockQuery) (source.BlockResult, error) {
	s.record("FetchBlock", q)
	if err := ctx.Err(); err != nil {
		return source.BlockResult{}, err
	}
	s.mu.Lock()
	h := s.blockHandler
	env := s.staticBlock
	s.mu.Unlock()
	if h != nil {
		return h(ctx, q)
	}
	if env != nil {
		return env.r, env.err
	}
	return source.BlockResult{}, source.ErrUnsupported
}

func (s *Source) FetchAddressLatest(ctx context.Context, q source.AddressQuery) (source.AddressLatestResult, error) {
	s.record("FetchAddressLatest", q)
	if err := ctx.Err(); err != nil {
		return source.AddressLatestResult{}, err
	}
	s.mu.Lock()
	h := s.addressLatestHandler
	env := s.staticAddressLatest
	s.mu.Unlock()
	if h != nil {
		return h(ctx, q)
	}
	if env != nil {
		return env.r, env.err
	}
	return source.AddressLatestResult{}, source.ErrUnsupported
}

func (s *Source) FetchAddressAtBlock(ctx context.Context, q source.AddressAtBlockQuery) (source.AddressAtBlockResult, error) {
	s.record("FetchAddressAtBlock", q)
	if err := ctx.Err(); err != nil {
		return source.AddressAtBlockResult{}, err
	}
	s.mu.Lock()
	h := s.addressAtBlockHandler
	env := s.staticAddressAtBlock
	s.mu.Unlock()
	if h != nil {
		return h(ctx, q)
	}
	if env != nil {
		return env.r, env.err
	}
	return source.AddressAtBlockResult{}, source.ErrUnsupported
}

func (s *Source) FetchSnapshot(ctx context.Context, q source.SnapshotQuery) (source.SnapshotResult, error) {
	s.record("FetchSnapshot", q)
	if err := ctx.Err(); err != nil {
		return source.SnapshotResult{}, err
	}
	s.mu.Lock()
	h := s.snapshotHandler
	env := s.staticSnapshot
	s.mu.Unlock()
	if h != nil {
		return h(ctx, q)
	}
	if env != nil {
		return env.r, env.err
	}
	return source.SnapshotResult{}, source.ErrUnsupported
}

func (s *Source) FetchERC20Balance(ctx context.Context, q source.ERC20BalanceQuery) (source.ERC20BalanceResult, error) {
	s.record("FetchERC20Balance", q)
	if err := ctx.Err(); err != nil {
		return source.ERC20BalanceResult{}, err
	}
	s.mu.Lock()
	h := s.erc20BalanceHandler
	env := s.staticERC20Balance
	s.mu.Unlock()
	if h != nil {
		return h(ctx, q)
	}
	if env != nil {
		return env.r, env.err
	}
	return source.ERC20BalanceResult{}, source.ErrUnsupported
}

func (s *Source) FetchERC20Holdings(ctx context.Context, q source.ERC20HoldingsQuery) (source.ERC20HoldingsResult, error) {
	s.record("FetchERC20Holdings", q)
	if err := ctx.Err(); err != nil {
		return source.ERC20HoldingsResult{}, err
	}
	s.mu.Lock()
	h := s.erc20HoldingsHandler
	env := s.staticERC20Holdings
	s.mu.Unlock()
	if h != nil {
		return h(ctx, q)
	}
	if env != nil {
		return env.r, env.err
	}
	return source.ERC20HoldingsResult{}, source.ErrUnsupported
}

func (s *Source) FetchInternalTxByTx(ctx context.Context, q source.InternalTxByTxQuery) (source.InternalTxResult, error) {
	s.record("FetchInternalTxByTx", q)
	if err := ctx.Err(); err != nil {
		return source.InternalTxResult{}, err
	}
	s.mu.Lock()
	h := s.internalTxByTxHandler
	env := s.staticInternalTxByTx
	s.mu.Unlock()
	if h != nil {
		return h(ctx, q)
	}
	if env != nil {
		return env.r, env.err
	}
	return source.InternalTxResult{}, source.ErrUnsupported
}

func (s *Source) FetchInternalTxByBlock(ctx context.Context, q source.InternalTxByBlockQuery) (source.InternalTxResult, error) {
	s.record("FetchInternalTxByBlock", q)
	if err := ctx.Err(); err != nil {
		return source.InternalTxResult{}, err
	}
	s.mu.Lock()
	h := s.internalTxByBlockHandler
	env := s.staticInternalTxByBlock
	s.mu.Unlock()
	if h != nil {
		return h(ctx, q)
	}
	if env != nil {
		return env.r, env.err
	}
	return source.InternalTxResult{}, source.ErrUnsupported
}

// --- Configuration helpers -----------------------------------------------

// SetBlockResponse installs a static response+error for FetchBlock.
// Use this when every block query in a test should return the same
// value; use SetBlockHandler when responses depend on the query.
func (s *Source) SetBlockResponse(r source.BlockResult, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.staticBlock = &blockEnvelope{r: r, err: err}
}

// SetAddressLatestResponse installs a static response+error for
// FetchAddressLatest.
func (s *Source) SetAddressLatestResponse(r source.AddressLatestResult, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.staticAddressLatest = &addressLatestEnvelope{r: r, err: err}
}

// SetAddressAtBlockResponse installs a static response+error for
// FetchAddressAtBlock.
func (s *Source) SetAddressAtBlockResponse(r source.AddressAtBlockResult, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.staticAddressAtBlock = &addressAtBlockEnvelope{r: r, err: err}
}

// SetSnapshotResponse installs a static response+error for
// FetchSnapshot.
func (s *Source) SetSnapshotResponse(r source.SnapshotResult, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.staticSnapshot = &snapshotEnvelope{r: r, err: err}
}

// SetBlockHandler overrides FetchBlock with a dynamic function. Useful
// when the test needs the response to depend on the query arguments.
func (s *Source) SetBlockHandler(fn func(context.Context, source.BlockQuery) (source.BlockResult, error)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.blockHandler = fn
}

// SetAddressLatestHandler overrides FetchAddressLatest with a dynamic
// function.
func (s *Source) SetAddressLatestHandler(fn func(context.Context, source.AddressQuery) (source.AddressLatestResult, error)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.addressLatestHandler = fn
}

// SetAddressAtBlockHandler overrides FetchAddressAtBlock with a
// dynamic function.
func (s *Source) SetAddressAtBlockHandler(fn func(context.Context, source.AddressAtBlockQuery) (source.AddressAtBlockResult, error)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.addressAtBlockHandler = fn
}

// SetSnapshotHandler overrides FetchSnapshot with a dynamic function.
func (s *Source) SetSnapshotHandler(fn func(context.Context, source.SnapshotQuery) (source.SnapshotResult, error)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.snapshotHandler = fn
}

// SetERC20BalanceResponse installs a static response+error for
// FetchERC20Balance.
func (s *Source) SetERC20BalanceResponse(r source.ERC20BalanceResult, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.staticERC20Balance = &erc20BalanceEnvelope{r: r, err: err}
}

// SetERC20BalanceHandler overrides FetchERC20Balance with a dynamic
// function.
func (s *Source) SetERC20BalanceHandler(fn func(context.Context, source.ERC20BalanceQuery) (source.ERC20BalanceResult, error)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.erc20BalanceHandler = fn
}

// SetERC20HoldingsResponse installs a static response+error for
// FetchERC20Holdings.
func (s *Source) SetERC20HoldingsResponse(r source.ERC20HoldingsResult, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.staticERC20Holdings = &erc20HoldingsEnvelope{r: r, err: err}
}

// SetERC20HoldingsHandler overrides FetchERC20Holdings with a dynamic
// function.
func (s *Source) SetERC20HoldingsHandler(fn func(context.Context, source.ERC20HoldingsQuery) (source.ERC20HoldingsResult, error)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.erc20HoldingsHandler = fn
}

// SetInternalTxByTxResponse installs a static response+error for
// FetchInternalTxByTx.
func (s *Source) SetInternalTxByTxResponse(r source.InternalTxResult, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.staticInternalTxByTx = &internalTxEnvelope{r: r, err: err}
}

// SetInternalTxByTxHandler overrides FetchInternalTxByTx with a
// dynamic function.
func (s *Source) SetInternalTxByTxHandler(fn func(context.Context, source.InternalTxByTxQuery) (source.InternalTxResult, error)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.internalTxByTxHandler = fn
}

// SetInternalTxByBlockResponse installs a static response+error for
// FetchInternalTxByBlock.
func (s *Source) SetInternalTxByBlockResponse(r source.InternalTxResult, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.staticInternalTxByBlock = &internalTxEnvelope{r: r, err: err}
}

// SetInternalTxByBlockHandler overrides FetchInternalTxByBlock with a
// dynamic function.
func (s *Source) SetInternalTxByBlockHandler(fn func(context.Context, source.InternalTxByBlockQuery) (source.InternalTxResult, error)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.internalTxByBlockHandler = fn
}

// --- Call recording ------------------------------------------------------

// Calls returns a snapshot of the recorded invocations in call order.
// The returned slice is a copy — mutating it does not affect the fake.
func (s *Source) Calls() []Call {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.calls) == 0 {
		return nil
	}
	out := make([]Call, len(s.calls))
	copy(out, s.calls)
	return out
}

// Reset clears the call log and all configured responses/handlers.
// Capabilities are kept — the common test pattern is "configure once,
// exercise many times".
func (s *Source) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = nil
	s.staticBlock = nil
	s.staticAddressLatest = nil
	s.staticAddressAtBlock = nil
	s.staticSnapshot = nil
	s.staticERC20Balance = nil
	s.staticERC20Holdings = nil
	s.staticInternalTxByTx = nil
	s.staticInternalTxByBlock = nil
	s.blockHandler = nil
	s.addressLatestHandler = nil
	s.addressAtBlockHandler = nil
	s.snapshotHandler = nil
	s.erc20BalanceHandler = nil
	s.erc20HoldingsHandler = nil
	s.internalTxByTxHandler = nil
	s.internalTxByBlockHandler = nil
}

func (s *Source) record(method string, args any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, Call{Method: method, Args: args})
}
