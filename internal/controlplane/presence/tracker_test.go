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

func TestTrackerHeartbeatRecoversDegradedAgent(t *testing.T) {
	now := time.Date(2026, time.March, 14, 8, 0, 0, 0, time.UTC)
	tracker := NewTracker(30*time.Second, 90*time.Second)

	tracker.MarkConnected("agent-1", now)
	tracker.Heartbeat("agent-1", now.Add(40*time.Second))

	if state := tracker.Evaluate("agent-1", now.Add(45*time.Second)); state != StateOnline {
		t.Fatalf("Evaluate() recovered state = %q, want %q", state, StateOnline)
	}
}
