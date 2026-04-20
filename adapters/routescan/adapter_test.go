package routescan_test

import (
	"context"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/seokheejang/chain-sync-watch/adapters/routescan"
	"github.com/seokheejang/chain-sync-watch/internal/chain"
	"github.com/seokheejang/chain-sync-watch/internal/source"
)

var _ source.Source = (*routescan.Adapter)(nil)

const (
	fixtureAddr  = "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	fixtureToken = "0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

// The test harness routes by (module, action) — Routescan is a
// single-endpoint API so everything hits the same server URL.
type harness struct {
	srv      *httptest.Server
	handlers map[string]func(q url.Values) (any, string, string)
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	h := &harness{handlers: map[string]func(url.Values) (any, string, string){}}
	h.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		key := q.Get("module") + "/" + q.Get("action")
		fn, ok := h.handlers[key]
		if !ok {
			http.Error(w, "no handler: "+key, http.StatusNotFound)
			return
		}
		result, status, message := fn(q)
		resultBytes, _ := json.Marshal(result)
		env := map[string]any{
			"status":  status,
			"message": message,
			"result":  json.RawMessage(resultBytes),
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(env)
	}))
	t.Cleanup(h.srv.Close)
	return h
}

func (h *harness) on(module, action string, fn func(url.Values) (any, string, string)) {
	h.handlers[module+"/"+action] = fn
}

func newAdapter(t *testing.T, h *harness) *routescan.Adapter {
	t.Helper()
	a, err := routescan.New(chain.OptimismMainnet, routescan.WithBaseURL(h.srv.URL))
	require.NoError(t, err)
	return a
}

func TestAdapter_Identity(t *testing.T) {
	a, err := routescan.New(chain.OptimismMainnet)
	require.NoError(t, err)
	require.Equal(t, source.SourceID("routescan"), a.ID())
	require.Equal(t, chain.OptimismMainnet, a.ChainID())
}

// FetchBlock goes through the proxy module — the whole block header
// (roots included) arrives in one response.
func TestFetchBlock_ViaProxy(t *testing.T) {
	h := newHarness(t)
	h.on("proxy", "eth_getBlockByNumber", func(q url.Values) (any, string, string) {
		require.Equal(t, "0x64", q.Get("tag"))
		return map[string]any{
			"number":           "0x64",
			"hash":             "0x" + strings.Repeat("a", 64),
			"parentHash":       "0x" + strings.Repeat("b", 64),
			"stateRoot":        "0x" + strings.Repeat("c", 64),
			"transactionsRoot": "0x" + strings.Repeat("d", 64),
			"receiptsRoot":     "0x" + strings.Repeat("e", 64),
			"miner":            "0x4200000000000000000000000000000000000011",
			"gasUsed":          "0x5208",
			"timestamp":        "0x68054310",
			"transactions":     []string{"0x1", "0x2"},
		}, "1", "OK"
	})

	res, err := newAdapter(t, h).FetchBlock(context.Background(), source.BlockQuery{
		Number: chain.NewBlockNumber(100),
	})
	require.NoError(t, err)
	require.NotNil(t, res.Hash)
	require.NotNil(t, res.StateRoot)
	require.NotNil(t, res.ReceiptsRoot)
	require.NotNil(t, res.TransactionsRoot)
	require.NotNil(t, res.Miner)
	require.Equal(t, uint64(2), *res.TxCount)
}

// FetchAddressLatest combines account/balance with proxy nonce.
func TestFetchAddressLatest(t *testing.T) {
	h := newHarness(t)
	h.on("account", "balance", func(q url.Values) (any, string, string) {
		require.Equal(t, chain.MustAddress(fixtureAddr).Hex(), q.Get("address"))
		return "123456789", "1", "OK"
	})
	h.on("proxy", "eth_getTransactionCount", func(_ url.Values) (any, string, string) {
		return "0xa", "1", "OK"
	})

	res, err := newAdapter(t, h).FetchAddressLatest(context.Background(),
		source.AddressQuery{Address: chain.MustAddress(fixtureAddr)})
	require.NoError(t, err)
	require.Equal(t, 0, res.Balance.Cmp(big.NewInt(123456789)))
	require.NotNil(t, res.Nonce)
	require.Equal(t, uint64(10), *res.Nonce)
}

// FetchAddressAtBlock uses balancehistory + proxy nonce at the
// numeric block. This is the Routescan-specific win — free tier
// historical balance on Optimism.
func TestFetchAddressAtBlock_UsesBalanceHistory(t *testing.T) {
	h := newHarness(t)
	h.on("account", "balancehistory", func(q url.Values) (any, string, string) {
		require.Equal(t, "1000000", q.Get("blockno"))
		return "77960845929259999654297", "1", "OK"
	})
	h.on("proxy", "eth_getTransactionCount", func(q url.Values) (any, string, string) {
		require.Equal(t, "0xf4240", q.Get("tag"))
		return "0x5", "1", "OK"
	})

	res, err := newAdapter(t, h).FetchAddressAtBlock(context.Background(),
		source.AddressAtBlockQuery{
			Address: chain.MustAddress(fixtureAddr),
			Block:   chain.NewBlockNumber(1_000_000),
		})
	require.NoError(t, err)
	expected, _ := new(big.Int).SetString("77960845929259999654297", 10)
	require.Equal(t, 0, res.Balance.Cmp(expected))
	require.NotNil(t, res.ReflectedBlock)
	require.Equal(t, uint64(1_000_000), res.ReflectedBlock.Uint64())
}

// FetchERC20Holdings returns every Routescan-reported holding — we
// deliberately do NOT filter spam here (the judgement layer does).
func TestFetchERC20Holdings(t *testing.T) {
	h := newHarness(t)
	h.on("account", "addresstokenbalance", func(_ url.Values) (any, string, string) {
		return []any{
			map[string]any{
				"TokenAddress":  fixtureToken,
				"TokenName":     "USD Coin",
				"TokenSymbol":   "USDC",
				"TokenQuantity": "2004458",
				"TokenDivisor":  "6",
			},
		}, "1", "OK"
	})

	res, err := newAdapter(t, h).FetchERC20Holdings(context.Background(),
		source.ERC20HoldingsQuery{Address: chain.MustAddress(fixtureAddr)})
	require.NoError(t, err)
	require.Len(t, res.Tokens, 1)
	require.Equal(t, "USDC", res.Tokens[0].Symbol)
	require.Equal(t, uint8(6), res.Tokens[0].Decimals)
}

// Snapshot is explicitly unsupported.
func TestFetchSnapshot_Unsupported(t *testing.T) {
	a, err := routescan.New(chain.OptimismMainnet, routescan.WithBaseURL("http://unused"))
	require.NoError(t, err)
	_, err = a.FetchSnapshot(context.Background(), source.SnapshotQuery{})
	require.ErrorIs(t, err, source.ErrUnsupported)
	require.False(t, a.Supports(source.CapTotalAddressCount))
}

// FetchInternalTxByBlock uses startblock=endblock=N. The result
// flattens straight into InternalTx.
func TestFetchInternalTxByBlock(t *testing.T) {
	h := newHarness(t)
	h.on("account", "txlistinternal", func(q url.Values) (any, string, string) {
		require.Equal(t, "100", q.Get("startblock"))
		require.Equal(t, "100", q.Get("endblock"))
		return []any{
			map[string]any{
				"blockNumber": "100",
				"from":        fixtureAddr,
				"to":          fixtureToken,
				"value":       "0",
				"gas":         "100000",
				"gasUsed":     "21000",
				"type":        "call",
				"isError":     "0",
			},
		}, "1", "OK"
	})

	res, err := newAdapter(t, h).FetchInternalTxByBlock(context.Background(),
		source.InternalTxByBlockQuery{Block: chain.NewBlockNumber(100)})
	require.NoError(t, err)
	require.Len(t, res.Traces, 1)
	require.Equal(t, "call", res.Traces[0].CallType)
	require.Equal(t, uint64(21000), res.Traces[0].GasUsed)
	require.NotNil(t, res.ReflectedBlock)
	require.Equal(t, uint64(100), res.ReflectedBlock.Uint64())
}

func TestSupports_NeverPanics(t *testing.T) {
	a, err := routescan.New(chain.OptimismMainnet, routescan.WithBaseURL("http://unused"))
	require.NoError(t, err)
	for _, c := range source.AllCapabilities() {
		_ = a.Supports(c)
	}
}

// The default (no WithBaseURL) constructor wires the canonical
// Routescan URL — smoke-check that the URL template is applied.
func TestBaseURL_DefaultTemplate(t *testing.T) {
	want := "https://api.routescan.io/v2/network/mainnet/evm/10/etherscan/api"
	require.Equal(t, want, routescan.BaseURL(chain.OptimismMainnet))
}
