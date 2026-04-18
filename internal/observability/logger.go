// Package observability provides structured logging and (future) tracing
// primitives shared across server, worker, and CLI binaries.
package observability

import (
	"context"
	"io"
	"log/slog"
	"os"
	"strings"
)

// Format selects the slog handler backing the returned logger.
type Format string

const (
	FormatJSON Format = "json"
	FormatText Format = "text"
)

// Level is a string alias matching the YAML/env config values.
type Level string

const (
	LevelDebug Level = "debug"
	LevelInfo  Level = "info"
	LevelWarn  Level = "warn"
	LevelError Level = "error"
)

// LoggerOptions configures NewLogger. Zero value yields a sensible default
// (info/json on stderr).
type LoggerOptions struct {
	Level  Level
	Format Format
	// Writer is the destination. Nil falls back to os.Stderr, keeping
	// stdout free for command output (e.g., csw openapi-dump).
	Writer io.Writer
	// AddSource attaches file:line to records. Off by default — useful
	// in tests but adds runtime cost in hot paths.
	AddSource bool
}

// NewLogger returns a slog.Logger wired from LoggerOptions.
func NewLogger(opts LoggerOptions) *slog.Logger {
	w := opts.Writer
	if w == nil {
		w = os.Stderr
	}

	handlerOpts := &slog.HandlerOptions{
		Level:     parseLevel(opts.Level),
		AddSource: opts.AddSource,
	}

	var handler slog.Handler
	switch normalizeFormat(opts.Format) {
	case FormatText:
		handler = slog.NewTextHandler(w, handlerOpts)
	default:
		handler = slog.NewJSONHandler(w, handlerOpts)
	}
	return slog.New(handler)
}

// parseLevel maps Level strings to slog.Level. Unknown input falls back
// to LevelInfo — we never want logging misconfiguration to silence logs.
func parseLevel(l Level) slog.Level {
	switch Level(strings.ToLower(string(l))) {
	case LevelDebug:
		return slog.LevelDebug
	case LevelWarn:
		return slog.LevelWarn
	case LevelError:
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

func normalizeFormat(f Format) Format {
	switch Format(strings.ToLower(string(f))) {
	case FormatText:
		return FormatText
	default:
		return FormatJSON
	}
}

// --- Request-scoped context helpers --------------------------------------

// Context carries correlation identifiers that every log record in a
// given request should inherit. We keep the set small: anything larger
// should be added as explicit attrs at the call site.
type ctxKey struct{}

type ctxFields struct {
	requestID string
	runID     string
	sourceID  string
}

// WithRequestID attaches a request id to ctx; subsequent calls to
// LoggerFromContext pick it up automatically.
func WithRequestID(ctx context.Context, id string) context.Context {
	f := fieldsFromCtx(ctx)
	f.requestID = id
	return context.WithValue(ctx, ctxKey{}, f)
}

// WithRunID attaches a verification run id.
func WithRunID(ctx context.Context, id string) context.Context {
	f := fieldsFromCtx(ctx)
	f.runID = id
	return context.WithValue(ctx, ctxKey{}, f)
}

// WithSourceID attaches a source adapter id.
func WithSourceID(ctx context.Context, id string) context.Context {
	f := fieldsFromCtx(ctx)
	f.sourceID = id
	return context.WithValue(ctx, ctxKey{}, f)
}

func fieldsFromCtx(ctx context.Context) ctxFields {
	if ctx == nil {
		return ctxFields{}
	}
	if v, ok := ctx.Value(ctxKey{}).(ctxFields); ok {
		return v
	}
	return ctxFields{}
}

// LoggerFromContext returns base decorated with any correlation fields
// stashed via the With* helpers. Safe to call with a nil context.
func LoggerFromContext(ctx context.Context, base *slog.Logger) *slog.Logger {
	if base == nil {
		base = slog.Default()
	}
	f := fieldsFromCtx(ctx)
	var attrs []slog.Attr
	if f.requestID != "" {
		attrs = append(attrs, slog.String("request_id", f.requestID))
	}
	if f.runID != "" {
		attrs = append(attrs, slog.String("run_id", f.runID))
	}
	if f.sourceID != "" {
		attrs = append(attrs, slog.String("source_id", f.sourceID))
	}
	if len(attrs) == 0 {
		return base
	}
	return slog.New(newAttrHandler(base.Handler(), attrs))
}

// ErrAttr is a small convenience for the common `slog.Any("error", err)`
// pattern; returns a no-op attr when err is nil so call sites stay flat.
func ErrAttr(err error) slog.Attr {
	if err == nil {
		return slog.Attr{}
	}
	return slog.String("error", err.Error())
}

// --- attrHandler: wrap a handler with a fixed attr set ------------------

// attrHandler prepends a static attr slice to every record. Using this
// instead of slog.Logger.With keeps the attrs on a fresh handler without
// allocating a new Logger per LoggerFromContext call.
type attrHandler struct {
	inner slog.Handler
	attrs []slog.Attr
}

func newAttrHandler(inner slog.Handler, attrs []slog.Attr) slog.Handler {
	return &attrHandler{inner: inner, attrs: attrs}
}

func (h *attrHandler) Enabled(ctx context.Context, lvl slog.Level) bool {
	return h.inner.Enabled(ctx, lvl)
}

func (h *attrHandler) Handle(ctx context.Context, r slog.Record) error {
	r.AddAttrs(h.attrs...)
	return h.inner.Handle(ctx, r)
}

func (h *attrHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	merged := make([]slog.Attr, 0, len(h.attrs)+len(attrs))
	merged = append(merged, h.attrs...)
	merged = append(merged, attrs...)
	return &attrHandler{inner: h.inner.WithAttrs(attrs), attrs: merged}
}

func (h *attrHandler) WithGroup(name string) slog.Handler {
	return &attrHandler{inner: h.inner.WithGroup(name), attrs: h.attrs}
}

// compile-time interface check
var _ slog.Handler = (*attrHandler)(nil)
