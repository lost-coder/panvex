package server

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/storage/sqlite"
	"github.com/lost-coder/panvex/internal/gatewayrpc"
	"github.com/lost-coder/panvex/internal/security"
)

// TestRestoreStoredTelemetry_BulkRehydrationMatchesPerAgent locks in the
// P3-3.1 cold-start rehydration: runtime (DCs, upstreams, and every recent
// event) is persisted as one runtime_json blob, so a panel restart restores
// each agent's runtime BYTE-FOR-BYTE — no per-projection tables, no 10-event
// cap. It seeds two agents (one with 15 recent events), restarts the panel
// over the same store, and asserts each restored runtime equals the live one.
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

	// Blob round-trip: the restored runtime must equal the live one for
	// each agent. Agent A keeps all 15 events (no cap), agent B keeps its
	// one — and neither leaks into the other. Compare via canonical JSON
	// (time.Time internal representation differs between wall/ext, so
	// reflect.DeepEqual would false-positive).
	assertRuntimeJSONEqual(t, "agent A", gotA, wantA)
	assertRuntimeJSONEqual(t, "agent B", gotB, wantB)
	if len(gotA.RecentEvents) != 15 {
		t.Fatalf("restored agent A events = %d, want 15 (blob keeps the full set)", len(gotA.RecentEvents))
	}
}

func assertRuntimeJSONEqual(t *testing.T, label string, got, want AgentRuntime) {
	t.Helper()
	gotJSON, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("%s: marshal got: %v", label, err)
	}
	wantJSON, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("%s: marshal want: %v", label, err)
	}
	if string(gotJSON) != string(wantJSON) {
		t.Fatalf("%s: restored runtime != live runtime\n got: %s\nwant: %s", label, gotJSON, wantJSON)
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
		CSRPEM:   testCSRPEM(t),
	}, now.Add(10*time.Second))
	if err != nil {
		t.Fatalf("enrollAgent(%s) error = %v", nodeName, err)
	}
	return identity.AgentID
}
