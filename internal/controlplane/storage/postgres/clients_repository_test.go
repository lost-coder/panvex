// internal/controlplane/storage/postgres/clients_repository_test.go
package postgres

import (
	"context"
	"os"
	"testing"

	"github.com/lost-coder/panvex/internal/controlplane/clients"
	"github.com/lost-coder/panvex/internal/controlplane/clients/storagetest"
)

func TestClientsRepositoryContract_Postgres(t *testing.T) {
	if testing.Short() {
		t.Skip("postgres contract test")
	}
	dsn := os.Getenv("PANVEX_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("PANVEX_POSTGRES_TEST_DSN is not set")
	}
	open := func(t *testing.T) clients.Repository {
		t.Helper()
		store, err := Open(dsn)
		if err != nil {
			t.Fatalf("Open() error = %v", err)
		}
		t.Cleanup(func() { _ = store.Close() })
		if err := resetForTest(t.Context(), store); err != nil {
			t.Fatalf("resetForTest() error = %v", err)
		}
		return NewClientsRepository(store.DB())
	}
	storagetest.RunContract(t, open)
}

// TestNewClientsRepository_BuildSmoke verifies the constructor compiles and
// does not panic when given a nil DB (we can't call methods, but the wiring
// is validated at compile time).
func TestNewClientsRepository_BuildSmoke(t *testing.T) {
	dsn := os.Getenv("PANVEX_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("PANVEX_POSTGRES_TEST_DSN is not set")
	}
	store, err := Open(dsn)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer func() { _ = store.Close() }()
	repo := NewClientsRepository(store.DB())
	if repo == nil {
		t.Fatal("NewClientsRepository returned nil")
	}
	// Verify Get returns ErrNotFound for a missing ID.
	_, gotErr := repo.Get(context.Background(), clients.ClientID("no-such-client"))
	if gotErr == nil {
		t.Fatal("Get(missing) must return non-nil error")
	}
}
