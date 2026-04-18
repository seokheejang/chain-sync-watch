package observability_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/seokheejang/chain-sync-watch/internal/observability"
)

// decodeLines parses a JSON-handler buffer into one record per line.
func decodeLines(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if line == "" {
			continue
		}
		var m map[string]any
		require.NoErrorf(t, json.Unmarshal([]byte(line), &m), "line: %s", line)
		out = append(out, m)
	}
	return out
}

func TestNewLogger_JSONDefault(t *testing.T) {
	buf := &bytes.Buffer{}
	log := observability.NewLogger(observability.LoggerOptions{Writer: buf})

	log.Info("hello", "who", "world")

	records := decodeLines(t, buf)
	require.Len(t, records, 1)
	require.Equal(t, "hello", records[0]["msg"])
	require.Equal(t, "world", records[0]["who"])
	require.Equal(t, "INFO", records[0]["level"])
}

func TestNewLogger_LevelFiltering(t *testing.T) {
	buf := &bytes.Buffer{}
	log := observability.NewLogger(observability.LoggerOptions{
		Level:  observability.LevelWarn,
		Writer: buf,
	})

	log.Debug("d")
	log.Info("i")
	log.Warn("w")
	log.Error("e")

	records := decodeLines(t, buf)
	require.Len(t, records, 2, "only warn and error should pass")
	require.Equal(t, "w", records[0]["msg"])
	require.Equal(t, "e", records[1]["msg"])
}

func TestNewLogger_TextFormat(t *testing.T) {
	buf := &bytes.Buffer{}
	log := observability.NewLogger(observability.LoggerOptions{
		Format: observability.FormatText,
		Writer: buf,
	})

	log.Info("hi")

	out := buf.String()
	require.Contains(t, out, "level=INFO")
	require.Contains(t, out, `msg=hi`)
	require.NotContains(t, out, "{", "text handler should not emit JSON braces")
}

func TestNewLogger_UnknownLevelDefaultsToInfo(t *testing.T) {
	buf := &bytes.Buffer{}
	log := observability.NewLogger(observability.LoggerOptions{
		Level:  "bogus",
		Writer: buf,
	})

	log.Debug("skipped")
	log.Info("kept")

	records := decodeLines(t, buf)
	require.Len(t, records, 1)
	require.Equal(t, "kept", records[0]["msg"])
}

func TestLoggerFromContext_AttachesIDs(t *testing.T) {
	buf := &bytes.Buffer{}
	base := observability.NewLogger(observability.LoggerOptions{Writer: buf})

	ctx := context.Background()
	ctx = observability.WithRequestID(ctx, "req-123")
	ctx = observability.WithRunID(ctx, "run-abc")
	ctx = observability.WithSourceID(ctx, "rpc")

	log := observability.LoggerFromContext(ctx, base)
	log.Info("work done")

	records := decodeLines(t, buf)
	require.Len(t, records, 1)
	require.Equal(t, "req-123", records[0]["request_id"])
	require.Equal(t, "run-abc", records[0]["run_id"])
	require.Equal(t, "rpc", records[0]["source_id"])
}

func TestLoggerFromContext_NoAttrsWhenEmpty(t *testing.T) {
	buf := &bytes.Buffer{}
	base := observability.NewLogger(observability.LoggerOptions{Writer: buf})

	log := observability.LoggerFromContext(context.Background(), base)
	log.Info("bare")

	records := decodeLines(t, buf)
	require.Len(t, records, 1)
	_, hasReq := records[0]["request_id"]
	require.False(t, hasReq)
}

func TestLoggerFromContext_NilSafety(t *testing.T) {
	// nil context must not panic; nil base should fall back to slog.Default.
	log := observability.LoggerFromContext(nil, nil)
	require.NotNil(t, log)
	log.Info("should-not-panic")
}

func TestErrAttr(t *testing.T) {
	// Non-nil error becomes a string attr.
	a := observability.ErrAttr(errors.New("boom"))
	require.Equal(t, "error", a.Key)
	require.Equal(t, "boom", a.Value.String())

	// Nil error returns the zero Attr (key empty) so slog skips it.
	zero := observability.ErrAttr(nil)
	require.Equal(t, slog.Attr{}, zero)
}
