package server

import (
	"testing"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

// TestNormalizeAgentRuntimeDropsMEArtifactsForDirect verifies the API
// projection clears ME-only flags when the node is operating in Direct
// mode. Telemt's /v1/runtime/initialization endpoint reflects the ME
// pool initialization state; Direct nodes have no ME pool so the
// "Degraded" flag is either permanently true (older Telemt builds) or
// semantically meaningless. Surfacing it as gates.degraded made every
// Direct node look "operating in degraded mode" in the dashboard.
func TestNormalizeAgentRuntimeDropsMEArtifactsForDirect(t *testing.T) {
	in := AgentRuntime{
		UseMiddleProxy: false,
		Degraded:       true,
		LifecycleState: "degraded",
	}
	got := normalizeAgentRuntime(in)
	if got.Degraded {
		t.Fatalf("normalizeAgentRuntime kept Degraded=true for Direct mode; want false")
	}
	if got.LifecycleState == "degraded" {
		t.Fatalf("normalizeAgentRuntime kept LifecycleState=%q for Direct mode; want non-degraded", got.LifecycleState)
	}
}

func TestNormalizeAgentRuntimePreservesDegradedForME(t *testing.T) {
	in := AgentRuntime{
		UseMiddleProxy: true,
		Degraded:       true,
		LifecycleState: "degraded",
	}
	got := normalizeAgentRuntime(in)
	if !got.Degraded {
		t.Fatalf("normalizeAgentRuntime cleared Degraded for ME mode; want preserved")
	}
	if got.LifecycleState != "degraded" {
		t.Fatalf("normalizeAgentRuntime changed LifecycleState=%q for ME mode; want 'degraded'", got.LifecycleState)
	}
}

// TestRuntimeLifecycleStateFromCurrentDirectIgnoresDegraded covers the
// storage-restore path: Telemt-reported Degraded must not classify a
// Direct-mode runtime as "degraded" because the source flag describes
// ME-pool init only.
func TestRuntimeLifecycleStateFromCurrentDirectIgnoresDegraded(t *testing.T) {
	rec := storage.TelemetryRuntimeCurrentRecord{
		UseMiddleProxy:          false,
		Degraded:                true,
		AcceptingNewConnections: true,
		MERuntimeReady:          false, // expected for Direct
		StartupStatus:           "ready",
		InitializationStatus:    "ready",
	}
	got := runtimeLifecycleStateFromCurrent(rec)
	if got == "degraded" {
		t.Fatalf("runtimeLifecycleStateFromCurrent = %q for Direct mode with Degraded=true; must not be 'degraded'", got)
	}
	if got != "ready" {
		t.Fatalf("runtimeLifecycleStateFromCurrent = %q; want 'ready'", got)
	}
}

func TestRuntimeLifecycleStateFromCurrentMEKeepsDegraded(t *testing.T) {
	rec := storage.TelemetryRuntimeCurrentRecord{
		UseMiddleProxy:          true,
		Degraded:                true,
		AcceptingNewConnections: true,
		MERuntimeReady:          true,
		StartupStatus:           "ready",
		InitializationStatus:    "ready",
	}
	got := runtimeLifecycleStateFromCurrent(rec)
	if got != "degraded" {
		t.Fatalf("runtimeLifecycleStateFromCurrent = %q for ME with Degraded=true; want 'degraded'", got)
	}
}
