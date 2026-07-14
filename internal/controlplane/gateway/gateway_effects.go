package gateway

import (
	"context"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

type jobResultEffect struct {
	agentID    string
	jobID      string
	success    bool
	message    string
	resultJSON string
	observedAt time.Time
}

type auditEffect struct {
	actorID  string
	action   string
	targetID string
	details  map[string]any
}

// queuePolicy selects what enqueue does when the queue is full. The three
// policies are NOT interchangeable — each expresses what losing an item
// would cost:
//
//   - policyBlock: never lose the item. Used for priority inbound messages
//     (job results / acknowledgements): a lost job result strands the job.
//     The sender parks until the queue accepts or the connection dies.
//   - policyTryOnce: the caller has a synchronous fallback. Used for the
//     priority effect queues, whose enqueue failure makes the caller perform
//     the audit/result write inline instead.
//   - policyDropOldest: freshest-wins. Used for regular inbound messages and
//     runtime snapshots, which carry absolute state (not deltas), so a
//     dropped item is superseded by the next tick anyway.
type queuePolicy int

const (
	policyBlock queuePolicy = iota
	policyTryOnce
	policyDropOldest
)

// boundedQueue is one of the per-connection stream queues. It exists so the
// five queues in an agent stream share one enqueue/drain implementation and
// differ only in their policy and capacity.
//
// Not safe against concurrent close (there is none — the queues live on
// Connect's stack and are garbage-collected with it).
type boundedQueue[T any] struct {
	ch     chan T
	policy queuePolicy

	// drops counts items lost to a full queue under policyDropOldest. May be
	// nil (tests, no metrics wired).
	drops prometheus.Counter
	// onDrop, when set, is called for each dropped item so the caller can log
	// item-specific context.
	onDrop func(ctx context.Context, item T)
}

func newBoundedQueue[T any](capacity int, policy queuePolicy) *boundedQueue[T] {
	return &boundedQueue[T]{ch: make(chan T, capacity), policy: policy}
}

// withDropObserver attaches the drop counter and an optional per-item log
// hook. Only meaningful for policyDropOldest queues.
func (q *boundedQueue[T]) withDropObserver(drops prometheus.Counter, onDrop func(ctx context.Context, item T)) *boundedQueue[T] {
	q.drops = drops
	q.onDrop = onDrop
	return q
}

// recv exposes the receive side for select loops.
func (q *boundedQueue[T]) recv() <-chan T {
	if q == nil {
		return nil
	}
	return q.ch
}

// enqueue applies the queue's policy. It reports whether the item was taken
// into the pipeline: false means the caller must handle the item itself
// (policyTryOnce) or give up because the connection is gone. A drop-oldest
// queue that discards an item still reports true — the item is gone on
// purpose, and the caller has no better recourse.
func (q *boundedQueue[T]) enqueue(ctx context.Context, item T) bool {
	if q == nil || ctx.Err() != nil {
		return false
	}

	switch q.policy {
	case policyBlock:
		select {
		case <-ctx.Done():
			return false
		case q.ch <- item:
			return true
		}

	case policyTryOnce:
		select {
		case <-ctx.Done():
			return false
		case q.ch <- item:
			return true
		default:
			return false
		}

	case policyDropOldest:
		select {
		case <-ctx.Done():
			return false
		case q.ch <- item:
			return true
		default:
		}

		// Drop one stale item to make room for the freshest state.
		select {
		case <-q.ch:
		default:
		}

		select {
		case <-ctx.Done():
			return false
		case q.ch <- item:
		default:
			// A concurrent reader re-filled the slot between the drain above
			// and this send, so the fresh item is discarded. Surface it rather
			// than losing it silently (D-2 / R4 §1.12).
			q.observeDrop(ctx, item)
		}
		return true
	}

	return false
}

func (q *boundedQueue[T]) observeDrop(ctx context.Context, item T) {
	if q.drops != nil {
		q.drops.Inc()
	}
	if q.onDrop != nil {
		q.onDrop(ctx, item)
	}
}

// drain consumes everything currently queued and returns. Used on the
// shutdown path, where the stream context is already cancelled and the
// handler runs against context.Background().
func (q *boundedQueue[T]) drain(handle func(item T)) {
	if q == nil {
		return
	}
	for {
		select {
		case item := <-q.ch:
			handle(item)
		default:
			return
		}
	}
}
