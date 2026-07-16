package server

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"strings"

	"github.com/lost-coder/panvex/internal/controlplane/kdf"
)

const (
	// encryptedPEMPrefix is the pre-release ENC:v1 prefix (SHA-256 key
	// derivation). Retained only to detect and loudly reject pre-release v1
	// blobs — decryptPEM returns an error for any value with this prefix.
	encryptedPEMPrefix = "ENC:"
	// encryptedPEMPrefixV2 uses Argon2id key derivation with a random salt and
	// implicit, hardcoded parameters. Decrypt-only: still read so existing
	// installs keep working, never written any more.
	//
	// Layout: salt(16B) || nonce || ciphertext
	encryptedPEMPrefixV2 = "ENC2:"
	// encryptedPEMPrefixV3 is the current format: same AES-256-GCM envelope as
	// ENC2, but the Argon2id parameters are stored in the blob so the key stays
	// readable after a kdf profile change.
	//
	// Layout: t(4B BE) || m(4B BE) || p(1B) || salt(16B) || nonce || ciphertext
	encryptedPEMPrefixV3 = "ENC3:"

	// Legacy ENC2 params (Q3.U-S-16): 4 iterations / 96 MiB / 2 threads.
	// Verify-only — no new blob is written with these; new blobs follow the
	// active kdf profile.
	legacyArgon2Time    = 4
	legacyArgon2Memory  = 96 * 1024
	legacyArgon2Threads = 2

	argon2KeyLen  = 32
	argon2SaltLen = 16

	// enc3HeaderLen is t(4) + m(4) + p(1).
	enc3HeaderLen = 9
)

// Sanity ceilings on the parameters read out of an ENC3 header. The blob comes
// from our own database, but a corrupted row or a bad restore must not be able
// to make the panel allocate gigabytes or spin for weeks at startup — fail the
// decrypt instead. Mirrors the ceilings in auth/password.go.
const (
	maxKeyIter   uint32 = 16
	maxKeyMemKiB uint32 = 512 * 1024
	maxKeyPar    uint8  = 8
)

// encryptPEM encrypts a PEM string using AES-256-GCM with a key derived from
// the provided passphrase via Argon2id, using the ACTIVE kdf profile. The
// parameters, a random 16-byte salt, and the nonce+ciphertext are packed into
// one blob, base64-encoded with an "ENC3:" prefix.
func encryptPEM(plainPEM, passphrase string) (string, error) {
	salt := make([]byte, argon2SaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}

	p := kdf.Active()
	key := deriveKeyV3(passphrase, salt, p.Time, p.MemoryKiB, p.Threads)
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

	// Layout: t || m || p || salt || nonce || ciphertext
	ciphertext := gcm.Seal(nonce, nonce, []byte(plainPEM), nil)
	blob := make([]byte, enc3HeaderLen+argon2SaltLen+len(ciphertext))
	binary.BigEndian.PutUint32(blob[0:4], p.Time)
	binary.BigEndian.PutUint32(blob[4:8], p.MemoryKiB)
	blob[8] = p.Threads
	copy(blob[enc3HeaderLen:], salt)
	copy(blob[enc3HeaderLen+argon2SaltLen:], ciphertext)

	return encryptedPEMPrefixV3 + base64.StdEncoding.EncodeToString(blob), nil
}

// decryptPEM decrypts a value produced by encryptPEM. Both current formats are
// supported: "ENC3:" (self-describing Argon2id params) and "ENC2:" (Argon2id
// with the legacy hardcoded params) so an install that predates the kdf
// profile split can still read its own CA key.
//
// Values with the legacy "ENC:" prefix (pre-release SHA-256 format) are
// rejected with a loud error — callers must delete the stored CA record and
// re-bootstrap.
//
// If an encryption key is configured but the stored value carries neither
// prefix, an error is returned to prevent silent use of an unprotected key.
func decryptPEM(stored, passphrase string) (string, error) {
	if strings.HasPrefix(stored, encryptedPEMPrefixV3) {
		return decryptPEMv3(stored, passphrase)
	}
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

// decryptPEMv3 handles the "ENC3:" format, taking the Argon2id parameters from
// the blob header.
func decryptPEMv3(stored, passphrase string) (string, error) {
	data, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(stored, encryptedPEMPrefixV3))
	if err != nil {
		return "", errors.New("CA private key: invalid base64 encoding")
	}
	if len(data) < enc3HeaderLen+argon2SaltLen {
		return "", errors.New("CA private key: ciphertext too short for header and salt")
	}

	iter := binary.BigEndian.Uint32(data[0:4])
	mem := binary.BigEndian.Uint32(data[4:8])
	par := data[8]
	if iter == 0 || iter > maxKeyIter ||
		mem == 0 || mem > maxKeyMemKiB ||
		par == 0 || par > maxKeyPar {
		return "", errors.New("CA private key: stored Argon2id parameters are out of range (corrupted record?)")
	}

	salt := data[enc3HeaderLen : enc3HeaderLen+argon2SaltLen]
	remainder := data[enc3HeaderLen+argon2SaltLen:]

	return openPEMEnvelope(deriveKeyV3(passphrase, salt, iter, mem, par), remainder)
}

// decryptPEMv2 handles the "ENC2:" format with the legacy hardcoded Argon2id
// parameters. Read-only compatibility path.
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

	return openPEMEnvelope(deriveKeyV2(passphrase, salt), remainder)
}

// openPEMEnvelope opens the AES-256-GCM envelope shared by ENC2 and ENC3
// (nonce || ciphertext) with an already-derived key.
func openPEMEnvelope(key, remainder []byte) (string, error) {
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
	return strings.HasPrefix(stored, encryptedPEMPrefix) ||
		strings.HasPrefix(stored, encryptedPEMPrefixV2) ||
		strings.HasPrefix(stored, encryptedPEMPrefixV3)
}

// needsReEncryption reports whether the STORED value should be re-encrypted:
// it uses the legacy ENC: format, or it is plaintext.
//
// ENC2 counts as current even though it is never written any more: it decrypts
// correctly and rewriting it would cost an extra Argon2id derivation plus a DB
// write on every startup for no security gain. Callers MUST pass the value as
// it was read from the store — passing an already-decrypted key makes this
// unconditionally true and re-encrypts in a loop.
func needsReEncryption(stored string) bool {
	return !strings.HasPrefix(stored, encryptedPEMPrefixV2) &&
		!strings.HasPrefix(stored, encryptedPEMPrefixV3)
}

// deriveKeyV2 produces a 256-bit AES key with the legacy ENC2 parameters.
// Read-only: used to open existing ENC2 blobs.
func deriveKeyV2(passphrase string, salt []byte) []byte {
	return kdf.IDKey([]byte(passphrase), salt, legacyArgon2Time, legacyArgon2Memory, legacyArgon2Threads, argon2KeyLen)
}

// deriveKeyV3 produces a 256-bit AES key with explicit Argon2id parameters —
// the active profile's when encrypting, the blob's own when decrypting.
func deriveKeyV3(passphrase string, salt []byte, iter, mem uint32, par uint8) []byte {
	return kdf.IDKey([]byte(passphrase), salt, iter, mem, par, argon2KeyLen)
}
