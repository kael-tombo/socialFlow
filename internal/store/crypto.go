package store

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
)

// Sealer encrypts provider tokens at rest with AES-256-GCM.
type Sealer struct{ aead cipher.AEAD }

// NewSealer takes a 32-byte key as 64 hex characters.
func NewSealer(hexKey string) (*Sealer, error) {
	key, err := hex.DecodeString(hexKey)
	if err != nil || len(key) != 32 {
		return nil, errors.New("ENCRYPTION_KEY must be 64 hex characters (32 bytes); generate one with `openssl rand -hex 32`")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Sealer{aead: aead}, nil
}

func (s *Sealer) Seal(plain string) (string, error) {
	nonce := make([]byte, s.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(s.aead.Seal(nonce, nonce, []byte(plain), nil)), nil
}

func (s *Sealer) Open(sealed string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(sealed)
	if err != nil || len(raw) < s.aead.NonceSize() {
		return "", errors.New("sealed value is malformed")
	}
	n := s.aead.NonceSize()
	plain, err := s.aead.Open(nil, raw[:n], raw[n:], nil)
	if err != nil {
		return "", fmt.Errorf("decrypting: %w", err)
	}
	return string(plain), nil
}

// HashKey is how API keys are stored: only the SHA-256 digest is kept.
func HashKey(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}

// RandomID returns prefix + 24 hex characters.
func RandomID(prefix string) string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return prefix + hex.EncodeToString(b)
}

// NewAPIKey returns a fresh secret key: "sfl_" + 43 url-safe characters.
func NewAPIKey() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return "sfl_" + base64.RawURLEncoding.EncodeToString(b)
}
