package presence

import (
	"log/slog"
	"sync"
	"time"
)

// State describes the current liveness state of an agent stream.
type State string

const (
	// StateOnline marks an agent with a recent heartbeat.
	StateOnline State = "online"
	// StateDegraded marks an agent whose heartbeat is stale but not fully expired.
	StateDegraded State = "degraded"
	// StateOffline marks an agent that is disconnected or has exceeded the offline threshold.
	StateOffline State = "offline"
)

type agentPresence struct {
	connectedAt time.Time
	lastSeenAt  time.Time
	// lastState caches the most recently Evaluate-derived state so we can
	// emit a single Info-level "presence transition" log only when the
	// derived state actually changes. The zero value (empty string) means
	// "never evaluated"; the first Evaluate after MarkConnected establishes
	// the baseline without logging.
	lastState State
}

// Tracker evaluates agent liveness from connect and heartbeat timestamps.
type Tracker struct {
	mu              sync.RWMutex
	degradedAfter   time.Duration
	offlineAfter    time.Duration
	agentTimestamps map[string]agentPresence
}

// NewTracker constructs a presence tracker using degraded and offline thresholds.
func NewTracker(degradedAfter, offlineAfter time.Duration) *Tracker {
	return &Tracker{
		degradedAfter:   degradedAfter,
		offlineAfter:    offlineAfter,
		agentTimestamps: make(map[string]agentPresence),
	}
}

// MarkConnected records a new or replacement stream for an agent.
func (t *Tracker) MarkConnected(agentID string, connectedAt time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.agentTimestamps[agentID] = agentPresence{
		connectedAt: connectedAt.UTC(),
		lastSeenAt:  connectedAt.UTC(),
	}
}

// Heartbeat records activity for an existing agent stream.
func (t *Tracker) Heartbeat(agentID string, observedAt time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()

	presence := t.agentTimestamps[agentID]
	if presence.connectedAt.IsZero() {
		presence.connectedAt = observedAt.UTC()
	}

	presence.lastSeenAt = observedAt.UTC()
	t.agentTimestamps[agentID] = presence
}

// Remove deletes an agent from the presence tracker entirely.
func (t *Tracker) Remove(agentID string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.agentTimestamps, agentID)
}

// ConnectedAt returns the recorded stream-open timestamp for the given agent.
// The second return value is false when the agent is not currently tracked.
// Used by diagnostics and tests (P2-LOG-12 / L-05) to verify that connectedAt
// reflects the actual stream-open moment rather than being rewritten on every
// snapshot.
func (t *Tracker) ConnectedAt(agentID string) (time.Time, bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	presence, ok := t.agentTimestamps[agentID]
	if !ok {
		return time.Time{}, false
	}
	return presence.connectedAt, true
}

// Evaluate returns the derived liveness state for the requested agent.
//
// As a side effect, Evaluate logs a single Info-level "presence transition"
// event when the derived state differs from the previously observed state
// for the same agent. Per-tick "no change" evaluations stay silent. The
// transition log is the single point at which presence flips are surfaced
// (P2-LOG-09 / L-09); callers that just want the raw state pay the same
// cost they always did.
// P6-6.3a (finding #14): the common no-transition case runs entirely
// under RLock so per-agent evaluations from list handlers do not
// serialize against each other or against stream heartbeats. Only a
// REAL transition (derived != cached lastState) escalates to the write
// lock, where evaluateLocked re-derives under exclusion (the state may
// have changed between RUnlock and Lock) and emits the transition log
// exactly once (P2-LOG-09 semantics preserved).
func (t *Tracker) Evaluate(agentID string, now time.Time) State {
	t.mu.RLock()
	presence, ok := t.agentTimestamps[agentID]
	if !ok {
		t.mu.RUnlock()
		return StateOffline
	}
	next := t.deriveState(presence, now)
	if presence.lastState == next {
		t.mu.RUnlock()
		return next
	}
	t.mu.RUnlock()

	t.mu.Lock()
	next, transition := t.evaluateLocked(agentID, now)
	t.mu.Unlock()

	if transition != nil {
		logTransitions([]presenceTransition{*transition})
	}
	return next
}

// deriveState computes the liveness state from the last-seen timestamp.
// Pure — no cache mutation, callable under RLock.
func (t *Tracker) deriveState(p agentPresence, now time.Time) State {
	idle := now.UTC().Sub(p.lastSeenAt)
	switch {
	case idle >= t.offlineAfter:
		return StateOffline
	case idle >= t.degradedAfter:
		return StateDegraded
	default:
		return StateOnline
	}
}

// EvaluateAll derives the liveness state of every tracked agent at now —
// applying the same transition-logging side effect as Evaluate — and
// returns how many are currently connected (state != StateOffline).
//
// This is the background-poller entry point (L-8). Without a periodic
// sweep, an agent that stops heartbeating never transitions to offline
// and the panvex_agent_connected gauge (previously TrackedCount) keeps
// counting it until deregistration. The metrics poller calls this each
// tick so the gauge reflects evaluated liveness, not raw map size.
func (t *Tracker) EvaluateAll(now time.Time) (connected int) {
	t.mu.Lock()
	transitions := make([]presenceTransition, 0, 4)
	for agentID := range t.agentTimestamps {
		state, transition := t.evaluateLocked(agentID, now)
		if state != StateOffline {
			connected++
		}
		if transition != nil {
			transitions = append(transitions, *transition)
		}
	}
	t.mu.Unlock()

	// R7: log AFTER releasing the lock. This walks the whole fleet, and
	// emitting slog.Info from inside the critical section meant a slow log
	// sink (a blocked stderr pipe, a busy journald) stalled every heartbeat
	// in the panel behind it.
	logTransitions(transitions)
	return connected
}

// presenceTransition is one agent's state change, collected under the lock and
// logged after it is released.
type presenceTransition struct {
	agentID string
	from    State
	to      State
}

func logTransitions(transitions []presenceTransition) {
	for _, tr := range transitions {
		// Info on every real transition. No ctx is available here — Evaluate is
		// invoked from request handlers and background metric pollers alike;
		// slog.Info propagates default attrs but drops the per-request span
		// linkage. That is acceptable for an agent-level lifecycle event.
		slog.Info("presence transition",
			"agent_id", tr.agentID,
			"from", string(tr.from),
			"to", string(tr.to),
		)
	}
}

// evaluateLocked is the shared core of Evaluate/EvaluateAll. The caller must
// hold t.mu (write lock — it may mutate the cached lastState). It does NOT log:
// it RETURNS the transition, if any, so the caller can emit it after unlocking.
func (t *Tracker) evaluateLocked(agentID string, now time.Time) (State, *presenceTransition) {
	presence, ok := t.agentTimestamps[agentID]
	if !ok {
		return StateOffline, nil
	}

	next := t.deriveState(presence, now)
	prev := presence.lastState

	var transition *presenceTransition
	if prev != "" && prev != next {
		transition = &presenceTransition{agentID: agentID, from: prev, to: next}
	}
	if prev != next {
		presence.lastState = next
		t.agentTimestamps[agentID] = presence
	}
	return next, transition
}
