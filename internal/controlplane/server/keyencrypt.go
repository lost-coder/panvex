package server

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"strings"

	"golang.org/x/crypto/argon2"
)

const (
	// encryptedPEMPrefix is the pre-release ENC:v1 prefix (SHA-256 key
	// derivation). Retained only to detect and loudly reject pre-release v1
	// blobs — decryptPEM returns an error for any value with this prefix.
	encryptedPEMPrefix = "ENC:"
	// encryptedPEMPrefixV2 uses Argon2id key derivation with a random salt.
	encryptedPEMPrefixV2 = "ENC2:"

	// Q3.U-S-16: lift the Argon2id parameters above OWASP minimum so a
	// dictionary attack on a leaked CA blob is materially more expensive.
	// 4 iterations / 96 MiB / 2 threads matches the current OWASP "high-
	// trust secret" recommendation.
	argon2Time    = 4
	argon2Memory  = 96 * 1024
	argon2Threads = 2
	argon2KeyLen  = 32
	argon2SaltLen = 16
)

// encryptPEM encrypts a PEM string using AES-256-GCM with a key derived from
// the provided passphrase via Argon2id. A random 16-byte salt is generated and
// prepended to the nonce+ciphertext blob. The result is base64-encoded with an
// "ENC2:" prefix.
func encryptPEM(plainPEM, passphrase string) (string, error) {
	salt := make([]byte, argon2SaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}

	key := deriveKeyV2(passphrase, salt)
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

	// Layout: salt || nonce || ciphertext
	ciphertext := gcm.Seal(nonce, nonce, []byte(plainPEM), nil)
	blob := make([]byte, argon2SaltLen+len(ciphertext))
	copy(blob, salt)
	copy(blob[argon2SaltLen:], ciphertext)

	return encryptedPEMPrefixV2 + base64.StdEncoding.EncodeToString(blob), nil
}

// decryptPEM decrypts a value produced by encryptPEM. Only the current "ENC2:"
// format (Argon2id derived key) is supported.
//
// Values with the legacy "ENC:" prefix (pre-release SHA-256 format) are
// rejected with a loud error — callers must delete the stored CA record and
// re-bootstrap.
//
// If an encryption key is configured but the stored value carries neither
// prefix, an error is returned to prevent silent use of an unprotected key.
func decryptPEM(stored, passphrase string) (string, error) {
	if strings.HasPrefix(stored, encryptedPEMPrefixV2) {
		return decryptPEMv2(stored, passphrase)
	}
	if strings.HasPrefix(stored, encryptedPEMPrefix) {
		return "", errors.New("CA private key: legacy ENC:v1 format is no longer supported (pre-release format removed); delete the stored certificate authority record and re-bootstrap, or restore an ENC2: backup")
	}

	// No encryption prefix — the stored value is plaintext.
	if passphrase != "" {
		return "", errors.New("CA private key is stored without encryption but an encryption key is configured; re-encrypt the key or remove the encryption key setting")
	}
	return stored, nil
}

// decryptPEMv2 handles the "ENC2:" format with Argon2id key derivation.
func decryptPEMv2(stored, passphrase string) (string, error) {
	data, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(stored, encryptedPEMPrefixV2))
	if err != nil {
		return "", errors.New("CA private key: invalid base64 encoding")
	}

	if len(data) < argon2SaltLen {
		return "", errors.New("CA private key: ciphertext too short for salt")
	}
	salt := data[:argon2SaltLen]
	remainder := data[argon2SaltLen:]

	key := deriveKeyV2(passphrase, salt)
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	nonceSize := gcm.NonceSize()
	if len(remainder) < nonceSize {
		return "", errors.New("CA private key: ciphertext too short")
	}

	plaintext, err := gcm.Open(nil, remainder[:nonceSize], remainder[nonceSize:], nil)
	if err != nil {
		return "", errors.New("CA private key: decryption failed (wrong encryption key?)")
	}

	return string(plaintext), nil
}

// isEncryptedPEM reports whether the stored value carries an encryption prefix.
func isEncryptedPEM(stored string) bool {
	return strings.HasPrefix(stored, encryptedPEMPrefix) || strings.HasPrefix(stored, encryptedPEMPrefixV2)
}

// needsReEncryption reports whether the stored value should be re-encrypted:
// either it uses the legacy ENC: format or it is plaintext.
func needsReEncryption(stored string) bool {
	return !strings.HasPrefix(stored, encryptedPEMPrefixV2)
}

// deriveKeyV2 produces a 256-bit AES key from a passphrase using Argon2id with
// the provided salt. This is the current recommended derivation.
func deriveKeyV2(passphrase string, salt []byte) []byte {
	return argon2.IDKey([]byte(passphrase), salt, argon2Time, argon2Memory, argon2Threads, argon2KeyLen)
}
