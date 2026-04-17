// Package crypto provides AES-256-GCM encryption/decryption helpers
// for workspace secrets (per PRD §11.4).
//
// The master key is 32 bytes and must be provided by the caller
// (typically from MYTEAM_SECRET_KEY env var, base64-decoded).
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
)

// KeySize is the required master-key length in bytes (AES-256).
const KeySize = 32

var (
	ErrInvalidKey     = errors.New("crypto: master key must be exactly 32 bytes")
	ErrCipherTooShort = errors.New("crypto: ciphertext too short")
)

// Encrypt seals plaintext under key with AES-256-GCM. Output layout is:
//
//	nonce (12 bytes) || ciphertext || tag (16 bytes)
//
// Nonce is freshly randomized; output is self-contained.
func Encrypt(plaintext, key []byte) ([]byte, error) {
	if len(key) != KeySize {
		return nil, ErrInvalidKey
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("new cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("new gcm: %w", err)
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("read nonce: %w", err)
	}
	// Seal appends the tag; prepend nonce so decrypt can find it.
	out := make([]byte, 0, len(nonce)+len(plaintext)+aead.Overhead())
	out = append(out, nonce...)
	out = aead.Seal(out, nonce, plaintext, nil)
	return out, nil
}

// Decrypt inverses Encrypt. Returns an error on any tag mismatch.
func Decrypt(ciphertext, key []byte) ([]byte, error) {
	if len(key) != KeySize {
		return nil, ErrInvalidKey
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("new cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("new gcm: %w", err)
	}
	if len(ciphertext) < aead.NonceSize() {
		return nil, ErrCipherTooShort
	}
	nonce := ciphertext[:aead.NonceSize()]
	body := ciphertext[aead.NonceSize():]
	return aead.Open(nil, nonce, body, nil)
}
