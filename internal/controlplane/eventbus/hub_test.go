package eventbus

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestHubBroadcastsPublishedEventToSubscribers(t *testing.T) {
	hub := NewHub()
	subscription, cancel := hub.Subscribe()
	defer cancel()

	event := Event{
		Type: "jobs.created",
		Data: map[string]any{
			"id": "job-1",
		},
	}
	hub.Publish(event)

	select {
	case received := <-subscription:
		if received.Type != event.Type {
			t.Fatalf("received.Type = %q, want %q", received.Type, event.Type)
		}
	case <-time.After(time.Second):
		t.Fatal("Publish() did not reach subscriber")
	}
}

// TestHubNonBlockingWithSlowSubscriber verifies P2-PERF-01: Publish() must
// not hold the hub mutex while writing to a slow subscriber's channel, so
// one slow consumer cannot stall the publisher or block other subscribers.
// We attach 100 subscribers — 1 slow (never drained) and 99 fast — and
// publish 1000 events. The total wall time must be well under what a
// lock-while-send implementation would take (which would block on the slow
// channel's 64-slot buffer immediately).
func TestHubNonBlockingWithSlowSubscriber(t *testing.T) {
	hub := NewHub()

	// Count drops so we can confirm the slow subscriber is, in fact, being
	// dropped on the non-blocking select path rather than secretly being
	// delivered to via some other route.
	var drops atomic.Int64
	hub.SetDropHook(func() { drops.Add(1) })

	// 1 slow subscriber we never drain.
	slowCh, slowCancel := hub.Subscribe()
	defer slowCancel()
	_ = slowCh

	// 99 fast subscribers that drain eagerly.
	const fastCount = 99
	var wg sync.WaitGroup
	fastCancels := make([]func(), 0, fastCount)
	for i := 0; i < fastCount; i++ {
		ch, cancel := hub.Subscribe()
		fastCancels = append(fastCancels, cancel)
		wg.Add(1)
		go func() {
			defer wg.Done()
			// Drain until channel closes (cancel()). The body is intentionally
			// empty — discarding events is exactly what a fast consumer does.
			for range ch {
				_ = struct{}{}
			}
		}()
	}

	const publishCount = 1000
	start := time.Now()
	for i := 0; i < publishCount; i++ {
		hub.Publish(Event{Type: "test.event", Data: i})
	}
	elapsed := time.Since(start)

	// With the non-blocking broadcast we expect this to finish almost
	// instantly. Give it a generous 2-second budget to stay stable on CI;
	// the old lock-while-send implementation would hang indefinitely on the
	// slow subscriber's full buffer (64 slots, never drained) after ~64
	// publishes.
	if elapsed > 2*time.Second {
		t.Fatalf("Publish() stalled: 1000 publishes with 1 slow subscriber took %s, want < 2s", elapsed)
	}

	// Close fast subscribers so drain goroutines can exit before the test
	// returns (avoids leaking goroutines between tests).
	for _, cancel := range fastCancels {
		cancel()
	}
	wg.Wait()

	// The slow subscriber's 64-slot buffer fills within 64 publishes; the
	// remaining 1000-64 publishes must each be dropped for that subscriber.
	// 99 fast subscribers never drop. So total drops >= 900.
	if got := drops.Load(); got < 900 {
		t.Fatalf("drops = %d, want >= 900 (slow subscriber buffer should overflow)", got)
	}
}

// TestHubPublishZeroAllocHotPath verifies P-5: Publish must not allocate per
// call. The copy-on-write snapshot pattern lets Publish read the subscriber
// list via a single atomic load — no per-publish slice copy, no map iter.
// Per-subscriber RWMutex.RLock guards the send against a concurrent close
// but is uncontended (and therefore zero-alloc) in the steady state. We
// attach 10 fast subscribers and run Publish via testing.AllocsPerRun,
// which already discards the first iteration so warm-up allocations don't
// taint the measurement.
func TestHubPublishZeroAllocHotPath(t *testing.T) {
	hub := NewHub()

	// 10 fast subscribers; drained by background goroutines so the channels
	// never block and Publish always takes the success branch of its select.
	const subCount = 10
	var wg sync.WaitGroup
	cancels := make([]func(), 0, subCount)
	for i := 0; i < subCount; i++ {
		ch, cancel := hub.Subscribe()
		cancels = append(cancels, cancel)
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range ch {
				_ = struct{}{}
			}
		}()
	}

	evt := Event{Type: "test.event", Data: 42}
	allocs := testing.AllocsPerRun(1000, func() {
		hub.Publish(evt)
	})

	for _, cancel := range cancels {
		cancel()
	}
	wg.Wait()

	// Each Publish should perform zero heap allocations: no slice copy, no
	// closure capture, no map iteration. Allow tiny slack for runtime jitter
	// (e.g. slog.Debug which is gated by level and shouldn't fire here).
	if allocs > 0 {
		t.Fatalf("Publish allocates per call: AllocsPerRun = %.2f, want 0", allocs)
	}
}

// TestHubSubscribeCancelRace stresses Subscribe + cancel + Publish under
// concurrent load to catch lock-free snapshot bugs (use-after-free, double
// close, lost subscriber). Run with `go test -race` in CI.
func TestHubSubscribeCancelRace(t *testing.T) {
	hub := NewHub()

	stop := make(chan struct{})
	var pubWG sync.WaitGroup

	// Two publishers hammer Publish concurrently with Subscribe/cancel churn.
	for i := 0; i < 2; i++ {
		pubWG.Add(1)
		go func() {
			defer pubWG.Done()
			evt := Event{Type: "race.event", Data: nil}
			for {
				select {
				case <-stop:
					return
				default:
					hub.Publish(evt)
				}
			}
		}()
	}

	// Worker that subscribes, drains briefly, then cancels — repeatedly.
	const workers = 8
	const iterations = 200
	var workerWG sync.WaitGroup
	for w := 0; w < workers; w++ {
		workerWG.Add(1)
		go func() {
			defer workerWG.Done()
			for i := 0; i < iterations; i++ {
				ch, cancel := hub.Subscribe()
				// Drain whatever lands until cancel closes the channel.
				done := make(chan struct{})
				go func() {
					for range ch {
						_ = struct{}{}
					}
					close(done)
				}()
				cancel()
				<-done
			}
		}()
	}

	workerWG.Wait()
	close(stop)
	pubWG.Wait()

	if got := hub.SubscriberCount(); got != 0 {
		t.Fatalf("subscriber count after race = %d, want 0", got)
	}
}

func TestHubSubscriberCountTracksActiveSubscribers(t *testing.T) {
	hub := NewHub()
	if got := hub.SubscriberCount(); got != 0 {
		t.Fatalf("initial SubscriberCount = %d, want 0", got)
	}

	_, cancel1 := hub.Subscribe()
	_, cancel2 := hub.Subscribe()
	if got := hub.SubscriberCount(); got != 2 {
		t.Fatalf("after 2 Subscribe: SubscriberCount = %d, want 2", got)
	}

	cancel1()
	if got := hub.SubscriberCount(); got != 1 {
		t.Fatalf("after 1 cancel: SubscriberCount = %d, want 1", got)
	}

	cancel2()
	if got := hub.SubscriberCount(); got != 0 {
		t.Fatalf("after all cancel: SubscriberCount = %d, want 0", got)
	}
}
