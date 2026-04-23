package server

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

// seedTestFleetGroup ensures a fleet group with the given slug exists
// and returns its UUID. Idempotent: if Server.New() already seeded
// the group (as it does for the built-in "default" slug at startup),
// we look it up and return its id rather than inserting a duplicate.
// Otherwise a fresh UUID is minted and persisted. The returned id is
// the value every downstream record (agent, token, assignment)
// should stamp into its `fleet_group_id` column. When `createdAt` is
// zero time, a fixed fallback is used so tests stay deterministic.
func seedTestFleetGroup(t *testing.T, store storage.Store, name string, createdAt time.Time) string {
	t.Helper()
	ctx := context.Background()
	if existing, err := store.GetFleetGroupByName(ctx, name); err == nil {
		return existing.ID
	}
	if createdAt.IsZero() {
		createdAt = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	}
	id := uuid.NewString()
	if err := store.PutFleetGroup(ctx, storage.FleetGroupRecord{
		ID:        id,
		Name:      name,
		Label:     name,
		CreatedAt: createdAt,
		UpdatedAt: createdAt,
	}); err != nil {
		t.Fatalf("seedTestFleetGroup(%q) error = %v", name, err)
	}
	return id
}

// resolveTestFleetGroupID returns the UUID for the fleet group with
// the given slug, failing the test if the lookup errors. Tests that
// kick off enrollment via `issueEnrollmentToken(scope{slug})` rely
// on this helper when they later need to compare the stored
// `fleet_group_id` against a concrete value — the token-issue path
// auto-seeds the group, so the caller only needs the UUID lookup.
func resolveTestFleetGroupID(t *testing.T, store storage.Store, name string) string {
	t.Helper()
	group, err := store.GetFleetGroupByName(context.Background(), name)
	if err != nil {
		t.Fatalf("resolveTestFleetGroupID(%q) error = %v", name, err)
	}
	return group.ID
}
