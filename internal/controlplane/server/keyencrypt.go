package server

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strings"
)

const encryptedPEMPrefix = "ENC:"

// encryptPEM encrypts a PEM string using AES-256-GCM with a key derived from
// the provided passphrase. The nonce is prepended to the ciphertext and the
// result is base64-encoded with an "ENC:" prefix so the loader can distinguish
// encrypted from plaintext values.
func encryptPEM(plainPEM string, passphrase string) (string, error) {
	key := deriveKey(passphrase)
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}

	ciphertext := gcm.Seal(nonce, nonce, []byte(plainPEM), nil)
	return encryptedPEMPrefix + base64.StdEncoding.EncodeToString(ciphertext), nil
}

// decryptPEM decrypts a value produced by encryptPEM. If the value does not
// carry the "ENC:" prefix it is returned as-is, allowing transparent migration
// from unencrypted to encrypted storage.
func decryptPEM(stored string, passphrase string) (string, error) {
	if !strings.HasPrefix(stored, encryptedPEMPrefix) {
		return stored, nil
	}

	data, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(stored, encryptedPEMPrefix))
	if err != nil {
		return "", errors.New("CA private key: invalid base64 encoding")
	}

	key := deriveKey(passphrase)
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return "", errors.New("CA private key: ciphertext too short")
	}

	plaintext, err := gcm.Open(nil, data[:nonceSize], data[nonceSize:], nil)
	if err != nil {
		return "", errors.New("CA private key: decryption failed (wrong encryption key?)")
	}

	return string(plaintext), nil
}

// isEncryptedPEM reports whether the stored value carries the encryption prefix.
func isEncryptedPEM(stored string) bool {
	return strings.HasPrefix(stored, encryptedPEMPrefix)
}

// deriveKey produces a 256-bit AES key from an arbitrary passphrase using
// SHA-256. The passphrase should be high-entropy (e.g. 32+ random bytes).
func deriveKey(passphrase string) []byte {
	h := sha256.Sum256([]byte(passphrase))
	return h[:]
}
