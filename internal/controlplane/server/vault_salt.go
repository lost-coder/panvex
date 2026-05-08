package server

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

// vaultHKDFSaltStoreKey is the cp_secrets row used to persist the
// per-install HKDF salt that the secretvault binds its domain keys to.
// Stable name — once written it must keep the same row so legacy
// PVS2-encrypted values stay decryptable across restarts.
const vaultHKDFSaltStoreKey = "vault_hkdf_salt_v1"

// loadOrCreateVaultSalt returns the persisted per-install HKDF salt,
// generating and persisting a fresh one if no row exists. A store
// without an existing row plus a write failure means the operator
// would lose the only path to decrypt later writes — so we fail loud
// rather than silently fall back to the legacy hard-coded salt.
//
// ctx is the boot-time lifecycle context (s.serverCtx) so Close()
// can abort a wedged storage call (Plan 3 Task 3).
func loadOrCreateVaultSalt(ctx context.Context, store storage.Store) ([]byte, error) {
	if store == nil {
		// No store wired (in-memory dev/tests). Mint a transient salt
		// — values encrypted in this process won't survive a restart,
		// which matches the no-store contract elsewhere.
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
