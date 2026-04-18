package chain

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// BlockNumber is a non-negative block height.
//
// On the wire we use the Ethereum JSON-RPC canonical form ("0xa",
// lowercase, no leading zeros). On input we accept either a JSON number
// or a hex string with 0x/0X prefix so indexers that emit integers and
// raw RPC responses both round-trip cleanly.
type BlockNumber uint64

// NewBlockNumber is the explicit constructor — keeps call sites greppable
// and leaves room to add validation later without changing the API.
func NewBlockNumber(n uint64) BlockNumber { return BlockNumber(n) }

// Uint64 returns the underlying height.
func (b BlockNumber) Uint64() uint64 { return uint64(b) }

// Hex returns the RPC-canonical "0x..." representation.
func (b BlockNumber) Hex() string {
	return "0x" + strconv.FormatUint(uint64(b), 16)
}

// MarshalJSON emits the RPC-canonical hex string so values we produce
// are directly usable as eth_getBlockByNumber parameters.
func (b BlockNumber) MarshalJSON() ([]byte, error) {
	return json.Marshal(b.Hex())
}

// UnmarshalJSON accepts either a JSON number (common in indexer
// responses) or a hex string with 0x/0X prefix (RPC form).
//
// Decimal strings ("10") are intentionally rejected: if a value is
// quoted it must use hex, otherwise callers mixing "10" and "0x10"
// would silently disagree about what the digits mean.
func (b *BlockNumber) UnmarshalJSON(data []byte) error {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" || trimmed == "null" {
		return errors.New("block number: cannot decode null/empty")
	}

	// JSON number — parse as uint64 directly.
	if trimmed[0] != '"' {
		var n uint64
		if err := json.Unmarshal(data, &n); err != nil {
			return fmt.Errorf("block number: decode number: %w", err)
		}
		*b = BlockNumber(n)
		return nil
	}

	// JSON string — must be hex with 0x/0X prefix.
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("block number: decode string: %w", err)
	}
	if !strings.HasPrefix(s, "0x") && !strings.HasPrefix(s, "0X") {
		return fmt.Errorf("block number: string form must be hex with 0x prefix, got %q", s)
	}
	v, err := strconv.ParseUint(s[2:], 16, 64)
	if err != nil {
		return fmt.Errorf("block number: parse hex %q: %w", s, err)
	}
	*b = BlockNumber(v)
	return nil
}
