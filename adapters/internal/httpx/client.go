// Package httpx provides the shared HTTP client base used by every
// HTTP-backed source adapter (Blockscout, Routescan, Etherscan, ...).
// It centralises four cross-cutting concerns so each adapter only
// wires in its endpoint schemas:
//
//   - Rate limiting — token-bucket via golang.org/x/time/rate, waits
//     with the caller's context so cancellation is honoured during
//     budget holds.
//   - Retry with backoff — exponential ± jitter, capped at MaxDelay;
//     respects Retry-After on 429/503 responses.
//   - Request re-playability — POST bodies must expose GetBody so a
//     retry can re-read identical bytes; the client relies on that
//     hook rather than buffering bodies itself.
//   - Structured logging — every attempt emits an slog record with
//     status, duration, and attempt number so observability can
//     surface retry storms without per-adapter instrumentation.
//
// The package sits under adapters/internal/ so only packages rooted
// in adapters/ can import it — a hard guarantee that domain packages
// never reach network code by accident.
package httpx

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"golang.org/x/time/rate"
)

// Client wraps *http.Client with rate-limit, retry, and logging.
// Zero value is not usable; always construct via New.
type Client struct {
	hc      *http.Client
	limiter *rate.Limiter // nil => unlimited
	retry   RetryPolicy
	log     *slog.Logger
}

// Option configures a Client at construction time.
type Option func(*Client)

// New builds a Client with sane defaults. Override individual knobs
// via Option functions: httpx.New(httpx.WithRateLimit(5, 1)).
func New(opts ...Option) *Client {
	c := &Client{
		hc:    &http.Client{Timeout: 30 * time.Second},
		retry: DefaultRetryPolicy(),
		log:   slog.Default(),
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// WithHTTPClient swaps the underlying *http.Client. Callers use this
// to inject custom transports (proxies, test round-trippers) or a
// shared client pool.
func WithHTTPClient(hc *http.Client) Option {
	return func(c *Client) {
		if hc != nil {
			c.hc = hc
		}
	}
}

// WithTimeout sets the per-request timeout on the default HTTP
// client. Ignored when the caller supplies their own via
// WithHTTPClient (use that client's own Timeout instead).
func WithTimeout(d time.Duration) Option {
	return func(c *Client) {
		if d > 0 {
			c.hc.Timeout = d
		}
	}
}

// WithRateLimit installs a token-bucket limiter. rps is the steady-
// state rate in requests per second; burst is the bucket capacity
// (minimum 1 — a burst of 0 would block forever).
func WithRateLimit(rps float64, burst int) Option {
	return func(c *Client) {
		if rps <= 0 {
			return
		}
		if burst < 1 {
			burst = 1
		}
		c.limiter = rate.NewLimiter(rate.Limit(rps), burst)
	}
}

// WithRetry overrides the default retry policy.
func WithRetry(p RetryPolicy) Option {
	return func(c *Client) {
		if p.MaxAttempts >= 1 {
			c.retry = p
		}
	}
}

// WithLogger installs a custom slog logger. Useful in tests and when
// the adapter wants a per-source logger with request_id fields
// already attached.
func WithLogger(l *slog.Logger) Option {
	return func(c *Client) {
		if l != nil {
			c.log = l
		}
	}
}

// Do executes req with rate-limiting and retry. It respects
// ctx cancellation at every await point (limiter, backoff) and
// returns the final *http.Response or the last network error.
//
// Retry semantics (via RetryPolicy):
//   - Transport errors and retryable statuses (default: 429, 5xx)
//     trigger backoff + another attempt.
//   - 4xx (other than 429) returns immediately.
//   - Retry-After on 429/503 overrides the computed backoff when
//     it asks for a longer wait.
//   - POST requests without GetBody cannot be retried safely: the
//     first attempt runs, and retry-eligible outcomes return as-is
//     with a logged warning.
//
// The caller owns response body lifecycle — close it when done.
func (c *Client) Do(ctx context.Context, req *http.Request) (*http.Response, error) {
	if req == nil {
		return nil, errors.New("httpx: nil request")
	}

	attempts := c.retry.MaxAttempts
	if attempts < 1 {
		attempts = 1
	}

	var (
		lastResp *http.Response
		lastErr  error
	)

	for attempt := range attempts {
		// Respect ctx BEFORE waiting on the limiter so a cancelled
		// caller bails without holding tokens.
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		// Rate-limit hold. The limiter itself respects ctx.
		if c.limiter != nil {
			if err := c.limiter.Wait(ctx); err != nil {
				return nil, err
			}
		}

		// Rewind body for retries. http.NewRequest sets GetBody for
		// bodies of known types; POSTs without one are single-shot.
		// http.NoBody reads zero bytes so it's re-playable without
		// a rewind.
		if attempt > 0 && hasReplayableBody(req) && req.GetBody != nil {
			b, err := req.GetBody()
			if err != nil {
				return nil, fmt.Errorf("httpx: rewind body: %w", err)
			}
			req.Body = b
		}

		reqWithCtx := req.WithContext(ctx)
		start := time.Now()
		// The whole point of a shared HTTP client is that callers
		// control the URL. G704 (SSRF taint) fires on every generic
		// HTTP wrapper; authorisation of destinations happens at
		// the adapter layer (allow-list of bases), not here.
		resp, err := c.hc.Do(reqWithCtx) //nolint:gosec // G704: URL is adapter-supplied by design
		elapsed := time.Since(start)

		status := 0
		if resp != nil {
			status = resp.StatusCode
		}
		c.log.LogAttrs(ctx, slog.LevelDebug, "httpx attempt",
			slog.Int("attempt", attempt+1),
			slog.Int("status", status),
			slog.Duration("elapsed", elapsed),
			slog.String("method", req.Method),
			slog.String("url", req.URL.Redacted()),
			slog.Any("err", err),
		)

		// Decide whether this outcome is worth retrying.
		if !PolicyRetryable(c.retry, status, err) {
			// Non-retryable: return whatever we have. Err wins over
			// resp when both are set (net/http guarantees one is
			// nil, but be explicit).
			if err != nil {
				return nil, err
			}
			return resp, nil
		}

		// Retryable path: close the body if any (so the connection
		// can be reused) and plan the next attempt.
		if resp != nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
		}
		lastResp = resp
		lastErr = err

		// If this was the last allowed attempt, return the last
		// outcome.
		if attempt == attempts-1 {
			break
		}

		// If the caller sent a meaningful body without GetBody, we
		// can't safely replay — surface the last outcome rather than
		// making a broken second attempt. GET/HEAD with http.NoBody
		// (or with a nil body) are always replayable.
		if hasReplayableBody(req) && req.GetBody == nil {
			c.log.LogAttrs(ctx, slog.LevelWarn,
				"httpx: body-ful request has no GetBody; retry disabled",
				slog.String("url", req.URL.Redacted()),
			)
			break
		}

		// Compute delay — Retry-After wins if it's longer.
		delay := BackoffDelay(c.retry, attempt)
		if resp != nil {
			if ra := parseRetryAfter(resp.Header.Get("Retry-After")); ra > delay {
				delay = ra
			}
		}

		// Honour cancellation during the backoff hold.
		t := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			t.Stop()
			return nil, ctx.Err()
		case <-t.C:
		}
	}

	// All attempts consumed. Return the most recent outcome; err
	// takes precedence over a final retryable response because the
	// caller almost always wants to branch on err first.
	if lastErr != nil {
		return nil, fmt.Errorf("httpx: exhausted %d attempts: %w", attempts, lastErr)
	}
	return lastResp, nil
}

// hasReplayableBody reports whether the request carries a body that
// the client would need to rewind to retry. A nil body or http.NoBody
// means "no bytes to replay" — those requests are always safe to try
// again.
func hasReplayableBody(req *http.Request) bool {
	if req.Body == nil {
		return false
	}
	if req.Body == http.NoBody {
		return false
	}
	return true
}

// parseRetryAfter decodes the Retry-After header. The RFC allows
// either a delta-seconds integer or an HTTP-date; we handle the
// integer form (the common case for 429/503) and fall back to zero
// on anything else so the computed backoff is used instead.
func parseRetryAfter(h string) time.Duration {
	if h == "" {
		return 0
	}
	n, err := strconv.Atoi(h)
	if err != nil || n < 0 {
		return 0
	}
	return time.Duration(n) * time.Second
}
