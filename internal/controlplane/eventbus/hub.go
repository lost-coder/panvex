// Package eventbus provides an in-process audit/event pub/sub facade. It is
// an in-memory broadcast hub extracted from internal/controlplane/server
// (event_hub.go) by P3-ARCH-01e. Single-instance by design — the panel does
// not scale horizontally (owner decision) — so the former pluggable Backend
// seam was collapsed into the concrete memoryBackend (P5, audit #19).
//
// The Hub type is the public facade callers interact with. Publish and
// Subscribe are the only two operations application code needs.
package eventbus

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"sync/atomic"
)

// Event is a transport-agnostic envelope broadcast to every subscriber. The
// shape intentionally mirrors the JSON encoding consumed by the WebSocket
// handler in server/http_events.go so the payload can be serialised verbatim.
type Event struct {
	Type string `json:"type"`
	Data any    `json:"data"`
	// Seq is a hub-global monotonic sequence number assigned by Publish
	// (NOT per subscriber): events dropped for a slow subscriber still
	// consume numbers, so a gap in the delivered stream is the consumer's
	// signal that it missed something and must resync (D6c). The WebSocket
	// envelope serialises this verbatim; the dashboard reacts to gaps with
	// a broad query refetch.
	Seq uint64 `json:"seq"`
	// Raw is the pre-marshaled JSON envelope {"type","data","seq"},
	// assigned by Publish AFTER Seq so every subscriber ships identical
	// bytes without re-marshaling per connection (P6-6.3b, finding #14).
	// Excluded from json.Marshal so marshaling an Event still yields the
	// same envelope. Consumers treat empty Raw as "marshal yourself"
	// (fallback for test backends that do not pre-marshal).
	Raw []byte `json:"-"`
}

// eventEncodingFailedEnvelope is the fallback payload used when an
// event's Data cannot be marshaled. Mirrors the former server-side
// mustJSON fallback byte-for-byte.
var eventEncodingFailedEnvelope = []byte(`{"type":"server.error","data":{"error":"event encoding failed"}}`)

// marshalEnvelope serialises the full event envelope once. Returns the
// fallback error envelope when Data is not marshalable.
func marshalEnvelope(evt Event) []byte {
	data, err := json.Marshal(evt) // Raw is json:"-", so this is the plain envelope
	if err != nil {
		return eventEncodingFailedEnvelope
	}
	return data
}

// memorySubscriberBuffer is the per-subscriber channel buffer depth. Events
// beyond this watermark are dropped for that subscriber; other subscribers
// are unaffected. Matches the pre-extraction constant (64).
const memorySubscriberBuffer = 64

// subscriber pairs a unique id with its delivery channel and a per-channel
// state pointer. The id lets the cancel closure identify which entry to
// drop from the snapshot. The state pointer carries a sync.RWMutex + closed
// flag that serialises sends against close so the race detector and
// runtime never see send-on-closed:
//
//   - Publish takes state.mu.RLock() before sending. Many publishers hold
//     RLocks simultaneously — no contention in the steady state.
//   - cancel takes state.mu.Lock() to flip closed and close the channel.
//     This blocks behind any in-flight Publish RLock holder, guaranteeing
//     close runs only when no goroutine is mid-send on this channel.
//
// All snapshot copies share the same *subState so close visibility is
// consistent regardless of which slice generation a Publisher loaded.
type subscriber struct {
	id    uint64
	ch    chan Event
	state *subState
}

type subState struct {
	mu     sync.RWMutex
	closed bool
}

// Hub is the process-local pub/sub fan-out every server-side caller uses.
// Single-instance by design (the panel does not scale horizontally), so
// there is no backend abstraction: this IS the in-memory implementation.
//
// It uses a copy-on-write atomic.Pointer snapshot so Publish reads the
// subscriber list lock-free. Subscribe / cancel serialise on `mu` and
// replace the snapshot pointer; this trades a per-mutation O(N) copy for
// a completely lock-free, allocation-free hot publish path (P-5).
type Hub struct {
	subs atomic.Pointer[[]subscriber] // immutable snapshot, never mutated in place

	// pubSeq is the hub-global publish sequence counter (D6c). Incremented
	// once per Publish call, before fan-out, so dropped events still consume
	// numbers and gaps are visible to consumers.
	pubSeq atomic.Uint64

	// mu serialises Subscribe / cancel / SetDropHook so concurrent
	// mutations do not race when copying the subscriber slice.
	mu       sync.Mutex
	sequence uint64

	// onDrop, if non-nil, is invoked every time Publish drops an event for a
	// slow subscriber. The metrics subsystem wires this to increment
	// panvex_event_hub_drop_total. Stored via atomic.Pointer so Publish can
	// read it without acquiring mu.
	onDrop atomic.Pointer[func()]
}

// NewHub returns an empty Hub ready for Publish/Subscribe.
func NewHub() *Hub {
	h := &Hub{}
	empty := make([]subscriber, 0)
	h.subs.Store(&empty)
	return h
}

// SubscriberCount returns the current number of active subscribers.
func (h *Hub) SubscriberCount() int {
	snap := h.subs.Load()
	if snap == nil {
		return 0
	}
	return len(*snap)
}

// SetDropHook installs the callback invoked on every dropped event. Calling
// with nil disables the hook. Safe to call concurrently with Publish.
//
// Contract: the hook MUST be non-blocking. It runs outside any hub lock so
// re-entry into the hub is allowed, but keep it cheap — Publish calls it
// for every slow subscriber.
func (h *Hub) SetDropHook(fn func()) {
	if fn == nil {
		h.onDrop.Store(nil)
		return
	}
	h.onDrop.Store(&fn)
}

// Subscribe returns a receive-only channel and a cancel func. Safe for
// concurrent use. Replaces the subscriber snapshot using copy-on-write so
// the publish hot path stays lock-free.
func (h *Hub) Subscribe() (<-chan Event, func()) {
	h.mu.Lock()

	h.sequence++
	id := h.sequence
	ch := make(chan Event, memorySubscriberBuffer)
	state := &subState{}
	sub := subscriber{id: id, ch: ch, state: state}

	old := h.subs.Load()
	newSubs := make([]subscriber, 0, len(*old)+1)
	newSubs = append(newSubs, *old...)
	newSubs = append(newSubs, sub)
	h.subs.Store(&newSubs)

	h.mu.Unlock()

	cancel := func() {
		h.mu.Lock()
		defer h.mu.Unlock()

		cur := h.subs.Load()
		filtered := make([]subscriber, 0, len(*cur))
		found := false
		for _, s := range *cur {
			if s.id == id {
				found = true
				continue
			}
			filtered = append(filtered, s)
		}
		if !found {
			return
		}
		h.subs.Store(&filtered)
		// Synchronise close against any in-flight Publish. A Publish that
		// loaded the prior snapshot is currently inside state.mu.RLock();
		// taking state.mu.Lock() blocks until it returns, then we flip
		// closed and close the channel. All subsequent Publish calls see
		// closed==true under their RLock and skip the send.
		state.mu.Lock()
		state.closed = true
		close(ch)
		state.mu.Unlock()
	}

	return ch, cancel
}

// Publish delivers one event to every current subscriber. Reads the
// subscriber snapshot via a single atomic load — no mutex on the hub,
// no allocation, no per-publish copy of the channel slice. Per-subscriber
// state.mu RLock serialises the send against a concurrent close so the
// race detector and runtime never observe a send on a closed channel.
// RLocks are uncontended in the steady state (cancel is rare relative to
// publish), so the cost is a single atomic-CAS-equivalent per subscriber.
//
// Drop-on-full semantics: if a subscriber's buffered channel is full the
// event is dropped for that subscriber and the onDrop hook fires.
//
// D6c: Seq is assigned here, once, before fan-out. Dropped events still
// consume a sequence number so gaps in the delivered stream signal the
// dashboard that a resync is required.
//
// Ordering contract: Seq is unique and monotonic, but under CONCURRENT
// Publish calls a subscriber may observe seq numbers out of order — the
// counter is bumped before fan-out, so a publisher that claimed a lower
// number can lose the race to the send. Consumers must treat Seq as a
// gap detector, not as a total order.
//
// The fan-out is fire-and-forget and carries no request context by
// design; the WS layer owns the request ctx.
//
//nolint:contextcheck // reason: pub/sub fan-out is ctx-free by design; the WS layer owns request ctx
func (h *Hub) Publish(evt Event) {
	evt.Seq = h.pubSeq.Add(1)
	evt.Raw = marshalEnvelope(evt)
	snap := h.subs.Load()
	if snap == nil {
		return
	}
	subs := *snap
	if len(subs) == 0 {
		return
	}
	hookPtr := h.onDrop.Load()
	for i := range subs {
		sub := &subs[i]
		sub.state.mu.RLock()
		if sub.state.closed {
			sub.state.mu.RUnlock()
			continue
		}
		dropped := false
		select {
		case sub.ch <- evt:
		default:
			dropped = true
		}
		sub.state.mu.RUnlock()
		if dropped {
			// Gate slog.Debug behind Enabled() so we don't pay the
			// interface-boxing alloc on the hot path when debug logging
			// is disabled (the common case in production).
			if slog.Default().Enabled(context.Background(), slog.LevelDebug) {
				slog.Debug("event dropped for slow subscriber", "event_type", evt.Type)
			}
			if hookPtr != nil {
				(*hookPtr)()
			}
		}
	}
}
