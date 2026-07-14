package server

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/storage/sqlite"
)

// TestCloseDrainsAuditWrittenByBackgroundGoroutines pins the shutdown ordering
// (R11 / R-5): a background goroutine registered on bgWG — the panel
// self-update is the real one — writes an audit row as it finishes. Close used
// to drain the batch writer BEFORE joining bgWG, so that row landed in an
// already-drained buffer and was lost. The record of the update that just
// replaced the binary was the one thing shutdown could drop.
func TestCloseDrainsAuditWrittenByBackgroundGoroutines(t *testing.T) {
	now := time.Date(2026, time.July, 14, 10, 0, 0, 0, time.UTC)
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "panvex.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	defer store.Close()

	server := mustNew(t, Options{
		LoginTimingFloor: -1,
		Now:              func() time.Time { return now },
		Store:            store,
	})

	// A background goroutine that is still running when Close starts, and
	// writes its audit row on the way out — exactly the self-update shape.
	started := make(chan struct{})
	var once sync.Once
	server.bgWG.Add(1)
	go func() {
		defer server.bgWG.Done()
		once.Do(func() { close(started) })
		time.Sleep(50 * time.Millisecond)
		server.appendAuditWithContext(context.Background(), "system", "panel.update.applied", "panel", map[string]any{
			"version": "1.2.3",
		})
	}()
	<-started

	server.Close()

	events, err := store.ListAuditEvents(context.Background(), 100)
	if err != nil {
		t.Fatalf("ListAuditEvents() error = %v", err)
	}
	for _, event := range events {
		if event.Action == "panel.update.applied" {
			return
		}
	}
	t.Fatalf("panel.update.applied is missing from the store after Close; got %d audit rows", len(events))
}
