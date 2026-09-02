package calendar

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
)

type TokenCipher struct{ aead cipher.AEAD }

func NewTokenCipher(encodedKey string) (*TokenCipher, error) {
	key, err := base64.StdEncoding.DecodeString(encodedKey)
	if err != nil || len(key) != 32 {
		return nil, fmt.Errorf("CALENDAR_TOKEN_ENCRYPTION_KEY must be a base64-encoded 32-byte key")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("create token cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create token AEAD: %w", err)
	}
	return &TokenCipher{aead: aead}, nil
}

func (value *TokenCipher) Encrypt(plaintext string) ([]byte, error) {
	nonce := make([]byte, value.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("create token nonce: %w", err)
	}
	return value.aead.Seal(nonce, nonce, []byte(plaintext), nil), nil
}

func (value *TokenCipher) Decrypt(ciphertext []byte) (string, error) {
	if len(ciphertext) < value.aead.NonceSize() {
		return "", fmt.Errorf("invalid encrypted token")
	}
	nonce := ciphertext[:value.aead.NonceSize()]
	plaintext, err := value.aead.Open(nil, nonce, ciphertext[value.aead.NonceSize():], nil)
	if err != nil {
		return "", fmt.Errorf("decrypt token: %w", err)
	}
	return string(plaintext), nil
}
