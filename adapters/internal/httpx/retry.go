package httpx

import (
	"context"
	"errors"
	"math/rand/v2"
	"net/http"
	"time"
)

// RetryPolicy governs how Do retries a failed attempt.
//
// MaxAttempts counts total tries (1 means "no retry"). BaseDelay is
// the wait before the first retry; subsequent retries double until
// MaxDelay caps them. Retryable classifies a (status, err) pair as
// worth another attempt; a nil value uses DefaultRetryable.
//
// The zero value RetryPolicy{} is unusable — always construct one via
// DefaultRetryPolicy() or supply MaxAttempts ≥ 1 yourself.
type RetryPolicy struct {
	MaxAttempts int
	BaseDelay   time.Duration
	MaxDelay    time.Duration
	Retryable   func(status int, err error) bool
}

// DefaultRetryPolicy is tuned for the typical Blockscout/Routescan
// free-tier profile: a handful of attempts, starting at 200ms and
// capping at 5s. Adapters with stricter budgets can narrow
// MaxAttempts; adapters with stricter latency goals can reduce
// MaxDelay.
func DefaultRetryPolicy() RetryPolicy {
	return RetryPolicy{
		MaxAttempts: 4,
		BaseDelay:   200 * time.Millisecond,
		MaxDelay:    5 * time.Second,
		Retryable:   DefaultRetryable,
	}
}

// DefaultRetryable returns true when the outcome is worth retrying.
//
// The predicate deliberately treats context errors as non-retryable:
// if the caller cancelled or the deadline elapsed, the operation is
// over — another attempt would just leak another request.
func DefaultRetryable(status int, err error) bool {
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return false
		}
		return true
	}
	switch status {
	case http.StatusTooManyRequests,
		http.StatusInternalServerError,
		http.StatusBadGateway,
		http.StatusServiceUnavailable,
		http.StatusGatewayTimeout:
		return true
	}
	return false
}

// PolicyRetryable is a small helper that routes the decision through
// a policy's custom predicate (if any) or falls back to the default.
// Exported for test access; adapters should call through Client.Do.
func PolicyRetryable(p RetryPolicy, status int, err error) bool {
	if p.Retryable != nil {
		return p.Retryable(status, err)
	}
	return DefaultRetryable(status, err)
}

// BackoffDelay returns the wait for the given retry attempt
// (0-indexed: attempt=0 is the first retry). The progression is
// exponential with ±25% jitter, capped at MaxDelay + jitter.
//
// Exported so tests can verify the bounds without duplicating the
// formula; in production Do calls it internally.
func BackoffDelay(p RetryPolicy, attempt int) time.Duration {
	base := p.BaseDelay
	if base <= 0 {
		base = DefaultRetryPolicy().BaseDelay
	}
	maxDelay := p.MaxDelay
	if maxDelay <= 0 {
		maxDelay = DefaultRetryPolicy().MaxDelay
	}

	// Exponential: base * 2^attempt, capped.
	d := base
	for range attempt {
		d *= 2
		if d > maxDelay {
			d = maxDelay
			break
		}
	}
	if d > maxDelay {
		d = maxDelay
	}

	// Jitter: ±25%. Keeps thundering herds apart when many callers
	// retry after a shared upstream blip. math/rand/v2 is the right
	// tool here — cryptographic randomness would just burn CPU
	// without improving desynchronisation.
	jitter := float64(d) * 0.25
	// rand.Float64 returns [0, 1); shift to [-0.5, 0.5) and scale.
	offset := time.Duration((rand.Float64() - 0.5) * 2 * jitter) //nolint:gosec // G404: jitter, not cryptographic
	return d + offset
}
