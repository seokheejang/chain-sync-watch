package rpc_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

// mockRPC is a tiny JSON-RPC 2.0 server for adapter tests. Tests
// register per-method handlers via Handle(); the server decodes the
// incoming request, dispatches by method name, and wraps the return
// value (or error) in the correct envelope shape.
type mockRPC struct {
	mu       sync.Mutex
	srv      *httptest.Server
	handlers map[string]func(params []json.RawMessage) (any, *mockRPCError)
	calls    []mockCall
}

type mockCall struct {
	Method string
	Params []json.RawMessage
}

type mockRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func newMockRPC(t *testing.T) *mockRPC {
	t.Helper()
	m := &mockRPC{
		handlers: make(map[string]func([]json.RawMessage) (any, *mockRPCError)),
	}
	m.srv = httptest.NewServer(http.HandlerFunc(m.serve))
	t.Cleanup(m.srv.Close)
	return m
}

// URL returns the server's base URL — pass it to rpc.New.
func (m *mockRPC) URL() string { return m.srv.URL }

// Handle registers a handler for method. The handler receives the raw
// param list and returns either the result (any JSON-serialisable
// value) or a JSON-RPC error. Overrides any previous handler for the
// same method.
func (m *mockRPC) Handle(method string, fn func(params []json.RawMessage) (any, *mockRPCError)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.handlers[method] = fn
}

// Calls returns a copy of the recorded requests in call order.
func (m *mockRPC) Calls() []mockCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]mockCall, len(m.calls))
	copy(out, m.calls)
	return out
}

func (m *mockRPC) serve(w http.ResponseWriter, r *http.Request) {
	defer func() { _ = r.Body.Close() }()

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	var req struct {
		JSONRPC string            `json:"jsonrpc"`
		ID      uint64            `json:"id"`
		Method  string            `json:"method"`
		Params  []json.RawMessage `json:"params"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	m.mu.Lock()
	m.calls = append(m.calls, mockCall{Method: req.Method, Params: req.Params})
	h, ok := m.handlers[req.Method]
	m.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	if !ok {
		// Emulate geth's "method not found" so classifyRPCError maps
		// this onto ErrUnsupported.
		resp := map[string]any{
			"jsonrpc": "2.0",
			"id":      req.ID,
			"error": map[string]any{
				"code":    -32601,
				"message": "method not found: " + req.Method,
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
		return
	}

	result, errObj := h(req.Params)
	envelope := map[string]any{
		"jsonrpc": "2.0",
		"id":      req.ID,
	}
	if errObj != nil {
		envelope["error"] = errObj
	} else {
		envelope["result"] = result
	}
	_ = json.NewEncoder(w).Encode(envelope)
}

// paramString is a small helper for tests that want the Nth param as
// a string. Fails the test on decode error.
func paramString(t *testing.T, params []json.RawMessage, idx int) string {
	t.Helper()
	require.Greater(t, len(params), idx)
	var s string
	require.NoError(t, json.Unmarshal(params[idx], &s))
	return s
}

// paramBool mirrors paramString for boolean params (eth_getBlockByNumber's
// "include full tx objects" flag).
func paramBool(t *testing.T, params []json.RawMessage, idx int) bool {
	t.Helper()
	require.Greater(t, len(params), idx)
	var b bool
	require.NoError(t, json.Unmarshal(params[idx], &b))
	return b
}
