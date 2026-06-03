package agents

import (
	"sync"
	"time"
)

// FallbackTracker is the in-memory owner of the per-agent
// "fallback entered at" timestamp — the moment an agent entered
// ME->Direct fallback, used by telemetry severity escalation to measure
// how long the underlying ME outage has persisted.
//
// It is the service-side home for what the server formerly kept in
// s.fallbackEnteredAt (audit finding A2: a single owner for fleet state,
// off the Server struct). The DB durability path (agent_fallback_state
// rows, written via the server's batch writer) is unchanged; this tracker
// is the authoritative in-RAM copy on the single-instance panel.
//
// # Why a dedicated tracker (not LiveStore / Service)
//
// The timestamp is set ONCE on entry and preserved across subsequent
// snapshots while fallback is still active — it must survive the live
// store's replace-semantics snapshot overwrite, so it cannot live on the
// Agent value. It also has a different persistence path (the batch writer,
// not the identity Repository) than agents.Service, so a small dedicated
// owner keeps each concern's lock and lifecycle separate.
//
// The transition CLASSIFICATION (reading runtime flags to decide entry vs
// exit) and the batch-writer enqueue stay server-side; this tracker owns
// only the in-memory map mechanics.
//
// # Lock discipline
//
// FallbackTracker.mu protects the map. It owns its own mutex and never
// reaches into Server.mu (mirroring LiveStore / agents.Service /
// clients.Service), so the documented control-plane lock ordering is
// preserved: callers that already hold s.mu may call into the tracker
// (s.mu -> tracker), and the tracker never calls back.
//
// time.Time is a value type with no internal pointers that callers can use
// to mutate shared state, so no deep copy is needed on get/set.
type FallbackTracker struct {
	mu      sync.RWMutex
	entered map[string]time.Time
}

// NewFallbackTracker constructs an empty tracker. Call Restore to
// rehydrate it from persisted state at boot.
func NewFallbackTracker() *FallbackTracker {
	return &FallbackTracker{
		entered: make(map[string]time.Time),
	}
}

// Set records that agentID entered fallback at the given time.
func (t *FallbackTracker) Set(agentID string, at time.Time) {
	t.mu.Lock()
	t.entered[agentID] = at
	t.mu.Unlock()
}

// Clear removes any fallback timestamp for agentID. Idempotent: clearing
// an agent with no timestamp is a no-op.
func (t *FallbackTracker) Clear(agentID string) {
	t.mu.Lock()
	delete(t.entered, agentID)
	t.mu.Unlock()
}

// Get returns the fallback-entered-at timestamp for agentID and ok=true
// when the agent is currently in fallback, or the zero time and false
// otherwise.
func (t *FallbackTracker) Get(agentID string) (time.Time, bool) {
	t.mu.RLock()
	at, ok := t.entered[agentID]
	t.mu.RUnlock()
	return at, ok
}

// Restore replaces the tracker's contents with the supplied map. It is a
// full snapshot (not a merge), so subsequent calls overwrite prior state.
// The input map is copied; the caller retains ownership of its argument.
func (t *FallbackTracker) Restore(entered map[string]time.Time) {
	t.mu.Lock()
	t.entered = make(map[string]time.Time, len(entered))
	for id, at := range entered {
		t.entered[id] = at
	}
	t.mu.Unlock()
}
