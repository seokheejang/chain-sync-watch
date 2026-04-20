package chain_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/seokheejang/chain-sync-watch/internal/chain"
)

// Synthetic 32-byte hash used as a well-formed test vector. Mixed hex
// digits ensure we exercise encoding paths without tying the test to
// any real-chain observation.
const sampleHashLower = "0xabcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"

func TestHash32_NewHash32_Valid(t *testing.T) {
	cases := []string{
		"0x0000000000000000000000000000000000000000000000000000000000000000",
		sampleHashLower,
		// Uppercase hex is valid — hashes aren't EIP-55 case-sensitive.
		"0x" + "ABCDEF0123456789ABCDEF0123456789ABCDEF0123456789ABCDEF0123456789",
		// Mixed case also accepted (hash strings are unchecked).
		"0xAbCdEf0123456789aBcDeF0123456789AbCdEf0123456789aBcDeF0123456789",
	}
	for _, s := range cases {
		t.Run(s[:10]+"...", func(t *testing.T) {
			h, err := chain.NewHash32(s)
			require.NoError(t, err)
			// Hex() always returns lowercase — canonical form.
			require.Len(t, h.Hex(), 66)
			require.Equal(t, "0x", h.Hex()[:2])
		})
	}
}

func TestHash32_Hex_IsLowercase(t *testing.T) {
	h, err := chain.NewHash32("0xABCDEF0123456789ABCDEF0123456789ABCDEF0123456789ABCDEF0123456789")
	require.NoError(t, err)
	require.Equal(t,
		"0xabcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789",
		h.Hex(),
	)
}

func TestHash32_NewHash32_Invalid(t *testing.T) {
	cases := []struct {
		name string
		in   string
	}{
		{"empty", ""},
		{"missing 0x", "99b8da780155e8770edfe7d43f96c1f722234984d5cfdb4630d5445d26e9884f"},
		{"too short", "0xabcd"},
		{"too long", sampleHashLower + "00"},
		{"non-hex char", "0xabcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456GZ"},
		{"only prefix", "0x"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := chain.NewHash32(tc.in)
			require.Error(t, err, "input %q must be rejected", tc.in)
		})
	}
}

func TestHash32_String_MatchesHex(t *testing.T) {
	h, err := chain.NewHash32(sampleHashLower)
	require.NoError(t, err)
	require.Equal(t, h.Hex(), h.String())
}

func TestHash32_IsZero(t *testing.T) {
	var zero chain.Hash32
	require.True(t, zero.IsZero())

	nonZero, err := chain.NewHash32(sampleHashLower)
	require.NoError(t, err)
	require.False(t, nonZero.IsZero())

	allZero, err := chain.NewHash32("0x" + "00" + "00000000000000000000000000000000000000000000000000000000000000")
	require.NoError(t, err)
	require.True(t, allZero.IsZero())
}

func TestHash32_JSON_RoundTrip(t *testing.T) {
	orig, err := chain.NewHash32(sampleHashLower)
	require.NoError(t, err)

	data, err := json.Marshal(orig)
	require.NoError(t, err)
	require.JSONEq(t, `"`+sampleHashLower+`"`, string(data))

	var back chain.Hash32
	require.NoError(t, json.Unmarshal(data, &back))
	require.Equal(t, orig, back)
}

func TestHash32_UnmarshalJSON_Rejects(t *testing.T) {
	cases := []string{
		`null`,
		`""`,
		`"0x"`,
		`"0xabcd"`,
		`"not-a-hash"`,
		`123`,
		`{}`,
	}
	for _, in := range cases {
		t.Run(in, func(t *testing.T) {
			var h chain.Hash32
			require.Error(t, json.Unmarshal([]byte(in), &h))
		})
	}
}

// TxHash and BlockHash are aliases of Hash32 — this test locks in that
// aliasing so callers can assign between the names without conversions.
func TestTxHash_BlockHash_AliasOfHash32(t *testing.T) {
	h, err := chain.NewHash32(sampleHashLower)
	require.NoError(t, err)

	// Assignment works both directions without conversion — compiles
	// only if the types are true aliases (type X = Hash32). The
	// explicit type annotations here are the subject under test, so we
	// intentionally disable staticcheck ST1023 (which would ask us to
	// drop them) — dropping them would defeat the aliasing assertion.
	var tx chain.TxHash = h    //nolint:staticcheck // ST1023: explicit type is the assertion.
	var bh chain.BlockHash = h //nolint:staticcheck // ST1023: explicit type is the assertion.
	require.Equal(t, h, tx)
	require.Equal(t, h, bh)

	// Same the other way.
	var back chain.Hash32 = tx //nolint:staticcheck // ST1023: explicit type is the assertion.
	require.Equal(t, h, back)
}
