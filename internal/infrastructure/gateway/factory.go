// Package gateway wires the DB-backed SourceGateway. It reads
// SourceConfig rows through a SourceConfigRepository, decrypts any
// api_key using a secrets.Cipher, and hands the payload to a
// type-keyed factory registry that constructs an adapter.Source.
//
// The registry is the extension point: new adapter packages
// (user-defined indexers, additional explorers) register their
// constructor under a stable type string. The bundled factories
// cover the three public adapters (rpc / blockscout / routescan);
// custom adapters call Registry.Add at process startup.
//
// Every adapter.Source returned here is wrapped in aliasedSource so
// `Source.ID()` reports the config row's id rather than the
// adapter-package's global const. This is the only way the
// compare engine + replay engine can tell two instances of the
// same adapter type (one per chain, or eventually one per chain
// per redundancy slot) apart in the diff.Values map.
package gateway

import (
	"errors"
	"fmt"

	"github.com/seokheejang/chain-sync-watch/adapters/blockscout"
	"github.com/seokheejang/chain-sync-watch/adapters/routescan"
	"github.com/seokheejang/chain-sync-watch/adapters/rpc"
	"github.com/seokheejang/chain-sync-watch/internal/application"
	"github.com/seokheejang/chain-sync-watch/internal/source"
)

// Adapter type strings persisted in the sources.type column. New
// adapters must pick a string that won't collide with these.
const (
	TypeRPC        = "rpc"
	TypeBlockscout = "blockscout"
	TypeRoutescan  = "routescan"
)

// ErrUnknownType surfaces when a sources.type value has no matching
// factory. Seed / CRUD flows should catch this before persisting —
// hitting it at runtime means a rename landed without a migration
// or a registry update.
var ErrUnknownType = errors.New("gateway: unknown adapter type")

// FactoryFunc constructs the underlying adapter.Source (the
// aliasing wrapper is applied by Registry.Build above it).
// apiKey is the already-decrypted credential or nil when the
// adapter needs no auth. Factories should validate type-specific
// options on the spot — fail fast on bad config rather than at
// first fetch.
type FactoryFunc func(cfg application.SourceConfig, apiKey []byte) (source.Source, error)

// Registry maps adapter type strings to their FactoryFunc. Safe
// for concurrent reads once populated; callers mutate only during
// process startup.
type Registry map[string]FactoryFunc

// DefaultRegistry returns a Registry pre-wired with the three
// bundled adapters. Custom adapters extend this at startup:
//
//	reg := gateway.DefaultRegistry()
//	reg.Add("myindexer", myIndexerFactory)
func DefaultRegistry() Registry {
	return Registry{
		TypeRPC:        rpcFactory,
		TypeBlockscout: blockscoutFactory,
		TypeRoutescan:  routescanFactory,
	}
}

// Add registers or overwrites a FactoryFunc for the given type.
// Overwriting is intentional for test scaffolding; production code
// should panic on collision by checking the caller side.
func (r Registry) Add(kind string, f FactoryFunc) { r[kind] = f }

// Build resolves a config to a source.Source with its runtime ID
// overridden to cfg.ID. Callers feed this into application.ExecuteRun
// indirectly via DBGateway.
func (r Registry) Build(cfg application.SourceConfig, apiKey []byte) (source.Source, error) {
	f, ok := r[cfg.Type]
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrUnknownType, cfg.Type)
	}
	s, err := f(cfg, apiKey)
	if err != nil {
		return nil, fmt.Errorf("gateway: build %q (%s): %w", cfg.ID, cfg.Type, err)
	}
	return aliasedSource{Source: s, id: source.SourceID(cfg.ID)}, nil
}

// --- bundled factories ---------------------------------------------

// rpcFactory honours the `archive` bool option for historical
// state reads. debug_trace is intentionally left out of the MVP
// options surface — we only flip it for private nodes that the
// operator tests separately.
func rpcFactory(cfg application.SourceConfig, _ []byte) (source.Source, error) {
	var opts []rpc.Option
	if b, ok := cfg.Options["archive"].(bool); ok && b {
		opts = append(opts, rpc.WithArchive(true))
	}
	return rpc.New(cfg.ChainID, cfg.Endpoint, opts...)
}

// blockscoutFactory passes cfg.Endpoint through WithBaseURL when
// set. An empty endpoint falls back to the adapter package's
// per-chain default — handy when operators want to flag a chain
// as "use the bundled default" without committing to a URL in DB.
func blockscoutFactory(cfg application.SourceConfig, _ []byte) (source.Source, error) {
	var opts []blockscout.Option
	if cfg.Endpoint != "" {
		opts = append(opts, blockscout.WithBaseURL(cfg.Endpoint))
	}
	return blockscout.New(cfg.ChainID, opts...)
}

// routescanFactory mirrors blockscoutFactory — same endpoint
// override semantics against a different upstream.
func routescanFactory(cfg application.SourceConfig, _ []byte) (source.Source, error) {
	var opts []routescan.Option
	if cfg.Endpoint != "" {
		opts = append(opts, routescan.WithBaseURL(cfg.Endpoint))
	}
	return routescan.New(cfg.ChainID, opts...)
}

// aliasedSource overrides the underlying adapter's fixed ID const
// with the operator-chosen config id. ChainID and every Fetch
// method delegate to the embedded Source so adapter behaviour is
// identical — only the ID() surface changes.
type aliasedSource struct {
	source.Source
	id source.SourceID
}

// ID returns the alias the factory assigned.
func (a aliasedSource) ID() source.SourceID { return a.id }
