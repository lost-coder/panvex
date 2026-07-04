package main

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/lost-coder/panvex/internal/controlplane/secretvault"
	"github.com/lost-coder/panvex/internal/controlplane/storage/postgres"
)

// TestRotateEncryptionKey_HappyPath_Postgres mirrors the SQLite happy path
// against a live PostgreSQL store. Regression for audit 2026-07-02 #1:
// RotateKEK -> cliCPSecretAdapter.WithCPSecretTx -> store.Transact ->
// tx.PutCPSecret failed with "method requires pool handle, not tx-bound
// store" on Postgres, so key rotation was impossible on that backend.
// Skips without PANVEX_POSTGRES_TEST_DSN, matching the storage-tree
// convention (see postgres/store_test.go); CI's schema-sync job provides
// the DSN.
func TestRotateEncryptionKey_HappyPath_Postgres(t *testing.T) {
	dsn := os.Getenv("PANVEX_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("PANVEX_POSTGRES_TEST_DSN is not set")
	}

	store, err := postgres.Open(dsn)
	if err != nil {
		t.Fatalf("postgres.Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	ctx := context.Background()
	// The vault rows in cp_secrets live under fixed keys (HKDF salt, KEK
	// fingerprint, wrapped DEKs) — wipe them so a previous run against the
	// shared panvex_test database cannot shadow this run's passphrases.
	if _, err := store.DB().ExecContext(ctx, `DELETE FROM cp_secrets`); err != nil {
		t.Fatalf("reset cp_secrets: %v", err)
	}

	const oldPass = "pg-first-pass"
	const newPass = "pg-rolled-pass-2026"

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

	// Legacy-ciphertext safety scan must work against the Postgres store
	// (tryStoreSQLDB path) and report zero on a fresh database.
	n, err := countLegacyCiphertexts(ctx, store)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("countLegacyCiphertexts on fresh store = %d, want 0", n)
	}

	// Rotate KEK — the whole point: lands multi-row re-wrap via Transact.
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
