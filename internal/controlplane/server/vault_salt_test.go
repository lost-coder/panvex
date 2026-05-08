package server

import (
	"context"
	"errors"
	"testing"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

// vaultCtxCapturingStore is a minimal storage.Store stub used by the
// vault-salt cancellation test. The CSRF loader has its own copy in
// the csrf package; the vault salt loader stays in server, so its
// stub lives alongside.
type vaultCtxCapturingStore struct {
	storage.Store
}

func (vaultCtxCapturingStore) GetCPSecret(ctx context.Context, _ string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return nil, storage.ErrNotFound
}

func (vaultCtxCapturingStore) PutCPSecret(ctx context.Context, _ string, _ []byte) error {
	return ctx.Err()
}

// TestLoadOrCreateVaultSalt_RespectsContextCancellation pins the
// fail-loud contract for the vault HKDF salt loader. Unlike the CSRF
// loader, a storage error must propagate so the operator does not
// silently lose the only path to decrypt later writes.
func TestLoadOrCreateVaultSalt_RespectsContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := loadOrCreateVaultSalt(ctx, vaultCtxCapturingStore{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("loadOrCreateVaultSalt error = %v, want context.Canceled", err)
	}
}
