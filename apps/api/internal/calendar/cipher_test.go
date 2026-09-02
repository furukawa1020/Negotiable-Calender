package calendar

import (
	"encoding/base64"
	"testing"
)

func TestTokenCipherRoundTripAndTamperDetection(t *testing.T) {
	key := base64.StdEncoding.EncodeToString(make([]byte, 32))
	value, err := NewTokenCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	encrypted, err := value.Encrypt("refresh-secret")
	if err != nil {
		t.Fatal(err)
	}
	if string(encrypted) == "refresh-secret" {
		t.Fatal("refresh token stored as plaintext")
	}
	decrypted, err := value.Decrypt(encrypted)
	if err != nil || decrypted != "refresh-secret" {
		t.Fatalf("round trip failed: value=%q err=%v", decrypted, err)
	}
	encrypted[len(encrypted)-1] ^= 1
	if _, err := value.Decrypt(encrypted); err == nil {
		t.Fatal("tampered ciphertext was accepted")
	}
}

func TestTokenCipherRejectsInvalidKey(t *testing.T) {
	if _, err := NewTokenCipher(base64.StdEncoding.EncodeToString(make([]byte, 16))); err == nil {
		t.Fatal("short encryption key was accepted")
	}
}
