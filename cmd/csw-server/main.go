// Command csw-server is the HTTP API process. It fronts the
// Postgres-backed RunRepository / DiffRepository / ScheduleRepository
// via the application use cases and exposes them as a huma-managed
// REST surface plus an OpenAPI 3.1 document at /openapi.json. A
// liveness (/healthz) and a readiness (/readyz) probe ship alongside
// so Kubernetes rollouts can gate traffic.
//
// SourceGateway is DB-backed (gateway.DBGateway over the sources
// table; Phase 10a). A fresh database requires `csw migrate seed`
// to populate adapter rows from the embedded defaults.yaml, after
// which the table is the single source of truth and the admin UI
// takes over via /sources CRUD.
//
// Environment:
//
//	DATABASE_URL — Postgres DSN for repositories and readiness.
//	REDIS_URL    — asynq Redis DSN for dispatcher + readiness.
//	CSW_SERVER_ADDR — optional listener, defaults to :8080.
//	CSW_SERVER_CORS_ORIGINS — comma-separated allow-list for the
//	              frontend dev server; empty disables CORS.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/hibiken/asynq"
	"gorm.io/gorm"

	"github.com/seokheejang/chain-sync-watch/internal/application"
	"github.com/seokheejang/chain-sync-watch/internal/config"
	"github.com/seokheejang/chain-sync-watch/internal/diff"
	"github.com/seokheejang/chain-sync-watch/internal/infrastructure/gateway"
	"github.com/seokheejang/chain-sync-watch/internal/infrastructure/httpapi"
	"github.com/seokheejang/chain-sync-watch/internal/infrastructure/httpapi/dto"
	"github.com/seokheejang/chain-sync-watch/internal/infrastructure/httpapi/routes"
	"github.com/seokheejang/chain-sync-watch/internal/infrastructure/persistence"
	"github.com/seokheejang/chain-sync-watch/internal/infrastructure/queue"
	"github.com/seokheejang/chain-sync-watch/internal/infrastructure/stubs"
	"github.com/seokheejang/chain-sync-watch/internal/secrets"
)

func main() {
	os.Exit(mainRun())
}

func mainRun() int {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	if err := run(ctx, logger); err != nil {
		logger.Error("server exited", "err", err)
		return 1
	}
	return 0
}

func run(ctx context.Context, logger *slog.Logger) error {
	cfg, err := config.Load(nil)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		return errors.New("DATABASE_URL env var is required")
	}
	redisURL := os.Getenv("REDIS_URL")
	if redisURL == "" {
		return errors.New("REDIS_URL env var is required")
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

	// Optional master key — loaded when set so adapter CRUD can
	// encrypt / decrypt api_keys. A deployment with zero
	// credential-requiring sources can run without it.
	var cipher *secrets.Cipher
	if os.Getenv(secrets.EnvKeyName) != "" {
		cipher, err = secrets.Load()
		if err != nil {
			return fmt.Errorf("load secret key: %w", err)
		}
	} else {
		logger.Warn("CSW_SECRET_KEY not set; source CRUD with api_keys will be rejected")
	}

	deps := buildDeps(db, redisOpt, cipher, cfg)

	addr := envOrDefault("CSW_SERVER_ADDR", ":8080")
	corsOrigins := splitNonEmpty(os.Getenv("CSW_SERVER_CORS_ORIGINS"))

	srv := httpapi.NewServer(httpapi.Config{
		Addr:         addr,
		CORSOrigins:  corsOrigins,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		Logger:       logger,
	}, deps)

	errCh := make(chan error, 1)
	go func() {
		logger.Info("http server listening", "addr", addr)
		if serveErr := srv.ListenAndServe(); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			errCh <- serveErr
		}
	}()

	select {
	case <-ctx.Done():
		logger.Info("received shutdown signal")
	case err := <-errCh:
		return fmt.Errorf("http server: %w", err)
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Warn("server shutdown", "err", err)
	}
	return nil
}

// buildDeps assembles the full httpapi.Deps tree: repositories from
// the Postgres handle, a dispatcher on the Redis handle, a DB-backed
// SourceGateway + RPC ChainHead, and the application use cases
// that bind them. Phase 10a completes the stubs → real-infra
// transition started in Phase 10a.1–10a.7.
func buildDeps(db *gorm.DB, redisOpt asynq.RedisConnOpt, cipher *secrets.Cipher, cfg *config.Config) httpapi.Deps {
	runs := persistence.NewRunRepo(db)
	diffs := persistence.NewDiffRepo(db)
	schedules := persistence.NewScheduleRepo(db)
	sourcesRepo := persistence.NewSourceRepo(db)

	clock := stubs.SystemClock{}
	dispatcher := queue.NewDispatcher(redisOpt, schedules)
	reg := gateway.DefaultRegistry()
	// registerPrivateAdapters is a no-op in the default build and
	// plugs in private/<user-package>/Register calls under the
	// `private` build tag. See cmd/csw-server/private_on.go.
	registerPrivateAdapters(reg)
	dbGateway := gateway.NewDBGateway(sourcesRepo, cipher, reg)
	policy := diff.DefaultPolicy{}

	schedule := &application.ScheduleRun{Runs: runs, Dispatcher: dispatcher, Clock: clock}
	cancel := &application.CancelRun{Runs: runs, Clock: clock}
	replay := &application.ReplayDiff{
		Diffs:     diffs,
		Sources:   dbGateway,
		Clock:     clock,
		Policy:    policy,
		Tolerance: application.DefaultToleranceResolver{},
	}

	readiness := readinessProbe{db: db, redisOpt: redisOpt}

	return httpapi.Deps{
		Health: routes.HealthDeps{Readiness: readiness},
		Runs: routes.RunsDeps{
			Schedule: schedule,
			Query:    application.QueryRuns{Runs: runs},
			Cancel:   cancel,
		},
		Diffs: routes.DiffsDeps{
			Query:  application.QueryDiffs{Diffs: diffs},
			Replay: replay,
		},
		Schedules: routes.SchedulesDeps{
			Schedule:   schedule,
			Query:      application.QuerySchedules{Schedules: schedules},
			Dispatcher: dispatcher,
		},
		Sources: routes.SourcesDeps{
			Repo:    sourcesRepo,
			Gateway: dbGateway,
			Cipher:  cipher,
			Clock:   clock,
			Types:   registryKeys(reg),
		},
		Chains: routes.ChainsDeps{Catalog: chainCatalog(cfg)},
	}
}

// chainCatalog projects config.ChainConfig entries into the wire DTO
// the /chains endpoint serves. Stays next to buildDeps so the one
// place that knows both shapes is the wiring layer.
func chainCatalog(cfg *config.Config) []dto.ChainView {
	out := make([]dto.ChainView, len(cfg.Chains))
	for i, c := range cfg.Chains {
		out[i] = dto.ChainView{
			ID:          c.ID,
			Slug:        c.Slug,
			DisplayName: c.DisplayName,
		}
	}
	return out
}

// readinessProbe combines a Postgres ping and a Redis ping. Either
// failing flips /readyz to 503, which is what Kubernetes wants for
// a rolling-restart gate.
type readinessProbe struct {
	db       *gorm.DB
	redisOpt asynq.RedisConnOpt
}

func (p readinessProbe) Ready(ctx context.Context) error {
	if err := persistence.Ping(ctx, p.db); err != nil {
		return fmt.Errorf("postgres: %w", err)
	}
	if err := pingRedis(ctx, p.redisOpt); err != nil {
		return fmt.Errorf("redis: %w", err)
	}
	return nil
}

// pingRedis issues a TCP connect to the Redis address asynq exposes.
// Using the asynq-returned RedisConnOpt keeps auth/URL parsing
// consistent with the worker.
func pingRedis(ctx context.Context, opt asynq.RedisConnOpt) error {
	rc, ok := opt.(asynq.RedisClientOpt)
	if !ok {
		return errors.New("unsupported redis opt")
	}
	dialer := &net.Dialer{}
	conn, err := dialer.DialContext(ctx, "tcp", rc.Addr)
	if err != nil {
		return err
	}
	return conn.Close()
}

// registryKeys returns a stable, sorted slice of the Registry's
// type strings. Routes expose this via GET /sources/types.
func registryKeys(r gateway.Registry) []string {
	out := make([]string, 0, len(r))
	for k := range r {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func splitNonEmpty(s string) []string {
	if s == "" {
		return nil
	}
	out := []string{}
	for _, part := range strings.Split(s, ",") {
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}
