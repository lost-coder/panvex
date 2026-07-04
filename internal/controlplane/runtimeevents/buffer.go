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

type ring struct {
	mu    sync.Mutex
	items []Event
	cap   int
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
	if len(r.items) >= r.cap {
		copy(r.items, r.items[1:])
		r.items = r.items[:len(r.items)-1]
	}
	r.items = append(r.items, ev)
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
	snapshot := make([]Event, len(r.items))
	copy(snapshot, r.items)
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
