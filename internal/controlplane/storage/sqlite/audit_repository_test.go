package sqlite

import (
	"path/filepath"
	"testing"

	"github.com/lost-coder/panvex/internal/controlplane/audit"
	auditstoragetest "github.com/lost-coder/panvex/internal/controlplane/audit/storagetest"
)

func TestAuditRepositoryContract_SQLite(t *testing.T) {
	open := func(t *testing.T) audit.Repository {
		t.Helper()
		store, err := Open(filepath.Join(t.TempDir(), "panvex.db"))
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		t.Cleanup(func() { store.Close() })
		return NewAuditRepository(store.DB())
	}
	auditstoragetest.RunContract(t, open)
}
