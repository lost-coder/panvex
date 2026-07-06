package server

import (
	"strings"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/presence"
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
	// P3-3.1: reachability now round-trips through the runtime_json blob.
	// Build the record the way the snapshot path does (marshal the whole
	// AgentRuntime) and confirm restore preserves the fields.
	healthy := runtimeCurrentRecordFromAgent(Agent{ID: "agent-1", Runtime: AgentRuntime{
		UseMiddleProxy: true,
		MERuntimeReady: true,
		UpdatedAt:      time.Unix(1700000000, 0).UTC(),
	}})
	out := runtimeFromCurrentRecord(healthy)
	if out.TelemtUnreachable {
		t.Fatal("healthy record: TelemtUnreachable = true, want false")
	}

	unreachable := runtimeCurrentRecordFromAgent(Agent{ID: "agent-1", Runtime: AgentRuntime{
		TelemtUnreachable:          true,
		TelemtUnreachableSinceUnix: 1699999970,
		UpdatedAt:                  time.Unix(1700000000, 0).UTC(),
	}})
	out = runtimeFromCurrentRecord(unreachable)
	if !out.TelemtUnreachable {
		t.Fatal("unreachable record: TelemtUnreachable = false, want true")
	}
	if out.TelemtUnreachableSinceUnix != 1699999970 {
		t.Fatalf("TelemtUnreachableSinceUnix = %d, want 1699999970", out.TelemtUnreachableSinceUnix)
	}
}

// TestTelemetryWriteUnitCarriesForwardHashGatedDiagnostics guards D5
// panel-side: a hash-only (body-less) diagnostics / security-inventory
// snapshot must NOT overwrite the stored row; a blank record with an EMPTY
// hash keeps the historical overwrite.
func TestTelemetryWriteUnitCarriesForwardHashGatedDiagnostics(t *testing.T) {
	agent := Agent{ID: "agent-1"}
	observedAt := time.Date(2026, time.June, 9, 12, 0, 0, 0, time.UTC)
	base := agentSnapshot{
		AgentID:    "agent-1",
		ObservedAt: observedAt,
		HasRuntime: true,
		Runtime:    &gatewayrpc.RuntimeSnapshot{},
	}

	gated := base
	gated.RuntimeDiagnostics = &gatewayrpc.RuntimeDiagnosticsSnapshot{ContentHash: "abc"}
	gated.RuntimeSecurityInventory = &gatewayrpc.RuntimeSecurityInventorySnapshot{ContentHash: "def"}
	unit := telemetryWriteUnitForRuntime(agent, gated)
	if unit.Diagnostics != nil {
		t.Fatal("hash-only diagnostics must not overwrite the stored row")
	}
	if unit.Security != nil {
		t.Fatal("hash-only security inventory must not overwrite the stored row")
	}

	full := base
	full.RuntimeDiagnostics = &gatewayrpc.RuntimeDiagnosticsSnapshot{
		ContentHash: "abc", State: "ok", SystemInfoJson: `{"cpu":4}`,
	}
	full.RuntimeSecurityInventory = &gatewayrpc.RuntimeSecurityInventorySnapshot{
		ContentHash: "def", State: "ok", Enabled: true, EntriesJson: `[]`,
	}
	unit = telemetryWriteUnitForRuntime(agent, full)
	if unit.Diagnostics == nil || unit.Diagnostics.SystemInfoJSON != `{"cpu":4}` {
		t.Fatalf("full-body diagnostics must persist, got %+v", unit.Diagnostics)
	}
	if unit.Security == nil || !unit.Security.Enabled {
		t.Fatalf("full-body security inventory must persist, got %+v", unit.Security)
	}

	blank := base
	blank.RuntimeDiagnostics = &gatewayrpc.RuntimeDiagnosticsSnapshot{}
	blank.RuntimeSecurityInventory = &gatewayrpc.RuntimeSecurityInventorySnapshot{}
	unit = telemetryWriteUnitForRuntime(agent, blank)
	if unit.Diagnostics == nil || unit.Security == nil {
		t.Fatal("empty-hash blank record must keep the historical overwrite semantics")
	}
}
