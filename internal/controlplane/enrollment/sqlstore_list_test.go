package enrollment_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/lost-coder/panvex/internal/controlplane/enrollment"
	"github.com/lost-coder/panvex/internal/controlplane/storage/sqlite"
	"github.com/lost-coder/panvex/internal/dbsqlc"
)

// TestSQLStoreListAttemptsFindsRowsBeyondTheOldPrefetchWindow is the R8-D fix:
// ListAttempts used to fetch the 1000 newest rows and filter them in Go, so an
// attempt older than that window was invisible no matter what you filtered on —
// the endpoint just returned empty, with no hint that it had stopped looking.
// The filter runs in SQL now.
func TestSQLStoreListAttemptsFindsRowsBeyondTheOldPrefetchWindow(t *testing.T) {
	store, db := migratedEnrollmentDB(t)
	defer store.Close()

	ctx := context.Background()
	sqlStore := enrollment.NewSQLStore(dbsqlc.New(db), db)

	base := time.Date(2026, time.July, 14, 12, 0, 0, 0, time.UTC)
	needleID := uuid.NewString()

	// The needle is the OLDEST attempt, then 1200 newer ones bury it — well past
	// the 1000-row prefix the old implementation looked at.
	if err := sqlStore.CreateAttempt(ctx, enrollment.Attempt{
		ID:        needleID,
		Mode:      enrollment.ModeOutbound,
		RequestID: "needle",
		Status:    enrollment.StatusFailed,
		StartedAt: base,
	}); err != nil {
		t.Fatalf("CreateAttempt(needle) error = %v", err)
	}
	for i := 1; i <= 1200; i++ {
		if err := sqlStore.CreateAttempt(ctx, enrollment.Attempt{
			ID:        uuid.NewString(),
			Mode:      enrollment.ModeInbound,
			RequestID: "noise",
			Status:    enrollment.StatusInProgress,
			StartedAt: base.Add(time.Duration(i) * time.Minute),
		}); err != nil {
			t.Fatalf("CreateAttempt(noise %d) error = %v", i, err)
		}
	}

	mode := enrollment.ModeOutbound
	got, err := sqlStore.ListAttempts(ctx, enrollment.ListFilter{Mode: &mode, Limit: 10})
	if err != nil {
		t.Fatalf("ListAttempts() error = %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len(ListAttempts(mode=outbound)) = %d, want 1 (the needle, 1200 rows deep)", len(got))
	}
	if got[0].ID != needleID {
		t.Fatalf("ListAttempts() returned %q, want the needle %q", got[0].ID, needleID)
	}
}

// TestSQLStoreListAttemptsPaginates walks the keyset cursor and checks the pages
// are contiguous and strictly newest-first.
func TestSQLStoreListAttemptsPaginates(t *testing.T) {
	store, db := migratedEnrollmentDB(t)
	defer store.Close()

	ctx := context.Background()
	sqlStore := enrollment.NewSQLStore(dbsqlc.New(db), db)

	base := time.Date(2026, time.July, 14, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 5; i++ {
		if err := sqlStore.CreateAttempt(ctx, enrollment.Attempt{
			ID:        uuid.NewString(),
			Mode:      enrollment.ModeInbound,
			RequestID: "page",
			Status:    enrollment.StatusInProgress,
			StartedAt: base.Add(time.Duration(i) * time.Minute),
		}); err != nil {
			t.Fatalf("CreateAttempt(%d) error = %v", i, err)
		}
	}

	first, err := sqlStore.ListAttempts(ctx, enrollment.ListFilter{Limit: 2})
	if err != nil {
		t.Fatalf("ListAttempts(page 1) error = %v", err)
	}
	if len(first) != 2 {
		t.Fatalf("len(page 1) = %d, want 2", len(first))
	}
	if !first[0].StartedAt.After(first[1].StartedAt) {
		t.Fatal("page 1 is not newest-first")
	}

	cursorTs := first[1].StartedAt
	cursorID := first[1].ID
	second, err := sqlStore.ListAttempts(ctx, enrollment.ListFilter{
		Limit:    2,
		CursorTs: &cursorTs,
		CursorID: &cursorID,
	})
	if err != nil {
		t.Fatalf("ListAttempts(page 2) error = %v", err)
	}
	if len(second) != 2 {
		t.Fatalf("len(page 2) = %d, want 2", len(second))
	}
	if !second[0].StartedAt.Before(cursorTs) {
		t.Fatalf("page 2 starts at %v, which is not older than the cursor %v", second[0].StartedAt, cursorTs)
	}
	for _, item := range second {
		if item.ID == first[0].ID || item.ID == first[1].ID {
			t.Fatalf("page 2 repeats a row from page 1 (%s)", item.ID)
		}
	}
}

func migratedEnrollmentDB(t *testing.T) (*sqlite.Store, *sql.DB) {
	t.Helper()
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "enrollment.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	return store, store.DB()
}
