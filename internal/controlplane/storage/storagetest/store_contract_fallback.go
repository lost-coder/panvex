package storagetest

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

// runFallbackContract exercises the agent_fallback_state lifecycle and the
// FK ON DELETE CASCADE link from agents → agent_fallback_state introduced in
// Phase 4 of the direct-mode-panel plan. RunStoreContract dispatches into it
// so each backend exercises the same coverage.
func runFallbackContract(t *testing.T, open OpenStore) {
	t.Helper()

	t.Run("AgentFallbackStateLifecycle", func(t *testing.T) {
		store := open(t)
		defer store.Close()

		testAgentFallbackStateLifecycle(t, store)
	})

	t.Run("AgentFallbackStateCascadesOnAgentDelete", func(t *testing.T) {
		store := open(t)
		defer store.Close()

		testAgentFallbackStateCascadesOnAgentDelete(t, store)
	})
}

// testAgentFallbackStateLifecycle exercises insert / read / delete and the
// ON-CONFLICT-DO-NOTHING idempotency contract on agent_fallback_state.
func testAgentFallbackStateLifecycle(t *testing.T, store storage.Store) {
	t.Helper()
	ctx := context.Background()

	group := storage.FleetGroupRecord{
		ID:        testFleetGroupID,
		Name:      "Default",
		CreatedAt: time.Date(2026, time.April, 29, 12, 0, 0, 0, time.UTC),
	}
	if err := store.PutFleetGroup(ctx, group); err != nil {
		t.Fatalf("PutFleetGroup() error = %v", err)
	}

	agent := storage.AgentRecord{
		ID:           "agent-fallback-test",
		NodeName:     "n1",
		FleetGroupID: group.ID,
		Version:      "dev",
		LastSeenAt:   time.Date(2026, time.April, 29, 12, 0, 0, 0, time.UTC),
	}
	if err := store.PutAgent(ctx, agent); err != nil {
		t.Fatalf("PutAgent() error = %v", err)
	}
	t.Cleanup(func() { _ = store.DeleteAgent(ctx, agent.ID) })

	enteredAt := time.Date(2026, 4, 29, 12, 0, 0, 0, time.UTC)
	rec := storage.AgentFallbackStateRecord{AgentID: agent.ID, EnteredAt: enteredAt}

	if err := store.PutAgentFallbackState(ctx, rec); err != nil {
		t.Fatalf("PutAgentFallbackState() error = %v", err)
	}

	got, err := store.GetAgentFallbackState(ctx, agent.ID)
	if err != nil {
		t.Fatalf("GetAgentFallbackState() error = %v", err)
	}
	if !got.EnteredAt.Equal(enteredAt) {
		t.Fatalf("EnteredAt = %v, want %v", got.EnteredAt, enteredAt)
	}

	// Idempotent insert (ON CONFLICT DO NOTHING) — entered_at unchanged.
	later := enteredAt.Add(time.Hour)
	if err := store.PutAgentFallbackState(ctx, storage.AgentFallbackStateRecord{
		AgentID: agent.ID, EnteredAt: later,
	}); err != nil {
		t.Fatalf("PutAgentFallbackState() second call error = %v", err)
	}
	got, _ = store.GetAgentFallbackState(ctx, agent.ID)
	if !got.EnteredAt.Equal(enteredAt) {
		t.Fatalf("EnteredAt after re-Put = %v, want unchanged %v", got.EnteredAt, enteredAt)
	}

	if err := store.DeleteAgentFallbackState(ctx, agent.ID); err != nil {
		t.Fatalf("DeleteAgentFallbackState() error = %v", err)
	}

	if _, err := store.GetAgentFallbackState(ctx, agent.ID); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("Get after Delete error = %v, want ErrNotFound", err)
	}
}

// testAgentFallbackStateCascadesOnAgentDelete asserts the FK CASCADE: deleting
// the parent agent row must purge the dependent agent_fallback_state row so
// no orphan entries linger after deregistration.
func testAgentFallbackStateCascadesOnAgentDelete(t *testing.T, store storage.Store) {
	t.Helper()
	ctx := context.Background()

	group := storage.FleetGroupRecord{
		ID:        testFleetGroupID,
		Name:      "Default",
		CreatedAt: time.Date(2026, time.April, 29, 12, 0, 0, 0, time.UTC),
	}
	if err := store.PutFleetGroup(ctx, group); err != nil {
		t.Fatalf("PutFleetGroup() error = %v", err)
	}

	agent := storage.AgentRecord{
		ID:           "agent-cascade-fallback",
		NodeName:     "n1",
		FleetGroupID: group.ID,
		Version:      "dev",
		LastSeenAt:   time.Date(2026, time.April, 29, 12, 0, 0, 0, time.UTC),
	}
	if err := store.PutAgent(ctx, agent); err != nil {
		t.Fatalf("PutAgent() error = %v", err)
	}
	if err := store.PutAgentFallbackState(ctx, storage.AgentFallbackStateRecord{
		AgentID: agent.ID, EnteredAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("PutAgentFallbackState() error = %v", err)
	}

	if err := store.DeleteAgent(ctx, agent.ID); err != nil {
		t.Fatalf("DeleteAgent() error = %v", err)
	}

	if _, err := store.GetAgentFallbackState(ctx, agent.ID); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("Get after agent delete error = %v, want ErrNotFound (cascade should have removed row)", err)
	}
}
