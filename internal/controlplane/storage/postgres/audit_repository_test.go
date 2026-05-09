package postgres

import (
	"os"
	"testing"

	"github.com/lost-coder/panvex/internal/controlplane/audit"
	auditstoragetest "github.com/lost-coder/panvex/internal/controlplane/audit/storagetest"
)

func TestAuditRepositoryContract_Postgres(t *testing.T) {
	if testing.Short() {
		t.Skip("postgres contract test")
	}
	dsn := os.Getenv("PANVEX_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("PANVEX_POSTGRES_TEST_DSN is not set")
	}
	open := func(t *testing.T) audit.Repository {
		t.Helper()
		store, err := Open(dsn)
		if err != nil {
			t.Fatalf("Open() error = %v", err)
		}
		t.Cleanup(func() { _ = store.Close() })
		if err := resetForTest(t.Context(), store); err != nil {
			t.Fatalf("resetForTest() error = %v", err)
		}
		return NewAuditRepository(store.DB())
	}
	auditstoragetest.RunContract(t, open)
}
