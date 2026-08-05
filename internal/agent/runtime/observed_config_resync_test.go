package runtime

import (
	"context"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/agent/telemt"
)

// TestResetDeltaGatesResendsObservedConfig guards the panel-restart case: the
// observed managed config lives only in the panel's in-memory live store, while
// the agent's delta gate remembers the last reported hash for the lifetime of
// the process. After the panel restarts (or the stream is re-established), the
// agent must re-send the full config body on the first snapshot of the new
// session — otherwise the panel is stuck with an empty `observed` and a drift
// status of "unknown" until the Telemt config happens to change.
func TestResetDeltaGatesResendsObservedConfig(t *testing.T) {
	client := &fakeTelemtClient{
		managedConfig:   map[string]any{"general": map[string]any{"log_level": "silent"}},
		managedRevision: "rev-1",
		state: telemt.RuntimeState{
			Diagnostics: telemt.RuntimeDiagnostics{State: "ok", SystemInfoJSON: `{"cpu":4}`},
		},
	}
	agent := New(Config{AgentID: "agent-1", NodeName: "node-a"}, client)
	now := time.Date(2026, time.August, 5, 12, 0, 0, 0, time.UTC)

	first, err := agent.BuildRuntimeSnapshot(context.Background(), now)
	if err != nil {
		t.Fatalf("BuildRuntimeSnapshot(first) error = %v", err)
	}
	if first.Instances[0].ManagedConfigJson == "" {
		t.Fatal("first snapshot carries no managed config JSON, want the full body")
	}

	second, err := agent.BuildRuntimeSnapshot(context.Background(), now.Add(15*time.Second))
	if err != nil {
		t.Fatalf("BuildRuntimeSnapshot(second) error = %v", err)
	}
	if second.Instances[0].ManagedConfigJson != "" {
		t.Fatal("second snapshot re-sent the body, want delta-gated empty")
	}

	// A new session with the panel: the panel may have lost its live store.
	agent.ResetDeltaGates()

	third, err := agent.BuildRuntimeSnapshot(context.Background(), now.Add(30*time.Second))
	if err != nil {
		t.Fatalf("BuildRuntimeSnapshot(third) error = %v", err)
	}
	if third.Instances[0].ManagedConfigJson == "" {
		t.Fatal("snapshot after ResetDeltaGates carries no managed config JSON, want full body re-sent")
	}
	if third.RuntimeDiagnostics.GetSystemInfoJson() == "" {
		t.Fatal("diagnostics body not re-sent after ResetDeltaGates")
	}
}
