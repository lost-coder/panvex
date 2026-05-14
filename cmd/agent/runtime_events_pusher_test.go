package main

import (
	"context"
	"sync"
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
	p := newRuntimeEventsPusher(buf, sender.send, time.Hour, notify)

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
	p := newRuntimeEventsPusher(buf, sender.send, interval, notify)

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
