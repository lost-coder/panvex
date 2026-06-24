package sqlite

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

// TestTransactRollsBackOnContextCancel verifies that cancelling the caller
// context mid-transaction rolls back, releases the BEGIN IMMEDIATE writer
// lock, and leaves no zombie state (S27 T1).
//
// SQLite is single-writer: if the deferred ROLLBACK in Transact never ran
// (or ran against the cancelled ctx and silently swallowed the failure),
// the next BEGIN IMMEDIATE would block on the held writer lock until
// busy_timeout (5 s) elapses. We assert that a follow-up Transact succeeds
// promptly — proving the writer lock was released.
func TestTransactRollsBackOnContextCancel(t *testing.T) {
	store := openTestStore(t)

	rootCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Seed a fleet-group row outside the to-be-cancelled tx so we can
	// later prove no follow-up writes leaked through.
	seed := storage.FleetGroupRecord{
		ID:        "fg-tx-cancel-seed",
		Name:      "seed",
		CreatedAt: time.Date(2026, time.April, 18, 10, 0, 0, 0, time.UTC),
	}
	if err := store.PutFleetGroup(rootCtx, seed); err != nil {
		t.Fatalf("PutFleetGroup(seed): %v", err)
	}

	txCtx, txCancel := context.WithCancel(rootCtx)
	defer txCancel()
	cancelled := errors.New("cancelled mid-tx")
	err := store.Transact(txCtx, func(tx storage.Store) error {
		// First write inside the tx — uses BEGIN IMMEDIATE so the writer
		// lock is already held by the time we get here.
		if err := tx.PutFleetGroup(txCtx, storage.FleetGroupRecord{
			ID:        "fg-tx-cancel-rollback",
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

	// The "rollback-me" row must NOT be visible — proving the deferred
	// ROLLBACK ran even though ctx was cancelled.
	if _, err := store.GetFleetGroup(rootCtx, "fg-tx-cancel-rollback"); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("GetFleetGroup(rollback-me) error = %v, want ErrNotFound (tx must have rolled back)", err)
	}

	// The next Transact must succeed promptly. If the writer lock leaked,
	// BEGIN IMMEDIATE would either fail with SQLITE_BUSY (since the test
	// ctx is short) or block until busy_timeout. We bound the deadline at
	// 2 s, well below the 5 s busy_timeout pragma, so any leak is a fail.
	probeCtx, probeCancel := context.WithTimeout(rootCtx, 2*time.Second)
	defer probeCancel()
	if err := store.Transact(probeCtx, func(tx storage.Store) error {
		return tx.PutFleetGroup(probeCtx, storage.FleetGroupRecord{
			ID:        "fg-tx-cancel-probe",
			Name:      "probe",
			CreatedAt: time.Date(2026, time.April, 18, 10, 10, 0, 0, time.UTC),
		})
	}); err != nil {
		t.Fatalf("follow-up Transact failed (writer lock leak suspected): %v", err)
	}

	// Sanity: the seed and probe rows survive; the rollback row does not.
	if _, err := store.GetFleetGroup(rootCtx, seed.ID); err != nil {
		t.Fatalf("GetFleetGroup(seed): %v", err)
	}
	if _, err := store.GetFleetGroup(rootCtx, "fg-tx-cancel-probe"); err != nil {
		t.Fatalf("GetFleetGroup(probe): %v", err)
	}
}

// TestUpsertClientUsageBulkLargeBatch exercises the chunking loop with a
// payload large enough to require multiple chunks (bulkChunkSize == 250
// → 600 rows ⇒ 3 chunks). Catches off-by-one in chunkBounds + missing
// rows on the trailing chunk (S27 T1).
func TestUpsertClientUsageBulkLargeBatch(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	group := storage.FleetGroupRecord{
		ID: "fg-bulk-large", Name: "bulk-large",
		CreatedAt: time.Date(2026, time.April, 18, 10, 0, 0, 0, time.UTC),
	}
	if err := store.PutFleetGroup(ctx, group); err != nil {
		t.Fatalf("PutFleetGroup: %v", err)
	}
	client := storage.ClientRecord{
		ID: "cli-bulk-large", Name: "bulk", SecretCiphertext: "s",
		UserADTag: "0123456789abcdef0123456789abcdef",
		Enabled:   true,
		CreatedAt: time.Date(2026, time.April, 18, 10, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2026, time.April, 18, 10, 0, 0, 0, time.UTC),
	}
	if err := store.PutClient(ctx, client); err != nil {
		t.Fatalf("PutClient: %v", err)
	}

	const total = 600 // ⇒ 3 chunks (250 + 250 + 100)
	now := time.Date(2026, time.April, 18, 11, 0, 0, 0, time.UTC)
	batch := make([]storage.ClientUsageRecord, 0, total)
	for i := 0; i < total; i++ {
		agentID := fmt.Sprintf("ag-bulk-%04d", i)
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
		t.Fatalf("UpsertClientUsageBulk(600 rows): %v", err)
	}

	got, err := store.ListClientUsage(ctx)
	if err != nil {
		t.Fatalf("ListClientUsage: %v", err)
	}
	if len(got) != total {
		t.Fatalf("ListClientUsage len = %d, want %d (chunked bulk lost rows)", len(got), total)
	}
}

// TestConcurrentUpsertClientUsageBulkSameKey races N goroutines all
// upserting the SAME (client, agent) key with different TrafficUsedBytes.
// SQLite is single-writer so each batch serialises through BEGIN IMMEDIATE;
// ON CONFLICT DO UPDATE must give last-write-wins semantics with no
// constraint violation surfacing to any caller (S27 T1).
func TestConcurrentUpsertClientUsageBulkSameKey(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	group := storage.FleetGroupRecord{
		ID: "fg-bulk-conc", Name: "bulk-conc",
		CreatedAt: time.Date(2026, time.April, 18, 10, 0, 0, 0, time.UTC),
	}
	if err := store.PutFleetGroup(ctx, group); err != nil {
		t.Fatalf("PutFleetGroup: %v", err)
	}
	agent := storage.AgentRecord{
		ID: "ag-bulk-conc", NodeName: "ag-bulk-conc", FleetGroupID: group.ID,
		LastSeenAt: time.Date(2026, time.April, 18, 10, 0, 0, 0, time.UTC),
	}
	if err := store.PutAgent(ctx, agent); err != nil {
		t.Fatalf("PutAgent: %v", err)
	}
	client := storage.ClientRecord{
		ID: "cli-bulk-conc", Name: "conc", SecretCiphertext: "s",
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
	errs := make(chan error, writers*iters)

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
					errs <- fmt.Errorf("writer %d iter %d: %w", writer, i, err)
					return
				}
			}
		}(w)
	}
	wg.Wait()
	close(errs)

	for e := range errs {
		t.Errorf("concurrent UpsertClientUsageBulk: %v", e)
	}

	// Exactly one row survives — last-write-wins under ON CONFLICT.
	rows, err := store.ListClientUsage(ctx)
	if err != nil {
		t.Fatalf("ListClientUsage: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("ListClientUsage len = %d, want 1 (concurrent upsert must collapse on key)", len(rows))
	}
}

// TestListJobsCursorBoundaryLimits exercises the cursor pagination's
// limit-clamping code path against the real backend. limit=1 (smallest
// non-zero), limit=MaxCursorPageSize (boundary), limit=MaxCursorPageSize+1
// (must clamp). Catches a regression where the SQL backend forgets to
// honour storage.NormalizeCursorLimit (S27 T1).
func TestListJobsCursorBoundaryLimits(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	// Seed enough jobs to exceed MaxCursorPageSize so a clamp is observable.
	const jobs = storage.MaxCursorPageSize + 5
	base := time.Date(2026, time.April, 18, 12, 0, 0, 0, time.UTC)
	for i := 0; i < jobs; i++ {
		if err := store.PutJob(ctx, storage.JobRecord{
			ID:             fmt.Sprintf("job-%04d", i),
			Action:         "rollout",
			ActorID:        "tester",
			Status:         "queued",
			CreatedAt:      base.Add(time.Duration(i) * time.Second),
			TTL:            time.Hour,
			IdempotencyKey: fmt.Sprintf("idem-%04d", i),
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
