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
		groupA := storage.FleetGroupRecord{
			ID:        "00000000-0000-4000-8000-000000000009",
			Name:      "tx-commit-group-a",
			CreatedAt: time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
		}
		groupB := storage.FleetGroupRecord{
			ID:        "00000000-0000-4000-8000-00000000000a",
			Name:      "tx-commit-group-b",
			CreatedAt: time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
		}

		if err := store.Transact(ctx, func(tx storage.Store) error {
			if err := tx.PutFleetGroup(ctx, groupA); err != nil {
				return err
			}
			return tx.PutFleetGroup(ctx, groupB)
		}); err != nil {
			t.Fatalf("Transact() commit error = %v", err)
		}

		got, err := store.GetFleetGroup(ctx, groupB.ID)
		if err != nil {
			t.Fatalf("GetFleetGroup() after commit error = %v", err)
		}
		if got.ID != groupB.ID {
			t.Fatalf("GetFleetGroup().ID = %q, want %q", got.ID, groupB.ID)
		}
	})

	t.Run("Transact rolls back on fn error", func(t *testing.T) {
		store := open(t)
		defer store.Close()

		ctx := context.Background()
		groupA := storage.FleetGroupRecord{
			ID:        "00000000-0000-4000-8000-00000000000c",
			Name:      "tx-rollback-group-a",
			CreatedAt: time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
		}
		groupB := storage.FleetGroupRecord{
			ID:        "00000000-0000-4000-8000-00000000000d",
			Name:      "tx-rollback-group-b",
			CreatedAt: time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
		}

		sentinel := errors.New("sentinel rollback")
		err := store.Transact(ctx, func(tx storage.Store) error {
			if err := tx.PutFleetGroup(ctx, groupA); err != nil {
				return err
			}
			if err := tx.PutFleetGroup(ctx, groupB); err != nil {
				return err
			}
			return sentinel
		})
		if !errors.Is(err, sentinel) {
			t.Fatalf("Transact() err = %v, want %v", err, sentinel)
		}

		if _, err := store.GetFleetGroup(ctx, groupB.ID); !errors.Is(err, storage.ErrNotFound) {
			t.Fatalf("GetFleetGroup() after rollback err = %v, want ErrNotFound", err)
		}
	})

	t.Run("Transact rolls back on panic and re-raises", func(t *testing.T) {
		store := open(t)
		defer store.Close()

		ctx := context.Background()
		group := storage.FleetGroupRecord{
			ID:        "00000000-0000-4000-8000-00000000000b",
			Name:      "tx-panic-group",
			CreatedAt: time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
		}

		func() {
			defer func() {
				if recover() == nil {
					t.Fatalf("expected panic to propagate out of Transact")
				}
			}()
			_ = store.Transact(ctx, func(tx storage.Store) error {
				if err := tx.PutFleetGroup(ctx, group); err != nil {
					t.Fatalf("PutFleetGroup inside Transact error = %v", err)
				}
				panic("boom")
			})
		}()

		if _, err := store.GetFleetGroup(ctx, group.ID); !errors.Is(err, storage.ErrNotFound) {
			t.Fatalf("GetFleetGroup() after panic-rollback err = %v, want ErrNotFound", err)
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

		const groupID = "00000000-0000-4000-8000-00000000000e"
		type result struct {
			err    error
			winner string
		}
		results := make(chan result, 2)
		run := func(name string) {
			err := store.Transact(ctx, func(tx storage.Store) error {
				group := storage.FleetGroupRecord{
					ID:        groupID,
					Name:      name,
					CreatedAt: time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
				}
				return tx.PutFleetGroup(ctx, group)
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

		got, err := store.GetFleetGroup(ctx, groupID)
		if err != nil {
			t.Fatalf("GetFleetGroup() error = %v", err)
		}
		if got.Name != "name-a" && got.Name != "name-b" {
			t.Fatalf("GetFleetGroup().Name = %q, want name-a or name-b", got.Name)
		}
	})

	t.Run("Transact rejects nil fn", func(t *testing.T) {
		store := open(t)
		defer store.Close()

		if err := store.Transact(context.Background(), nil); err == nil {
			t.Fatalf("Transact(nil) err = nil, want non-nil")
		}
	})

	t.Run("consume token inside transact rolls back with the tx", func(t *testing.T) {
		store := open(t)
		defer store.Close()

		ctx := context.Background()
		if err := store.PutFleetGroup(ctx, storage.FleetGroupRecord{
			ID:        testFleetGroupID,
			Name:      "Default",
			CreatedAt: time.Date(2026, time.March, 15, 8, 0, 0, 0, time.UTC),
		}); err != nil {
			t.Fatalf("PutFleetGroup() error = %v", err)
		}
		token := storage.EnrollmentTokenRecord{
			Value:        "tx-rollback-token",
			FleetGroupID: testFleetGroupID,
			IssuedAt:     time.Date(2026, time.March, 15, 8, 5, 0, 0, time.UTC),
			ExpiresAt:    time.Date(2026, time.March, 15, 9, 0, 0, 0, time.UTC),
		}
		if err := store.PutEnrollmentToken(ctx, token); err != nil {
			t.Fatalf("PutEnrollmentToken() error = %v", err)
		}

		sentinel := errors.New("forced rollback after consume")
		err := store.Transact(ctx, func(tx storage.Store) error {
			if _, err := tx.ConsumeEnrollmentToken(ctx, token.Value, time.Date(2026, time.March, 15, 8, 10, 0, 0, time.UTC)); err != nil {
				return err
			}
			return sentinel
		})
		if !errors.Is(err, sentinel) {
			t.Fatalf("Transact() error = %v, want sentinel rollback error", err)
		}

		rec, err := store.GetEnrollmentToken(ctx, token.Value)
		if err != nil {
			t.Fatalf("GetEnrollmentToken() error = %v", err)
		}
		if rec.ConsumedAt != nil {
			t.Fatalf("token consumed despite rollback: ConsumedAt = %v, want nil", rec.ConsumedAt)
		}
	})

}
