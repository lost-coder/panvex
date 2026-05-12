package main

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
	postgresstore "github.com/lost-coder/panvex/internal/controlplane/storage/postgres"
	sqlitestore "github.com/lost-coder/panvex/internal/controlplane/storage/sqlite"
)

// Helpers shared by the operator-facing subcommands that need a
// boot-equivalent vault (currently rotate-encryption-key; future
// upgrade-vault will reuse them too). These intentionally do NOT
// import internal/controlplane/server — that package pulls the full
// runtime + boot pipeline and would slow every operator command
// while also dragging unwanted dependencies into the cmd layer.

// vaultHKDFSaltStoreKey mirrors the constant in
// internal/controlplane/server/vault_salt.go. Kept in sync by hand;
// see the panic-on-divergence test in
// server/vault_salt_alignment_test.go (added alongside this file).
const vaultHKDFSaltStoreKey = "vault_hkdf_salt_v1"

// loadOrCreateVaultSaltCLI mirrors server.loadOrCreateVaultSalt for
// CLI subcommands. Boots a transient salt when no store is wired
// (mostly tests) and reads / persists the canonical row otherwise.
func loadOrCreateVaultSaltCLI(ctx context.Context, store storage.Store) ([]byte, error) {
	if store == nil {
		fresh := make([]byte, 32)
		if _, err := rand.Read(fresh); err != nil {
			return nil, err
		}
		return fresh, nil
	}
	existing, err := store.GetCPSecret(ctx, vaultHKDFSaltStoreKey)
	if err == nil && len(existing) >= 16 {
		return existing, nil
	}
	if err != nil && !errors.Is(err, storage.ErrNotFound) {
		return nil, fmt.Errorf("load vault HKDF salt: %w", err)
	}
	fresh := make([]byte, 32)
	if _, err := rand.Read(fresh); err != nil {
		return nil, fmt.Errorf("mint vault HKDF salt: %w", err)
	}
	if err := store.PutCPSecret(ctx, vaultHKDFSaltStoreKey, fresh); err != nil {
		return nil, fmt.Errorf("persist vault HKDF salt: %w", err)
	}
	return fresh, nil
}

// cliCPSecretAdapter mirrors server.vaultCPSecretAdapter. Translates
// storage.ErrNotFound to the empty-bytes convention the secretvault
// CPSecretReader interface uses; everything else passes through.
type cliCPSecretAdapter struct {
	store storage.Store
}

func (a cliCPSecretAdapter) GetCPSecret(ctx context.Context, key string) ([]byte, error) {
	if a.store == nil {
		return nil, nil
	}
	value, err := a.store.GetCPSecret(ctx, key)
	if err == nil {
		return value, nil
	}
	if errors.Is(err, storage.ErrNotFound) {
		return nil, nil
	}
	return nil, err
}

func (a cliCPSecretAdapter) PutCPSecret(ctx context.Context, key string, value []byte) error {
	if a.store == nil {
		return nil
	}
	return a.store.PutCPSecret(ctx, key, value)
}

// tryStoreSQLDB extracts the underlying *sql.DB from a concrete
// storage.Store. Used by subcommands that need ad-hoc cross-table
// reads outside the domain repos (e.g. the rotate-encryption-key
// legacy-ciphertext scan). Returns (nil, false) on test fixtures or
// future drivers that don't expose a *sql.DB.
func tryStoreSQLDB(store storage.Store) (*sql.DB, bool) {
	switch s := store.(type) {
	case *sqlitestore.Store:
		return s.DB(), true
	case *postgresstore.Store:
		return s.DB(), true
	default:
		return nil, false
	}
}
