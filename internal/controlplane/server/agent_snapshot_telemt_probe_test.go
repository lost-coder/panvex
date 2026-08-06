package server

import (
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/gateway"
	"github.com/lost-coder/panvex/internal/gatewayrpc"
)

// TestTelemtUpdateProbeFromSnapshotPresent asserts that a populated wire
// probe is converted field-for-field into the presentation type.
func TestTelemtUpdateProbeFromSnapshotPresent(t *testing.T) {
	in := &gatewayrpc.TelemtUpdateProbe{
		Mode:                 "binary",
		SuggestedRestartSpec: "systemd:telemt",
		BinaryPath:           "/usr/local/bin/telemt",
		Available:            true,
		Reason:               "",
	}
	out := telemtUpdateProbeFromSnapshot(in)
	if out == nil {
		t.Fatal("telemtUpdateProbeFromSnapshot() = nil, want populated probe")
	}
	if out.Mode != "binary" {
		t.Errorf("Mode = %q, want %q", out.Mode, "binary")
	}
	if out.SuggestedRestartSpec != "systemd:telemt" {
		t.Errorf("SuggestedRestartSpec = %q, want %q", out.SuggestedRestartSpec, "systemd:telemt")
	}
	if out.BinaryPath != "/usr/local/bin/telemt" {
		t.Errorf("BinaryPath = %q, want %q", out.BinaryPath, "/usr/local/bin/telemt")
	}
	if !out.Available {
		t.Error("Available = false, want true")
	}
	if out.Reason != "" {
		t.Errorf("Reason = %q, want empty", out.Reason)
	}
}

// TestTelemtUpdateProbeFromSnapshotUnavailableReason covers the "no in-place
// path" shape, where Available is false and Reason carries a localizable code.
func TestTelemtUpdateProbeFromSnapshotUnavailableReason(t *testing.T) {
	in := &gatewayrpc.TelemtUpdateProbe{
		Mode:      "docker",
		Available: false,
		Reason:    "docker_only",
	}
	out := telemtUpdateProbeFromSnapshot(in)
	if out == nil {
		t.Fatal("telemtUpdateProbeFromSnapshot() = nil, want populated probe")
	}
	if out.Mode != "docker" {
		t.Errorf("Mode = %q, want %q", out.Mode, "docker")
	}
	if out.Available {
		t.Error("Available = true, want false")
	}
	if out.Reason != "docker_only" {
		t.Errorf("Reason = %q, want %q", out.Reason, "docker_only")
	}
}

// TestTelemtUpdateProbeFromSnapshotNil asserts that a nil wire probe (agents
// that predate the probe, or an outright missing field) converts to a nil
// presentation value rather than a zero-valued struct — the panel must be
// able to tell "unknown" apart from "probed and empty".
func TestTelemtUpdateProbeFromSnapshotNil(t *testing.T) {
	if out := telemtUpdateProbeFromSnapshot(nil); out != nil {
		t.Fatalf("telemtUpdateProbeFromSnapshot(nil) = %+v, want nil", out)
	}
}

// TestUpdateAgentRecordFromSnapshotPropagatesTelemtUpdateProbe exercises the
// production call site end-to-end: it fails if updateAgentRecordFromSnapshot
// stops forwarding the probe from the wire snapshot into the live Agent view.
func TestUpdateAgentRecordFromSnapshotPropagatesTelemtUpdateProbe(t *testing.T) {
	now := time.Date(2026, time.August, 6, 12, 0, 0, 0, time.UTC)
	srv := testServerWithSQLite(t, now)

	snapshot := gateway.AgentSnapshot{
		AgentID: "test-agent-probe",
		Snap: &gatewayrpc.Snapshot{
			NodeName: "node-probe",
			Version:  "1.0.0",
			TelemtUpdateProbe: &gatewayrpc.TelemtUpdateProbe{
				Mode:                 "binary",
				SuggestedRestartSpec: "openrc:telemt",
				BinaryPath:           "/opt/telemt/telemt",
				Available:            true,
			},
		},
		ObservedAt: now,
	}

	srv.mu.Lock()
	agent := srv.updateAgentRecordFromSnapshot(snapshot)
	srv.mu.Unlock()

	if agent.TelemtUpdateProbe == nil {
		t.Fatal("agent.TelemtUpdateProbe = nil, want populated probe")
	}
	if agent.TelemtUpdateProbe.Mode != "binary" {
		t.Errorf("Mode = %q, want %q", agent.TelemtUpdateProbe.Mode, "binary")
	}
	if agent.TelemtUpdateProbe.SuggestedRestartSpec != "openrc:telemt" {
		t.Errorf("SuggestedRestartSpec = %q, want %q", agent.TelemtUpdateProbe.SuggestedRestartSpec, "openrc:telemt")
	}

	// Commit to the live store so the next call reads back this probe as
	// its "previous value" — otherwise the carry-forward check below would
	// pass trivially against a zero-value agent.
	srv.live.ApplySnapshot(snapshot.AgentID, agent, nil)

	// A subsequent snapshot without a probe (pre-probe agent, or a
	// transient wire drop) overwrites rather than carrying the old value
	// forward — TelemtUpdateProbe is always fully re-stamped, unlike
	// version/read_only under `partial`.
	snapshotNoProbe := gateway.AgentSnapshot{
		AgentID:    "test-agent-probe",
		Snap:       &gatewayrpc.Snapshot{NodeName: "node-probe", Version: "1.0.0"},
		ObservedAt: now,
	}
	srv.mu.Lock()
	agentAfter := srv.updateAgentRecordFromSnapshot(snapshotNoProbe)
	srv.mu.Unlock()

	if agentAfter.TelemtUpdateProbe != nil {
		t.Fatalf("agent.TelemtUpdateProbe = %+v, want nil", agentAfter.TelemtUpdateProbe)
	}
}
