//go:build integration

package persistence_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/seokheejang/chain-sync-watch/internal/application"
	"github.com/seokheejang/chain-sync-watch/internal/chain"
	"github.com/seokheejang/chain-sync-watch/internal/infrastructure/persistence"
)

func mkSourceConfig(id, sourceType string, chainID chain.ChainID) application.SourceConfig {
	now := time.Now().UTC()
	return application.SourceConfig{
		ID:        id,
		ChainID:   chainID,
		Type:      sourceType,
		Endpoint:  "https://example.com",
		Options:   map[string]any{"rate_limit_rps": float64(10)},
		Enabled:   true,
		CreatedAt: now,
		UpdatedAt: now,
	}
}

func TestIntegrationSourceRepo_SaveAndFind(t *testing.T) {
	resetDB(t)
	repo := persistence.NewSourceRepo(testDB)
	ctx := context.Background()

	cfg := mkSourceConfig("rpc-opt", "rpc", chain.OptimismMainnet)
	cfg.SecretCiphertext = []byte{1, 2, 3}
	cfg.SecretNonce = []byte("nonce123456-")
	require.NoError(t, repo.Save(ctx, cfg))

	got, err := repo.FindByID(ctx, "rpc-opt")
	require.NoError(t, err)
	require.Equal(t, cfg.ID, got.ID)
	require.Equal(t, cfg.Type, got.Type)
	require.Equal(t, cfg.Endpoint, got.Endpoint)
	require.Equal(t, cfg.SecretCiphertext, got.SecretCiphertext)
	require.Equal(t, cfg.SecretNonce, got.SecretNonce)
	require.Equal(t, float64(10), got.Options["rate_limit_rps"])
	require.True(t, got.HasSecret())
}

func TestIntegrationSourceRepo_FindByID_NotFound(t *testing.T) {
	resetDB(t)
	repo := persistence.NewSourceRepo(testDB)
	_, err := repo.FindByID(context.Background(), "missing")
	require.ErrorIs(t, err, application.ErrSourceNotFound)
}

func TestIntegrationSourceRepo_ListByChain(t *testing.T) {
	resetDB(t)
	repo := persistence.NewSourceRepo(testDB)
	ctx := context.Background()

	rpc := mkSourceConfig("rpc-opt", "rpc", chain.OptimismMainnet)
	bs := mkSourceConfig("blockscout-opt", "blockscout", chain.OptimismMainnet)
	bs.Enabled = false
	otherChain := mkSourceConfig("rpc-other", "rpc", chain.ChainID(1))
	require.NoError(t, repo.Save(ctx, rpc))
	require.NoError(t, repo.Save(ctx, bs))
	require.NoError(t, repo.Save(ctx, otherChain))

	all, err := repo.ListByChain(ctx, chain.OptimismMainnet, false)
	require.NoError(t, err)
	require.Len(t, all, 2)

	onlyEnabled, err := repo.ListByChain(ctx, chain.OptimismMainnet, true)
	require.NoError(t, err)
	require.Len(t, onlyEnabled, 1)
	require.Equal(t, "rpc", onlyEnabled[0].Type)
}

func TestIntegrationSourceRepo_Save_DuplicateTypeChain(t *testing.T) {
	resetDB(t)
	repo := persistence.NewSourceRepo(testDB)
	ctx := context.Background()

	require.NoError(t, repo.Save(ctx, mkSourceConfig("rpc-opt", "rpc", chain.OptimismMainnet)))

	// Second row with same (type, chain_id) but different id violates
	// the UNIQUE constraint → sentinel error surfaces.
	err := repo.Save(ctx, mkSourceConfig("rpc-opt-alt", "rpc", chain.OptimismMainnet))
	require.Error(t, err)
	require.True(t, errors.Is(err, application.ErrDuplicateSource))
}

func TestIntegrationSourceRepo_Save_Upsert(t *testing.T) {
	resetDB(t)
	repo := persistence.NewSourceRepo(testDB)
	ctx := context.Background()

	cfg := mkSourceConfig("rpc-opt", "rpc", chain.OptimismMainnet)
	require.NoError(t, repo.Save(ctx, cfg))

	cfg.Endpoint = "https://alt.example.com"
	cfg.Enabled = false
	require.NoError(t, repo.Save(ctx, cfg))

	got, err := repo.FindByID(ctx, "rpc-opt")
	require.NoError(t, err)
	require.Equal(t, "https://alt.example.com", got.Endpoint)
	require.False(t, got.Enabled)
}

func TestIntegrationSourceRepo_Delete(t *testing.T) {
	resetDB(t)
	repo := persistence.NewSourceRepo(testDB)
	ctx := context.Background()

	require.NoError(t, repo.Save(ctx, mkSourceConfig("rpc-opt", "rpc", chain.OptimismMainnet)))
	require.NoError(t, repo.Delete(ctx, "rpc-opt"))
	_, err := repo.FindByID(ctx, "rpc-opt")
	require.ErrorIs(t, err, application.ErrSourceNotFound)

	// Missing id is a no-op.
	require.NoError(t, repo.Delete(ctx, "nonexistent"))
}

func TestIntegrationSourceRepo_SecretCheckConstraint(t *testing.T) {
	resetDB(t)
	repo := persistence.NewSourceRepo(testDB)
	ctx := context.Background()

	// Ciphertext without nonce must be rejected by the CHECK.
	cfg := mkSourceConfig("rpc-opt", "rpc", chain.OptimismMainnet)
	cfg.SecretCiphertext = []byte{1}
	cfg.SecretNonce = nil
	err := repo.Save(ctx, cfg)
	require.Error(t, err, "CHECK constraint must reject half-filled secrets")
}
