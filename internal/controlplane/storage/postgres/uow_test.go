// internal/controlplane/storage/postgres/uow_test.go
package postgres

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/clients"
	"github.com/lost-coder/panvex/internal/controlplane/storage"
	"github.com/lost-coder/panvex/internal/controlplane/storage/uow"
)

// openTestStore opens a postgres store and resets it for a test. Skips if no
// DSN is available.
func openTestStoreForUoW(t *testing.T) *Store {
	t.Helper()
	if testing.Short() {
		t.Skip("postgres uow test requires database")
	}
	dsn := os.Getenv("PANVEX_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("PANVEX_POSTGRES_TEST_DSN is not set")
	}
	store, err := Open(dsn)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := resetForTest(t.Context(), store); err != nil {
		t.Fatalf("resetForTest() error = %v", err)
	}
	return store
}

// makeTestClient returns a minimal clients.Client with a unique ID.
func makeTestClientForUoW(id string) clients.Client {
	now := time.Unix(1700000000, 0).UTC()
	return clients.Client{
		ID:        clients.ClientID(id),
		Name:      id,
		Secret:    "opaque-secret",
		CreatedAt: now,
		UpdatedAt: now,
	}
}

func TestUoW_CommitOnSuccess_Postgres(t *testing.T) {
	store := openTestStoreForUoW(t)
	u := NewUoW(store.DB())

	c := makeTestClientForUoW("uow-pg-commit-1")
	err := u.Do(context.Background(), func(rs uow.RepoSet) error {
		return rs.Clients().Save(context.Background(), c)
	})
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}

	// Row must be visible outside the transaction.
	repo := NewClientsRepository(store.DB())
	got, err := repo.Get(context.Background(), c.ID)
	if err != nil {
		t.Fatalf("Get after commit: %v", err)
	}
	if got.ID != c.ID {
		t.Fatalf("Get returned ID %q, want %q", got.ID, c.ID)
	}
}

func TestUoW_RollbackOnError_Postgres(t *testing.T) {
	store := openTestStoreForUoW(t)
	u := NewUoW(store.DB())

	wantErr := errors.New("boom")
	c := makeTestClientForUoW("uow-pg-rollback-1")
	err := u.Do(context.Background(), func(rs uow.RepoSet) error {
		if saveErr := rs.Clients().Save(context.Background(), c); saveErr != nil {
			t.Fatalf("Save inside Tx: %v", saveErr)
		}
		return wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("Do() err = %v, want %v", err, wantErr)
	}

	// Row must NOT be visible — the transaction rolled back.
	repo := NewClientsRepository(store.DB())
	_, getErr := repo.Get(context.Background(), c.ID)
	if !errors.Is(getErr, storage.ErrNotFound) {
		t.Fatalf("after rollback Get returned err=%v, want ErrNotFound", getErr)
	}
}

func TestUoW_PanicPropagates_Postgres(t *testing.T) {
	store := openTestStoreForUoW(t)
	u := NewUoW(store.DB())

	c := makeTestClientForUoW("uow-pg-panic-1")
	sentinel := errors.New("test panic sentinel")

	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("expected panic to propagate out of Do()")
			}
		}()
		_ = u.Do(context.Background(), func(rs uow.RepoSet) error {
			if saveErr := rs.Clients().Save(context.Background(), c); saveErr != nil {
				t.Fatalf("Save inside Tx: %v", saveErr)
			}
			panic(sentinel)
		})
	}()

	// Row must NOT be visible — the transaction rolled back on panic.
	repo := NewClientsRepository(store.DB())
	_, getErr := repo.Get(context.Background(), c.ID)
	if !errors.Is(getErr, storage.ErrNotFound) {
		t.Fatalf("after panic rollback Get returned err=%v, want ErrNotFound", getErr)
	}
}
