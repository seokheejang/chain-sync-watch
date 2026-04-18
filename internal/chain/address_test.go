package chain_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/seokheejang/chain-sync-watch/internal/chain"
)

// Canonical EIP-55 addresses from the spec's own test vectors
// (https://eips.ethereum.org/EIPS/eip-55).
var eip55Vectors = []string{
	"0x52908400098527886E0F7030069857D2E4169EE7",
	"0x8617E340B3D01FA5F11F306F4090FD50E238070D",
	"0xde709f2102306220921060314715629080e2fb77",
	"0x27b1fdb04752bbc536007a920d24acb045561c26",
	"0x5aAeb6053F3E94C9b9A09f33669435E7Ef1BeAed",
	"0xfB6916095ca1df60bB79Ce92cE3Ea74c37c5d359",
	"0xdbF03B407c01E7cD3CBea99509d93f8DDDC8C6FB",
	"0xD1220A0cf47c7B9Be7A2E6BA89F429762e7b9aDb",
}

func TestAddress_NewAddress_Canonical(t *testing.T) {
	for _, want := range eip55Vectors {
		t.Run(want, func(t *testing.T) {
			addr, err := chain.NewAddress(want)
			require.NoError(t, err)
			require.Equal(t, want, addr.Hex(), "round-trip must preserve EIP-55 casing")
		})
	}
}

func TestAddress_NewAddress_AcceptsAllLower(t *testing.T) {
	// All-lowercase is valid input per the EIP: absence of case signals
	// "unchecked". NewAddress must accept it and produce the canonical
	// checksummed form on Hex().
	addr, err := chain.NewAddress("0xde709f2102306220921060314715629080e2fb77")
	require.NoError(t, err)
	require.Equal(t, "0xde709f2102306220921060314715629080e2fb77", addr.Hex(),
		"this vector is already canonical lowercase")

	// Another vector whose canonical form has mixed case.
	addr, err = chain.NewAddress("0x5aaeb6053f3e94c9b9a09f33669435e7ef1beaed")
	require.NoError(t, err)
	require.Equal(t, "0x5aAeb6053F3E94C9b9A09f33669435E7Ef1BeAed", addr.Hex())
}

func TestAddress_NewAddress_AcceptsAllUpper(t *testing.T) {
	// All-uppercase hex portion is also unchecked per EIP-55.
	addr, err := chain.NewAddress("0X5AAEB6053F3E94C9B9A09F33669435E7EF1BEAED")
	require.NoError(t, err)
	require.Equal(t, "0x5aAeb6053F3E94C9b9A09f33669435E7Ef1BeAed", addr.Hex())
}

func TestAddress_NewAddress_RejectsInvalid(t *testing.T) {
	cases := []struct {
		name string
		in   string
	}{
		{"empty", ""},
		{"missing 0x prefix", "5aAeb6053F3E94C9b9A09f33669435E7Ef1BeAed"},
		{"too short", "0x5aAeb6053F3E94C9b9A09f33669435E7Ef1BeAe"},
		{"too long", "0x5aAeb6053F3E94C9b9A09f33669435E7Ef1BeAed00"},
		{"non-hex char", "0x5aAeb6053F3E94C9b9A09f33669435E7Ef1BeAeZ"},
		{"only prefix", "0x"},
		{"checksum mismatch", "0x5AAEb6053F3E94C9b9A09f33669435E7Ef1BeAed"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := chain.NewAddress(tc.in)
			require.Error(t, err, "input %q must be rejected", tc.in)
		})
	}
}

func TestAddress_MustAddress_PanicsOnInvalid(t *testing.T) {
	require.Panics(t, func() { chain.MustAddress("not-an-address") })
	require.NotPanics(t, func() { chain.MustAddress(eip55Vectors[0]) })
}

func TestAddress_Equality(t *testing.T) {
	// Same address parsed from different casings must be byte-equal.
	a1, err := chain.NewAddress("0x5aAeb6053F3E94C9b9A09f33669435E7Ef1BeAed")
	require.NoError(t, err)
	a2, err := chain.NewAddress("0x5aaeb6053f3e94c9b9a09f33669435e7ef1beaed")
	require.NoError(t, err)
	a3, err := chain.NewAddress("0X5AAEB6053F3E94C9B9A09F33669435E7EF1BEAED")
	require.NoError(t, err)
	require.Equal(t, a1, a2)
	require.Equal(t, a1, a3)
}

func TestAddress_String_MatchesHex(t *testing.T) {
	addr, err := chain.NewAddress("0x5aAeb6053F3E94C9b9A09f33669435E7Ef1BeAed")
	require.NoError(t, err)
	require.Equal(t, addr.Hex(), addr.String())
}

func TestAddress_JSON_RoundTrip(t *testing.T) {
	orig, err := chain.NewAddress("0x5aAeb6053F3E94C9b9A09f33669435E7Ef1BeAed")
	require.NoError(t, err)

	data, err := json.Marshal(orig)
	require.NoError(t, err)
	require.JSONEq(t, `"0x5aAeb6053F3E94C9b9A09f33669435E7Ef1BeAed"`, string(data))

	var back chain.Address
	require.NoError(t, json.Unmarshal(data, &back))
	require.Equal(t, orig, back)
}

func TestAddress_UnmarshalJSON_Rejects(t *testing.T) {
	cases := []string{
		`null`,
		`""`,
		`"0x"`,
		`"notanaddress"`,
		`"0xZZZZb6053F3E94C9b9A09f33669435E7Ef1BeAed"`,
		`123`,
		`{}`,
	}
	for _, in := range cases {
		t.Run(in, func(t *testing.T) {
			var a chain.Address
			require.Error(t, json.Unmarshal([]byte(in), &a))
		})
	}
}

func TestAddress_IsZero(t *testing.T) {
	var zero chain.Address
	require.True(t, zero.IsZero())

	addr, err := chain.NewAddress("0x0000000000000000000000000000000000000000")
	require.NoError(t, err)
	require.True(t, addr.IsZero())

	nonZero, err := chain.NewAddress("0x5aAeb6053F3E94C9b9A09f33669435E7Ef1BeAed")
	require.NoError(t, err)
	require.False(t, nonZero.IsZero())
}
