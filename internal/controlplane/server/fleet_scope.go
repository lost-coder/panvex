package server

import (
	"context"

	"github.com/lost-coder/panvex/internal/controlplane/auth"
)

// FleetScopeAccess captures the operator's effective fleet-group scope
// for one request. R-S-14 introduced this as the foundation for
// per-resource authorization: handlers that touch a fleet-group-scoped
// resource (clients, fleet groups, discovered clients, jobs targeting
// agents) consult the access set before reading or mutating.
//
// Semantics:
//   - Global == true  → no per-group restriction; admin role always
//     resolves to global, and an operator with no scope rows behaves
//     the same way (single-tenant default).
//   - Global == false → only fleet-group ids in Allowed are visible.
//
// Methods on this struct are the only path callers should use; do not
// inspect Allowed directly so a future migration to a different scope
// model (regex, hierarchical) lands in one place.
type FleetScopeAccess struct {
	Global  bool
	Allowed map[string]struct{}
}

// IsAllowed reports whether the operator can act on the given fleet
// group id. Global access always passes; otherwise the id must be in
// the explicit allow-set.
func (a FleetScopeAccess) IsAllowed(fleetGroupID string) bool {
	if a.Global {
		return true
	}
	_, ok := a.Allowed[fleetGroupID]
	return ok
}

// Filter returns the subset of input ids the operator can access.
// Useful for narrowing list responses and bulk-job target lists in one
// pass without per-id allocation.
func (a FleetScopeAccess) Filter(fleetGroupIDs []string) []string {
	if a.Global {
		return fleetGroupIDs
	}
	out := make([]string, 0, len(fleetGroupIDs))
	for _, id := range fleetGroupIDs {
		if _, ok := a.Allowed[id]; ok {
			out = append(out, id)
		}
	}
	return out
}

// resolveFleetScope loads the operator's per-fleet-group scope from the
// store. Admins are always global. Operators with zero rows in the
// scope table fall back to global so single-tenant deploys keep working
// without an explicit migration.
//
// Errors from the store are returned unwrapped — the caller should
// fail-closed on read failure rather than serve a partial view.
func (s *Server) resolveFleetScope(ctx context.Context, user auth.User) (FleetScopeAccess, error) {
	if user.Role == auth.RoleAdmin {
		return FleetScopeAccess{Global: true}, nil
	}
	if s.store == nil {
		return FleetScopeAccess{Global: true}, nil
	}
	ids, err := s.store.ListUserFleetGroupScopes(ctx, user.ID)
	if err != nil {
		return FleetScopeAccess{}, err
	}
	if len(ids) == 0 {
		// Single-tenant default: empty scope rows == global view. The
		// flip to "deny by default" lands when the multi-tenant rollout
		// PR seeds non-admin users with explicit scopes.
		return FleetScopeAccess{Global: true}, nil
	}
	allowed := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		allowed[id] = struct{}{}
	}
	return FleetScopeAccess{Global: false, Allowed: allowed}, nil
}
