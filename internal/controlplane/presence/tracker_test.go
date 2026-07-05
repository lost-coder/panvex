package presence

import (
	"testing"
	"time"
)

func TestTrackerEvaluateTransitionsFromOnlineToDegradedToOffline(t *testing.T) {
	now := time.Date(2026, time.March, 14, 8, 0, 0, 0, time.UTC)
	tracker := NewTracker(30*time.Second, 90*time.Second)

	tracker.MarkConnected("agent-1", now)

	if state := tracker.Evaluate("agent-1", now.Add(20*time.Second)); state != StateOnline {
		t.Fatalf("Evaluate() online state = %q, want %q", state, StateOnline)
	}

	if state := tracker.Evaluate("agent-1", now.Add(45*time.Second)); state != StateDegraded {
		t.Fatalf("Evaluate() degraded state = %q, want %q", state, StateDegraded)
	}

	if state := tracker.Evaluate("agent-1", now.Add(100*time.Second)); state != StateOffline {
		t.Fatalf("Evaluate() offline state = %q, want %q", state, StateOffline)
	}
}

// TestTrackerEvaluateAllCountsNonOfflineAgents is the L-8 regression: the
// connected gauge must reflect evaluated liveness, not the raw tracked
// count. An agent that stopped heartbeating past offlineAfter is still in
// the map (no deregistration) yet must NOT be counted as connected.
func TestTrackerEvaluateAllCountsNonOfflineAgents(t *testing.T) {
	base := time.Date(2026, time.March, 14, 8, 0, 0, 0, time.UTC)
	tracker := NewTracker(30*time.Second, 90*time.Second)

	tracker.MarkConnected("online", base)
	tracker.MarkConnected("degraded", base)
	tracker.MarkConnected("offline", base)

	now := base.Add(100 * time.Second)
	tracker.Heartbeat("online", now)                        // fresh → online
	tracker.Heartbeat("degraded", now.Add(-45*time.Second)) // 45s idle → degraded
	// "offline" never heartbeats again → 100s idle ≥ offlineAfter.

	// All three still have map entries; the naive TrackedCount would be 3.
	if got := tracker.TrackedCount(); got != 3 {
		t.Fatalf("TrackedCount() = %d, want 3 (all still tracked)", got)
	}

	connected := tracker.EvaluateAll(now)
	if connected != 2 {
		t.Fatalf("EvaluateAll() connected = %d, want 2 (online+degraded, offline excluded)", connected)
	}

	// The sweep must have driven the offline agent's state transition.
	if state := tracker.Evaluate("offline", now); state != StateOffline {
		t.Fatalf("offline agent state after sweep = %q, want %q", state, StateOffline)
	}
}

func TestTrackerHeartbeatRecoversDegradedAgent(t *testing.T) {
	now := time.Date(2026, time.March, 14, 8, 0, 0, 0, time.UTC)
	tracker := NewTracker(30*time.Second, 90*time.Second)

	tracker.MarkConnected("agent-1", now)
	tracker.Heartbeat("agent-1", now.Add(40*time.Second))

	if state := tracker.Evaluate("agent-1", now.Add(45*time.Second)); state != StateOnline {
		t.Fatalf("Evaluate() recovered state = %q, want %q", state, StateOnline)
	}
}

// Test-only accessor relocated from production in P5 (audit #18 §5.2).
//
// TrackedCount returns the number of agents currently tracked (have an entry
// in the presence map), regardless of their evaluated liveness. This is the
// raw map size; the panvex_agent_connected gauge instead uses EvaluateAll so
// stale-but-not-deregistered agents are excluded (L-8). Retained for
// diagnostics/tests.
func (t *Tracker) TrackedCount() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return len(t.agentTimestamps)
}
