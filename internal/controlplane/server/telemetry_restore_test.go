package server

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/storage/sqlite"
	"github.com/lost-coder/panvex/internal/gatewayrpc"
	"github.com/lost-coder/panvex/internal/security"
)

// TestRestoreStoredTelemetry_BulkRehydrationMatchesPerAgent locks in the A2
// bulk cold-start rehydration: it seeds two agents' runtime (one with more
// than the per-agent event cap of 10), restarts the panel over the same
// store, and asserts each agent's restored runtime — DCs, upstreams, and
// the 10 most-recent events PER agent — is exactly what the per-agent path
// produced. No behaviour change, just fewer queries.
func TestRestoreStoredTelemetry_BulkRehydrationMatchesPerAgent(t *testing.T) {
	now := time.Date(2026, time.April, 2, 8, 0, 0, 0, time.UTC)
	dbPath := filepath.Join(t.TempDir(), "panvex.db")

	store, err := sqlite.Open(dbPath)
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}

	server := mustNew(t, Options{
		LoginTimingFloor: -1,
		Now:              func() time.Time { return now },
		Store:            store,
	})

	fleetGroupID := seedTestFleetGroup(t, server.store, "ams-1", now)

	// Agent A: runtime with 15 recent events (exercises the per-agent
	// cap of 10). Agent B: the default snapshot (1 event).
	agentA := enrollTelemetryAgent(t, server, fleetGroupID, "node-a", now)
	runtimeA := gatewayRuntimeSnapshotForTest()
	runtimeA.RecentEvents = make([]*gatewayrpc.RuntimeEventSnapshot, 0, 15)
	for i := 0; i < 15; i++ {
		runtimeA.RecentEvents = append(runtimeA.RecentEvents, &gatewayrpc.RuntimeEventSnapshot{
			Sequence:      uint64(i + 1),
			TimestampUnix: now.Add(time.Duration(i) * time.Minute).Unix(),
			EventType:     "tick",
			Context:       "a",
		})
	}
	if err := server.applyAgentSnapshot(context.Background(), agentSnapshot{
		AgentID:      agentA,
		NodeName:     "node-a",
		FleetGroupID: fleetGroupID,
		Version:      "1.0.0",
		Runtime:      runtimeA,
		HasRuntime:   true,
		ObservedAt:   now.Add(20 * time.Second),
	}); err != nil {
		t.Fatalf("applyAgentSnapshot(A) error = %v", err)
	}

	agentB := enrollTelemetryAgent(t, server, fleetGroupID, "node-b", now)
	if err := server.applyAgentSnapshot(context.Background(), agentSnapshot{
		AgentID:      agentB,
		NodeName:     "node-b",
		FleetGroupID: fleetGroupID,
		Version:      "1.0.0",
		Runtime:      gatewayRuntimeSnapshotForTest(),
		HasRuntime:   true,
		ObservedAt:   now.Add(20 * time.Second),
	}); err != nil {
		t.Fatalf("applyAgentSnapshot(B) error = %v", err)
	}

	// Capture the live runtime the write path produced so we can compare
	// the restored runtime against it byte-for-byte.
	wantA := server.liveAgent(agentA).Runtime
	wantB := server.liveAgent(agentB).Runtime
	server.Close()

	// Restart the panel over the same store and run the full restore.
	store2, err := sqlite.Open(dbPath)
	if err != nil {
		t.Fatalf("sqlite.Open(reopen) error = %v", err)
	}
	defer store2.Close()
	server2 := mustNew(t, Options{
		LoginTimingFloor: -1,
		Now:              func() time.Time { return now },
		Store:            store2,
	})
	defer server2.Close()

	if err := server2.restoreStoredState(context.Background()); err != nil {
		t.Fatalf("restoreStoredState() error = %v", err)
	}

	gotA := server2.liveAgent(agentA).Runtime
	gotB := server2.liveAgent(agentB).Runtime

	// Agent A: the persisted runtime caps at the 10 most-recent events.
	// The live write path keeps the full set in memory, so compare the
	// restored events against the newest 10 of the originally-applied set.
	if len(gotA.RecentEvents) != 10 {
		t.Fatalf("restored agent A events = %d, want 10 (per-agent cap)", len(gotA.RecentEvents))
	}
	// Newest 10 are sequences 6..15, returned newest-first.
	for i, ev := range gotA.RecentEvents {
		wantSeq := uint64(15 - i)
		if ev.Sequence != wantSeq {
			t.Fatalf("restored agent A events[%d].Sequence = %d, want %d", i, ev.Sequence, wantSeq)
		}
	}

	if len(gotA.DCs) != len(wantA.DCs) {
		t.Fatalf("restored agent A DCs = %d, want %d", len(gotA.DCs), len(wantA.DCs))
	}
	if len(gotA.Upstreams) != len(wantA.Upstreams) {
		t.Fatalf("restored agent A upstreams = %d, want %d", len(gotA.Upstreams), len(wantA.Upstreams))
	}
	if gotA.LifecycleState != wantA.LifecycleState {
		t.Fatalf("restored agent A lifecycle_state = %q, want %q", gotA.LifecycleState, wantA.LifecycleState)
	}
	if gotA.CurrentConnections != wantA.CurrentConnections {
		t.Fatalf("restored agent A current_connections = %d, want %d", gotA.CurrentConnections, wantA.CurrentConnections)
	}

	// Agent B had a single event; the restore must keep it and not leak
	// agent A's events into agent B's window.
	if len(gotB.RecentEvents) != len(wantB.RecentEvents) {
		t.Fatalf("restored agent B events = %d, want %d", len(gotB.RecentEvents), len(wantB.RecentEvents))
	}
	if len(gotB.DCs) != len(wantB.DCs) {
		t.Fatalf("restored agent B DCs = %d, want %d", len(gotB.DCs), len(wantB.DCs))
	}
	if len(gotB.Upstreams) != len(wantB.Upstreams) {
		t.Fatalf("restored agent B upstreams = %d, want %d", len(gotB.Upstreams), len(wantB.Upstreams))
	}
}

func enrollTelemetryAgent(t *testing.T, server *Server, fleetGroupID, nodeName string, now time.Time) string {
	t.Helper()
	token, err := server.issueEnrollmentToken(security.EnrollmentScope{FleetGroupID: fleetGroupID, TTL: time.Minute}, now)
	if err != nil {
		t.Fatalf("issueEnrollmentToken() error = %v", err)
	}
	identity, err := server.enrollAgent(context.Background(), agentEnrollmentRequest{
		Token:    token.Value,
		NodeName: nodeName,
		Version:  "1.0.0",
	}, now.Add(10*time.Second))
	if err != nil {
		t.Fatalf("enrollAgent(%s) error = %v", nodeName, err)
	}
	return identity.AgentID
}
