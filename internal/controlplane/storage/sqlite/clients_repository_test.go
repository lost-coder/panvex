package sqlite

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/lost-coder/panvex/internal/controlplane/clients"
	"github.com/lost-coder/panvex/internal/controlplane/clients/storagetest"
)

// seedContractFixtures inserts the minimal referenced rows that the contract
// test data depends on. The contract test uses:
//   - fleet_group id="fg-test"  (referenced by client_assignments.fleet_group_id FK)
//   - agent id="a-1"            (referenced by client_usage.agent_id FK)
//
// SQLite enforces FK constraints with foreign_keys=ON, so these rows must exist
// before the contract's SaveAssignments / UpsertUsageBulk calls.
func seedContractFixtures(t *testing.T, repo clients.Repository) {
	t.Helper()
	// Cast back to *clientsRepository to access the raw dbtx.
	cr, ok := repo.(*clientsRepository)
	if !ok {
		t.Fatal("seedContractFixtures: repo is not *clientsRepository")
	}
	ctx := context.Background()
	// fleet_group
	if _, err := cr.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO fleet_groups (id, name, created_at_unix) VALUES (?, ?, ?)`,
		"fg-test", "fg-test", 0,
	); err != nil {
		t.Fatalf("seed fleet_group: %v", err)
	}
	// agent — minimum required columns
	if _, err := cr.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO agents (id, node_name, last_seen_at_unix) VALUES (?, ?, ?)`,
		"a-1", "a-1", 0,
	); err != nil {
		t.Fatalf("seed agent: %v", err)
	}
}

func TestClientsRepositoryContract_SQLite(t *testing.T) {
	open := func(t *testing.T) clients.Repository {
		t.Helper()
		store, err := Open(filepath.Join(t.TempDir(), "panvex.db"))
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		t.Cleanup(func() { store.Close() })
		repo := NewClientsRepository(store.DB())
		seedContractFixtures(t, repo)
		return repo
	}
	storagetest.RunContract(t, open)
}
