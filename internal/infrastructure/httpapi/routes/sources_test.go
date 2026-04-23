package routes_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/seokheejang/chain-sync-watch/internal/application"
	"github.com/seokheejang/chain-sync-watch/internal/application/testsupport"
	"github.com/seokheejang/chain-sync-watch/internal/chain"
	"github.com/seokheejang/chain-sync-watch/internal/infrastructure/httpapi"
	"github.com/seokheejang/chain-sync-watch/internal/infrastructure/httpapi/routes"
	"github.com/seokheejang/chain-sync-watch/internal/secrets"
	"github.com/seokheejang/chain-sync-watch/internal/source"
	"github.com/seokheejang/chain-sync-watch/internal/source/fake"
)

// fakeSourceRepo is the in-memory SourceConfigRepository the CRUD
// tests drive. Mirrors the gorm SourceRepo behaviour for the rules
// the handlers care about (ErrSourceNotFound, duplicate Save OK,
// ListByChain filter). Not thread-safe — tests are serial.
type fakeSourceRepo struct {
	rows map[string]application.SourceConfig
}

func newFakeSourceRepo() *fakeSourceRepo {
	return &fakeSourceRepo{rows: map[string]application.SourceConfig{}}
}

func (f *fakeSourceRepo) Save(_ context.Context, s application.SourceConfig) error {
	f.rows[s.ID] = s
	return nil
}

func (f *fakeSourceRepo) FindByID(_ context.Context, id string) (*application.SourceConfig, error) {
	cfg, ok := f.rows[id]
	if !ok {
		return nil, application.ErrSourceNotFound
	}
	return &cfg, nil
}

func (f *fakeSourceRepo) ListByChain(_ context.Context, cid chain.ChainID, enabledOnly bool) ([]application.SourceConfig, error) {
	out := make([]application.SourceConfig, 0)
	for _, r := range f.rows {
		if r.ChainID != cid {
			continue
		}
		if enabledOnly && !r.Enabled {
			continue
		}
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Type < out[j].Type })
	return out, nil
}

func (f *fakeSourceRepo) Delete(_ context.Context, id string) error {
	delete(f.rows, id)
	return nil
}

type sourcesFixture struct {
	ts      *httptest.Server
	repo    *fakeSourceRepo
	gateway *testsupport.FakeSourceGateway
}

func newSourcesFixture(t *testing.T, cipher *secrets.Cipher, gatewaySources ...source.Source) *sourcesFixture {
	t.Helper()
	repo := newFakeSourceRepo()
	gw := testsupport.NewFakeSourceGateway()
	for _, s := range gatewaySources {
		gw.Register(s)
	}
	srv := httpapi.NewServer(httpapi.Config{}, httpapi.Deps{
		Sources: routes.SourcesDeps{
			Repo:    repo,
			Gateway: gw,
			Cipher:  cipher,
		},
	})
	ts := httptest.NewServer(srv.Handler)
	t.Cleanup(ts.Close)
	return &sourcesFixture{ts: ts, repo: repo, gateway: gw}
}

func testCipher(t *testing.T) *secrets.Cipher {
	t.Helper()
	c, err := secrets.NewCipher(bytes.Repeat([]byte{0xCD}, 32))
	require.NoError(t, err)
	return c
}

func TestSources_ListEmpty_ReturnsZero(t *testing.T) {
	fx := newSourcesFixture(t, nil)
	resp, err := http.Get(fx.ts.URL + "/sources?chain_id=10")
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

func TestSources_ListReturnsRows(t *testing.T) {
	fx := newSourcesFixture(t, nil)
	fx.repo.rows["rpc-10"] = application.SourceConfig{
		ID:        "rpc-10",
		Type:      "rpc",
		ChainID:   chain.OptimismMainnet,
		Endpoint:  "https://rpc.example",
		Options:   map[string]any{"archive": true},
		Enabled:   true,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	fx.repo.rows["blockscout-10"] = application.SourceConfig{
		ID:      "blockscout-10",
		Type:    "blockscout",
		ChainID: chain.OptimismMainnet,
		Enabled: false,
	}

	resp, err := http.Get(fx.ts.URL + "/sources?chain_id=10")
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var body struct {
		Items []struct {
			ID        string `json:"id"`
			Type      string `json:"type"`
			Endpoint  string `json:"endpoint"`
			Enabled   bool   `json:"enabled"`
			HasSecret bool   `json:"has_secret"`
		} `json:"items"`
		Total int `json:"total"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	require.Equal(t, 2, body.Total)
	require.Equal(t, "blockscout-10", body.Items[0].ID)
	require.False(t, body.Items[0].Enabled)
	require.Equal(t, "rpc-10", body.Items[1].ID)
}

func TestSources_MissingChainID_Returns422(t *testing.T) {
	fx := newSourcesFixture(t, nil)
	resp, err := http.Get(fx.ts.URL + "/sources")
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusUnprocessableEntity, resp.StatusCode)
}

func TestSources_Create_WithoutAPIKey(t *testing.T) {
	fx := newSourcesFixture(t, nil)
	body := map[string]any{
		"type":     "rpc",
		"chain_id": 10,
		"endpoint": "https://rpc.example",
		"options":  map[string]any{"archive": true},
	}
	payload, _ := json.Marshal(body)
	resp, err := http.Post(fx.ts.URL+"/sources", "application/json", bytes.NewReader(payload))
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusCreated, resp.StatusCode)

	var got struct {
		ID        string `json:"id"`
		HasSecret bool   `json:"has_secret"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&got))
	require.Equal(t, "rpc-10", got.ID)
	require.False(t, got.HasSecret)
	require.Contains(t, fx.repo.rows, "rpc-10")
}

func TestSources_Create_WithAPIKey_EncryptsAtRest(t *testing.T) {
	c := testCipher(t)
	fx := newSourcesFixture(t, c)
	body := map[string]any{
		"type":     "blockscout",
		"chain_id": 10,
		"endpoint": "https://bs.example",
		"api_key":  "plaintext-key",
	}
	payload, _ := json.Marshal(body)
	resp, err := http.Post(fx.ts.URL+"/sources", "application/json", bytes.NewReader(payload))
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusCreated, resp.StatusCode)

	// Plaintext must not appear in the stored row — only ciphertext.
	stored := fx.repo.rows["blockscout-10"]
	require.True(t, stored.HasSecret())
	require.NotContains(t, string(stored.SecretCiphertext), "plaintext-key")

	// Round-trip decrypt works.
	plain, err := c.Decrypt(stored.SecretCiphertext, stored.SecretNonce)
	require.NoError(t, err)
	require.Equal(t, "plaintext-key", string(plain))

	// Response also never leaks ciphertext — HasSecret is the only
	// clue to the operator.
	var got struct {
		HasSecret bool `json:"has_secret"`
		APIKey    any  `json:"api_key"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&got)
	require.True(t, got.HasSecret)
	require.Nil(t, got.APIKey)
}

func TestSources_Create_APIKey_NoCipher_Returns400(t *testing.T) {
	fx := newSourcesFixture(t, nil) // cipher intentionally nil
	body := map[string]any{
		"type":     "blockscout",
		"chain_id": 10,
		"endpoint": "https://bs.example",
		"api_key":  "would-be-secret",
	}
	payload, _ := json.Marshal(body)
	resp, err := http.Post(fx.ts.URL+"/sources", "application/json", bytes.NewReader(payload))
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestSources_Update_ClearSecret(t *testing.T) {
	c := testCipher(t)
	fx := newSourcesFixture(t, c)
	ct, nonce, err := c.Encrypt([]byte("existing"))
	require.NoError(t, err)
	fx.repo.rows["blockscout-10"] = application.SourceConfig{
		ID:               "blockscout-10",
		Type:             "blockscout",
		ChainID:          chain.OptimismMainnet,
		Endpoint:         "https://bs.example",
		SecretCiphertext: ct,
		SecretNonce:      nonce,
		Enabled:          true,
	}

	payload, _ := json.Marshal(map[string]any{"clear_secret": true})
	req, _ := http.NewRequest(http.MethodPut, fx.ts.URL+"/sources/blockscout-10", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	updated := fx.repo.rows["blockscout-10"]
	require.False(t, updated.HasSecret())
}

func TestSources_GetNotFound_Returns404(t *testing.T) {
	fx := newSourcesFixture(t, nil)
	resp, err := http.Get(fx.ts.URL + "/sources/missing-id")
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestSources_Delete_Idempotent(t *testing.T) {
	fx := newSourcesFixture(t, nil)
	fx.repo.rows["rpc-10"] = application.SourceConfig{ID: "rpc-10", Type: "rpc", ChainID: chain.OptimismMainnet}

	req, _ := http.NewRequest(http.MethodDelete, fx.ts.URL+"/sources/rpc-10", nil)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusNoContent, resp.StatusCode)
	require.NotContains(t, fx.repo.rows, "rpc-10")

	// Second delete still 204 — handler does not error on missing.
	req2, _ := http.NewRequest(http.MethodDelete, fx.ts.URL+"/sources/rpc-10", nil)
	resp2, err := http.DefaultClient.Do(req2)
	require.NoError(t, err)
	defer resp2.Body.Close()
	require.Equal(t, http.StatusNoContent, resp2.StatusCode)
}

func TestSources_Capabilities_ViaGateway(t *testing.T) {
	rpc := fake.New("rpc-10", chain.OptimismMainnet,
		fake.WithCapabilities(source.CapBlockHash, source.CapBalanceAtBlock),
	)
	fx := newSourcesFixture(t, nil, rpc)

	resp, err := http.Get(fx.ts.URL + "/sources/rpc-10/capabilities")
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var body struct {
		ID           string `json:"id"`
		Capabilities []struct {
			Name string `json:"name"`
			Tier string `json:"tier"`
		} `json:"capabilities"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	require.Equal(t, "rpc-10", body.ID)
	names := make([]string, 0, len(body.Capabilities))
	for _, c := range body.Capabilities {
		names = append(names, c.Name)
	}
	require.Contains(t, names, "block.hash")
	require.Contains(t, names, "address.balance_at_block")
}
