package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/lost-coder/panvex/internal/controlplane/config"
	"github.com/lost-coder/panvex/internal/controlplane/secretvault"
	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

// runRotateEncryptionKey re-wraps each persisted DEK under a fresh
// KEK derived from a new passphrase. Existing ciphertext columns
// (clients.secret_ciphertext, users.totp_secret,
// webhook_endpoints.secret_ciphertext) stay byte-identical — the only
// thing that changes is the wrapped-DEK rows in cp_secrets.
//
// Safety: refuses to run if any data column still holds a PVS1/PVS2
// ciphertext (those depend on the current passphrase directly; the
// upgrade-vault migration converts them to PVS3 first; landed as a
// follow-up).
//
// Both old and new passphrases come from stdin (one per prompt) so
// argv never carries them. Reads the second one only after verifying
// the first matches the persisted KEK fingerprint.
func runRotateEncryptionKey(args []string) error {
	flags := flag.NewFlagSet("rotate-encryption-key", flag.ContinueOnError)
	storageDriver := flags.String(flagStorageDriver, "", helpStorageDriver)
	storageDSN := flags.String(flagStorageDSN, "", helpStorageDSN)
	allowLegacy := flags.Bool("allow-legacy-ciphertexts", false,
		"WARNING: rotate even if PVS1/PVS2 ciphertexts remain. They will become unreadable after rotation. Used only by tests.")
	flags.Usage = func() {
		fmt.Fprintf(flags.Output(), "Usage: panvex-control-plane rotate-encryption-key [flags]\n\n")
		fmt.Fprintf(flags.Output(), "Re-wraps the at-rest encryption envelope under a new passphrase.\n")
		fmt.Fprintf(flags.Output(), "Data ciphertexts are NOT re-encrypted (the whole point of the envelope).\n\n")
		fmt.Fprintf(flags.Output(), "Workflow:\n")
		fmt.Fprintf(flags.Output(), "  1. Stop the panel.\n")
		fmt.Fprintf(flags.Output(), "  2. Run this command; supply the CURRENT passphrase, then the NEW passphrase.\n")
		fmt.Fprintf(flags.Output(), "  3. Update PANVEX_ENCRYPTION_KEY across the deployment.\n")
		fmt.Fprintf(flags.Output(), "  4. Restart the panel.\n\n")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		return err
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	storageConfig, err := config.ResolveStorage(*storageDriver, *storageDSN)
	if err != nil {
		return err
	}
	store, err := openStore(storageConfig)
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer store.Close()

	reader := bufio.NewReader(os.Stdin)

	currentPass, err := readPassphrase(reader, "Current PANVEX_ENCRYPTION_KEY: ")
	if err != nil {
		return err
	}
	if strings.TrimSpace(currentPass) == "" {
		return errors.New("current passphrase is empty")
	}

	// Boot a vault under the current passphrase. NewWithEnvelope
	// verifies the KEK fingerprint and loads every DEK; a wrong
	// passphrase trips ErrEnvelopeWrongKEK before we ever ask for
	// the new one.
	saltBytes, err := loadOrCreateVaultSaltCLI(ctx, store)
	if err != nil {
		return fmt.Errorf("resolve vault HKDF salt: %w", err)
	}
	adapter := cliCPSecretAdapter{store: store}
	vault, err := secretvault.NewWithEnvelope(ctx, currentPass, secretvault.AllDomains, saltBytes, adapter)
	if err != nil {
		return fmt.Errorf("verify current passphrase: %w", err)
	}
	if !vault.EnvelopeEnabled() {
		return errors.New("vault is in pass-through mode (no PANVEX_ENCRYPTION_KEY); nothing to rotate")
	}

	if !*allowLegacy {
		legacy, err := countLegacyCiphertexts(ctx, store)
		if err != nil {
			return fmt.Errorf("scan for legacy ciphertexts: %w", err)
		}
		if legacy > 0 {
			return fmt.Errorf(
				"%d PVS1/PVS2 ciphertext(s) still present; rotation would invalidate them. "+
					"Run `panvex-control-plane upgrade-vault` first (Wave 5.2 follow-up) "+
					"or pass --allow-legacy-ciphertexts to override (DESTRUCTIVE)",
				legacy,
			)
		}
	}

	newPass, err := readPassphrase(reader, "NEW PANVEX_ENCRYPTION_KEY: ")
	if err != nil {
		return err
	}
	if strings.TrimSpace(newPass) == "" {
		return errors.New("new passphrase is empty")
	}
	if newPass == currentPass {
		return errors.New("new passphrase equals current passphrase — refusing no-op rotation")
	}
	confirmPass, err := readPassphrase(reader, "Confirm NEW PANVEX_ENCRYPTION_KEY: ")
	if err != nil {
		return err
	}
	if confirmPass != newPass {
		return errors.New("confirmation did not match — aborting (no rotation performed)")
	}

	rewrapped, err := vault.RotateKEK(ctx, adapter, newPass)
	if err != nil {
		return fmt.Errorf("rotate: %w", err)
	}

	fmt.Printf("Rotated %d DEK wrapper(s).\n", rewrapped)
	fmt.Println("Update PANVEX_ENCRYPTION_KEY across the deployment before restarting the panel.")
	return nil
}

// readPassphrase reads a single trimmed line from reader after
// writing prompt to stderr. Stderr-only so the prompt doesn't pollute
// pipelines that capture stdout for the success message.
func readPassphrase(reader *bufio.Reader, prompt string) (string, error) {
	fmt.Fprint(os.Stderr, prompt)
	line, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", fmt.Errorf("read passphrase: %w", err)
	}
	return strings.TrimRight(line, "\r\n\t "), nil
}

// countLegacyCiphertexts scans the data columns that hold encrypted
// values and counts how many still carry the PVS1/PVS2 prefix. A
// non-zero result blocks rotation because those values depend on the
// current passphrase directly.
//
// Legacy columns are documented in secretvault.go's Domain constants
// — `clients.secret_ciphertext`,
// `discovered_clients.secret_ciphertext`, `users.totp_secret`,
// `webhook_endpoints.secret_ciphertext`. Falls back to a best-effort
// skip on tables that don't exist (fresh install before the
// corresponding domain ran its migration).
func countLegacyCiphertexts(ctx context.Context, store storage.Store) (int, error) {
	sqlDB, ok := tryStoreSQLDB(store)
	if !ok {
		// Test fixtures / in-memory stores can't be scanned — assume
		// clean.
		return 0, nil
	}
	tables := []struct {
		name   string
		column string
	}{
		{"clients", "secret_ciphertext"},
		{"discovered_clients", "secret_ciphertext"},
		{"users", "totp_secret"},
		{"webhook_endpoints", "secret_ciphertext"},
	}
	total := 0
	for _, t := range tables {
		var n int
		q := fmt.Sprintf(
			"SELECT COUNT(*) FROM %s WHERE %s LIKE 'PVS1:%%' OR %s LIKE 'PVS2:%%'",
			t.name, t.column, t.column,
		)
		if err := sqlDB.QueryRowContext(ctx, q).Scan(&n); err != nil {
			// Schema may not carry the table yet (fresh install
			// before first migration ran), or the column is gone in
			// a future schema. Either way: don't block rotation on
			// a missing table.
			continue
		}
		total += n
	}
	return total, nil
}
