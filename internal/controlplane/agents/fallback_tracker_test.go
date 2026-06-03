package agents

import (
	"sync"
	"testing"
	"time"
)

func TestFallbackTracker_SetGetClear(t *testing.T) {
	tr := NewFallbackTracker()

	if _, ok := tr.Get("a1"); ok {
		t.Fatal("expected miss for unknown agent")
	}

	at := time.Date(2026, 6, 3, 12, 0, 0, 0, time.UTC)
	tr.Set("a1", at)

	got, ok := tr.Get("a1")
	if !ok {
		t.Fatal("expected hit after Set")
	}
	if !got.Equal(at) {
		t.Fatalf("got %v, want %v", got, at)
	}

	// Set is idempotent-overwrite.
	at2 := at.Add(time.Hour)
	tr.Set("a1", at2)
	got, _ = tr.Get("a1")
	if !got.Equal(at2) {
		t.Fatalf("after overwrite got %v, want %v", got, at2)
	}

	tr.Clear("a1")
	if _, ok := tr.Get("a1"); ok {
		t.Fatal("expected miss after Clear")
	}

	// Clear is idempotent on an absent key.
	tr.Clear("a1")
	tr.Clear("never-set")
}

func TestFallbackTracker_Restore(t *testing.T) {
	tr := NewFallbackTracker()
	tr.Set("stale", time.Unix(1, 0).UTC())

	at1 := time.Date(2026, 6, 3, 1, 0, 0, 0, time.UTC)
	at2 := time.Date(2026, 6, 3, 2, 0, 0, 0, time.UTC)
	src := map[string]time.Time{"a1": at1, "a2": at2}
	tr.Restore(src)

	// Restore is a full snapshot: prior "stale" entry is gone.
	if _, ok := tr.Get("stale"); ok {
		t.Fatal("expected Restore to replace, not merge")
	}
	if got, ok := tr.Get("a1"); !ok || !got.Equal(at1) {
		t.Fatalf("a1: got %v ok=%v, want %v", got, ok, at1)
	}
	if got, ok := tr.Get("a2"); !ok || !got.Equal(at2) {
		t.Fatalf("a2: got %v ok=%v, want %v", got, ok, at2)
	}

	// Mutating the source map after Restore must not affect the tracker.
	src["a1"] = time.Unix(99, 0).UTC()
	if got, _ := tr.Get("a1"); !got.Equal(at1) {
		t.Fatalf("tracker aliased the source map: got %v, want %v", got, at1)
	}
}

func TestFallbackTracker_Concurrent(t *testing.T) {
	tr := NewFallbackTracker()
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			id := "agent"
			tr.Set(id, time.Unix(int64(n), 0).UTC())
			tr.Get(id)
			tr.Restore(map[string]time.Time{id: time.Unix(int64(n), 0).UTC()})
			tr.Clear("other")
		}(i)
	}
	wg.Wait()
}
