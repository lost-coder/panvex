package server

import (
	"strings"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/presence"
	"github.com/lost-coder/panvex/internal/controlplane/storage"
	"github.com/lost-coder/panvex/internal/gatewayrpc"
)

func TestAgentRuntimeFromSnapshotPropagatesTelemtUnreachable(t *testing.T) {
	snap := &gatewayrpc.RuntimeSnapshot{
		TelemtUnreachable:          true,
		TelemtUnreachableSinceUnix: 1700000000,
	}
	out := agentRuntimeFromSnapshot(snap, time.Unix(1700000050, 0))
	if !out.TelemtUnreachable {
		t.Fatal("TelemtUnreachable = false, want true")
	}
	if out.TelemtUnreachableSinceUnix != 1700000000 {
		t.Fatalf("TelemtUnreachableSinceUnix = %d, want 1700000000", out.TelemtUnreachableSinceUnix)
	}
}

func TestAgentRuntimeFromSnapshotPassesThroughHealthyDefault(t *testing.T) {
	snap := &gatewayrpc.RuntimeSnapshot{
		UseMiddleProxy: true,
		MeRuntimeReady: true,
		// TelemtUnreachable left at proto3 default (false) = healthy.
	}
	out := agentRuntimeFromSnapshot(snap, time.Unix(1700000050, 0))
	if out.TelemtUnreachable {
		t.Fatal("TelemtUnreachable = true, want false (passthrough of healthy default)")
	}
	if out.TelemtUnreachableSinceUnix != 0 {
		t.Fatalf("TelemtUnreachableSinceUnix = %d, want 0", out.TelemtUnreachableSinceUnix)
	}
}

// TestServerSeverityCriticalWhenTelemtUnreachable exercises the production
// projection path (telemetrySeverityAndReason) end-to-end. It fails if either
// production site in agent_snapshot.go or telemetry_runtime.go drops the
// TelemtUnreachable field instead of forwarding it from agent.Runtime.
func TestServerSeverityCriticalWhenTelemtUnreachable(t *testing.T) {
	now := time.Unix(1700000100, 0)
	srv := testServerWithSQLite(t, now)

	agent := Agent{
		ID:       "test-agent-unreachable",
		NodeName: "test-node",
		Runtime: AgentRuntime{
			// Telemt is down; the agent is otherwise online with a recent runtime report.
			TelemtUnreachable:          true,
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

// TestRuntimeFromCurrentRecordPropagatesTelemtUnreachable guards the
// cold-start path: when the panel rebuilds in-memory AgentRuntime from a
// persisted TelemetryRuntimeCurrentRecord (after restart, or whenever the
// list endpoint re-hydrates from storage), the reachability fields must
// survive. Without this, a panel restart would silently mis-classify an
// unreachable agent as healthy until a fresh snapshot arrived.
func TestRuntimeFromCurrentRecordPropagatesTelemtUnreachable(t *testing.T) {
	rec := storage.TelemetryRuntimeCurrentRecord{
		AgentID:        "agent-1",
		ObservedAt:     time.Unix(1700000000, 0).UTC(),
		UseMiddleProxy: true,
		MERuntimeReady: true,
		// Healthy record: TelemtUnreachable left at default (false).
	}
	out := runtimeFromCurrentRecord(rec)
	if out.TelemtUnreachable {
		t.Fatal("healthy record: TelemtUnreachable = true, want false")
	}

	rec.TelemtUnreachable = true
	rec.TelemtUnreachableSinceUnix = 1699999970
	out = runtimeFromCurrentRecord(rec)
	if !out.TelemtUnreachable {
		t.Fatal("unreachable record: TelemtUnreachable = false, want true")
	}
	if out.TelemtUnreachableSinceUnix != 1699999970 {
		t.Fatalf("TelemtUnreachableSinceUnix = %d, want 1699999970", out.TelemtUnreachableSinceUnix)
	}
}
