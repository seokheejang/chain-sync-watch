package httpprobe

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	probeapp "github.com/seokheejang/chain-sync-watch/internal/application/probe"
	"github.com/seokheejang/chain-sync-watch/internal/probe"
)

// Prober runs HTTP probe calls. It implements probeapp.HTTPProber.
//
// Construction is via New so the zero value remains unusable — that
// keeps the contract clear (operators must supply at least an
// http.Client they trust) while letting tests inject a custom
// transport.
type Prober struct {
	hc *http.Client
}

// Option configures a Prober at construction time.
type Option func(*Prober)

// New builds a Prober. The default http.Client uses a short overall
// timeout (10s) but the per-call ceiling is whatever
// probeapp.RunProbe passes via Probe(); the http.Client's Timeout
// is a safety net for the unusual case where the caller forgets to
// supply one.
func New(opts ...Option) *Prober {
	p := &Prober{
		hc: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

// WithHTTPClient swaps the underlying *http.Client. Tests use this to
// inject httptest.Server's client; production callers can install
// custom transports (proxies, response-size limits) before
// constructing the Prober.
func WithHTTPClient(hc *http.Client) Option {
	return func(p *Prober) {
		if hc != nil {
			p.hc = hc
		}
	}
}

// Probe implements probeapp.HTTPProber.
//
// The flow is intentionally narrow:
//
//  1. Build a context with the supplied timeout.
//  2. Construct the request from the target. Body is buffered so
//     retries / replays would work, but we don't retry — see package
//     docs for why.
//  3. Issue the request. Measure elapsed time around the Do call;
//     reading the response body's headers is part of the measurement,
//     since indexers commonly stream large payloads and "first byte"
//     latency under-counts what an operator actually feels.
//  4. Classify the outcome and return.
func (p *Prober) Probe(
	ctx context.Context,
	target probe.ProbeTarget,
	timeout time.Duration,
) probeapp.ProbeResult {
	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := buildRequest(callCtx, target)
	if err != nil {
		// Building the request can only fail on a malformed URL or
		// method (the domain Validate() should have caught it). Treat
		// it as a network-class failure so the operator sees something
		// in the dashboard rather than a silent skip.
		return probeapp.ProbeResult{
			ErrorClass: probe.ErrorNetwork,
			ErrorMsg:   truncate("build request: " + err.Error()),
		}
	}

	start := time.Now()
	resp, err := p.hc.Do(req)
	elapsed := time.Since(start)

	if err != nil {
		return probeapp.ProbeResult{
			ElapsedMS:  elapsed.Milliseconds(),
			ErrorClass: classifyTransportError(callCtx, err),
			ErrorMsg:   truncate(err.Error()),
		}
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()

	return classifyResponse(resp, elapsed)
}

// buildRequest constructs the *http.Request from a ProbeTarget. The
// body is buffered so http.NewRequestWithContext can populate
// GetBody — handy if a future option wires retries in via httpx.
func buildRequest(ctx context.Context, target probe.ProbeTarget) (*http.Request, error) {
	method := target.Method
	if method == "" {
		method = http.MethodGet
	}

	var body io.Reader
	if len(target.Body) > 0 {
		body = bytes.NewReader(target.Body)
	}

	req, err := http.NewRequestWithContext(ctx, method, target.URL, body)
	if err != nil {
		return nil, err
	}
	for k, v := range target.Headers {
		req.Header.Set(k, v)
	}
	return req, nil
}

// classifyResponse turns a successful round-trip into a ProbeResult.
// A 2xx/3xx response counts as ErrorNone — the probe doesn't try to
// parse the body for application-level errors at this stage (that's
// reserved for protocol-aware probers in Phase 12B).
func classifyResponse(resp *http.Response, elapsed time.Duration) probeapp.ProbeResult {
	out := probeapp.ProbeResult{
		ElapsedMS:  elapsed.Milliseconds(),
		StatusCode: resp.StatusCode,
	}
	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 400:
		out.ErrorClass = probe.ErrorNone
	case resp.StatusCode >= 400 && resp.StatusCode < 500:
		out.ErrorClass = probe.ErrorHTTP4xx
		out.ErrorMsg = truncate("HTTP " + resp.Status)
	case resp.StatusCode >= 500 && resp.StatusCode < 600:
		out.ErrorClass = probe.ErrorHTTP5xx
		out.ErrorMsg = truncate("HTTP " + resp.Status)
	default:
		// 1xx (informational) shouldn't surface here — net/http
		// upgrades through them — but if it does, treat as protocol
		// noise rather than dropping the row.
		out.ErrorClass = probe.ErrorProtocol
		out.ErrorMsg = truncate("unexpected status " + resp.Status)
	}
	return out
}

// classifyTransportError maps a Go error from http.Client.Do into an
// ErrorClass. Order matters: deadline / cancellation are detected
// first so a timeout doesn't mistakenly resolve to ErrorNetwork via
// the generic net.Error path.
//
// context.Canceled is folded into ErrorTimeout: from a dashboard
// perspective both mean "we did not observe a response within our
// window", and the alternative (a separate ErrorCancelled enum
// member) would force every consumer to handle a class they almost
// never need to distinguish from timeout.
func classifyTransportError(ctx context.Context, err error) probe.ErrorClass {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) ||
		errors.Is(err, context.DeadlineExceeded) ||
		errors.Is(ctx.Err(), context.Canceled) ||
		errors.Is(err, context.Canceled) {
		return probe.ErrorTimeout
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return probe.ErrorTimeout
	}

	// url.Error wraps DNS / TLS / TCP failures — they all surface here.
	var ue *url.Error
	if errors.As(err, &ue) {
		// http.Client.Do can also wrap a "no such host" or "connection
		// refused" inside *net.OpError. Either way, it's an
		// ErrorNetwork from our taxonomy's perspective.
		return probe.ErrorNetwork
	}

	// Catch-all: an unwrapped error from the transport. Treat as
	// network so the operator at least sees the failure rather than
	// nothing.
	return probe.ErrorNetwork
}

// truncate caps the message length to probe.ErrMsgMaxLen. The domain
// also truncates inside NewObservation, but trimming earlier means
// the use case never has to mutate a result it received.
func truncate(s string) string {
	if len(s) <= probe.ErrMsgMaxLen {
		return s
	}
	// Avoid splitting a multi-byte rune at the boundary — strings
	// can be UTF-8 from server responses.
	end := probe.ErrMsgMaxLen
	for end > 0 && (s[end]&0xC0) == 0x80 {
		end--
	}
	return strings.TrimRight(s[:end], "\x00")
}
