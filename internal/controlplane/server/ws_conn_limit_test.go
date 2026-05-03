package server

import (
	"sync"
	"testing"
)

func TestWSConnLimiter_AllowsUpToCap(t *testing.T) {
	t.Parallel()
	l := newWSConnLimiter()
	for i := 0; i < maxWSConnsPerUser; i++ {
		if !l.acquire("user:alice", maxWSConnsPerUser) {
			t.Fatalf("acquire #%d should succeed under cap", i+1)
		}
	}
	if got := l.snapshot("user:alice"); int(got) != maxWSConnsPerUser {
		t.Fatalf("counter = %d, want %d", got, maxWSConnsPerUser)
	}
}

func TestWSConnLimiter_RejectsOverCap(t *testing.T) {
	t.Parallel()
	l := newWSConnLimiter()
	for i := 0; i < maxWSConnsPerUser; i++ {
		if !l.acquire("user:bob", maxWSConnsPerUser) {
			t.Fatalf("acquire #%d should succeed", i+1)
		}
	}
	// 9th must be rejected.
	if l.acquire("user:bob", maxWSConnsPerUser) {
		t.Fatalf("acquire over cap should fail")
	}
	// Counter must still equal the cap — the rejected attempt rolled back.
	if got := l.snapshot("user:bob"); int(got) != maxWSConnsPerUser {
		t.Fatalf("counter = %d, want %d (no leak on rejection)", got, maxWSConnsPerUser)
	}
}

func TestWSConnLimiter_ReleaseFreesSlot(t *testing.T) {
	t.Parallel()
	l := newWSConnLimiter()
	for i := 0; i < maxWSConnsPerUser; i++ {
		if !l.acquire("user:carol", maxWSConnsPerUser) {
			t.Fatalf("acquire #%d should succeed", i+1)
		}
	}
	if l.acquire("user:carol", maxWSConnsPerUser) {
		t.Fatalf("acquire over cap should fail")
	}
	// Release one slot — a new acquire must now succeed.
	l.release("user:carol")
	if !l.acquire("user:carol", maxWSConnsPerUser) {
		t.Fatalf("acquire after release should succeed")
	}
}

func TestWSConnLimiter_KeysAreIndependent(t *testing.T) {
	t.Parallel()
	l := newWSConnLimiter()
	for i := 0; i < maxWSConnsPerUser; i++ {
		if !l.acquire("user:alice", maxWSConnsPerUser) {
			t.Fatalf("alice acquire #%d should succeed", i+1)
		}
	}
	// Separate user is unaffected.
	if !l.acquire("user:bob", maxWSConnsPerUser) {
		t.Fatalf("bob's first acquire must succeed independently of alice")
	}
}

func TestWSConnLimiter_MapEntryDroppedWhenCounterZero(t *testing.T) {
	t.Parallel()
	l := newWSConnLimiter()
	if !l.acquire("user:dan", maxWSConnsPerUser) {
		t.Fatalf("acquire should succeed")
	}
	l.release("user:dan")
	l.mu.Lock()
	_, present := l.counts["user:dan"]
	l.mu.Unlock()
	if present {
		t.Fatalf("counter map should drop zero-count entries to avoid unbounded growth")
	}
}

func TestWSConnLimiter_ConcurrentAcquireRespectsCap(t *testing.T) {
	t.Parallel()
	l := newWSConnLimiter()
	const cap = 8
	const goroutines = 64

	var wg sync.WaitGroup
	var success int32
	var mu sync.Mutex
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			if l.acquire("user:race", cap) {
				mu.Lock()
				success++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if success != cap {
		t.Fatalf("under contention, exactly %d acquires should succeed; got %d", cap, success)
	}
	if got := l.snapshot("user:race"); int(got) != cap {
		t.Fatalf("counter under contention = %d, want %d", got, cap)
	}
}

func TestWSConnLimiter_NilSafeAndDegenerateInputs(t *testing.T) {
	t.Parallel()
	var l *wsConnLimiter
	if !l.acquire("user:x", 4) {
		t.Fatalf("nil limiter must always allow")
	}
	l.release("user:x") // must not panic

	l2 := newWSConnLimiter()
	if !l2.acquire("", 4) {
		t.Fatalf("empty key should be a no-op pass-through")
	}
	if !l2.acquire("user:x", 0) {
		t.Fatalf("non-positive limit should be a no-op pass-through")
	}
}
