package blockscout_test

import (
	"context"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/seokheejang/chain-sync-watch/adapters/blockscout"
	"github.com/seokheejang/chain-sync-watch/internal/chain"
	"github.com/seokheejang/chain-sync-watch/internal/source"
)

var _ source.Source = (*blockscout.Adapter)(nil)

const (
	fixtureAddr  = "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	fixtureToken = "0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

type blockscoutHarness struct {
	srv    *httptest.Server
	routes map[string]http.HandlerFunc
}

func newHarness(t *testing.T) *blockscoutHarness {
	t.Helper()
	h := &blockscoutHarness{routes: map[string]http.HandlerFunc{}}
	h.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if r.URL.RawQuery != "" {
			// Proxy module routes collapse to /api + action for match.
			if strings.HasPrefix(path, "/api") && r.URL.Query().Get("module") == "proxy" {
				path = "/api?proxy=" + r.URL.Query().Get("action")
			}
		}
		if h, ok := h.routes[path]; ok {
			h(w, r)
			return
		}
		http.Error(w, "no route: "+path, http.StatusNotFound)
	}))
	t.Cleanup(h.srv.Close)
	return h
}

func (h *blockscoutHarness) route(path string, fn http.HandlerFunc) { h.routes[path] = fn }
func (h *blockscoutHarness) url() string                            { return h.srv.URL }

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func TestAdapter_Identity(t *testing.T) {
	a, err := blockscout.New(chain.OptimismMainnet)
	require.NoError(t, err)
	require.Equal(t, source.SourceID("blockscout"), a.ID())
	require.Equal(t, chain.OptimismMainnet, a.ChainID())
}

// FetchBlock round-trips every nine fields through the proxy module
// so state/receipts/tx roots (absent from REST v2) come for free.
func TestFetchBlock_AllFields(t *testing.T) {
	h := newHarness(t)
	h.route("/api?proxy=eth_getBlockByNumber", func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "0x150534f", r.URL.Query().Get("tag"))
		require.Equal(t, "false", r.URL.Query().Get("boolean"))
		writeJSON(w, map[string]any{
			"jsonrpc": "2.0", "id": 1,
			"result": map[string]any{
				"number":           "0x150534f",
				"hash":             "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
				"parentHash":       "0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
				"stateRoot":        "0xcccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
				"transactionsRoot": "0xdddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd",
				"receiptsRoot":     "0xeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee",
				"miner":            "0x4200000000000000000000000000000000000011",
				"gasUsed":          "0xbc614e",
				"timestamp":        "0x68054310",
				"transactions":     []string{"0x1", "0x2", "0x3"},
			},
		})
	})

	a, err := blockscout.New(chain.OptimismMainnet, blockscout.WithBaseURL(h.url()))
	require.NoError(t, err)

	res, err := a.FetchBlock(context.Background(), source.BlockQuery{
		Number: chain.NewBlockNumber(0x150534f),
	})
	require.NoError(t, err)
	require.NotNil(t, res.Hash)
	require.NotNil(t, res.StateRoot)
	require.NotNil(t, res.ReceiptsRoot)
	require.NotNil(t, res.TransactionsRoot)
	require.NotNil(t, res.Miner)
	require.NotNil(t, res.GasUsed)
	require.NotNil(t, res.TxCount)
	require.Equal(t, uint64(3), *res.TxCount)
}

// FetchAddressLatest pulls balance from REST v2 and nonce from the
// proxy module — two calls but only one of them has the
// reflected-block hint we propagate into the result.
func TestFetchAddressLatest_CombinesRestAndProxy(t *testing.T) {
	h := newHarness(t)
	h.route("/api/v2/addresses/"+chain.MustAddress(fixtureAddr).Hex(), func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{
			"coin_balance":                    "1000000000000000000",
			"block_number_balance_updated_at": 150534783,
		})
	})
	h.route("/api?proxy=eth_getTransactionCount", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{"jsonrpc": "2.0", "id": 1, "result": "0x2a"})
	})

	a, err := blockscout.New(chain.OptimismMainnet, blockscout.WithBaseURL(h.url()))
	require.NoError(t, err)

	res, err := a.FetchAddressLatest(context.Background(), source.AddressQuery{
		Address: chain.MustAddress(fixtureAddr),
	})
	require.NoError(t, err)
	require.Equal(t, 0, res.Balance.Cmp(new(big.Int).Mul(big.NewInt(1), new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil)))) // 1 ETH
	require.NotNil(t, res.Nonce)
	require.Equal(t, uint64(42), *res.Nonce)
	require.NotNil(t, res.ReflectedBlock)
	require.Equal(t, uint64(150534783), res.ReflectedBlock.Uint64())
}

// ERC-20 holdings drops scam-flagged tokens and non-ERC-20 types,
// and carries the parent /addresses/{addr} reflected block as the
// inferred anchor-window hint (API does not expose per-item meta).
func TestFetchERC20Holdings_ScamFilteringAndReflectedBlock(t *testing.T) {
	h := newHarness(t)
	h.route("/api/v2/addresses/"+chain.MustAddress(fixtureAddr).Hex(), func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{
			"coin_balance":                    "0",
			"block_number_balance_updated_at": 9001,
		})
	})
	h.route("/api/v2/addresses/"+chain.MustAddress(fixtureAddr).Hex()+"/token-balances", func(w http.ResponseWriter, _ *http.Request) {
		payload := []any{
			map[string]any{
				"token": map[string]any{
					"address_hash": fixtureToken,
					"name":         "USD Coin",
					"symbol":       "USDC",
					"decimals":     "6",
					"type":         "ERC-20",
					"reputation":   "ok",
					"is_scam":      false,
				},
				"value": "1000000",
			},
			map[string]any{
				"token": map[string]any{
					"address_hash": "0xcccccccccccccccccccccccccccccccccccccccc",
					"name":         "scammy",
					"symbol":       "SCAM",
					"decimals":     "18",
					"type":         "ERC-20",
					"is_scam":      true,
				},
				"value": "999",
			},
			map[string]any{
				"token": map[string]any{
					"address_hash": "0xdddddddddddddddddddddddddddddddddddddddd",
					"name":         "nft",
					"symbol":       "NFT",
					"type":         "ERC-721",
					"reputation":   "ok",
				},
				"value": "1",
			},
		}
		writeJSON(w, payload)
	})

	a, err := blockscout.New(chain.OptimismMainnet, blockscout.WithBaseURL(h.url()))
	require.NoError(t, err)

	res, err := a.FetchERC20Holdings(context.Background(), source.ERC20HoldingsQuery{
		Address: chain.MustAddress(fixtureAddr),
	})
	require.NoError(t, err)
	require.Len(t, res.Tokens, 1)
	require.Equal(t, "USDC", res.Tokens[0].Symbol)
	require.Equal(t, uint8(6), res.Tokens[0].Decimals)
	require.NotNil(t, res.ReflectedBlock)
	require.Equal(t, uint64(9001), res.ReflectedBlock.Uint64())
}

// FetchERC20Holdings refuses a numeric Anchor — REST v2 is latest-
// only, so we would otherwise lie about the historical view.
func TestFetchERC20Holdings_NumericAnchorUnsupported(t *testing.T) {
	a, err := blockscout.New(chain.OptimismMainnet, blockscout.WithBaseURL("http://unused"))
	require.NoError(t, err)
	_, err = a.FetchERC20Holdings(context.Background(), source.ERC20HoldingsQuery{
		Address: chain.MustAddress(fixtureAddr),
		Anchor:  source.BlockTagAt(chain.NewBlockNumber(1)),
	})
	require.ErrorIs(t, err, source.ErrUnsupported)
}

// FetchSnapshot pulls the chain-wide aggregates Blockscout owns
// uniquely among our sources.
func TestFetchSnapshot(t *testing.T) {
	h := newHarness(t)
	h.route("/api/v2/stats", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{
			"total_blocks":       "150000000",
			"total_addresses":    "500000",
			"total_transactions": "9876543",
		})
	})
	a, err := blockscout.New(chain.OptimismMainnet, blockscout.WithBaseURL(h.url()))
	require.NoError(t, err)
	res, err := a.FetchSnapshot(context.Background(), source.SnapshotQuery{})
	require.NoError(t, err)
	require.NotNil(t, res.TotalAddressCount)
	require.Equal(t, uint64(500000), *res.TotalAddressCount)
	require.NotNil(t, res.TotalTxCount)
	require.Equal(t, uint64(9876543), *res.TotalTxCount)
}

// InternalTxByTx flattens Blockscout's response items into our
// InternalTx slice and surfaces the item-provided block_number as
// the reflected block.
func TestFetchInternalTxByTx(t *testing.T) {
	h := newHarness(t)
	txh := "0x" + strings.Repeat("a", 64)
	h.route("/api/v2/transactions/"+txh+"/internal-transactions", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{
			"items": []any{
				map[string]any{
					"block_number": 150000001,
					"type":         "call",
					"value":        "100",
					"gas_limit":    "21000",
					"from":         map[string]string{"hash": fixtureAddr},
					"to":           map[string]string{"hash": fixtureToken},
				},
				map[string]any{
					"block_number": 150000001,
					"type":         "staticcall",
					"value":        "0",
					"gas_limit":    "5000",
					"from":         map[string]string{"hash": fixtureToken},
					"to":           map[string]string{"hash": fixtureAddr},
				},
			},
		})
	})

	a, err := blockscout.New(chain.OptimismMainnet, blockscout.WithBaseURL(h.url()))
	require.NoError(t, err)
	hash, err := chain.NewHash32(txh)
	require.NoError(t, err)
	res, err := a.FetchInternalTxByTx(context.Background(), source.InternalTxByTxQuery{TxHash: hash})
	require.NoError(t, err)
	require.Len(t, res.Traces, 2)
	require.Equal(t, "call", res.Traces[0].CallType)
	require.Equal(t, "staticcall", res.Traces[1].CallType)
	require.NotNil(t, res.ReflectedBlock)
	require.Equal(t, uint64(150000001), res.ReflectedBlock.Uint64())
}

func TestFetchInternalTxByBlock_Unsupported(t *testing.T) {
	a, err := blockscout.New(chain.OptimismMainnet, blockscout.WithBaseURL("http://unused"))
	require.NoError(t, err)
	_, err = a.FetchInternalTxByBlock(context.Background(), source.InternalTxByBlockQuery{})
	require.ErrorIs(t, err, source.ErrUnsupported)
	require.False(t, a.Supports(source.CapInternalTxByBlock))
}

func TestSupports_FullMatrix(t *testing.T) {
	a, err := blockscout.New(chain.OptimismMainnet, blockscout.WithBaseURL("http://unused"))
	require.NoError(t, err)
	for _, c := range source.AllCapabilities() {
		_ = a.Supports(c) // must not panic; absent case = false
	}
}
