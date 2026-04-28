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

	t.Run("deregister flow deletes instances and agent", func(t *testing.T) {
		store := open(t)
		defer store.Close()

		ctx := context.Background()
		group := storage.FleetGroupRecord{
			ID:        testFleetGroupID,
			Name:      "Default",
			CreatedAt: time.Date(2026, time.March, 15, 8, 20, 0, 0, time.UTC),
		}
		agent := storage.AgentRecord{
			ID:           "agent-deregister",
			NodeName:     "node-z",
			FleetGroupID: group.ID,
			Version:      "dev",
			LastSeenAt:   time.Date(2026, time.March, 15, 8, 25, 0, 0, time.UTC),
		}
		instance := storage.InstanceRecord{
			ID:                "instance-deregister",
			AgentID:           agent.ID,
			Name:              "telemt-main",
			Version:           "1.0.0",
			ConfigFingerprint: "cfg-1",
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

		// Seed satellite rows that reference agents (id) to lock in the
		// FK cascade contract: a recovery grant and a discovered client.
		// Without ON DELETE CASCADE on those FKs, DeleteAgent below
		// would fail with a foreign-key constraint error — exactly the
		// failure mode that affected real deployments before migration
		// 0028.
		if err := store.PutAgentCertificateRecoveryGrant(ctx, storage.AgentCertificateRecoveryGrantRecord{
			AgentID:   agent.ID,
			IssuedBy:  "tester",
			IssuedAt:  agent.LastSeenAt,
			ExpiresAt: agent.LastSeenAt.Add(24 * time.Hour),
		}); err != nil {
			t.Fatalf("PutAgentCertificateRecoveryGrant() error = %v", err)
		}
		if err := store.PutDiscoveredClient(ctx, storage.DiscoveredClientRecord{
			ID:           "discovered-deregister",
			AgentID:      agent.ID,
			ClientName:   "stranger",
			Status:       "pending_review",
			DiscoveredAt: agent.LastSeenAt,
			UpdatedAt:    agent.LastSeenAt,
		}); err != nil {
			t.Fatalf("PutDiscoveredClient() error = %v", err)
		}

		if err := store.DeleteInstancesByAgent(ctx, agent.ID); err != nil {
			t.Fatalf("DeleteInstancesByAgent() error = %v", err)
		}
		if err := store.DeleteAgent(ctx, agent.ID); err != nil {
			t.Fatalf("DeleteAgent() error = %v", err)
		}

		instances, err := store.ListInstances(ctx)
		if err != nil {
			t.Fatalf("ListInstances() error = %v", err)
		}
		for _, inst := range instances {
			if inst.AgentID == agent.ID {
				t.Fatalf("ListInstances() still contains instance for deregistered agent: %+v", inst)
			}
		}

		agents, err := store.ListAgents(ctx)
		if err != nil {
			t.Fatalf("ListAgents() error = %v", err)
		}
		for _, a := range agents {
			if a.ID == agent.ID {
				t.Fatalf("ListAgents() still contains deregistered agent: %+v", a)
			}
		}

		// Cascade should have purged the satellite rows along with the
		// agent. Memory-store backends that don't enforce FKs may keep
		// them; only check on backends where the row count is exposed
		// via list helpers.
		discovered, err := store.ListDiscoveredClientsByAgent(ctx, agent.ID)
		if err != nil {
			t.Fatalf("ListDiscoveredClientsByAgent() error = %v", err)
		}
		if len(discovered) != 0 {
			t.Fatalf("ListDiscoveredClientsByAgent() = %d rows, want 0 (cascade did not purge)", len(discovered))
		}
	})
}
