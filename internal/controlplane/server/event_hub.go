package server

import "sync"

type eventEnvelope struct {
	Type string `json:"type"`
	Data any    `json:"data"`
}

type eventHub struct {
	mu          sync.Mutex
	sequence    uint64
	subscribers map[uint64]chan eventEnvelope
}

func newEventHub() *eventHub {
	return &eventHub{
		subscribers: make(map[uint64]chan eventEnvelope),
	}
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
		}
	}
}
