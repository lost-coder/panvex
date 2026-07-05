package runtimeevents_test

import (
	"fmt"
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

// TestAppendBatchDedupsReconnectReplay: a replayed batch (same seq+ts)
// must be dropped; the returned slice reports what was actually stored.
func TestAppendBatchDedupsReconnectReplay(t *testing.T) {
	b := runtimeevents.New(300)
	ts := time.Now().UTC()
	batch := make([]runtimeevents.Event, 0, 200)
	for i := 1; i <= 200; i++ {
		batch = append(batch, runtimeevents.Event{Seq: uint64(i), Ts: ts.Add(time.Duration(i) * time.Millisecond), Level: "info", Message: fmt.Sprintf("ev-%d", i)})
	}

	first := b.AppendBatch("agent-1", batch)
	if len(first) != 200 {
		t.Fatalf("first AppendBatch stored %d, want 200", len(first))
	}
	// Reconnect replay: agent re-sends the same buffered events.
	replay := b.AppendBatch("agent-1", batch)
	if len(replay) != 0 {
		t.Fatalf("replay AppendBatch stored %d, want 0", len(replay))
	}
	if got := len(b.Snapshot("agent-1", nil, 0)); got != 200 {
		t.Fatalf("ring holds %d events, want 200 (each exactly once)", got)
	}
}

// TestAppendBatchAdmitsAgentRestart: a fresh agent process rewinds seq
// but carries newer timestamps — those events must NOT be treated as
// replays, and the watermark must rewind with them.
func TestAppendBatchAdmitsAgentRestart(t *testing.T) {
	b := runtimeevents.New(300)
	oldTs := time.Now().UTC()
	b.AppendBatch("agent-1", []runtimeevents.Event{
		{Seq: 149, Ts: oldTs, Level: "info", Message: "before-restart-1"},
		{Seq: 150, Ts: oldTs.Add(time.Millisecond), Level: "info", Message: "before-restart-2"},
	})

	newTs := oldTs.Add(time.Minute) // restart: fresh counter, newer clock
	stored := b.AppendBatch("agent-1", []runtimeevents.Event{
		{Seq: 1, Ts: newTs, Level: "info", Message: "after-restart-1"},
		{Seq: 2, Ts: newTs.Add(time.Millisecond), Level: "info", Message: "after-restart-2"},
	})
	if len(stored) != 2 {
		t.Fatalf("restart batch stored %d, want 2", len(stored))
	}
	if got := len(b.Snapshot("agent-1", nil, 0)); got != 4 {
		t.Fatalf("ring holds %d events, want 4", got)
	}
}

func TestRingEvictionKeepsNewestInOrder(t *testing.T) {
	b := runtimeevents.New(3)
	base := time.Date(2026, time.July, 2, 12, 0, 0, 0, time.UTC)
	// 7 событий в ринг ёмкостью 3 → живут 5, 6, 7 (три обёртки head'а).
	for i := 1; i <= 7; i++ {
		b.Append("a1", runtimeevents.Event{Ts: base.Add(time.Duration(i) * time.Second), Level: "info", Message: fmt.Sprintf("m%d", i)})
	}
	got := b.Snapshot("a1", nil, 0)
	want := []string{"m7", "m6", "m5"} // newest first
	if len(got) != len(want) {
		t.Fatalf("snapshot len = %d, want %d (%+v)", len(got), len(want), got)
	}
	for i, w := range want {
		if got[i].Message != w {
			t.Fatalf("snapshot[%d] = %q, want %q (full: %+v)", i, got[i].Message, w, got)
		}
	}
}

func TestRingSnapshotFiltersAndLimitsAcrossWrap(t *testing.T) {
	b := runtimeevents.New(4)
	for i := 1; i <= 10; i++ {
		level := "info"
		if i%2 == 0 {
			level = "error"
		}
		b.Append("a1", runtimeevents.Event{Level: level, Message: fmt.Sprintf("m%d", i)})
	}
	// Живут m7..m10; error среди них — m8, m10; newest-first + limit 1.
	got := b.Snapshot("a1", []string{"error"}, 1)
	if len(got) != 1 || got[0].Message != "m10" {
		t.Fatalf("filtered snapshot = %+v, want single m10", got)
	}
}

func TestRemoveDropsAgentRing(t *testing.T) {
	b := runtimeevents.New(3)
	b.Append("a1", runtimeevents.Event{Message: "m1"})
	b.Append("a2", runtimeevents.Event{Message: "n1"})

	b.Remove("a1")

	if got := b.Snapshot("a1", nil, 0); len(got) != 0 {
		t.Fatalf("a1 snapshot after Remove = %+v, want empty", got)
	}
	if got := b.Snapshot("a2", nil, 0); len(got) != 1 {
		t.Fatalf("a2 snapshot = %+v, want untouched", got)
	}
	// Повторный Append после Remove создаёт свежий ринг (нет «воскресших»).
	b.Append("a1", runtimeevents.Event{Message: "fresh"})
	if got := b.Snapshot("a1", nil, 0); len(got) != 1 || got[0].Message != "fresh" {
		t.Fatalf("a1 snapshot after re-append = %+v, want single fresh", got)
	}
}

func BenchmarkAppendFullRing(b *testing.B) {
	buf := runtimeevents.New(256)
	for i := 0; i < 256; i++ {
		buf.Append("a1", runtimeevents.Event{Message: "warm"})
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.Append("a1", runtimeevents.Event{Message: "x"})
	}
}
