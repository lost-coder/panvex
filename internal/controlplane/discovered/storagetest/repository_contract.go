// internal/controlplane/discovered/storagetest/repository_contract.go
//
// RunContract exercises any discovered.Repository implementation. Backends
// invoke this from their own *_test.go to verify they meet the contract.
package storagetest

import (
	"context"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/discovered"
)

// OpenRepo is a factory function that creates a fresh, empty repository for
// each sub-test.
type OpenRepo func(t *testing.T) discovered.Repository

// RunContract runs all contract sub-tests against the given OpenRepo factory.
func RunContract(t *testing.T, open OpenRepo) {
	t.Helper()
	t.Run("SaveLoadRoundTrip", func(t *testing.T) { runSaveLoadRoundTrip(t, open(t)) })
	t.Run("ListEmpty", func(t *testing.T) { runListEmpty(t, open(t)) })
	t.Run("GetNotFound", func(t *testing.T) { runGetNotFound(t, open(t)) })
	t.Run("GetByAgentAndName", func(t *testing.T) { runGetByAgentAndName(t, open(t)) })
	t.Run("ListByAgent", func(t *testing.T) { runListByAgent(t, open(t)) })
	t.Run("UpdateStatus", func(t *testing.T) { runUpdateStatus(t, open(t)) })
	t.Run("UpdateStatusBulk", func(t *testing.T) { runUpdateStatusBulk(t, open(t)) })
	t.Run("Delete", func(t *testing.T) { runDelete(t, open(t)) })
}

func runSaveLoadRoundTrip(t *testing.T, repo discovered.Repository) {
	ctx := context.Background()
	now := time.Unix(1700000000, 0).UTC()
	dc := discovered.DiscoveredClient{
		ID:                 discovered.DiscoveredID("d-rt-1"),
		AgentID:            "a-1",
		ClientName:         "alpha",
		Status:             discovered.StatusPending,
		TotalOctets:        1024,
		CurrentConnections: 5,
		ActiveUniqueIPs:    3,
		FirstSeen:          now,
		UpdatedAt:          now,
	}
	if err := repo.Save(ctx, dc); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := repo.Get(ctx, dc.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.ID != dc.ID || got.AgentID != dc.AgentID || got.ClientName != dc.ClientName {
		t.Fatalf("Get returned %+v, want %+v", got, dc)
	}
	if got.Status != dc.Status {
		t.Fatalf("Status: got %q, want %q", got.Status, dc.Status)
	}
	if got.TotalOctets != dc.TotalOctets {
		t.Fatalf("TotalOctets: got %d, want %d", got.TotalOctets, dc.TotalOctets)
	}
}

func runListEmpty(t *testing.T, repo discovered.Repository) {
	ctx := context.Background()
	list, err := repo.List(ctx)
	if err != nil {
		t.Fatalf("List on empty: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("List on empty repo returned %d items", len(list))
	}
}

func runGetNotFound(t *testing.T, repo discovered.Repository) {
	ctx := context.Background()
	_, err := repo.Get(ctx, discovered.DiscoveredID("does-not-exist"))
	if err == nil {
		t.Fatal("Get of nonexistent must return error")
	}
}

func runGetByAgentAndName(t *testing.T, repo discovered.Repository) {
	ctx := context.Background()
	now := time.Unix(1700000000, 0).UTC()
	dc := discovered.DiscoveredClient{
		ID:         discovered.DiscoveredID("d-byname-1"),
		AgentID:    "a-byname",
		ClientName: "byname-alpha",
		Status:     discovered.StatusPending,
		FirstSeen:  now,
		UpdatedAt:  now,
	}
	if err := repo.Save(ctx, dc); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := repo.GetByAgentAndName(ctx, "a-byname", "byname-alpha")
	if err != nil {
		t.Fatalf("GetByAgentAndName: %v", err)
	}
	if got.ID != dc.ID {
		t.Fatalf("GetByAgentAndName returned %+v, want %+v", got, dc)
	}
	// Negative path
	_, err = repo.GetByAgentAndName(ctx, "a-byname", "nonexistent")
	if err == nil {
		t.Fatal("GetByAgentAndName for missing must return error")
	}
}

func runListByAgent(t *testing.T, repo discovered.Repository) {
	ctx := context.Background()
	now := time.Unix(1700000000, 0).UTC()
	for i, name := range []string{"alpha", "beta", "gamma"} {
		dc := discovered.DiscoveredClient{
			ID:         discovered.DiscoveredID("d-listagent-" + name),
			AgentID:    "a-list",
			ClientName: name,
			Status:     discovered.StatusPending,
			FirstSeen:  now,
			UpdatedAt:  now.Add(time.Duration(i) * time.Second),
		}
		if err := repo.Save(ctx, dc); err != nil {
			t.Fatalf("Save %s: %v", name, err)
		}
	}
	// Save one for a different agent to verify filtering
	other := discovered.DiscoveredClient{
		ID:         discovered.DiscoveredID("d-listagent-other"),
		AgentID:    "a-other",
		ClientName: "other",
		Status:     discovered.StatusPending,
		FirstSeen:  now,
		UpdatedAt:  now,
	}
	if err := repo.Save(ctx, other); err != nil {
		t.Fatalf("Save other: %v", err)
	}
	list, err := repo.ListByAgent(ctx, "a-list")
	if err != nil {
		t.Fatalf("ListByAgent: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("ListByAgent(a-list) returned %d items, want 3", len(list))
	}
	for _, item := range list {
		if item.AgentID != "a-list" {
			t.Fatalf("ListByAgent returned item with AgentID=%s, want a-list", item.AgentID)
		}
	}
}

func runUpdateStatus(t *testing.T, repo discovered.Repository) {
	ctx := context.Background()
	now := time.Unix(1700000000, 0).UTC()
	dc := discovered.DiscoveredClient{
		ID:         discovered.DiscoveredID("d-upd-1"),
		AgentID:    "a-upd",
		ClientName: "upd",
		Status:     discovered.StatusPending,
		FirstSeen:  now,
		UpdatedAt:  now,
	}
	if err := repo.Save(ctx, dc); err != nil {
		t.Fatalf("Save: %v", err)
	}
	later := now.Add(time.Hour)
	if err := repo.UpdateStatus(ctx, dc.ID, discovered.StatusAdopted, later); err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}
	got, err := repo.Get(ctx, dc.ID)
	if err != nil {
		t.Fatalf("Get after UpdateStatus: %v", err)
	}
	if got.Status != discovered.StatusAdopted {
		t.Fatalf("Status after UpdateStatus = %q, want %q", got.Status, discovered.StatusAdopted)
	}
}

func runUpdateStatusBulk(t *testing.T, repo discovered.Repository) {
	ctx := context.Background()
	now := time.Unix(1700000000, 0).UTC()
	ids := []discovered.DiscoveredID{
		discovered.DiscoveredID("d-bulk-1"),
		discovered.DiscoveredID("d-bulk-2"),
		discovered.DiscoveredID("d-bulk-3"),
	}
	for i, id := range ids {
		dc := discovered.DiscoveredClient{
			ID:         id,
			AgentID:    "a-bulk",
			ClientName: "bulk-" + string(rune('a'+i)),
			Status:     discovered.StatusPending,
			FirstSeen:  now,
			UpdatedAt:  now,
		}
		if err := repo.Save(ctx, dc); err != nil {
			t.Fatalf("Save %s: %v", id, err)
		}
	}
	later := now.Add(time.Hour)
	if err := repo.UpdateStatusBulk(ctx, ids, discovered.StatusIgnored, later); err != nil {
		t.Fatalf("UpdateStatusBulk: %v", err)
	}
	for _, id := range ids {
		got, err := repo.Get(ctx, id)
		if err != nil {
			t.Fatalf("Get %s after bulk update: %v", id, err)
		}
		if got.Status != discovered.StatusIgnored {
			t.Fatalf("Status of %s = %q, want %q", id, got.Status, discovered.StatusIgnored)
		}
	}
}

func runDelete(t *testing.T, repo discovered.Repository) {
	ctx := context.Background()
	now := time.Unix(1700000000, 0).UTC()
	dc := discovered.DiscoveredClient{
		ID:         discovered.DiscoveredID("d-del-1"),
		AgentID:    "a-del",
		ClientName: "del",
		Status:     discovered.StatusPending,
		FirstSeen:  now,
		UpdatedAt:  now,
	}
	if err := repo.Save(ctx, dc); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := repo.Delete(ctx, dc.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	_, err := repo.Get(ctx, dc.ID)
	if err == nil {
		t.Fatal("Get after Delete must return error")
	}
}
