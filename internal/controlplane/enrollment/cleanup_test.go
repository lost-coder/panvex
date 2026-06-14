package enrollment_test

import (
	"context"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/enrollment"
	"github.com/lost-coder/panvex/internal/controlplane/enrollment/enrollmenttest"
)

// TestRecorderDeleteOlderThanRemovesAttempts seeds one back-dated
// attempt and one fresh attempt (via Recorder.Begin so the clock
// stamps "now") and confirms DeleteOlderThan keeps the fresh one and
// drops the old one. Exercises the MemStore path; the SQLStore path is
// covered transitively by the lifecycle / retention worker once it
// fires, and intentionally not asserted here because the test would
// have to migrate a fresh SQLite file and the simpler in-memory path
// pins the recorder semantics on its own.
func TestRecorderDeleteOlderThanRemovesAttempts(t *testing.T) {
	store := enrollmenttest.NewMemStore()
	rec := enrollment.NewRecorder(store, time.Now)
	ctx := context.Background()

	// Old attempt: seed directly so we can back-date StartedAt past
	// the cutoff. Going through Recorder.Begin would stamp "now".
	store.InsertAttemptForTest(enrollment.Attempt{
		ID:        "old-id-1",
		Mode:      enrollment.ModeInbound,
		Status:    enrollment.StatusSuccess,
		RequestID: "req-old",
		StartedAt: time.Now().Add(-31 * 24 * time.Hour),
	})

	// Fresh attempt via the normal Begin path so it lands at "now".
	fresh, err := rec.Begin(ctx, enrollment.ModeInbound, "", "1.2.3.4")
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}

	n, err := rec.DeleteOlderThan(ctx, time.Now().Add(-30*24*time.Hour))
	if err != nil {
		t.Fatalf("DeleteOlderThan: %v", err)
	}
	if n != 1 {
		t.Fatalf("deleted = %d, want 1", n)
	}

	atts := store.SnapshotAttempts()
	if len(atts) != 1 {
		t.Fatalf("remaining attempts = %d, want 1", len(atts))
	}
	if atts[0].ID != fresh {
		t.Fatalf("wrong attempt remains: %q (want %q)", atts[0].ID, fresh)
	}
}
