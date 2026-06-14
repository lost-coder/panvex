package agents

import "sync"

// ReachabilityTracker remembers the last-known Telemt reachability for each
// agent so the snapshot pipeline can detect the unreachable→reachable edge —
// the moment a node's Telemt API comes back — and trigger a fresh client
// re-discovery. It mirrors FallbackTracker: a small dedicated owner of one
// piece of per-agent fleet state, with its own lock that never calls back into
// the server, so the documented control-plane lock ordering is preserved.
type ReachabilityTracker struct {
	mu          sync.Mutex
	unreachable map[string]bool
}

// NewReachabilityTracker constructs an empty tracker.
func NewReachabilityTracker() *ReachabilityTracker {
	return &ReachabilityTracker{unreachable: make(map[string]bool)}
}

// Observe records the agent's current Telemt reachability and reports whether
// this observation is a recovery edge (previous=unreachable, now=reachable).
// The first observation of any agent is never an edge.
func (t *ReachabilityTracker) Observe(agentID string, unreachable bool) (recovered bool) {
	t.mu.Lock()
	prev := t.unreachable[agentID]
	t.unreachable[agentID] = unreachable
	t.mu.Unlock()
	return prev && !unreachable
}

// Forget drops any state for agentID (e.g. on deregistration). Idempotent.
func (t *ReachabilityTracker) Forget(agentID string) {
	t.mu.Lock()
	delete(t.unreachable, agentID)
	t.mu.Unlock()
}
