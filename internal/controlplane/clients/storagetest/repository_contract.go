// internal/controlplane/clients/storagetest/repository_contract.go
//
// RunContract exercises any clients.Repository implementation. Backends
// invoke this from their own *_test.go to verify they meet the contract.
package storagetest

import (
	"context"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/clients"
)

// OpenRepo is a factory that creates a fresh, empty Repository for a single
// sub-test. Each sub-test receives its own instance so state does not leak.
type OpenRepo func(t *testing.T) clients.Repository

// RunContract runs all repository contract sub-tests against the given
// OpenRepo factory. Backends call this once from their *_test.go files.
func RunContract(t *testing.T, open OpenRepo) {
	t.Helper()
	t.Run("SaveLoadRoundTrip", func(t *testing.T) { runSaveLoadRoundTrip(t, open(t)) })
	t.Run("ListEmpty", func(t *testing.T) { runListEmpty(t, open(t)) })
	t.Run("GetNotFound", func(t *testing.T) { runGetNotFound(t, open(t)) })
	t.Run("DeleteCascadesAssignments", func(t *testing.T) { runDeleteCascadesAssignments(t, open(t)) })
	t.Run("UsageBulkRoundtrip", func(t *testing.T) { runUsageBulk(t, open(t)) })
	// More subtests added as Repository surface grows.
}

func runSaveLoadRoundTrip(t *testing.T, repo clients.Repository) {
	t.Helper()
	ctx := context.Background()
	c := clients.Client{
		ID:        clients.ClientID("c-rt-1"),
		Name:      "round-trip",
		Secret:    "opaque-secret",
		CreatedAt: time.Unix(1700000000, 0).UTC(),
		UpdatedAt: time.Unix(1700000001, 0).UTC(),
	}
	if err := repo.Save(ctx, c); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := repo.Get(ctx, c.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.ID != c.ID || got.Name != c.Name {
		t.Fatalf("Get returned %+v, want %+v", got, c)
	}
	if got.Secret != c.Secret {
		t.Fatalf("Secret mismatch: got %q, want %q", got.Secret, c.Secret)
	}
}

func runListEmpty(t *testing.T, repo clients.Repository) {
	t.Helper()
	ctx := context.Background()
	list, err := repo.List(ctx)
	if err != nil {
		t.Fatalf("List on empty: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("List on empty repo returned %d items", len(list))
	}
}

func runGetNotFound(t *testing.T, repo clients.Repository) {
	t.Helper()
	ctx := context.Background()
	_, err := repo.Get(ctx, clients.ClientID("does-not-exist"))
	// We expect storage.ErrNotFound here. Backend wires the sentinel via
	// repository wrapping. Specific error check goes here once
	// storage.ErrNotFound is a stable sentinel — for now we accept any
	// non-nil error and only verify backends agree.
	if err == nil {
		t.Fatal("Get of nonexistent must return error")
	}
}

func runDeleteCascadesAssignments(t *testing.T, repo clients.Repository) {
	t.Helper()
	ctx := context.Background()
	id := clients.ClientID("c-cascade")
	c := clients.Client{ID: id, Name: "cascade"}
	if err := repo.Save(ctx, c); err != nil {
		t.Fatalf("Save: %v", err)
	}
	a := clients.Assignment{
		ID:           clients.AssignmentID("a-1"),
		ClientID:     id,
		TargetType:   clients.TargetTypeFleetGroup,
		FleetGroupID: clients.FleetGroupID("fg-test"),
	}
	if err := repo.SaveAssignments(ctx, id, []clients.Assignment{a}); err != nil {
		t.Fatalf("SaveAssignments: %v", err)
	}
	if err := repo.Delete(ctx, id); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	got, err := repo.ListAssignments(ctx, id)
	if err != nil {
		t.Fatalf("ListAssignments after Delete: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("Delete must cascade assignments, got %d remaining", len(got))
	}
}

func runUsageBulk(t *testing.T, repo clients.Repository) {
	t.Helper()
	ctx := context.Background()
	// Set up two clients so foreign keys hold.
	for _, id := range []clients.ClientID{"c-u-1", "c-u-2"} {
		c := clients.Client{ID: id, Name: string(id)}
		if err := repo.Save(ctx, c); err != nil {
			t.Fatalf("Save %s: %v", id, err)
		}
	}
	batch := []clients.Usage{
		{ClientID: "c-u-1", AgentID: "a-1", TrafficUsedBytes: 100},
		{ClientID: "c-u-2", AgentID: "a-1", TrafficUsedBytes: 200},
	}
	if err := repo.UpsertUsageBulk(ctx, batch); err != nil {
		t.Fatalf("UpsertUsageBulk: %v", err)
	}
	got, err := repo.ListUsage(ctx)
	if err != nil {
		t.Fatalf("ListUsage: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("ListUsage returned %d, want 2", len(got))
	}
}
