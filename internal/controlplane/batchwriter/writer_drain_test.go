package batchwriter

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

// TestDrainWaitsForConcurrentFlush pins the durability contract that Flush
// depends on: once Drain returns, every item that was in the buffer when it
// was called has reached the store — including items a CONCURRENT drain is
// still writing.
//
// The old implementation swapped the batch out under the lock and then
// released the lock before calling flushFn, so a second Drain arriving mid
// write saw an empty buffer and returned immediately, while the first drain's
// rows were still in flight. Flush() is a test-only seam (production drains
// via StopWithTimeout, which stops both loops before draining), and that gap
// is what made TestServerServesRecentMetricSnapshotsFromStore flake: the
// background flushLoop had taken the batch, the test's Flush returned early,
// and the store read came back empty.
func TestDrainWaitsForConcurrentFlush(t *testing.T) {
	flushStarted := make(chan struct{})
	releaseFlush := make(chan struct{})
	var flushCompleted atomic.Bool

	buf := newBatchBuffer("test", 10, func(ctx context.Context, items []int) {
		close(flushStarted)
		<-releaseFlush
		flushCompleted.Store(true)
	})

	buf.Enqueue(1)

	// Stand in for the background flushLoop: takes the batch, then blocks
	// inside the store write.
	go buf.Drain(context.Background())
	<-flushStarted

	// Stand in for Flush(): the buffer now looks empty, but the data is not
	// durable yet.
	secondDrainReturned := make(chan struct{})
	secondDrainEntered := make(chan struct{})
	go func() {
		close(secondDrainEntered)
		buf.Drain(context.Background())
		close(secondDrainReturned)
	}()
	<-secondDrainEntered

	// The risk here is a false PASS, not a false failure: with the bug the
	// second drain returns in microseconds, so no amount of CI load makes
	// this window too short to catch it. What load could do is delay the
	// goroutine so far that the window expires before it even calls Drain —
	// hence the explicit entered-signal above, and a second of headroom.
	select {
	case <-secondDrainReturned:
		t.Fatal("Drain returned while a concurrent flush was still writing; " +
			"Flush() cannot guarantee the buffered items reached the store")
	case <-time.After(time.Second):
		// Expected: the second drain blocks until the first one finishes.
	}

	close(releaseFlush)
	<-secondDrainReturned

	if !flushCompleted.Load() {
		t.Fatal("flush did not complete before the second Drain returned")
	}
}
