package httpapi_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/seokheejang/chain-sync-watch/internal/infrastructure/httpapi"
	"github.com/seokheejang/chain-sync-watch/internal/infrastructure/httpapi/routes"
)

// --- fixtures -------------------------------------------------------

// newTestServer returns a started *httptest.Server plus a cleanup
// func. The caller picks the readiness behaviour by passing a
// fakeReadiness; nil means "no readiness probe configured".
func newTestServer(t *testing.T, readiness routes.HealthChecker) *httptest.Server {
	t.Helper()
	srv := httpapi.NewServer(httpapi.Config{}, httpapi.Deps{
		Health: routes.HealthDeps{Readiness: readiness},
	})
	// Extract the mux so httptest handles listening.
	ts := httptest.NewServer(srv.Handler)
	t.Cleanup(ts.Close)
	return ts
}

type fakeReadiness struct {
	err error
}

func (f fakeReadiness) Ready(_ context.Context) error { return f.err }

// --- tests ----------------------------------------------------------

func TestServer_Healthz_Returns200(t *testing.T) {
	ts := newTestServer(t, nil)

	resp, err := http.Get(ts.URL + "/healthz")
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode)
	var body struct {
		Status string `json:"status"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	require.Equal(t, "ok", body.Status)
}

func TestServer_Readyz_WithNilCheckerReturns200(t *testing.T) {
	ts := newTestServer(t, nil)

	resp, err := http.Get(ts.URL + "/readyz")
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestServer_Readyz_CheckerFailureReturns503(t *testing.T) {
	ts := newTestServer(t, fakeReadiness{err: errors.New("db down")})

	resp, err := http.Get(ts.URL + "/readyz")
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)
}

func TestServer_RequestIDEcho_UsesProvidedHeader(t *testing.T) {
	ts := newTestServer(t, nil)

	req, err := http.NewRequest(http.MethodGet, ts.URL+"/healthz", nil)
	require.NoError(t, err)
	req.Header.Set(httpapi.HeaderRequestID, "test-request-id-123")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, "test-request-id-123", resp.Header.Get(httpapi.HeaderRequestID))
}

func TestServer_RequestIDEcho_GeneratesWhenMissing(t *testing.T) {
	ts := newTestServer(t, nil)

	resp, err := http.Get(ts.URL + "/healthz")
	require.NoError(t, err)
	defer resp.Body.Close()

	got := resp.Header.Get(httpapi.HeaderRequestID)
	require.NotEmpty(t, got)
	require.Len(t, got, 32, "generated id is 16 hex bytes")
}

func TestServer_OpenAPIDoc_IsServed(t *testing.T) {
	ts := newTestServer(t, nil)

	resp, err := http.Get(ts.URL + "/openapi.json")
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var doc map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&doc))
	require.Equal(t, "3.1.0", doc["openapi"])
	info, ok := doc["info"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "chain-sync-watch", info["title"])
}

func TestServer_UnknownRoute_Returns404(t *testing.T) {
	ts := newTestServer(t, nil)

	resp, err := http.Get(ts.URL + "/nope")
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusNotFound, resp.StatusCode)
}
