package secretvault

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/lost-coder/panvex/internal/controlplane/kdf"
	"golang.org/x/crypto/argon2"
)

const (
	domainClient = "client_secret"
	domainTotp   = "totp_secret"
)

func mustVault(t *testing.T, passphrase string) *Vault {
	t.Helper()
	v, err := NewWithSalt(passphrase, []string{domainClient, domainTotp}, []byte("panvex-test-hkdf-salt-16b"))
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
		t.Fatalf("Encrypt() = %q, want %s prefix", ciphertext, Prefix)
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
	v, err := NewWithSalt("", []string{domainClient}, []byte("panvex-test-hkdf-salt-16b"))
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
	disabled, _ := NewWithSalt("", []string{domainClient}, []byte("panvex-test-hkdf-salt-16b"))
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
	v, err := NewWithSalt("passphrase", []string{domainClient}, []byte("panvex-test-hkdf-salt-16b"))
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

// TestMasterDerivationGoesThroughKDFGate pins that the vault's Argon2id
// master derivation is serialised with every other one in the panel. It runs
// at startup (server lifecycle) next to the CA-key decrypt and the admin
// bootstrap hash; outside the gate its 64 MiB block array stacks on top of
// theirs instead of peaking one buffer at a time
// (see docs/2026-07-16-resource-footprint.md).
func TestMasterDerivationGoesThroughKDFGate(t *testing.T) {
	before := kdf.Derivations()
	v, err := NewWithSalt("test-passphrase", []string{domainClient}, []byte("panvex-test-hkdf-salt-16b"))
	if err != nil {
		t.Fatalf("NewWithSalt: %v", err)
	}
	if !v.enabled {
		t.Fatal("vault must be enabled")
	}
	if got := kdf.Derivations() - before; got != 1 {
		t.Fatalf("master derivation must go through kdf.IDKey exactly once, got %d", got)
	}
}

// TestMasterDerivationParamsUnchanged is the compatibility guard for routing
// the derivation through kdf: the key MUST stay byte-identical to the
// hardcoded 3 / 64 MiB / 2 derivation, or every stored secret becomes
// undecryptable. The vault's params are deliberately NOT profile-driven —
// the master key must be reproducible across restarts and installs.
func TestMasterDerivationParamsUnchanged(t *testing.T) {
	want := argon2.IDKey([]byte("test-passphrase"), []byte(masterSalt), 3, 64*1024, 2, 32)
	got := deriveKEK("test-passphrase")
	if !bytes.Equal(got, want) {
		t.Fatal("KEK derivation must stay byte-identical to the legacy 3/64MiB/2 parameters")
	}
}
