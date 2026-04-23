package persistence

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/seokheejang/chain-sync-watch/internal/application"
	"github.com/seokheejang/chain-sync-watch/internal/chain"
)

// SourceRepo is the Postgres + gorm SourceConfigRepository.
type SourceRepo struct {
	db *gorm.DB
}

// NewSourceRepo constructs a SourceRepo around an opened *gorm.DB.
func NewSourceRepo(db *gorm.DB) *SourceRepo { return &SourceRepo{db: db} }

// Save upserts by primary key. A UNIQUE(type, chain_id) violation
// surfaces as application.ErrDuplicateSource so the HTTP handler
// can map to 409.
func (r *SourceRepo) Save(ctx context.Context, s application.SourceConfig) error {
	m, err := toSourceModel(s)
	if err != nil {
		return fmt.Errorf("source repo save: %w", err)
	}
	res := r.db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "id"}},
			UpdateAll: true,
		}).
		Create(&m)
	if res.Error != nil {
		if isUniqueViolation(res.Error, "sources_type_chain_unique") {
			return application.ErrDuplicateSource
		}
		return fmt.Errorf("source repo save: %w", res.Error)
	}
	return nil
}

// FindByID returns ErrSourceNotFound when the id is free.
func (r *SourceRepo) FindByID(ctx context.Context, id string) (*application.SourceConfig, error) {
	var m sourceModel
	err := r.db.WithContext(ctx).First(&m, "id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, application.ErrSourceNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("source repo find: %w", err)
	}
	cfg, err := toSourceConfig(m)
	if err != nil {
		return nil, fmt.Errorf("source repo find decode: %w", err)
	}
	return &cfg, nil
}

// ListByChain returns rows for chainID, optionally filtered to
// enabled rows only. Deterministic ordering by type keeps listing
// snapshots stable for the UI.
func (r *SourceRepo) ListByChain(ctx context.Context, chainID chain.ChainID, enabledOnly bool) ([]application.SourceConfig, error) {
	q := r.db.WithContext(ctx).Where("chain_id = ?", chainID.Uint64())
	if enabledOnly {
		q = q.Where("enabled = ?", true)
	}
	var rows []sourceModel
	if err := q.Order("type ASC").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("source repo list: %w", err)
	}
	out := make([]application.SourceConfig, 0, len(rows))
	for i := range rows {
		cfg, err := toSourceConfig(rows[i])
		if err != nil {
			return nil, fmt.Errorf("source repo list decode: %w", err)
		}
		out = append(out, cfg)
	}
	return out, nil
}

// Delete removes a row by id. Missing id is a no-op — HTTP DELETE
// is idempotent by contract.
func (r *SourceRepo) Delete(ctx context.Context, id string) error {
	res := r.db.WithContext(ctx).Delete(&sourceModel{}, "id = ?", id)
	if res.Error != nil {
		return fmt.Errorf("source repo delete: %w", res.Error)
	}
	return nil
}

// toSourceModel converts the domain DTO into a gorm row. Options is
// marshalled to JSONB bytes; an empty map serialises to "{}" rather
// than nil so the NOT NULL column default stays intact on update.
func toSourceModel(s application.SourceConfig) (sourceModel, error) {
	opts := s.Options
	if opts == nil {
		opts = map[string]any{}
	}
	data, err := json.Marshal(opts)
	if err != nil {
		return sourceModel{}, fmt.Errorf("marshal options: %w", err)
	}
	return sourceModel{
		ID:               s.ID,
		ChainID:          s.ChainID.Uint64(),
		Type:             s.Type,
		Endpoint:         s.Endpoint,
		SecretCiphertext: s.SecretCiphertext,
		SecretNonce:      s.SecretNonce,
		Options:          data,
		Enabled:          s.Enabled,
		CreatedAt:        s.CreatedAt,
		UpdatedAt:        s.UpdatedAt,
	}, nil
}

// toSourceConfig reverses toSourceModel. Empty / NULL Options
// decodes to an empty map so downstream code never branches on
// "do I have options?".
func toSourceConfig(m sourceModel) (application.SourceConfig, error) {
	opts := map[string]any{}
	if len(m.Options) > 0 && string(m.Options) != "null" {
		if err := json.Unmarshal(m.Options, &opts); err != nil {
			return application.SourceConfig{}, fmt.Errorf("unmarshal options: %w", err)
		}
	}
	return application.SourceConfig{
		ID:               m.ID,
		ChainID:          chain.ChainID(m.ChainID),
		Type:             m.Type,
		Endpoint:         m.Endpoint,
		SecretCiphertext: m.SecretCiphertext,
		SecretNonce:      m.SecretNonce,
		Options:          opts,
		Enabled:          m.Enabled,
		CreatedAt:        m.CreatedAt,
		UpdatedAt:        m.UpdatedAt,
	}, nil
}

// isUniqueViolation reports whether err is a Postgres "unique
// constraint violation" on constraint name. gorm wraps pgx errors
// transparently, so we unwrap to *pgconn.PgError.
func isUniqueViolation(err error, constraint string) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return false
	}
	// 23505 = unique_violation
	return pgErr.Code == "23505" && strings.Contains(pgErr.ConstraintName, constraint)
}

// Compile-time assertion.
var _ application.SourceConfigRepository = (*SourceRepo)(nil)
