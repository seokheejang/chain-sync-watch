package routes_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/seokheejang/chain-sync-watch/internal/application/testsupport"
	"github.com/seokheejang/chain-sync-watch/internal/chain"
	"github.com/seokheejang/chain-sync-watch/internal/infrastructure/httpapi"
	"github.com/seokheejang/chain-sync-watch/internal/infrastructure/httpapi/routes"
	"github.com/seokheejang/chain-sync-watch/internal/source"
	"github.com/seokheejang/chain-sync-watch/internal/source/fake"
)

func newSourcesFixture(t *testing.T, sources ...source.Source) *httptest.Server {
	t.Helper()
	gw := testsupport.NewFakeSourceGateway()
	for _, s := range sources {
		gw.Register(s)
	}
	srv := httpapi.NewServer(httpapi.Config{}, httpapi.Deps{
		Sources: routes.SourcesDeps{Gateway: gw},
	})
	ts := httptest.NewServer(srv.Handler)
	t.Cleanup(ts.Close)
	return ts
}

func TestListSources_EmptyChain_ReturnsZero(t *testing.T) {
	ts := newSourcesFixture(t)
	resp, err := http.Get(ts.URL + "/sources?chain_id=10")
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var body struct {
		Items []any `json:"items"`
		Total int   `json:"total"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	require.Equal(t, 0, body.Total)
}

func TestListSources_ReturnsCapabilityMatrix(t *testing.T) {
	rpc := fake.New("rpc", chain.OptimismMainnet,
		fake.WithCapabilities(source.CapBlockHash, source.CapBalanceAtBlock),
	)
	bs := fake.New("blockscout", chain.OptimismMainnet,
		fake.WithCapabilities(source.CapERC20HoldingsAtLatest, source.CapBlockHash),
	)
	ts := newSourcesFixture(t, rpc, bs)

	resp, err := http.Get(ts.URL + "/sources?chain_id=10")
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var body struct {
		Items []struct {
			ID           string `json:"id"`
			ChainID      uint64 `json:"chain_id"`
			Capabilities []struct {
				Name string `json:"name"`
				Tier string `json:"tier"`
			} `json:"capabilities"`
		} `json:"items"`
		Total int `json:"total"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	require.Equal(t, 2, body.Total)

	// First item is whichever the gateway registered first (rpc).
	require.Equal(t, "rpc", body.Items[0].ID)
	require.Equal(t, uint64(10), body.Items[0].ChainID)
	rpcCaps := map[string]string{}
	for _, c := range body.Items[0].Capabilities {
		rpcCaps[c.Name] = c.Tier
	}
	require.Equal(t, "A", rpcCaps["block.hash"])
	require.Equal(t, "A", rpcCaps["address.balance_at_block"])

	bsCaps := map[string]string{}
	for _, c := range body.Items[1].Capabilities {
		bsCaps[c.Name] = c.Tier
	}
	require.Equal(t, "B", bsCaps["address.erc20_holdings_at_latest"])
}

func TestListSources_MissingChainID_Returns422(t *testing.T) {
	ts := newSourcesFixture(t)
	// required:"true" on chain_id — missing param is caught by the
	// huma schema validator.
	resp, err := http.Get(ts.URL + "/sources")
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusUnprocessableEntity, resp.StatusCode)
}
