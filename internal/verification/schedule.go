package verification

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// Schedule pairs a cron expression with an IANA timezone. It is a
// value object: construction validates shape up front so every
// Schedule the domain hands out is at least structurally sensible.
//
// The cron parser and the firing loop live in infrastructure (Phase
// 7). Validation here is intentionally shallow — field count and
// character allowlist — because a full parser would pull in external
// dependencies forbidden in the domain layer, and the scheduler will
// reject anything that slips through anyway.
type Schedule struct {
	cronExpr string
	timezone *time.Location
}

// NewSchedule validates the cron expression and timezone and
// returns a ready-to-use Schedule. Empty or whitespace-only
// expressions are rejected. Empty timezone defaults to UTC, matching
// time.LoadLocation semantics.
func NewSchedule(cronExpr, tz string) (Schedule, error) {
	cronExpr = strings.TrimSpace(cronExpr)
	if cronExpr == "" {
		return Schedule{}, errors.New("schedule: cron expression is empty")
	}
	if err := validateCronShape(cronExpr); err != nil {
		return Schedule{}, fmt.Errorf("schedule: %w", err)
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return Schedule{}, fmt.Errorf("schedule: timezone %q: %w", tz, err)
	}
	return Schedule{cronExpr: cronExpr, timezone: loc}, nil
}

// CronExpr returns the validated cron expression.
func (s Schedule) CronExpr() string { return s.cronExpr }

// Timezone returns the resolved IANA location. A zero Schedule
// returns a nil pointer; callers that accept zero values must
// handle that case.
func (s Schedule) Timezone() *time.Location { return s.timezone }

// IsZero reports whether the Schedule has never been initialised.
// Useful for struct fields where Schedule is optional.
func (s Schedule) IsZero() bool { return s.cronExpr == "" && s.timezone == nil }

// validateCronShape is the domain-layer sanity check: 5 or 6
// whitespace-separated fields, each made of the cron character
// alphabet. It exists so the scheduler never has to parse garbage,
// but it does not claim to evaluate semantic correctness (e.g., a
// "day 32" field would pass here and fail in the real parser).
func validateCronShape(expr string) error {
	fields := strings.Fields(expr)
	if len(fields) != 5 && len(fields) != 6 {
		return fmt.Errorf("cron expression must have 5 or 6 fields, got %d: %q",
			len(fields), expr)
	}
	for i, f := range fields {
		if !cronFieldChars(f) {
			return fmt.Errorf("cron field %d has invalid characters: %q", i+1, f)
		}
	}
	return nil
}

// cronFieldChars returns true when s consists entirely of the
// characters a cron field may contain. It does NOT parse ranges or
// step semantics — only shape.
func cronFieldChars(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9':
		case r == '*', r == '/', r == ',', r == '-', r == '?':
		default:
			return false
		}
	}
	return true
}
