package server

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
	"github.com/lost-coder/panvex/internal/controlplane/storage/sqlite"
)

// newTestServer constructs a minimal Server for lifecycle assertions. It
// mirrors the construction pattern used by sibling tests (see
// authz_middleware_test.go) but stays in-memory so Close()/Context() can be
// exercised without touching SQLite.
func newTestServer(t *testing.T) *Server {
	t.Helper()
	now := time.Date(2026, time.May, 2, 10, 0, 0, 0, time.UTC)
	srv, err := New(Options{
		LoginTimingFloor: -1,
		Now:              func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
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

// rollupCtxObserverStore wraps a Store so the rollup worker's first
// RollupServerLoadHourly call publishes its ctx to the test and blocks
// until that ctx is cancelled. Lets the regression test observe whether
// the ctx the worker handed to storage is rooted in serverCtx (good) or
// context.Background() (bad — Close cannot abort it via serverCancel).
type rollupCtxObserverStore struct {
	storage.Store
	once    sync.Once
	started chan context.Context
	calls   atomic.Int32
}

func (s *rollupCtxObserverStore) RollupServerLoadHourly(ctx context.Context, bucketHour time.Time) error {
	s.calls.Add(1)
	// Publish the ctx exactly once so the test can wait for the worker to
	// be in-flight, then sleep until ctx fires. If ctx is Background-rooted
	// (pre-migration), serverCancel never propagates here and the wait
	// blocks indefinitely — that is the bug the migration fixes.
	s.once.Do(func() {
		select {
		case s.started <- ctx:
		default:
		}
	})
	<-ctx.Done()
	return ctx.Err()
}

// TestServer_RollupHonoursServerCtxCancellation pins Task 2 of Plan 3:
// the rollup worker's context must be derived from serverCtx, so a direct
// serverCancel() (or any future SIGTERM-handler that cancels serverCtx
// without going through Close's full ordered shutdown) propagates to
// in-flight storage calls.
//
// Pre-migration the rollup ctx is rooted in context.Background(); only the
// stopRollup() call in Close cancels it, and that runs *after* the batch
// writer drain (up to 10s) — so a wedged batch writer leaves the rollup
// goroutine stuck on slow storage for the whole drain window.
//
// Post-migration the rollup ctx is rooted in serverCtx, so serverCancel
// alone aborts in-flight storage immediately.
func TestServer_RollupHonoursServerCtxCancellation(t *testing.T) {
	base, err := sqlite.Open(filepath.Join(t.TempDir(), "rollup-ctx.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = base.Close() })

	observer := &rollupCtxObserverStore{
		Store:   base,
		started: make(chan context.Context, 1),
	}

	now := time.Date(2026, time.May, 2, 10, 0, 0, 0, time.UTC)
	srv, err := New(Options{
		Store:            observer,
		LoginTimingFloor: -1,
		Now:              func() time.Time { return now },
		// Tiny rollup interval so the first tick fires within the test
		// budget. Other intervals stay at defaults — they do not factor
		// into this assertion.
		Intervals: Intervals{Rollup: 25 * time.Millisecond},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	t.Cleanup(srv.Close)

	// Wait for the rollup worker to enter its first storage call.
	var rollupCtx context.Context
	select {
	case rollupCtx = <-observer.started:
	case <-time.After(2 * time.Second):
		t.Fatalf("rollup worker never invoked RollupServerLoadHourly")
	}

	// Cancel only serverCtx — do not call Close. We are asserting that
	// serverCtx-cancellation alone (i.e. step 1 of Close) is enough to
	// abort the in-flight storage call. Pre-migration this is a no-op
	// because rollupCtx is Background-rooted.
	srv.serverCancel()

	select {
	case <-rollupCtx.Done():
		// Migration successful: rollup ctx is derived from serverCtx.
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("rollup ctx did not honour serverCancel; rollup parent is not serverCtx")
	}
}

// failingCPSecretStore returns errFailingCPSecretStore from every CPSecret
// call so the boot path that loads the vault HKDF salt fails. Plan 3 Task 4
// (Q-7): library-level panics violate Go style — embedders/tests cannot
// recover. Used to pin that New surfaces the failure as an error instead.
type failingCPSecretStore struct {
	storage.Store
}

var errFailingCPSecretStore = errors.New("disk failure")

func (failingCPSecretStore) GetCPSecret(_ context.Context, _ string) ([]byte, error) {
	return nil, errFailingCPSecretStore
}

func (failingCPSecretStore) PutCPSecret(_ context.Context, _ string, _ []byte) error {
	return errFailingCPSecretStore
}

// TestNew_ReturnsErrorOnVaultSaltFailure pins Plan 3 Task 4 (Q-7): when
// loadOrCreateVaultSalt fails (storage error on the salt row), New must
// return the error rather than panic. Pre-fix the constructor panicked,
// which makes embedders unable to recover and forces tests to wrap calls
// in defer/recover. Post-fix the error is bubbled through to the caller.
func TestNew_ReturnsErrorOnVaultSaltFailure(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("New must return error, not panic, on store failure: %v", r)
		}
	}()
	store := failingCPSecretStore{}
	now := time.Date(2026, time.May, 2, 10, 0, 0, 0, time.UTC)
	srv, err := New(Options{
		Store:            store,
		EncryptionKey:    "any-passphrase-for-vault",
		LoginTimingFloor: -1,
		Now:              func() time.Time { return now },
	})
	if err == nil {
		if srv != nil {
			srv.Close()
		}
		t.Fatalf("New must return error on store failure, got nil")
	}
	if srv != nil {
		t.Fatalf("New must return nil server on error, got %v", srv)
	}
	if !errors.Is(err, errFailingCPSecretStore) && !strings.Contains(err.Error(), "vault HKDF salt") {
		t.Fatalf("err = %v, want wrapping errFailingCPSecretStore or vault salt context", err)
	}
}
