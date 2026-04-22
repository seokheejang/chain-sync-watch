package queue_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/require"

	"github.com/seokheejang/chain-sync-watch/internal/infrastructure/queue"
)

func newJSONLogger(buf *bytes.Buffer) *slog.Logger {
	return slog.New(slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

func TestLoggingMiddleware_SuccessLogsInfo(t *testing.T) {
	var buf bytes.Buffer
	logger := newJSONLogger(&buf)

	handler := asynq.HandlerFunc(func(_ context.Context, _ *asynq.Task) error {
		return nil
	})
	wrapped := queue.LoggingMiddleware(logger)(handler)

	task := asynq.NewTask("verification:execute_run", []byte(`{}`))
	require.NoError(t, wrapped.ProcessTask(context.Background(), task))

	require.True(t, strings.Contains(buf.String(), "asynq task ok"))
	var entry map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &entry))
	require.Equal(t, "INFO", entry["level"])
	require.Equal(t, "verification:execute_run", entry["task_type"])
	_, ok := entry["duration_ms"]
	require.True(t, ok, "duration_ms key must be present")
}

func TestLoggingMiddleware_ErrorLogsWarn(t *testing.T) {
	var buf bytes.Buffer
	logger := newJSONLogger(&buf)

	handler := asynq.HandlerFunc(func(_ context.Context, _ *asynq.Task) error {
		return errors.New("boom")
	})
	wrapped := queue.LoggingMiddleware(logger)(handler)

	task := asynq.NewTask("verification:scheduled_run", []byte(`{}`))
	err := wrapped.ProcessTask(context.Background(), task)
	require.Error(t, err)

	var entry map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &entry))
	require.Equal(t, "WARN", entry["level"])
	require.Equal(t, "asynq task error", entry["msg"])
	require.Equal(t, "verification:scheduled_run", entry["task_type"])
	require.Equal(t, "boom", entry["err"])
}

func TestLoggingMiddleware_NilLoggerIsPassThrough(t *testing.T) {
	called := false
	handler := asynq.HandlerFunc(func(_ context.Context, _ *asynq.Task) error {
		called = true
		return nil
	})
	wrapped := queue.LoggingMiddleware(nil)(handler)

	task := asynq.NewTask("verification:execute_run", []byte(`{}`))
	require.NoError(t, wrapped.ProcessTask(context.Background(), task))
	require.True(t, called, "inner handler must still run with nil logger")
}
