package secretvault

import (
	"context"
	"strings"
	"sync"
	"testing"
)

// memStore is the in-memory CPSecretReader used by envelope tests.
// Mirrors the postgres / sqlite cp_secrets behaviour: bytes in, bytes
// out, ErrCPSecretNotFound modelled as the empty []byte the real
// stores return on a missing row.
type memStore struct {
	mu   sync.Mutex
	rows map[string][]byte
}

func newMemStore() *memStore { return &memStore{rows: map[string][]byte{}} }

func (m *memStore) GetCPSecret(_ context.Context, key string) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]byte(nil), m.rows[key]...), nil
}

func (m *memStore) PutCPSecret(_ context.Context, key string, value []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rows[key] = append([]byte(nil), value...)
	return nil
}

const testPassphrase = "correct horse battery staple"

func TestEnvelopeFirstStartMintsDEKs(t *testing.T) {
	store := newMemStore()
	v, err := NewWithEnvelope(context.Background(), testPassphrase, AllDomains, []byte("salt-test-1234567890123456789012"), store)
	if err != nil {
		t.Fatalf("NewWithEnvelope: %v", err)
	}
	if !v.EnvelopeEnabled() {
		t.Fatal("EnvelopeEnabled = false after construction with a real store")
	}
	for _, d := range AllDomains {
		version, dek, ok := v.activeDEK(d)
		if !ok {
			t.Errorf("activeDEK(%q) missing after init", d)
			continue
		}
		if version != 1 {
			t.Errorf("active version for %q = %d, want 1", d, version)
		}
		if len(dek) != keyLen {
			t.Errorf("DEK len for %q = %d, want %d", d, len(dek), keyLen)
		}
		// cp_secrets carries the wrapped + active rows.
		if got, _ := store.GetCPSecret(context.Background(), wrappedKey(d, 1)); len(got) == 0 {
			t.Errorf("wrapped DEK not persisted for %q", d)
		}
		if got, _ := store.GetCPSecret(context.Background(), activeKey(d)); string(got) != "1" {
			t.Errorf("active version row for %q = %q, want \"1\"", d, got)
		}
	}
	if got, _ := store.GetCPSecret(context.Background(), cpSecretKEKFingerprint); len(got) == 0 {
		t.Error("KEK fingerprint not persisted on first start")
	}
}

func TestEnvelopeRoundTripPVS3(t *testing.T) {
	store := newMemStore()
	v, err := NewWithEnvelope(context.Background(), testPassphrase, AllDomains, []byte("salt-test-1234567890123456789012"), store)
	if err != nil {
		t.Fatal(err)
	}
	ct, err := v.Encrypt(DomainClientSecret, "hunter2")
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if !strings.HasPrefix(ct, Prefix3) {
		t.Errorf("Encrypt didn't emit PVS3 (got prefix %.5q)", ct)
	}
	got, err := v.Decrypt(DomainClientSecret, ct)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if got != "hunter2" {
		t.Errorf("round-trip plaintext = %q, want hunter2", got)
	}
}

func TestEnvelopeSecondStartLoadsExistingDEKs(t *testing.T) {
	store := newMemStore()
	salt := []byte("salt-test-1234567890123456789012")
	v1, err := NewWithEnvelope(context.Background(), testPassphrase, AllDomains, salt, store)
	if err != nil {
		t.Fatal(err)
	}
	ct, err := v1.Encrypt(DomainTOTP, "654321")
	if err != nil {
		t.Fatal(err)
	}

	// Fresh vault over the same cp_secrets — must reload the same DEK
	// and decrypt v1's ciphertext.
	v2, err := NewWithEnvelope(context.Background(), testPassphrase, AllDomains, salt, store)
	if err != nil {
		t.Fatalf("second start: %v", err)
	}
	got, err := v2.Decrypt(DomainTOTP, ct)
	if err != nil {
		t.Fatalf("v2 Decrypt: %v", err)
	}
	if got != "654321" {
		t.Errorf("v2 plaintext = %q, want 654321", got)
	}
}

func TestEnvelopeRejectsWrongPassphraseOnSecondStart(t *testing.T) {
	store := newMemStore()
	salt := []byte("salt-test-1234567890123456789012")
	if _, err := NewWithEnvelope(context.Background(), testPassphrase, AllDomains, salt, store); err != nil {
		t.Fatal(err)
	}
	_, err := NewWithEnvelope(context.Background(), "wrong-passphrase", AllDomains, salt, store)
	if err == nil {
		t.Fatal("second start with wrong passphrase did not error")
	}
	if !strings.Contains(err.Error(), "KEK fingerprint mismatch") {
		t.Errorf("error = %v, want fingerprint mismatch", err)
	}
}

func TestEnvelopeCrossDomainOpenFails(t *testing.T) {
	store := newMemStore()
	v, err := NewWithEnvelope(context.Background(), testPassphrase, AllDomains, []byte("salt-test-1234567890123456789012"), store)
	if err != nil {
		t.Fatal(err)
	}
	ct, err := v.Encrypt(DomainClientSecret, "for-clients-only")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := v.Decrypt(DomainTOTP, ct); err == nil {
		t.Error("Decrypt with wrong domain unexpectedly succeeded (AAD binding broken)")
	}
}

func TestEnvelopeStillReadsPVS2(t *testing.T) {
	// Backwards compatibility: pre-envelope ciphertexts must keep
	// decrypting after a process is upgraded to envelope mode (until
	// the upgrade-vault subcommand re-encrypts them to PVS3).
	store := newMemStore()
	salt := []byte("salt-test-1234567890123456789012")
	legacy, err := NewWithSalt(testPassphrase, AllDomains, salt)
	if err != nil {
		t.Fatal(err)
	}
	pvs2, err := legacy.Encrypt(DomainTOTP, "old-secret")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(pvs2, Prefix2) {
		t.Fatalf("legacy did not emit PVS2: %q", pvs2)
	}

	v, err := NewWithEnvelope(context.Background(), testPassphrase, AllDomains, salt, store)
	if err != nil {
		t.Fatal(err)
	}
	got, err := v.Decrypt(DomainTOTP, pvs2)
	if err != nil {
		t.Fatalf("PVS2 decrypt under envelope vault: %v", err)
	}
	if got != "old-secret" {
		t.Errorf("PVS2 plaintext = %q, want old-secret", got)
	}
}

func TestRotateKEKRewrapsAndPreservesCiphertext(t *testing.T) {
	store := newMemStore()
	salt := []byte("salt-test-1234567890123456789012")
	v, err := NewWithEnvelope(context.Background(), testPassphrase, AllDomains, salt, store)
	if err != nil {
		t.Fatal(err)
	}
	// Capture a PVS3 ciphertext + the active DEK bytes BEFORE rotation.
	const want = "secret-survives-rotation"
	ct, err := v.Encrypt(DomainClientSecret, want)
	if err != nil {
		t.Fatal(err)
	}
	_, oldDEK, _ := v.activeDEK(DomainClientSecret)
	oldDEKCopy := append([]byte(nil), oldDEK...)

	const newPass = "rolled-key-2026"
	n, err := v.RotateKEK(context.Background(), store, newPass)
	if err != nil {
		t.Fatalf("RotateKEK: %v", err)
	}
	if n != len(AllDomains) {
		t.Errorf("rewrapped %d DEKs, want %d", n, len(AllDomains))
	}
	// DEK plaintext stayed identical (the whole point of envelope
	// rotation) — ciphertext still decrypts.
	_, newDEK, _ := v.activeDEK(DomainClientSecret)
	if string(newDEK) != string(oldDEKCopy) {
		t.Error("DEK plaintext changed across rotation (should stay constant)")
	}
	got, err := v.Decrypt(DomainClientSecret, ct)
	if err != nil {
		t.Fatalf("Decrypt after rotation: %v", err)
	}
	if got != want {
		t.Errorf("plaintext after rotation = %q, want %q", got, want)
	}

	// A fresh vault constructed with the NEW passphrase must boot,
	// load the re-wrapped DEKs, and decrypt the old ciphertext.
	v2, err := NewWithEnvelope(context.Background(), newPass, AllDomains, salt, store)
	if err != nil {
		t.Fatalf("vault boot with new passphrase: %v", err)
	}
	got2, err := v2.Decrypt(DomainClientSecret, ct)
	if err != nil {
		t.Fatalf("v2 Decrypt: %v", err)
	}
	if got2 != want {
		t.Errorf("v2 plaintext = %q, want %q", got2, want)
	}

	// And the OLD passphrase must now fail the fingerprint check.
	if _, err := NewWithEnvelope(context.Background(), testPassphrase, AllDomains, salt, store); err == nil {
		t.Error("old passphrase still accepted after rotation")
	}
}

func TestRotateKEKRejectsSamePassphrase(t *testing.T) {
	store := newMemStore()
	v, err := NewWithEnvelope(context.Background(), testPassphrase, AllDomains, []byte("salt-test-1234567890123456789012"), store)
	if err != nil {
		t.Fatal(err)
	}
	_, err = v.RotateKEK(context.Background(), store, testPassphrase)
	if err == nil {
		t.Error("rotation with identical passphrase did not error")
	}
}

func TestIsEncryptedRecognisesPVS3(t *testing.T) {
	if !IsEncrypted("PVS3:v1:abc") {
		t.Error("IsEncrypted should accept PVS3")
	}
	if IsLegacyEncrypted("PVS3:v1:abc") {
		t.Error("IsLegacyEncrypted should NOT accept PVS3 (envelope is the new world)")
	}
	if !IsLegacyEncrypted("PVS2:abc") {
		t.Error("IsLegacyEncrypted should accept PVS2")
	}
	if !IsLegacyEncrypted("PVS1:abc") {
		t.Error("IsLegacyEncrypted should accept PVS1")
	}
}
