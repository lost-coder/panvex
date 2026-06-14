package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/auth"
	"github.com/lost-coder/panvex/internal/controlplane/config"
	"github.com/lost-coder/panvex/internal/controlplane/secretvault"
	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

func runResetUserTotp(args []string) error {
	flags := flag.NewFlagSet("reset-user-totp", flag.ContinueOnError)
	username := flags.String("username", "", "Username to reset TOTP for")
	storageDriver := flags.String(flagStorageDriver, "", helpStorageDriver)
	storageDSN := flags.String(flagStorageDSN, "", helpStorageDSN)
	encryptionKeyFile := flags.String("encryption-key-file", "", "Path to a file containing PANVEX_ENCRYPTION_KEY (needed to decrypt an encrypted TOTP secret)")
	encryptionKeyStdin := flags.Bool("encryption-key-stdin", false, "Read PANVEX_ENCRYPTION_KEY from stdin")
	if err := flags.Parse(args); err != nil {
		return err
	}

	if *username == "" {
		return errors.New("username is required")
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	storageConfig, err := config.ResolveStorage(*storageDriver, *storageDSN)
	if err != nil {
		return err
	}

	store, err := openStore(ctx, storageConfig)
	if err != nil {
		return err
	}
	defer store.Close()

	record, err := store.GetUserByUsername(ctx, *username)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return fmt.Errorf("user %q not found", *username)
		}
		return err
	}

	service := auth.NewServiceWithStore(store)

	// Wire the vault so ResetTotp's GetUserByID can decrypt an encrypted
	// TOTP secret. Without this, a production install (PANVEX_ENCRYPTION_KEY
	// set) fails the reset because the stored secret can't be opened —
	// reset would only ever work for plaintext/dev installs.
	encryptionKey, err := resolveEncryptionKey(*encryptionKeyFile, *encryptionKeyStdin)
	if err != nil {
		return err
	}
	if encryptionKey != "" {
		saltBytes, err := loadOrCreateVaultSaltCLI(ctx, store)
		if err != nil {
			return fmt.Errorf("resolve vault HKDF salt: %w", err)
		}
		vault, err := secretvault.NewWithEnvelope(ctx, encryptionKey, secretvault.AllDomains, saltBytes, cliCPSecretAdapter{store: store})
		if err != nil {
			return fmt.Errorf("init vault: %w", err)
		}
		service.SetVault(vault)
	}

	// Wire the session store so the reset also revokes the user's persisted
	// sessions — a TOTP reset is an account-recovery action and must not
	// leave a live cookie usable after the second factor is cleared.
	service.SetSessionStore(store)
	// RevokeSessionsForUser only deletes store rows for sessions present in
	// the in-memory map, which a fresh CLI Service starts empty. Hydrate it
	// from the store first so the revocation actually reaches the persisted
	// rows (otherwise the reset would clear TOTP but leave live cookies valid
	// until the next panel restart rehydrated — and then kept — them).
	if err := service.RestoreSessions(context.Background()); err != nil {
		return fmt.Errorf("restore sessions for revocation: %w", err)
	}

	user, err := service.ResetTotp(ctx, record.ID)
	if err != nil {
		return err
	}

	if err := store.AppendAuditEvent(ctx, storage.AuditEventRecord{
		ID:        fmt.Sprintf("audit-cli-%d", time.Now().UTC().UnixNano()),
		ActorID:   "system",
		Action:    "auth.totp.reset_by_cli",
		TargetID:  user.ID,
		CreatedAt: time.Now().UTC(),
		Details: map[string]any{
			"username": user.Username,
		},
	}); err != nil {
		return err
	}

	fmt.Printf("TOTP reset for user %q.\n", user.Username)
	return nil
}
