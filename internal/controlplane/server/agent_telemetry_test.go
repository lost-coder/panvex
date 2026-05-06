package server

import (
	"strings"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/presence"
	"github.com/lost-coder/panvex/internal/gatewayrpc"
)

func TestAgentRuntimeFromSnapshotPropagatesTelemtReachability(t *testing.T) {
	snap := &gatewayrpc.RuntimeSnapshot{
		TelemtReachable:            false,
		TelemtUnreachableSinceUnix: 1700000000,
	}
	out := agentRuntimeFromSnapshot(snap, time.Unix(1700000050, 0))
	if out.TelemtReachable {
		t.Fatal("TelemtReachable = true, want false")
	}
	if out.TelemtUnreachableSinceUnix != 1700000000 {
		t.Fatalf("TelemtUnreachableSinceUnix = %d, want 1700000000", out.TelemtUnreachableSinceUnix)
	}
}

func TestAgentRuntimeFromSnapshotPassesThroughReachableTrue(t *testing.T) {
	snap := &gatewayrpc.RuntimeSnapshot{
		UseMiddleProxy:             true,
		MeRuntimeReady:             true,
		TelemtReachable:            true,
		TelemtUnreachableSinceUnix: 0,
	}
	out := agentRuntimeFromSnapshot(snap, time.Unix(1700000050, 0))
	if !out.TelemtReachable {
		t.Fatal("TelemtReachable = false, want true (passthrough)")
	}
	if out.TelemtUnreachableSinceUnix != 0 {
		t.Fatalf("TelemtUnreachableSinceUnix = %d, want 0", out.TelemtUnreachableSinceUnix)
	}
}

// TestServerSeverityCriticalWhenTelemtUnreachable exercises the production
// projection path (telemetrySeverityAndReason) end-to-end. It fails if either
// production site in agent_snapshot.go or telemetry_runtime.go still
// hardcodes TelemtReachable: true instead of forwarding the field from
// agent.Runtime.
func TestServerSeverityCriticalWhenTelemtUnreachable(t *testing.T) {
	now := time.Unix(1700000100, 0)
	srv := testServerWithSQLite(t, now)

	agent := Agent{
		ID:       "test-agent-unreachable",
		NodeName: "test-node",
		Runtime: AgentRuntime{
			// Telemt is down; the agent is otherwise online with a recent runtime report.
			TelemtReachable:            false,
			TelemtUnreachableSinceUnix: 1700000000,
			UpdatedAt:                  now.Add(-5 * time.Second),
		},
	}

	freshness := telemetryFreshnessForRuntime(agent.Runtime, now)
	severity, reason := srv.telemetrySeverityAndReason(
		agent,
		presence.StateOnline,
		freshness,
		time.Time{}, // no fallback active
		now,
	)

	if severity != "critical" {
		t.Errorf("severity = %q, want %q", severity, "critical")
	}
	if !strings.Contains(reason, "Telemt API unreachable since") {
		t.Errorf("reason = %q, want it to contain %q", reason, "Telemt API unreachable since")
	}
}
