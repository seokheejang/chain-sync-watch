package queue

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/hibiken/asynq"
)

// HealthServer exposes /healthz (liveness) and /readyz (liveness +
// Redis ping via asynq.Inspector) on a small HTTP server. Kubernetes
// probes consume both endpoints in Phase 11; running locally it's
// also a convenient way to confirm the worker is able to talk to
// Redis.
type HealthServer struct {
	addr      string
	inspector *asynq.Inspector
	srv       *http.Server
	listener  net.Listener
	logger    *slog.Logger
}

// NewHealthServer returns a HealthServer bound to addr (e.g.
// ":8081") that verifies Redis reachability through the supplied
// asynq RedisConnOpt.
func NewHealthServer(addr string, opt asynq.RedisConnOpt, logger *slog.Logger) *HealthServer {
	return &HealthServer{
		addr:      addr,
		inspector: asynq.NewInspector(opt),
		logger:    logger,
	}
}

// Addr returns the bound address (useful when Start was called
// with :0 and the OS picked a port).
func (h *HealthServer) Addr() string {
	if h.listener == nil {
		return h.addr
	}
	return h.listener.Addr().String()
}

// Start binds the listener and launches the HTTP server in a
// goroutine. Returns once the listener is accepting connections.
func (h *HealthServer) Start() error {
	ln, err := net.Listen("tcp", h.addr)
	if err != nil {
		return fmt.Errorf("queue: health listen %q: %w", h.addr, err)
	}
	h.listener = ln

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", h.healthz)
	mux.HandleFunc("/readyz", h.readyz)

	h.srv = &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		if err := h.srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			if h.logger != nil {
				h.logger.Error("health server exited", "err", err)
			}
		}
	}()
	return nil
}

// Shutdown stops the HTTP server and closes the inspector.
func (h *HealthServer) Shutdown(ctx context.Context) error {
	var firstErr error
	if h.srv != nil {
		if err := h.srv.Shutdown(ctx); err != nil {
			firstErr = err
		}
	}
	if h.inspector != nil {
		if err := h.inspector.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (h *HealthServer) healthz(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

func (h *HealthServer) readyz(w http.ResponseWriter, _ *http.Request) {
	if _, err := h.inspector.Queues(); err != nil {
		http.Error(w, "redis unreachable: "+err.Error(), http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ready"))
}
