package main

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/agent/runtimeevents"
)

type fakeSender struct {
	mu    sync.Mutex
	calls [][]runtimeevents.Event
}

func (f *fakeSender) send(batch []runtimeevents.Event) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := make([]runtimeevents.Event, len(batch))
	copy(cp, batch)
	f.calls = append(f.calls, cp)
	return nil
}

func (f *fakeSender) callsSnapshot() [][]runtimeevents.Event {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([][]runtimeevents.Event, len(f.calls))
	copy(out, f.calls)
	return out
}

func TestPusherImmediatePushOnWarn(t *testing.T) {
	buf := runtimeevents.NewBuffer(10)
	sender := &fakeSender{}
	notify := make(chan struct{}, 1)
	p := newRuntimeEventsPusher(buf, sender.send, time.Hour, notify, nil)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { p.Run(ctx); close(done) }()

	buf.Append(runtimeevents.Event{Ts: time.Now(), Level: "warn", Message: "boom"})
	notify <- struct{}{}

	deadline := time.After(time.Second)
	for len(sender.callsSnapshot()) == 0 {
		select {
		case <-deadline:
			t.Fatalf("no push within 1s")
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}

	cancel()
	<-done
	calls := sender.callsSnapshot()
	if len(calls) < 1 || calls[0][0].Message != "boom" {
		t.Fatalf("expected warn pushed, got %+v", calls)
	}
}

func TestPusherBatchesInfoOnTick(t *testing.T) {
	buf := runtimeevents.NewBuffer(10)
	sender := &fakeSender{}
	notify := make(chan struct{}, 1)
	interval := 30 * time.Millisecond
	p := newRuntimeEventsPusher(buf, sender.send, interval, notify, nil)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { p.Run(ctx); close(done) }()

	buf.Append(runtimeevents.Event{Ts: time.Now(), Level: "info", Message: "a"})
	buf.Append(runtimeevents.Event{Ts: time.Now().Add(time.Millisecond), Level: "info", Message: "b"})

	time.Sleep(100 * time.Millisecond)
	cancel()
	<-done

	calls := sender.callsSnapshot()
	total := 0
	for _, c := range calls {
		total += len(c)
	}
	if total < 2 {
		t.Fatalf("expected at least 2 events; got %d", total)
	}
}

// TestPusherCursorSurvivesReconnect reproduces audit #9b: the ring
// buffer is process-wide (200 entries) but the pusher used to be
// re-created per connection with a zero cursor, replaying the whole
// ring after every reconnect. With a shared process-level cursor each
// buffered event is delivered exactly once across the reconnect.
func TestPusherCursorSurvivesReconnect(t *testing.T) {
	buf := runtimeevents.NewBuffer(200)
	for i := 0; i < 200; i++ {
		buf.Append(runtimeevents.Event{Ts: time.Now(), Level: "info", Message: fmt.Sprintf("ev-%d", i)})
	}

	cursor := new(atomic.Uint64)
	var mu sync.Mutex
	seen := map[uint64]int{}
	sends := 0
	send := func(batch []runtimeevents.Event) error {
		mu.Lock()
		defer mu.Unlock()
		sends++
		// Simulate the connection dying mid-buffer: the 3rd batch fails.
		if sends == 3 {
			return context.Canceled
		}
		for _, ev := range batch {
			seen[ev.Seq]++
		}
		return nil
	}

	// Connection 1: flushes two 50-event batches, dies on the third.
	p1 := newRuntimeEventsPusher(buf, send, time.Hour, nil, cursor)
	p1.flush(context.Background())

	// Connection 2 (reconnect): fresh pusher, SAME process-level cursor.
	p2 := newRuntimeEventsPusher(buf, send, time.Hour, nil, cursor)
	p2.flush(context.Background())

	mu.Lock()
	defer mu.Unlock()
	if len(seen) != 200 {
		t.Fatalf("delivered %d distinct events, want 200", len(seen))
	}
	for seq, n := range seen {
		if n != 1 {
			t.Fatalf("event seq=%d delivered %d times, want exactly once", seq, n)
		}
	}
}
