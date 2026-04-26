package storagetest

import (
	"context"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

// runSessionsContract extracts the session lifecycle contract blocks from
// the historic store_contract.go monolith (R-Q-18). RunStoreContract
// dispatches into it so each backend exercises the same coverage.
func runSessionsContract(t *testing.T, open OpenStore) {
	t.Helper()

	t.Run("session put get delete round trip", func(t *testing.T) {
		store := open(t)
		defer store.Close()

		ctx := context.Background()
		// P2-DB-03: sessions.user_id is now a CASCADE FK to users(id).
		// Seed the owning user before persisting the session.
		if err := store.PutUser(ctx, storage.UserRecord{
			ID:           "user-001",
			Username:     "session-user",
			PasswordHash: "argon2id$hash",
			Role:         "admin",
			CreatedAt:    time.Date(2026, time.April, 15, 9, 0, 0, 0, time.UTC),
		}); err != nil {
			t.Fatalf("PutUser() error = %v", err)
		}
		session := storage.SessionRecord{
			ID:        "sess-001",
			UserID:    "user-001",
			CreatedAt: time.Date(2026, time.April, 15, 10, 0, 0, 0, time.UTC),
		}

		if err := store.PutSession(ctx, session); err != nil {
			t.Fatalf("PutSession() error = %v", err)
		}

		got, err := store.GetSession(ctx, session.ID)
		if err != nil {
			t.Fatalf("GetSession() error = %v", err)
		}
		if got.UserID != session.UserID {
			t.Fatalf("GetSession().UserID = %q, want %q", got.UserID, session.UserID)
		}

		sessions, err := store.ListSessions(ctx)
		if err != nil {
			t.Fatalf("ListSessions() error = %v", err)
		}
		if len(sessions) != 1 {
			t.Fatalf("len(ListSessions()) = %d, want 1", len(sessions))
		}

		if err := store.DeleteSession(ctx, session.ID); err != nil {
			t.Fatalf("DeleteSession() error = %v", err)
		}

		_, err = store.GetSession(ctx, session.ID)
		if err == nil {
			t.Fatal("GetSession() after delete returned nil error, want ErrNotFound")
		}
	})

	t.Run("session delete expired removes old sessions", func(t *testing.T) {
		store := open(t)
		defer store.Close()

		ctx := context.Background()
		// P2-DB-03: seed users referenced by the sessions below.
		for _, u := range []storage.UserRecord{
			{ID: "user-001", Username: "session-u1", PasswordHash: "h", Role: "admin", CreatedAt: time.Date(2026, time.April, 14, 7, 0, 0, 0, time.UTC)},
			{ID: "user-002", Username: "session-u2", PasswordHash: "h", Role: "admin", CreatedAt: time.Date(2026, time.April, 15, 11, 0, 0, 0, time.UTC)},
		} {
			if err := store.PutUser(ctx, u); err != nil {
				t.Fatalf("PutUser(%s) error = %v", u.ID, err)
			}
		}
		old := storage.SessionRecord{
			ID:        "sess-old",
			UserID:    "user-001",
			CreatedAt: time.Date(2026, time.April, 14, 8, 0, 0, 0, time.UTC),
		}
		fresh := storage.SessionRecord{
			ID:        "sess-fresh",
			UserID:    "user-002",
			CreatedAt: time.Date(2026, time.April, 15, 12, 0, 0, 0, time.UTC),
		}

		if err := store.PutSession(ctx, old); err != nil {
			t.Fatalf("PutSession(old) error = %v", err)
		}
		if err := store.PutSession(ctx, fresh); err != nil {
			t.Fatalf("PutSession(fresh) error = %v", err)
		}

		cutoff := time.Date(2026, time.April, 15, 0, 0, 0, 0, time.UTC)
		if err := store.DeleteExpiredSessions(ctx, cutoff); err != nil {
			t.Fatalf("DeleteExpiredSessions() error = %v", err)
		}

		sessions, err := store.ListSessions(ctx)
		if err != nil {
			t.Fatalf("ListSessions() error = %v", err)
		}
		if len(sessions) != 1 {
			t.Fatalf("len(ListSessions()) after expiry = %d, want 1", len(sessions))
		}
		if sessions[0].ID != fresh.ID {
			t.Fatalf("remaining session ID = %q, want %q", sessions[0].ID, fresh.ID)
		}
	})


}
