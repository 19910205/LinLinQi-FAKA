package security

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
)

// Vault provides application-level envelope encryption for card secrets and credentials.
// Production deployments should inject the root secret from a KMS-backed secret manager.
type Vault struct {
	aead cipher.AEAD
	key  []byte
}

func NewVault(secret string) (*Vault, error) {
	if len(secret) < 24 {
		return nil, fmt.Errorf("data encryption key must contain at least 24 characters")
	}
	sum := sha256.Sum256([]byte(secret))
	block, err := aes.NewCipher(sum[:])
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Vault{aead: aead, key: sum[:]}, nil
}

func (v *Vault) Encrypt(plaintext string, associatedData []byte) ([]byte, []byte, string, error) {
	nonce := make([]byte, v.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, nil, "", err
	}
	ciphertext := v.aead.Seal(nil, nonce, []byte(plaintext), associatedData)
	mac := hmac.New(sha256.New, v.key)
	mac.Write(associatedData)
	mac.Write([]byte{0})
	mac.Write([]byte(plaintext))
	return ciphertext, nonce, hex.EncodeToString(mac.Sum(nil)), nil
}

func (v *Vault) Decrypt(ciphertext, nonce, associatedData []byte) (string, error) {
	plaintext, err := v.aead.Open(nil, nonce, ciphertext, associatedData)
	if err != nil {
		return "", fmt.Errorf("decrypt secret: %w", err)
	}
	return string(plaintext), nil
}

func SecretPreview(value string) string {
	runes := []rune(value)
	if len(runes) <= 8 {
		return "••••••••"
	}
	return string(runes[:4]) + "••••" + string(runes[len(runes)-4:])
}
