package queue

import (
	"fmt"
	"time"

	"github.com/hibiken/asynq"
)

// Scheduler wraps asynq's PeriodicTaskManager into a start/stop
// pair aligned with the rest of the worker lifecycle. The sync
// interval controls how often the manager re-queries the
// ConfigProvider for changes — 10s is asynq's default and a sane
// floor for human-edited cron schedules.
type Scheduler struct {
	manager *asynq.PeriodicTaskManager
}

// SchedulerOptions captures the knobs a caller might tune.
type SchedulerOptions struct {
	// SyncInterval controls how often PeriodicTaskManager re-reads
	// the ConfigProvider. 0 falls back to 10s.
	SyncInterval time.Duration
}

// NewScheduler constructs a Scheduler around the given Redis
// connection options and ConfigProvider. The provider is usually
// Dispatcher.ConfigProvider() so enqueue and schedule share an
// in-process store.
func NewScheduler(
	opt asynq.RedisConnOpt,
	provider asynq.PeriodicTaskConfigProvider,
	options SchedulerOptions,
) (*Scheduler, error) {
	if provider == nil {
		return nil, fmt.Errorf("queue: scheduler: nil config provider")
	}
	interval := options.SyncInterval
	if interval <= 0 {
		interval = 10 * time.Second
	}
	mgr, err := asynq.NewPeriodicTaskManager(asynq.PeriodicTaskManagerOpts{
		RedisConnOpt:               opt,
		PeriodicTaskConfigProvider: provider,
		SyncInterval:               interval,
	})
	if err != nil {
		return nil, fmt.Errorf("queue: new periodic task manager: %w", err)
	}
	return &Scheduler{manager: mgr}, nil
}

// Start runs the scheduler in a background goroutine. It returns
// after the initial sync completes; the manager keeps polling until
// Shutdown is called.
func (s *Scheduler) Start() error {
	if err := s.manager.Start(); err != nil {
		return fmt.Errorf("queue: start scheduler: %w", err)
	}
	return nil
}

// Shutdown stops the polling goroutine. Safe to call multiple times.
func (s *Scheduler) Shutdown() { s.manager.Shutdown() }
