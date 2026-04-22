package application

import (
	"strconv"

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
