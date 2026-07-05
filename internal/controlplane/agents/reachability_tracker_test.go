package agents

import "testing"

func TestReachabilityTrackerRecoveryEdge(t *testing.T) {
	tr := NewReachabilityTracker()

	// First observation of a healthy agent: not a recovery edge.
	if tr.Observe("a", false) {
		t.Fatal("first healthy observation must not be a recovery edge")
	}
	// Goes unreachable: not a recovery edge.
	if tr.Observe("a", true) {
		t.Fatal("healthy→unreachable must not be a recovery edge")
	}
	// Stays unreachable: still no edge.
	if tr.Observe("a", true) {
		t.Fatal("unreachable→unreachable must not be a recovery edge")
	}
	// Recovers: THIS is the recovery edge.
	if !tr.Observe("a", false) {
		t.Fatal("unreachable→reachable must be a recovery edge")
	}
	// Stays healthy: no further edge.
	if tr.Observe("a", false) {
		t.Fatal("reachable→reachable must not be a recovery edge")
	}
}

func TestReachabilityTrackerPerAgent(t *testing.T) {
	tr := NewReachabilityTracker()
	tr.Observe("a", true)
	tr.Observe("b", false)
	if !tr.Observe("a", false) {
		t.Fatal("agent a should report a recovery edge")
	}
	if tr.Observe("b", false) {
		t.Fatal("agent b never went unreachable; no edge")
	}
}

func TestReachabilityTrackerForgetResetsEdgeState(t *testing.T) {
	tr := NewReachabilityTracker()

	// Ordinary recovery edge: unreachable -> reachable reports recovered=true.
	if tr.Observe("a1", true) {
		t.Fatal("first observation must never be an edge")
	}
	if !tr.Observe("a1", false) {
		t.Fatal("unreachable -> reachable must report recovery")
	}

	// After Forget the agent is a clean slate: a stale unreachable flag must
	// not produce a false recovery edge on re-registration.
	if tr.Observe("a1", true) {
		t.Fatal("reachable -> unreachable is not a recovery edge")
	}
	tr.Forget("a1")
	if tr.Observe("a1", false) {
		t.Fatal("first observation after Forget must not be a recovery edge")
	}

	// Idempotent.
	tr.Forget("missing")
}
