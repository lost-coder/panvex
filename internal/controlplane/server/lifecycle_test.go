package server

import (
	"context"
	"errors"
	"net"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/auth"
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

// TestNew_FailsInProductionWithPublicBindAndEmptyTrustedProxyCIDRs is the
// Task 2.1 prod hard-fail regression test. The default panel runtime binds
// HTTP to ":8080" (a public, non-loopback address). Booting with
// PANVEX_ENV=production and no TrustedProxyCIDRs must fail outright rather
// than merely warn: unfixed, every request (login lockout, rate limiter, IP
// whitelist) resolves to the reverse proxy's own address and the fleet
// shares one lockout/rate-limit bucket.
func TestNew_FailsInProductionWithPublicBindAndEmptyTrustedProxyCIDRs(t *testing.T) {
	t.Setenv("PANVEX_ENV", "production")
	now := time.Date(2026, time.May, 2, 10, 0, 0, 0, time.UTC)
	srv, err := New(Options{
		LoginTimingFloor: -1,
		Now:              func() time.Time { return now },
	})
	if srv != nil {
		srv.Close()
	}
	if err == nil {
		t.Fatalf("New must return error in production with public bind + empty TrustedProxyCIDRs, got nil")
	}
	if !errors.Is(err, ErrTrustedProxyMisconfiguredProd) {
		t.Fatalf("err = %v, want ErrTrustedProxyMisconfiguredProd", err)
	}
}

// TestNew_ProductionSucceedsWithTrustedProxyCIDRsConfigured is the positive
// counterpart: the same public bind in production must boot cleanly once
// the operator configures TrustedProxyCIDRs.
func TestNew_ProductionSucceedsWithTrustedProxyCIDRsConfigured(t *testing.T) {
	t.Setenv("PANVEX_ENV", "production")
	now := time.Date(2026, time.May, 2, 10, 0, 0, 0, time.UTC)
	srv, err := New(Options{
		LoginTimingFloor:  -1,
		Now:               func() time.Time { return now },
		TrustedProxyCIDRs: []*net.IPNet{mustCIDR(t, "10.0.0.0/8")},
	})
	if err != nil {
		t.Fatalf("New() error = %v, want nil once TrustedProxyCIDRs is configured", err)
	}
	if srv == nil {
		t.Fatalf("New() returned nil server with nil error")
	}
	srv.Close()
}

// TestNew_ProductionSucceedsWithDirectExposureAllowed is the review-fix
// regression test for the escape hatch: a legitimate direct-exposure
// deployment (no reverse proxy at all, panel bound directly to a public
// address) must still boot in production when the operator declares that
// topology via PANVEX_ALLOW_DIRECT_EXPOSURE=1, even though
// TrustedProxyCIDRs is empty. Unlike the reverse-proxy-forgotten-CIDRs
// misconfiguration this guards against, an empty CIDR list here is
// architecturally correct — resolveTrustedClientIP falls back to
// RemoteAddr and no collapse bug occurs.
func TestNew_ProductionSucceedsWithDirectExposureAllowed(t *testing.T) {
	t.Setenv("PANVEX_ENV", "production")
	t.Setenv(EnvAllowDirectExposure, "1")
	now := time.Date(2026, time.May, 2, 10, 0, 0, 0, time.UTC)
	srv, err := New(Options{
		LoginTimingFloor: -1,
		Now:              func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("New() error = %v, want nil with PANVEX_ALLOW_DIRECT_EXPOSURE=1", err)
	}
	if srv == nil {
		t.Fatalf("New() returned nil server with nil error")
	}
	srv.Close()
}

// TestNew_DevWarnsButSucceedsWithPublicBindAndEmptyTrustedProxyCIDRs pins
// the dev-mode side of the split: outside of PANVEX_ENV=production the same
// misconfiguration must NOT block startup (warnIfTrustedProxyMisconfigured
// covers dev via a WARN log line instead).
func TestNew_DevWarnsButSucceedsWithPublicBindAndEmptyTrustedProxyCIDRs(t *testing.T) {
	now := time.Date(2026, time.May, 2, 10, 0, 0, 0, time.UTC)
	srv, err := New(Options{
		LoginTimingFloor: -1,
		Now:              func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("New() error = %v, want nil outside production", err)
	}
	if srv == nil {
		t.Fatalf("New() returned nil server with nil error")
	}
	srv.Close()
}

// TestNew_ServePathWiresPersistentConsumedTotpStore pins audit S3: on the
// production serve path (a real store passed to New), the auth service's
// consumed-TOTP replay-prevention store MUST be backed by the persistent
// store, never the silent in-memory fallback. The in-memory map is wiped
// on restart, which reopens the TOTP replay window for any code consumed
// within the ~90s acceptance window before the restart.
//
// initStoreBackedSubsystems fail-fasts (records a startup error) when the
// store does not satisfy storage.ConsumedTotpStore. A real SQLite store
// does satisfy it, so New must succeed with no startup error attributable
// to the consumed-TOTP wiring. This test would fail if a future refactor
// reintroduced a branch that left the store nil / in-memory.
func TestNew_ServePathWiresPersistentConsumedTotpStore(t *testing.T) {
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "panvex.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	now := time.Date(2026, time.May, 2, 10, 0, 0, 0, time.UTC)
	srv, err := New(Options{
		Store:            store,
		LoginTimingFloor: -1,
		Now:              func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	t.Cleanup(func() {
		srv.Close()
		store.Close()
	})
	if startupErr := srv.StartupError(); startupErr != nil &&
		strings.Contains(startupErr.Error(), "consumed-TOTP") {
		t.Fatalf("serve path left consumed-TOTP store unwired: %v", startupErr)
	}
}

// TestNew_ConsumedTotpSurvivesRestart pins audit S3 end-to-end at the
// serve path: a TOTP code consumed before a restart must still be seen as
// consumed after the server is re-created over the same persistent store.
// Pre-fix, the consumed-TOTP map could silently live only in memory, so a
// restart reopened the replay window. The test drives the real auth flow
// (enable TOTP, consume a code via login) against a shared SQLite file,
// rebuilds the server, and asserts the same code is rejected as a replay.
func TestNew_ConsumedTotpSurvivesRestart(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "panvex.db")
	now := time.Date(2026, time.May, 2, 10, 0, 0, 0, time.UTC)

	first, err := sqlite.Open(dbPath)
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	srv1 := mustNew(t, Options{
		Store:            first,
		LoginTimingFloor: -1,
		Now:              func() time.Time { return now },
	})

	user, _, err := srv1.auth.BootstrapUser(context.Background(), auth.BootstrapInput{
		Username: "operator",
		Password: "Correct1horse2battery",
		Role:     auth.RoleOperator,
	}, now)
	if err != nil {
		t.Fatalf("BootstrapUser: %v", err)
	}
	secret, err := srv1.auth.StartTotpSetup(context.Background(), user.ID, now.Add(10*time.Second))
	if err != nil {
		t.Fatalf("StartTotpSetup: %v", err)
	}
	enableCode, err := srv1.auth.GenerateTotpCode(secret, now.Add(30*time.Second))
	if err != nil {
		t.Fatalf("GenerateTotpCode(enable): %v", err)
	}
	if _, err := srv1.auth.EnableTotp(context.Background(), user.ID, "Correct1horse2battery", enableCode, now.Add(30*time.Second)); err != nil {
		t.Fatalf("EnableTotp: %v", err)
	}

	// Consume a login code at a fresh window so it differs from the enable code.
	loginAt := now.Add(2 * time.Minute)
	loginCode, err := srv1.auth.GenerateTotpCode(secret, loginAt)
	if err != nil {
		t.Fatalf("GenerateTotpCode(login): %v", err)
	}
	if _, err := srv1.auth.Authenticate(context.Background(), auth.LoginInput{
		Username: "operator",
		Password: "Correct1horse2battery",
		TotpCode: loginCode,
	}, loginAt); err != nil {
		t.Fatalf("Authenticate(first): %v", err)
	}

	// verifyTotpAndConsumeLocked mirrors the consumed code to the store in a
	// detached goroutine, so the write can still be in flight when Authenticate
	// returns. Synchronise on it landing before tearing down — otherwise the
	// restart below may restore an empty consumed-TOTP map and the replay would
	// (wrongly) succeed. This is a test-only race; the async best-effort persist
	// is the intended production behaviour.
	waitForConsumedTotp(t, first)

	srv1.Close()
	first.Close()

	// Restart over the SAME database file. The persistent consumed-TOTP
	// store must rebuild the in-memory map so the just-used code is still
	// rejected as a replay.
	second, err := sqlite.Open(dbPath)
	if err != nil {
		t.Fatalf("sqlite.Open(restart) error = %v", err)
	}
	srv2 := mustNew(t, Options{
		Store:            second,
		LoginTimingFloor: -1,
		Now:              func() time.Time { return loginAt },
	})
	t.Cleanup(func() {
		srv2.Close()
		second.Close()
	})

	_, err = srv2.auth.Authenticate(context.Background(), auth.LoginInput{
		Username: "operator",
		Password: "Correct1horse2battery",
		TotpCode: loginCode,
	}, loginAt.Add(time.Second))
	if !errors.Is(err, auth.ErrInvalidTotpCode) {
		t.Fatalf("Authenticate(after restart) error = %v, want ErrInvalidTotpCode (consumed-TOTP replay must survive restart)", err)
	}
}

// waitForConsumedTotp blocks until at least one consumed-TOTP record is
// visible in the persistent store, or fails the test after a short timeout.
// The auth service persists consumed codes asynchronously, so tests that
// restart over the same store must synchronise on the write completing.
func waitForConsumedTotp(t *testing.T, store storage.ConsumedTotpStore) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		records, err := store.ListConsumedTotp(context.Background())
		if err != nil {
			t.Fatalf("ListConsumedTotp: %v", err)
		}
		if len(records) > 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for consumed TOTP code to persist")
		}
		time.Sleep(5 * time.Millisecond)
	}
}
