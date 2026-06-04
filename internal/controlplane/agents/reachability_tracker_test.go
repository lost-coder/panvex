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
