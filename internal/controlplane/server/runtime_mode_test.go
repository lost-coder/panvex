package server

import (
	"testing"
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

// NOTE: the storage-restore lifecycle-classification tests
// (runtimeLifecycleStateFromCurrent) were removed with P3-3.1: restore no
// longer recomputes lifecycle from narrow columns — LifecycleState is
// carried verbatim in the runtime_json blob. The Direct-vs-ME degraded
// distinction is enforced on the live snapshot path by
// normalizeAgentRuntime, covered by the two tests above.
