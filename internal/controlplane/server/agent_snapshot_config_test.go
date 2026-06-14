package server

import "testing"

func TestCarryForwardObservedConfig(t *testing.T) {
	prev := []Instance{{ID: "telemt-primary", ManagedConfigHash: "h1", ManagedConfigJSON: `{"censorship":{"tls_domain":"a"}}`}}

	// Unchanged snapshot: hash same, json empty -> carry forward the last value.
	next := []Instance{{ID: "telemt-primary", ManagedConfigHash: "h1", ManagedConfigJSON: ""}}
	carryForwardObservedConfig(next, prev)
	if next[0].ManagedConfigJSON != `{"censorship":{"tls_domain":"a"}}` {
		t.Fatalf("expected carry-forward, got %q", next[0].ManagedConfigJSON)
	}

	// Changed snapshot: new json present -> replaces, no carry-forward.
	next2 := []Instance{{ID: "telemt-primary", ManagedConfigHash: "h2", ManagedConfigJSON: `{"general":{"log_level":"debug"}}`}}
	carryForwardObservedConfig(next2, prev)
	if next2[0].ManagedConfigJSON != `{"general":{"log_level":"debug"}}` {
		t.Fatalf("changed json must not be overwritten, got %q", next2[0].ManagedConfigJSON)
	}

	// No matching previous instance: empty json stays empty (nothing to carry).
	next3 := []Instance{{ID: "telemt-secondary", ManagedConfigHash: "h9", ManagedConfigJSON: ""}}
	carryForwardObservedConfig(next3, prev)
	if next3[0].ManagedConfigJSON != "" {
		t.Fatalf("unmatched instance must stay empty, got %q", next3[0].ManagedConfigJSON)
	}
}
