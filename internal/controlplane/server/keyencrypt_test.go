package server

import (
	"strings"
	"testing"
)

func TestEncryptDecryptPEMRoundTrip(t *testing.T) {
	original := "-----BEGIN EC PRIVATE KEY-----\nMIGkAgEBBDDfake+key+data+here==\n-----END EC PRIVATE KEY-----\n"
	passphrase := "test-passphrase-32-bytes-long!!!"

	encrypted, err := encryptPEM(original, passphrase)
	if err != nil {
		t.Fatalf("encryptPEM() error = %v", err)
	}

	if !strings.HasPrefix(encrypted, encryptedPEMPrefixV2) {
		t.Fatalf("encryptPEM() should produce ENC2: prefix, got %q", encrypted[:10])
	}

	decrypted, err := decryptPEM(encrypted, passphrase)
	if err != nil {
		t.Fatalf("decryptPEM() error = %v", err)
	}

	if decrypted != original {
		t.Fatalf("decryptPEM() = %q, want %q", decrypted, original)
	}
}

func TestDecryptPEMPlaintextPassthrough(t *testing.T) {
	plainPEM := "-----BEGIN EC PRIVATE KEY-----\nMIGkAgEBBDDfake+key+data+here==\n-----END EC PRIVATE KEY-----\n"

	result, err := decryptPEM(plainPEM, "any-passphrase")
	if err != nil {
		t.Fatalf("decryptPEM() error = %v", err)
	}

	if result != plainPEM {
		t.Fatalf("decryptPEM() = %q, want original plaintext PEM", result)
	}
}

func TestDecryptPEMPlaintextWithoutPassphrase(t *testing.T) {
	plainPEM := "-----BEGIN EC PRIVATE KEY-----\nMIGkAgEBBDDfake+key+data+here==\n-----END EC PRIVATE KEY-----\n"

	result, err := decryptPEM(plainPEM, "")
	if err != nil {
		t.Fatalf("decryptPEM() error = %v", err)
	}

	if result != plainPEM {
		t.Fatalf("decryptPEM() = %q, want original plaintext PEM", result)
	}
}

func TestDecryptPEMWrongKey(t *testing.T) {
	original := "-----BEGIN EC PRIVATE KEY-----\nMIGkAgEBBDDfake+key+data+here==\n-----END EC PRIVATE KEY-----\n"

	encrypted, err := encryptPEM(original, "correct-key")
	if err != nil {
		t.Fatalf("encryptPEM() error = %v", err)
	}

	_, err = decryptPEM(encrypted, "wrong-key")
	if err == nil {
		t.Fatal("decryptPEM() with wrong key should return an error")
	}
}

func TestDecryptPEMLegacyV1Format(t *testing.T) {
	original := "-----BEGIN EC PRIVATE KEY-----\nMIGkAgEBBDDfake+key+data+here==\n-----END EC PRIVATE KEY-----\n"
	passphrase := "legacy-passphrase"

	// Simulate a legacy ENC: value by encrypting with the v1 derivation.
	key := deriveKeyV1(passphrase)
	encrypted, err := encryptPEMWithKey(original, key, encryptedPEMPrefix)
	if err != nil {
		t.Fatalf("legacy encrypt error = %v", err)
	}

	if !strings.HasPrefix(encrypted, encryptedPEMPrefix) {
		t.Fatalf("expected ENC: prefix, got %q", encrypted[:10])
	}

	decrypted, err := decryptPEM(encrypted, passphrase)
	if err != nil {
		t.Fatalf("decryptPEM() legacy v1 error = %v", err)
	}

	if decrypted != original {
		t.Fatalf("decryptPEM() = %q, want %q", decrypted, original)
	}
}

func TestIsEncryptedPEM(t *testing.T) {
	if !isEncryptedPEM("ENC:base64data") {
		t.Fatal("isEncryptedPEM(\"ENC:...\") = false, want true")
	}
	if !isEncryptedPEM("ENC2:base64data") {
		t.Fatal("isEncryptedPEM(\"ENC2:...\") = false, want true")
	}
	if isEncryptedPEM("-----BEGIN EC PRIVATE KEY-----") {
		t.Fatal("isEncryptedPEM(plain PEM) = true, want false")
	}
}

func TestNeedsReEncryption(t *testing.T) {
	if !needsReEncryption("ENC:legacydata") {
		t.Fatal("needsReEncryption(\"ENC:...\") = false, want true")
	}
	if !needsReEncryption("-----BEGIN EC PRIVATE KEY-----") {
		t.Fatal("needsReEncryption(plaintext) = false, want true")
	}
	if needsReEncryption("ENC2:currentdata") {
		t.Fatal("needsReEncryption(\"ENC2:...\") = true, want false")
	}
}

func TestEncryptPEMProducesDifferentCiphertexts(t *testing.T) {
	plaintext := "-----BEGIN EC PRIVATE KEY-----\nMIGkAgEBBDDfake+key+data+here==\n-----END EC PRIVATE KEY-----\n"
	passphrase := "same-passphrase"

	enc1, err := encryptPEM(plaintext, passphrase)
	if err != nil {
		t.Fatalf("first encryptPEM() error = %v", err)
	}

	enc2, err := encryptPEM(plaintext, passphrase)
	if err != nil {
		t.Fatalf("second encryptPEM() error = %v", err)
	}

	if enc1 == enc2 {
		t.Fatal("two encryptions of the same plaintext produced identical ciphertexts; nonce should make them differ")
	}
}
