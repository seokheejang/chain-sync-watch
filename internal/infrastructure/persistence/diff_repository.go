package persistence

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"gorm.io/gorm"

	"github.com/seokheejang/chain-sync-watch/internal/application"
	"github.com/seokheejang/chain-sync-watch/internal/diff"
	"github.com/seokheejang/chain-sync-watch/internal/verification"
)

// DiffRepo is the Postgres + gorm DiffRepository. Save inserts a
// fresh row and returns the DB-assigned id as a DiffID string.
type DiffRepo struct {
	db *gorm.DB
}

// NewDiffRepo constructs a DiffRepo.
func NewDiffRepo(db *gorm.DB) *DiffRepo { return &DiffRepo{db: db} }

// Save inserts the Discrepancy + Judgement pair and returns the
// assigned DiffID.
func (r *DiffRepo) Save(ctx context.Context, d *diff.Discrepancy, j diff.Judgement) (application.DiffID, error) {
	m, err := toDiffModel(d, j)
	if err != nil {
		return "", fmt.Errorf("diff repo save: %w", err)
	}
	m.DetectedAt = nonZeroTime(m.DetectedAt)
	if err := r.db.WithContext(ctx).Create(&m).Error; err != nil {
		return "", fmt.Errorf("diff repo save: %w", err)
	}
	return application.DiffID(strconv.FormatInt(m.ID, 10)), nil
}

// FindByRun returns every DiffRecord for a given RunID in
// DetectedAt-ascending order (matches the order they were
// produced).
func (r *DiffRepo) FindByRun(ctx context.Context, runID verification.RunID) ([]application.DiffRecord, error) {
	var rows []diffModel
	err := r.db.WithContext(ctx).
		Where("run_id = ?", string(runID)).
		Order("detected_at ASC, id ASC").
		Find(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("diff repo find by run: %w", err)
	}
	out := make([]application.DiffRecord, 0, len(rows))
	for i := range rows {
		rec, err := toDiffRecord(rows[i])
		if err != nil {
			return nil, fmt.Errorf("diff repo decode: %w", err)
		}
		out = append(out, rec)
	}
	return out, nil
}

// FindByID returns the record or application.ErrDiffNotFound.
func (r *DiffRepo) FindByID(ctx context.Context, id application.DiffID) (*application.DiffRecord, error) {
	pk, err := strconv.ParseInt(string(id), 10, 64)
	if err != nil {
		return nil, application.ErrDiffNotFound
	}
	var m diffModel
	if err := r.db.WithContext(ctx).First(&m, "id = ?", pk).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, application.ErrDiffNotFound
		}
		return nil, fmt.Errorf("diff repo find by id: %w", err)
	}
	rec, err := toDiffRecord(m)
	if err != nil {
		return nil, fmt.Errorf("diff repo decode: %w", err)
	}
	return &rec, nil
}

// List applies the filter and returns filtered records plus total
// count (pre-pagination). Order: DetectedAt DESC, id DESC.
func (r *DiffRepo) List(ctx context.Context, f application.DiffFilter) ([]application.DiffRecord, int, error) {
	q := r.db.WithContext(ctx).Model(&diffModel{})
	if f.RunID != nil {
		q = q.Where("run_id = ?", string(*f.RunID))
	}
	if f.MetricKey != nil {
		q = q.Where("metric_key = ?", *f.MetricKey)
	}
	if f.Severity != nil {
		q = q.Where("severity = ?", string(*f.Severity))
	}
	if f.Resolved != nil {
		q = q.Where("resolved = ?", *f.Resolved)
	}
	if f.BlockRange != nil {
		q = q.Where("block_number BETWEEN ? AND ?",
			//nolint:gosec // G115: block heights stay within int64.
			int64(f.BlockRange.Start.Uint64()),
			//nolint:gosec // G115: block heights stay within int64.
			int64(f.BlockRange.End.Uint64()),
		)
	}

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("diff repo list count: %w", err)
	}

	q = q.Order("detected_at DESC, id DESC")
	if f.Limit > 0 {
		q = q.Limit(f.Limit)
	}
	if f.Offset > 0 {
		q = q.Offset(f.Offset)
	}

	var rows []diffModel
	if err := q.Find(&rows).Error; err != nil {
		return nil, 0, fmt.Errorf("diff repo list: %w", err)
	}
	out := make([]application.DiffRecord, 0, len(rows))
	for i := range rows {
		rec, err := toDiffRecord(rows[i])
		if err != nil {
			return nil, 0, fmt.Errorf("diff repo list decode: %w", err)
		}
		out = append(out, rec)
	}
	return out, int(total), nil
}

// MarkResolved flips resolved=true and stamps resolved_at.
func (r *DiffRepo) MarkResolved(ctx context.Context, id application.DiffID, at time.Time) error {
	pk, err := strconv.ParseInt(string(id), 10, 64)
	if err != nil {
		return application.ErrDiffNotFound
	}
	res := r.db.WithContext(ctx).
		Model(&diffModel{}).
		Where("id = ?", pk).
		Updates(map[string]any{
			"resolved":    true,
			"resolved_at": at,
		})
	if res.Error != nil {
		return fmt.Errorf("diff repo mark resolved: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return application.ErrDiffNotFound
	}
	return nil
}

// nonZeroTime guards against inserting a zero time.Time into a
// NOT NULL TIMESTAMPTZ column — callers that omit DetectedAt get
// a sensible "now".
func nonZeroTime(t time.Time) time.Time {
	if t.IsZero() {
		return time.Now().UTC()
	}
	return t
}

var _ application.DiffRepository = (*DiffRepo)(nil)
