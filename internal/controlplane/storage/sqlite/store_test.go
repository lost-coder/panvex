package sqlite

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
	"github.com/lost-coder/panvex/internal/controlplane/storage/storagetest"
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

func TestOpenCreatesParentDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data", "panvex.db")

	store, err := Open(path)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}

	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	if _, err := os.Stat(filepath.Dir(path)); err != nil {
		t.Fatalf("Stat(parent) error = %v", err)
	}
}

func TestStoreContract(t *testing.T) {
	storagetest.RunStoreContract(t, func(t *testing.T) storage.MigrationStore {
		t.Helper()

		store, err := Open(filepath.Join(t.TempDir(), "panvex.db"))
		if err != nil {
			t.Fatalf("Open() error = %v", err)
		}

		return store
	})
}

// TestJSONValidationContract (M3) asserts SQLite rejects malformed JSON on
// the `config` columns guarded by the json_valid CHECK constraints added
// in db/migrations/sqlite/0052_json_valid_checks.sql, achieving write-time
// parity with PostgreSQL's native JSONB validation.
func TestJSONValidationContract(t *testing.T) {
	open := func(t *testing.T) storage.MigrationStore {
		t.Helper()

		store, err := Open(filepath.Join(t.TempDir(), "panvex.db"))
		if err != nil {
			t.Fatalf("Open() error = %v", err)
		}

		return store
	}
	storagetest.RunJSONValidationContract(t, open)

	// jobs.payload_json is TEXT (not JSONB) on PostgreSQL too, so the
	// json_valid CHECK on it is SQLite-only hardening, not cross-backend
	// parity — see RunSQLiteOnlyJSONValidationContract's doc comment.
	storagetest.RunSQLiteOnlyJSONValidationContract(t, open)
}
