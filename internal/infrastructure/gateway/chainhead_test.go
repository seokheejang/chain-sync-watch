package gateway_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/seokheejang/chain-sync-watch/internal/application"
	"github.com/seokheejang/chain-sync-watch/internal/chain"
	"github.com/seokheejang/chain-sync-watch/internal/infrastructure/gateway"
)

// rpcStub answers a tiny JSON-RPC surface: eth_blockNumber (hex
// string) and eth_getBlockByNumber (minimal rawBlock shape). The
// ChainHead implementation only needs .Number from the finalized
// response, so the stub keeps the JSON skeleton minimal.
func rpcStub(t *testing.T, tip string, finalizedBlock string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID     any    `json:"id"`
			Method string `json:"method"`
			Params []any  `json:"params"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		w.Header().Set("Content-Type", "application/json")
		switch req.Method {
		case "eth_blockNumber":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      req.ID,
				"result":  tip,
			})
		case "eth_getBlockByNumber":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      req.ID,
				"result":  map[string]string{"number": finalizedBlock},
			})
		default:
			t.Fatalf("unexpected method %q", req.Method)
		}
	}))
}

func TestRPCChainHead_TipAndFinalized(t *testing.T) {
	srv := rpcStub(t, "0x3e8", "0x3e6") // tip=1000, finalized=998
	defer srv.Close()

	repo := &fakeRepo{
		byChain: map[chain.ChainID][]application.SourceConfig{
			chain.OptimismMainnet: {{
				ID:       "rpc-opt",
				Type:     gateway.TypeRPC,
				ChainID:  chain.OptimismMainnet,
				Endpoint: srv.URL,
				Enabled:  true,
			}},
		},
	}
	head := gateway.NewRPCChainHead(repo)

	tip, err := head.Tip(context.Background(), chain.OptimismMainnet)
	require.NoError(t, err)
	require.Equal(t, chain.BlockNumber(1000), tip)

	finalized, err := head.Finalized(context.Background(), chain.OptimismMainnet)
	require.NoError(t, err)
	require.Equal(t, chain.BlockNumber(998), finalized)
}

func TestRPCChainHead_NoRPCSource(t *testing.T) {
	// Only a blockscout row for the chain → ChainHead errs with the
	// sentinel so the caller can distinguish "misconfigured" from a
	// transient JSON-RPC failure.
	repo := &fakeRepo{
		byChain: map[chain.ChainID][]application.SourceConfig{
			chain.OptimismMainnet: {{
				ID:      "bs-opt",
				Type:    gateway.TypeBlockscout,
				ChainID: chain.OptimismMainnet,
				Enabled: true,
			}},
		},
	}
	head := gateway.NewRPCChainHead(repo)
	_, err := head.Tip(context.Background(), chain.OptimismMainnet)
	require.ErrorIs(t, err, gateway.ErrNoRPCSource)
}

func TestRPCChainHead_RPCError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "down", http.StatusInternalServerError)
	}))
	defer srv.Close()

	repo := &fakeRepo{
		byChain: map[chain.ChainID][]application.SourceConfig{
			chain.OptimismMainnet: {{
				ID:       "rpc-opt",
				Type:     gateway.TypeRPC,
				ChainID:  chain.OptimismMainnet,
				Endpoint: srv.URL,
				Enabled:  true,
			}},
		},
	}
	head := gateway.NewRPCChainHead(repo)
	_, err := head.Tip(context.Background(), chain.OptimismMainnet)
	require.Error(t, err)
}
