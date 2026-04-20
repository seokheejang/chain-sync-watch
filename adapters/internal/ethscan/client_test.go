package ethscan_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/seokheejang/chain-sync-watch/adapters/internal/ethscan"
	"github.com/seokheejang/chain-sync-watch/internal/source"
)

// The Etherscan V1-compat envelope looks like
//
//	{"status":"1","message":"OK","result":<any>}
//
// on success. Every adapter built on top of this client relies on
// the parsing being faithful to that shape, so the first test locks
// in the common path — a scalar string result (as eth_getBalance
// style endpoints return).
func TestClient_Call_SuccessScalarResult(t *testing.T) {
	srv := newEthscanServer(t, func(q url.Values) (any, string, string) {
		require.Equal(t, "account", q.Get("module"))
		require.Equal(t, "balance", q.Get("action"))
		require.Equal(t, "0xaaaa", q.Get("address"))
		return "1000000000000000000", "1", "OK"
	})
	defer srv.Close()

	c := ethscan.New(srv.URL)
	var got string
	err := c.Call(context.Background(), "account", "balance",
		map[string]string{"address": "0xaaaa"}, &got)
	require.NoError(t, err)
	require.Equal(t, "1000000000000000000", got)
}

// Array-shaped results (txlist and its cousins) must unmarshal into
// a slice of structs without the caller needing to know the envelope
// exists.
func TestClient_Call_SuccessArrayResult(t *testing.T) {
	type item struct {
		Hash  string `json:"hash"`
		Value string `json:"value"`
	}
	srv := newEthscanServer(t, func(_ url.Values) (any, string, string) {
		return []item{{Hash: "0x1", Value: "100"}, {Hash: "0x2", Value: "200"}}, "1", "OK"
	})
	defer srv.Close()

	c := ethscan.New(srv.URL)
	var out []item
	require.NoError(t, c.Call(context.Background(), "account", "txlist", nil, &out))
	require.Equal(t, []item{{Hash: "0x1", Value: "100"}, {Hash: "0x2", Value: "200"}}, out)
}

// "No transactions found" / "No records found" is Etherscan's
// idiomatic "empty success": status=0 but result is an empty list
// and the operation actually succeeded. The client must surface
// this as a nil error so callers can just iterate the decoded slice.
func TestClient_Call_NoRecordsTreatedAsEmpty(t *testing.T) {
	srv := newEthscanServer(t, func(_ url.Values) (any, string, string) {
		return []any{}, "0", "No transactions found"
	})
	defer srv.Close()

	c := ethscan.New(srv.URL)
	var out []map[string]any
	require.NoError(t, c.Call(context.Background(), "account", "txlist", nil, &out))
	require.Empty(t, out)
}

// A real error status must classify onto the source.Err* sentinels
// so callers can branch without reading message strings themselves.
func TestClient_Call_ErrorClassification(t *testing.T) {
	cases := []struct {
		name    string
		status  string
		message string
		result  any
		want    error
	}{
		{
			name:    "unsupported action",
			status:  "0",
			message: "NOTOK",
			result:  "Error! Missing Or invalid Action name",
			want:    source.ErrUnsupported,
		},
		{
			name:    "rate limit",
			status:  "0",
			message: "NOTOK",
			result:  "Max rate limit reached, please use API Key for higher rate limit",
			want:    source.ErrRateLimited,
		},
		{
			name:    "invalid api key",
			status:  "0",
			message: "NOTOK",
			result:  "Invalid API Key",
			want:    source.ErrInvalidResponse,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := newEthscanServer(t, func(_ url.Values) (any, string, string) {
				return tc.result, tc.status, tc.message
			})
			defer srv.Close()

			c := ethscan.New(srv.URL)
			var ignored any
			err := c.Call(context.Background(), "account", "balance", nil, &ignored)
			require.Error(t, err)
			require.Truef(t, errors.Is(err, tc.want),
				"expected %v, got %v", tc.want, err)
		})
	}
}

// The API key, when configured, must be appended as an &apikey=
// query parameter. For keyless endpoints (Routescan, Blockscout) the
// param must NOT appear — some gateways reject requests with empty
// apikey values.
func TestClient_Call_APIKeyHandling(t *testing.T) {
	var seenWithKey, seenWithoutKey url.Values
	srvWith := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenWithKey = r.URL.Query()
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "1", "message": "OK", "result": "ok",
		})
	}))
	defer srvWith.Close()
	srvWithout := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenWithoutKey = r.URL.Query()
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "1", "message": "OK", "result": "ok",
		})
	}))
	defer srvWithout.Close()

	cWith := ethscan.New(srvWith.URL, ethscan.WithAPIKey("SECRET123"))
	var s string
	require.NoError(t, cWith.Call(context.Background(), "x", "y", nil, &s))
	require.Equal(t, "SECRET123", seenWithKey.Get("apikey"))

	cNoKey := ethscan.New(srvWithout.URL)
	require.NoError(t, cNoKey.Call(context.Background(), "x", "y", nil, &s))
	require.False(t, seenWithoutKey.Has("apikey"), "apikey must be absent when not configured")
}

// Caller-supplied params must be URL-encoded correctly — the test
// uses a startblock/endblock pair to lock in ordering invariance and
// correct encoding of numeric values.
func TestClient_Call_ParamEncoding(t *testing.T) {
	var seen url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.URL.Query()
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "1", "message": "OK", "result": "ok",
		})
	}))
	defer srv.Close()

	c := ethscan.New(srv.URL)
	var s string
	require.NoError(t, c.Call(context.Background(), "account", "txlist",
		map[string]string{
			"startblock": "1000000",
			"endblock":   "2000000",
			"address":    "0x" + strings.Repeat("a", 40),
		}, &s))

	require.Equal(t, "account", seen.Get("module"))
	require.Equal(t, "txlist", seen.Get("action"))
	require.Equal(t, "1000000", seen.Get("startblock"))
	require.Equal(t, "2000000", seen.Get("endblock"))
	require.Equal(t, "0x"+strings.Repeat("a", 40), seen.Get("address"))
}

// Transport errors bubble out as ErrSourceUnavailable so the scheduler
// can skip this source for the current run — not as an opaque wrapper.
func TestClient_Call_TransportErrorMapsToUnavailable(t *testing.T) {
	// Point at a port that is certainly closed to force a dial error.
	c := ethscan.New("http://127.0.0.1:1/api")
	var s string
	err := c.Call(context.Background(), "x", "y", nil, &s)
	require.Error(t, err)
	require.True(t, errors.Is(err, source.ErrSourceUnavailable),
		"expected ErrSourceUnavailable, got %v", err)
}

// --- Helpers ----------------------------------------------------------------

// newEthscanServer wires up a minimal server that decodes the URL
// query, asks the test callback for an envelope, and writes it as
// Etherscan-style JSON.
func newEthscanServer(
	t *testing.T,
	fn func(q url.Values) (result any, status string, message string),
) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		result, status, message := fn(r.URL.Query())
		resultBytes, err := json.Marshal(result)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		env := map[string]any{
			"status":  status,
			"message": message,
			"result":  json.RawMessage(resultBytes),
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(env)
	}))
}
