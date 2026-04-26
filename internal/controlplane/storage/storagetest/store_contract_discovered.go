package storagetest

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

// runDiscoveredContract extracts the discovered-client contract blocks from
// the historic store_contract.go monolith (R-Q-18). RunStoreContract
// dispatches into it so each backend exercises the same coverage.
func runDiscoveredContract(t *testing.T, open OpenStore) {
	t.Helper()

	t.Run("discovered client put list and delete round trip", func(t *testing.T) {
		store := open(t)
		defer store.Close()

		ctx := context.Background()
		group := storage.FleetGroupRecord{
			ID:        testFleetGroupID,
			Name:      "Default",
			CreatedAt: time.Date(2026, time.April, 15, 10, 0, 0, 0, time.UTC),
		}
		agent := storage.AgentRecord{
			ID:           "agent-dc-001",
			NodeName:     "node-dc",
			FleetGroupID: group.ID,
			Version:      "dev",
			ReadOnly:     false,
			LastSeenAt:   time.Date(2026, time.April, 15, 10, 1, 0, 0, time.UTC),
		}

		if err := store.PutFleetGroup(ctx, group); err != nil {
			t.Fatalf("PutFleetGroup() error = %v", err)
		}
		if err := store.PutAgent(ctx, agent); err != nil {
			t.Fatalf("PutAgent() error = %v", err)
		}

		dc := storage.DiscoveredClientRecord{
			ID:           "dc-001",
			AgentID:      agent.ID,
			ClientName:   "external-user",
			Secret:       "abc123",
			Status:       "new",
			DiscoveredAt: time.Date(2026, time.April, 15, 10, 5, 0, 0, time.UTC),
			UpdatedAt:    time.Date(2026, time.April, 15, 10, 5, 0, 0, time.UTC),
		}

		if err := store.PutDiscoveredClient(ctx, dc); err != nil {
			t.Fatalf("PutDiscoveredClient() error = %v", err)
		}

		list, err := store.ListDiscoveredClients(ctx)
		if err != nil {
			t.Fatalf("ListDiscoveredClients() error = %v", err)
		}
		if len(list) != 1 {
			t.Fatalf("len(ListDiscoveredClients()) = %d, want 1", len(list))
		}

		byAgent, err := store.ListDiscoveredClientsByAgent(ctx, agent.ID)
		if err != nil {
			t.Fatalf("ListDiscoveredClientsByAgent() error = %v", err)
		}
		if len(byAgent) != 1 {
			t.Fatalf("len(ListDiscoveredClientsByAgent()) = %d, want 1", len(byAgent))
		}

		got, err := store.GetDiscoveredClient(ctx, dc.ID)
		if err != nil {
			t.Fatalf("GetDiscoveredClient() error = %v", err)
		}
		if got.ClientName != dc.ClientName {
			t.Fatalf("GetDiscoveredClient().ClientName = %q, want %q", got.ClientName, dc.ClientName)
		}

		updatedAt := time.Date(2026, time.April, 15, 10, 10, 0, 0, time.UTC)
		if err := store.UpdateDiscoveredClientStatus(ctx, dc.ID, "ignored", updatedAt); err != nil {
			t.Fatalf("UpdateDiscoveredClientStatus() error = %v", err)
		}
		got, _ = store.GetDiscoveredClient(ctx, dc.ID)
		if got.Status != "ignored" {
			t.Fatalf("status after update = %q, want %q", got.Status, "ignored")
		}

		if err := store.DeleteDiscoveredClient(ctx, dc.ID); err != nil {
			t.Fatalf("DeleteDiscoveredClient() error = %v", err)
		}
		_, err = store.GetDiscoveredClient(ctx, dc.ID)
		if err == nil {
			t.Fatal("GetDiscoveredClient() after delete returned nil error, want ErrNotFound")
		}
	})

	t.Run("GetDiscoveredClientByAgentAndName", func(t *testing.T) {
		// P2-LOG-02: the reconcile path relies on this lookup to dedupe
		// repeated FULL_SNAPSHOT reports. Verify the natural-key lookup
		// returns the correct row when it exists and ErrNotFound otherwise.
		store := open(t)
		defer store.Close()

		ctx := context.Background()
		group := storage.FleetGroupRecord{
			ID:        testFleetGroupID,
			Name:      "Default",
			CreatedAt: time.Date(2026, time.April, 15, 10, 0, 0, 0, time.UTC),
		}
		agentA := storage.AgentRecord{
			ID:           "agent-dc-nk-A",
			NodeName:     "node-A",
			FleetGroupID: group.ID,
			Version:      "dev",
			LastSeenAt:   time.Date(2026, time.April, 15, 10, 1, 0, 0, time.UTC),
		}
		agentB := storage.AgentRecord{
			ID:           "agent-dc-nk-B",
			NodeName:     "node-B",
			FleetGroupID: group.ID,
			Version:      "dev",
			LastSeenAt:   time.Date(2026, time.April, 15, 10, 1, 0, 0, time.UTC),
		}

		if err := store.PutFleetGroup(ctx, group); err != nil {
			t.Fatalf("PutFleetGroup() error = %v", err)
		}
		if err := store.PutAgent(ctx, agentA); err != nil {
			t.Fatalf("PutAgent(A) error = %v", err)
		}
		if err := store.PutAgent(ctx, agentB); err != nil {
			t.Fatalf("PutAgent(B) error = %v", err)
		}

		ts := time.Date(2026, time.April, 15, 10, 5, 0, 0, time.UTC)

		// Nothing yet -> ErrNotFound.
		if _, err := store.GetDiscoveredClientByAgentAndName(ctx, agentA.ID, "alpha"); !errors.Is(err, storage.ErrNotFound) {
			t.Fatalf("GetDiscoveredClientByAgentAndName() before insert error = %v, want ErrNotFound", err)
		}

		// Insert one row on agentA and another with the SAME client_name on
		// agentB: the lookup must scope by agent_id and not collide across
		// agents.
		dcA := storage.DiscoveredClientRecord{
			ID:           "dc-nk-A-alpha",
			AgentID:      agentA.ID,
			ClientName:   "alpha",
			Secret:       "secretA",
			Status:       "pending_review",
			DiscoveredAt: ts,
			UpdatedAt:    ts,
		}
		dcB := storage.DiscoveredClientRecord{
			ID:           "dc-nk-B-alpha",
			AgentID:      agentB.ID,
			ClientName:   "alpha",
			Secret:       "secretB",
			Status:       "pending_review",
			DiscoveredAt: ts,
			UpdatedAt:    ts,
		}
		if err := store.PutDiscoveredClient(ctx, dcA); err != nil {
			t.Fatalf("PutDiscoveredClient(A) error = %v", err)
		}
		if err := store.PutDiscoveredClient(ctx, dcB); err != nil {
			t.Fatalf("PutDiscoveredClient(B) error = %v", err)
		}

		gotA, err := store.GetDiscoveredClientByAgentAndName(ctx, agentA.ID, "alpha")
		if err != nil {
			t.Fatalf("GetDiscoveredClientByAgentAndName(A) error = %v", err)
		}
		if gotA.ID != dcA.ID {
			t.Fatalf("GetDiscoveredClientByAgentAndName(A).ID = %q, want %q", gotA.ID, dcA.ID)
		}
		if gotA.Secret != "secretA" {
			t.Fatalf("GetDiscoveredClientByAgentAndName(A).Secret = %q, want %q", gotA.Secret, "secretA")
		}

		gotB, err := store.GetDiscoveredClientByAgentAndName(ctx, agentB.ID, "alpha")
		if err != nil {
			t.Fatalf("GetDiscoveredClientByAgentAndName(B) error = %v", err)
		}
		if gotB.ID != dcB.ID {
			t.Fatalf("GetDiscoveredClientByAgentAndName(B).ID = %q, want %q", gotB.ID, dcB.ID)
		}

		// Unknown name on a known agent -> ErrNotFound.
		if _, err := store.GetDiscoveredClientByAgentAndName(ctx, agentA.ID, "does-not-exist"); !errors.Is(err, storage.ErrNotFound) {
			t.Fatalf("GetDiscoveredClientByAgentAndName(missing name) error = %v, want ErrNotFound", err)
		}

		// Unknown agent -> ErrNotFound.
		if _, err := store.GetDiscoveredClientByAgentAndName(ctx, "agent-nobody", "alpha"); !errors.Is(err, storage.ErrNotFound) {
			t.Fatalf("GetDiscoveredClientByAgentAndName(missing agent) error = %v, want ErrNotFound", err)
		}
	})


}
