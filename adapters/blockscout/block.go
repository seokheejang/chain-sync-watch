package blockscout

import (
	"context"
	"strconv"
	"time"

	"github.com/seokheejang/chain-sync-watch/internal/chain"
	"github.com/seokheejang/chain-sync-watch/internal/source"
)

// rawProxyBlock is the eth_getBlockByNumber proxy response shape we
// use — we ask with boolean=false so transactions is a list of tx
// hashes whose length is the canonical count.
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

// FetchBlock uses the Etherscan-compat proxy so all nine block-
// immutable fields (including the three roots REST v2 drops) come
// back in one round-trip.
func (a *Adapter) FetchBlock(ctx context.Context, q source.BlockQuery) (source.BlockResult, error) {
	var raw *rawProxyBlock
	err := a.proxy.CallProxy(ctx, "eth_getBlockByNumber", map[string]string{
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
		return source.BlockResult{SourceID: ID}, wrapInvalid(err)
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

// parseHash decodes a canonical 0x-prefixed 32-byte hash.
func parseHash(s string) (chain.Hash32, error) {
	h, err := chain.NewHash32(s)
	if err != nil {
		return chain.Hash32{}, wrapInvalid(err)
	}
	return h, nil
}

// parseHexU64 decodes an RPC-canonical 0xN hex string to uint64.
func parseHexU64(s string) (uint64, error) {
	raw, err := trim0x(s)
	if err != nil {
		return 0, err
	}
	if raw == "" {
		return 0, nil
	}
	n, err := strconv.ParseUint(raw, 16, 64)
	if err != nil {
		return 0, wrapInvalid(err)
	}
	return n, nil
}

func trim0x(s string) (string, error) {
	if len(s) < 2 || (s[:2] != "0x" && s[:2] != "0X") {
		return "", wrapInvalid(nil)
	}
	return s[2:], nil
}

func wrapInvalid(err error) error {
	if err == nil {
		return source.ErrInvalidResponse
	}
	return &wrappedErr{outer: source.ErrInvalidResponse, inner: err}
}

type wrappedErr struct {
	outer error
	inner error
}

func (w *wrappedErr) Error() string { return w.outer.Error() + ": " + w.inner.Error() }
func (w *wrappedErr) Unwrap() error { return w.outer }
