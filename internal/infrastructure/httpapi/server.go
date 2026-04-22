package httpapi

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/cors"

	"github.com/seokheejang/chain-sync-watch/internal/infrastructure/httpapi/routes"
)

// Config tunes the HTTP server's wire behaviour. Fields correspond
// to ServerConfig in internal/config; cmd/csw-server/main.go is the
// one place both structures meet.
type Config struct {
	// Addr is the TCP listener address, e.g. ":8080".
	Addr string
	// ReadTimeout bounds the full request (header + body). Zero uses
	// http.Server's zero-value (no timeout — fine for local dev, but
	// real deployments should override).
	ReadTimeout time.Duration
	// WriteTimeout bounds the response writing phase.
	WriteTimeout time.Duration
	// CORSOrigins is the list of allowed Origin values. Empty means
	// "disable CORS middleware entirely" — production deployments
	// that front the API with an edge proxy may prefer this.
	CORSOrigins []string
	// Logger is used by the logging and recovery middleware. nil is
	// a pass-through (silent).
	Logger *slog.Logger
}

// Deps bundles the route-registration dependencies. Callers populate
// only the fields they want to expose — leaving a set nil simply
// omits that resource's routes. Phase 8.1 shipped Health; Phase 8.2
// adds Runs. Diffs / Schedules / Sources land in 8.3–8.5.
type Deps struct {
	Health routes.HealthDeps
	Runs   routes.RunsDeps
	Diffs  routes.DiffsDeps
}

// NewServer constructs a ready-to-Serve *http.Server. The server
// mounts:
//
//   - chi-level middleware: request id, logging, recovery, optional CORS
//   - huma API at the root with OpenAPI 3.1 metadata ("/openapi.json"
//     and "/docs" served automatically by huma)
//   - the resource routes supplied by `deps`.
//
// The returned handler is a chi.Mux so callers can mount additional
// non-huma endpoints (Prometheus, pprof) on the same listener if
// they want.
func NewServer(cfg Config, deps Deps) *http.Server {
	r := chi.NewRouter()

	r.Use(requestIDMiddleware())
	r.Use(recoverMiddleware(cfg.Logger))
	r.Use(loggingMiddleware(cfg.Logger))
	if len(cfg.CORSOrigins) > 0 {
		r.Use(cors.Handler(cors.Options{
			AllowedOrigins:   cfg.CORSOrigins,
			AllowedMethods:   []string{"GET", "POST", "DELETE", "OPTIONS"},
			AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", HeaderRequestID},
			ExposedHeaders:   []string{HeaderRequestID},
			AllowCredentials: false,
			MaxAge:           300,
		}))
	}

	humaCfg := huma.DefaultConfig("chain-sync-watch", "v1")
	humaCfg.Info.Description = "Cross-source chain indexer verification API. Runs, discrepancies, schedules, sources."
	api := humachi.New(r, humaCfg)

	routes.RegisterHealth(api, deps.Health)
	routes.RegisterRuns(api, deps.Runs)
	routes.RegisterDiffs(api, deps.Diffs)

	return &http.Server{
		Addr:              cfg.Addr,
		Handler:           r,
		ReadTimeout:       cfg.ReadTimeout,
		ReadHeaderTimeout: 5 * time.Second,
		WriteTimeout:      cfg.WriteTimeout,
	}
}
