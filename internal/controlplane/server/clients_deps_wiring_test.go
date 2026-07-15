package server

// Regression test for a wiring trap found in review of R8a Task 2.
//
// clients.Service carries a jobQueue (a *jobs.Service) wired via SetDeps.
// initStoreBackedSubsystems rebuilds s.clientsSvc and re-calls SetDeps, but
// that rebuild lives inside a trySetStartupErr closure — so it is SKIPPED
// whenever an earlier boot step (RestoreSessions, lockout Restore,
// seedUsers, restoreStoredState, ...) already recorded a startupErr. s.jobs
// is reassigned to the store-backed jobs.Service a few lines above that same
// closure UNCONDITIONALLY, though. Before the fix, that combination left
// s.clientsSvc wired to the abandoned no-repo jobs.Service from
// newServerFromOptions while s.jobs (and everything else) moved on to the
// store-backed instance — a trap that would silently misroute client-job
// enqueues once R8a Tasks 3/4 route orchestration through clientsSvc's
// jobQueue.
//
// The fix: New() now calls s.clientsSvc.SetDeps(s, s.jobs, s.logger)
// unconditionally as the very last step before returning, regardless of
// which construction/early-exit branches ran. This test forces seedUsers to
// fail (via failingStore.listUsersErr), which sets startupErr BEFORE the
// clientsSvc-rebuild closure runs, skipping that rebuild's SetDeps call —
// and then asserts the final wiring is still correct.

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/auth"
	"github.com/lost-coder/panvex/internal/controlplane/jobs"
	"github.com/lost-coder/panvex/internal/controlplane/storage/sqlite"
)

// assertClientsSvcWiredToServerJobs fails the test unless srv.clientsSvc's
// currently-wired JobQueue is the exact same *jobs.Service instance as
// srv.jobs. This is the invariant the final unconditional SetDeps call in
// New() is responsible for maintaining no matter which boot branches ran.
func assertClientsSvcWiredToServerJobs(t *testing.T, srv *Server) {
	t.Helper()
	got, ok := srv.clientsSvc.CurrentJobQueue().(*jobs.Service)
	if !ok {
		t.Fatalf("clientsSvc.CurrentJobQueue() = %T, want *jobs.Service", srv.clientsSvc.CurrentJobQueue())
	}
	if got != srv.jobs {
		t.Fatalf("clientsSvc.CurrentJobQueue() is wired to a different *jobs.Service than srv.jobs: clientsSvc=%p srv.jobs=%p", got, srv.jobs)
	}
}

// TestNew_ClientsSvcWiredToServerJobs_HealthyBoot covers the ordinary
// store-backed boot path: no startup step fails, so the clientsSvc-rebuild
// closure inside initStoreBackedSubsystems runs and calls SetDeps itself.
// The final unconditional SetDeps at the end of New() is redundant here, but
// must not break anything.
func TestNew_ClientsSvcWiredToServerJobs_HealthyBoot(t *testing.T) {
	now := time.Date(2026, time.July, 15, 12, 0, 0, 0, time.UTC)
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "panvex.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	defer store.Close()

	srv := mustNew(t, Options{
		LoginTimingFloor: -1,
		Now:              func() time.Time { return now },
		Store:            store,
	})
	defer srv.Close()

	if srv.StartupError() != nil {
		t.Fatalf("unexpected StartupError() = %v", srv.StartupError())
	}
	assertClientsSvcWiredToServerJobs(t, srv)
}

// TestNew_ClientsSvcWiredToServerJobs_EarlyStartupErr is the trap-reproducing
// case: failingStore.listUsersErr makes seedUsers fail, which records
// startupErr via trySetStartupErr BEFORE the clientsSvc-rebuild closure runs
// (seedUsers is called at lifecycle.go:316, the rebuild closure at :328) —
// so the rebuild (and its SetDeps call) is skipped and s.clientsSvc stays
// the no-repo Service built in newServerFromOptions. s.jobs, meanwhile, was
// already reassigned unconditionally to the store-backed instance a few
// lines above (lifecycle.go:233). Without the final unconditional SetDeps in
// New(), clientsSvc.CurrentJobQueue() would still point at the original
// jobs.NewService() from newServerFromOptions instead of srv.jobs.
func TestNew_ClientsSvcWiredToServerJobs_EarlyStartupErr(t *testing.T) {
	now := time.Date(2026, time.July, 15, 12, 0, 0, 0, time.UTC)
	baseStore, err := sqlite.Open(filepath.Join(t.TempDir(), "panvex.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	defer baseStore.Close()

	injectedErr := errors.New("injected: seedUsers ListUsers failure")
	store := &failingStore{MigrationStore: baseStore, listUsersErr: injectedErr}

	// Options.Users must be non-empty: seedUsers short-circuits to nil
	// (never touching the store) when there are no users to seed.
	srv := mustNew(t, Options{
		LoginTimingFloor: -1,
		Now:              func() time.Time { return now },
		Store:            store,
		Users: []auth.User{{
			ID:           "u1",
			Username:     "admin",
			PasswordHash: "hash",
			Role:         auth.RoleAdmin,
			CreatedAt:    now,
		}},
	})
	defer srv.Close()

	if srv.StartupError() == nil {
		t.Fatal("StartupError() = nil, want the injected seedUsers failure (test setup did not reproduce the trap's precondition)")
	}

	assertClientsSvcWiredToServerJobs(t, srv)
}
