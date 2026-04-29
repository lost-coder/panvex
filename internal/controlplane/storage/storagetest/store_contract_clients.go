package storagetest

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

// runClientsContract extracts the client / assignment / deployment contract blocks from
// the historic store_contract.go monolith (R-Q-18). RunStoreContract
// dispatches into it so each backend exercises the same coverage.
func runClientsContract(t *testing.T, open OpenStore) {
	t.Helper()

	t.Run("client, assignment, and deployment round trip", func(t *testing.T) {
		store := open(t)
		defer store.Close()

		ctx := context.Background()
		group := storage.FleetGroupRecord{
			ID:        testFleetGroupID,
			Name:      "Default",
			CreatedAt: time.Date(2026, time.March, 17, 8, 51, 0, 0, time.UTC),
		}
		agent := storage.AgentRecord{
			ID:           "agent-000001",
			NodeName:     "node-a",
			FleetGroupID: group.ID,
			Version:      "dev",
			ReadOnly:     false,
			LastSeenAt:   time.Date(2026, time.March, 17, 8, 52, 0, 0, time.UTC),
		}
		deletedAt := time.Date(2026, time.March, 17, 9, 25, 0, 0, time.UTC)
		lastAppliedAt := time.Date(2026, time.March, 17, 9, 20, 0, 0, time.UTC)
		client := storage.ClientRecord{
			ID:               "client-000001",
			Name:             "alice",
			SecretCiphertext: "enc:alice-secret",
			UserADTag:        "0123456789abcdef0123456789abcdef",
			Enabled:          true,
			MaxTCPConns:      4,
			MaxUniqueIPs:     2,
			DataQuotaBytes:   1073741824,
			ExpirationRFC3339: "2026-03-31T00:00:00Z",
			CreatedAt:        time.Date(2026, time.March, 17, 9, 0, 0, 0, time.UTC),
			UpdatedAt:        time.Date(2026, time.March, 17, 9, 10, 0, 0, time.UTC),
			DeletedAt:        &deletedAt,
		}
		groupAssignment := storage.ClientAssignmentRecord{
			ID:           "assignment-000001",
			ClientID:     client.ID,
			TargetType:   "fleet_group",
			FleetGroupID: testFleetGroupID,
			CreatedAt:    time.Date(2026, time.March, 17, 9, 11, 0, 0, time.UTC),
		}
		nodeAssignment := storage.ClientAssignmentRecord{
			ID:         "assignment-000002",
			ClientID:   client.ID,
			TargetType: "agent",
			AgentID:    agent.ID,
			CreatedAt:  time.Date(2026, time.March, 17, 9, 12, 0, 0, time.UTC),
		}
		deployment := storage.ClientDeploymentRecord{
			ClientID:         client.ID,
			AgentID:          agent.ID,
			DesiredOperation: "client.create",
			Status:           "succeeded",
			LastError:        "",
			ConnectionLinks:  []string{"tg://proxy?server=node-a&secret=alice"},
			LastAppliedAt:    &lastAppliedAt,
			UpdatedAt:        time.Date(2026, time.March, 17, 9, 21, 0, 0, time.UTC),
		}

		if err := store.PutFleetGroup(ctx, group); err != nil {
			t.Fatalf("PutFleetGroup() error = %v", err)
		}

		if err := store.PutAgent(ctx, agent); err != nil {
			t.Fatalf("PutAgent() error = %v", err)
		}

		if err := store.PutClient(ctx, client); err != nil {
			t.Fatalf("PutClient() error = %v", err)
		}

		if err := store.PutClientAssignment(ctx, groupAssignment); err != nil {
			t.Fatalf("PutClientAssignment(group) error = %v", err)
		}

		if err := store.PutClientAssignment(ctx, nodeAssignment); err != nil {
			t.Fatalf("PutClientAssignment(node) error = %v", err)
		}

		if err := store.PutClientDeployment(ctx, deployment); err != nil {
			t.Fatalf("PutClientDeployment() error = %v", err)
		}

		storedClient, err := store.GetClientByID(ctx, client.ID)
		if err != nil {
			t.Fatalf("GetClientByID() error = %v", err)
		}

		if storedClient.Name != client.Name {
			t.Fatalf("GetClientByID() Name = %q, want %q", storedClient.Name, client.Name)
		}
		if storedClient.SecretCiphertext != client.SecretCiphertext {
			t.Fatalf("GetClientByID() SecretCiphertext = %q, want %q", storedClient.SecretCiphertext, client.SecretCiphertext)
		}
		if storedClient.UserADTag != client.UserADTag {
			t.Fatalf("GetClientByID() UserADTag = %q, want %q", storedClient.UserADTag, client.UserADTag)
		}
		if storedClient.DataQuotaBytes != client.DataQuotaBytes {
			t.Fatalf("GetClientByID() DataQuotaBytes = %d, want %d", storedClient.DataQuotaBytes, client.DataQuotaBytes)
		}
		if storedClient.DeletedAt == nil || !storedClient.DeletedAt.Equal(deletedAt) {
			t.Fatalf("GetClientByID() DeletedAt = %v, want %v", storedClient.DeletedAt, deletedAt)
		}

		clients, err := store.ListClients(ctx)
		if err != nil {
			t.Fatalf("ListClients() error = %v", err)
		}
		if len(clients) != 1 {
			t.Fatalf("len(ListClients()) = %d, want 1", len(clients))
		}

		assignments, err := store.ListClientAssignments(ctx, client.ID)
		if err != nil {
			t.Fatalf("ListClientAssignments() error = %v", err)
		}
		if len(assignments) != 2 {
			t.Fatalf("len(ListClientAssignments()) = %d, want 2", len(assignments))
		}
		if err := store.DeleteClientAssignments(ctx, client.ID); err != nil {
			t.Fatalf("DeleteClientAssignments() error = %v", err)
		}
		assignments, err = store.ListClientAssignments(ctx, client.ID)
		if err != nil {
			t.Fatalf("ListClientAssignments() after delete error = %v", err)
		}
		if len(assignments) != 0 {
			t.Fatalf("len(ListClientAssignments()) after delete = %d, want 0", len(assignments))
		}

		deployments, err := store.ListClientDeployments(ctx, client.ID)
		if err != nil {
			t.Fatalf("ListClientDeployments() error = %v", err)
		}
		if len(deployments) != 1 {
			t.Fatalf("len(ListClientDeployments()) = %d, want 1", len(deployments))
		}
		if !slices.Equal(deployments[0].ConnectionLinks, deployment.ConnectionLinks) {
			t.Fatalf("ListClientDeployments()[0].ConnectionLinks = %v, want %v", deployments[0].ConnectionLinks, deployment.ConnectionLinks)
		}
		if deployments[0].LastAppliedAt == nil || !deployments[0].LastAppliedAt.Equal(lastAppliedAt) {
			t.Fatalf("ListClientDeployments()[0].LastAppliedAt = %v, want %v", deployments[0].LastAppliedAt, lastAppliedAt)
		}
	})


}
