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
	got := buf.DrainAfter(0)
	if len(got) != 2 || got[0].Message != "b" || got[1].Message != "c" {
		t.Fatalf("got = %+v, want [b c]", got)
	}
}

func TestBufferDrainAfterReturnsChronologicalSliceAfterCursor(t *testing.T) {
	buf := runtimeevents.NewBuffer(10)
	buf.Append(runtimeevents.Event{Ts: time.Unix(1, 0), Message: "a"})
	buf.Append(runtimeevents.Event{Ts: time.Unix(2, 0), Message: "b"})
	buf.Append(runtimeevents.Event{Ts: time.Unix(3, 0), Message: "c"})

	all := buf.DrainAfter(0)
	if len(all) != 3 {
		t.Fatalf("DrainAfter(0) len = %d, want 3", len(all))
	}
	got := buf.DrainAfter(all[0].Seq)
	if len(got) != 2 || got[0].Message != "b" || got[1].Message != "c" {
		t.Fatalf("got = %+v, want [b c]", got)
	}
}

// TestBufferDrainAfterKeepsEqualTimestampEvents is the L-7 regression: a
// strictly-after-by-Ts cursor dropped events sharing the previous event's
// timestamp. The monotonic seq cursor must surface every distinct event
// even when timestamps collide (coarse clocks, burst logging).
func TestBufferDrainAfterKeepsEqualTimestampEvents(t *testing.T) {
	buf := runtimeevents.NewBuffer(10)
	ts := time.Unix(1700000000, 0).UTC()
	buf.Append(runtimeevents.Event{Ts: ts, Message: "first"})
	buf.Append(runtimeevents.Event{Ts: ts, Message: "second"}) // identical Ts

	all := buf.DrainAfter(0)
	if len(all) != 2 {
		t.Fatalf("DrainAfter(0) len = %d, want 2", len(all))
	}
	if all[0].Seq == all[1].Seq {
		t.Fatalf("equal-Ts events share Seq %d; cursor cannot disambiguate", all[0].Seq)
	}

	// Advance the cursor as the pusher would after sending only the first.
	rest := buf.DrainAfter(all[0].Seq)
	if len(rest) != 1 {
		t.Fatalf("DrainAfter(first seq) len = %d, want 1 (equal-Ts event lost)", len(rest))
	}
	if rest[0].Message != "second" {
		t.Errorf("remaining event = %q, want %q", rest[0].Message, "second")
	}
}

// TestBufferSeqIsMonotonicAcrossOverflow ensures the seq counter keeps
// climbing past dropped entries, so a cursor never re-reads a recycled slot.
func TestBufferSeqIsMonotonicAcrossOverflow(t *testing.T) {
	buf := runtimeevents.NewBuffer(2)
	ts := time.Unix(1, 0)
	buf.Append(runtimeevents.Event{Ts: ts, Message: "a"})
	buf.Append(runtimeevents.Event{Ts: ts, Message: "b"})
	buf.Append(runtimeevents.Event{Ts: ts, Message: "c"}) // drops "a"

	got := buf.DrainAfter(0)
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if got[0].Seq >= got[1].Seq {
		t.Fatalf("seq not increasing: %d, %d", got[0].Seq, got[1].Seq)
	}
	if got[1].Seq != 3 {
		t.Errorf("third append Seq = %d, want 3 (monotonic across overflow)", got[1].Seq)
	}
}

func TestBufferEmptyDrain(t *testing.T) {
	buf := runtimeevents.NewBuffer(3)
	got := buf.DrainAfter(0)
	if len(got) != 0 {
		t.Fatalf("got %d events, want 0", len(got))
	}
}
