// Package secrets wraps AES-GCM for encrypting the 3rd-party API
// credentials the sources table stores. The master key is read once
// at process start from the CSW_SECRET_KEY env var (base64, 32
// bytes); binaries refuse to boot without it so a misconfigured
// deployment surfaces immediately rather than at first decrypt.
//
// Threat model:
//
//   - An attacker who gets a DB dump sees ciphertext + nonce only.
//     Without CSW_SECRET_KEY they cannot recover the stored api_key.
//   - An attacker who gets CSW_SECRET_KEY but no DB cannot harm us.
//   - An attacker who gets both is already root — that is an
//     operational failure outside this layer's remit.
//
// Key rotation is a follow-up. The MVP contract: CSW_SECRET_KEY is
// stable for the lifetime of the sources table contents. A future
// `csw rotate-secrets <old> <new>` subcommand will re-encrypt every
// row in place.
package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
)

// EnvKeyName is the env var read at Load time. Kept exported so
// main binaries and tests reference the same constant.
const EnvKeyName = "CSW_SECRET_KEY"

// keyBytes is the required master-key length. AES-256 (32 bytes)
// balances current best practice with no practical downside — and
// matches the `openssl rand -base64 32` guidance in .env.example.
const keyBytes = 32

// Cipher is the stateless encrypt/decrypt surface. A single instance
// is built at boot and shared across goroutines; AEAD backends are
// safe for concurrent use.
type Cipher struct {
	aead cipher.AEAD
}

// Load reads CSW_SECRET_KEY, base64-decodes it, and constructs a
// ready-to-use Cipher. Any failure (missing var, wrong length,
// invalid base64) returns an error — callers abort startup.
func Load() (*Cipher, error) {
	raw := os.Getenv(EnvKeyName)
	if raw == "" {
		return nil, fmt.Errorf("secrets: %s is not set", EnvKeyName)
	}
	key, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		return nil, fmt.Errorf("secrets: %s is not valid base64: %w", EnvKeyName, err)
	}
	return NewCipher(key)
}

// NewCipher builds a Cipher from a 32-byte key. Tests use this
// directly with a deterministic key so they don't depend on the
// process environment.
func NewCipher(key []byte) (*Cipher, error) {
	if len(key) != keyBytes {
		return nil, fmt.Errorf("secrets: key must be %d bytes, got %d", keyBytes, len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("secrets: new cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("secrets: new gcm: %w", err)
	}
	return &Cipher{aead: aead}, nil
}

// Encrypt seals plaintext into (ciphertext, nonce) using a fresh
// random nonce. The nonce is safe to store alongside the ciphertext
// — GCM security survives nonce disclosure as long as the nonce is
// unique per (key, plaintext) pair.
//
// Empty plaintext is rejected: an empty api_key is almost always a
// bug, and refusing it upstream keeps "no credentials" modelled as
// NULL DB columns rather than a zero-length encrypted blob.
func (c *Cipher) Encrypt(plaintext []byte) (ciphertext []byte, nonce []byte, err error) {
	if len(plaintext) == 0 {
		return nil, nil, errors.New("secrets: refuse to encrypt empty plaintext")
	}
	nonce = make([]byte, c.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, nil, fmt.Errorf("secrets: rand nonce: %w", err)
	}
	ciphertext = c.aead.Seal(nil, nonce, plaintext, nil)
	return ciphertext, nonce, nil
}

// Decrypt reverses Encrypt. Any tampering (flipped bit in ciphertext
// or nonce, truncated payload, wrong key) surfaces as an error —
// GCM authenticates the whole package.
func (c *Cipher) Decrypt(ciphertext, nonce []byte) ([]byte, error) {
	if len(nonce) != c.aead.NonceSize() {
		return nil, fmt.Errorf("secrets: nonce must be %d bytes, got %d", c.aead.NonceSize(), len(nonce))
	}
	plain, err := c.aead.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("secrets: decrypt: %w", err)
	}
	return plain, nil
}
