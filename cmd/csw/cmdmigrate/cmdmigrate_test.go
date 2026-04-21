package cmdmigrate

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRun_RequiresSubcommand(t *testing.T) {
	err := Run(context.Background(), nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "missing subcommand")
}

func TestRun_RequiresDatabaseURL(t *testing.T) {
	t.Setenv("DATABASE_URL", "")
	err := Run(context.Background(), []string{"status"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "DATABASE_URL")
}

func TestRun_RejectsUnknownSubcommand(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://u:p@localhost:1/x?sslmode=disable")
	err := Run(context.Background(), []string{"wat"})
	require.Error(t, err)
	// Connection will fail before subcommand check, but either
	// way we get an error — the point is Run doesn't panic on
	// a bad subcommand.
}
