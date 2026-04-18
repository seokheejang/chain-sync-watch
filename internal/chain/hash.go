package chain

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// Hash32 is a 32-byte hash value. EVM block hashes, transaction hashes,
// state/receipts/transactions roots, log topic values, and storage keys
// all share this shape, so we keep a single underlying type and expose
// aliases at the usage site.
//
// Unlike Address, hashes are not case-checksummed — any casing on the
// wire is valid input, and the canonical output is always lowercase.
type Hash32 [32]byte

// TxHash is an alias of Hash32. Using a distinct name at call sites
// documents intent without complicating assignments.
type TxHash = Hash32

// BlockHash is an alias of Hash32.
type BlockHash = Hash32

var zeroHash32 Hash32

// NewHash32 parses a 64-hex-char string with 0x/0X prefix.
func NewHash32(s string) (Hash32, error) {
	if !strings.HasPrefix(s, "0x") && !strings.HasPrefix(s, "0X") {
		return Hash32{}, fmt.Errorf("hash32: missing 0x/0X prefix: %q", s)
	}
	hexPart := s[2:]
	if len(hexPart) != 64 {
		return Hash32{}, fmt.Errorf("hash32: expected 64 hex chars after prefix, got %d", len(hexPart))
	}

	var h Hash32
	if _, err := hex.Decode(h[:], []byte(strings.ToLower(hexPart))); err != nil {
		return Hash32{}, fmt.Errorf("hash32: invalid hex: %w", err)
	}
	return h, nil
}

// Bytes returns a copy of the 32-byte value so callers cannot mutate
// the underlying array through the returned slice.
func (h Hash32) Bytes() []byte {
	out := make([]byte, len(h))
	copy(out, h[:])
	return out
}

// Hex returns the canonical lowercase "0x..." representation.
func (h Hash32) Hex() string {
	return "0x" + hex.EncodeToString(h[:])
}

// String satisfies fmt.Stringer, returning the canonical hex.
func (h Hash32) String() string { return h.Hex() }

// IsZero reports whether h is the all-zero value.
func (h Hash32) IsZero() bool { return h == zeroHash32 }

// MarshalJSON emits the canonical hex string.
func (h Hash32) MarshalJSON() ([]byte, error) {
	return json.Marshal(h.Hex())
}

// UnmarshalJSON accepts only the string form.
func (h *Hash32) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		return errors.New("hash32: cannot decode null")
	}
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("hash32: decode string: %w", err)
	}
	parsed, err := NewHash32(s)
	if err != nil {
		return err
	}
	*h = parsed
	return nil
}
