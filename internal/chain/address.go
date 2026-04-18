package chain

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/sha3"
)

// Address is a 20-byte EVM account address. The canonical textual form
// is the EIP-55 checksummed hex with a "0x" prefix.
//
// See https://eips.ethereum.org/EIPS/eip-55 for the checksum algorithm.
type Address [20]byte

// zeroAddress is the 20-zero-byte sentinel value ("0x0000...0000").
var zeroAddress Address

// NewAddress parses a hex address string. Accepted forms:
//
//   - all lowercase (unchecked): "0x5aaeb6..."
//   - all uppercase (unchecked): "0X5AAEB6..."
//   - mixed case: must match the EIP-55 checksum exactly; otherwise the
//     string is treated as a possible typo and rejected.
//
// The "0x" prefix is required so callers cannot accidentally pass
// unrelated hex-looking strings.
func NewAddress(s string) (Address, error) {
	if !strings.HasPrefix(s, "0x") && !strings.HasPrefix(s, "0X") {
		return Address{}, fmt.Errorf("address: missing 0x/0X prefix: %q", s)
	}
	hexPart := s[2:]
	if len(hexPart) != 40 {
		return Address{}, fmt.Errorf("address: expected 40 hex chars after prefix, got %d", len(hexPart))
	}

	var a Address
	if _, err := hex.Decode(a[:], []byte(strings.ToLower(hexPart))); err != nil {
		return Address{}, fmt.Errorf("address: invalid hex: %w", err)
	}

	// Reject a checksum mismatch only when the caller actually supplied
	// a mixed-case string — a pure lowercase/uppercase input is, per
	// EIP-55, declared "unchecked" and must be accepted.
	if hasMixedCase(hexPart) {
		canonical := a.eip55Hex()
		if "0x"+hexPart != canonical {
			return Address{}, fmt.Errorf(
				"address: EIP-55 checksum mismatch: got %q, want %q", s, canonical)
		}
	}
	return a, nil
}

// MustAddress is the panicking constructor. Only for package-level
// constants and test fixtures where the literal is known good.
func MustAddress(s string) Address {
	a, err := NewAddress(s)
	if err != nil {
		panic(err)
	}
	return a
}

// Bytes returns a copy of the underlying 20-byte value so callers
// cannot mutate the Address through the returned slice.
func (a Address) Bytes() []byte {
	out := make([]byte, len(a))
	copy(out, a[:])
	return out
}

// Hex returns the canonical EIP-55 string.
func (a Address) Hex() string { return a.eip55Hex() }

// String satisfies fmt.Stringer — same as Hex so logs show the
// checksummed form by default.
func (a Address) String() string { return a.Hex() }

// IsZero reports whether the address is the all-zero sentinel.
func (a Address) IsZero() bool { return a == zeroAddress }

// MarshalJSON emits the checksummed hex string.
func (a Address) MarshalJSON() ([]byte, error) {
	return json.Marshal(a.Hex())
}

// UnmarshalJSON accepts the string form only — numbers and objects
// are rejected to keep the JSON contract narrow.
func (a *Address) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		return errors.New("address: cannot decode null")
	}
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("address: decode string: %w", err)
	}
	parsed, err := NewAddress(s)
	if err != nil {
		return err
	}
	*a = parsed
	return nil
}

// eip55Hex computes the canonical EIP-55 checksummed form of a.
func (a Address) eip55Hex() string {
	lower := hex.EncodeToString(a[:])

	hasher := sha3.NewLegacyKeccak256()
	// Keccak is applied over the ASCII bytes of the lowercase hex, not
	// the raw address bytes. This is what every Ethereum client does.
	_, _ = hasher.Write([]byte(lower))
	digest := hasher.Sum(nil)

	out := []byte("0x")
	for i := 0; i < len(lower); i++ {
		c := lower[i]
		if c >= 'a' && c <= 'f' {
			// Each hash byte covers two hex nibbles (high nibble first).
			var nibble byte
			if i%2 == 0 {
				nibble = digest[i/2] >> 4
			} else {
				nibble = digest[i/2] & 0x0f
			}
			if nibble >= 8 {
				c = c - 'a' + 'A'
			}
		}
		out = append(out, c)
	}
	return string(out)
}

// hasMixedCase reports whether s contains at least one lowercase a-f
// AND one uppercase A-F. Pure-digit strings and single-case strings
// return false — EIP-55 treats them as unchecked.
func hasMixedCase(s string) bool {
	hasLower, hasUpper := false, false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'a' && c <= 'f' {
			hasLower = true
		}
		if c >= 'A' && c <= 'F' {
			hasUpper = true
		}
	}
	return hasLower && hasUpper
}
