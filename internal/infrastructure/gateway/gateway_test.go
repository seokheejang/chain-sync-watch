package gateway_test

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/seokheejang/chain-sync-watch/internal/application"
	"github.com/seokheejang/chain-sync-watch/internal/chain"
	"github.com/seokheejang/chain-sync-watch/internal/infrastructure/gateway"
	"github.com/seokheejang/chain-sync-watch/internal/secrets"
	"github.com/seokheejang/chain-sync-watch/internal/source"
)

func TestRegistry_Build_UnknownType(t *testing.T) {
	reg := gateway.DefaultRegistry()
	_, err := reg.Build(application.SourceConfig{
		ID:      "weird",
		Type:    "not-a-real-adapter",
		ChainID: chain.OptimismMainnet,
	}, nil)
	require.ErrorIs(t, err, gateway.ErrUnknownType)
}

func TestRegistry_Build_AliasesSourceID(t *testing.T) {
	// rpc.Adapter's const ID is "rpc"; the Registry must override
	// that with cfg.ID so ForChain results are distinguishable when
	// the same adapter type is configured for multiple chains.
	reg := gateway.DefaultRegistry()
	cfg := application.SourceConfig{
		ID:       "rpc-optimism",
		Type:     gateway.TypeRPC,
		ChainID:  chain.OptimismMainnet,
		Endpoint: "https://optimism-rpc.publicnode.com",
	}
	s, err := reg.Build(cfg, nil)
	require.NoError(t, err)
	require.Equal(t, source.SourceID("rpc-optimism"), s.ID())
	require.Equal(t, chain.OptimismMainnet, s.ChainID())
}

func TestRegistry_Build_EachBundledAdapter(t *testing.T) {
	reg := gateway.DefaultRegistry()
	cases := []struct {
		name string
		typ  string
	}{
		{"rpc", gateway.TypeRPC},
		{"blockscout", gateway.TypeBlockscout},
		{"routescan", gateway.TypeRoutescan},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := application.SourceConfig{
				ID:       tc.typ + "-opt",
				Type:     tc.typ,
				ChainID:  chain.OptimismMainnet,
				Endpoint: "https://example.com",
			}
			s, err := reg.Build(cfg, nil)
			require.NoError(t, err)
			require.Equal(t, source.SourceID(tc.typ+"-opt"), s.ID())
		})
	}
}

func TestRegistry_Build_RPCArchiveOption(t *testing.T) {
	// The factory must read the `archive` bool option and propagate
	// it to rpc.WithArchive. Historical-state capabilities only
	// switch on when the operator explicitly opts in.
	reg := gateway.DefaultRegistry()
	cfg := application.SourceConfig{
		ID:       "rpc-archive",
		Type:     gateway.TypeRPC,
		ChainID:  chain.OptimismMainnet,
		Endpoint: "https://archive.example.com",
		Options:  map[string]any{"archive": true},
	}
	s, err := reg.Build(cfg, nil)
	require.NoError(t, err)
	require.True(t, s.Supports(source.CapBalanceAtBlock), "archive rpc must advertise BalanceAtBlock")
}

// fakeRepo backs DBGateway tests without a live DB.
type fakeRepo struct {
	byChain  map[chain.ChainID][]application.SourceConfig
	byID     map[string]application.SourceConfig
	listErr  error
	findMiss bool
}

func (f *fakeRepo) Save(context.Context, application.SourceConfig) error { return nil }
func (f *fakeRepo) Delete(context.Context, string) error                 { return nil }
func (f *fakeRepo) FindByID(_ context.Context, id string) (*application.SourceConfig, error) {
	if f.findMiss {
		return nil, application.ErrSourceNotFound
	}
	cfg, ok := f.byID[id]
	if !ok {
		return nil, application.ErrSourceNotFound
	}
	return &cfg, nil
}

func (f *fakeRepo) ListByChain(_ context.Context, c chain.ChainID, _ bool) ([]application.SourceConfig, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.byChain[c], nil
}

func testCipher(t *testing.T) *secrets.Cipher {
	t.Helper()
	c, err := secrets.NewCipher(bytes.Repeat([]byte{0xAB}, 32))
	require.NoError(t, err)
	return c
}

func TestDBGateway_ForChain_ReturnsAliasedSources(t *testing.T) {
	repo := &fakeRepo{
		byChain: map[chain.ChainID][]application.SourceConfig{
			chain.OptimismMainnet: {
				{ID: "rpc-opt", Type: gateway.TypeRPC, ChainID: chain.OptimismMainnet, Endpoint: "https://rpc.example", Enabled: true},
				{ID: "bs-opt", Type: gateway.TypeBlockscout, ChainID: chain.OptimismMainnet, Endpoint: "https://bs.example", Enabled: true},
			},
		},
	}
	g := gateway.NewDBGateway(repo, nil, nil)
	sources, err := g.ForChain(chain.OptimismMainnet)
	require.NoError(t, err)
	require.Len(t, sources, 2)

	ids := []source.SourceID{sources[0].ID(), sources[1].ID()}
	require.ElementsMatch(t, ids, []source.SourceID{"rpc-opt", "bs-opt"})
}

func TestDBGateway_ForChain_ListError(t *testing.T) {
	repo := &fakeRepo{listErr: errors.New("db down")}
	g := gateway.NewDBGateway(repo, nil, nil)
	_, err := g.ForChain(chain.OptimismMainnet)
	require.Error(t, err)
}

func TestDBGateway_ForChain_DecryptsSecret(t *testing.T) {
	cipher := testCipher(t)
	ct, nonce, err := cipher.Encrypt([]byte("abcd-1234"))
	require.NoError(t, err)

	repo := &fakeRepo{
		byChain: map[chain.ChainID][]application.SourceConfig{
			chain.OptimismMainnet: {{
				ID:               "rpc-opt",
				Type:             gateway.TypeRPC,
				ChainID:          chain.OptimismMainnet,
				Endpoint:         "https://rpc.example",
				SecretCiphertext: ct,
				SecretNonce:      nonce,
				Enabled:          true,
			}},
		},
	}
	g := gateway.NewDBGateway(repo, cipher, nil)
	sources, err := g.ForChain(chain.OptimismMainnet)
	require.NoError(t, err)
	require.Len(t, sources, 1)
}

func TestDBGateway_ForChain_EncryptedButNoCipher(t *testing.T) {
	repo := &fakeRepo{
		byChain: map[chain.ChainID][]application.SourceConfig{
			chain.OptimismMainnet: {{
				ID:               "rpc-opt",
				Type:             gateway.TypeRPC,
				ChainID:          chain.OptimismMainnet,
				Endpoint:         "https://rpc.example",
				SecretCiphertext: []byte{1, 2, 3},
				SecretNonce:      []byte{4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15},
				Enabled:          true,
			}},
		},
	}
	g := gateway.NewDBGateway(repo, nil, nil)
	_, err := g.ForChain(chain.OptimismMainnet)
	require.Error(t, err)
	require.Contains(t, err.Error(), "cipher is nil")
}

func TestDBGateway_ForChain_TamperedSecret(t *testing.T) {
	cipher := testCipher(t)
	ct, nonce, err := cipher.Encrypt([]byte("abcd"))
	require.NoError(t, err)
	tampered := make([]byte, len(ct))
	copy(tampered, ct)
	tampered[0] ^= 0xFF

	repo := &fakeRepo{
		byChain: map[chain.ChainID][]application.SourceConfig{
			chain.OptimismMainnet: {{
				ID:               "rpc-opt",
				Type:             gateway.TypeRPC,
				ChainID:          chain.OptimismMainnet,
				Endpoint:         "https://rpc.example",
				SecretCiphertext: tampered,
				SecretNonce:      nonce,
				Enabled:          true,
			}},
		},
	}
	g := gateway.NewDBGateway(repo, cipher, nil)
	_, err = g.ForChain(chain.OptimismMainnet)
	require.Error(t, err)
	require.Contains(t, err.Error(), "decrypt")
}

func TestDBGateway_Get_NotFound(t *testing.T) {
	repo := &fakeRepo{findMiss: true}
	g := gateway.NewDBGateway(repo, nil, nil)
	_, err := g.Get("missing")
	require.ErrorIs(t, err, application.ErrSourceNotFound)
}

func TestDBGateway_Get_Resolves(t *testing.T) {
	repo := &fakeRepo{
		byID: map[string]application.SourceConfig{
			"rpc-opt": {ID: "rpc-opt", Type: gateway.TypeRPC, ChainID: chain.OptimismMainnet, Endpoint: "https://rpc", Enabled: true},
		},
	}
	g := gateway.NewDBGateway(repo, nil, nil)
	s, err := g.Get("rpc-opt")
	require.NoError(t, err)
	require.Equal(t, source.SourceID("rpc-opt"), s.ID())
}
