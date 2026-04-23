package persistence

import (
	"time"

	"github.com/lib/pq"
)

// runModel mirrors the `runs` table. Fields are unexported to the
// package so only the mapper interacts with gorm's reflection
// pipeline; the rest of the codebase stays on domain aggregates.
//
// AddressPlans / TokenPlans are JSONB arrays of tagged envelopes
// serialising the Run's []AddressSamplingPlan and []TokenSamplingPlan.
// Migrations 002 / 005 introduced the columns with a "[]"-literal
// default, so rows that predate them round-trip to an empty slice
// on Rehydrate.
type runModel struct {
	ID           string         `gorm:"primaryKey;column:id"`
	ChainID      uint64         `gorm:"column:chain_id;not null"`
	Status       string         `gorm:"column:status;not null"`
	TriggerType  string         `gorm:"column:trigger_type;not null"`
	TriggerData  []byte         `gorm:"column:trigger_data;type:jsonb;not null"`
	StrategyKind string         `gorm:"column:strategy_kind;not null"`
	StrategyData []byte         `gorm:"column:strategy_data;type:jsonb;not null"`
	AddressPlans []byte         `gorm:"column:address_plans;type:jsonb;not null;default:'[]'::jsonb"`
	TokenPlans   []byte         `gorm:"column:token_plans;type:jsonb;not null;default:'[]'::jsonb"`
	Metrics      pq.StringArray `gorm:"column:metrics;type:text[];not null"`
	ErrorMsg     string         `gorm:"column:error_msg;not null;default:''"`
	CreatedAt    time.Time      `gorm:"column:created_at;not null"`
	StartedAt    *time.Time     `gorm:"column:started_at"`
	FinishedAt   *time.Time     `gorm:"column:finished_at"`
}

// TableName pins the table name so gorm's default pluraliser does
// not produce something unexpected from the unexported type name.
func (runModel) TableName() string { return "runs" }

// diffModel mirrors the `discrepancies` table. BlockNumber is int64
// because Postgres BIGINT is signed — real block heights stay well
// inside the positive range.
type diffModel struct {
	ID             int64          `gorm:"primaryKey;column:id;autoIncrement"`
	RunID          string         `gorm:"column:run_id;not null;index"`
	MetricKey      string         `gorm:"column:metric_key;not null"`
	MetricCategory string         `gorm:"column:metric_category;not null"`
	BlockNumber    int64          `gorm:"column:block_number;not null"`
	SubjectType    string         `gorm:"column:subject_type;not null"`
	SubjectAddr    []byte         `gorm:"column:subject_addr"`
	Values         []byte         `gorm:"column:values;type:jsonb;not null"`
	Severity       string         `gorm:"column:severity;not null"`
	TrustedSources pq.StringArray `gorm:"column:trusted_sources;type:text[];not null"`
	Reasoning      string         `gorm:"column:reasoning;not null;default:''"`
	Resolved       bool           `gorm:"column:resolved;not null;default:false"`
	ResolvedAt     *time.Time     `gorm:"column:resolved_at"`
	DetectedAt     time.Time      `gorm:"column:detected_at;not null"`
	Tier           *int16         `gorm:"column:tier"`
	AnchorBlock    *int64         `gorm:"column:anchor_block"`
	SamplingSeed   *int64         `gorm:"column:sampling_seed"`
}

// TableName returns the fixed table name.
func (diffModel) TableName() string { return "discrepancies" }

// scheduleModel mirrors the `schedules` table. job_id is the
// primary key rather than an auto-increment id because the
// Dispatcher assigns a random hex id and uses it as the asynq
// cancellation key — upserts must key on the external id.
//
// Active is a soft-delete flag; CancelScheduled flips it to false
// so the provider stops emitting the task at the next poll while
// the audit trail (who cancelled what, when) survives.
type scheduleModel struct {
	JobID        string         `gorm:"primaryKey;column:job_id"`
	ChainID      uint64         `gorm:"column:chain_id;not null"`
	CronExpr     string         `gorm:"column:cron_expr;not null"`
	Timezone     string         `gorm:"column:timezone;not null;default:'UTC'"`
	StrategyKind string         `gorm:"column:strategy_kind;not null"`
	StrategyData []byte         `gorm:"column:strategy_data;type:jsonb;not null"`
	AddressPlans []byte         `gorm:"column:address_plans;type:jsonb;not null;default:'[]'::jsonb"`
	TokenPlans   []byte         `gorm:"column:token_plans;type:jsonb;not null;default:'[]'::jsonb"`
	Metrics      pq.StringArray `gorm:"column:metrics;type:text[];not null"`
	Active       bool           `gorm:"column:active;not null;default:true"`
	CreatedAt    time.Time      `gorm:"column:created_at;not null"`
}

// TableName pins the table name.
func (scheduleModel) TableName() string { return "schedules" }
