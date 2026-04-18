// Package chain holds the primitive value objects that describe a chain
// — ids, block numbers, addresses, hashes, ranges. Every other bounded
// context (source, verification, diff) imports these as the shared
// vocabulary of the chain world.
//
// This package is deliberately dependency-free: standard library only.
// Framework bindings (HTTP, ORMs, RPC clients) live in adapters.
package chain

import "fmt"

// ChainID identifies a blockchain by its EIP-155 numeric id.
// Zero is intentionally not a valid chain.
type ChainID uint64

// Known chains. Extend this list as new adapters gain support.
const (
	EthereumMainnet ChainID = 1
	OptimismMainnet ChainID = 10
)

// Uint64 returns the underlying numeric id (useful for map keys that
// must stay primitive and for JSON-RPC chainid parameters).
func (c ChainID) Uint64() uint64 { return uint64(c) }

// IsKnown reports whether the id is registered in this package. Adapters
// may still accept unknown ids when they have a per-chain override in
// config.
func (c ChainID) IsKnown() bool {
	switch c {
	case EthereumMainnet, OptimismMainnet:
		return true
	}
	return false
}

// Slug returns the lowercase adapter-friendly key for this chain
// ("optimism", "ethereum"). Unknown chains return an empty string so
// callers can fall through to an explicit override path instead of
// silently using a wrong identifier.
func (c ChainID) Slug() string {
	switch c {
	case EthereumMainnet:
		return "ethereum"
	case OptimismMainnet:
		return "optimism"
	}
	return ""
}

// DisplayName returns the chain's human-facing label ("Optimism").
// Unknown chains return an empty string; format them for display with
// String() if you need a placeholder.
func (c ChainID) DisplayName() string {
	switch c {
	case EthereumMainnet:
		return "Ethereum"
	case OptimismMainnet:
		return "Optimism"
	}
	return ""
}

// String satisfies fmt.Stringer. Known chains render as their slug;
// unknown ids fall back to "chain:N" so log lines remain informative
// instead of printing an empty value.
func (c ChainID) String() string {
	if slug := c.Slug(); slug != "" {
		return slug
	}
	return fmt.Sprintf("chain:%d", uint64(c))
}
