// Package routescan implements source.Source against Routescan's
// Etherscan-compatible API. Routescan keeps us keyless on chains
// where the official Etherscan free tier withholds coverage (most
// notably Optimism), and in particular serves balancehistory at
// historical blocks without a PRO subscription.
//
// Every wire call goes through adapters/internal/ethscan, which
// already handles the envelope, error mapping, rate-limit, and
// retry. This adapter stays thin — it is basically a typed façade
// over the Etherscan-style actions.
package routescan

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"strconv"
	"time"

	"github.com/seokheejang/chain-sync-watch/adapters/internal/ethscan"
	"github.com/seokheejang/chain-sync-watch/adapters/internal/httpx"
	"github.com/seokheejang/chain-sync-watch/internal/chain"
	"github.com/seokheejang/chain-sync-watch/internal/source"
)

// ID is the SourceID every routescan.Adapter reports.
const ID source.SourceID = "routescan"

// Adapter is the Routescan source implementation.
type Adapter struct {
	chainID chain.ChainID
	client  *ethscan.Client
}

// Option configures the adapter.
type Option func(*Adapter)

// WithBaseURL overrides the per-chain URL for private mirrors or
// tests. Production callers rely on BaseURL(chainID).
func WithBaseURL(url string) Option {
	return func(a *Adapter) {
		a.client = ethscan.New(url, ethscan.WithHTTPX(sharedHTTPX()))
	}
}

// WithHTTPX swaps the shared HTTP client (rate limit / timeout /
// transport). Must be applied before WithBaseURL if both are used.
func WithHTTPX(hc *httpx.Client) Option {
	return func(a *Adapter) {
		a.client = ethscan.New(BaseURL(a.chainID), ethscan.WithHTTPX(hc))
	}
}

// New builds a Routescan adapter.
func New(chainID chain.ChainID, opts ...Option) (*Adapter, error) {
	if chainID == 0 {
		return nil, errors.New("routescan: chain id is required")
	}
	a := &Adapter{chainID: chainID}
	a.client = ethscan.New(BaseURL(chainID), ethscan.WithHTTPX(sharedHTTPX()))
	for _, opt := range opts {
		opt(a)
	}
	return a, nil
}

func sharedHTTPX() *httpx.Client {
	// Routescan observed: 120 rpm, 10k rpd. 2 rps keeps us safely
	// inside both bounds.
	return httpx.New(
		httpx.WithTimeout(15*time.Second),
		httpx.WithRateLimit(2, 1),
	)
}

func (a *Adapter) ID() source.SourceID    { return ID }
func (a *Adapter) ChainID() chain.ChainID { return a.chainID }

// Supports mirrors the research doc's Routescan column. Chain-wide
// aggregates have no Etherscan-compat action, so we decline those
// here and let Blockscout own the Snapshot category. ERC-20
// holdings are reported but not anchor-windowed — Routescan does
// not expose a reflected block.
func (a *Adapter) Supports(c source.Capability) bool {
	switch c {
	case source.CapBlockHash,
		source.CapBlockParentHash,
		source.CapBlockTimestamp,
		source.CapBlockTxCount,
		source.CapBlockGasUsed,
		source.CapBlockStateRoot,
		source.CapBlockReceiptsRoot,
		source.CapBlockTransactionsRoot,
		source.CapBlockMiner:
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

	case source.CapInternalTxByTx,
		source.CapInternalTxByBlock:
		return true

	case source.CapTotalAddressCount,
		source.CapTotalTxCount,
		source.CapTotalContractCount,
		source.CapERC20TokenCount:
		return false
	}
	return false
}

// --- Block ---------------------------------------------------------------

type rawProxyBlock struct {
	Number           string   `json:"number"`
	Hash             string   `json:"hash"`
	ParentHash       string   `json:"parentHash"`
	StateRoot        string   `json:"stateRoot"`
	TransactionsRoot string   `json:"transactionsRoot"`
	ReceiptsRoot     string   `json:"receiptsRoot"`
	Miner            string   `json:"miner"`
	GasUsed          string   `json:"gasUsed"`
	Timestamp        string   `json:"timestamp"`
	Transactions     []string `json:"transactions"`
}

// FetchBlock calls the proxy module once — all nine block-immutable
// fields live in the raw eth_getBlockByNumber response.
func (a *Adapter) FetchBlock(ctx context.Context, q source.BlockQuery) (source.BlockResult, error) {
	var raw *rawProxyBlock
	err := a.client.CallProxy(ctx, "eth_getBlockByNumber", map[string]string{
		"tag":     q.Number.Hex(),
		"boolean": "false",
	}, &raw)
	if err != nil {
		return source.BlockResult{SourceID: ID}, err
	}
	if raw == nil {
		return source.BlockResult{SourceID: ID}, source.ErrNotFound
	}
	out := source.BlockResult{
		Number:    q.Number,
		SourceID:  ID,
		FetchedAt: time.Now().UTC(),
	}
	hash, err := parseHash(raw.Hash)
	if err != nil {
		return source.BlockResult{SourceID: ID}, err
	}
	out.Hash = &hash
	parent, err := parseHash(raw.ParentHash)
	if err != nil {
		return source.BlockResult{SourceID: ID}, err
	}
	out.ParentHash = &parent
	state, err := parseHash(raw.StateRoot)
	if err != nil {
		return source.BlockResult{SourceID: ID}, err
	}
	out.StateRoot = &state
	receipts, err := parseHash(raw.ReceiptsRoot)
	if err != nil {
		return source.BlockResult{SourceID: ID}, err
	}
	out.ReceiptsRoot = &receipts
	txRoot, err := parseHash(raw.TransactionsRoot)
	if err != nil {
		return source.BlockResult{SourceID: ID}, err
	}
	out.TransactionsRoot = &txRoot
	miner, err := chain.NewAddress(raw.Miner)
	if err != nil {
		return source.BlockResult{SourceID: ID}, fmt.Errorf("%w: %v", source.ErrInvalidResponse, err)
	}
	out.Miner = &miner
	gas, err := parseHexU64(raw.GasUsed)
	if err != nil {
		return source.BlockResult{SourceID: ID}, err
	}
	out.GasUsed = &gas
	ts, err := parseHexU64(raw.Timestamp)
	if err != nil {
		return source.BlockResult{SourceID: ID}, err
	}
	tm := time.Unix(int64(ts), 0).UTC() //nolint:gosec // G115: unix seconds fit in int64
	out.Timestamp = &tm
	txc := uint64(len(raw.Transactions))
	out.TxCount = &txc
	return out, nil
}

// --- Address -------------------------------------------------------------

// FetchAddressLatest via account/balance (Etherscan action) + proxy
// eth_getTransactionCount. No reflected-block meta — Routescan's
// account module returns values only.
func (a *Adapter) FetchAddressLatest(ctx context.Context, q source.AddressQuery) (source.AddressLatestResult, error) {
	var balStr string
	err := a.client.Call(ctx, "account", "balance", map[string]string{
		"address": q.Address.Hex(),
		"tag":     q.Anchor.String(),
	}, &balStr)
	if err != nil {
		return source.AddressLatestResult{SourceID: ID}, err
	}
	bal, ok := new(big.Int).SetString(balStr, 10)
	if !ok {
		return source.AddressLatestResult{SourceID: ID}, source.ErrInvalidResponse
	}
	nonce, err := a.fetchNonceProxy(ctx, q.Address, q.Anchor.String())
	if err != nil {
		return source.AddressLatestResult{SourceID: ID}, err
	}
	return source.AddressLatestResult{
		Balance:   bal,
		Nonce:     &nonce,
		TxCount:   &nonce,
		SourceID:  ID,
		FetchedAt: time.Now().UTC(),
	}, nil
}

// FetchAddressAtBlock via account/balancehistory (Optimism free) +
// proxy nonce at the given block.
func (a *Adapter) FetchAddressAtBlock(ctx context.Context, q source.AddressAtBlockQuery) (source.AddressAtBlockResult, error) {
	blockno := strconv.FormatUint(q.Block.Uint64(), 10)
	var balStr string
	err := a.client.Call(ctx, "account", "balancehistory", map[string]string{
		"address": q.Address.Hex(),
		"blockno": blockno,
	}, &balStr)
	if err != nil {
		return source.AddressAtBlockResult{SourceID: ID}, err
	}
	bal, ok := new(big.Int).SetString(balStr, 10)
	if !ok {
		return source.AddressAtBlockResult{SourceID: ID}, source.ErrInvalidResponse
	}
	nonce, err := a.fetchNonceProxy(ctx, q.Address, q.Block.Hex())
	if err != nil {
		return source.AddressAtBlockResult{SourceID: ID}, err
	}
	refl := q.Block
	return source.AddressAtBlockResult{
		Balance:        bal,
		Nonce:          &nonce,
		Block:          q.Block,
		SourceID:       ID,
		FetchedAt:      time.Now().UTC(),
		ReflectedBlock: &refl,
	}, nil
}

func (a *Adapter) fetchNonceProxy(ctx context.Context, addr chain.Address, tag string) (uint64, error) {
	var hex string
	err := a.client.CallProxy(ctx, "eth_getTransactionCount", map[string]string{
		"address": addr.Hex(),
		"tag":     tag,
	}, &hex)
	if err != nil {
		return 0, err
	}
	return parseHexU64(hex)
}

// --- ERC-20 --------------------------------------------------------------

type rawTokenHolding struct {
	TokenAddress  string `json:"TokenAddress"`
	TokenName     string `json:"TokenName"`
	TokenSymbol   string `json:"TokenSymbol"`
	TokenQuantity string `json:"TokenQuantity"`
	TokenDivisor  string `json:"TokenDivisor"`
}

// FetchERC20Balance via account/tokenbalance.
func (a *Adapter) FetchERC20Balance(ctx context.Context, q source.ERC20BalanceQuery) (source.ERC20BalanceResult, error) {
	if q.Anchor.Kind() == source.BlockTagNumeric {
		// tokenbalance has no blockno parameter on the free tier.
		return source.ERC20BalanceResult{SourceID: ID}, source.ErrUnsupported
	}
	var balStr string
	err := a.client.Call(ctx, "account", "tokenbalance", map[string]string{
		"contractaddress": q.Token.Hex(),
		"address":         q.Address.Hex(),
		"tag":             "latest",
	}, &balStr)
	if err != nil {
		return source.ERC20BalanceResult{SourceID: ID}, err
	}
	bal, ok := new(big.Int).SetString(balStr, 10)
	if !ok {
		return source.ERC20BalanceResult{SourceID: ID}, source.ErrInvalidResponse
	}
	return source.ERC20BalanceResult{
		Balance:   bal,
		SourceID:  ID,
		FetchedAt: time.Now().UTC(),
	}, nil
}

// FetchERC20Holdings via account/addresstokenbalance. Routescan does
// not filter spam tokens — we leave filtering to the judgement
// layer, which cross-checks is_scam / reputation against Blockscout.
func (a *Adapter) FetchERC20Holdings(ctx context.Context, q source.ERC20HoldingsQuery) (source.ERC20HoldingsResult, error) {
	if q.Anchor.Kind() == source.BlockTagNumeric {
		return source.ERC20HoldingsResult{SourceID: ID}, source.ErrUnsupported
	}
	var raw []rawTokenHolding
	err := a.client.Call(ctx, "account", "addresstokenbalance", map[string]string{
		"address": q.Address.Hex(),
		"page":    "1",
		"offset":  "100",
	}, &raw)
	if err != nil {
		return source.ERC20HoldingsResult{SourceID: ID}, err
	}

	tokens := make([]source.TokenHolding, 0, len(raw))
	for i := range raw {
		t := &raw[i]
		addr, err := chain.NewAddress(t.TokenAddress)
		if err != nil {
			continue
		}
		bal, ok := new(big.Int).SetString(t.TokenQuantity, 10)
		if !ok {
			continue
		}
		dec, _ := strconv.ParseUint(t.TokenDivisor, 10, 8)
		tokens = append(tokens, source.TokenHolding{
			Contract: addr,
			Name:     t.TokenName,
			Symbol:   t.TokenSymbol,
			Decimals: uint8(dec), //nolint:gosec // G115: ERC-20 decimals <= 255
			Balance:  bal,
		})
	}
	return source.ERC20HoldingsResult{
		Tokens:    tokens,
		SourceID:  ID,
		FetchedAt: time.Now().UTC(),
	}, nil
}

// --- Internal tx ---------------------------------------------------------

type rawInternal struct {
	BlockNumber     string `json:"blockNumber"`
	From            string `json:"from"`
	To              string `json:"to"`
	Value           string `json:"value"`
	Gas             string `json:"gas"`
	GasUsed         string `json:"gasUsed"`
	Type            string `json:"type"`
	IsError         string `json:"isError"`
	ErrorCode       string `json:"errCode"`
	ContractAddress string `json:"contractAddress"`
}

// FetchInternalTxByTx via account/txlistinternal&txhash=...
func (a *Adapter) FetchInternalTxByTx(ctx context.Context, q source.InternalTxByTxQuery) (source.InternalTxResult, error) {
	var raw []rawInternal
	err := a.client.Call(ctx, "account", "txlistinternal", map[string]string{
		"txhash": q.TxHash.Hex(),
	}, &raw)
	if err != nil {
		return source.InternalTxResult{SourceID: ID}, err
	}
	return source.InternalTxResult{
		Traces:         toInternalTxSlice(raw),
		SourceID:       ID,
		FetchedAt:      time.Now().UTC(),
		ReflectedBlock: firstBlockNumber(raw),
	}, nil
}

// FetchInternalTxByBlock via account/txlistinternal&startblock=N&endblock=N.
func (a *Adapter) FetchInternalTxByBlock(ctx context.Context, q source.InternalTxByBlockQuery) (source.InternalTxResult, error) {
	block := strconv.FormatUint(q.Block.Uint64(), 10)
	var raw []rawInternal
	err := a.client.Call(ctx, "account", "txlistinternal", map[string]string{
		"startblock": block,
		"endblock":   block,
	}, &raw)
	if err != nil {
		return source.InternalTxResult{SourceID: ID}, err
	}
	refl := q.Block
	return source.InternalTxResult{
		Traces:         toInternalTxSlice(raw),
		SourceID:       ID,
		FetchedAt:      time.Now().UTC(),
		ReflectedBlock: &refl,
	}, nil
}

func toInternalTxSlice(raw []rawInternal) []source.InternalTx {
	out := make([]source.InternalTx, 0, len(raw))
	for i := range raw {
		it := &raw[i]
		from, _ := chain.NewAddress(it.From)
		to, _ := chain.NewAddress(it.To)
		val, _ := new(big.Int).SetString(it.Value, 10)
		if val == nil {
			val = new(big.Int)
		}
		gas, _ := strconv.ParseUint(it.GasUsed, 10, 64)
		errMsg := ""
		if it.IsError == "1" {
			errMsg = it.ErrorCode
			if errMsg == "" {
				errMsg = "execution reverted"
			}
		}
		out = append(out, source.InternalTx{
			From:     from,
			To:       to,
			Value:    val,
			GasUsed:  gas,
			CallType: it.Type,
			Error:    errMsg,
		})
	}
	return out
}

func firstBlockNumber(raw []rawInternal) *chain.BlockNumber {
	for i := range raw {
		n, err := strconv.ParseUint(raw[i].BlockNumber, 10, 64)
		if err == nil {
			b := chain.NewBlockNumber(n)
			return &b
		}
	}
	return nil
}

// --- Snapshot (unsupported) ---------------------------------------------

// FetchSnapshot is unsupported: Etherscan-compat `stats` module has
// no chain-wide total_addresses / total_transactions action.
func (a *Adapter) FetchSnapshot(_ context.Context, _ source.SnapshotQuery) (source.SnapshotResult, error) {
	return source.SnapshotResult{SourceID: ID}, source.ErrUnsupported
}

// --- parse helpers (local to avoid depending on other adapter pkgs) -----

func parseHash(s string) (chain.Hash32, error) {
	h, err := chain.NewHash32(s)
	if err != nil {
		return chain.Hash32{}, fmt.Errorf("%w: %v", source.ErrInvalidResponse, err)
	}
	return h, nil
}

func parseHexU64(s string) (uint64, error) {
	if len(s) < 2 || (s[:2] != "0x" && s[:2] != "0X") {
		return 0, source.ErrInvalidResponse
	}
	raw := s[2:]
	if raw == "" {
		return 0, nil
	}
	return strconv.ParseUint(raw, 16, 64)
}
