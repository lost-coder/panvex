package loadtest

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
	"github.com/lost-coder/panvex/internal/controlplane/storage/sqlite"
)

// fleetGroupID is the fixed UUID seeded in every harness so agents enrolled
// across scenarios share a fleet group. UUIDv7 picked at authoring time;
// kept stable so the bench output is reproducible across runs.
const fleetGroupID = "0190e000-0000-7000-8000-loadtestfleet"

// openHarnessStore opens a fresh SQLite store under the test's TempDir,
// seeds a single fleet group (the FK target every AgentRecord needs), and
// registers Close cleanup. Mirrors openSQLite from load_bench_test.go so
// the new scenarios reuse the proven bootstrap pattern.
func openHarnessStore(tb testing.TB) *sqlite.Store {
	tb.Helper()
	store, err := sqlite.Open(filepath.Join(tb.TempDir(), "loadtest.db"))
	if err != nil {
		tb.Fatalf("sqlite.Open: %v", err)
	}
	tb.Cleanup(func() { _ = store.Close() })

	ctx := tbContext(tb)
	if err := store.PutFleetGroup(ctx, storage.FleetGroupRecord{
		ID:        fleetGroupID,
		Name:      "loadtest",
		Label:     "Load Test",
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}); err != nil {
		tb.Fatalf("PutFleetGroup: %v", err)
	}
	return store
}

// tbContext returns the per-test context for testing.T (preferred) or a
// background context for testing.B. b.Context() exists on Go 1.24+; we
// prefer it when available so cancellation flows through.
func tbContext(tb testing.TB) context.Context {
	type ctxer interface{ Context() context.Context }
	if c, ok := tb.(ctxer); ok {
		return c.Context()
	}
	return context.Background()
}
