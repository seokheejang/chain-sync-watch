package httpx_test

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/seokheejang/chain-sync-watch/adapters/internal/httpx"
)

// DefaultRetryable is the predicate every adapter inherits unless it
// overrides. The rules encode "only retry what a second attempt could
// plausibly fix": transient server errors, rate limits, and
// network-layer failures. Client errors (4xx other than 429) are
// permanent and must not be retried.
func TestDefaultRetryable(t *testing.T) {
	cases := []struct {
		name   string
		status int
		err    error
		want   bool
	}{
		// Network-layer errors → retry.
		{"net error wrapped", 0, &net.OpError{Op: "dial"}, true},
		{"eof after connect", 0, errors.New("unexpected EOF"), true},

		// Context-tied errors → NOT retryable (caller asked to stop).
		{"context canceled", 0, context.Canceled, false},
		{"context deadline", 0, context.DeadlineExceeded, false},

		// Transient statuses → retry.
		{"429 too many requests", 429, nil, true},
		{"500 server error", 500, nil, true},
		{"502 bad gateway", 502, nil, true},
		{"503 unavailable", 503, nil, true},
		{"504 gateway timeout", 504, nil, true},

		// Success → no retry.
		{"200 ok", 200, nil, false},

		// Permanent client errors → no retry.
		{"400 bad request", 400, nil, false},
		{"401 unauthorized", 401, nil, false},
		{"403 forbidden", 403, nil, false},
		{"404 not found", 404, nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := httpx.DefaultRetryable(tc.status, tc.err)
			require.Equalf(t, tc.want, got,
				"status=%d err=%v: want %v, got %v", tc.status, tc.err, tc.want, got)
		})
	}
}

// Backoff is exponential with a MaxDelay cap. Verify the progression
// without jitter (tests assert bounds rather than exact values since
// production code includes ±25% jitter).
func TestBackoff_ExponentialProgression(t *testing.T) {
	p := httpx.RetryPolicy{
		MaxAttempts: 5,
		BaseDelay:   100 * time.Millisecond,
		MaxDelay:    5 * time.Second,
	}

	// Attempt 0 (before first retry) should request BaseDelay-ish.
	// Attempt 1 should be ~2x, attempt 2 ~4x, etc., capped at MaxDelay.
	expectedMid := []time.Duration{
		100 * time.Millisecond,
		200 * time.Millisecond,
		400 * time.Millisecond,
		800 * time.Millisecond,
		1600 * time.Millisecond,
	}

	for i, mid := range expectedMid {
		got := httpx.BackoffDelay(p, i)
		lo := time.Duration(float64(mid) * 0.75)
		hi := time.Duration(float64(mid) * 1.25)
		require.GreaterOrEqualf(t, got, lo, "attempt %d: want ≥ %v, got %v", i, lo, got)
		require.LessOrEqualf(t, got, hi, "attempt %d: want ≤ %v, got %v", i, hi, got)
	}
}

// Delay must never exceed MaxDelay, even for attempts that would
// exponentially overflow. The cap is what protects long-running
// verification jobs from minute-long sleeps.
func TestBackoff_CapsAtMaxDelay(t *testing.T) {
	p := httpx.RetryPolicy{
		MaxAttempts: 20,
		BaseDelay:   100 * time.Millisecond,
		MaxDelay:    2 * time.Second,
	}
	for i := 5; i < 20; i++ {
		got := httpx.BackoffDelay(p, i)
		require.LessOrEqualf(t, got, p.MaxDelay+time.Duration(float64(p.MaxDelay)*0.25),
			"attempt %d exceeded MaxDelay + jitter: got %v", i, got)
	}
}

// A nil Retryable predicate must mean "use the default" — callers
// assembling a policy without naming the predicate should not
// accidentally disable retry entirely.
func TestRetryPolicy_NilPredicateUsesDefault(t *testing.T) {
	p := httpx.RetryPolicy{MaxAttempts: 3}
	// With no predicate, 500 should still be considered retryable.
	require.True(t, httpx.PolicyRetryable(p, 500, nil))
	require.False(t, httpx.PolicyRetryable(p, 200, nil))
}
