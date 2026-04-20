package httpx_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/seokheejang/chain-sync-watch/adapters/internal/httpx"
)

// Helper — builds a GET request at srv.URL+path.
func newGET(t *testing.T, srvURL, path string) *http.Request {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, srvURL+path, http.NoBody)
	require.NoError(t, err)
	return req
}

// Drains and closes the response body; tests assert on the status
// code, not the body, so we treat read errors as test failures.
func drain(t *testing.T, resp *http.Response) {
	t.Helper()
	if resp == nil || resp.Body == nil {
		return
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	require.NoError(t, resp.Body.Close())
}

// A happy-path GET must return immediately with the upstream response
// and no retry. Nothing to prove about the logic except that the
// wrapper doesn't pessimise the common case.
func TestClient_Success200_NoRetry(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := httpx.New(httpx.WithRetry(httpx.RetryPolicy{MaxAttempts: 3}))
	resp, err := c.Do(context.Background(), newGET(t, srv.URL, "/"))
	require.NoError(t, err)
	defer drain(t, resp)

	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, int32(1), atomic.LoadInt32(&calls))
}

// A transient 500 followed by success must result in one retry and a
// 200 response returned to the caller. This is the core "retry
// actually works" test; everything else verifies edges around it.
func TestClient_RetryableThenSuccess(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		if n < 2 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := httpx.New(httpx.WithRetry(httpx.RetryPolicy{
		MaxAttempts: 3,
		BaseDelay:   10 * time.Millisecond,
		MaxDelay:    50 * time.Millisecond,
	}))
	resp, err := c.Do(context.Background(), newGET(t, srv.URL, "/"))
	require.NoError(t, err)
	defer drain(t, resp)

	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, int32(2), atomic.LoadInt32(&calls))
}

// 400 Bad Request is permanent — the wrapper must surface it after
// the first attempt. Retrying a bad request only amplifies noise on
// the upstream.
func TestClient_NoRetryOn400(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer srv.Close()

	c := httpx.New(httpx.WithRetry(httpx.RetryPolicy{
		MaxAttempts: 5,
		BaseDelay:   10 * time.Millisecond,
	}))
	resp, err := c.Do(context.Background(), newGET(t, srv.URL, "/"))
	require.NoError(t, err)
	defer drain(t, resp)

	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
	require.Equal(t, int32(1), atomic.LoadInt32(&calls))
}

// Exhausting MaxAttempts on persistent 500s returns the last response
// (not an error): callers can inspect the status and body if they
// need to surface diagnostic details. Retry is best-effort, not a
// guarantee of success.
func TestClient_MaxAttemptsExhausted(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := httpx.New(httpx.WithRetry(httpx.RetryPolicy{
		MaxAttempts: 3,
		BaseDelay:   5 * time.Millisecond,
		MaxDelay:    20 * time.Millisecond,
	}))
	resp, err := c.Do(context.Background(), newGET(t, srv.URL, "/"))
	require.NoError(t, err)
	defer drain(t, resp)

	require.Equal(t, http.StatusInternalServerError, resp.StatusCode)
	require.Equal(t, int32(3), atomic.LoadInt32(&calls))
}

// A context cancelled mid-backoff must short-circuit: Do returns
// ctx.Err() without issuing another request. This matters for run
// cancellation — we can't have in-flight retries outlive the run
// they belong to.
func TestClient_ContextCancelDuringBackoff(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := httpx.New(httpx.WithRetry(httpx.RetryPolicy{
		MaxAttempts: 10,
		BaseDelay:   100 * time.Millisecond,
		MaxDelay:    1 * time.Second,
	}))

	ctx, cancel := context.WithCancel(context.Background())
	// Cancel shortly after the first attempt so the backoff sleep
	// should be interrupted.
	go func() {
		time.Sleep(30 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	resp, err := c.Do(ctx, newGET(t, srv.URL, "/"))
	elapsed := time.Since(start)
	drain(t, resp) // no-op when nil, which is the expected case here

	require.Error(t, err)
	require.True(t, errors.Is(err, context.Canceled))
	// We must bail well before the 10-attempt budget would have
	// taken.
	require.Less(t, elapsed, 500*time.Millisecond)
	require.Less(t, atomic.LoadInt32(&calls), int32(10))
}

// Rate limiting: rps=10 (one token per 100ms). Ten back-to-back
// requests cannot complete in under ~900ms because the limiter has to
// refill 9 times after the initial burst=1 token.
func TestClient_RateLimitWaitsAcrossCalls(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := httpx.New(httpx.WithRateLimit(10, 1)) // 10 rps, burst 1
	start := time.Now()
	for range 10 {
		resp, err := c.Do(context.Background(), newGET(t, srv.URL, "/"))
		require.NoError(t, err)
		drain(t, resp)
	}
	elapsed := time.Since(start)
	// Nine waits of 100ms each = 900ms minimum; allow 750ms for
	// scheduler wiggle on busy CI.
	require.GreaterOrEqualf(t, elapsed, 750*time.Millisecond,
		"rate limiter did not introduce the expected delay; got %v", elapsed)
}

// 429 with Retry-After (seconds) must be honoured — the client should
// sleep at least the indicated duration before retrying. Upstreams
// issue Retry-After for a reason and ignoring it is the fast path to
// a permanent ban.
func TestClient_429RetryAfterHonored(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		if n == 1 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := httpx.New(httpx.WithRetry(httpx.RetryPolicy{
		MaxAttempts: 3,
		BaseDelay:   1 * time.Millisecond, // overridden by Retry-After
		MaxDelay:    5 * time.Second,
	}))
	start := time.Now()
	resp, err := c.Do(context.Background(), newGET(t, srv.URL, "/"))
	elapsed := time.Since(start)
	require.NoError(t, err)
	defer drain(t, resp)

	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.GreaterOrEqualf(t, elapsed, 900*time.Millisecond,
		"Retry-After=1s not honoured; elapsed %v", elapsed)
}

// A transport-layer error (server closed connection, DNS failure,
// etc.) must be retried just like a 5xx. Here we simulate it by
// pointing at a URL whose server shuts down between attempts.
func TestClient_NetworkError_Retried(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		if n == 1 {
			// Hijack the connection and slam it closed so the client
			// sees a transport error instead of a status code.
			hj, ok := w.(http.Hijacker)
			if !ok {
				t.Fatalf("hijacker unavailable")
			}
			conn, _, err := hj.Hijack()
			require.NoError(t, err)
			_ = conn.Close()
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := httpx.New(httpx.WithRetry(httpx.RetryPolicy{
		MaxAttempts: 3,
		BaseDelay:   10 * time.Millisecond,
		MaxDelay:    50 * time.Millisecond,
	}))
	resp, err := c.Do(context.Background(), newGET(t, srv.URL, "/"))
	require.NoError(t, err)
	defer drain(t, resp)

	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, int32(2), atomic.LoadInt32(&calls))
}

// POST requests with a body must retry correctly, which means the
// body has to be re-readable. http.NewRequest sets GetBody for
// body-ful requests when the body is a known type (strings.Reader
// qualifies); our client relies on that hook.
func TestClient_POSTBodyRetry_ReReads(t *testing.T) {
	var calls int32
	var received []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		body, _ := io.ReadAll(r.Body)
		received = append(received, string(body))
		if n == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	req, err := http.NewRequest(http.MethodPost, srv.URL+"/", strings.NewReader(`{"key":"value"}`))
	require.NoError(t, err)

	c := httpx.New(httpx.WithRetry(httpx.RetryPolicy{
		MaxAttempts: 2,
		BaseDelay:   5 * time.Millisecond,
	}))
	resp, err := c.Do(context.Background(), req)
	require.NoError(t, err)
	defer drain(t, resp)

	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, int32(2), atomic.LoadInt32(&calls))
	require.Equal(t, []string{`{"key":"value"}`, `{"key":"value"}`}, received,
		"POST body must replay identically on retry")
}

// A nil Retryable predicate at client construction time must still
// treat retryable statuses as retryable via DefaultRetryable — the
// policy's "no predicate means use defaults" behaviour has to reach
// the Do path, not just the unit helpers.
func TestClient_NilPredicate_FallsBackToDefault(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		if n < 2 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := httpx.New(httpx.WithRetry(httpx.RetryPolicy{
		MaxAttempts: 3,
		BaseDelay:   5 * time.Millisecond,
		// Retryable intentionally nil.
	}))
	resp, err := c.Do(context.Background(), newGET(t, srv.URL, "/"))
	require.NoError(t, err)
	defer drain(t, resp)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, int32(2), atomic.LoadInt32(&calls))
}

// A non-retryable custom predicate must short-circuit retry even for
// statuses the defaults would retry. The predicate is the adapter's
// chance to encode upstream-specific semantics (e.g. Etherscan's
// "NOTOK" JSON body returned with HTTP 200).
func TestClient_CustomPredicate_DisablesRetry(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	never := func(status int, err error) bool { return false }
	c := httpx.New(httpx.WithRetry(httpx.RetryPolicy{
		MaxAttempts: 5,
		BaseDelay:   5 * time.Millisecond,
		Retryable:   never,
	}))
	resp, err := c.Do(context.Background(), newGET(t, srv.URL, "/"))
	require.NoError(t, err)
	defer drain(t, resp)
	require.Equal(t, http.StatusInternalServerError, resp.StatusCode)
	require.Equal(t, int32(1), atomic.LoadInt32(&calls))
}

// Guard that failure after exhaustion from a transport error surfaces
// the last error to the caller (no retry hides the root cause).
func TestClient_NetworkError_ExhaustedReturnsError(t *testing.T) {
	// Point to a port that's guaranteed unused on the test host.
	c := httpx.New(httpx.WithRetry(httpx.RetryPolicy{
		MaxAttempts: 2,
		BaseDelay:   1 * time.Millisecond,
	}))
	req, err := http.NewRequest(http.MethodGet, "http://127.0.0.1:1/never", http.NoBody)
	require.NoError(t, err)
	resp, err := c.Do(context.Background(), req)
	drain(t, resp) // nil-safe; resp is expected nil here
	require.Error(t, err)
	// Must contain a meaningful hint — not just wrap context.
	require.NotContains(t, fmt.Sprintf("%v", err), "context")
}
