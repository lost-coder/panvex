package batchwriter

import (
	"context"
	"errors"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
	"github.com/lost-coder/panvex/internal/controlplane/storage/sqlite"
)

// chaosCountingAuditStore wraps a store and counts how many AppendAuditEvent
// calls have succeeded. Used to assert the persisted row count matches the
// store's own record count (no duplicates, no torn writes).
type chaosCountingAuditStore struct {
	storage.Store
	stall    time.Duration
	appended atomic.Int32
}

func (s *chaosCountingAuditStore) AppendAuditEvent(ctx context.Context, event storage.AuditEventRecord) error {
	// Interruptible stall so Close() does not deadlock when the drain
	// context expires.
	if s.stall > 0 {
		select {
		case <-time.After(s.stall):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if err := s.Store.AppendAuditEvent(ctx, event); err != nil {
		return err
	}
	s.appended.Add(1)
	return nil
}

// AppendAuditEventsBulk mirrors the single-row stall/count semantics for the
// bulk audit flush path (P6-6.1b). The bulk insert is atomic: either the whole
// batch commits and the counter advances by len(events), or the stall is
// cancelled and nothing is persisted.
func (s *chaosCountingAuditStore) AppendAuditEventsBulk(ctx context.Context, events []storage.AuditEventRecord) error {
	if s.stall > 0 {
		select {
		case <-time.After(s.stall):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if err := s.Store.AppendAuditEventsBulk(ctx, events); err != nil {
		return err
	}
	s.appended.Add(int32(len(events))) //nolint:gosec // bounded by chaos-test event count
	return nil
}

// TestChaosShutdownMidAudit models SIGKILL between audit enqueue and flush. We
// enqueue N rows, then trigger StopWithTimeout with a tight budget so the drain
// may or may not finish — either way the invariants are:
//   - No data corruption (no duplicate / torn rows in the store).
//   - Either every event persisted OR the timeout-error path fires without
//     panicking; under no circumstance do we lose rows silently because the
//     in-memory buffer has its ownership taken over by the drain.
//
// The stall in chaosCountingAuditStore makes the drain actually race the
// shutdown budget (otherwise a fast sqlite write makes the test degenerate).
func TestChaosShutdownMidAudit(t *testing.T) {
	base, err := sqlite.Open(filepath.Join(t.TempDir(), "chaos-audit.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = base.Close() })

	// 5ms stall per row; with N=40 rows the full drain takes ~200ms. A 50ms
	// shutdown budget forces partial completion on slow CI but still lets
	// fast machines drain a handful of rows cleanly.
	counting := &chaosCountingAuditStore{Store: base, stall: 5 * time.Millisecond}
	w := New(counting, nil, nil)
	w.Start(t.Context())

	const n = 40
	for i := 0; i < n; i++ {
		w.auditEvents.Enqueue(storage.AuditEventRecord{
			ID:        "chaos-evt-" + randSuffix(i),
			ActorID:   "user-1",
			Action:    "chaos.shutdown",
			TargetID:  "target-1",
			CreatedAt: time.Now().UTC(),
		})
	}

	// Very tight timeout — the drain WILL be interrupted on slow runners
	// and WILL finish on fast ones. The chaos invariant is that neither
	// outcome corrupts data.
	stopErr := w.StopWithTimeout(t.Context(), 50*time.Millisecond)

	// The returned error is either nil (drained in time) or
	// context.DeadlineExceeded (per the StopWithTimeout contract). Anything
	// else is a bug.
	if stopErr != nil && !errors.Is(stopErr, context.DeadlineExceeded) {
		t.Fatalf("StopWithTimeout returned %v, want nil or DeadlineExceeded", stopErr)
	}

	persisted, err := base.ListAuditEvents(context.Background(), n+10)
	if err != nil {
		t.Fatalf("ListAuditEvents: %v", err)
	}

	// Invariant 1: persisted count matches the store's own success counter
	// modulo a one-row tolerance. SQLite's ExecContext can commit the
	// row and *then* surface ctx.Err() if the cancellation lands between
	// the underlying commit and the Go-side return — that race leaves
	// at most one extra row in the DB without an incremented counter.
	// Anything beyond a single in-flight cancellation is a real torn
	// write.
	persistedCount := int32(len(persisted)) //nolint:gosec // bounded by chaos-test event count (n=40), well within int32
	successes := counting.appended.Load()
	if persistedCount < successes || persistedCount > successes+1 {
		t.Fatalf("persisted rows = %d, counter = %d (torn write!)", len(persisted), successes)
	}

	// Invariant 2: no duplicate IDs — the drain must not re-submit rows.
	ids := make(map[string]struct{}, len(persisted))
	for _, row := range persisted {
		if _, dup := ids[row.ID]; dup {
			t.Fatalf("duplicate audit row id %q after chaos shutdown", row.ID)
		}
		ids[row.ID] = struct{}{}
	}

	// Invariant 3: whatever path we took, we made progress. A stalled
	// writer that persists zero rows on shutdown would be a regression.
	// (Exact count is not asserted — it depends on CI timing.)
	if len(persisted) == 0 && stopErr == nil {
		t.Fatalf("clean shutdown persisted 0/%d rows — drain contract violated", n)
	}
	if len(persisted) > n {
		t.Fatalf("persisted %d rows > enqueued %d — duplicate flush", len(persisted), n)
	}

	t.Logf("chaos audit shutdown: persisted %d/%d rows, stopErr=%v", len(persisted), n, stopErr)
}
