package rpc

import (
	"encoding/hex"
	"fmt"
	"math/big"
	"strconv"
	"strings"

	"github.com/seokheejang/chain-sync-watch/internal/chain"
	"github.com/seokheejang/chain-sync-watch/internal/source"
)

// parseHexUint64 decodes an RPC-canonical "0xN" string into uint64.
// Missing "0x" prefix or non-hex characters are treated as invalid
// responses rather than parse errors so the caller surfaces a single
// sentinel to the application layer.
func parseHexUint64(s string) (uint64, error) {
	raw, err := trimHexPrefix(s)
	if err != nil {
		return 0, err
	}
	if raw == "" {
		// Geth occasionally emits "0x" for a zero quantity; accept it.
		return 0, nil
	}
	n, err := strconv.ParseUint(raw, 16, 64)
	if err != nil {
		return 0, fmt.Errorf("%w: parse uint64 %q: %v", source.ErrInvalidResponse, s, err)
	}
	return n, nil
}

// parseHexBigInt decodes an RPC-canonical "0xN" string into *big.Int.
// Unlike uint64 parsing, big-int parsing has to accept the empty
// "0x" zero form too.
func parseHexBigInt(s string) (*big.Int, error) {
	raw, err := trimHexPrefix(s)
	if err != nil {
		return nil, err
	}
	if raw == "" {
		return new(big.Int), nil
	}
	v, ok := new(big.Int).SetString(raw, 16)
	if !ok {
		return nil, fmt.Errorf("%w: parse bigint %q", source.ErrInvalidResponse, s)
	}
	return v, nil
}

// parseHash32 decodes a 32-byte hex hash from an RPC response. The
// input must be the canonical 66-character "0x..." form.
func parseHash32(s string) (chain.Hash32, error) {
	h, err := chain.NewHash32(s)
	if err != nil {
		return chain.Hash32{}, fmt.Errorf("%w: %v", source.ErrInvalidResponse, err)
	}
	return h, nil
}

// parseAddress decodes a 20-byte hex address. RPC responses are
// lowercase by convention; chain.NewAddress accepts that.
func parseAddress(s string) (chain.Address, error) {
	a, err := chain.NewAddress(s)
	if err != nil {
		return chain.Address{}, fmt.Errorf("%w: %v", source.ErrInvalidResponse, err)
	}
	return a, nil
}

// trimHexPrefix validates and strips the "0x" prefix. Returns the
// empty string if the input is exactly "0x" (a legal zero quantity);
// callers interpret that as the value zero.
func trimHexPrefix(s string) (string, error) {
	if !strings.HasPrefix(s, "0x") && !strings.HasPrefix(s, "0X") {
		return "", fmt.Errorf("%w: missing 0x prefix: %q", source.ErrInvalidResponse, s)
	}
	return s[2:], nil
}

// hexBytes decodes a hex string (with or without 0x prefix) into its
// raw bytes. Used for eth_call return data decoding.
func hexBytes(s string) ([]byte, error) {
	raw := s
	if strings.HasPrefix(raw, "0x") || strings.HasPrefix(raw, "0X") {
		raw = raw[2:]
	}
	if raw == "" {
		return nil, nil
	}
	// Pad odd-length strings by prepending a zero so hex.DecodeString
	// does not reject responses like "0xf" that geth occasionally
	// emits for single-nibble quantities.
	if len(raw)%2 == 1 {
		raw = "0" + raw
	}
	b, err := hex.DecodeString(raw)
	if err != nil {
		return nil, fmt.Errorf("%w: decode hex: %v", source.ErrInvalidResponse, err)
	}
	return b, nil
}
