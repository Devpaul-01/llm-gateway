package crypto

import (
	"bytes"
	"testing"
)

func TestEncryptDecrypt_RoundTrip(t *testing.T) {
	key := make([]byte, 32) // AES-256 key size
	plaintext := []byte("gsk_super_secret_api_key")

	ciphertext, nonce, err := Encrypt(plaintext, key)
	if err != nil {
		t.Fatalf("unexpected encrypt error: %v", err)
	}

	decrypted, err := Decrypt(ciphertext, nonce, key)
	if err != nil {
		t.Fatalf("unexpected decrypt error: %v", err)
	}

	if !bytes.Equal(decrypted, plaintext) {
		t.Errorf("decrypted plaintext does not match original: got %q, want %q", decrypted, plaintext)
	}
}

// TestDecrypt_DetectsTamperedCiphertext verifies the GCM authentication tag
// actually does its job: AES-GCM was specifically chosen (Architecture
// Section 6.1) because a tampered ciphertext should fail to decrypt rather
// than silently produce corrupted plaintext. This test exists because
// round-trip success alone cannot distinguish "GCM is really verifying
// integrity" from "GCM mode was accidentally swapped for something that
// isn't actually authenticating."
func TestDecrypt_DetectsTamperedCiphertext(t *testing.T) {
	key := make([]byte, 32)
	plaintext := []byte("gsk_super_secret_api_key")

	ciphertext, nonce, err := Encrypt(plaintext, key)
	if err != nil {
		t.Fatalf("unexpected encrypt error: %v", err)
	}

	tampered := make([]byte, len(ciphertext))
	copy(tampered, ciphertext)
	tampered[0] ^= 0xFF // flip a bit in the ciphertext

	if _, err := Decrypt(tampered, nonce, key); err == nil {
		t.Error("expected Decrypt to fail on tampered ciphertext, but it succeeded")
	}
}

// TestDecrypt_DetectsTamperedNonce verifies that a mismatched/corrupted
// nonce also fails closed rather than silently decrypting into garbage.
func TestDecrypt_DetectsTamperedNonce(t *testing.T) {
	key := make([]byte, 32)
	plaintext := []byte("gsk_super_secret_api_key")

	ciphertext, nonce, err := Encrypt(plaintext, key)
	if err != nil {
		t.Fatalf("unexpected encrypt error: %v", err)
	}

	tamperedNonce := make([]byte, len(nonce))
	copy(tamperedNonce, nonce)
	tamperedNonce[0] ^= 0xFF

	if _, err := Decrypt(ciphertext, tamperedNonce, key); err == nil {
		t.Error("expected Decrypt to fail on tampered nonce, but it succeeded")
	}
}

// TestDecrypt_DetectsWrongKey verifies decryption with the wrong key
// fails rather than returning corrupted plaintext.
func TestDecrypt_DetectsWrongKey(t *testing.T) {
	key := make([]byte, 32)
	wrongKey := make([]byte, 32)
	wrongKey[0] = 0x01 // ensure it differs from the zero-value key

	plaintext := []byte("gsk_super_secret_api_key")

	ciphertext, nonce, err := Encrypt(plaintext, key)
	if err != nil {
		t.Fatalf("unexpected encrypt error: %v", err)
	}

	if _, err := Decrypt(ciphertext, nonce, wrongKey); err == nil {
		t.Error("expected Decrypt to fail with the wrong key, but it succeeded")
	}
}