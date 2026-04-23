package secrets_test

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/seokheejang/chain-sync-watch/internal/secrets"
)

// testKey is a fixed 32-byte key used by every test so encrypt/
// decrypt assertions stay deterministic. Tests never touch the
// process-wide CSW_SECRET_KEY env var.
var testKey = bytes.Repeat([]byte{0xAB}, 32)

func TestNewCipher_KeyLengthValidation(t *testing.T) {
	cases := []struct {
		name string
		size int
		ok   bool
	}{
		{"too short", 16, false},
		{"too long", 64, false},
		{"exactly 32", 32, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := secrets.NewCipher(bytes.Repeat([]byte{1}, tc.size))
			if tc.ok {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}

func TestCipher_RoundTrip(t *testing.T) {
	c, err := secrets.NewCipher(testKey)
	require.NoError(t, err)

	plain := []byte("my-etherscan-api-key-ABCD")
	ct, nonce, err := c.Encrypt(plain)
	require.NoError(t, err)
	require.NotEqual(t, plain, ct)
	require.Len(t, nonce, 12)

	out, err := c.Decrypt(ct, nonce)
	require.NoError(t, err)
	require.Equal(t, plain, out)
}

func TestCipher_NonceUniqueness(t *testing.T) {
	c, err := secrets.NewCipher(testKey)
	require.NoError(t, err)

	seen := map[string]struct{}{}
	for i := 0; i < 1000; i++ {
		_, nonce, err := c.Encrypt([]byte("x"))
		require.NoError(t, err)
		key := string(nonce)
		_, dup := seen[key]
		require.Falsef(t, dup, "nonce collision at iter %d", i)
		seen[key] = struct{}{}
	}
}

func TestCipher_EmptyPlaintextRejected(t *testing.T) {
	c, err := secrets.NewCipher(testKey)
	require.NoError(t, err)

	_, _, err = c.Encrypt(nil)
	require.Error(t, err)
	_, _, err = c.Encrypt([]byte{})
	require.Error(t, err)
}

func TestCipher_TamperDetected(t *testing.T) {
	c, err := secrets.NewCipher(testKey)
	require.NoError(t, err)

	ct, nonce, err := c.Encrypt([]byte("secret"))
	require.NoError(t, err)

	tampered := make([]byte, len(ct))
	copy(tampered, ct)
	tampered[0] ^= 0xFF
	_, err = c.Decrypt(tampered, nonce)
	require.Error(t, err, "flipped ciphertext bit must surface")

	badNonce := make([]byte, len(nonce))
	copy(badNonce, nonce)
	badNonce[0] ^= 0xFF
	_, err = c.Decrypt(ct, badNonce)
	require.Error(t, err, "flipped nonce byte must surface")
}

func TestCipher_WrongKeyCantDecrypt(t *testing.T) {
	c1, _ := secrets.NewCipher(testKey)
	c2, _ := secrets.NewCipher(bytes.Repeat([]byte{0xCD}, 32))

	ct, nonce, err := c1.Encrypt([]byte("top-secret"))
	require.NoError(t, err)

	_, err = c2.Decrypt(ct, nonce)
	require.Error(t, err)
}

func TestCipher_NonceLengthValidation(t *testing.T) {
	c, err := secrets.NewCipher(testKey)
	require.NoError(t, err)
	_, err = c.Decrypt([]byte("xx"), []byte("too-short"))
	require.Error(t, err)
}

func TestLoad_MissingEnv(t *testing.T) {
	t.Setenv(secrets.EnvKeyName, "")
	_, err := secrets.Load()
	require.Error(t, err)
}

func TestLoad_InvalidBase64(t *testing.T) {
	t.Setenv(secrets.EnvKeyName, "!!not-base64!!")
	_, err := secrets.Load()
	require.Error(t, err)
}

func TestLoad_WrongKeySize(t *testing.T) {
	// Valid base64, but decodes to only 16 bytes.
	t.Setenv(secrets.EnvKeyName, "YWJjZGVmZ2hpamtsbW5vcA==")
	_, err := secrets.Load()
	require.Error(t, err)
}
