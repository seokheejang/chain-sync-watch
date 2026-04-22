//go:build integration

package persistence_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
	"gorm.io/gorm"

	"github.com/seokheejang/chain-sync-watch/internal/infrastructure/persistence"
	"github.com/seokheejang/chain-sync-watch/migrations"
)

// Shared across every test in this package (build tag: integration).
// Spawning one Postgres container per test file is expensive enough
// that a single boot amortises across the whole suite.
var testDB *gorm.DB

func TestMain(m *testing.M) {
	ctx := context.Background()

	container, err := tcpostgres.Run(ctx,
		"postgres:17-alpine",
		tcpostgres.WithDatabase("csw_test"),
		tcpostgres.WithUsername("csw"),
		tcpostgres.WithPassword("csw"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).WithStartupTimeout(60*time.Second),
		),
	)
	if err != nil {
		fmt.Fprintln(os.Stderr, "integration: start postgres:", err)
		os.Exit(1)
	}
	defer func() { _ = container.Terminate(ctx) }()

	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		fmt.Fprintln(os.Stderr, "integration: dsn:", err)
		os.Exit(1)
	}

	if err := runMigrations(dsn); err != nil {
		fmt.Fprintln(os.Stderr, "integration: migrate:", err)
		os.Exit(1)
	}

	db, err := persistence.OpenDB(dsn)
	if err != nil {
		fmt.Fprintln(os.Stderr, "integration: gorm open:", err)
		os.Exit(1)
	}
	testDB = db

	code := m.Run()
	_ = persistence.Close(db)
	os.Exit(code)
}

func runMigrations(dsn string) error {
	src, err := iofs.New(migrations.FS, ".")
	if err != nil {
		return err
	}
	mg, err := migrate.NewWithSourceInstance("iofs", src, dsn)
	if err != nil {
		return err
	}
	defer func() { _, _ = mg.Close() }()
	if err := mg.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return err
	}
	return nil
}

// resetDB wipes every table between test cases.
func resetDB(t *testing.T) {
	t.Helper()
	if err := testDB.Exec("TRUNCATE runs, discrepancies, schedules RESTART IDENTITY CASCADE").Error; err != nil {
		t.Fatalf("reset: %v", err)
	}
}
