package application

import (
	"bytes"
	"sort"
	"strconv"
	"strings"

	"github.com/seokheejang/chain-sync-watch/internal/source"
)

// extractBlockField renders the BlockResult field corresponding to a
// Capability into its canonical string form. The returned ok is
// false when the Source did not populate the field — the caller
// should skip that (Source, Metric) combination rather than emit a
// comparison against an empty value.
//
// Canonical forms:
//
//   - block hashes / state roots / receipt roots / tx roots / parent
//     hash: lowercase 0x-prefixed hex (chain.Hash32.Hex()).
//   - timestamps: decimal Unix seconds.
//   - tx count / gas used: base-10 decimal string.
//   - miner address: EIP-55 canonical (chain.Address.String()).
//
// Normalising here lets the Tolerance layer compare Raw strings
// byte-for-byte, which is what ExactMatch wants.
func extractBlockField(cap source.Capability, r source.BlockResult) (string, bool) {
	switch cap {
	case source.CapBlockHash:
		if r.Hash == nil {
			return "", false
		}
		return r.Hash.Hex(), true
	case source.CapBlockParentHash:
		if r.ParentHash == nil {
			return "", false
		}
		return r.ParentHash.Hex(), true
	case source.CapBlockStateRoot:
		if r.StateRoot == nil {
			return "", false
		}
		return r.StateRoot.Hex(), true
	case source.CapBlockReceiptsRoot:
		if r.ReceiptsRoot == nil {
			return "", false
		}
		return r.ReceiptsRoot.Hex(), true
	case source.CapBlockTransactionsRoot:
		if r.TransactionsRoot == nil {
			return "", false
		}
		return r.TransactionsRoot.Hex(), true
	case source.CapBlockTimestamp:
		if r.Timestamp == nil {
			return "", false
		}
		return strconv.FormatInt(r.Timestamp.Unix(), 10), true
	case source.CapBlockTxCount:
		if r.TxCount == nil {
			return "", false
		}
		return strconv.FormatUint(*r.TxCount, 10), true
	case source.CapBlockGasUsed:
		if r.GasUsed == nil {
			return "", false
		}
		return strconv.FormatUint(*r.GasUsed, 10), true
	case source.CapBlockMiner:
		if r.Miner == nil {
			return "", false
		}
		return r.Miner.String(), true
	}
	return "", false
}

// extractAddressLatestField renders the AddressLatestResult field
// corresponding to a Capability into its canonical string form.
// Returns ok=false when the Source did not populate the field.
//
// Canonical forms:
//
//   - balance: big.Int decimal (adapter normalises hex/decimal
//     wire forms into *big.Int before the result ever reaches here).
//   - nonce / tx_count: base-10 decimal string.
//
// This helper only covers the plain AddressLatestResult fields
// (balance, nonce, tx_count). ERC-20 balance and holdings live on
// ERC20BalanceResult / ERC20HoldingsResult and need their own
// extractors when that path lands.
func extractAddressLatestField(capb source.Capability, r source.AddressLatestResult) (string, bool) {
	switch capb {
	case source.CapBalanceAtLatest:
		if r.Balance == nil {
			return "", false
		}
		return r.Balance.String(), true
	case source.CapNonceAtLatest:
		if r.Nonce == nil {
			return "", false
		}
		return strconv.FormatUint(*r.Nonce, 10), true
	case source.CapTxCountAtLatest:
		if r.TxCount == nil {
			return "", false
		}
		return strconv.FormatUint(*r.TxCount, 10), true
	}
	return "", false
}

// extractAddressAtBlockField renders the AddressAtBlockResult field
// corresponding to a Capability. The same canonical forms as
// extractAddressLatestField apply; only the result type differs
// (archive reads populate Block as well, but Block is not a
// comparison target — it is the query key, echoed back for
// caller sanity checks).
func extractAddressAtBlockField(capb source.Capability, r source.AddressAtBlockResult) (string, bool) {
	switch capb {
	case source.CapBalanceAtBlock:
		if r.Balance == nil {
			return "", false
		}
		return r.Balance.String(), true
	case source.CapNonceAtBlock:
		if r.Nonce == nil {
			return "", false
		}
		return strconv.FormatUint(*r.Nonce, 10), true
	}
	return "", false
}

// extractERC20HoldingsField renders ERC20HoldingsResult into a
// canonical, order-independent string so ExactMatch tolerance can
// tell two sources apart.
//
// Canonical form:
//
//	contract1=balance1;contract2=balance2;...
//
// where contracts are sorted byte-ascending on the 20-byte address
// and balances are big.Int decimals. We deliberately omit Name /
// Symbol / Decimals — those are metadata the source may or may not
// populate, and including them in the canonical form would trigger
// spurious diffs whenever one source cached a display name and
// another did not.
//
// An empty holdings list (Tokens == nil OR len == 0) renders as
// the empty string AND returns ok=true — "I checked, there is
// nothing" is a valid observation, distinct from "I could not
// fetch". Adapters that truly cannot serve holdings return a
// transport error which the caller skips.
//
// nil Balance on a TokenHolding is a malformed record; we render
// it as "<contract>=" so a disagreement still surfaces rather than
// being silently dropped.
func extractERC20HoldingsField(capb source.Capability, r source.ERC20HoldingsResult) (string, bool) {
	if capb != source.CapERC20HoldingsAtLatest {
		return "", false
	}
	holdings := make([]source.TokenHolding, len(r.Tokens))
	copy(holdings, r.Tokens)
	sort.Slice(holdings, func(i, j int) bool {
		return bytes.Compare(holdings[i].Contract.Bytes(), holdings[j].Contract.Bytes()) < 0
	})
	var b strings.Builder
	for i, h := range holdings {
		if i > 0 {
			b.WriteByte(';')
		}
		b.WriteString(h.Contract.String())
		b.WriteByte('=')
		if h.Balance != nil {
			b.WriteString(h.Balance.String())
		}
	}
	return b.String(), true
}
