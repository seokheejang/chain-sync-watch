package blockscout

import (
	"context"
	"math/big"
	"time"

	"github.com/seokheejang/chain-sync-watch/internal/chain"
	"github.com/seokheejang/chain-sync-watch/internal/source"
)

// rawAddress is the subset of /api/v2/addresses/{addr} we consume.
// block_number_balance_updated_at is the canonical reflected-block
// hint for Tier B anchor-window comparison.
type rawAddress struct {
	CoinBalance                 string `json:"coin_balance"`
	BlockNumberBalanceUpdatedAt uint64 `json:"block_number_balance_updated_at"`
}

// FetchAddressLatest returns balance + nonce at the query's Anchor.
// REST v2 gives us the balance and the reflected block; nonce comes
// from the proxy eth_getTransactionCount with the anchor tag forwarded.
func (a *Adapter) FetchAddressLatest(ctx context.Context, q source.AddressQuery) (source.AddressLatestResult, error) {
	bal, reflected, err := a.fetchBalanceREST(ctx, q.Address)
	if err != nil {
		return source.AddressLatestResult{SourceID: ID}, err
	}
	nonce, err := a.fetchNonceProxy(ctx, q.Address, q.Anchor.String())
	if err != nil {
		return source.AddressLatestResult{SourceID: ID}, err
	}
	return source.AddressLatestResult{
		Balance:        bal,
		Nonce:          &nonce,
		TxCount:        &nonce,
		SourceID:       ID,
		FetchedAt:      time.Now().UTC(),
		ReflectedBlock: reflected,
	}, nil
}

// FetchAddressAtBlock uses the proxy module so the caller's numeric
// anchor is honoured strictly.
func (a *Adapter) FetchAddressAtBlock(ctx context.Context, q source.AddressAtBlockQuery) (source.AddressAtBlockResult, error) {
	tag := q.Block.Hex()

	var balHex string
	err := a.proxy.CallProxy(ctx, "eth_getBalance", map[string]string{
		"address": q.Address.Hex(),
		"tag":     tag,
	}, &balHex)
	if err != nil {
		return source.AddressAtBlockResult{SourceID: ID}, err
	}
	bal, err := parseHexBigInt(balHex)
	if err != nil {
		return source.AddressAtBlockResult{SourceID: ID}, err
	}

	nonce, err := a.fetchNonceProxy(ctx, q.Address, tag)
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

func (a *Adapter) fetchBalanceREST(ctx context.Context, addr chain.Address) (*big.Int, *chain.BlockNumber, error) {
	var raw rawAddress
	if err := a.getJSON(ctx, "/addresses/"+addr.Hex(), &raw); err != nil {
		return nil, nil, err
	}
	bal, ok := new(big.Int).SetString(raw.CoinBalance, 10)
	if !ok {
		return nil, nil, source.ErrInvalidResponse
	}
	var reflected *chain.BlockNumber
	if raw.BlockNumberBalanceUpdatedAt > 0 {
		n := chain.NewBlockNumber(raw.BlockNumberBalanceUpdatedAt)
		reflected = &n
	}
	return bal, reflected, nil
}

func (a *Adapter) fetchNonceProxy(ctx context.Context, addr chain.Address, tag string) (uint64, error) {
	var hex string
	err := a.proxy.CallProxy(ctx, "eth_getTransactionCount", map[string]string{
		"address": addr.Hex(),
		"tag":     tag,
	}, &hex)
	if err != nil {
		return 0, err
	}
	return parseHexU64(hex)
}

func parseHexBigInt(s string) (*big.Int, error) {
	raw, err := trim0x(s)
	if err != nil {
		return nil, err
	}
	if raw == "" {
		return new(big.Int), nil
	}
	v, ok := new(big.Int).SetString(raw, 16)
	if !ok {
		return nil, source.ErrInvalidResponse
	}
	return v, nil
}
