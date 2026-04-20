// Package ethscan provides the shared client every Etherscan-compat
// adapter (Routescan, Etherscan V2, Blockscout's RPC-style proxy)
// layers on top of.
//
// The V1-compat contract is a single envelope:
//
//	{"status":"1|0","message":"OK|NOTOK|No records found|...","result":<any>}
//
// Success puts the data in result; errors put a human-readable string
// there too. This package hides that shape behind a Call method that
// either decodes result into the caller's destination value or
// returns one of the source.Err* sentinels so downstream code never
// has to read error messages directly.
//
// The client sits under adapters/internal/ so only packages rooted
// in adapters/ can import it.
package ethscan

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/seokheejang/chain-sync-watch/adapters/internal/httpx"
	"github.com/seokheejang/chain-sync-watch/internal/source"
)

// Client talks to one Etherscan-compat endpoint.
type Client struct {
	baseURL string
	apiKey  string
	hc      *httpx.Client
}

// Option configures a Client at construction time.
type Option func(*Client)

// WithAPIKey attaches an &apikey= parameter to every request. Leave
// empty to talk to keyless upstreams (Routescan, Blockscout proxy).
// The key is never logged — only the parameter name appears.
func WithAPIKey(key string) Option {
	return func(c *Client) { c.apiKey = key }
}

// WithHTTPX swaps the underlying httpx.Client. The constructor wires
// a conservative default (2 rps, 15s timeout); pass your own to
// share a limiter across adapters or raise the ceiling for private
// gateways.
func WithHTTPX(hc *httpx.Client) Option {
	return func(c *Client) {
		if hc != nil {
			c.hc = hc
		}
	}
}

// New returns a Client rooted at base. base should be the endpoint
// the adapter targets, e.g.
//
//	https://api.routescan.io/v2/network/mainnet/evm/10/etherscan/api
//	https://api.etherscan.io/v2/api
//
// — everything up to and including /api (or the equivalent path).
// Call paths append ?module=...&action=... on top.
func New(base string, opts ...Option) *Client {
	c := &Client{
		baseURL: strings.TrimRight(base, "?&"),
		hc: httpx.New(
			httpx.WithTimeout(15*time.Second),
			httpx.WithRateLimit(2, 1),
		),
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// envelope is the V1-compat response shape.
type envelope struct {
	Status  string          `json:"status"`
	Message string          `json:"message"`
	Result  json.RawMessage `json:"result"`
}

// Call issues a V1-compat GET, parses the envelope, and either
// unmarshals result into out (if non-nil) or classifies the error.
//
// Caller contract:
//
//   - module/action are the Etherscan-style coordinates ("account",
//     "txlist"); apikey is added automatically when configured.
//   - params may be nil; any values set are URL-encoded and appended.
//   - out may be nil when the caller wants to probe whether the call
//     would succeed without caring about the payload.
//
// The returned error is always either nil or one of the source.Err*
// sentinels (wrapped for context). "No records found" is treated as
// a successful empty response — the result is the literal empty list
// the upstream sent, which unmarshals into out as an empty slice.
func (c *Client) Call(ctx context.Context, module, action string, params map[string]string, out any) error {
	q := url.Values{}
	q.Set("module", module)
	q.Set("action", action)
	for k, v := range params {
		q.Set(k, v)
	}
	if c.apiKey != "" {
		q.Set("apikey", c.apiKey)
	}

	fullURL := c.baseURL + "?" + q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fullURL, http.NoBody)
	if err != nil {
		return fmt.Errorf("ethscan: build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.hc.Do(ctx, req)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return err
		}
		return fmt.Errorf("%w: %v", source.ErrSourceUnavailable, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusTooManyRequests {
		return source.ErrRateLimited
	}
	if resp.StatusCode >= 500 {
		return fmt.Errorf("%w: upstream status %d", source.ErrSourceUnavailable, resp.StatusCode)
	}
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("%w: http %d: %s",
			source.ErrInvalidResponse, resp.StatusCode, previewBody(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("%w: read body: %v", source.ErrInvalidResponse, err)
	}

	var env envelope
	if err := json.Unmarshal(body, &env); err != nil {
		return fmt.Errorf("%w: decode envelope: %v", source.ErrInvalidResponse, err)
	}

	if env.Status == "1" {
		if out == nil {
			return nil
		}
		if err := json.Unmarshal(env.Result, out); err != nil {
			return fmt.Errorf("%w: decode result: %v", source.ErrInvalidResponse, err)
		}
		return nil
	}

	// status != "1" — figure out whether this is a real error or
	// Etherscan's "empty success" pattern.
	if isEmptyResultMessage(env.Message) {
		if out == nil {
			return nil
		}
		// env.Result is the literal empty payload the upstream sent
		// (typically "[]"). Unmarshalling it into out yields the
		// zero value, which is what the caller wants.
		if len(env.Result) == 0 {
			return nil
		}
		if err := json.Unmarshal(env.Result, out); err != nil {
			return fmt.Errorf("%w: decode empty result: %v", source.ErrInvalidResponse, err)
		}
		return nil
	}

	return classifyErrorMessage(env.Result, env.Message)
}

// CallProxy hits ?module=proxy&action=<method>&... and decodes the
// RPC result into out. Some upstreams (Routescan) wrap proxy
// responses in the Etherscan envelope; others (Blockscout, classic
// Etherscan) emit raw JSON-RPC. We sniff the first non-space byte
// pair and handle both.
func (c *Client) CallProxy(ctx context.Context, method string, params map[string]string, out any) error {
	q := url.Values{}
	q.Set("module", "proxy")
	q.Set("action", method)
	for k, v := range params {
		q.Set(k, v)
	}
	if c.apiKey != "" {
		q.Set("apikey", c.apiKey)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"?"+q.Encode(), http.NoBody)
	if err != nil {
		return fmt.Errorf("ethscan: build proxy request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.hc.Do(ctx, req)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return err
		}
		return fmt.Errorf("%w: %v", source.ErrSourceUnavailable, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusTooManyRequests {
		return source.ErrRateLimited
	}
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode >= 500 {
			return fmt.Errorf("%w: upstream status %d", source.ErrSourceUnavailable, resp.StatusCode)
		}
		return fmt.Errorf("%w: http %d: %s",
			source.ErrInvalidResponse, resp.StatusCode, previewBody(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("%w: read body: %v", source.ErrInvalidResponse, err)
	}

	return decodeProxyBody(body, out)
}

// decodeProxyBody handles both envelope-wrapped and raw JSON-RPC
// payloads. We pick by the presence of a top-level "jsonrpc" field;
// absence falls through to envelope mode.
func decodeProxyBody(body []byte, out any) error {
	// Raw JSON-RPC: {"jsonrpc":"2.0", "id":..., "result":..., "error":...}
	var rpc struct {
		JSONRPC string          `json:"jsonrpc"`
		Result  json.RawMessage `json:"result"`
		Error   *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &rpc); err == nil && rpc.JSONRPC != "" {
		if rpc.Error != nil {
			if rpc.Error.Code == -32601 {
				return fmt.Errorf("%w: %s", source.ErrUnsupported, rpc.Error.Message)
			}
			return fmt.Errorf("%w: %s", source.ErrInvalidResponse, rpc.Error.Message)
		}
		if out == nil || len(rpc.Result) == 0 {
			return nil
		}
		if err := json.Unmarshal(rpc.Result, out); err != nil {
			return fmt.Errorf("%w: decode proxy result: %v", source.ErrInvalidResponse, err)
		}
		return nil
	}

	// Fallback: envelope shape with nested result.
	var env envelope
	if err := json.Unmarshal(body, &env); err != nil {
		return fmt.Errorf("%w: decode proxy envelope: %v", source.ErrInvalidResponse, err)
	}
	if env.Status != "1" {
		return classifyErrorMessage(env.Result, env.Message)
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(env.Result, out)
}

// isEmptyResultMessage recognises Etherscan's idiomatic "no data
// here, but the call itself worked" messages. The set is small and
// stable across upstreams; new phrases can be added as encountered.
func isEmptyResultMessage(msg string) bool {
	lower := strings.ToLower(msg)
	switch {
	case strings.Contains(lower, "no transactions found"),
		strings.Contains(lower, "no records found"):
		return true
	}
	return false
}

// classifyErrorMessage maps the upstream error message onto one of
// the source.Err* sentinels. We pattern-match on substrings because
// Etherscan / Routescan / Blockscout all phrase their errors
// slightly differently but share the same underlying categories.
func classifyErrorMessage(rawResult json.RawMessage, message string) error {
	// result is usually a JSON-encoded string like
	// "Error! Missing Or invalid Action name"; peel the quotes.
	var detail string
	_ = json.Unmarshal(rawResult, &detail)
	lowerDetail := strings.ToLower(detail)
	lowerMsg := strings.ToLower(message)

	contains := func(s ...string) bool {
		for _, needle := range s {
			if strings.Contains(lowerDetail, needle) || strings.Contains(lowerMsg, needle) {
				return true
			}
		}
		return false
	}

	switch {
	case contains("missing or invalid action", "invalid action", "method not found"):
		return fmt.Errorf("%w: %s", source.ErrUnsupported, firstNonEmpty(detail, message))
	case contains("rate limit", "throttled", "too many requests"):
		return source.ErrRateLimited
	case contains("invalid api key", "missing api key"):
		return fmt.Errorf("%w: %s", source.ErrInvalidResponse, firstNonEmpty(detail, message))
	}
	return fmt.Errorf("%w: %s", source.ErrInvalidResponse, firstNonEmpty(detail, message))
}

func firstNonEmpty(s ...string) string {
	for _, v := range s {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// previewBody trims a response body to a log-friendly size.
func previewBody(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 200 {
		return s[:200] + "…"
	}
	return s
}
