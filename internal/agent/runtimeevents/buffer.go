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
	// Seq is a per-Buffer monotonic sequence number assigned on Append.
	// It is the drain cursor: it disambiguates events that share a Ts
	// (coarse clocks, burst logging) which a strictly-after-by-Ts cursor
	// would silently drop. Producers leave it zero; Append overwrites it.
	// See L-7.
	Seq     uint64
	Ts      time.Time
	Level   string // "info" | "warn" | "error"
	Message string
	Fields  map[string]string
}

// Buffer is a fixed-capacity ring of events. Append never blocks the
// producer; on overflow the oldest entry is dropped silently. DrainAfter
// returns events whose Seq is strictly greater than the provided cursor,
// in chronological order. Safe for concurrent use.
type Buffer struct {
	mu       sync.Mutex
	capacity int
	items    []Event
	seq      uint64 // last assigned sequence number; monotonic, survives overflow
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
	b.seq++
	ev.Seq = b.seq
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

// DrainAfter returns events whose Seq is strictly greater than afterSeq,
// in chronological (append) order. Pass 0 to drain everything currently
// buffered. Buffer is not mutated; callers advance their cursor to the
// Seq of the last event they successfully handled. Using the monotonic
// Seq instead of Ts means events sharing a timestamp are never lost
// (L-7).
func (b *Buffer) DrainAfter(afterSeq uint64) []Event {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]Event, 0, len(b.items))
	for _, ev := range b.items {
		if ev.Seq > afterSeq {
			out = append(out, ev)
		}
	}
	return out
}
