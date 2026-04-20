package blockscout

import (
	"context"
	"math/big"
	"time"

	"github.com/seokheejang/chain-sync-watch/internal/chain"
	"github.com/seokheejang/chain-sync-watch/internal/source"
)

// rawInternalItem is the subset of each item in
// /api/v2/transactions/{hash}/internal-transactions we consume.
type rawInternalItem struct {
	BlockNumber uint64 `json:"block_number"`
	Type        string `json:"type"`
	Value       string `json:"value"`
	GasLimit    string `json:"gas_limit"`
	Error       string `json:"error"`
	From        struct {
		Hash string `json:"hash"`
	} `json:"from"`
	To struct {
		Hash string `json:"hash"`
	} `json:"to"`
}

type rawInternalPage struct {
	Items []rawInternalItem `json:"items"`
}

// FetchInternalTxByTx lists every internal call recorded under one
// transaction. Result ordering mirrors upstream (execution order).
func (a *Adapter) FetchInternalTxByTx(ctx context.Context, q source.InternalTxByTxQuery) (source.InternalTxResult, error) {
	var page rawInternalPage
	if err := a.getJSON(ctx, "/transactions/"+q.TxHash.Hex()+"/internal-transactions", &page); err != nil {
		return source.InternalTxResult{SourceID: ID}, err
	}

	traces := make([]source.InternalTx, 0, len(page.Items))
	var refl *chain.BlockNumber
	for i := range page.Items {
		it := &page.Items[i]
		from, _ := chain.NewAddress(it.From.Hash)
		to, _ := chain.NewAddress(it.To.Hash)
		gas, _ := parseDecimalU64(it.GasLimit)
		val := new(big.Int)
		if it.Value != "" {
			val, _ = new(big.Int).SetString(it.Value, 10)
			if val == nil {
				val = new(big.Int)
			}
		}
		traces = append(traces, source.InternalTx{
			From:     from,
			To:       to,
			Value:    val,
			GasUsed:  gas,
			CallType: it.Type,
			Error:    it.Error,
		})
		if refl == nil && it.BlockNumber > 0 {
			n := chain.NewBlockNumber(it.BlockNumber)
			refl = &n
		}
	}

	return source.InternalTxResult{
		Traces:         traces,
		SourceID:       ID,
		FetchedAt:      time.Now().UTC(),
		ReflectedBlock: refl,
	}, nil
}

// FetchInternalTxByBlock is unsupported: Blockscout REST v2 exposes
// internal txs per address/per transaction, not per block.
func (a *Adapter) FetchInternalTxByBlock(_ context.Context, _ source.InternalTxByBlockQuery) (source.InternalTxResult, error) {
	return source.InternalTxResult{SourceID: ID}, source.ErrUnsupported
}
