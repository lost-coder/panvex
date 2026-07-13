package server

import (
	"context"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/presence"
	"github.com/lost-coder/panvex/internal/gatewayrpc"
)

// TestSnapshotClockSkewDoesNotAffectLiveness (P3-3.2, аудит #25b): снапшот
// с агентским ObservedAt на 10 минут в прошлом НЕ должен делать агента
// "stale"/"last seen 10 min ago" — presence, LastSeenAt и freshness-базис
// штампуются панельными часами в момент приёма; агентское время остаётся
// только в диагностическом Runtime.ReportedObservedAt.
func TestSnapshotClockSkewDoesNotAffectLiveness(t *testing.T) {
	now := time.Date(2026, time.July, 2, 12, 0, 0, 0, time.UTC)
	server := mustNew(t, Options{
		LoginTimingFloor: -1,
		Now:              func() time.Time { return now },
	})
	defer server.Close()

	skewed := now.Add(-10 * time.Minute)
	err := server.applyAgentSnapshot(context.Background(), agentSnapshot{
		AgentID: "agent-skew",
		Snap: &gatewayrpc.Snapshot{
			NodeName: "node-skew",
			Version:  "dev",
			Runtime:  &gatewayrpc.RuntimeSnapshot{AcceptingNewConnections: true},
		},
		ObservedAt: skewed,
	})
	if err != nil {
		t.Fatalf("applyAgentSnapshot() error = %v", err)
	}

	agent, ok := server.live.Get("agent-skew")
	if !ok {
		t.Fatal("agent not found in live store")
	}
	if !agent.LastSeenAt.Equal(now) {
		t.Errorf("LastSeenAt = %v, want panel clock %v", agent.LastSeenAt, now)
	}
	if !agent.Runtime.UpdatedAt.Equal(now) {
		t.Errorf("Runtime.UpdatedAt = %v, want panel clock %v", agent.Runtime.UpdatedAt, now)
	}
	if !agent.Runtime.ReportedObservedAt.Equal(skewed) {
		t.Errorf("Runtime.ReportedObservedAt = %v, want agent clock %v", agent.Runtime.ReportedObservedAt, skewed)
	}

	// Freshness считается от панельного UpdatedAt → "fresh", не "stale".
	freshness := telemetryFreshnessForRuntime(agent.Runtime, now)
	if freshness.State != "fresh" {
		t.Errorf("freshness.State = %q, want %q", freshness.State, "fresh")
	}

	// Presence уже штампуется панельными часами (M-5) — фиксируем
	// согласованность всех трёх величин в одном тесте.
	if state := server.presence.Evaluate("agent-skew", now); state != presence.StateOnline {
		t.Errorf("presence = %q, want %q", state, presence.StateOnline)
	}
}

// TestClampObservedAt (R9b Task 9): stored telemetry timestamps must not be
// dated in the future. Past and within-tolerance values pass through; an
// egregious future lead clamps to the panel now.
func TestClampObservedAt(t *testing.T) {
	now := time.Date(2026, time.July, 2, 12, 0, 0, 0, time.UTC)
	server := mustNew(t, Options{LoginTimingFloor: -1, Now: func() time.Time { return now }})
	defer server.Close()
	ctx := context.Background()

	past := now.Add(-10 * time.Minute)
	if got := server.clampObservedAt(ctx, "a", past); !got.Equal(past) {
		t.Errorf("past observed clamped: got %v, want %v", got, past)
	}
	small := now.Add(30 * time.Second)
	if got := server.clampObservedAt(ctx, "a", small); !got.Equal(small) {
		t.Errorf("within-tolerance future clamped: got %v, want %v", got, small)
	}
	future := now.Add(time.Hour)
	ceiling := now.Add(clockSkewTolerance)
	if got := server.clampObservedAt(ctx, "a", future); !got.Equal(ceiling) {
		t.Errorf("future observed not clamped: got %v, want %v", got, ceiling)
	}
}

// TestSnapshotFutureClockKeepsRawDiagnostic (R9b Task 9): a future-dated agent
// clock is clamped for stored history but the raw value is still preserved in
// Runtime.ReportedObservedAt for diagnostics, and liveness stays panel-stamped.
func TestSnapshotFutureClockKeepsRawDiagnostic(t *testing.T) {
	now := time.Date(2026, time.July, 2, 12, 0, 0, 0, time.UTC)
	server := mustNew(t, Options{LoginTimingFloor: -1, Now: func() time.Time { return now }})
	defer server.Close()

	future := now.Add(time.Hour)
	err := server.applyAgentSnapshot(context.Background(), agentSnapshot{
		AgentID: "agent-future",
		Snap: &gatewayrpc.Snapshot{
			NodeName: "node-future",
			Version:  "dev",
			Runtime:  &gatewayrpc.RuntimeSnapshot{AcceptingNewConnections: true},
		},
		ObservedAt: future,
	})
	if err != nil {
		t.Fatalf("applyAgentSnapshot() error = %v", err)
	}
	agent, ok := server.live.Get("agent-future")
	if !ok {
		t.Fatal("agent not found in live store")
	}
	if !agent.Runtime.ReportedObservedAt.Equal(future) {
		t.Errorf("ReportedObservedAt = %v, want raw future clock %v", agent.Runtime.ReportedObservedAt, future)
	}
	if !agent.LastSeenAt.Equal(now) {
		t.Errorf("LastSeenAt = %v, want panel clock %v", agent.LastSeenAt, now)
	}
}
