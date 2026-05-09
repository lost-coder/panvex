// internal/controlplane/storage/sqlite/discovered_repository_test.go
package sqlite

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/lost-coder/panvex/internal/controlplane/discovered"
	"github.com/lost-coder/panvex/internal/controlplane/discovered/storagetest"
)

// seedAgentsForDiscoveredContract inserts placeholder agent rows for every
// agent ID referenced by the discovered repository contract. discovered_clients
// has an agent_id FK → agents(id), so these must exist before Save is called.
// SQLite enforces FK constraints with foreign_keys=ON.
func seedAgentsForDiscoveredContract(t *testing.T, db dbtx) {
	t.Helper()
	agentIDs := []string{"a-1", "a-byname", "a-list", "a-other", "a-upd", "a-bulk", "a-del"}
	ctx := context.Background()
	for _, id := range agentIDs {
		if _, err := db.ExecContext(ctx,
			`INSERT OR IGNORE INTO agents (id, node_name, last_seen_at_unix) VALUES (?, ?, 0)`,
			id, id,
		); err != nil {
			t.Fatalf("seedAgentsForDiscoveredContract id=%s: %v", id, err)
		}
	}
}

func TestDiscoveredRepositoryContract_SQLite(t *testing.T) {
	open := func(t *testing.T) discovered.Repository {
		t.Helper()
		store, err := Open(filepath.Join(t.TempDir(), "panvex.db"))
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		t.Cleanup(func() { store.Close() })
		db := store.DB()
		seedAgentsForDiscoveredContract(t, db)
		return NewDiscoveredRepository(db)
	}
	storagetest.RunContract(t, open)
}
