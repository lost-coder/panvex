package server

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/storage/sqlite"
)

// mustNew constructs a Server via New and fails the test if New
// returns an error. Plan 3 Task 4 (Q-7) made New return an error
// instead of panicking on boot-time secret-init failures, which means
// every test call site grew an error-check. mustNew keeps those sites
// to a single line by funnelling the check through one place. Tests
// that intend to exercise the error path (e.g. failingCPSecretStore)
// still call New directly.
func mustNew(t *testing.T, options Options) *Server {
	t.Helper()
	srv, err := New(options)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return srv
}

// testServerWithSQLite constructs a Server wired to a fresh SQLite
// store scoped to the test's TempDir, and registers Close cleanup.
// After S8 removed the in-memory enrollment-service fallback,
// issueEnrollmentToken requires a real store; tests that needed to
// mint a token but did not previously care about storage now use this
// helper to stay succinct.
func testServerWithSQLite(t *testing.T, now time.Time) *Server {
	t.Helper()
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "panvex.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	server := mustNew(t, Options{
		LoginTimingFloor: -1,
		Now:              func() time.Time { return now },
		Store:            store,
	})
	t.Cleanup(func() {
		server.Close()
		store.Close()
	})
	return server
}
