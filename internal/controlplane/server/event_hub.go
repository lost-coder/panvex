package server

import (
	"log/slog"
	"sync"
)

type eventEnvelope struct {
	Type string `json:"type"`
	Data any    `json:"data"`
}

type eventHub struct {
	mu          sync.RWMutex
	sequence    uint64
	subscribers map[uint64]chan eventEnvelope
	// onDrop, if non-nil, is invoked every time publish() drops an event for a
	// slow subscriber. The metrics subsystem wires this to increment
	// panvex_event_hub_drop_total. Kept as a func so the hub has no direct
	// dependency on Prometheus.
	//
	// Stored as an atomic-ish value read under RLock so publish() can invoke
	// it without holding the write lock. See publish() for the reasoning.
	onDrop func()
}

func newEventHub() *eventHub {
	return &eventHub{
		subscribers: make(map[uint64]chan eventEnvelope),
	}
}

// subscriberCount returns the current number of active subscribers.
func (h *eventHub) subscriberCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.subscribers)
}

// setDropHook installs the callback invoked on every dropped event. Calling
// with nil disables the hook. Safe to call concurrently with publish.
//
// Contract: the hook MUST be non-blocking. It runs outside any hub lock so
// re-entry into the hub is allowed, but keep it cheap — publish() calls it
// for every slow subscriber.
func (h *eventHub) setDropHook(fn func()) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.onDrop = fn
}

func (h *eventHub) subscribe() (<-chan eventEnvelope, func()) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.sequence++
	id := h.sequence
	ch := make(chan eventEnvelope, 64)
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

// publish delivers one event to every current subscriber without holding the
// hub write lock across sends. The subscriber slice is snapshotted under
// RLock, then released before the non-blocking select for each channel. This
// ensures a slow subscriber cannot stall concurrent publish() callers or
// block subscribe()/cancel.
//
// Drop-on-full semantics are unchanged: if a subscriber's buffered channel is
// full the event is dropped for that subscriber and the onDrop hook fires.
func (h *eventHub) publish(event eventEnvelope) {
	h.mu.RLock()
	// Snapshot subscriber channels onto a local slice so we do not hold the
	// lock while sending. Map iteration under RLock is safe; allocating a
	// small slice per publish is cheap compared to the cost of holding the
	// lock across 100+ channel sends to sluggish HTTP SSE clients.
	subscribers := make([]chan eventEnvelope, 0, len(h.subscribers))
	for _, subscriber := range h.subscribers {
		subscribers = append(subscribers, subscriber)
	}
	onDrop := h.onDrop
	h.mu.RUnlock()

	for _, subscriber := range subscribers {
		select {
		case subscriber <- event:
		default:
			slog.Debug("event dropped for slow subscriber", "event_type", event.Type)
			if onDrop != nil {
				onDrop()
			}
		}
	}
}
