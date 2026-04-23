// Package cmdmigrate implements the `csw migrate` subcommand. It
// wraps golang-migrate against the migrations embedded in the
// migrations/ package so the binary ships with its schema and does
// not require a migrations directory on disk.
package cmdmigrate

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres" // register postgres driver
	"github.com/golang-migrate/migrate/v4/source/iofs"

	"github.com/seokheejang/chain-sync-watch/adapters/routescan"
	"github.com/seokheejang/chain-sync-watch/internal/application"
	"github.com/seokheejang/chain-sync-watch/internal/chain"
	"github.com/seokheejang/chain-sync-watch/internal/config"
	"github.com/seokheejang/chain-sync-watch/internal/infrastructure/gateway"
	"github.com/seokheejang/chain-sync-watch/internal/infrastructure/persistence"
	"github.com/seokheejang/chain-sync-watch/internal/secrets"
	"github.com/seokheejang/chain-sync-watch/migrations"
)

// Run dispatches `csw migrate <subcommand>`. Supported subcommands:
// up, down, status, seed. DATABASE_URL is mandatory; `seed` also
// reads CSW_SECRET_KEY when an adapter row carries an encrypted
// credential.
func Run(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errors.New("migrate: missing subcommand (up | down | status | seed)")
	}
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		return errors.New("migrate: DATABASE_URL env var is required")
	}

	if args[0] == "seed" {
		return runSeed(ctx, dsn)
	}

	src, err := iofs.New(migrations.FS, ".")
	if err != nil {
		return fmt.Errorf("migrate: open embedded source: %w", err)
	}

	m, err := migrate.NewWithSourceInstance("iofs", src, dsn)
	if err != nil {
		return fmt.Errorf("migrate: connect: %w", err)
	}
	defer func() { _, _ = m.Close() }()

	switch args[0] {
	case "up":
		if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
			return fmt.Errorf("migrate up: %w", err)
		}
		fmt.Println("migrate: up — ok")
		return nil
	case "down":
		// Steps(-1) rolls back a single migration; Down() rolls
		// back everything. We pick Down here to match the Makefile
		// `migrate-down` wording, but log a warning so operators
		// know it is destructive.
		fmt.Fprintln(os.Stderr, "migrate: rolling back ALL migrations")
		if err := m.Down(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
			return fmt.Errorf("migrate down: %w", err)
		}
		fmt.Println("migrate: down — ok")
		return nil
	case "status":
		version, dirty, err := m.Version()
		if errors.Is(err, migrate.ErrNilVersion) {
			fmt.Println("migrate: no migrations applied")
			return nil
		}
		if err != nil {
			return fmt.Errorf("migrate status: %w", err)
		}
		fmt.Printf("migrate: version=%d dirty=%t\n", version, dirty)
		return nil
	}
	return fmt.Errorf("migrate: unknown subcommand %q", args[0])
}

// runSeed populates the sources table from the embedded
// defaults.yaml (+ any local/env overrides). Runs exactly once per
// fresh DB — if the table already has rows the command errors out
// rather than duplicate-inserting, because the DB is the source of
// truth past first boot.
//
// Etherscan is skipped intentionally: the bundled gateway registry
// does not ship an etherscan factory in the 10a MVP. Routescan has
// no endpoint in defaults.yaml — its per-chain URL is derived from
// routescan.BaseURL() so the seed wires it directly.
//
// CSW_SECRET_KEY is only required when a row carries a real secret
// (today that's never — none of the seeded adapters need api_keys).
// The Cipher is loaded lazily to keep dev setups without a master
// key usable for first-time seed.
func runSeed(ctx context.Context, dsn string) error {
	cfg, err := config.Load(nil)
	if err != nil {
		return fmt.Errorf("seed: load config: %w", err)
	}

	db, err := persistence.OpenDB(dsn)
	if err != nil {
		return fmt.Errorf("seed: open db: %w", err)
	}
	defer func() { _ = persistence.Close(db) }()
	repo := persistence.NewSourceRepo(db)

	// Empty-only semantics: if any row exists, abort. The operator
	// edits via the API from here on.
	for _, c := range cfg.Chains {
		existing, err := repo.ListByChain(ctx, chain.ChainID(c.ID), false)
		if err != nil {
			return fmt.Errorf("seed: check existing: %w", err)
		}
		if len(existing) > 0 {
			return fmt.Errorf("seed: chain %d already has %d rows — table is the source of truth now", c.ID, len(existing))
		}
	}

	var cipher *secrets.Cipher
	if os.Getenv(secrets.EnvKeyName) != "" {
		cipher, err = secrets.Load()
		if err != nil {
			return fmt.Errorf("seed: load cipher: %w", err)
		}
	}

	now := time.Now().UTC()
	inserted := 0
	for _, c := range cfg.Chains {
		chainID := chain.ChainID(c.ID)

		if cfg.Adapters.RPC.Enabled {
			if url, ok := cfg.Adapters.RPC.Endpoints[c.ID]; ok && url != "" {
				s := application.SourceConfig{
					ID:       fmt.Sprintf("%s-%d", gateway.TypeRPC, c.ID),
					ChainID:  chainID,
					Type:     gateway.TypeRPC,
					Endpoint: url,
					Options: map[string]any{
						"archive": cfg.Adapters.RPC.Archive,
					},
					Enabled:   true,
					CreatedAt: now,
					UpdatedAt: now,
				}
				if err := repo.Save(ctx, s); err != nil {
					return fmt.Errorf("seed: save rpc chain %d: %w", c.ID, err)
				}
				inserted++
			}
		}

		if cfg.Adapters.Blockscout.Enabled {
			if url, ok := cfg.Adapters.Blockscout.Endpoints[c.ID]; ok && url != "" {
				s := application.SourceConfig{
					ID:        fmt.Sprintf("%s-%d", gateway.TypeBlockscout, c.ID),
					ChainID:   chainID,
					Type:      gateway.TypeBlockscout,
					Endpoint:  url,
					Options:   map[string]any{},
					Enabled:   true,
					CreatedAt: now,
					UpdatedAt: now,
				}
				if err := repo.Save(ctx, s); err != nil {
					return fmt.Errorf("seed: save blockscout chain %d: %w", c.ID, err)
				}
				inserted++
			}
		}

		// Routescan has no EndpointMap — its BaseURL is computed
		// from chain id. We seed it unconditionally; operators who
		// don't want it can disable via the API after seeding.
		rs := application.SourceConfig{
			ID:        fmt.Sprintf("%s-%d", gateway.TypeRoutescan, c.ID),
			ChainID:   chainID,
			Type:      gateway.TypeRoutescan,
			Endpoint:  routescan.BaseURL(chainID),
			Options:   map[string]any{},
			Enabled:   true,
			CreatedAt: now,
			UpdatedAt: now,
		}
		if err := repo.Save(ctx, rs); err != nil {
			return fmt.Errorf("seed: save routescan chain %d: %w", c.ID, err)
		}
		inserted++
	}

	// Avoid "unused" compiler complaints on cipher when no seeded
	// adapter needs it today. The variable exists so the 10a follow-
	// up (etherscan / custom indexer seeding) can flip into using it
	// without touching this skeleton.
	_ = cipher

	fmt.Printf("seed: inserted %d source config rows across %d chains\n", inserted, len(cfg.Chains))
	return nil
}
