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
	mu          sync.Mutex
	sequence    uint64
	subscribers map[uint64]chan eventEnvelope
	// onDrop, if non-nil, is invoked every time publish() drops an event for a
	// slow subscriber. The metrics subsystem wires this to increment
	// panvex_event_hub_drop_total. Kept as a func so the hub has no direct
	// dependency on Prometheus.
	onDrop func()
}

func newEventHub() *eventHub {
	return &eventHub{
		subscribers: make(map[uint64]chan eventEnvelope),
	}
}

// subscriberCount returns the current number of active subscribers.
func (h *eventHub) subscriberCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.subscribers)
}

// setDropHook installs the callback invoked on every dropped event. Calling
// with nil disables the hook. Safe to call concurrently with publish.
//
// Contract: the hook MUST be non-blocking and MUST NOT call back into the
// hub (publish/subscribe/setDropHook/subscriberCount) — it runs while
// eventHub.mu is held, so any re-entry will deadlock. The current wiring
// (prometheus.Counter.Inc, which is lock-free) satisfies this.
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

func (h *eventHub) publish(event eventEnvelope) {
	h.mu.Lock()
	defer h.mu.Unlock()

	for _, subscriber := range h.subscribers {
		select {
		case subscriber <- event:
		default:
			slog.Debug("event dropped for slow subscriber", "event_type", event.Type)
			if h.onDrop != nil {
				h.onDrop()
			}
		}
	}
}
