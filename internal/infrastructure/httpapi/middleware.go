package httpapi

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/seokheejang/chain-sync-watch/internal/observability"
)

// HeaderRequestID is the wire name used for incoming and outgoing
// request correlation. Standard is "X-Request-ID" — kept exported so
// clients and tests can set or assert it without string literals.
const HeaderRequestID = "X-Request-ID"

// requestIDMiddleware reads the incoming X-Request-ID header (generates
// a fresh id if missing), stashes it on the request context via
// observability.WithRequestID, and mirrors it back on the response so
// clients can correlate on the wire. Generated ids are 16 random
// bytes hex-encoded — enough entropy for ops without dragging in a
// UUID dependency.
func requestIDMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := r.Header.Get(HeaderRequestID)
			if id == "" {
				id = newRequestID()
			}
			w.Header().Set(HeaderRequestID, id)
			ctx := observability.WithRequestID(r.Context(), id)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func newRequestID() string {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		// Collision-prone fallback — still better than crashing. The
		// only realistic path to this branch is entropy exhaustion,
		// which we've never seen in practice.
		return fmt.Sprintf("req-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(buf[:])
}

// loggingMiddleware emits one slog entry per request with method,
// path, status, duration, and the correlation fields stashed on the
// context. The response status is captured by wrapping the writer;
// we deliberately do not log request bodies (noise + PII risk).
// Nil logger is a pass-through.
func loggingMiddleware(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if logger == nil {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
			start := time.Now()
			next.ServeHTTP(rec, r)
			dur := time.Since(start)
			logger := observability.LoggerFromContext(r.Context(), logger)
			logger.LogAttrs(r.Context(), slog.LevelInfo, "http request",
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.Int("status", rec.status),
				slog.Int64("duration_ms", dur.Milliseconds()),
			)
		})
	}
}

// statusRecorder captures the final status written by the handler so
// the logging middleware can record it. http.ResponseWriter does not
// expose the status, hence this wrapper.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(status int) {
	s.status = status
	s.ResponseWriter.WriteHeader(status)
}

// recoverMiddleware catches panics in downstream handlers, logs them
// (including a stack trace) and returns a 500 response. Without this
// a panic in one handler would kill the server.
func recoverMiddleware(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					if logger != nil {
						logger.ErrorContext(r.Context(), "http panic",
							slog.Any("panic", rec),
							slog.String("stack", string(debug.Stack())),
						)
					}
					http.Error(w, "internal server error", http.StatusInternalServerError)
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}
