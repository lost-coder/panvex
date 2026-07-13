package hashchain

import (
	"strings"
	"testing"
	"time"
)

func TestComputeEventHashDeterministic(t *testing.T) {
	r := Record{
		ID:        "evt_42",
		ActorID:   "user_admin",
		Action:    "client.create",
		TargetID:  "client_99",
		CreatedAt: time.Date(2026, 5, 8, 12, 34, 56, 789, time.UTC),
		Details:   map[string]any{"name": "demo", "limits": map[string]any{"max_tcp_conns": 10, "quota_mb": 1024}},
	}

	h1, err := ComputeEventHash("", r)
	if err != nil {
		t.Fatalf("ComputeEventHash: %v", err)
	}
	h2, err := ComputeEventHash("", r)
	if err != nil {
		t.Fatalf("ComputeEventHash: %v", err)
	}
	if h1 != h2 {
		t.Fatalf("identical records produced different hashes: %s vs %s", h1, h2)
	}
	if len(h1) != 64 {
		t.Fatalf("expected 64-char hex SHA-256, got %d-char %q", len(h1), h1)
	}
}

func TestComputeEventHashDetailsKeyOrderIrrelevant(t *testing.T) {
	base := Record{
		ID:        "evt_1",
		ActorID:   "user_x",
		Action:    "settings.update",
		TargetID:  "panel",
		CreatedAt: time.Unix(1700000000, 0).UTC(),
	}

	a := base
	a.Details = map[string]any{"alpha": 1, "beta": 2, "gamma": map[string]any{"x": "y", "z": "w"}}

	b := base
	b.Details = map[string]any{"gamma": map[string]any{"z": "w", "x": "y"}, "beta": 2, "alpha": 1}

	ha, err := ComputeEventHash("prev", a)
	if err != nil {
		t.Fatal(err)
	}
	hb, err := ComputeEventHash("prev", b)
	if err != nil {
		t.Fatal(err)
	}
	if ha != hb {
		t.Fatalf("key order leaked into hash: %s vs %s", ha, hb)
	}
}

// TestComputeEventHashFieldBoundary (R4, audit §1.5): moving a delimiter
// between two fields must change the hash. Before canonical-JSON the payload
// was "%s|…|%s"-joined, so (Action="a|b", TargetID="c") and
// (Action="a", TargetID="b|c") collided into one hash.
func TestComputeEventHashFieldBoundary(t *testing.T) {
	base := Record{
		ID:        "evt_x",
		ActorID:   "u",
		CreatedAt: time.Unix(1700000000, 0).UTC(),
	}
	a := base
	a.Action, a.TargetID = "a|b", "c"
	b := base
	b.Action, b.TargetID = "a", "b|c"

	ha, err := ComputeEventHash("", a)
	if err != nil {
		t.Fatal(err)
	}
	hb, err := ComputeEventHash("", b)
	if err != nil {
		t.Fatal(err)
	}
	if ha == hb {
		t.Fatal("delimiter collision: distinct (Action,TargetID) split produced the same hash")
	}
}

func TestComputeEventHashPrevHashChangesOutput(t *testing.T) {
	r := Record{
		ID:        "evt_2",
		ActorID:   "user_y",
		Action:    "agent.deregister",
		TargetID:  "agent_3",
		CreatedAt: time.Now().UTC(),
		Details:   map[string]any{"reason": "manual"},
	}

	h1, err := ComputeEventHash("aaaa", r)
	if err != nil {
		t.Fatal(err)
	}
	h2, err := ComputeEventHash("bbbb", r)
	if err != nil {
		t.Fatal(err)
	}
	if h1 == h2 {
		t.Fatalf("prev_hash failed to chain into output (got %s for both)", h1)
	}
}

// TestComputeEventHashPrevHashCantSpoofPayload verifies the unit-separator
// boundary between prev_hash and the payload.
func TestComputeEventHashPrevHashCantSpoofPayload(t *testing.T) {
	r := Record{
		ID:        "evt_3",
		ActorID:   "u",
		Action:    "a",
		TargetID:  "t",
		CreatedAt: time.Unix(1700000000, 0).UTC(),
		Details:   map[string]any{},
	}

	hWith, err := ComputeEventHash("evt_3|u", r)
	if err != nil {
		t.Fatal(err)
	}
	hClean, err := ComputeEventHash("", r)
	if err != nil {
		t.Fatal(err)
	}
	if hWith == hClean {
		t.Fatalf("unit separator boundary missing — prev_hash spoofed payload prefix")
	}
}

func TestCanonicaliseJSONValueEmptyMap(t *testing.T) {
	s, err := canonicaliseJSONValue(map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if s != "{}" {
		t.Fatalf("empty map should serialise to {}, got %q", s)
	}
}

func TestCanonicaliseJSONValueStableNesting(t *testing.T) {
	got, err := canonicaliseJSONValue(map[string]any{
		"a": []any{3, 1, 2}, // arrays preserve order
		"b": map[string]any{"y": 2, "x": 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"a":[3,1,2],"b":{"x":1,"y":2}}`
	if got != want {
		t.Fatalf("unexpected canonical output: %s", got)
	}
	if !strings.Contains(got, `"x":1,"y":2`) {
		t.Fatalf("inner map keys not sorted: %s", got)
	}
}
