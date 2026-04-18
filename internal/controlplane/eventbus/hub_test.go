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
			// Drain until channel closes (cancel()).
			for range ch {
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
