package server

import (
	"context"
	"errors"

	"github.com/lost-coder/panvex/internal/controlplane/secretvault"
	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

// vaultCPSecretAdapter wraps a storage.Store to satisfy
// secretvault.CPSecretReader without forcing the secretvault package
// to import storage (the latter pulls dbsqlc and would loop back into
// every domain package).
//
// Translation rules:
//   - GetCPSecret returning storage.ErrNotFound becomes (nil, nil) so
//     the envelope code can treat "no row" as the first-start signal.
//   - PutCPSecret with a nil store returns an explicit error so a
//     test fixture cannot silently drop a vault write and later
//     surface the loss as "DEK re-minted under different bytes."
//     (Earlier revision returned nil; finding #4 in the 2026-05-12
//     code review.)
type vaultCPSecretAdapter struct {
	store storage.Store
}

func (a vaultCPSecretAdapter) GetCPSecret(ctx context.Context, key string) ([]byte, error) {
	if a.store == nil {
		return nil, errAdapterNoStore
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
		return errAdapterNoStore
	}
	return a.store.PutCPSecret(ctx, key, value)
}

// WithCPSecretTx satisfies secretvault.TxCPSecretStore. Reuses the
// store's existing Transact infrastructure (SQLite: BEGIN IMMEDIATE;
// Postgres: serializable with retry on SQLSTATE 40001) so the
// RotateKEK re-wrap + fingerprint write land atomically.
func (a vaultCPSecretAdapter) WithCPSecretTx(ctx context.Context, fn func(tx secretvault.CPSecretReader) error) error {
	if a.store == nil {
		return errAdapterNoStore
	}
	return a.store.Transact(ctx, func(tx storage.Store) error {
		return fn(vaultCPSecretAdapter{store: tx})
	})
}

var errAdapterNoStore = errors.New("vault adapter: store unset (envelope mode requires a real CPSecretStore)")
