package auth

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

// ctxSessionStore embeds the interface so only the methods the restore
// path touches need real bodies; it surfaces ctx.Err() so a cancelled
// lifecycle context aborts the restore instead of hanging on storage.
type ctxSessionStore struct{ SessionStore }

func (ctxSessionStore) ListSessions(ctx context.Context) ([]storage.SessionRecord, error) {
	return nil, ctx.Err()
}
func (ctxSessionStore) DeleteExpiredSessions(ctx context.Context, _ time.Time) error {
	return ctx.Err()
}

func TestRestoreSessions_HonoursCancelledContext(t *testing.T) {
	service := NewService()
	service.SetSessionStore(ctxSessionStore{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := service.RestoreSessions(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("RestoreSessions(cancelled ctx) err = %v, want context.Canceled", err)
	}
}

// recordsSessionStore feeds RestoreSessions a fixed set of records; the
// embedded interface panics on any other method (restore touches only
// ListSessions + DeleteExpiredSessions).
type recordsSessionStore struct {
	SessionStore
	records []storage.SessionRecord
}

func (s recordsSessionStore) ListSessions(context.Context) ([]storage.SessionRecord, error) {
	return s.records, nil
}
func (recordsSessionStore) DeleteExpiredSessions(context.Context, time.Time) error {
	return nil
}

// R1.3 (audit 2026-07-07 §1.4): the restore cutoff must honour the
// operator-configured max lifetime, not the compiled-in 8h constant —
// otherwise every panel restart silently logs out sessions the operator
// explicitly allowed to live longer.
func TestRestoreSessions_UsesConfiguredMaxLifetime(t *testing.T) {
	service := NewService()
	service.SetSessionTimeoutFns(
		func() time.Duration { return 720 * time.Hour }, // idle — не мешает
		func() time.Duration { return 168 * time.Hour }, // max lifetime
	)
	now := time.Now().UTC()
	service.SetSessionStore(recordsSessionStore{records: []storage.SessionRecord{
		{ID: "young", UserID: "u1", CreatedAt: now.Add(-9 * time.Hour)},
		{ID: "old", UserID: "u1", CreatedAt: now.Add(-169 * time.Hour)},
	}})

	if err := service.RestoreSessions(context.Background()); err != nil {
		t.Fatalf("RestoreSessions: %v", err)
	}
	if _, err := service.GetSession("young"); err != nil {
		t.Fatalf("session inside configured lifetime must survive restore, got %v", err)
	}
	if _, err := service.GetSession("old"); err == nil {
		t.Fatalf("session beyond configured lifetime must be dropped")
	}
}
