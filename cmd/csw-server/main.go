// Command csw-server is the HTTP API process. It fronts the
// Postgres-backed RunRepository / DiffRepository via the application
// use cases and exposes them as a huma-managed REST surface plus an
// OpenAPI 3.1 document at /openapi.json. A liveness (/healthz) and
// a readiness (/readyz) probe ship alongside so Kubernetes rollouts
// can gate traffic.
//
// Environment:
//
//	DATABASE_URL — Postgres DSN for readiness probes and resource
//	              repositories.
//	REDIS_URL    — asynq Redis DSN; used by readiness.
//	CSW_SERVER_ADDR — optional listener, defaults to :8080.
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
	"strings"
	"syscall"
	"time"

	"github.com/hibiken/asynq"
	"gorm.io/gorm"

	"github.com/seokheejang/chain-sync-watch/internal/infrastructure/httpapi"
	"github.com/seokheejang/chain-sync-watch/internal/infrastructure/httpapi/routes"
	"github.com/seokheejang/chain-sync-watch/internal/infrastructure/persistence"
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

	addr := envOrDefault("CSW_SERVER_ADDR", ":8080")
	corsOrigins := splitNonEmpty(os.Getenv("CSW_SERVER_CORS_ORIGINS"))

	readiness := readinessProbe{db: db, redisOpt: redisOpt}

	srv := httpapi.NewServer(httpapi.Config{
		Addr:         addr,
		CORSOrigins:  corsOrigins,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		Logger:       logger,
	}, httpapi.Deps{
		Health: routes.HealthDeps{Readiness: readiness},
	})

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
