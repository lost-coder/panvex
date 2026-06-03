package server

import "testing"

// TestLiveStoreCloneIsolation guards the most important correctness property
// of the A2 migration: the clone funcs (cloneAgentForMirror /
// cloneInstanceForMirror) must deep-copy every reference-type field so a
// handler mutating a value the live store returned cannot corrupt the mirror
// or race a concurrent ApplySnapshot.
func TestLiveStoreCloneIsolation(t *testing.T) {
	server := mustNew(t, Options{LoginTimingFloor: -1})

	entered := int64(123)
	agent := Agent{
		ID:           "agent-iso",
		NodeName:     "node-iso",
		FleetGroupID: "default",
		Runtime: AgentRuntime{
			DCs:                      []RuntimeDC{{DC: 1, AvailablePct: 50}},
			Upstreams:                []RuntimeUpstream{{UpstreamID: 7, Scopes: []string{"a", "b"}}},
			RecentEvents:             []RuntimeEvent{{Sequence: 1, EventType: "boot"}},
			ConnectionsBadByClass:    []ConnectionClassCount{{Class: "x", Total: 3}},
			HandshakeFailuresByClass: []ConnectionClassCount{{Class: "y", Total: 4}},
			FallbackEnteredAtUnix:    &entered,
			MeWritersSummary:         &RuntimeMeWritersSummary{AliveWriters: 2},
		},
	}
	instances := []Instance{{ID: "inst-iso", AgentID: "agent-iso", Connections: 9}}
	server.live.ApplySnapshot(agent.ID, agent, instances)

	// Mutating the caller-owned arguments after the call must not affect the
	// mirror (ApplySnapshot deep-cloned on the way in).
	agent.Runtime.DCs[0].AvailablePct = 999
	agent.Runtime.Upstreams[0].Scopes[0] = "MUTATED"
	*agent.Runtime.FallbackEnteredAtUnix = 999
	agent.Runtime.MeWritersSummary.AliveWriters = 999
	instances[0].Connections = 999

	got, ok := server.live.Get("agent-iso")
	if !ok {
		t.Fatal("agent-iso missing from live store")
	}
	if got.Runtime.DCs[0].AvailablePct != 50 {
		t.Fatalf("DCs aliased: got AvailablePct=%v want 50", got.Runtime.DCs[0].AvailablePct)
	}
	if got.Runtime.Upstreams[0].Scopes[0] != "a" {
		t.Fatalf("Upstream Scopes aliased: got %q want a", got.Runtime.Upstreams[0].Scopes[0])
	}
	if *got.Runtime.FallbackEnteredAtUnix != 123 {
		t.Fatalf("FallbackEnteredAtUnix aliased: got %d want 123", *got.Runtime.FallbackEnteredAtUnix)
	}
	if got.Runtime.MeWritersSummary.AliveWriters != 2 {
		t.Fatalf("MeWritersSummary aliased: got %d want 2", got.Runtime.MeWritersSummary.AliveWriters)
	}

	// Now mutate the RETURNED copy and confirm a fresh Get is unaffected.
	got.Runtime.DCs[0].AvailablePct = 777
	got.Runtime.Upstreams[0].Scopes[0] = "AGAIN"
	regot, _ := server.live.Get("agent-iso")
	if regot.Runtime.DCs[0].AvailablePct != 50 {
		t.Fatalf("returned-copy mutation leaked into mirror: DCs AvailablePct=%v", regot.Runtime.DCs[0].AvailablePct)
	}
	if regot.Runtime.Upstreams[0].Scopes[0] != "a" {
		t.Fatalf("returned-copy mutation leaked into mirror: Scopes=%q", regot.Runtime.Upstreams[0].Scopes[0])
	}

	insts := server.live.InstancesForAgent("agent-iso")
	if len(insts) != 1 || insts[0].Connections != 9 {
		t.Fatalf("instances aliased or lost: %+v", insts)
	}
}
