package main

import (
	"github.com/lost-coder/panvex/internal/controlplane/storage"
	postgresstore "github.com/lost-coder/panvex/internal/controlplane/storage/postgres"
	sqlitestore "github.com/lost-coder/panvex/internal/controlplane/storage/sqlite"
	"github.com/lost-coder/panvex/internal/controlplane/webhooks"
)

// webhookStorageFactory returns a closure that, given a vault-backed
// secret decrypter, builds a webhooks.Storage on top of the same
// *sql.DB pool the rest of the control-plane uses.
//
// The factory pattern lets server.New() construct the decrypter
// (which closes over the secretvault.Vault built lazily inside
// initSecrets) without leaking the vault out to this layer. When
// the storage backend is something other than sqlite/postgres
// (typically test fixtures wired by storagetest), the factory
// returns nil and the webhook subsystem stays dormant.
func webhookStorageFactory(store storage.Store) func(webhooks.SecretDecrypter) webhooks.Storage {
	return func(decrypt webhooks.SecretDecrypter) webhooks.Storage {
		switch s := store.(type) {
		case *sqlitestore.Store:
			return sqlitestore.NewWebhookStore(s.DB(), decrypt)
		case *postgresstore.Store:
			return postgresstore.NewWebhookStore(s.DB(), decrypt)
		default:
			return nil
		}
	}
}
