package chain_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/seokheejang/chain-sync-watch/internal/chain"
)

func TestBlockNumber_Uint64(t *testing.T) {
	require.Equal(t, uint64(0), chain.NewBlockNumber(0).Uint64())
	require.Equal(t, uint64(10), chain.NewBlockNumber(10).Uint64())
	require.Equal(t, uint64(150449305), chain.NewBlockNumber(150449305).Uint64())
}

func TestBlockNumber_Hex(t *testing.T) {
	// Ethereum JSON-RPC canonical form: lowercase, no leading zeros,
	// "0x0" for zero (not "0x" or "0x00").
	tests := []struct {
		n    uint64
		want string
	}{
		{0, "0x0"},
		{1, "0x1"},
		{10, "0xa"},
		{255, "0xff"},
		{4096, "0x1000"},
		{150449305, "0x8f7ac99"},
	}
	for _, tc := range tests {
		t.Run(tc.want, func(t *testing.T) {
			require.Equal(t, tc.want, chain.NewBlockNumber(tc.n).Hex())
		})
	}
}

func TestBlockNumber_MarshalJSON_EmitsHexString(t *testing.T) {
	// Always round-trip to hex on the wire so consumers that hit RPC
	// directly receive a spec-compliant value.
	data, err := json.Marshal(chain.NewBlockNumber(10))
	require.NoError(t, err)
	require.JSONEq(t, `"0xa"`, string(data))

	data, err = json.Marshal(chain.NewBlockNumber(0))
	require.NoError(t, err)
	require.JSONEq(t, `"0x0"`, string(data))
}

func TestBlockNumber_UnmarshalJSON_AcceptsNumberAndHex(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want uint64
	}{
		{"decimal number", `10`, 10},
		{"zero number", `0`, 0},
		{"big decimal number", `150449305`, 150449305},
		{"hex lowercase", `"0xa"`, 10},
		{"hex uppercase", `"0xA"`, 10},
		{"hex zero", `"0x0"`, 0},
		{"hex big", `"0x8f7ac99"`, 150449305},
		{"hex mixed case prefix", `"0X1f"`, 31},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var bn chain.BlockNumber
			require.NoError(t, json.Unmarshal([]byte(tc.in), &bn))
			require.Equal(t, tc.want, bn.Uint64())
		})
	}
}

func TestBlockNumber_UnmarshalJSON_Rejects(t *testing.T) {
	cases := []struct {
		name string
		in   string
	}{
		{"negative number", `-1`},
		{"null", `null`},
		{"decimal string (ambiguous)", `"10"`},
		{"empty string", `""`},
		{"hex without prefix", `"abc"`},
		{"malformed hex", `"0xzz"`},
		{"hex too large for uint64", `"0x10000000000000000"`},
		{"json object", `{}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var bn chain.BlockNumber
			require.Error(t, json.Unmarshal([]byte(tc.in), &bn),
				"input %s must be rejected", tc.in)
		})
	}
}

// Round-trip: marshal → unmarshal must give back the same value.
func TestBlockNumber_JSON_RoundTrip(t *testing.T) {
	for _, n := range []uint64{0, 1, 10, 255, 150449305, 1<<63 - 1} {
		original := chain.NewBlockNumber(n)
		data, err := json.Marshal(original)
		require.NoError(t, err)

		var restored chain.BlockNumber
		require.NoError(t, json.Unmarshal(data, &restored))
		require.Equal(t, original, restored)
	}
}
