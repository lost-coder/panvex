package server

import (
	"context"
	"errors"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

// vaultCPSecretAdapter wraps a storage.Store to satisfy
// secretvault.CPSecretReader without forcing the secretvault package
// to import storage (the latter pulls dbsqlc and would loop back into
// every domain package).
//
// The translation is one rule: GetCPSecret returning storage.ErrNotFound
// becomes (nil, nil) so the envelope code can treat "no row" as the
// first-start signal. Every other error is forwarded verbatim.
type vaultCPSecretAdapter struct {
	store storage.Store
}

func (a vaultCPSecretAdapter) GetCPSecret(ctx context.Context, key string) ([]byte, error) {
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

func (a vaultCPSecretAdapter) PutCPSecret(ctx context.Context, key string, value []byte) error {
	if a.store == nil {
		// In-memory dev/test path: silently drop. Matches the
		// transient-salt behaviour vault_salt.go uses for the same
		// no-store case.
		return nil
	}
	return a.store.PutCPSecret(ctx, key, value)
}
