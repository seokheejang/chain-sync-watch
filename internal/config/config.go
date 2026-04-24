// Package config loads configuration from an embedded default YAML, an
// optional on-disk override, and environment variables.
//
// Precedence (later wins):
//  1. Embedded defaults (defaults.yaml, compiled into the binary)
//  2. Optional local override file (configs/config.local.yaml, gitignored)
//  3. Environment variables with the CSW_ prefix
//
// Nested keys in env vars are separated by double underscore ("__"); a
// single underscore stays literal inside a key segment so keys like
// "rate_limit_rps" remain addressable:
//
//	CSW_SERVER__ADDR                    -> server.addr
//	CSW_ADAPTERS__RPC__RATE_LIMIT_RPS   -> adapters.rpc.rate_limit_rps
//	CSW_ADAPTERS__RPC__ENDPOINTS__10    -> adapters.rpc.endpoints.10
package config

import (
	_ "embed"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/env"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/providers/rawbytes"
	"github.com/knadh/koanf/v2"
)

//go:embed defaults.yaml
var defaultYAML []byte

const (
	// EnvPrefix identifies environment variables that override config.
	EnvPrefix = "CSW_"
	// EnvPathSep separates nested keys inside an env var name.
	EnvPathSep = "__"
	// DefaultLocalFile is the optional override file relative to the cwd.
	DefaultLocalFile = "configs/config.local.yaml"
)

// Config is the fully-resolved configuration tree.
type Config struct {
	Server       ServerConfig       `koanf:"server"`
	Worker       WorkerConfig       `koanf:"worker"`
	Log          LogConfig          `koanf:"log"`
	Chains       []ChainConfig      `koanf:"chains"`
	Adapters     AdaptersConfig     `koanf:"adapters"`
	Verification VerificationConfig `koanf:"verification"`
	RawResponse  RawResponseConfig  `koanf:"raw_response"`
	Retention    RetentionConfig    `koanf:"retention"`
}

type ServerConfig struct {
	Addr          string        `koanf:"addr"`
	CORSOrigins   []string      `koanf:"cors_origins"`
	ReadTimeout   time.Duration `koanf:"read_timeout"`
	WriteTimeout  time.Duration `koanf:"write_timeout"`
	ShutdownGrace time.Duration `koanf:"shutdown_grace"`
}

type WorkerConfig struct {
	Concurrency   int           `koanf:"concurrency"`
	HealthAddr    string        `koanf:"health_addr"`
	ShutdownGrace time.Duration `koanf:"shutdown_grace"`
}

type LogConfig struct {
	Level  string `koanf:"level"`
	Format string `koanf:"format"`
}

type ChainConfig struct {
	ID          uint64 `koanf:"id"`
	Slug        string `koanf:"slug"`
	DisplayName string `koanf:"display_name"`
}

type AdaptersConfig struct {
	RPC        RPCAdapterConfig        `koanf:"rpc"`
	Blockscout BlockscoutAdapterConfig `koanf:"blockscout"`
	Etherscan  EtherscanAdapterConfig  `koanf:"etherscan"`
}

// EndpointMap is a per-chain endpoint lookup keyed by numeric chain ID.
type EndpointMap = map[uint64]string

type RPCAdapterConfig struct {
	Enabled      bool          `koanf:"enabled"`
	Endpoints    EndpointMap   `koanf:"endpoints"`
	Archive      bool          `koanf:"archive"`
	RateLimitRPS int           `koanf:"rate_limit_rps"`
	Timeout      time.Duration `koanf:"timeout"`
	MaxRetries   int           `koanf:"max_retries"`
}

type BlockscoutAdapterConfig struct {
	Enabled      bool          `koanf:"enabled"`
	Endpoints    EndpointMap   `koanf:"endpoints"`
	RateLimitRPS int           `koanf:"rate_limit_rps"`
	Timeout      time.Duration `koanf:"timeout"`
	MaxRetries   int           `koanf:"max_retries"`
}

type EtherscanAdapterConfig struct {
	Enabled      bool          `koanf:"enabled"`
	BaseURL      string        `koanf:"base_url"`
	APIKey       string        `koanf:"api_key"`
	RateLimitRPS int           `koanf:"rate_limit_rps"`
	Timeout      time.Duration `koanf:"timeout"`
	MaxRetries   int           `koanf:"max_retries"`
}

type VerificationConfig struct {
	MaxConcurrentPerSource int           `koanf:"max_concurrent_per_source"`
	DefaultTimeout         time.Duration `koanf:"default_timeout"`
}

type RawResponseConfig struct {
	Persist bool `koanf:"persist"`
}

// RetentionConfig controls the worker's housekeeping sweep. RunsDays
// is the age threshold (0 = disable the sweep); CronExpr is the 5-
// field asynq spec that fires the maintenance task.
type RetentionConfig struct {
	RunsDays int    `koanf:"runs_days"`
	CronExpr string `koanf:"cron_expr"`
}

// Options tunes how Load resolves configuration. Nil uses production
// defaults; tests pass LocalPath to point at fixtures.
type Options struct {
	// LocalPath overrides the local YAML override location. Empty string
	// (zero value) means "use DefaultLocalFile"; pass a non-existent path
	// to explicitly disable the local layer.
	LocalPath string
}

// Load resolves configuration from the three layers in precedence order
// and returns the validated Config.
func Load(opts *Options) (*Config, error) {
	if opts == nil {
		opts = &Options{}
	}

	k := koanf.New(".")

	// Layer 1: embedded defaults.
	if err := k.Load(rawbytes.Provider(defaultYAML), yaml.Parser()); err != nil {
		return nil, fmt.Errorf("load embedded defaults: %w", err)
	}

	// Layer 2: optional local override file.
	localPath := opts.LocalPath
	if localPath == "" {
		localPath = DefaultLocalFile
	}
	if _, err := os.Stat(localPath); err == nil {
		if err := k.Load(file.Provider(localPath), yaml.Parser()); err != nil {
			return nil, fmt.Errorf("load local override %q: %w", localPath, err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("stat local override %q: %w", localPath, err)
	}

	// Layer 3: environment overrides.
	provider := env.Provider(EnvPrefix, ".", envKeyToPath)
	if err := k.Load(provider, nil); err != nil {
		return nil, fmt.Errorf("load env: %w", err)
	}

	var cfg Config
	if err := k.Unmarshal("", &cfg); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}
	return &cfg, nil
}

// envKeyToPath converts an environment variable name into a dotted
// koanf path. "__" becomes ".", single "_" is preserved literally.
func envKeyToPath(name string) string {
	trimmed := strings.TrimPrefix(name, EnvPrefix)
	lowered := strings.ToLower(trimmed)
	return strings.ReplaceAll(lowered, EnvPathSep, ".")
}

// Validate reports invariants that must hold regardless of how the
// config was assembled.
func (c *Config) Validate() error {
	if c.Server.Addr == "" {
		return errors.New("server.addr must not be empty")
	}
	if len(c.Chains) == 0 {
		return errors.New("chains must not be empty")
	}
	seen := make(map[uint64]bool, len(c.Chains))
	for i, ch := range c.Chains {
		if ch.ID == 0 {
			return fmt.Errorf("chains[%d].id must be > 0", i)
		}
		if ch.Slug == "" {
			return fmt.Errorf("chains[%d].slug must not be empty", i)
		}
		if seen[ch.ID] {
			return fmt.Errorf("chains[%d]: duplicate id %d", i, ch.ID)
		}
		seen[ch.ID] = true
	}
	switch c.Log.Level {
	case "debug", "info", "warn", "error":
	default:
		return fmt.Errorf("log.level must be one of debug|info|warn|error, got %q", c.Log.Level)
	}
	switch c.Log.Format {
	case "json", "text":
	default:
		return fmt.Errorf("log.format must be json|text, got %q", c.Log.Format)
	}
	if c.Adapters.Etherscan.Enabled && c.Adapters.Etherscan.APIKey == "" {
		return errors.New("adapters.etherscan.enabled requires api_key to be set")
	}
	return nil
}
