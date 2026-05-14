package runtimeevents_test

import (
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/agent/runtimeevents"
)

func TestBufferAppendUnderCapacity(t *testing.T) {
	buf := runtimeevents.NewBuffer(3)
	buf.Append(runtimeevents.Event{Ts: time.Unix(1, 0), Message: "a"})
	buf.Append(runtimeevents.Event{Ts: time.Unix(2, 0), Message: "b"})
	if buf.Len() != 2 {
		t.Fatalf("Len = %d, want 2", buf.Len())
	}
}

func TestBufferDropsOldestAtOverflow(t *testing.T) {
	buf := runtimeevents.NewBuffer(2)
	buf.Append(runtimeevents.Event{Ts: time.Unix(1, 0), Message: "a"})
	buf.Append(runtimeevents.Event{Ts: time.Unix(2, 0), Message: "b"})
	buf.Append(runtimeevents.Event{Ts: time.Unix(3, 0), Message: "c"})
	if buf.Len() != 2 {
		t.Fatalf("Len = %d, want 2", buf.Len())
	}
	got := buf.DrainSince(time.Unix(0, 0))
	if len(got) != 2 || got[0].Message != "b" || got[1].Message != "c" {
		t.Fatalf("got = %+v, want [b c]", got)
	}
}

func TestBufferDrainSinceReturnsChronologicalSliceAfterCursor(t *testing.T) {
	buf := runtimeevents.NewBuffer(10)
	buf.Append(runtimeevents.Event{Ts: time.Unix(1, 0), Message: "a"})
	buf.Append(runtimeevents.Event{Ts: time.Unix(2, 0), Message: "b"})
	buf.Append(runtimeevents.Event{Ts: time.Unix(3, 0), Message: "c"})

	got := buf.DrainSince(time.Unix(1, 0))
	if len(got) != 2 || got[0].Message != "b" || got[1].Message != "c" {
		t.Fatalf("got = %+v, want [b c]", got)
	}
}

func TestBufferEmptyDrain(t *testing.T) {
	buf := runtimeevents.NewBuffer(3)
	got := buf.DrainSince(time.Unix(0, 0))
	if len(got) != 0 {
		t.Fatalf("got %d events, want 0", len(got))
	}
}
