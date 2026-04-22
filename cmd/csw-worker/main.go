// Command csw-worker is the asynq worker process. It drains
// verification:execute_run and verification:scheduled_run tasks
// from Redis, invokes the ExecuteRun use case against the Postgres-
// backed repositories, and exposes /healthz + /readyz for liveness
// and readiness probes.
//
// Environment:
//
//	REDIS_URL     — asynq Redis DSN (e.g. redis://localhost:6379/0)
//	DATABASE_URL  — Postgres DSN for RunRepository / DiffRepository
//	CSW_WORKER_HEALTH_ADDR — optional, defaults to :8081
//	CSW_WORKER_CONCURRENCY — optional integer, defaults to 10
//
// Scope note (Phase 7A): the SourceGateway and ChainHead are stub
// implementations that always return "not configured". ExecuteRun
// will mark a Run as failed with a clear message until Phase 10
// wires real adapters from config. The worker still boots, connects
// to Redis, answers health probes, and processes the task queue —
// which is exactly what Phase 7A covers.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/hibiken/asynq"

	"github.com/seokheejang/chain-sync-watch/internal/application"
	"github.com/seokheejang/chain-sync-watch/internal/chain"
	"github.com/seokheejang/chain-sync-watch/internal/diff"
	"github.com/seokheejang/chain-sync-watch/internal/infrastructure/persistence"
	"github.com/seokheejang/chain-sync-watch/internal/infrastructure/queue"
	"github.com/seokheejang/chain-sync-watch/internal/source"
)

func main() {
	code := mainRun()
	os.Exit(code)
}

func mainRun() int {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	if err := run(ctx, logger); err != nil {
		logger.Error("worker exited", "err", err)
		return 1
	}
	return 0
}

func run(ctx context.Context, logger *slog.Logger) error {
	redisURL := os.Getenv("REDIS_URL")
	if redisURL == "" {
		return errors.New("REDIS_URL env var is required")
	}
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		return errors.New("DATABASE_URL env var is required")
	}

	redisOpt, err := asynq.ParseRedisURI(redisURL)
	if err != nil {
		return fmt.Errorf("parse REDIS_URL: %w", err)
	}

	db, err := persistence.OpenDB(dbURL)
	if err != nil {
		return err
	}
	defer func() { _ = persistence.Close(db) }()

	runs := persistence.NewRunRepo(db)
	diffs := persistence.NewDiffRepo(db)
	schedules := persistence.NewScheduleRepo(db)

	clock := systemClock{}
	dispatcher := queue.NewDispatcher(redisOpt, schedules)
	defer func() { _ = dispatcher.Close() }()

	exec := &application.ExecuteRun{
		Runs:      runs,
		Diffs:     diffs,
		Sources:   nullGateway{},
		ChainHead: nullChainHead{},
		Clock:     clock,
		Policy:    diff.DefaultPolicy{},
	}

	// Handlers also process TaskTypeScheduledRun — that path needs
	// the Run repository (to persist the materialised Run), the
	// Dispatcher (to kick off the follow-up ExecuteRun task), and
	// the Clock (for CreatedAt stamping).
	handlers := &queue.Handlers{
		ExecuteRun: exec,
		Runs:       runs,
		Enqueuer:   dispatcher,
		Clock:      clock,
		Logger:     logger,
	}
	mux := asynq.NewServeMux()
	handlers.Register(mux)

	healthAddr := envOrDefault("CSW_WORKER_HEALTH_ADDR", ":8081")
	health := queue.NewHealthServer(healthAddr, redisOpt, logger)
	if err := health.Start(); err != nil {
		return err
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := health.Shutdown(shutdownCtx); err != nil {
			logger.Warn("health shutdown", "err", err)
		}
	}()
	logger.Info("health server ready", "addr", health.Addr())

	scheduler, err := queue.NewScheduler(redisOpt, dispatcher.ConfigProvider(), queue.SchedulerOptions{})
	if err != nil {
		return fmt.Errorf("new scheduler: %w", err)
	}
	if err := scheduler.Start(); err != nil {
		return fmt.Errorf("start scheduler: %w", err)
	}
	defer scheduler.Shutdown()
	logger.Info("scheduler started")

	srv := asynq.NewServer(redisOpt, asynq.Config{
		Concurrency: envOrDefaultInt("CSW_WORKER_CONCURRENCY", 10),
		Queues: map[string]int{
			queue.QueueDefault: 5,
		},
		Logger: slogAsynqAdapter{logger: logger},
	})

	errCh := make(chan error, 1)
	go func() { errCh <- srv.Run(mux) }()
	logger.Info("asynq server running")

	select {
	case <-ctx.Done():
		logger.Info("received shutdown signal")
	case err := <-errCh:
		return fmt.Errorf("asynq server: %w", err)
	}

	srv.Shutdown()
	return nil
}

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envOrDefaultInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

// --- Phase 7A stub ports ---------------------------------------------
//
// These zero-functionality implementations let the worker boot and
// answer health probes before Phase 10 wires real adapters from
// config. ExecuteRun will fail any Run it picks up here with a clear
// message, which is the intended signal for operators.

type nullGateway struct{}

func (nullGateway) ForChain(chain.ChainID) ([]source.Source, error) {
	return nil, nil
}

func (nullGateway) Get(id source.SourceID) (source.Source, error) {
	return nil, fmt.Errorf("no sources configured for %q", id)
}

type nullChainHead struct{}

func (nullChainHead) Tip(context.Context, chain.ChainID) (chain.BlockNumber, error) {
	return 0, errors.New("chain head not configured")
}

func (nullChainHead) Finalized(context.Context, chain.ChainID) (chain.BlockNumber, error) {
	return 0, errors.New("chain head not configured")
}

type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now() }

// slogAsynqAdapter forwards asynq's internal logs through slog so
// all worker logs share the same structured pipeline.
type slogAsynqAdapter struct {
	logger *slog.Logger
}

func (s slogAsynqAdapter) Debug(args ...any) { s.logger.Debug(fmt.Sprint(args...)) }
func (s slogAsynqAdapter) Info(args ...any)  { s.logger.Info(fmt.Sprint(args...)) }
func (s slogAsynqAdapter) Warn(args ...any)  { s.logger.Warn(fmt.Sprint(args...)) }
func (s slogAsynqAdapter) Error(args ...any) { s.logger.Error(fmt.Sprint(args...)) }
func (s slogAsynqAdapter) Fatal(args ...any) { s.logger.Error("fatal: " + fmt.Sprint(args...)) }
