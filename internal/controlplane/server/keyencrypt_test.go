package server

import (
	"testing"
)

func TestEncryptDecryptPEMRoundTrip(t *testing.T) {
	original := "-----BEGIN EC PRIVATE KEY-----\nMIGkAgEBBDDfake+key+data+here==\n-----END EC PRIVATE KEY-----\n"
	passphrase := "test-passphrase-32-bytes-long!!!"

	encrypted, err := encryptPEM(original, passphrase)
	if err != nil {
		t.Fatalf("encryptPEM() error = %v", err)
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

func TestIsEncryptedPEM(t *testing.T) {
	if !isEncryptedPEM("ENC:base64data") {
		t.Fatal("isEncryptedPEM(\"ENC:...\") = false, want true")
	}
	if isEncryptedPEM("-----BEGIN EC PRIVATE KEY-----") {
		t.Fatal("isEncryptedPEM(plain PEM) = true, want false")
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
