// internal/controlplane/storage/postgres/discovered_repository_test.go
package postgres

import (
	"context"
	"os"
	"testing"

	"github.com/lost-coder/panvex/internal/controlplane/discovered"
	"github.com/lost-coder/panvex/internal/controlplane/discovered/storagetest"
)

// seedAgentsForDiscoveredContract inserts placeholder agent rows for every
// agent ID referenced by the discovered repository contract. discovered_clients
// has an agent_id FK → agents(id), so these must exist before Save is called.
func seedAgentsForDiscoveredContract(ctx context.Context, t *testing.T, store *Store) {
	t.Helper()
	agentIDs := []string{"a-1", "a-byname", "a-list", "a-other", "a-upd", "a-bulk", "a-del"}
	for _, id := range agentIDs {
		if _, err := store.sqlDB.ExecContext(ctx,
			`INSERT INTO agents (id, node_name, last_seen_at) VALUES ($1, $2, NOW())
			 ON CONFLICT (id) DO NOTHING`,
			id, id,
		); err != nil {
			t.Fatalf("seedAgentsForDiscoveredContract id=%s: %v", id, err)
		}
	}
}

func TestDiscoveredRepositoryContract_Postgres(t *testing.T) {
	if testing.Short() {
		t.Skip("postgres contract test")
	}
	dsn := os.Getenv("PANVEX_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("PANVEX_POSTGRES_TEST_DSN is not set")
	}
	open := func(t *testing.T) discovered.Repository {
		t.Helper()
		store, err := Open(dsn)
		if err != nil {
			t.Fatalf("Open() error = %v", err)
		}
		t.Cleanup(func() { _ = store.Close() })
		ctx := t.Context()
		if err := resetForTest(ctx, store); err != nil {
			t.Fatalf("resetForTest() error = %v", err)
		}
		seedAgentsForDiscoveredContract(ctx, t, store)
		return NewDiscoveredRepository(store.DB())
	}
	storagetest.RunContract(t, open)
}
