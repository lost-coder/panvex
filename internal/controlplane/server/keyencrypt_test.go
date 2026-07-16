package server

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"strings"
	"testing"

	"github.com/lost-coder/panvex/internal/controlplane/kdf"
	"golang.org/x/crypto/argon2"
)

// seedLegacyEnc2Blob builds an ENC2 blob exactly as the pre-profile release
// wrote it: Argon2id with the hardcoded 4 / 96 MiB / 2 params, layout
// salt || nonce || ciphertext.
func seedLegacyEnc2Blob(t *testing.T, plainPEM, passphrase string) string {
	t.Helper()
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		t.Fatalf("salt: %v", err)
	}
	key := argon2.IDKey([]byte(passphrase), salt, 4, 96*1024, 2, 32)
	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatalf("NewCipher: %v", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatalf("NewGCM: %v", err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		t.Fatalf("nonce: %v", err)
	}
	ciphertext := gcm.Seal(nonce, nonce, []byte(plainPEM), nil)
	blob := make([]byte, 16+len(ciphertext))
	copy(blob, salt)
	copy(blob[16:], ciphertext)
	return "ENC2:" + base64.StdEncoding.EncodeToString(blob)
}

func TestEncryptDecryptPEMRoundTrip(t *testing.T) {
	original := "-----BEGIN EC PRIVATE KEY-----\nMIGkAgEBBDDfake+key+data+here==\n-----END EC PRIVATE KEY-----\n"
	passphrase := "test-passphrase-32-bytes-long!!!"

	encrypted, err := encryptPEM(original, passphrase)
	if err != nil {
		t.Fatalf("encryptPEM() error = %v", err)
	}

	if !strings.HasPrefix(encrypted, encryptedPEMPrefixV3) {
		t.Fatalf("encryptPEM() should produce ENC3: prefix, got %q", encrypted[:10])
	}

	decrypted, err := decryptPEM(encrypted, passphrase)
	if err != nil {
		t.Fatalf("decryptPEM() error = %v", err)
	}

	if decrypted != original {
		t.Fatalf("decryptPEM() = %q, want %q", decrypted, original)
	}
}

func TestDecryptPEMPlaintextWithPassphraseReturnsError(t *testing.T) {
	plainPEM := "-----BEGIN EC PRIVATE KEY-----\nMIGkAgEBBDDfake+key+data+here==\n-----END EC PRIVATE KEY-----\n"

	_, err := decryptPEM(plainPEM, "any-passphrase")
	if err == nil {
		t.Fatal("decryptPEM() error = nil, want error for plaintext key with passphrase configured")
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

func TestDecryptPEMRejectsLegacyV1(t *testing.T) {
	_, err := decryptPEM("ENC:AAAA", "passphrase")
	if err == nil {
		t.Fatal("decryptPEM(ENC:v1) error = nil, want loud rejection")
	}
	if !strings.Contains(err.Error(), "no longer supported") {
		t.Fatalf("error = %q, want mention of removed ENC:v1 support", err)
	}
	// Same rejection without a passphrase: the blob must never be
	// mistaken for plaintext PEM.
	if _, err := decryptPEM("ENC:AAAA", ""); err == nil {
		t.Fatal("decryptPEM(ENC:v1, no key) error = nil, want loud rejection")
	}
}

func TestIsEncryptedPEM(t *testing.T) {
	if !isEncryptedPEM("ENC:base64data") {
		t.Fatal("isEncryptedPEM(\"ENC:...\") = false, want true")
	}
	if !isEncryptedPEM("ENC2:base64data") {
		t.Fatal("isEncryptedPEM(\"ENC2:...\") = false, want true")
	}
	// ENC3 must be recognised as encrypted, or authority.go's ENC:v1 guard
	// (isEncryptedPEM && !ENC2/ENC3) would reject every new blob at startup.
	if !isEncryptedPEM("ENC3:base64data") {
		t.Fatal("isEncryptedPEM(\"ENC3:...\") = false, want true")
	}
	if isEncryptedPEM("-----BEGIN EC PRIVATE KEY-----") {
		t.Fatal("isEncryptedPEM(plain PEM) = true, want false")
	}
}

// TestNeedsReEncryption pins the anti-loop invariant: a blob already in a
// current format must NOT be re-encrypted, or every startup burns another
// Argon2id derivation and rewrites the CA row forever.
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
	if needsReEncryption("ENC3:currentdata") {
		t.Fatal("needsReEncryption(\"ENC3:...\") = true, want false")
	}
}

// TestDecryptPEMAcceptsLegacyEnc2 pins backward compatibility: blobs written
// before the profile split must keep decrypting with their legacy params, or
// an existing install cannot read its own CA key.
func TestDecryptPEMAcceptsLegacyEnc2(t *testing.T) {
	original := "-----BEGIN EC PRIVATE KEY-----\nMIGkAgEBBDDfake+key+data+here==\n-----END EC PRIVATE KEY-----\n"
	passphrase := "test-passphrase-32-bytes-long!!!"

	blob := seedLegacyEnc2Blob(t, original, passphrase)
	decrypted, err := decryptPEM(blob, passphrase)
	if err != nil {
		t.Fatalf("legacy ENC2 must decrypt: %v", err)
	}
	if decrypted != original {
		t.Fatalf("decryptPEM(ENC2) = %q, want %q", decrypted, original)
	}
	if _, err := decryptPEM(blob, "wrong-passphrase"); err == nil {
		t.Fatal("legacy ENC2 with wrong passphrase must fail")
	}
}

// TestEncryptPEMUsesActiveProfile: a low-memory panel must not write a 96 MiB
// blob, and the params must round-trip out of the blob itself.
func TestEncryptPEMUsesActiveProfile(t *testing.T) {
	t.Cleanup(func() { _ = kdf.SetActiveProfile("") })
	if err := kdf.SetActiveProfile("low-memory"); err != nil {
		t.Fatalf("SetActiveProfile: %v", err)
	}
	original := "-----BEGIN EC PRIVATE KEY-----\nMIGkAgEBBDDfake+key+data+here==\n-----END EC PRIVATE KEY-----\n"
	passphrase := "test-passphrase-32-bytes-long!!!"

	encrypted, err := encryptPEM(original, passphrase)
	if err != nil {
		t.Fatalf("encryptPEM: %v", err)
	}
	if !strings.HasPrefix(encrypted, encryptedPEMPrefixV3) {
		t.Fatalf("want ENC3 prefix, got %q", encrypted[:10])
	}
	iter, mem, par, err := parseEnc3Params(t, encrypted)
	if err != nil {
		t.Fatalf("parse header: %v", err)
	}
	if iter != 2 || mem != 19456 || par != 1 {
		t.Fatalf("header params = t=%d,m=%d,p=%d, want the low-memory profile", iter, mem, par)
	}

	// Decrypt under a DIFFERENT active profile: the blob is self-describing,
	// so switching the profile must not orphan it.
	if err := kdf.SetActiveProfile("default"); err != nil {
		t.Fatalf("SetActiveProfile: %v", err)
	}
	decrypted, err := decryptPEM(encrypted, passphrase)
	if err != nil {
		t.Fatalf("ENC3 must decrypt under another active profile: %v", err)
	}
	if decrypted != original {
		t.Fatalf("decryptPEM = %q, want %q", decrypted, original)
	}
}

// parseEnc3Params reads the t/m/p header out of an ENC3 blob.
func parseEnc3Params(t *testing.T, stored string) (iter, mem uint32, par uint8, err error) {
	t.Helper()
	data, decErr := base64.StdEncoding.DecodeString(strings.TrimPrefix(stored, encryptedPEMPrefixV3))
	if decErr != nil {
		return 0, 0, 0, decErr
	}
	iter = uint32(data[0])<<24 | uint32(data[1])<<16 | uint32(data[2])<<8 | uint32(data[3])
	mem = uint32(data[4])<<24 | uint32(data[5])<<16 | uint32(data[6])<<8 | uint32(data[7])
	par = data[8]
	return iter, mem, par, nil
}

// TestDecryptPEMRejectsOversizedEnc3Params: a corrupted header must not make
// the panel allocate gigabytes at startup.
func TestDecryptPEMRejectsOversizedEnc3Params(t *testing.T) {
	// t=2, m=4 GiB, p=1 + 16B salt + nonce/ciphertext-sized filler.
	blob := make([]byte, 0, enc3HeaderLen+16+12+16)
	blob = append(blob, 0, 0, 0, 2, 0, 64, 0, 0, 1)
	blob = append(blob, make([]byte, 16+12+16)...)
	stored := encryptedPEMPrefixV3 + base64.StdEncoding.EncodeToString(blob)

	before := kdf.Derivations()
	if _, err := decryptPEM(stored, "passphrase"); err == nil {
		t.Fatal("oversized ENC3 params must be rejected")
	}
	if got := kdf.Derivations() - before; got != 0 {
		t.Fatalf("rejected params must not derive at all, ran %d derivations", got)
	}
}

// TestDecryptPEMRejectsTruncatedEnc3 covers short blobs that cannot even
// carry a header/salt/nonce — they must error, not panic.
func TestDecryptPEMRejectsTruncatedEnc3(t *testing.T) {
	for _, size := range []int{0, 5, 9, 20, 30} {
		stored := encryptedPEMPrefixV3 + base64.StdEncoding.EncodeToString(make([]byte, size))
		if _, err := decryptPEM(stored, "passphrase"); err == nil {
			t.Fatalf("truncated ENC3 (%d bytes) must be rejected", size)
		}
	}
	if _, err := decryptPEM(encryptedPEMPrefixV3+"!!!not-base64!!!", "passphrase"); err == nil {
		t.Fatal("ENC3 with invalid base64 must be rejected")
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
