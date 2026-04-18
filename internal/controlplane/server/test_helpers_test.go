package server

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/storage/sqlite"
)

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
	server := New(Options{
		Now:   func() time.Time { return now },
		Store: store,
	})
	t.Cleanup(func() {
		server.Close()
		store.Close()
	})
	return server
}
