package server

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/panvex/panvex/internal/controlplane/storage"
	"github.com/panvex/panvex/internal/controlplane/storage/sqlite"
)

func TestBatchBufferDrainFlushesAccumulatedItems(t *testing.T) {
	var flushed []int
	buf := newBatchBuffer(10, func(_ context.Context, items []int) {
		flushed = append(flushed, items...)
	})

	buf.Enqueue(1)
	buf.Enqueue(2)
	buf.Enqueue(3)
	buf.Drain(context.Background())

	if len(flushed) != 3 {
		t.Fatalf("flushed %d items, want 3", len(flushed))
	}
	for i, want := range []int{1, 2, 3} {
		if flushed[i] != want {
			t.Fatalf("flushed[%d] = %d, want %d", i, flushed[i], want)
		}
	}
}

func TestBatchBufferDrainIsNoOpWhenEmpty(t *testing.T) {
	called := false
	buf := newBatchBuffer(10, func(_ context.Context, _ []int) {
		called = true
	})

	buf.Drain(context.Background())

	if called {
		t.Fatal("flush function was called on empty buffer")
	}
}

func TestBatchBufferSignalFiringOnFull(t *testing.T) {
	buf := newBatchBuffer(3, func(_ context.Context, _ []int) {})

	buf.Enqueue(1)
	buf.Enqueue(2)
	buf.Enqueue(3)

	select {
	case <-buf.signal:
		// expected
	default:
		t.Fatal("signal channel should have a message after buffer reaches maxSize")
	}
}

func TestBatchBufferDrainResetsBuffer(t *testing.T) {
	var mu sync.Mutex
	var batches [][]int

	buf := newBatchBuffer(10, func(_ context.Context, items []int) {
		mu.Lock()
		defer mu.Unlock()
		cp := make([]int, len(items))
		copy(cp, items)
		batches = append(batches, cp)
	})

	buf.Enqueue(1)
	buf.Enqueue(2)
	buf.Drain(context.Background())

	buf.Enqueue(3)
	buf.Drain(context.Background())

	mu.Lock()
	defer mu.Unlock()

	if len(batches) != 2 {
		t.Fatalf("got %d batches, want 2", len(batches))
	}
	if len(batches[0]) != 2 {
		t.Fatalf("first batch has %d items, want 2", len(batches[0]))
	}
	if len(batches[1]) != 1 {
		t.Fatalf("second batch has %d items, want 1", len(batches[1]))
	}
	if batches[1][0] != 3 {
		t.Fatalf("second batch[0] = %d, want 3", batches[1][0])
	}
}

func TestStoreBatchWriterStartAndStop(t *testing.T) {
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	defer store.Close()

	w := newStoreBatchWriter(store)
	w.Start()
	w.Stop()
}

func TestStoreBatchWriterDrainsOnStop(t *testing.T) {
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	defer store.Close()

	w := newStoreBatchWriter(store)
	w.Start()

	now := time.Now().UTC()
	w.audit.Enqueue(storage.AuditEventRecord{
		ID:        "evt-1",
		ActorID:   "user-1",
		Action:    "test.action",
		TargetID:  "target-1",
		CreatedAt: now,
	})
	w.audit.Enqueue(storage.AuditEventRecord{
		ID:        "evt-2",
		ActorID:   "user-1",
		Action:    "test.action",
		TargetID:  "target-2",
		CreatedAt: now,
	})

	// Stop triggers a final drain of all buffers.
	w.Stop()

	events, err := store.ListAuditEvents(context.Background())
	if err != nil {
		t.Fatalf("ListAuditEvents() error = %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("ListAuditEvents() returned %d events, want 2", len(events))
	}
}
