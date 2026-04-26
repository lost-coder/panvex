package storagetest

import (
	"context"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

// runAgentsContract extracts the agent and instance snapshot contract blocks from
// the historic store_contract.go monolith (R-Q-18). RunStoreContract
// dispatches into it so each backend exercises the same coverage.
func runAgentsContract(t *testing.T, open OpenStore) {
	t.Helper()

	t.Run("agent and instance snapshot persistence round trip", func(t *testing.T) {
		store := open(t)
		defer store.Close()

		ctx := context.Background()
		group := storage.FleetGroupRecord{
			ID:        testFleetGroupID,
			Name:      "Default",
			CreatedAt: time.Date(2026, time.March, 15, 8, 20, 0, 0, time.UTC),
		}
		agent := storage.AgentRecord{
			ID:           "agent-000001",
			NodeName:     "node-a",
			FleetGroupID: group.ID,
			Version:      "dev",
			ReadOnly:     false,
			LastSeenAt:   time.Date(2026, time.March, 15, 8, 25, 0, 0, time.UTC),
		}
		instance := storage.InstanceRecord{
			ID:                "instance-000001",
			AgentID:           agent.ID,
			Name:              "telemt-main",
			Version:           "1.0.0",
			ConfigFingerprint: "cfg-1",
			ConnectedUsers:    42,
			ReadOnly:          false,
			UpdatedAt:         agent.LastSeenAt,
		}

		if err := store.PutFleetGroup(ctx, group); err != nil {
			t.Fatalf("PutFleetGroup() error = %v", err)
		}

		if err := store.PutAgent(ctx, agent); err != nil {
			t.Fatalf("PutAgent() error = %v", err)
		}

		if err := store.PutInstance(ctx, instance); err != nil {
			t.Fatalf("PutInstance() error = %v", err)
		}

		agents, err := store.ListAgents(ctx)
		if err != nil {
			t.Fatalf("ListAgents() error = %v", err)
		}

		if len(agents) != 1 {
			t.Fatalf("len(ListAgents()) = %d, want 1", len(agents))
		}

		instances, err := store.ListInstances(ctx)
		if err != nil {
			t.Fatalf("ListInstances() error = %v", err)
		}

		if len(instances) != 1 {
			t.Fatalf("len(ListInstances()) = %d, want 1", len(instances))
		}

		if instances[0].AgentID != agent.ID {
			t.Fatalf("ListInstances()[0].AgentID = %q, want %q", instances[0].AgentID, agent.ID)
		}
	})


}
