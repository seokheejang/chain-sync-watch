package gateway

import (
	"context"
	"errors"
	"fmt"

	"github.com/seokheejang/chain-sync-watch/internal/application"
	"github.com/seokheejang/chain-sync-watch/internal/chain"
	"github.com/seokheejang/chain-sync-watch/internal/secrets"
	"github.com/seokheejang/chain-sync-watch/internal/source"
)

// DBGateway is the application.SourceGateway implementation that
// reads its adapter list from the sources table on every call.
// Caching is a post-MVP concern — a SELECT + three struct allocs
// per verification run is negligible next to the actual fetch
// storm each adapter kicks off.
//
// ForChain returns all enabled rows for the given chain. Get maps
// a single SourceID back to its row (ReplayDiff uses this to
// re-fetch only the sources that participated in a persisted
// DiffRecord).
type DBGateway struct {
	repo     application.SourceConfigRepository
	cipher   *secrets.Cipher
	registry Registry
}

// NewDBGateway wires a DBGateway. cipher may be nil only when the
// deployment configured zero adapters that require api_keys;
// otherwise a row with an encrypted secret surfaces as an error at
// ForChain / Get time. reg==nil falls back to DefaultRegistry().
func NewDBGateway(repo application.SourceConfigRepository, cipher *secrets.Cipher, reg Registry) *DBGateway {
	if reg == nil {
		reg = DefaultRegistry()
	}
	return &DBGateway{repo: repo, cipher: cipher, registry: reg}
}

// ForChain loads enabled sources for the chain and materialises an
// adapter for each. A single failing row errors the whole call —
// partial wiring would make ExecuteRun silently drop a source,
// which is precisely the class of bug the verification tool
// exists to catch.
//
// The SourceGateway port does not take a ctx; DBGateway runs with
// context.Background and relies on the DB driver's statement
// timeout. Threading ctx through the port is a follow-up (touches
// every use case).
func (g *DBGateway) ForChain(chainID chain.ChainID) ([]source.Source, error) {
	ctx := context.Background()
	rows, err := g.repo.ListByChain(ctx, chainID, true)
	if err != nil {
		return nil, fmt.Errorf("gateway: list by chain %d: %w", chainID, err)
	}
	out := make([]source.Source, 0, len(rows))
	for i := range rows {
		s, err := g.build(rows[i])
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, nil
}

// Get materialises a single source by its config id. Returns
// ErrSourceNotFound when the id has no row.
func (g *DBGateway) Get(id source.SourceID) (source.Source, error) {
	ctx := context.Background()
	cfg, err := g.repo.FindByID(ctx, string(id))
	if err != nil {
		if errors.Is(err, application.ErrSourceNotFound) {
			return nil, err
		}
		return nil, fmt.Errorf("gateway: find %q: %w", id, err)
	}
	return g.build(*cfg)
}

// build decrypts (if needed) and constructs. Kept private because
// callers always go through ForChain / Get.
func (g *DBGateway) build(cfg application.SourceConfig) (source.Source, error) {
	var apiKey []byte
	if cfg.HasSecret() {
		if g.cipher == nil {
			return nil, fmt.Errorf("gateway: source %q has encrypted secret but cipher is nil", cfg.ID)
		}
		pk, err := g.cipher.Decrypt(cfg.SecretCiphertext, cfg.SecretNonce)
		if err != nil {
			return nil, fmt.Errorf("gateway: decrypt %q: %w", cfg.ID, err)
		}
		apiKey = pk
	}
	return g.registry.Build(cfg, apiKey)
}

// Compile-time assertion — DBGateway satisfies the port.
var _ application.SourceGateway = (*DBGateway)(nil)
