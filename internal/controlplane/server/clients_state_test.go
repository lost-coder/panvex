package server

import (
	"context"
	"errors"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/clients"
	"github.com/lost-coder/panvex/internal/controlplane/storage/sqlite"
)

// slowListClientsRepo wraps a clients.Repository and stalls inside List when
// the armed flag is set so the test can prove restoreStoredClients honours its
// parent (s.serverCtx) ctx cancellation rather than waiting the full 60s
// WithTimeout budget.
type slowListClientsRepo struct {
	clients.Repository
	stall time.Duration
	armed atomic.Bool
}

func (r *slowListClientsRepo) List(ctx context.Context) ([]clients.Client, error) {
	if r.armed.Load() {
		select {
		case <-time.After(r.stall):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return r.Repository.List(ctx)
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

	now := time.Now().UTC()
	// Use a real sqlite.Store so initStoreBackedSubsystems wires NewServiceV2.
	server := mustNew(t, Options{
		LoginTimingFloor: -1,
		Now:              func() time.Time { return now },
		Store:            base,
	})
	t.Cleanup(func() { server.Close() })

	// Stall longer than the budget we'll assert on, but well under the
	// 60s WithTimeout the function uses internally — if cancellation is
	// not honoured the test still bounds at the 60s timeout via the
	// WithTimeout, but we want a much tighter local bound to fail fast.
	slowRepo := &slowListClientsRepo{
		Repository: sqlite.NewClientsRepository(base.DB()),
		stall:      30 * time.Second,
	}

	// Swap the repo-backed Service for one using our slow repo so the next
	// Restore call stalls inside repo.List. This exercises the new code path
	// (s.clientsSvc.Restore via repo) with the same BP-01 invariant.
	server.clientsSvc = clients.NewServiceV2(clients.ServiceConfig{
		Repo: slowRepo,
		Now:  func() time.Time { return now },
	})

	// Arm the stall and cancel serverCtx: the WithTimeout child context
	// now has an already-done parent, so the very first repo call
	// (repo.List) must observe ctx.Done() and return ctx.Err() without
	// sleeping the full stall budget.
	slowRepo.armed.Store(true)
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
