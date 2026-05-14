package runtimeevents_test

import (
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/runtimeevents"
)

func TestBufferAppendAndSnapshotNewestFirst(t *testing.T) {
	buf := runtimeevents.New(5)
	buf.Append("agent-1", runtimeevents.Event{Ts: time.Unix(1, 0), Level: "info", Message: "first"})
	buf.Append("agent-1", runtimeevents.Event{Ts: time.Unix(2, 0), Level: "warn", Message: "second"})

	got := buf.Snapshot("agent-1", nil, 0)
	if len(got) != 2 {
		t.Fatalf("got %d, want 2", len(got))
	}
	if got[0].Message != "second" || got[1].Message != "first" {
		t.Fatalf("not newest-first: %+v", got)
	}
}

func TestBufferDropsOldestAtCapacity(t *testing.T) {
	buf := runtimeevents.New(2)
	for i := 1; i <= 5; i++ {
		buf.Append("a", runtimeevents.Event{Ts: time.Unix(int64(i), 0), Message: string(rune('a' + i - 1))})
	}
	got := buf.Snapshot("a", nil, 0)
	if len(got) != 2 {
		t.Fatalf("got %d, want 2", len(got))
	}
	if got[0].Message != "e" || got[1].Message != "d" {
		t.Fatalf("expected [e d], got %+v", got)
	}
}

func TestBufferIsolatesAgents(t *testing.T) {
	buf := runtimeevents.New(3)
	buf.Append("a", runtimeevents.Event{Ts: time.Unix(1, 0), Message: "x"})
	buf.Append("b", runtimeevents.Event{Ts: time.Unix(2, 0), Message: "y"})
	if got := buf.Snapshot("a", nil, 0); len(got) != 1 || got[0].Message != "x" {
		t.Fatalf("a leaked: %+v", got)
	}
	if got := buf.Snapshot("b", nil, 0); len(got) != 1 || got[0].Message != "y" {
		t.Fatalf("b leaked: %+v", got)
	}
}

func TestBufferLevelFilter(t *testing.T) {
	buf := runtimeevents.New(5)
	buf.Append("a", runtimeevents.Event{Ts: time.Unix(1, 0), Level: "info"})
	buf.Append("a", runtimeevents.Event{Ts: time.Unix(2, 0), Level: "warn"})
	buf.Append("a", runtimeevents.Event{Ts: time.Unix(3, 0), Level: "error"})

	got := buf.Snapshot("a", []string{"warn", "error"}, 0)
	if len(got) != 2 {
		t.Fatalf("got %d, want 2", len(got))
	}
	if got[0].Level != "error" || got[1].Level != "warn" {
		t.Fatalf("filter wrong: %+v", got)
	}
}

func TestBufferLimitClamps(t *testing.T) {
	buf := runtimeevents.New(10)
	for i := 1; i <= 5; i++ {
		buf.Append("a", runtimeevents.Event{Ts: time.Unix(int64(i), 0)})
	}
	if got := buf.Snapshot("a", nil, 2); len(got) != 2 {
		t.Fatalf("got %d, want 2", len(got))
	}
}

func TestBufferAppendBatch(t *testing.T) {
	buf := runtimeevents.New(5)
	evs := []runtimeevents.Event{
		{Ts: time.Unix(1, 0), Message: "a"},
		{Ts: time.Unix(2, 0), Message: "b"},
	}
	buf.AppendBatch("agent-x", evs)
	if got := buf.Snapshot("agent-x", nil, 0); len(got) != 2 {
		t.Fatalf("got %d, want 2", len(got))
	}
}
