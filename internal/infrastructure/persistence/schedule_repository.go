package persistence

import (
	"context"
	"fmt"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/seokheejang/chain-sync-watch/internal/application"
)

// ScheduleRepo is the Postgres + gorm ScheduleRepository. Save is
// upsert on the JobID primary key so Dispatcher.ScheduleRecurring
// stays idempotent; Deactivate flips Active to false so cancelled
// schedules leave an audit trail rather than disappearing.
type ScheduleRepo struct {
	db *gorm.DB
}

// NewScheduleRepo constructs a ScheduleRepo around an opened *gorm.DB.
func NewScheduleRepo(db *gorm.DB) *ScheduleRepo { return &ScheduleRepo{db: db} }

// Save upserts the ScheduleRecord.
func (r *ScheduleRepo) Save(ctx context.Context, s application.ScheduleRecord) error {
	m, err := toScheduleModel(s)
	if err != nil {
		return fmt.Errorf("schedule repo save: %w", err)
	}
	res := r.db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "job_id"}},
			UpdateAll: true,
		}).
		Create(&m)
	if res.Error != nil {
		return fmt.Errorf("schedule repo save: %w", res.Error)
	}
	return nil
}

// Deactivate sets Active=false for the given JobID. A missing id is
// a no-op — CancelScheduled must be safe to call defensively.
func (r *ScheduleRepo) Deactivate(ctx context.Context, id application.JobID) error {
	res := r.db.WithContext(ctx).
		Model(&scheduleModel{}).
		Where("job_id = ?", string(id)).
		Update("active", false)
	if res.Error != nil {
		return fmt.Errorf("schedule repo deactivate: %w", res.Error)
	}
	return nil
}

// ListActive returns Active=true records in CreatedAt-ascending
// order so the periodic-task provider emits a stable list each
// poll.
func (r *ScheduleRepo) ListActive(ctx context.Context) ([]application.ScheduleRecord, error) {
	var rows []scheduleModel
	if err := r.db.WithContext(ctx).
		Where("active = ?", true).
		Order("created_at ASC").
		Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("schedule repo list: %w", err)
	}
	out := make([]application.ScheduleRecord, 0, len(rows))
	for i := range rows {
		rec, err := toScheduleRecord(rows[i])
		if err != nil {
			return nil, fmt.Errorf("schedule repo list decode: %w", err)
		}
		out = append(out, rec)
	}
	return out, nil
}

// Compile-time assertion.
var _ application.ScheduleRepository = (*ScheduleRepo)(nil)
