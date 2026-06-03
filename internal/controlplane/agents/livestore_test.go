package agents

import (
	"sort"
	"testing"
)

// testAgent and testInstance stand in for the server's presentation Agent /
// Instance. They carry the reference-type fields (slice, map, pointer) that
// the real AgentRuntime / Instance have, so the deep-copy-isolation tests
// exercise the same aliasing hazards the server types would.
type testAgent struct {
	ID     string
	Name   string
	Events []string
	Tags   map[string]int
	Note   *string
}

type testInstance struct {
	ID      string
	AgentID string
	Scopes  []string
}

func cloneTestAgent(a testAgent) testAgent {
	out := a
	out.Events = append([]string(nil), a.Events...)
	if a.Tags != nil {
		out.Tags = make(map[string]int, len(a.Tags))
		for k, v := range a.Tags {
			out.Tags[k] = v
		}
	}
	if a.Note != nil {
		n := *a.Note
		out.Note = &n
	}
	return out
}

func cloneTestInstance(i testInstance) testInstance {
	out := i
	out.Scopes = append([]string(nil), i.Scopes...)
	return out
}

func newTestStore() *LiveStore[testAgent, testInstance] {
	return NewLiveStore(
		cloneTestAgent,
		cloneTestInstance,
		func(i testInstance) string { return i.ID },
		func(i testInstance) string { return i.AgentID },
	)
}

func instanceIDs(insts []testInstance) []string {
	out := make([]string, 0, len(insts))
	for _, i := range insts {
		out = append(out, i.ID)
	}
	sort.Strings(out)
	return out
}

func TestLiveStoreNewLiveStorePanicsOnNilFuncs(t *testing.T) {
	ok := func(i testInstance) string { return "" }
	cases := []struct {
		name string
		mk   func() *LiveStore[testAgent, testInstance]
	}{
		{"cloneAgent", func() *LiveStore[testAgent, testInstance] {
			return NewLiveStore[testAgent, testInstance](nil, cloneTestInstance, ok, ok)
		}},
		{"cloneInstance", func() *LiveStore[testAgent, testInstance] {
			return NewLiveStore[testAgent, testInstance](cloneTestAgent, nil, ok, ok)
		}},
		{"instanceID", func() *LiveStore[testAgent, testInstance] {
			return NewLiveStore[testAgent, testInstance](cloneTestAgent, cloneTestInstance, nil, ok)
		}},
		{"instanceAgent", func() *LiveStore[testAgent, testInstance] {
			return NewLiveStore[testAgent, testInstance](cloneTestAgent, cloneTestInstance, ok, nil)
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
	s.ApplySnapshot("a1", testAgent{ID: "a1", Name: "alpha"}, []testInstance{
		{ID: "i1", AgentID: "a1"},
		{ID: "i2", AgentID: "a1"},
	})

	got, ok := s.Get("a1")
	if !ok || got.Name != "alpha" {
		t.Fatalf("Get(a1) = %+v ok=%v, want Name=alpha", got, ok)
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
	s.ApplySnapshot("a1", testAgent{ID: "a1"}, []testInstance{
		{ID: "i1", AgentID: "a1"},
		{ID: "i2", AgentID: "a1"},
		{ID: "i3", AgentID: "a1"},
	})
	// Second snapshot drops i2 and i3, adds i4. i2/i3 must be pruned.
	s.ApplySnapshot("a1", testAgent{ID: "a1"}, []testInstance{
		{ID: "i1", AgentID: "a1"},
		{ID: "i4", AgentID: "a1"},
	})
	if ids := instanceIDs(s.InstancesForAgent("a1")); len(ids) != 2 || ids[0] != "i1" || ids[1] != "i4" {
		t.Fatalf("after replace, InstancesForAgent(a1) = %v, want [i1 i4]", ids)
	}
}

func TestLiveStoreApplySnapshotDoesNotPruneOtherAgents(t *testing.T) {
	s := newTestStore()
	s.ApplySnapshot("a1", testAgent{ID: "a1"}, []testInstance{{ID: "i1", AgentID: "a1"}})
	s.ApplySnapshot("a2", testAgent{ID: "a2"}, []testInstance{{ID: "i2", AgentID: "a2"}})
	// Re-snapshot a1 with an empty instance set: a2's instance must survive.
	s.ApplySnapshot("a1", testAgent{ID: "a1"}, nil)

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
	s.ApplySnapshot("a1", testAgent{ID: "a1", Name: "alpha"}, []testInstance{
		{ID: "i1", AgentID: "a1"},
		{ID: "i2", AgentID: "a1"},
	})
	s.SetInstances("a1", []testInstance{{ID: "i1", AgentID: "a1"}})

	// Agent value unchanged.
	if got, _ := s.Get("a1"); got.Name != "alpha" {
		t.Fatalf("agent value changed by SetInstances: %+v", got)
	}
	// i2 pruned.
	if ids := instanceIDs(s.InstancesForAgent("a1")); len(ids) != 1 || ids[0] != "i1" {
		t.Fatalf("InstancesForAgent(a1) = %v, want [i1]", ids)
	}
}

func TestLiveStoreRemoveEvictsAgentAndItsInstances(t *testing.T) {
	s := newTestStore()
	s.ApplySnapshot("a1", testAgent{ID: "a1"}, []testInstance{{ID: "i1", AgentID: "a1"}})
	s.ApplySnapshot("a2", testAgent{ID: "a2"}, []testInstance{{ID: "i2", AgentID: "a2"}})

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

// TestLiveStoreGetDeepCopyIsolation mutates every reference-type field of a
// value returned by Get and asserts the mirror is untouched — the core
// concurrency-safety guarantee.
func TestLiveStoreGetDeepCopyIsolation(t *testing.T) {
	s := newTestStore()
	note := "original"
	s.ApplySnapshot("a1", testAgent{
		ID:     "a1",
		Name:   "alpha",
		Events: []string{"e1", "e2"},
		Tags:   map[string]int{"k": 1},
		Note:   &note,
	}, nil)

	got, _ := s.Get("a1")
	got.Name = "MUTATED"
	got.Events[0] = "MUTATED"
	got.Tags["k"] = 999
	got.Tags["new"] = 7
	*got.Note = "MUTATED"

	fresh, _ := s.Get("a1")
	if fresh.Name != "alpha" {
		t.Fatalf("Name mutated via returned copy: %q", fresh.Name)
	}
	if fresh.Events[0] != "e1" {
		t.Fatalf("Events slice mutated via returned copy: %v", fresh.Events)
	}
	if fresh.Tags["k"] != 1 || len(fresh.Tags) != 1 {
		t.Fatalf("Tags map mutated via returned copy: %v", fresh.Tags)
	}
	if *fresh.Note != "original" {
		t.Fatalf("Note pointer mutated via returned copy: %q", *fresh.Note)
	}
}

// TestLiveStoreApplySnapshotInputAliasing asserts that mutating the args
// AFTER ApplySnapshot returns does not corrupt the mirror (clone-on-write).
func TestLiveStoreApplySnapshotInputAliasing(t *testing.T) {
	s := newTestStore()
	agent := testAgent{ID: "a1", Events: []string{"e1"}, Tags: map[string]int{"k": 1}}
	insts := []testInstance{{ID: "i1", AgentID: "a1", Scopes: []string{"s1"}}}
	s.ApplySnapshot("a1", agent, insts)

	// Mutate the caller-retained args.
	agent.Events[0] = "MUTATED"
	agent.Tags["k"] = 999
	insts[0].Scopes[0] = "MUTATED"

	got, _ := s.Get("a1")
	if got.Events[0] != "e1" || got.Tags["k"] != 1 {
		t.Fatalf("mirror agent aliased to input args: %+v", got)
	}
	gotInst := s.InstancesForAgent("a1")
	if len(gotInst) != 1 || gotInst[0].Scopes[0] != "s1" {
		t.Fatalf("mirror instance aliased to input args: %+v", gotInst)
	}
}

// TestLiveStoreInstanceListDeepCopyIsolation mutates a returned instance's
// slice field and asserts the mirror is untouched.
func TestLiveStoreInstanceListDeepCopyIsolation(t *testing.T) {
	s := newTestStore()
	s.ApplySnapshot("a1", testAgent{ID: "a1"}, []testInstance{
		{ID: "i1", AgentID: "a1", Scopes: []string{"s1", "s2"}},
	})

	for _, getter := range []func() []testInstance{
		func() []testInstance { return s.InstancesForAgent("a1") },
		func() []testInstance { return s.AllInstances() },
	} {
		got := getter()
		if len(got) != 1 {
			t.Fatalf("getter returned %d instances, want 1", len(got))
		}
		got[0].Scopes[0] = "MUTATED"

		fresh := s.InstancesForAgent("a1")
		if fresh[0].Scopes[0] != "s1" {
			t.Fatalf("mirror instance Scopes mutated via returned copy: %v", fresh[0].Scopes)
		}
	}
}
