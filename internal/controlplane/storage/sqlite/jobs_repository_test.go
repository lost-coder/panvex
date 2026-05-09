package sqlite

import (
	"path/filepath"
	"testing"

	"github.com/lost-coder/panvex/internal/controlplane/jobs"
	jobsstoragetest "github.com/lost-coder/panvex/internal/controlplane/jobs/storagetest"
)

func TestJobsRepositoryContract_SQLite(t *testing.T) {
	open := func(t *testing.T) jobs.Repository {
		t.Helper()
		store, err := Open(filepath.Join(t.TempDir(), "panvex.db"))
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		t.Cleanup(func() { store.Close() })
		return NewJobsRepository(store.DB())
	}
	jobsstoragetest.RunContract(t, open)
}
