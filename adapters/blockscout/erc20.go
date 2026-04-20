package blockscout

import (
	"context"
	"encoding/hex"
	"math/big"
	"strconv"
	"time"

	"github.com/seokheejang/chain-sync-watch/internal/chain"
	"github.com/seokheejang/chain-sync-watch/internal/source"
)

// balanceOfSelector — keccak256("balanceOf(address)")[:4].
const balanceOfSelector = "70a08231"

// rawTokenHolding is one entry from /addresses/{addr}/token-balances.
type rawTokenHolding struct {
	Token struct {
		AddressHash string `json:"address_hash"`
		Name        string `json:"name"`
		Symbol      string `json:"symbol"`
		Decimals    string `json:"decimals"`
		Type        string `json:"type"`
		Reputation  string `json:"reputation"`
		IsScam      bool   `json:"is_scam"`
	} `json:"token"`
	Value string `json:"value"`
}

// FetchERC20Balance uses the proxy eth_call path so the anchor tag
// (latest / finalized / numeric) is honoured verbatim.
func (a *Adapter) FetchERC20Balance(ctx context.Context, q source.ERC20BalanceQuery) (source.ERC20BalanceResult, error) {
	data, err := encodeBalanceOf(q.Address)
	if err != nil {
		return source.ERC20BalanceResult{SourceID: ID}, err
	}
	params := map[string]string{
		"to":   q.Token.Hex(),
		"data": data,
		"tag":  q.Anchor.String(),
	}
	var retHex string
	if err := a.proxy.CallProxy(ctx, "eth_call", params, &retHex); err != nil {
		return source.ERC20BalanceResult{SourceID: ID}, err
	}
	bal, err := decodeUint256(retHex)
	if err != nil {
		return source.ERC20BalanceResult{SourceID: ID}, err
	}

	out := source.ERC20BalanceResult{
		Balance:   bal,
		SourceID:  ID,
		FetchedAt: time.Now().UTC(),
	}
	if q.Anchor.Kind() == source.BlockTagNumeric {
		n := q.Anchor.Number()
		out.ReflectedBlock = &n
	}
	return out, nil
}

// FetchERC20Holdings hits the REST v2 token-balances endpoint and
// pre-fetches /addresses/{addr} so we can surface the inferred
// reflected block (the coin_balance's block_number_balance_updated_at).
// Blockscout does not emit a per-token reflected block, but its
// indexer advances atomically — using the coin-balance block is a
// strictly-bounded approximation the comparison layer can trust within
// the anchor window.
//
// Scam-filtered by default: tokens flagged is_scam=true or with a
// non-ok reputation are dropped from the result so cross-source
// comparison does not spike on airdropped phishing tokens.
func (a *Adapter) FetchERC20Holdings(ctx context.Context, q source.ERC20HoldingsQuery) (source.ERC20HoldingsResult, error) {
	if q.Anchor.Kind() == source.BlockTagNumeric {
		// REST v2 has no at-block variant; refuse rather than return
		// latest-anchored data dressed up as historical.
		return source.ERC20HoldingsResult{SourceID: ID}, source.ErrUnsupported
	}

	_, reflected, err := a.fetchBalanceREST(ctx, q.Address)
	if err != nil {
		return source.ERC20HoldingsResult{SourceID: ID}, err
	}

	var raw []rawTokenHolding
	if err := a.getJSON(ctx, "/addresses/"+q.Address.Hex()+"/token-balances", &raw); err != nil {
		return source.ERC20HoldingsResult{SourceID: ID}, err
	}

	tokens := make([]source.TokenHolding, 0, len(raw))
	for i := range raw {
		t := &raw[i].Token
		if t.IsScam || (t.Reputation != "" && t.Reputation != "ok") {
			continue
		}
		if t.Type != "" && t.Type != "ERC-20" {
			continue
		}
		addr, err := chain.NewAddress(t.AddressHash)
		if err != nil {
			continue
		}
		bal, ok := new(big.Int).SetString(raw[i].Value, 10)
		if !ok {
			continue
		}
		dec, _ := strconv.ParseUint(t.Decimals, 10, 8)

		tokens = append(tokens, source.TokenHolding{
			Contract: addr,
			Name:     t.Name,
			Symbol:   t.Symbol,
			Decimals: uint8(dec), //nolint:gosec // G115: ERC-20 decimals <= 255
			Balance:  bal,
		})
	}

	return source.ERC20HoldingsResult{
		Tokens:         tokens,
		SourceID:       ID,
		FetchedAt:      time.Now().UTC(),
		ReflectedBlock: reflected,
	}, nil
}

func encodeBalanceOf(addr chain.Address) (string, error) {
	var buf [36]byte
	sel, err := hex.DecodeString(balanceOfSelector)
	if err != nil {
		return "", err
	}
	copy(buf[0:4], sel)
	copy(buf[16:36], addr.Bytes())
	return "0x" + hex.EncodeToString(buf[:]), nil
}

func decodeUint256(s string) (*big.Int, error) {
	raw, err := trim0x(s)
	if err != nil {
		return nil, err
	}
	if raw == "" {
		return nil, source.ErrInvalidResponse
	}
	v, ok := new(big.Int).SetString(raw, 16)
	if !ok {
		return nil, source.ErrInvalidResponse
	}
	return v, nil
}
