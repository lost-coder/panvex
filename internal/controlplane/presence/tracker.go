package presence

import (
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
}

// Tracker evaluates agent liveness from connect and heartbeat timestamps.
type Tracker struct {
	mu              sync.RWMutex
	degradedAfter   time.Duration
	offlineAfter    time.Duration
	agentTimestamps map[string]agentPresence
}

// NewTracker constructs a presence tracker using degraded and offline thresholds.
func NewTracker(degradedAfter time.Duration, offlineAfter time.Duration) *Tracker {
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

// TrackedCount returns the number of agents currently tracked (have an entry
// in the presence map). Used by the metrics subsystem to expose
// panvex_agent_connected without reaching into server-internal state.
func (t *Tracker) TrackedCount() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return len(t.agentTimestamps)
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
func (t *Tracker) Evaluate(agentID string, now time.Time) State {
	t.mu.RLock()
	presence, ok := t.agentTimestamps[agentID]
	t.mu.RUnlock()
	if !ok {
		return StateOffline
	}

	idle := now.UTC().Sub(presence.lastSeenAt)
	if idle >= t.offlineAfter {
		return StateOffline
	}

	if idle >= t.degradedAfter {
		return StateDegraded
	}

	return StateOnline
}
