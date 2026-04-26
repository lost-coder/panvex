package secretvault

import (
	"errors"
	"strings"
	"testing"
)

const (
	domainClient = "client_secret"
	domainTotp   = "totp_secret"
)

func mustVault(t *testing.T, passphrase string) *Vault {
	t.Helper()
	v, err := New(passphrase, []string{domainClient, domainTotp})
	if err != nil {
		t.Fatalf("New(%q) error = %v", passphrase, err)
	}
	return v
}

func TestRoundtripEncryptedValue(t *testing.T) {
	v := mustVault(t, "operator-passphrase")
	plaintext := "deadbeef0123456789abcdef01234567"

	ciphertext, err := v.Encrypt(domainClient, plaintext)
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}
	if !strings.HasPrefix(ciphertext, Prefix) {
		t.Fatalf("Encrypt() = %q, want PVS1: prefix", ciphertext)
	}
	if ciphertext == plaintext {
		t.Fatal("Encrypt() returned plaintext unchanged")
	}

	got, err := v.Decrypt(domainClient, ciphertext)
	if err != nil {
		t.Fatalf("Decrypt() error = %v", err)
	}
	if got != plaintext {
		t.Fatalf("Decrypt() = %q, want %q", got, plaintext)
	}
}

func TestEncryptIsRandomized(t *testing.T) {
	v := mustVault(t, "passphrase")
	a, _ := v.Encrypt(domainClient, "secret")
	b, _ := v.Encrypt(domainClient, "secret")
	if a == b {
		t.Fatal("Encrypt() produced identical ciphertexts; nonce reuse")
	}
}

func TestDecryptCrossDomainFails(t *testing.T) {
	v := mustVault(t, "passphrase")
	ciphertext, err := v.Encrypt(domainClient, "value")
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}
	if _, err := v.Decrypt(domainTotp, ciphertext); err == nil {
		t.Fatal("Decrypt() under wrong domain succeeded; AAD not bound")
	}
}

func TestDecryptWrongKeyFails(t *testing.T) {
	v1 := mustVault(t, "first-passphrase")
	v2 := mustVault(t, "second-passphrase")
	ciphertext, err := v1.Encrypt(domainClient, "value")
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}
	if _, err := v2.Decrypt(domainClient, ciphertext); err == nil {
		t.Fatal("Decrypt() with wrong vault succeeded")
	}
}

func TestPlaintextPassthroughWhenDisabled(t *testing.T) {
	v, err := New("", []string{domainClient})
	if err != nil {
		t.Fatalf("New(\"\") error = %v", err)
	}
	if v.Enabled() {
		t.Fatal("Vault.Enabled() = true for empty passphrase")
	}
	got, err := v.Encrypt(domainClient, "plain")
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}
	if got != "plain" {
		t.Fatalf("Encrypt() disabled = %q, want plain", got)
	}
	back, err := v.Decrypt(domainClient, "plain")
	if err != nil {
		t.Fatalf("Decrypt() error = %v", err)
	}
	if back != "plain" {
		t.Fatalf("Decrypt() = %q, want plain", back)
	}
}

func TestDecryptLegacyPlaintextWhenEnabled(t *testing.T) {
	v := mustVault(t, "passphrase")
	got, err := v.Decrypt(domainClient, "legacy-plaintext-secret")
	if err != nil {
		t.Fatalf("Decrypt() legacy error = %v", err)
	}
	if got != "legacy-plaintext-secret" {
		t.Fatalf("Decrypt() legacy = %q, want pass-through", got)
	}
}

func TestDecryptEncryptedRequiresEnabledVault(t *testing.T) {
	v := mustVault(t, "passphrase")
	ciphertext, err := v.Encrypt(domainClient, "x")
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}
	disabled, _ := New("", []string{domainClient})
	if _, err := disabled.Decrypt(domainClient, ciphertext); err == nil {
		t.Fatal("Decrypt() with disabled vault accepted ciphertext")
	}
}

func TestEmptyPlaintextStaysEmpty(t *testing.T) {
	v := mustVault(t, "passphrase")
	got, err := v.Encrypt(domainClient, "")
	if err != nil {
		t.Fatalf("Encrypt(\"\") error = %v", err)
	}
	if got != "" {
		t.Fatalf("Encrypt(\"\") = %q, want empty", got)
	}
}

func TestUnknownDomainErrors(t *testing.T) {
	v, err := New("passphrase", []string{domainClient})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if _, err := v.Encrypt("unknown_domain", "x"); !errors.Is(err, ErrUnknownDomain) {
		t.Fatalf("Encrypt(unknown) error = %v, want ErrUnknownDomain", err)
	}
	if _, err := v.Decrypt("unknown_domain", Prefix+"AAAA"); !errors.Is(err, ErrUnknownDomain) {
		t.Fatalf("Decrypt(unknown) error = %v, want ErrUnknownDomain", err)
	}
}

func TestCorruptedCiphertextDetected(t *testing.T) {
	v := mustVault(t, "passphrase")
	ciphertext, err := v.Encrypt(domainClient, "value")
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}
	// Flip a byte in the base64 portion.
	corrupted := []byte(ciphertext)
	pos := len(Prefix) + 5
	corrupted[pos] ^= 0x01
	if _, err := v.Decrypt(domainClient, string(corrupted)); err == nil {
		t.Fatal("Decrypt() accepted corrupted ciphertext")
	}
}

func TestIsEncrypted(t *testing.T) {
	if !IsEncrypted(Prefix + "abc") {
		t.Fatal("IsEncrypted() did not detect prefix")
	}
	if IsEncrypted("plain") {
		t.Fatal("IsEncrypted() flagged plaintext")
	}
}

func TestDeterministicAcrossInstances(t *testing.T) {
	a := mustVault(t, "stable")
	b := mustVault(t, "stable")
	ciphertext, err := a.Encrypt(domainClient, "value")
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}
	got, err := b.Decrypt(domainClient, ciphertext)
	if err != nil {
		t.Fatalf("Decrypt() across instances error = %v", err)
	}
	if got != "value" {
		t.Fatalf("Decrypt() = %q, want value", got)
	}
}

func TestNilVaultIsPassthrough(t *testing.T) {
	var v *Vault
	if v.Enabled() {
		t.Fatal("nil Vault.Enabled() = true")
	}
	got, err := v.Encrypt(domainClient, "x")
	if err != nil {
		t.Fatalf("nil Encrypt() error = %v", err)
	}
	if got != "x" {
		t.Fatalf("nil Encrypt() = %q, want x", got)
	}
}
