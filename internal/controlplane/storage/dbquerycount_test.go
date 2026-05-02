package storage

import (
	"context"
	"sync"
	"testing"
)

func TestDBQueryCounterStartsAtZero(t *testing.T) {
	t.Parallel()
	ctx := WithDBQueryCounter(context.Background())
	if got := DBQueryCount(ctx); got != 0 {
		t.Fatalf("DBQueryCount on fresh ctx = %d, want 0", got)
	}
}

func TestDBQueryCounterIncrements(t *testing.T) {
	t.Parallel()
	ctx := WithDBQueryCounter(context.Background())
	IncrementDBQuery(ctx)
	IncrementDBQuery(ctx)
	IncrementDBQuery(ctx)
	if got := DBQueryCount(ctx); got != 3 {
		t.Fatalf("DBQueryCount = %d, want 3", got)
	}
}

func TestIncrementWithoutCounterIsNoOp(t *testing.T) {
	t.Parallel()
	// Bare ctx with no counter installed (the "running outside an HTTP
	// request" case — startup, batch writer, gRPC streams) — increments
	// must not panic.
	IncrementDBQuery(context.Background())
	IncrementDBQuery(nil) //nolint:staticcheck // intentional nil ctx
	if got := DBQueryCount(context.Background()); got != 0 {
		t.Fatalf("DBQueryCount on bare ctx = %d, want 0", got)
	}
	if got := DBQueryCount(nil); got != 0 { //nolint:staticcheck // intentional nil ctx
		t.Fatalf("DBQueryCount on nil ctx = %d, want 0", got)
	}
}

// TestCounterIsRequestScoped asserts that counters on different ctx values
// don't share state — critical for HTTP middleware that creates a fresh
// counter per request.
func TestCounterIsRequestScoped(t *testing.T) {
	t.Parallel()
	a := WithDBQueryCounter(context.Background())
	b := WithDBQueryCounter(context.Background())
	IncrementDBQuery(a)
	IncrementDBQuery(a)
	IncrementDBQuery(b)
	if got := DBQueryCount(a); got != 2 {
		t.Fatalf("DBQueryCount(a) = %d, want 2", got)
	}
	if got := DBQueryCount(b); got != 1 {
		t.Fatalf("DBQueryCount(b) = %d, want 1", got)
	}
}

// TestCounterIsThreadSafe asserts increments from concurrent goroutines on
// the same ctx accumulate without lost updates.
func TestCounterIsThreadSafe(t *testing.T) {
	t.Parallel()
	ctx := WithDBQueryCounter(context.Background())
	const goroutines = 32
	const incsPerGoroutine = 100
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < incsPerGoroutine; j++ {
				IncrementDBQuery(ctx)
			}
		}()
	}
	wg.Wait()
	want := int64(goroutines * incsPerGoroutine)
	if got := DBQueryCount(ctx); got != want {
		t.Fatalf("DBQueryCount = %d, want %d (lost updates)", got, want)
	}
}
