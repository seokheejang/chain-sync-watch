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

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres" // register postgres driver
	"github.com/golang-migrate/migrate/v4/source/iofs"

	"github.com/seokheejang/chain-sync-watch/migrations"
)

// Run dispatches `csw migrate <subcommand>`. Supported subcommands:
// up, down, status. DATABASE_URL is mandatory.
func Run(_ context.Context, args []string) error {
	if len(args) == 0 {
		return errors.New("migrate: missing subcommand (up | down | status)")
	}
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		return errors.New("migrate: DATABASE_URL env var is required")
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
