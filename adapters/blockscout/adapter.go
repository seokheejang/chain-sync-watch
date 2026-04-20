// Package blockscout implements source.Source against a Blockscout
// instance. REST v2 is the primary path; we fall back to the
// Etherscan-compat proxy module (?module=proxy&action=eth_*) for the
// block fields REST v2 does not expose (state/receipts/transactions
// roots).
package blockscout

import (
	"errors"
	"fmt"
	"strings"

	"github.com/seokheejang/chain-sync-watch/adapters/internal/ethscan"
	"github.com/seokheejang/chain-sync-watch/adapters/internal/httpx"
	"github.com/seokheejang/chain-sync-watch/internal/chain"
	"github.com/seokheejang/chain-sync-watch/internal/source"
)

// ID is the SourceID every blockscout.Adapter reports.
const ID source.SourceID = "blockscout"

// Adapter is the Blockscout source implementation.
type Adapter struct {
	chainID chain.ChainID
	base    string // trimmed of trailing slash; REST v2 appends /api/v2/...
	hc      *httpx.Client
	proxy   *ethscan.Client // shares the base URL, served via /api
}

// Option configures an Adapter at construction time.
type Option func(*Adapter)

// WithBaseURL overrides the default per-chain URL (e.g., to point at
// a private mirror).
func WithBaseURL(url string) Option {
	return func(a *Adapter) { a.base = strings.TrimRight(url, "/") }
}

// WithHTTPX replaces the shared HTTP client used for REST v2 and the
// Etherscan-compat proxy. Useful for a per-adapter rate limiter or
// a test transport.
func WithHTTPX(hc *httpx.Client) Option {
	return func(a *Adapter) {
		if hc != nil {
			a.hc = hc
			a.proxy = ethscan.New(a.base+"/api", ethscan.WithHTTPX(hc))
		}
	}
}

// New builds a Blockscout adapter. Callers may omit a URL; the
// per-chain default in DefaultBaseURL is used.
func New(chainID chain.ChainID, opts ...Option) (*Adapter, error) {
	if chainID == 0 {
		return nil, errors.New("blockscout: chain id is required")
	}
	a := &Adapter{chainID: chainID}

	if def, ok := DefaultBaseURL[chainID]; ok {
		a.base = strings.TrimRight(def, "/")
	}
	// Apply options first so WithBaseURL can rewrite before we wire
	// the http client + proxy client off the final base.
	for _, opt := range opts {
		opt(a)
	}
	if a.base == "" {
		return nil, fmt.Errorf("blockscout: no base URL for chain %d; pass WithBaseURL", chainID)
	}
	if a.hc == nil {
		a.hc = httpx.New()
	}
	if a.proxy == nil {
		a.proxy = ethscan.New(a.base+"/api", ethscan.WithHTTPX(a.hc))
	}
	return a, nil
}

func (a *Adapter) ID() source.SourceID    { return ID }
func (a *Adapter) ChainID() chain.ChainID { return a.chainID }

// Supports mirrors the plan's Capability matrix for Blockscout. The
// one genuinely unsupported path is CapERC20HoldingsAtLatest at a
// numeric anchor — the REST endpoint is latest-only and we refuse
// to silently answer with wrong-block data.
func (a *Adapter) Supports(c source.Capability) bool {
	switch c {
	case source.CapBlockHash,
		source.CapBlockParentHash,
		source.CapBlockTimestamp,
		source.CapBlockTxCount,
		source.CapBlockGasUsed,
		source.CapBlockMiner,
		source.CapBlockStateRoot,
		source.CapBlockReceiptsRoot,
		source.CapBlockTransactionsRoot:
		return true

	case source.CapBalanceAtLatest,
		source.CapNonceAtLatest,
		source.CapTxCountAtLatest,
		source.CapBalanceAtBlock,
		source.CapNonceAtBlock:
		return true

	case source.CapERC20BalanceAtLatest,
		source.CapERC20HoldingsAtLatest:
		return true

	case source.CapInternalTxByTx:
		return true
	case source.CapInternalTxByBlock:
		// Blockscout REST v2 exposes internal txs per address and per
		// transaction, not per block. Callers aggregate by tx.
		return false

	case source.CapTotalAddressCount,
		source.CapTotalTxCount,
		source.CapTotalContractCount,
		source.CapERC20TokenCount:
		return true
	}
	return false
}
