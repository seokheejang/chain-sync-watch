package rpc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"

	"github.com/seokheejang/chain-sync-watch/internal/source"
)

// rpcRequest mirrors the JSON-RPC 2.0 request envelope. id monotonic
// across a single Adapter instance so server logs can correlate.
type rpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      uint64 `json:"id"`
	Method  string `json:"method"`
	Params  []any  `json:"params"`
}

// rpcError is the JSON-RPC 2.0 error object. Code meanings follow the
// spec: -32700 parse error, -32600 invalid request, -32601 method not
// found, -32602 invalid params, -32603 internal error; node-specific
// errors live in the -32000..-32099 band.
type rpcError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *rpcError) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("rpc error %d: %s", e.Code, e.Message)
}

// rpcResponse holds a single response. Result is left as
// json.RawMessage so callers decode into whatever shape they expect
// without a second marshal round.
type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      uint64          `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *rpcError       `json:"error,omitempty"`
}

// nextID returns a fresh request id. Safe for concurrent use.
func (a *Adapter) nextID() uint64 {
	return atomic.AddUint64(&a.reqID, 1)
}

// reqID holds the monotonic request counter. Declared here rather
// than in adapter.go so the id plumbing stays adjacent to the rest
// of the JSON-RPC internals.
//
// (The field itself is added to the Adapter struct below via a small
// extension — Go allows an unexported field to be populated from any
// file in the same package.)

// callRPC POSTs a JSON-RPC request and decodes the result into out.
// The function handles all transport concerns — retry, rate limit,
// and error classification — and surfaces every upstream failure as
// one of the source.Err* sentinels so adapter callers never need to
// branch on low-level details.
func (a *Adapter) callRPC(ctx context.Context, method string, out any, params ...any) error {
	reqBody, err := json.Marshal(rpcRequest{
		JSONRPC: "2.0",
		ID:      a.nextID(),
		Method:  method,
		Params:  params,
	})
	if err != nil {
		return fmt.Errorf("rpc: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, a.url, bytes.NewReader(reqBody))
	if err != nil {
		return fmt.Errorf("rpc: build http request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")

	resp, err := a.hc.Do(ctx, httpReq)
	if err != nil {
		return classifyTransportErr(err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("rpc: read body: %w", err)
	}

	// JSON-RPC servers are supposed to return 200 even for protocol
	// errors (the error lives in the JSON body). Some proxies do not
	// follow that convention, so treat 4xx/5xx as transport issues.
	if resp.StatusCode >= 400 {
		return classifyHTTPStatus(resp.StatusCode, body)
	}

	var env rpcResponse
	if err := json.Unmarshal(body, &env); err != nil {
		return fmt.Errorf("%w: decode envelope: %v", source.ErrInvalidResponse, err)
	}
	if env.Error != nil {
		return classifyRPCError(env.Error)
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(env.Result, out); err != nil {
		return fmt.Errorf("%w: decode result for %s: %v", source.ErrInvalidResponse, method, err)
	}
	return nil
}

// classifyTransportErr maps an httpx-level error onto the adapter's
// sentinel set. We check for context errors first because httpx wraps
// them inside its "exhausted N attempts" wrapper on the retry path.
func classifyTransportErr(err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	// Everything else is a transport-layer failure we classify as
	// "source unavailable" so the scheduler can skip this source for
	// the run instead of marking the whole verification failed.
	return fmt.Errorf("%w: %v", source.ErrSourceUnavailable, err)
}

// classifyHTTPStatus maps a non-JSON-RPC HTTP response (i.e. a proxy
// or gateway replied before the node could) onto the sentinel set.
func classifyHTTPStatus(status int, body []byte) error {
	switch {
	case status == http.StatusTooManyRequests:
		return source.ErrRateLimited
	case status >= 500:
		return fmt.Errorf("%w: upstream status %d", source.ErrSourceUnavailable, status)
	default:
		preview := strings.TrimSpace(string(body))
		if len(preview) > 200 {
			preview = preview[:200] + "…"
		}
		return fmt.Errorf("%w: http %d: %s", source.ErrInvalidResponse, status, preview)
	}
}

// classifyRPCError maps JSON-RPC error codes onto sentinel set. Most
// node implementations reuse the standard error code band so we can
// branch without parsing messages.
//
// Known public meanings (from JSON-RPC 2.0 spec + de-facto geth usage):
//
//	-32700 parse error        → ErrInvalidResponse (our code)
//	-32601 method not found   → ErrUnsupported
//	-32602 invalid params     → ErrInvalidResponse
//	-32603 internal error     → ErrSourceUnavailable
//	-32000..-32099 server     → ErrSourceUnavailable (conservative)
//
// Everything else is wrapped as ErrInvalidResponse so callers can
// branch conservatively.
func classifyRPCError(e *rpcError) error {
	switch {
	case e.Code == -32601:
		return fmt.Errorf("%w: %s", source.ErrUnsupported, e.Message)
	case e.Code == -32700 || e.Code == -32600 || e.Code == -32602:
		return fmt.Errorf("%w: %s", source.ErrInvalidResponse, e.Message)
	case e.Code == -32603:
		return fmt.Errorf("%w: %s", source.ErrSourceUnavailable, e.Message)
	case e.Code >= -32099 && e.Code <= -32000:
		// Many nodes use this band for rate limiting too; match on
		// message when the hint is obvious.
		if strings.Contains(strings.ToLower(e.Message), "rate limit") {
			return source.ErrRateLimited
		}
		return fmt.Errorf("%w: %s", source.ErrSourceUnavailable, e.Message)
	}
	return fmt.Errorf("%w: %s", source.ErrInvalidResponse, e.Message)
}
