package sqlite

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/panvex/panvex/internal/controlplane/storage"
	"github.com/panvex/panvex/internal/controlplane/storage/storagetest"
)

func TestOpenCreatesSQLiteDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "panvex.db")

	store, err := Open(path)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}

	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
}

func TestStoreContract(t *testing.T) {
	storagetest.RunStoreContract(t, func(t *testing.T) storage.Store {
		t.Helper()

		store, err := Open(filepath.Join(t.TempDir(), "panvex.db"))
		if err != nil {
			t.Fatalf("Open() error = %v", err)
		}

		return store
	})
}
