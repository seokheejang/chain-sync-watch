package config_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/seokheejang/chain-sync-watch/internal/config"
)

// clearCSWEnv removes every CSW_-prefixed variable so host-side leaks
// do not poison table-driven tests that run in the same process.
func clearCSWEnv(t *testing.T) {
	t.Helper()
	for _, kv := range os.Environ() {
		if i := indexByte(kv, '='); i > 0 && hasPrefix(kv[:i], "CSW_") {
			t.Setenv(kv[:i], "") // t.Setenv auto-reverts on cleanup; empty value is fine for our tests
			_ = os.Unsetenv(kv[:i])
		}
	}
}

func indexByte(s string, b byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return i
		}
	}
	return -1
}

func hasPrefix(s, p string) bool {
	return len(s) >= len(p) && s[:len(p)] == p
}

func TestLoad_DefaultsOnly(t *testing.T) {
	clearCSWEnv(t)

	cfg, err := config.Load(&config.Options{LocalPath: "/nonexistent.yaml"})
	require.NoError(t, err)

	require.Equal(t, ":8080", cfg.Server.Addr)
	require.Equal(t, []string{"http://localhost:3000"}, cfg.Server.CORSOrigins)
	require.Equal(t, 10*time.Second, cfg.Server.ReadTimeout)

	require.Equal(t, "info", cfg.Log.Level)
	require.Equal(t, "json", cfg.Log.Format)

	require.Len(t, cfg.Chains, 1)
	require.Equal(t, uint64(10), cfg.Chains[0].ID)
	require.Equal(t, "optimism", cfg.Chains[0].Slug)
	require.Equal(t, "Optimism", cfg.Chains[0].DisplayName)

	require.True(t, cfg.Adapters.RPC.Enabled)
	require.Equal(t, "https://optimism-rpc.publicnode.com", cfg.Adapters.RPC.Endpoints[10])
	require.False(t, cfg.Adapters.RPC.Archive)
	require.Equal(t, 10, cfg.Adapters.RPC.RateLimitRPS)

	require.True(t, cfg.Adapters.Blockscout.Enabled)
	require.Equal(t, "https://optimism.blockscout.com", cfg.Adapters.Blockscout.Endpoints[10])

	require.False(t, cfg.Adapters.Etherscan.Enabled)
	require.Equal(t, "https://api.etherscan.io/v2/api", cfg.Adapters.Etherscan.BaseURL)

	require.False(t, cfg.RawResponse.Persist)
}

func TestLoad_LocalOverride(t *testing.T) {
	clearCSWEnv(t)

	dir := t.TempDir()
	local := filepath.Join(dir, "local.yaml")
	require.NoError(t, os.WriteFile(local, []byte(`
server:
  addr: ":9999"
log:
  level: debug
adapters:
  rpc:
    archive: true
    rate_limit_rps: 42
    endpoints:
      10: "https://override.example.com"
`), 0o644))

	cfg, err := config.Load(&config.Options{LocalPath: local})
	require.NoError(t, err)

	// Overridden keys.
	require.Equal(t, ":9999", cfg.Server.Addr)
	require.Equal(t, "debug", cfg.Log.Level)
	require.True(t, cfg.Adapters.RPC.Archive)
	require.Equal(t, 42, cfg.Adapters.RPC.RateLimitRPS)
	require.Equal(t, "https://override.example.com", cfg.Adapters.RPC.Endpoints[10])

	// Non-overridden keys should keep defaults.
	require.Equal(t, "json", cfg.Log.Format)
	require.Equal(t, 10*time.Second, cfg.Server.ReadTimeout)
	require.Len(t, cfg.Chains, 1)
}

func TestLoad_EnvOverridesLocal(t *testing.T) {
	clearCSWEnv(t)

	dir := t.TempDir()
	local := filepath.Join(dir, "local.yaml")
	require.NoError(t, os.WriteFile(local, []byte(`
server:
  addr: ":7777"
log:
  level: debug
`), 0o644))

	t.Setenv("CSW_SERVER__ADDR", ":8888")
	t.Setenv("CSW_LOG__LEVEL", "warn")
	t.Setenv("CSW_ADAPTERS__RPC__ENDPOINTS__10", "https://env-rpc.example.com")
	t.Setenv("CSW_ADAPTERS__RPC__RATE_LIMIT_RPS", "99")

	cfg, err := config.Load(&config.Options{LocalPath: local})
	require.NoError(t, err)

	require.Equal(t, ":8888", cfg.Server.Addr, "env must beat local file")
	require.Equal(t, "warn", cfg.Log.Level)
	require.Equal(t, "https://env-rpc.example.com", cfg.Adapters.RPC.Endpoints[10])
	require.Equal(t, 99, cfg.Adapters.RPC.RateLimitRPS)
}

func TestLoad_ValidationFailures(t *testing.T) {
	clearCSWEnv(t)

	cases := []struct {
		name    string
		envs    map[string]string
		wantSub string
	}{
		{
			name:    "empty server addr",
			envs:    map[string]string{"CSW_SERVER__ADDR": ""},
			wantSub: "server.addr",
		},
		{
			name:    "invalid log level",
			envs:    map[string]string{"CSW_LOG__LEVEL": "trace"},
			wantSub: "log.level",
		},
		{
			name:    "invalid log format",
			envs:    map[string]string{"CSW_LOG__FORMAT": "pretty"},
			wantSub: "log.format",
		},
		{
			name: "etherscan enabled without api key",
			envs: map[string]string{
				"CSW_ADAPTERS__ETHERSCAN__ENABLED": "true",
				"CSW_ADAPTERS__ETHERSCAN__API_KEY": "",
			},
			wantSub: "etherscan",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			clearCSWEnv(t)
			for k, v := range tc.envs {
				t.Setenv(k, v)
			}
			_, err := config.Load(&config.Options{LocalPath: "/nonexistent.yaml"})
			require.Error(t, err)
			require.Contains(t, err.Error(), tc.wantSub)
		})
	}
}

func TestLoad_EtherscanEnabledWithKey(t *testing.T) {
	clearCSWEnv(t)

	t.Setenv("CSW_ADAPTERS__ETHERSCAN__ENABLED", "true")
	t.Setenv("CSW_ADAPTERS__ETHERSCAN__API_KEY", "some-key")

	cfg, err := config.Load(&config.Options{LocalPath: "/nonexistent.yaml"})
	require.NoError(t, err)
	require.True(t, cfg.Adapters.Etherscan.Enabled)
	require.Equal(t, "some-key", cfg.Adapters.Etherscan.APIKey)
}
