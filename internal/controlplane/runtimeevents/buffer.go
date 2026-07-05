// Package runtimeevents holds the panel-side per-agent in-memory rings
// of runtime events shipped over the Connect bidi stream. The package
// is intentionally lightweight: no persistence, no DB, no retention
// worker. Recovery on panel restart is by-design empty — durable
// history lives in agent slog output (Phase 2 JSON handler ships into
// Loki / journald).
package runtimeevents

import (
	"sync"
	"time"
)

// Event mirrors internal/agent/runtimeevents.Event but lives in its own
// package to keep the panel side independent of agent internals.
type Event struct {
	// Seq is the agent-process-monotonic sequence assigned by the
	// agent-side ring. 0 means "unknown" (never dedup-dropped).
	Seq     uint64
	Ts      time.Time
	Level   string // "info" | "warn" | "error"
	Message string
	Fields  map[string]string
}

// Buffer holds per-agent ring buffers, each capacity perAgentCapacity.
// The outer map is RWMutex-protected; per-agent rings are mu-protected.
type Buffer struct {
	perAgentCapacity int
	mu               sync.RWMutex
	rings            map[string]*ring
}

// ring is a fixed-capacity circular buffer (P6-6.3e, finding #14). items
// grows up to cap once and is then reused in place: head is the index of
// the OLDEST live element, count the number of live elements. Append is
// O(1) — the previous implementation memmoved the whole slice
// (copy(items, items[1:])) on every append once full.
type ring struct {
	mu    sync.Mutex
	items []Event
	cap   int
	head  int
	count int
	// lastSeq/lastTs form the reconnect-replay watermark (audit #9b):
	// an event with seq and ts both at-or-below the watermark is a
	// replay. A genuine agent restart rewinds seq but carries a newer
	// ts, so the ts guard admits it and the watermark rewinds too.
	lastSeq uint64
	lastTs  time.Time
}

func New(perAgentCapacity int) *Buffer {
	if perAgentCapacity <= 0 {
		perAgentCapacity = 1
	}
	return &Buffer{
		perAgentCapacity: perAgentCapacity,
		rings:            map[string]*ring{},
	}
}

// appendLocked inserts ev unless the watermark marks it as a reconnect
// replay. Reports whether the event was stored. Caller holds r.mu.
func (r *ring) appendLocked(ev Event) bool {
	if ev.Seq != 0 && ev.Seq <= r.lastSeq && !ev.Ts.After(r.lastTs) {
		return false
	}
	if ev.Seq != 0 {
		r.lastSeq = ev.Seq
		r.lastTs = ev.Ts
	}
	if r.count < r.cap {
		if len(r.items) < r.cap {
			r.items = append(r.items, ev)
		} else {
			r.items[(r.head+r.count)%r.cap] = ev
		}
		r.count++
		return true
	}
	// Full: overwrite the oldest slot and advance head.
	r.items[r.head] = ev
	r.head = (r.head + 1) % r.cap
	return true
}

// Append inserts one event; reports whether it was stored (false = replay).
func (b *Buffer) Append(agentID string, ev Event) bool {
	r := b.getOrCreate(agentID)
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.appendLocked(ev)
}

// AppendBatch inserts evs in order and returns the events that were
// actually stored, so callers publish only non-replayed records.
func (b *Buffer) AppendBatch(agentID string, evs []Event) []Event {
	r := b.getOrCreate(agentID)
	r.mu.Lock()
	defer r.mu.Unlock()
	stored := make([]Event, 0, len(evs))
	for _, ev := range evs {
		if r.appendLocked(ev) {
			stored = append(stored, ev)
		}
	}
	return stored
}

// Snapshot returns up to `limit` most-recent events for agentID, newest
// first, optionally filtered by `levels` (any-of). limit <= 0 means all.
func (b *Buffer) Snapshot(agentID string, levels []string, limit int) []Event {
	b.mu.RLock()
	r, ok := b.rings[agentID]
	b.mu.RUnlock()
	if !ok {
		return nil
	}
	r.mu.Lock()
	snapshot := make([]Event, r.count)
	for i := 0; i < r.count; i++ {
		snapshot[i] = r.items[(r.head+i)%r.cap]
	}
	r.mu.Unlock()

	levelSet := levelSetOf(levels)
	out := make([]Event, 0, len(snapshot))
	for i := len(snapshot) - 1; i >= 0; i-- {
		ev := snapshot[i]
		if levelSet != nil && !levelSet[ev.Level] {
			continue
		}
		out = append(out, ev)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}

// Remove drops the agent's ring entirely. Called from the deregistration
// path (purgeAgentInMemory) so per-agent rings do not leak after an agent
// is removed, and a reused agentID starts from an empty ring.
func (b *Buffer) Remove(agentID string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.rings, agentID)
}

func (b *Buffer) getOrCreate(agentID string) *ring {
	b.mu.RLock()
	if r, ok := b.rings[agentID]; ok {
		b.mu.RUnlock()
		return r
	}
	b.mu.RUnlock()
	b.mu.Lock()
	defer b.mu.Unlock()
	if r, ok := b.rings[agentID]; ok {
		return r
	}
	r := &ring{cap: b.perAgentCapacity, items: make([]Event, 0, b.perAgentCapacity)}
	b.rings[agentID] = r
	return r
}

func levelSetOf(levels []string) map[string]bool {
	if len(levels) == 0 {
		return nil
	}
	set := make(map[string]bool, len(levels))
	for _, l := range levels {
		set[l] = true
	}
	return set
}
