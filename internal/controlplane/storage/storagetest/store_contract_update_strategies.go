package storagetest

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

// runAgentUpdateStrategyContract exercises the agent_update_strategies table
// (Telemt Update v1, Task 8): get-missing → ErrNotFound, upsert+get
// round-trip, re-upsert preserves CreatedAt while replacing the other
// fields and advancing UpdatedAt, idempotent delete, and the FK ON DELETE
// CASCADE link from agents → agent_update_strategies. RunStoreContract
// dispatches into it so each backend exercises the same coverage.
func runAgentUpdateStrategyContract(t *testing.T, open OpenStore) {
	t.Helper()

	t.Run("AgentUpdateStrategyLifecycle", func(t *testing.T) {
		store := open(t)
		defer store.Close()

		testAgentUpdateStrategyLifecycle(t, store)
	})

	t.Run("AgentUpdateStrategyCascadesOnAgentDelete", func(t *testing.T) {
		store := open(t)
		defer store.Close()

		testAgentUpdateStrategyCascadesOnAgentDelete(t, store)
	})
}

func seedUpdateStrategyAgent(t *testing.T, store storage.Store, ctx context.Context, agentID string) {
	t.Helper()

	group := storage.FleetGroupRecord{
		ID:        testFleetGroupID,
		Name:      "Default",
		CreatedAt: time.Date(2026, time.April, 29, 12, 0, 0, 0, time.UTC),
	}
	if err := store.PutFleetGroup(ctx, group); err != nil {
		t.Fatalf("PutFleetGroup() error = %v", err)
	}

	agent := storage.AgentRecord{
		ID:           agentID,
		NodeName:     "n1",
		FleetGroupID: group.ID,
		Version:      "dev",
		LastSeenAt:   time.Date(2026, time.April, 29, 12, 0, 0, 0, time.UTC),
	}
	if err := store.PutAgent(ctx, agent); err != nil {
		t.Fatalf("PutAgent() error = %v", err)
	}
}

// testAgentUpdateStrategyLifecycle exercises get-missing / upsert / get /
// re-upsert / delete / get-after-delete.
func testAgentUpdateStrategyLifecycle(t *testing.T, store storage.Store) {
	t.Helper()
	ctx := context.Background()

	const agentID = "agent-update-strategy-test"
	seedUpdateStrategyAgent(t, store, ctx, agentID)
	t.Cleanup(func() { _ = store.DeleteAgent(ctx, agentID) })

	if _, err := store.GetAgentUpdateStrategy(ctx, agentID); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("GetAgentUpdateStrategy() before upsert error = %v, want ErrNotFound", err)
	}

	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)
	rec := storage.AgentUpdateStrategyRecord{
		AgentID:     agentID,
		Mode:        "binary",
		RestartSpec: "systemd:telemt",
		BinaryPath:  "/usr/local/bin/telemt",
		AssetFlavor: "",
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := store.UpsertAgentUpdateStrategy(ctx, rec); err != nil {
		t.Fatalf("UpsertAgentUpdateStrategy() error = %v", err)
	}

	got, err := store.GetAgentUpdateStrategy(ctx, agentID)
	if err != nil {
		t.Fatalf("GetAgentUpdateStrategy() error = %v", err)
	}
	if got.AgentID != rec.AgentID || got.Mode != rec.Mode || got.RestartSpec != rec.RestartSpec ||
		got.BinaryPath != rec.BinaryPath || got.AssetFlavor != rec.AssetFlavor {
		t.Fatalf("GetAgentUpdateStrategy() = %+v, want %+v", got, rec)
	}
	if !got.CreatedAt.Equal(now) || !got.UpdatedAt.Equal(now) {
		t.Fatalf("GetAgentUpdateStrategy() timestamps = %v/%v, want %v/%v", got.CreatedAt, got.UpdatedAt, now, now)
	}

	// Re-upsert with edited fields: CreatedAt must be preserved, UpdatedAt
	// must advance to the newly supplied value.
	later := now.Add(time.Hour)
	rec2 := storage.AgentUpdateStrategyRecord{
		AgentID:     agentID,
		Mode:        "docker",
		RestartSpec: "docker:telemt-container",
		BinaryPath:  "",
		AssetFlavor: "v3",
		CreatedAt:   later, // caller-supplied CreatedAt must be ignored/overridden by the stored original
		UpdatedAt:   later,
	}
	if err := store.UpsertAgentUpdateStrategy(ctx, rec2); err != nil {
		t.Fatalf("UpsertAgentUpdateStrategy() re-upsert error = %v", err)
	}

	got, err = store.GetAgentUpdateStrategy(ctx, agentID)
	if err != nil {
		t.Fatalf("GetAgentUpdateStrategy() after re-upsert error = %v", err)
	}
	if got.Mode != "docker" || got.RestartSpec != "docker:telemt-container" || got.BinaryPath != "" || got.AssetFlavor != "v3" {
		t.Fatalf("GetAgentUpdateStrategy() after re-upsert = %+v, want updated fields from rec2", got)
	}
	if !got.CreatedAt.Equal(now) {
		t.Fatalf("CreatedAt after re-upsert = %v, want preserved %v", got.CreatedAt, now)
	}
	if !got.UpdatedAt.Equal(later) {
		t.Fatalf("UpdatedAt after re-upsert = %v, want %v", got.UpdatedAt, later)
	}

	if err := store.DeleteAgentUpdateStrategy(ctx, agentID); err != nil {
		t.Fatalf("DeleteAgentUpdateStrategy() error = %v", err)
	}
	if _, err := store.GetAgentUpdateStrategy(ctx, agentID); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("GetAgentUpdateStrategy() after delete error = %v, want ErrNotFound", err)
	}

	// Delete is idempotent.
	if err := store.DeleteAgentUpdateStrategy(ctx, agentID); err != nil {
		t.Fatalf("DeleteAgentUpdateStrategy() second call error = %v, want nil (idempotent)", err)
	}
	if err := store.DeleteAgentUpdateStrategy(ctx, "agent-never-existed"); err != nil {
		t.Fatalf("DeleteAgentUpdateStrategy() on absent agent error = %v, want nil (idempotent)", err)
	}
}

// testAgentUpdateStrategyCascadesOnAgentDelete asserts the FK CASCADE:
// deleting the parent agent row must purge the dependent
// agent_update_strategies row so no orphan entries linger after
// deregistration.
func testAgentUpdateStrategyCascadesOnAgentDelete(t *testing.T, store storage.Store) {
	t.Helper()
	ctx := context.Background()

	const agentID = "agent-cascade-update-strategy"
	seedUpdateStrategyAgent(t, store, ctx, agentID)

	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)
	if err := store.UpsertAgentUpdateStrategy(ctx, storage.AgentUpdateStrategyRecord{
		AgentID:     agentID,
		Mode:        "binary",
		RestartSpec: "systemd:telemt",
		BinaryPath:  "/usr/local/bin/telemt",
		CreatedAt:   now,
		UpdatedAt:   now,
	}); err != nil {
		t.Fatalf("UpsertAgentUpdateStrategy() error = %v", err)
	}

	if err := store.DeleteAgent(ctx, agentID); err != nil {
		t.Fatalf("DeleteAgent() error = %v", err)
	}

	if _, err := store.GetAgentUpdateStrategy(ctx, agentID); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("GetAgentUpdateStrategy() after agent delete error = %v, want ErrNotFound (cascade should have removed row)", err)
	}
}
