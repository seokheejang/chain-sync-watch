package queue_test

import (
	"context"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/seokheejang/chain-sync-watch/internal/infrastructure/queue"
)

func TestHealthServer_HealthzAlwaysOK(t *testing.T) {
	opt, _ := startMiniRedis(t)
	hs := queue.NewHealthServer(":0", opt, nil)
	require.NoError(t, hs.Start())
	t.Cleanup(func() {
		_ = hs.Shutdown(context.Background())
	})

	resp, err := http.Get("http://" + hs.Addr() + "/healthz")
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	body, _ := io.ReadAll(resp.Body)
	require.Equal(t, "ok", string(body))
}

func TestHealthServer_ReadyzWhenRedisUp(t *testing.T) {
	opt, _ := startMiniRedis(t)
	hs := queue.NewHealthServer(":0", opt, nil)
	require.NoError(t, hs.Start())
	t.Cleanup(func() {
		_ = hs.Shutdown(context.Background())
	})

	resp, err := http.Get("http://" + hs.Addr() + "/readyz")
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestHealthServer_ReadyzWhenRedisDown(t *testing.T) {
	opt, mr := startMiniRedis(t)
	hs := queue.NewHealthServer(":0", opt, nil)
	require.NoError(t, hs.Start())
	t.Cleanup(func() {
		_ = hs.Shutdown(context.Background())
	})

	mr.Close()

	// Give asynq a moment to see the connection drop.
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get("http://" + hs.Addr() + "/readyz")
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)
}
