package server

import (
	"context"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/auth"
)

func TestFleetScopeAccessIsAllowedGlobal(t *testing.T) {
	scope := FleetScopeAccess{Global: true}
	if !scope.IsAllowed("fg-anything") {
		t.Fatalf("global scope should allow any id")
	}
}

func TestFleetScopeAccessIsAllowedNarrow(t *testing.T) {
	scope := FleetScopeAccess{
		Allowed: map[string]struct{}{
			"fg-1": {},
			"fg-2": {},
		},
	}
	for _, id := range []string{"fg-1", "fg-2"} {
		if !scope.IsAllowed(id) {
			t.Fatalf("expected %q allowed", id)
		}
	}
	if scope.IsAllowed("fg-3") {
		t.Fatalf("expected fg-3 denied")
	}
}

func TestFleetScopeAccessFilter(t *testing.T) {
	global := FleetScopeAccess{Global: true}
	if got := global.Filter([]string{"fg-1", "fg-2"}); len(got) != 2 {
		t.Fatalf("global Filter should keep all ids; got %v", got)
	}

	narrow := FleetScopeAccess{Allowed: map[string]struct{}{"fg-2": {}}}
	got := narrow.Filter([]string{"fg-1", "fg-2", "fg-3"})
	if len(got) != 1 || got[0] != "fg-2" {
		t.Fatalf("narrow Filter result = %v, want [fg-2]", got)
	}
}

func TestResolveFleetScopeAdminAlwaysGlobal(t *testing.T) {
	// Admins skip the store lookup entirely — even with non-empty scope
	// rows in the table, the admin role wins.
	srv := mustNew(t, Options{
		LoginTimingFloor: -1,
		Now:              func() time.Time { return time.Unix(0, 0) },
	})
	defer srv.Close()

	scope, err := srv.resolveFleetScope(context.Background(), auth.User{
		ID:   "admin-1",
		Role: auth.RoleAdmin,
	})
	if err != nil {
		t.Fatalf("resolveFleetScope error = %v", err)
	}
	if !scope.Global {
		t.Fatalf("admin should resolve to global scope")
	}
}

func TestFleetScopeAccessIsAllowedAny(t *testing.T) {
	scope := FleetScopeAccess{
		Allowed: map[string]struct{}{"fg-1": {}, "fg-2": {}},
	}
	if !scope.IsAllowedAny([]string{"fg-3", "fg-1"}) {
		t.Fatalf("expected at-least-one match to pass")
	}
	if scope.IsAllowedAny([]string{"fg-3", "fg-4"}) {
		t.Fatalf("expected zero matches to fail")
	}
	if scope.IsAllowedAny(nil) {
		t.Fatalf("empty input on narrow scope should fail")
	}
	global := FleetScopeAccess{Global: true}
	if !global.IsAllowedAny(nil) {
		t.Fatalf("global scope should pass on empty input")
	}
}

func TestResolveFleetScopeOperatorEmptyMeansGlobal(t *testing.T) {
	// Single-tenant default: an operator without explicit scope rows
	// keeps the legacy global view. The store is nil here so the helper
	// short-circuits with global; the same property is asserted in the
	// integration tests once the multi-tenant rollout lands.
	srv := mustNew(t, Options{
		LoginTimingFloor: -1,
		Now:              func() time.Time { return time.Unix(0, 0) },
	})
	defer srv.Close()

	scope, err := srv.resolveFleetScope(context.Background(), auth.User{
		ID:   "op-1",
		Role: auth.RoleOperator,
	})
	if err != nil {
		t.Fatalf("resolveFleetScope error = %v", err)
	}
	if !scope.Global {
		t.Fatalf("operator without scope rows should resolve to global")
	}
}
