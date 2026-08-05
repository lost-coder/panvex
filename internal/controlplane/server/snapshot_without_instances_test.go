package server

import (
	"context"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/gateway"
	"github.com/lost-coder/panvex/internal/gatewayrpc"
)

// TestApplyAgentSnapshotWithoutInstancesPreservesInstances guards the
// usage/IP snapshot path: those snapshots are built from the agent's
// baseSnapshot, which carries neither Instances nor Partial. Treating them
// as a full instance report wipes the agent's instance set — and with it the
// observed managed config, so the config editor loses `observed` and drift
// is stuck at "unknown" until the Telemt config happens to change.
func TestApplyAgentSnapshotWithoutInstancesPreservesInstances(t *testing.T) {
	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	server := mustNew(t, Options{
		LoginTimingFloor: -1,
		Now:              func() time.Time { return now },
	})

	runtimeSnapshot := gateway.AgentSnapshot{
		AgentID: "agent-1",
		Snap: &gatewayrpc.Snapshot{
			NodeName: "node-a",
			Version:  "3.4.25",
			Instances: []*gatewayrpc.InstanceSnapshot{{
				Id:                "telemt-primary",
				Name:              "telemt-primary",
				Version:           "3.4.25",
				Connections:       27,
				ManagedConfigHash: "hash-1",
				ManagedConfigJson: `{"general":{"log_level":"silent"}}`,
			}},
			Runtime: &gatewayrpc.RuntimeSnapshot{UptimeSeconds: 1000},
		},
		ObservedAt: now,
	}
	if err := server.applyAgentSnapshot(context.Background(), runtimeSnapshot); err != nil {
		t.Fatalf("applyAgentSnapshot(runtime) error = %v", err)
	}

	// A usage snapshot: not partial, and it simply does not report instances.
	usageSnapshot := gateway.AgentSnapshot{
		AgentID: "agent-1",
		Snap: &gatewayrpc.Snapshot{
			NodeName:  "node-a",
			Instances: nil,
			Partial:   false,
		},
		ObservedAt: now.Add(15 * time.Second),
	}
	if err := server.applyAgentSnapshot(context.Background(), usageSnapshot); err != nil {
		t.Fatalf("applyAgentSnapshot(usage) error = %v", err)
	}

	instances := server.live.InstancesForAgent("agent-1")
	if len(instances) != 1 {
		t.Fatalf("instances after usage snapshot = %d, want 1 preserved", len(instances))
	}
	if got := instances[0].ManagedConfigJSON; got != `{"general":{"log_level":"silent"}}` {
		t.Fatalf("observed config after usage snapshot = %q, want preserved", got)
	}

	observed, hasObserved := server.observedConfigForAgent("agent-1")
	if !hasObserved {
		t.Fatal("observedConfigForAgent reports no observed config after a usage snapshot")
	}
	if _, ok := observed["general"]; !ok {
		t.Fatalf("observed config lost its sections: %+v", observed)
	}
}
