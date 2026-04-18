// Package eventbus provides an in-process audit/event pub/sub facade with a
// pluggable Backend interface. The current implementation is an in-memory
// broadcast hub extracted from internal/controlplane/server (event_hub.go)
// by P3-ARCH-01e as preparation for P3-ARCH-02 where a NATS- or Redis-backed
// Backend will be plugged in for HA control-plane deployments.
//
// The Hub type is the public facade callers interact with. Publish and
// Subscribe are the only two operations application code needs; Backend is
// the seam for transport-specific implementations.
package eventbus

import (
	"log/slog"
	"sync"
)

// Event is a transport-agnostic envelope broadcast to every subscriber. The
// shape intentionally mirrors the JSON encoding consumed by the WebSocket
// handler in server/http_events.go so the payload can be serialised verbatim.
type Event struct {
	Type string `json:"type"`
	Data any    `json:"data"`
}

// Backend is the pluggable transport seam. The default in-process
// implementation is memoryBackend (returned by NewHub); a future
// NATS/Redis-backed implementation for P3-ARCH-02 will satisfy the same
// interface so the Server can swap transports without touching call sites.
//
// Implementations MUST be safe for concurrent use. Publish must be
// non-blocking for slow subscribers — a stuck consumer must not stall any
// publisher or other consumer. Subscribe returns a receive-only channel and
// a cancel func the caller must invoke to release resources (typically via
// defer).
type Backend interface {
	Publish(evt Event)
	Subscribe() (<-chan Event, func())
	// SubscriberCount returns the current number of active subscribers.
	// Used by the metrics subsystem.
	SubscriberCount() int
	// SetDropHook installs a callback invoked every time an event is dropped
	// for a slow subscriber. The hook MUST be non-blocking; it runs outside
	// any backend lock. Passing nil disables the hook.
	SetDropHook(fn func())
}

// Hub is the concrete facade every server-side caller uses. It delegates to
// the configured Backend but keeps a stable type the Server struct can
// depend on without importing the concrete backend package(s).
type Hub struct {
	backend Backend
}

// NewHub returns a Hub backed by the default in-process memoryBackend. This
// matches the behaviour of the former server.newEventHub() prior to
// P3-ARCH-01e.
func NewHub() *Hub {
	return &Hub{backend: newMemoryBackend()}
}

// NewHubWithBackend wraps an arbitrary Backend. Intended for P3-ARCH-02 and
// for tests that want to swap in a fake/mock transport.
func NewHubWithBackend(b Backend) *Hub {
	return &Hub{backend: b}
}

// Publish broadcasts one event to every current subscriber.
func (h *Hub) Publish(evt Event) { h.backend.Publish(evt) }

// Subscribe registers a new consumer. The returned channel receives every
// event published after Subscribe returns. The cancel func unsubscribes and
// closes the channel; callers MUST invoke cancel (typically via defer) to
// avoid leaking the subscriber slot.
func (h *Hub) Subscribe() (<-chan Event, func()) { return h.backend.Subscribe() }

// SubscriberCount exposes the live subscriber count for metrics.
func (h *Hub) SubscriberCount() int { return h.backend.SubscriberCount() }

// SetDropHook installs the drop counter callback. See Backend.SetDropHook.
func (h *Hub) SetDropHook(fn func()) { h.backend.SetDropHook(fn) }

// memorySubscriberBuffer is the per-subscriber channel buffer depth. Events
// beyond this watermark are dropped for that subscriber; other subscribers
// are unaffected. Matches the pre-extraction constant (64).
const memorySubscriberBuffer = 64

// memoryBackend is the default in-process pub/sub implementation. It is a
// direct move of the previous server.eventHub type. The RWMutex + snapshot
// pattern (landed in P2-PERF-01) is preserved so one slow subscriber cannot
// stall publish() for others.
type memoryBackend struct {
	mu          sync.RWMutex
	sequence    uint64
	subscribers map[uint64]chan Event

	// onDrop, if non-nil, is invoked every time Publish drops an event for a
	// slow subscriber. The metrics subsystem wires this to increment
	// panvex_event_hub_drop_total. Kept as a func so the hub has no direct
	// dependency on Prometheus.
	//
	// Read under RLock so Publish can invoke it without holding the write
	// lock.
	onDrop func()
}

func newMemoryBackend() *memoryBackend {
	return &memoryBackend{
		subscribers: make(map[uint64]chan Event),
	}
}

// SubscriberCount returns the current number of active subscribers.
func (h *memoryBackend) SubscriberCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.subscribers)
}

// SetDropHook installs the callback invoked on every dropped event. Calling
// with nil disables the hook. Safe to call concurrently with Publish.
//
// Contract: the hook MUST be non-blocking. It runs outside any hub lock so
// re-entry into the hub is allowed, but keep it cheap — Publish calls it
// for every slow subscriber.
func (h *memoryBackend) SetDropHook(fn func()) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.onDrop = fn
}

// Subscribe returns a receive-only channel and a cancel func. Safe for
// concurrent use.
func (h *memoryBackend) Subscribe() (<-chan Event, func()) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.sequence++
	id := h.sequence
	ch := make(chan Event, memorySubscriberBuffer)
	h.subscribers[id] = ch

	cancel := func() {
		h.mu.Lock()
		defer h.mu.Unlock()

		existing, ok := h.subscribers[id]
		if !ok {
			return
		}

		delete(h.subscribers, id)
		close(existing)
	}

	return ch, cancel
}

// Publish delivers one event to every current subscriber without holding the
// hub write lock across sends. The subscriber slice is snapshotted under
// RLock, then released before the non-blocking select for each channel. This
// ensures a slow subscriber cannot stall concurrent Publish callers or block
// Subscribe / cancel.
//
// Drop-on-full semantics: if a subscriber's buffered channel is full the
// event is dropped for that subscriber and the onDrop hook fires.
func (h *memoryBackend) Publish(evt Event) {
	h.mu.RLock()
	// Snapshot subscriber channels onto a local slice so we do not hold the
	// lock while sending. Map iteration under RLock is safe; allocating a
	// small slice per publish is cheap compared to the cost of holding the
	// lock across 100+ channel sends to sluggish HTTP SSE clients.
	subscribers := make([]chan Event, 0, len(h.subscribers))
	for _, subscriber := range h.subscribers {
		subscribers = append(subscribers, subscriber)
	}
	onDrop := h.onDrop
	h.mu.RUnlock()

	for _, subscriber := range subscribers {
		select {
		case subscriber <- evt:
		default:
			slog.Debug("event dropped for slow subscriber", "event_type", evt.Type)
			if onDrop != nil {
				onDrop()
			}
		}
	}
}
