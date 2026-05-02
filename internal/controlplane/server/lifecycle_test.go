package server

import (
	"sync"
	"testing"
	"time"
)

// newTestServer constructs a minimal Server for lifecycle assertions. It
// mirrors the construction pattern used by sibling tests (see
// authz_middleware_test.go) but stays in-memory so Close()/Context() can be
// exercised without touching SQLite.
func newTestServer(t *testing.T) *Server {
	t.Helper()
	now := time.Date(2026, time.May, 2, 10, 0, 0, 0, time.UTC)
	srv := New(Options{
		LoginTimingFloor: -1,
		Now:              func() time.Time { return now },
	})
	t.Cleanup(func() {
		// Close is idempotent — calling it after a test that already closed
		// must not panic.
		srv.Close()
	})
	return srv
}

// TestServer_CloseCancelsServerCtx pins the contract introduced by Plan 3
// Task 1: Server owns a lifecycle context that is alive between New() and
// Close(), and Close() cancels it. Long-lived workers (rollup, metrics
// poller, fleet-ensure, lockout-restore, batch-writer drain) will subscribe
// to this context in subsequent tasks so a wedged storage call cannot keep
// the process from shutting down.
func TestServer_CloseCancelsServerCtx(t *testing.T) {
	srv := newTestServer(t)
	derived := srv.Context()
	select {
	case <-derived.Done():
		t.Fatalf("serverCtx must be alive before Close")
	default:
	}
	srv.Close()
	select {
	case <-derived.Done():
	case <-time.After(time.Second):
		t.Fatalf("serverCtx not cancelled after Close")
	}
}

// TestServer_CloseIsRaceSafeUnderConcurrentInvocation pins the idempotency
// contract under racing callers. Bare nil-check + assign on serverCancel
// would let two goroutines both observe non-nil and double-cancel; sync.Once
// is the new guard. Run with `-race -count=3` to catch regressions.
func TestServer_CloseIsRaceSafeUnderConcurrentInvocation(t *testing.T) {
	srv := newTestServer(t)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			srv.Close()
		}()
	}
	wg.Wait()
	// Server.Context().Done() must still be closed exactly once.
	select {
	case <-srv.Context().Done():
	case <-time.After(time.Second):
		t.Fatalf("ctx not cancelled after concurrent Close")
	}
}
