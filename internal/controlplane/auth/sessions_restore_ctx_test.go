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
