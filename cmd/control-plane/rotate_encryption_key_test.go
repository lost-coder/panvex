package main

import (
	"bufio"
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lost-coder/panvex/internal/controlplane/secretvault"
	"github.com/lost-coder/panvex/internal/controlplane/storage/sqlite"
)

// These tests exercise the rotate-encryption-key building blocks
// directly (vault construction, RotateKEK, legacy-ciphertext scan)
// against a real SQLite store. We avoid driving runRotateEncryptionKey
// end-to-end because it reads from stdin; the stdin parser
// (readPassphrase) is tested separately below.

func freshStore(t *testing.T) *sqlite.Store {
	t.Helper()
	dir := t.TempDir()
	store, err := sqlite.Open(filepath.Join(dir, "panvex.db"))
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func TestRotateEncryptionKey_HappyPath(t *testing.T) {
	store := freshStore(t)
	ctx := context.Background()

	const oldPass = "first-pass"
	const newPass = "rolled-pass-2026"

	saltBytes, err := loadOrCreateVaultSaltCLI(ctx, store)
	if err != nil {
		t.Fatal(err)
	}
	adapter := cliCPSecretAdapter{store: store}

	// First boot — vault initialised under oldPass.
	v1, err := secretvault.NewWithEnvelope(ctx, oldPass, secretvault.AllDomains, saltBytes, adapter)
	if err != nil {
		t.Fatalf("first boot: %v", err)
	}
	ct, err := v1.Encrypt(secretvault.DomainClientSecret, "rolling-secret")
	if err != nil {
		t.Fatal(err)
	}

	// No legacy ciphertexts seeded → scan returns 0.
	n, err := countLegacyCiphertexts(ctx, store)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("countLegacyCiphertexts on fresh store = %d, want 0", n)
	}

	// Rotate KEK.
	rewrapped, err := v1.RotateKEK(ctx, adapter, newPass)
	if err != nil {
		t.Fatalf("RotateKEK: %v", err)
	}
	if rewrapped != len(secretvault.AllDomains) {
		t.Errorf("rewrapped = %d, want %d", rewrapped, len(secretvault.AllDomains))
	}

	// New boot under the new passphrase must succeed and decrypt the
	// pre-rotation ciphertext byte-identically.
	v2, err := secretvault.NewWithEnvelope(ctx, newPass, secretvault.AllDomains, saltBytes, adapter)
	if err != nil {
		t.Fatalf("boot under new passphrase: %v", err)
	}
	got, err := v2.Decrypt(secretvault.DomainClientSecret, ct)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if got != "rolling-secret" {
		t.Errorf("plaintext = %q, want rolling-secret", got)
	}

	// And the OLD passphrase must now be rejected.
	if _, err := secretvault.NewWithEnvelope(ctx, oldPass, secretvault.AllDomains, saltBytes, adapter); err == nil {
		t.Error("old passphrase still accepted after rotation")
	} else if !errors.Is(err, secretvault.ErrEnvelopeWrongKEK) {
		t.Errorf("post-rotation old-pass boot err = %v, want ErrEnvelopeWrongKEK", err)
	}
}

func TestCountLegacyCiphertexts_FindsPVS2Ciphertext(t *testing.T) {
	store := freshStore(t)
	ctx := context.Background()

	// Manually insert a webhook endpoint with a PVS2-prefixed
	// ciphertext to simulate a pre-envelope install awaiting
	// upgrade-vault.
	if _, err := store.DB().ExecContext(ctx, `
		INSERT INTO webhook_endpoints (id, name, url, secret_ciphertext, event_filter, allow_private, enabled)
		VALUES ('test-1', 'old-style', 'https://example.com', 'PVS2:abc==', '', 0, 1)
	`); err != nil {
		t.Fatalf("seed PVS2 row: %v", err)
	}

	n, err := countLegacyCiphertexts(ctx, store)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("countLegacyCiphertexts = %d, want 1", n)
	}
}

func TestReadPassphrase_TrimsTrailingNewline(t *testing.T) {
	r := bufio.NewReader(strings.NewReader("hunter2\n"))
	got, err := readPassphrase(r, "")
	if err != nil {
		t.Fatal(err)
	}
	if got != "hunter2" {
		t.Errorf("readPassphrase = %q, want hunter2", got)
	}
}

func TestReadPassphrase_HandlesNoTrailingNewline(t *testing.T) {
	r := bufio.NewReader(strings.NewReader("hunter2"))
	got, err := readPassphrase(r, "")
	if err != nil {
		t.Fatal(err)
	}
	if got != "hunter2" {
		t.Errorf("readPassphrase = %q, want hunter2", got)
	}
}
