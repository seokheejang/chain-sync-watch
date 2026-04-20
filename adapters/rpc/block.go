package rpc

import (
	"context"
	"time"

	"github.com/seokheejang/chain-sync-watch/internal/source"
)

// rawBlock is the eth_getBlockByNumber response shape for the fields
// we care about. The RPC returns many more fields; we pick only what
// Capability enumerates so the response surface stays deterministic.
type rawBlock struct {
	Number           string   `json:"number"`
	Hash             string   `json:"hash"`
	ParentHash       string   `json:"parentHash"`
	StateRoot        string   `json:"stateRoot"`
	TransactionsRoot string   `json:"transactionsRoot"`
	ReceiptsRoot     string   `json:"receiptsRoot"`
	Miner            string   `json:"miner"`
	GasUsed          string   `json:"gasUsed"`
	Timestamp        string   `json:"timestamp"`
	Transactions     []string `json:"transactions"` // tx hashes (we request boolean=false)
}

// FetchBlock loads block header data by height via
// eth_getBlockByNumber. We always request boolean=false so the node
// returns tx hashes instead of full transaction objects — the count
// is all we need and the payload stays small.
//
// When the node indicates "no such block" by returning a JSON null,
// we surface source.ErrNotFound so the caller can distinguish "tip
// not yet reached" from a real fetch failure.
func (a *Adapter) FetchBlock(ctx context.Context, q source.BlockQuery) (source.BlockResult, error) {
	var raw *rawBlock
	if err := a.callRPC(ctx, "eth_getBlockByNumber", &raw, q.Number.Hex(), false); err != nil {
		return source.BlockResult{SourceID: ID}, err
	}
	if raw == nil {
		// Some nodes return a literal null for unknown blocks; callRPC
		// decodes that to a nil pointer. Treat as "not found".
		return source.BlockResult{SourceID: ID}, source.ErrNotFound
	}

	out := source.BlockResult{
		Number:    q.Number,
		SourceID:  ID,
		FetchedAt: time.Now().UTC(),
	}

	// Parse required fields. Any parse error surfaces as
	// ErrInvalidResponse via parseXxx helpers — one bad field fails
	// the whole block rather than populating a half-correct result.
	if err := populateBlockHashes(&out, raw); err != nil {
		return source.BlockResult{SourceID: ID}, err
	}
	if err := populateBlockScalars(&out, raw); err != nil {
		return source.BlockResult{SourceID: ID}, err
	}
	if err := populateBlockMiner(&out, raw); err != nil {
		return source.BlockResult{SourceID: ID}, err
	}

	// Tx count is derived from the array length: when boolean=false
	// was passed, geth returns a tx-hash list whose length is the
	// canonical count.
	txc := uint64(len(raw.Transactions))
	out.TxCount = &txc

	return out, nil
}

func populateBlockHashes(out *source.BlockResult, raw *rawBlock) error {
	// chain.BlockHash is an alias of chain.Hash32, so the parsed
	// value can be used directly without a named conversion.
	h, err := parseHash32(raw.Hash)
	if err != nil {
		return err
	}
	out.Hash = &h

	p, err := parseHash32(raw.ParentHash)
	if err != nil {
		return err
	}
	out.ParentHash = &p

	sr, err := parseHash32(raw.StateRoot)
	if err != nil {
		return err
	}
	out.StateRoot = &sr

	rr, err := parseHash32(raw.ReceiptsRoot)
	if err != nil {
		return err
	}
	out.ReceiptsRoot = &rr

	tr, err := parseHash32(raw.TransactionsRoot)
	if err != nil {
		return err
	}
	out.TransactionsRoot = &tr

	return nil
}

func populateBlockScalars(out *source.BlockResult, raw *rawBlock) error {
	gas, err := parseHexUint64(raw.GasUsed)
	if err != nil {
		return err
	}
	out.GasUsed = &gas

	ts, err := parseHexUint64(raw.Timestamp)
	if err != nil {
		return err
	}
	tm := time.Unix(int64(ts), 0).UTC() //nolint:gosec // G115: unix seconds fit in int64
	out.Timestamp = &tm
	return nil
}

func populateBlockMiner(out *source.BlockResult, raw *rawBlock) error {
	m, err := parseAddress(raw.Miner)
	if err != nil {
		return err
	}
	out.Miner = &m
	return nil
}
