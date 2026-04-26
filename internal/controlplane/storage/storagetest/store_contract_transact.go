package storagetest

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

// runTransactContract exercises the Store.Transact contract that
// every backend must satisfy (Q5.U-Q-18: split out of the
// store_contract.go monolith). RunStoreContract calls this directly.
func runTransactContract(t *testing.T, open OpenStore) {
	t.Helper()

	// --- Transact contract (P2-ARCH-01) ---

	t.Run("Transact commits on nil return", func(t *testing.T) {
		store := open(t)
		defer store.Close()

		ctx := context.Background()
		group := storage.FleetGroupRecord{
			ID:        "tx-commit-group",
			Name:      "tx-commit-group",
			CreatedAt: time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
		}
		client := storage.ClientRecord{
			ID:        "tx-commit-client",
			Name:      "tx-commit-client",
			SecretCiphertext: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			CreatedAt: group.CreatedAt,
			UpdatedAt: group.CreatedAt,
		}

		if err := store.Transact(ctx, func(tx storage.Store) error {
			if err := tx.PutFleetGroup(ctx, group); err != nil {
				return err
			}
			return tx.PutClient(ctx, client)
		}); err != nil {
			t.Fatalf("Transact() commit error = %v", err)
		}

		got, err := store.GetClientByID(ctx, client.ID)
		if err != nil {
			t.Fatalf("GetClientByID() after commit error = %v", err)
		}
		if got.ID != client.ID {
			t.Fatalf("GetClientByID().ID = %q, want %q", got.ID, client.ID)
		}
	})

	t.Run("Transact rolls back on fn error", func(t *testing.T) {
		store := open(t)
		defer store.Close()

		ctx := context.Background()
		group := storage.FleetGroupRecord{
			ID:        "tx-rollback-group",
			Name:      "tx-rollback-group",
			CreatedAt: time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
		}
		client := storage.ClientRecord{
			ID:        "tx-rollback-client",
			Name:      "tx-rollback-client",
			SecretCiphertext: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			CreatedAt: group.CreatedAt,
			UpdatedAt: group.CreatedAt,
		}

		sentinel := errors.New("sentinel rollback")
		err := store.Transact(ctx, func(tx storage.Store) error {
			if err := tx.PutFleetGroup(ctx, group); err != nil {
				return err
			}
			if err := tx.PutClient(ctx, client); err != nil {
				return err
			}
			return sentinel
		})
		if !errors.Is(err, sentinel) {
			t.Fatalf("Transact() err = %v, want %v", err, sentinel)
		}

		if _, err := store.GetClientByID(ctx, client.ID); !errors.Is(err, storage.ErrNotFound) {
			t.Fatalf("GetClientByID() after rollback err = %v, want ErrNotFound", err)
		}
	})

	t.Run("Transact rolls back on panic and re-raises", func(t *testing.T) {
		store := open(t)
		defer store.Close()

		ctx := context.Background()
		client := storage.ClientRecord{
			ID:        "tx-panic-client",
			Name:      "tx-panic-client",
			SecretCiphertext: "cccccccccccccccccccccccccccccccc",
			CreatedAt: time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
			UpdatedAt: time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
		}

		func() {
			defer func() {
				if r := recover(); r == nil {
					t.Fatalf("expected panic to propagate out of Transact")
				}
			}()
			_ = store.Transact(ctx, func(tx storage.Store) error {
				if err := tx.PutClient(ctx, client); err != nil {
					t.Fatalf("PutClient inside Transact error = %v", err)
				}
				panic("boom")
			})
		}()

		if _, err := store.GetClientByID(ctx, client.ID); !errors.Is(err, storage.ErrNotFound) {
			t.Fatalf("GetClientByID() after panic-rollback err = %v, want ErrNotFound", err)
		}
	})

	t.Run("Transact returns ErrNestedTransact on nested call", func(t *testing.T) {
		store := open(t)
		defer store.Close()

		ctx := context.Background()
		var inner error
		outer := store.Transact(ctx, func(tx storage.Store) error {
			inner = tx.Transact(ctx, func(storage.Store) error { return nil })
			return nil
		})
		if outer != nil {
			t.Fatalf("outer Transact() err = %v, want nil", outer)
		}
		if !errors.Is(inner, storage.ErrNestedTransact) {
			t.Fatalf("inner Transact() err = %v, want ErrNestedTransact", inner)
		}
	})

	t.Run("Transact aborts when context cancelled before fn", func(t *testing.T) {
		store := open(t)
		defer store.Close()

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		err := store.Transact(ctx, func(tx storage.Store) error {
			t.Fatalf("fn should not run after ctx cancellation")
			return nil
		})
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Transact() err = %v, want context.Canceled", err)
		}
	})

	t.Run("Transact serializes concurrent writers on same row", func(t *testing.T) {
		store := open(t)
		defer store.Close()

		ctx := context.Background()
		group := storage.FleetGroupRecord{
			ID:        "tx-concurrent-group",
			Name:      "tx-concurrent-group",
			CreatedAt: time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
		}
		if err := store.PutFleetGroup(ctx, group); err != nil {
			t.Fatalf("PutFleetGroup() error = %v", err)
		}

		const clientID = "tx-concurrent-client"
		type result struct {
			err    error
			winner string
		}
		results := make(chan result, 2)
		run := func(name string) {
			err := store.Transact(ctx, func(tx storage.Store) error {
				client := storage.ClientRecord{
					ID:        clientID,
					Name:      name,
					SecretCiphertext: "dddddddddddddddddddddddddddddddd",
					CreatedAt: time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
					UpdatedAt: time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
				}
				if err := tx.PutClient(ctx, client); err != nil {
					return err
				}
				assignment := storage.ClientAssignmentRecord{
					ID:           name + "-assignment",
					ClientID:     clientID,
					FleetGroupID: group.ID,
					CreatedAt:    client.CreatedAt,
				}
				return tx.PutClientAssignment(ctx, assignment)
			})
			results <- result{err: err, winner: name}
		}
		go run("name-a")
		go run("name-b")
		r1 := <-results
		r2 := <-results

		if r1.err != nil && r2.err != nil {
			t.Fatalf("both Transacts failed: r1=%v r2=%v", r1.err, r2.err)
		}

		got, err := store.GetClientByID(ctx, clientID)
		if err != nil {
			t.Fatalf("GetClientByID() error = %v", err)
		}
		if got.Name != "name-a" && got.Name != "name-b" {
			t.Fatalf("GetClientByID().Name = %q, want name-a or name-b", got.Name)
		}

		assignments, err := store.ListClientAssignments(ctx, clientID)
		if err != nil {
			t.Fatalf("ListClientAssignments() error = %v", err)
		}
		if len(assignments) == 0 {
			t.Fatalf("expected at least one assignment from the winning Transact")
		}
	})

	t.Run("Transact rejects nil fn", func(t *testing.T) {
		store := open(t)
		defer store.Close()

		if err := store.Transact(context.Background(), nil); err == nil {
			t.Fatalf("Transact(nil) err = nil, want non-nil")
		}
	})

}
