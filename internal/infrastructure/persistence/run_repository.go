package persistence

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/seokheejang/chain-sync-watch/internal/application"
	"github.com/seokheejang/chain-sync-watch/internal/verification"
)

// RunRepo is the Postgres + gorm RunRepository. Save is upsert on
// the primary key so state transitions reissue Save without a prior
// Find; FindByID returns application.ErrRunNotFound on a missing
// row so callers can match with errors.Is without importing gorm.
type RunRepo struct {
	db *gorm.DB
}

// NewRunRepo constructs a RunRepo around an opened *gorm.DB.
func NewRunRepo(db *gorm.DB) *RunRepo { return &RunRepo{db: db} }

// Save upserts the Run by primary key.
func (r *RunRepo) Save(ctx context.Context, run *verification.Run) error {
	m, err := toRunModel(run)
	if err != nil {
		return fmt.Errorf("run repo save: %w", err)
	}
	res := r.db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "id"}},
			UpdateAll: true,
		}).
		Create(&m)
	if res.Error != nil {
		return fmt.Errorf("run repo save: %w", res.Error)
	}
	return nil
}

// FindByID fetches by primary key.
func (r *RunRepo) FindByID(ctx context.Context, id verification.RunID) (*verification.Run, error) {
	var m runModel
	if err := r.db.WithContext(ctx).First(&m, "id = ?", string(id)).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, application.ErrRunNotFound
		}
		return nil, fmt.Errorf("run repo find: %w", err)
	}
	return toRun(m)
}

// List applies the filter and returns the filtered slice plus the
// total count of rows matching the filter (pre-pagination).
func (r *RunRepo) List(ctx context.Context, f application.RunFilter) ([]*verification.Run, int, error) {
	q := r.db.WithContext(ctx).Model(&runModel{})
	if f.ChainID != nil {
		q = q.Where("chain_id = ?", f.ChainID.Uint64())
	}
	if f.Status != nil {
		q = q.Where("status = ?", string(*f.Status))
	}
	if f.CreatedAt != nil {
		q = q.Where("created_at BETWEEN ? AND ?", f.CreatedAt.From, f.CreatedAt.To)
	}

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("run repo list count: %w", err)
	}

	q = q.Order("created_at DESC")
	if f.Limit > 0 {
		q = q.Limit(f.Limit)
	}
	if f.Offset > 0 {
		q = q.Offset(f.Offset)
	}

	var rows []runModel
	if err := q.Find(&rows).Error; err != nil {
		return nil, 0, fmt.Errorf("run repo list: %w", err)
	}

	out := make([]*verification.Run, 0, len(rows))
	for i := range rows {
		r, err := toRun(rows[i])
		if err != nil {
			return nil, 0, fmt.Errorf("run repo list decode: %w", err)
		}
		out = append(out, r)
	}
	return out, int(total), nil
}

// PruneFinishedBefore deletes terminal runs whose finished_at falls
// strictly before the given cutoff. Discrepancies belonging to those
// runs CASCADE per the migration 001 foreign key. Returns the number
// of run rows removed so the retention task can surface it in logs.
//
// Rows with finished_at IS NULL (pending / running) are never
// touched — retention sweeps only reclaim storage from terminal
// history, never cancel in-flight work.
func (r *RunRepo) PruneFinishedBefore(ctx context.Context, cutoff time.Time) (int64, error) {
	res := r.db.WithContext(ctx).Where(
		"finished_at IS NOT NULL AND finished_at < ?", cutoff,
	).Delete(&runModel{})
	if res.Error != nil {
		return 0, fmt.Errorf("run repo prune: %w", res.Error)
	}
	return res.RowsAffected, nil
}

// Compile-time assertion that RunRepo satisfies the port.
var _ application.RunRepository = (*RunRepo)(nil)
