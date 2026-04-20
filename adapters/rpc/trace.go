package rpc

import (
	"context"
	"math/big"
	"time"

	"github.com/seokheejang/chain-sync-watch/internal/chain"
	"github.com/seokheejang/chain-sync-watch/internal/source"
)

// rawCallFrame mirrors geth's callTracer output. The recursion via
// Calls captures nested invocations — we flatten them depth-first on
// the way out so the consumer sees a linear sequence matching the
// on-chain execution order.
type rawCallFrame struct {
	Type    string         `json:"type"`
	From    string         `json:"from"`
	To      string         `json:"to"`
	Value   string         `json:"value"`
	Gas     string         `json:"gas"`
	GasUsed string         `json:"gasUsed"`
	Input   string         `json:"input"`
	Output  string         `json:"output"`
	Error   string         `json:"error,omitempty"`
	Calls   []rawCallFrame `json:"calls,omitempty"`
}

// rawBlockTraceItem is the per-transaction wrapper returned by
// debug_traceBlockByNumber. The actual call tree lives in .result.
type rawBlockTraceItem struct {
	Result rawCallFrame `json:"result"`
}

// FetchInternalTxByTx traces a single transaction's internal calls
// via debug_traceTransaction with the callTracer preset. The output
// is the depth-first flattened sequence of calls; consumers that
// need the tree structure can reconstruct it from the order + a
// node-specific depth field in a future extension.
func (a *Adapter) FetchInternalTxByTx(ctx context.Context, q source.InternalTxByTxQuery) (source.InternalTxResult, error) {
	if !a.debugTrace {
		return source.InternalTxResult{SourceID: ID}, source.ErrUnsupported
	}

	var root rawCallFrame
	cfg := map[string]string{"tracer": "callTracer"}
	if err := a.callRPC(ctx, "debug_traceTransaction", &root, q.TxHash.Hex(), cfg); err != nil {
		return source.InternalTxResult{SourceID: ID}, err
	}

	traces := flattenCallFrame(root, nil)
	return source.InternalTxResult{
		Traces:    traces,
		SourceID:  ID,
		FetchedAt: time.Now().UTC(),
	}, nil
}

// FetchInternalTxByBlock traces every transaction in a block via
// debug_traceBlockByNumber with callTracer. The per-tx call trees
// are concatenated (DFS per tx, preserving tx order) so the flat
// slice contains every internal call that executed in the block.
func (a *Adapter) FetchInternalTxByBlock(ctx context.Context, q source.InternalTxByBlockQuery) (source.InternalTxResult, error) {
	if !a.debugTrace {
		return source.InternalTxResult{SourceID: ID}, source.ErrUnsupported
	}

	var items []rawBlockTraceItem
	cfg := map[string]string{"tracer": "callTracer"}
	if err := a.callRPC(ctx, "debug_traceBlockByNumber", &items, q.Block.Hex(), cfg); err != nil {
		return source.InternalTxResult{SourceID: ID}, err
	}

	var traces []source.InternalTx
	for i := range items {
		traces = flattenCallFrame(items[i].Result, traces)
	}
	refl := q.Block
	return source.InternalTxResult{
		Traces:         traces,
		SourceID:       ID,
		FetchedAt:      time.Now().UTC(),
		ReflectedBlock: &refl,
	}, nil
}

// flattenCallFrame appends the frame (and, recursively, its nested
// calls) to out in depth-first order. Decode errors on any single
// field surface as empty/zero rather than aborting the whole batch
// — the caller compares traces as a whole and a corrupted frame
// usually means the node hit a transient issue we don't want to
// propagate as a fatal error.
func flattenCallFrame(f rawCallFrame, out []source.InternalTx) []source.InternalTx {
	from, _ := chain.NewAddress(f.From)
	to, _ := chain.NewAddress(f.To)
	gas, _ := parseHexUint64(f.GasUsed)

	val := new(big.Int)
	if f.Value != "" {
		parsed, err := parseHexBigInt(f.Value)
		if err == nil {
			val = parsed
		}
	}

	out = append(out, source.InternalTx{
		From:     from,
		To:       to,
		Value:    val,
		GasUsed:  gas,
		CallType: f.Type,
		Error:    f.Error,
	})
	for i := range f.Calls {
		out = flattenCallFrame(f.Calls[i], out)
	}
	return out
}
