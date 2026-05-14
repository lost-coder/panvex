// Package runtimeevents holds the agent-side in-memory ring of recent
// slog records that are eligible for shipping to the panel via the
// existing Connect bidi stream. The ring is bounded in size and lock-
// safe; it is fed by Handler (handler.go) and drained by the per-session
// pusher goroutine in cmd/agent.
package runtimeevents

import (
	"sync"
	"time"
)

// Event is the wire shape used both inside the agent ring and over gRPC
// after conversion in cmd/agent. Keeping a single Go-side type avoids
// drift between the buffer and the pusher.
type Event struct {
	Ts      time.Time
	Level   string // "info" | "warn" | "error"
	Message string
	Fields  map[string]string
}

// Buffer is a fixed-capacity ring of events. Append never blocks the
// producer; on overflow the oldest entry is dropped silently. DrainSince
// returns events strictly newer than the provided cursor, in
// chronological order. Safe for concurrent use.
type Buffer struct {
	mu       sync.Mutex
	capacity int
	items    []Event
}

// NewBuffer constructs a Buffer with the given capacity. capacity is
// clamped to 1 if <= 0.
func NewBuffer(capacity int) *Buffer {
	if capacity <= 0 {
		capacity = 1
	}
	return &Buffer{capacity: capacity, items: make([]Event, 0, capacity)}
}

func (b *Buffer) Append(ev Event) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.items) >= b.capacity {
		copy(b.items, b.items[1:])
		b.items = b.items[:len(b.items)-1]
	}
	b.items = append(b.items, ev)
}

func (b *Buffer) Len() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.items)
}

// DrainSince returns events whose Ts is strictly after cursor, in
// chronological order. Buffer is not mutated.
func (b *Buffer) DrainSince(after time.Time) []Event {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]Event, 0, len(b.items))
	for _, ev := range b.items {
		if ev.Ts.After(after) {
			out = append(out, ev)
		}
	}
	return out
}
