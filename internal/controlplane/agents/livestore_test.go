package agents

import (
	"sort"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/api"
)

// deepCopyAgent stands in for the server's cloneAgentForMirror. It clones the
// reference-type fields the isolation tests exercise — the Runtime.RecentEvents
// slice and the CertIssuedAt pointer — so the store's deep-copy guarantee is
// under test. The store only needs the caller's clone to be deep for fields a
// handler might mutate.
func deepCopyAgent(a api.Agent) api.Agent {
	out := a
	out.Runtime.RecentEvents = append([]api.RuntimeEvent(nil), a.Runtime.RecentEvents...)
	if a.CertIssuedAt != nil {
		t := *a.CertIssuedAt
		out.CertIssuedAt = &t
	}
	return out
}

// copyInstance is a plain struct copy: api.Instance has only scalar fields, so
// a value copy is already a deep copy. (This is why there is no instance
// deep-copy-isolation test below — the aliasing hazard the old generic test
// covered does not exist for the concrete presentation type.)
func copyInstance(i api.Instance) api.Instance { return i }

func newTestStore() *LiveStore {
	return NewLiveStore(
		deepCopyAgent,
		copyInstance,
		func(i api.Instance) string { return i.ID },
	)
}

func instanceIDs(insts []api.Instance) []string {
	out := make([]string, 0, len(insts))
	for _, i := range insts {
		out = append(out, i.ID)
	}
	sort.Strings(out)
	return out
}

func TestLiveStoreNewLiveStorePanicsOnNilFuncs(t *testing.T) {
	ok := func(i api.Instance) string { return "" }
	cases := []struct {
		name string
		mk   func() *LiveStore
	}{
		{"cloneAgent", func() *LiveStore {
			return NewLiveStore(nil, copyInstance, ok)
		}},
		{"cloneInstance", func() *LiveStore {
			return NewLiveStore(deepCopyAgent, nil, ok)
		}},
		{"instanceID", func() *LiveStore {
			return NewLiveStore(deepCopyAgent, copyInstance, nil)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatalf("NewLiveStore with nil %s did not panic", tc.name)
				}
			}()
			tc.mk()
		})
	}
}

func TestLiveStoreApplySnapshotStoresAgentAndInstances(t *testing.T) {
	s := newTestStore()
	s.ApplySnapshot("a1", api.Agent{ID: "a1", NodeName: "alpha"}, []api.Instance{
		{ID: "i1", AgentID: "a1"},
		{ID: "i2", AgentID: "a1"},
	})

	got, ok := s.Get("a1")
	if !ok || got.NodeName != "alpha" {
		t.Fatalf("Get(a1) = %+v ok=%v, want NodeName=alpha", got, ok)
	}
	if ids := instanceIDs(s.InstancesForAgent("a1")); len(ids) != 2 || ids[0] != "i1" || ids[1] != "i2" {
		t.Fatalf("InstancesForAgent(a1) = %v, want [i1 i2]", ids)
	}
	if s.Len() != 1 {
		t.Fatalf("Len = %d, want 1", s.Len())
	}
}

func TestLiveStoreApplySnapshotReplacesAndPrunesInstances(t *testing.T) {
	s := newTestStore()
	s.ApplySnapshot("a1", api.Agent{ID: "a1"}, []api.Instance{
		{ID: "i1", AgentID: "a1"},
		{ID: "i2", AgentID: "a1"},
		{ID: "i3", AgentID: "a1"},
	})
	// Second snapshot drops i2 and i3, adds i4. i2/i3 must be pruned.
	s.ApplySnapshot("a1", api.Agent{ID: "a1"}, []api.Instance{
		{ID: "i1", AgentID: "a1"},
		{ID: "i4", AgentID: "a1"},
	})
	if ids := instanceIDs(s.InstancesForAgent("a1")); len(ids) != 2 || ids[0] != "i1" || ids[1] != "i4" {
		t.Fatalf("after replace, InstancesForAgent(a1) = %v, want [i1 i4]", ids)
	}
}

func TestLiveStoreApplySnapshotDoesNotPruneOtherAgents(t *testing.T) {
	s := newTestStore()
	s.ApplySnapshot("a1", api.Agent{ID: "a1"}, []api.Instance{{ID: "i1", AgentID: "a1"}})
	s.ApplySnapshot("a2", api.Agent{ID: "a2"}, []api.Instance{{ID: "i2", AgentID: "a2"}})
	// Re-snapshot a1 with an empty instance set: a2's instance must survive.
	s.ApplySnapshot("a1", api.Agent{ID: "a1"}, nil)

	if ids := instanceIDs(s.InstancesForAgent("a1")); len(ids) != 0 {
		t.Fatalf("InstancesForAgent(a1) = %v, want empty", ids)
	}
	if ids := instanceIDs(s.InstancesForAgent("a2")); len(ids) != 1 || ids[0] != "i2" {
		t.Fatalf("InstancesForAgent(a2) = %v, want [i2]", ids)
	}
	if ids := instanceIDs(s.AllInstances()); len(ids) != 1 || ids[0] != "i2" {
		t.Fatalf("AllInstances = %v, want [i2]", ids)
	}
}

func TestLiveStoreSetInstancesReplacesWithoutTouchingAgent(t *testing.T) {
	s := newTestStore()
	s.ApplySnapshot("a1", api.Agent{ID: "a1", NodeName: "alpha"}, []api.Instance{
		{ID: "i1", AgentID: "a1"},
		{ID: "i2", AgentID: "a1"},
	})
	s.SetInstances("a1", []api.Instance{{ID: "i1", AgentID: "a1"}})

	// Agent value unchanged.
	if got, _ := s.Get("a1"); got.NodeName != "alpha" {
		t.Fatalf("agent value changed by SetInstances: %+v", got)
	}
	// i2 pruned.
	if ids := instanceIDs(s.InstancesForAgent("a1")); len(ids) != 1 || ids[0] != "i1" {
		t.Fatalf("InstancesForAgent(a1) = %v, want [i1]", ids)
	}
}

func TestLiveStoreRemoveEvictsAgentAndItsInstances(t *testing.T) {
	s := newTestStore()
	s.ApplySnapshot("a1", api.Agent{ID: "a1"}, []api.Instance{{ID: "i1", AgentID: "a1"}})
	s.ApplySnapshot("a2", api.Agent{ID: "a2"}, []api.Instance{{ID: "i2", AgentID: "a2"}})

	s.Remove("a1")

	if _, ok := s.Get("a1"); ok {
		t.Fatalf("a1 still present after Remove")
	}
	if s.Has("a1") {
		t.Fatalf("Has(a1) true after Remove")
	}
	if ids := instanceIDs(s.AllInstances()); len(ids) != 1 || ids[0] != "i2" {
		t.Fatalf("AllInstances after Remove = %v, want [i2]", ids)
	}
}

func TestLiveStoreGetMissingReturnsZeroFalse(t *testing.T) {
	s := newTestStore()
	if got, ok := s.Get("nope"); ok || got.ID != "" {
		t.Fatalf("Get(nope) = %+v ok=%v, want zero,false", got, ok)
	}
}

// TestLiveStoreGetDeepCopyIsolation mutates the reference-type fields of a
// value returned by Get (the Runtime.RecentEvents slice and the CertIssuedAt
// pointer) and asserts the mirror is untouched — the core concurrency-safety
// guarantee.
func TestLiveStoreGetDeepCopyIsolation(t *testing.T) {
	s := newTestStore()
	issued := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	s.ApplySnapshot("a1", api.Agent{
		ID:           "a1",
		NodeName:     "alpha",
		CertIssuedAt: &issued,
		Runtime: api.AgentRuntime{
			RecentEvents: []api.RuntimeEvent{{EventType: "e1"}, {EventType: "e2"}},
		},
	}, nil)

	got, _ := s.Get("a1")
	got.NodeName = "MUTATED"
	got.Runtime.RecentEvents[0].EventType = "MUTATED"
	*got.CertIssuedAt = time.Date(1999, 1, 1, 0, 0, 0, 0, time.UTC)

	fresh, _ := s.Get("a1")
	if fresh.NodeName != "alpha" {
		t.Fatalf("NodeName mutated via returned copy: %q", fresh.NodeName)
	}
	if fresh.Runtime.RecentEvents[0].EventType != "e1" {
		t.Fatalf("RecentEvents slice mutated via returned copy: %v", fresh.Runtime.RecentEvents)
	}
	if !fresh.CertIssuedAt.Equal(issued) {
		t.Fatalf("CertIssuedAt pointer mutated via returned copy: %v", *fresh.CertIssuedAt)
	}
}

// TestLiveStoreApplySnapshotInputAliasing asserts that mutating the args
// AFTER ApplySnapshot returns does not corrupt the mirror (clone-on-write).
func TestLiveStoreApplySnapshotInputAliasing(t *testing.T) {
	s := newTestStore()
	issued := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	// want is a value copy: mutating *agent.CertIssuedAt below also rewrites
	// the `issued` variable it points at, so we compare against this snapshot.
	want := issued
	agent := api.Agent{
		ID:           "a1",
		CertIssuedAt: &issued,
		Runtime:      api.AgentRuntime{RecentEvents: []api.RuntimeEvent{{EventType: "e1"}}},
	}
	s.ApplySnapshot("a1", agent, []api.Instance{{ID: "i1", AgentID: "a1"}})

	// Mutate the caller-retained args.
	agent.Runtime.RecentEvents[0].EventType = "MUTATED"
	*agent.CertIssuedAt = time.Date(1999, 1, 1, 0, 0, 0, 0, time.UTC)

	got, _ := s.Get("a1")
	if got.Runtime.RecentEvents[0].EventType != "e1" || !got.CertIssuedAt.Equal(want) {
		t.Fatalf("mirror agent aliased to input args: %+v", got)
	}
}

func TestLiveStoreReplaceIsScopedToOneAgent(t *testing.T) {
	s := newTestStore()
	s.ApplySnapshot("a1", api.Agent{ID: "a1"}, []api.Instance{{ID: "i1", AgentID: "a1"}, {ID: "i2", AgentID: "a1"}})
	s.ApplySnapshot("a2", api.Agent{ID: "a2"}, []api.Instance{{ID: "j1", AgentID: "a2"}})

	// Replace агента a1 новым набором: его старые инстансы уходят,
	// чужие остаются нетронутыми.
	s.SetInstances("a1", []api.Instance{{ID: "i3", AgentID: "a1"}})

	a1 := s.InstancesForAgent("a1")
	if len(a1) != 1 || a1[0].ID != "i3" {
		t.Fatalf("a1 instances = %+v, want single i3", a1)
	}
	a2 := s.InstancesForAgent("a2")
	if len(a2) != 1 || a2[0].ID != "j1" {
		t.Fatalf("a2 instances = %+v, want untouched j1", a2)
	}
	if all := s.AllInstances(); len(all) != 2 {
		t.Fatalf("AllInstances = %d, want 2", len(all))
	}

	// Remove эвиктит агента со ВСЕМИ его инстансами.
	s.Remove("a1")
	if got := s.InstancesForAgent("a1"); len(got) != 0 {
		t.Fatalf("a1 instances after Remove = %+v, want none", got)
	}
	if all := s.AllInstances(); len(all) != 1 {
		t.Fatalf("AllInstances after Remove = %d, want 1", len(all))
	}
}
