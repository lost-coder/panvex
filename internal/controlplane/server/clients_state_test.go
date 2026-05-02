package server

import (
	"context"
	"errors"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
	"github.com/lost-coder/panvex/internal/controlplane/storage/sqlite"
)

// slowListClientsStore wraps a Store and stalls inside ListClients when the
// armed flag is set so the test can prove restoreStoredClients honours its
// parent (s.serverCtx) ctx cancellation rather than waiting the full 60s
// WithTimeout budget. The flag-controlled stall keeps initial boot fast
// (boot calls restoreStoredClients once via lifecycle.go).
type slowListClientsStore struct {
	storage.Store
	stall time.Duration
	armed atomic.Bool
}

func (s *slowListClientsStore) ListClients(ctx context.Context) ([]storage.ClientRecord, error) {
	if s.armed.Load() {
		select {
		case <-time.After(s.stall):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return s.Store.ListClients(ctx)
}

// TestClientsState_RestoreHonoursServerCtxCancellation pins the BP-01 fix
// in clients_state.go: the WithTimeout used to be parented to
// context.Background(), so a Close() during a slow restore would still
// have to wait the full 60s budget. With s.serverCtx as the parent,
// cancelling serverCancel must abort restoreStoredClients within a few
// hundred ms.
func TestClientsState_RestoreHonoursServerCtxCancellation(t *testing.T) {
	t.Parallel()

	base, err := sqlite.Open(filepath.Join(t.TempDir(), "panvex.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = base.Close() })

	// Stall longer than the budget we'll assert on, but well under the
	// 60s WithTimeout the function uses internally — if cancellation is
	// not honoured the test still bounds at the 60s timeout via the
	// WithTimeout, but we want a much tighter local bound to fail fast.
	slow := &slowListClientsStore{Store: base, stall: 30 * time.Second}

	now := time.Now().UTC()
	server := mustNew(t, Options{
		LoginTimingFloor: -1,
		Now:              func() time.Time { return now },
		Store:            slow,
	})
	t.Cleanup(func() {
		server.Close()
	})

	// Arm the stall and cancel serverCtx: the WithTimeout child context
	// now has an already-done parent, so the very first store call
	// (ListClients) must observe ctx.Done() and return ctx.Err() without
	// sleeping the full stall budget.
	slow.armed.Store(true)
	server.serverCancel()

	const budget = 200 * time.Millisecond
	done := make(chan error, 1)
	start := time.Now()
	go func() {
		done <- server.restoreStoredClients()
	}()

	select {
	case err := <-done:
		elapsed := time.Since(start)
		if elapsed > budget {
			t.Fatalf("restoreStoredClients took %s, want <= %s — server ctx cancellation not honoured", elapsed, budget)
		}
		if err == nil || !errors.Is(err, context.Canceled) {
			t.Fatalf("restoreStoredClients err = %v, want ctx.Canceled", err)
		}
	case <-time.After(budget + 500*time.Millisecond):
		t.Fatalf("restoreStoredClients did not return within %s — server ctx cancellation not honoured", budget)
	}
}
