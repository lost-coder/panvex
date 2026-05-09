// internal/controlplane/storage/sqlite/uow_test.go
package sqlite

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/clients"
	"github.com/lost-coder/panvex/internal/controlplane/storage"
	"github.com/lost-coder/panvex/internal/controlplane/storage/uow"
)

// openTestStoreForUoW opens a fresh in-file SQLite store for a UoW test.
func openTestStoreForUoW(t *testing.T) *Store {
	t.Helper()
	store, err := Open(filepath.Join(t.TempDir(), "panvex_uow_test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

// makeTestClientForUoW returns a minimal clients.Client with the given ID.
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

func TestUoW_CommitOnSuccess_SQLite(t *testing.T) {
	store := openTestStoreForUoW(t)
	u := NewUoW(store.DB())

	c := makeTestClientForUoW("uow-sq-commit-1")
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

func TestUoW_RollbackOnError_SQLite(t *testing.T) {
	store := openTestStoreForUoW(t)
	u := NewUoW(store.DB())

	wantErr := errors.New("boom")
	c := makeTestClientForUoW("uow-sq-rollback-1")
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

func TestUoW_PanicPropagates_SQLite(t *testing.T) {
	store := openTestStoreForUoW(t)
	u := NewUoW(store.DB())

	c := makeTestClientForUoW("uow-sq-panic-1")
	sentinel := errors.New("test panic sentinel")

	func() {
		defer func() {
			if p := recover(); p == nil {
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
