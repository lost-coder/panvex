package server

import (
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
