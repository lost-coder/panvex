package postgres

import (
	"context"
	"errors"
	"fmt"
	"os"
	"runtime"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

// openForEdge opens the test postgres instance and resets all rows.
// Tests skip cleanly when PANVEX_POSTGRES_TEST_DSN is not set, matching
// the convention in cascade_test.go / store_test.go (S27 T1).
func openForEdge(t *testing.T) *Store {
	t.Helper()

	dsn := os.Getenv("PANVEX_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("PANVEX_POSTGRES_TEST_DSN is not set")
	}

	store, err := Open(dsn)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	if err := resetForTest(t.Context(), store); err != nil {
		t.Fatalf("resetForTest() error = %v", err)
	}
	return store
}

// TestTransactRollsBackOnContextCancel verifies that pgx aborts the
// in-flight tx and runs the deferred Rollback when the caller's ctx is
// cancelled. The follow-up Transact must succeed promptly, proving the
// underlying connection was returned to the pool — not leaked (S27 T1).
func TestTransactRollsBackOnContextCancel(t *testing.T) {
	store := openForEdge(t)

	ctx := context.Background()
	seed := storage.FleetGroupRecord{
		ID:        "00000000-0000-4000-8000-000000000011",
		Name:      "seed",
		CreatedAt: time.Date(2026, time.April, 18, 10, 0, 0, 0, time.UTC),
	}
	if err := store.PutFleetGroup(ctx, seed); err != nil {
		t.Fatalf("PutFleetGroup(seed): %v", err)
	}

	txCtx, txCancel := context.WithCancel(ctx)
	defer txCancel()
	cancelled := errors.New("cancelled")
	err := store.Transact(txCtx, func(tx storage.Store) error {
		if err := tx.PutFleetGroup(txCtx, storage.FleetGroupRecord{
			ID:        "00000000-0000-4000-8000-000000000012",
			Name:      "rollback-me",
			CreatedAt: time.Date(2026, time.April, 18, 10, 5, 0, 0, time.UTC),
		}); err != nil {
			return err
		}
		txCancel()
		return cancelled
	})
	if err == nil {
		t.Fatal("Transact returned nil, want non-nil after ctx-cancel")
	}

	if _, err := store.GetFleetGroup(ctx, "00000000-0000-4000-8000-000000000012"); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("GetFleetGroup(rollback-me) error = %v, want ErrNotFound", err)
	}

	probeCtx, probeCancel := context.WithTimeout(ctx, 5*time.Second)
	defer probeCancel()
	if err := store.Transact(probeCtx, func(tx storage.Store) error {
		return tx.PutFleetGroup(probeCtx, storage.FleetGroupRecord{
			ID:        "00000000-0000-4000-8000-000000000013",
			Name:      "probe",
			CreatedAt: time.Date(2026, time.April, 18, 10, 10, 0, 0, time.UTC),
		})
	}); err != nil {
		t.Fatalf("follow-up Transact failed (conn pool leak suspected): %v", err)
	}
}

// TestPoolExhaustionGracefulError exercises pool exhaustion: shrink
// MaxOpenConns to 1, hold the only connection in a long-lived tx, then
// fire a second concurrent op with a tight ctx. The second op MUST fail
// with the ctx-deadline error rather than block forever or panic. This
// catches a regression where pgx's wait-for-conn loop ignores ctx (S27 T1).
//
// Goroutine-leak guard: snapshot runtime.NumGoroutine before/after; the
// blocked acquirer must unwind cleanly on ctx-cancel.
func TestPoolExhaustionGracefulError(t *testing.T) {
	store := openForEdge(t)

	// Tighten the pool to a single connection so we can deterministically
	// starve the second caller.
	store.sqlDB.SetMaxOpenConns(1)
	store.sqlDB.SetMaxIdleConns(1)

	ctx := context.Background()
	hogStart := make(chan struct{})
	hogRelease := make(chan struct{})
	hogDone := make(chan error, 1)

	go func() {
		hogDone <- store.Transact(ctx, func(tx storage.Store) error {
			close(hogStart)
			<-hogRelease
			return nil
		})
	}()
	<-hogStart

	// Second op: deadline-bounded ctx. Should fail with ctx-deadline and
	// NOT block past the deadline by more than a small margin.
	deadlineCtx, cancel := context.WithTimeout(ctx, 250*time.Millisecond)
	defer cancel()

	gor0 := runtime.NumGoroutine()
	start := time.Now()
	pingErr := store.Ping(deadlineCtx)
	elapsed := time.Since(start)

	if pingErr == nil {
		t.Fatalf("Ping under exhausted pool returned nil, want ctx-deadline error")
	}
	if elapsed > 2*time.Second {
		t.Fatalf("Ping blocked %v under exhausted pool, want ≲ ctx deadline", elapsed)
	}

	close(hogRelease)
	if err := <-hogDone; err != nil {
		t.Fatalf("hog Transact: %v", err)
	}

	// Goroutine-leak guard: give the runtime a moment to GC the blocked
	// acquirer, then assert we are within a small slack of the baseline.
	time.Sleep(100 * time.Millisecond)
	if g := runtime.NumGoroutine(); g > gor0+4 {
		t.Fatalf("goroutine leak suspected: NumGoroutine before=%d after=%d", gor0, g)
	}
}

// TestListJobsCursorBoundaryLimits exercises the cursor pagination's
// limit clamp on the postgres backend. Mirrors the sqlite test (S27 T1).
func TestListJobsCursorBoundaryLimits(t *testing.T) {
	store := openForEdge(t)
	ctx := context.Background()

	const jobs = storage.MaxCursorPageSize + 5
	base := time.Date(2026, time.April, 18, 12, 0, 0, 0, time.UTC)
	for i := 0; i < jobs; i++ {
		if err := store.PutJob(ctx, storage.JobRecord{
			ID:             fmt.Sprintf("job-pg-%04d", i),
			Action:         "rollout",
			ActorID:        "tester",
			Status:         "queued",
			CreatedAt:      base.Add(time.Duration(i) * time.Second),
			TTL:            time.Hour,
			IdempotencyKey: fmt.Sprintf("idem-pg-%04d", i),
		}); err != nil {
			t.Fatalf("PutJob[%d]: %v", i, err)
		}
	}

	cases := []struct {
		name      string
		limit     int
		wantCount int
	}{
		{"limit=1", 1, 1},
		{"limit=zero-uses-default", 0, storage.DefaultCursorPageSize},
		{"limit=max", storage.MaxCursorPageSize, storage.MaxCursorPageSize},
		{"limit=max+1-clamps", storage.MaxCursorPageSize + 1, storage.MaxCursorPageSize},
		{"limit=huge-clamps", storage.MaxCursorPageSize * 100, storage.MaxCursorPageSize},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rows, _, err := store.ListJobsCursor(ctx, storage.ListJobsCursorParams{Limit: tc.limit})
			if err != nil {
				t.Fatalf("ListJobsCursor: %v", err)
			}
			if len(rows) != tc.wantCount {
				t.Fatalf("len(rows) = %d, want %d (limit=%d)", len(rows), tc.wantCount, tc.limit)
			}
		})
	}
}
