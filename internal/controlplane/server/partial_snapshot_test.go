package server

import (
	"context"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/gatewayrpc"
)

// TestApplyAgentSnapshotPartialPreservesLastKnown guards IN-H6: a partial
// snapshot (a telemt sub-endpoint failed mid-cycle) must not overwrite the
// last-known version / connections / uptime with blanks, which would
// flap the dashboard to zeros during a transient outage.
func TestApplyAgentSnapshotPartialPreservesLastKnown(t *testing.T) {
	now := time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC)
	server := mustNew(t, Options{
		LoginTimingFloor: -1,
		Now:              func() time.Time { return now },
	})

	// Full snapshot establishes the baseline.
	full := agentSnapshot{
		AgentID: "agent-1",
		Snap: &gatewayrpc.Snapshot{
			NodeName: "node-a",
			Version:  "2026.03",
			ReadOnly: true,
			Instances: []*gatewayrpc.InstanceSnapshot{
				{Id: "telemt-primary", Name: "telemt-primary", Version: "2026.03", Connections: 42, ReadOnly: true},
			},
			Runtime: &gatewayrpc.RuntimeSnapshot{UptimeSeconds: 1000},
		},
		ObservedAt: now,
	}
	if err := server.applyAgentSnapshot(context.Background(), full); err != nil {
		t.Fatalf("applyAgentSnapshot(full) error = %v", err)
	}

	// Partial snapshot with blanked version / instances / uptime.
	partial := agentSnapshot{
		AgentID: "agent-1",
		Snap: &gatewayrpc.Snapshot{
			Version:   "",
			ReadOnly:  false,
			Instances: nil,
			Runtime:   &gatewayrpc.RuntimeSnapshot{UptimeSeconds: 0},
			Partial:   true,
		},
		ObservedAt: now.Add(time.Minute),
	}
	if err := server.applyAgentSnapshot(context.Background(), partial); err != nil {
		t.Fatalf("applyAgentSnapshot(partial) error = %v", err)
	}

	agent := server.liveAgent("agent-1")
	if agent.Version != "2026.03" {
		t.Fatalf("version after partial = %q, want preserved 2026.03", agent.Version)
	}
	if !agent.ReadOnly {
		t.Fatal("read_only after partial = false, want preserved true")
	}
	if agent.Runtime.UptimeSeconds != 1000 {
		t.Fatalf("uptime after partial = %v, want preserved 1000", agent.Runtime.UptimeSeconds)
	}
	insts := server.live.InstancesForAgent("agent-1")
	if len(insts) != 1 || insts[0].Connections != 42 || insts[0].Version != "2026.03" {
		t.Fatalf("instances after partial = %+v, want preserved [version=2026.03 connections=42]", insts)
	}
}
