package application

import "context"

// QuerySchedules is the read-side use case for recurring schedule
// configurations. It wraps ScheduleRepository.ListActive so HTTP
// handlers can inject a single use-case struct and policy hooks
// (auth, rate limiting) can land in one place.
type QuerySchedules struct {
	Schedules ScheduleRepository
}

// ListActive returns every schedule whose Active=true flag is set,
// sorted by CreatedAt ascending.
func (uc QuerySchedules) ListActive(ctx context.Context) ([]ScheduleRecord, error) {
	return uc.Schedules.ListActive(ctx)
}
