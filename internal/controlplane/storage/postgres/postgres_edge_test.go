package postgres

import (
	"context"
	"errors"
	"fmt"
	"os"
	"runtime"
	"sync"
	"sync/atomic"
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

// TestUpsertClientUsageBulkLargeBatch covers the chunked bulk-upsert
// against postgres. Postgres has a 65535 bind-parameter ceiling per query;
// 600 rows × 8 columns = 4800 parameters — under the ceiling for a single
// query but we stay correct by chunking at 250. This catches a regression
// where the chunk loop drops the trailing partial chunk (S27 T1).
func TestUpsertClientUsageBulkLargeBatch(t *testing.T) {
	store := openForEdge(t)
	ctx := context.Background()

	group := storage.FleetGroupRecord{
		ID: "00000000-0000-4000-8000-000000000014", Name: "bulk-large",
		CreatedAt: time.Date(2026, time.April, 18, 10, 0, 0, 0, time.UTC),
	}
	if err := store.PutFleetGroup(ctx, group); err != nil {
		t.Fatalf("PutFleetGroup: %v", err)
	}
	client := storage.ClientRecord{
		ID: "cli-pg-bulk-large", Name: "bulk", SecretCiphertext: "s",
		UserADTag: "0123456789abcdef0123456789abcdef",
		Enabled:   true,
		CreatedAt: time.Date(2026, time.April, 18, 10, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2026, time.April, 18, 10, 0, 0, 0, time.UTC),
	}
	if err := store.PutClient(ctx, client); err != nil {
		t.Fatalf("PutClient: %v", err)
	}

	const total = 600
	now := time.Date(2026, time.April, 18, 11, 0, 0, 0, time.UTC)
	batch := make([]storage.ClientUsageRecord, 0, total)
	for i := 0; i < total; i++ {
		agentID := fmt.Sprintf("ag-pg-bulk-%04d", i)
		if err := store.PutAgent(ctx, storage.AgentRecord{
			ID: agentID, NodeName: agentID, FleetGroupID: group.ID,
			LastSeenAt: now,
		}); err != nil {
			t.Fatalf("PutAgent[%d]: %v", i, err)
		}
		batch = append(batch, storage.ClientUsageRecord{
			ClientID: client.ID, AgentID: agentID,
			TrafficUsedBytes: uint64(i + 1),
			LastSeq:          uint64(i + 1),
			ObservedAt:       now,
		})
	}
	if err := store.UpsertClientUsageBulk(ctx, batch); err != nil {
		t.Fatalf("UpsertClientUsageBulk: %v", err)
	}
	got, err := store.ListClientUsage(ctx)
	if err != nil {
		t.Fatalf("ListClientUsage: %v", err)
	}
	if len(got) != total {
		t.Fatalf("ListClientUsage len = %d, want %d", len(got), total)
	}
}

// TestConcurrentUpsertClientUsageBulkSameKey exercises postgres
// `INSERT ... ON CONFLICT DO UPDATE` under concurrent contention against
// the same (client, agent) key. With read-committed isolation the
// updates must serialise without surfacing constraint violations to any
// caller; the final row reflects exactly one of the writers' values
// (S27 T1).
func TestConcurrentUpsertClientUsageBulkSameKey(t *testing.T) {
	store := openForEdge(t)
	ctx := context.Background()

	group := storage.FleetGroupRecord{
		ID: "00000000-0000-4000-8000-000000000015", Name: "bulk-conc",
		CreatedAt: time.Date(2026, time.April, 18, 10, 0, 0, 0, time.UTC),
	}
	if err := store.PutFleetGroup(ctx, group); err != nil {
		t.Fatalf("PutFleetGroup: %v", err)
	}
	agent := storage.AgentRecord{
		ID: "ag-pg-bulk-conc", NodeName: "ag-pg-bulk-conc", FleetGroupID: group.ID,
		LastSeenAt: time.Date(2026, time.April, 18, 10, 0, 0, 0, time.UTC),
	}
	if err := store.PutAgent(ctx, agent); err != nil {
		t.Fatalf("PutAgent: %v", err)
	}
	client := storage.ClientRecord{
		ID: "cli-pg-bulk-conc", Name: "conc", SecretCiphertext: "s",
		UserADTag: "0123456789abcdef0123456789abcdef",
		Enabled:   true,
		CreatedAt: time.Date(2026, time.April, 18, 10, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2026, time.April, 18, 10, 0, 0, 0, time.UTC),
	}
	if err := store.PutClient(ctx, client); err != nil {
		t.Fatalf("PutClient: %v", err)
	}

	const writers = 8
	const iters = 25
	now := time.Date(2026, time.April, 18, 11, 0, 0, 0, time.UTC)

	var wg sync.WaitGroup
	var failures atomic.Int64

	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func(writer int) {
			defer wg.Done()
			for i := 0; i < iters; i++ {
				rec := storage.ClientUsageRecord{
					ClientID: client.ID, AgentID: agent.ID,
					TrafficUsedBytes: uint64(writer*1000 + i),
					LastSeq:          uint64(writer*1000 + i),
					ObservedAt:       now.Add(time.Duration(writer*1000+i) * time.Millisecond),
				}
				if err := store.UpsertClientUsageBulk(ctx, []storage.ClientUsageRecord{rec}); err != nil {
					t.Errorf("writer %d iter %d: %v", writer, i, err)
					failures.Add(1)
					return
				}
			}
		}(w)
	}
	wg.Wait()

	if failures.Load() > 0 {
		t.Fatalf("concurrent UpsertClientUsageBulk had %d failures", failures.Load())
	}

	rows, err := store.ListClientUsage(ctx)
	if err != nil {
		t.Fatalf("ListClientUsage: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("ListClientUsage len = %d, want 1 (concurrent ON CONFLICT must collapse)", len(rows))
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
