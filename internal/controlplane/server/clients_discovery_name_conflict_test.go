package server

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/clients"
	"github.com/lost-coder/panvex/internal/controlplane/storage"
	"github.com/lost-coder/panvex/internal/controlplane/storage/sqlite"
)

// TestAdoptDiscoveredNameConflictDifferentSecret guards R10b Task 6 / C7 on
// the adopt path: a discovered record whose NAME matches a living managed
// client but with a DIFFERENT secret must fail as a name conflict rather than
// creating a duplicate-named managed client (which would collapse into one
// Telemt user on any common node). Single-adopt surfaces errClientNameTaken;
// bulk-adopt surfaces it as a per-item error without aborting the batch.
func TestAdoptDiscoveredNameConflictDifferentSecret(t *testing.T) {
	now := time.Date(2026, time.July, 15, 12, 0, 0, 0, time.UTC)

	store, err := sqlite.Open(filepath.Join(t.TempDir(), "panvex.db"))
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	fleetGroupID := seedTestFleetGroup(t, store, "default", now.Add(-time.Minute))
	agentID := "agent-adopt-conflict-1"
	if err := store.PutAgent(ctx, storage.AgentRecord{
		ID:           agentID,
		NodeName:     "node-A",
		FleetGroupID: fleetGroupID,
		Version:      "dev",
		LastSeenAt:   now.Add(-time.Minute),
	}); err != nil {
		t.Fatalf("PutAgent: %v", err)
	}

	server := mustNew(t, Options{
		LoginTimingFloor: -1,
		Now:              func() time.Time { return now },
		Store:            store,
	})
	defer server.Close()

	// A living managed client "external-eve" with secret S1.
	existing := managedClient{
		ID:        clients.ClientID(server.clientsSvc.NextClientID()),
		Name:      "external-eve",
		Secret:    "5555555555555555eeeeeeeeeeeeeeee",
		Enabled:   true,
		CreatedAt: now.Add(-time.Hour),
		UpdatedAt: now.Add(-time.Hour),
	}
	if err := server.clientsSvc.SaveState(ctx, existing, nil, nil); err != nil {
		t.Fatalf("SaveState: %v", err)
	}
	server.PublishClientsUpdated(existing.ID)

	// A discovered record with the SAME name but a DIFFERENT secret S2.
	discoveredID := "discovered-conflict-1"
	seedDiscoveredClient(t, store, discoveredSeed{
		id:           discoveredID,
		agentID:      agentID,
		clientName:   "external-eve",
		secret:       "6666666666666666ffffffffffffffff", // different from S1
		status:       discoveredClientStatusPendingReview,
		discoveredAt: now,
		updatedAt:    now,
	})

	// Single-adopt → errClientNameTaken.
	if _, err := server.clientsSvc.AdoptDiscovered(ctx, discoveredID, "operator-1", now); !errors.Is(err, errClientNameTaken) {
		t.Fatalf("adoptDiscoveredClient error = %v, want errClientNameTaken", err)
	}
	// The discovered record must remain pending (not flipped to adopted).
	if status := discoveredStatus(t, store, discoveredID); status != discoveredClientStatusPendingReview {
		t.Fatalf("discovered status = %q, want %q (conflicted adopt must not flip status)", status, discoveredClientStatusPendingReview)
	}
	// No second managed client named external-eve was created.
	count := 0
	for _, c := range server.clientsSvc.MirrorSnapshot().Clients {
		if c.DeletedAt == nil && c.Name == "external-eve" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("managed clients named external-eve = %d, want 1 (no duplicate created)", count)
	}

	// Bulk-adopt surfaces the same conflict as a per-item error, not a panic
	// or a batch abort.
	results := server.clientsSvc.BulkAdoptDiscovered(ctx, []string{discoveredID}, "operator-1", now)
	if len(results) != 1 {
		t.Fatalf("bulkAdopt results = %d, want 1", len(results))
	}
	if results[0].Status != "error" {
		t.Fatalf("bulkAdopt result status = %q, want error", results[0].Status)
	}
	if results[0].Message != errClientNameTaken.Error() {
		t.Fatalf("bulkAdopt result message = %q, want %q", results[0].Message, errClientNameTaken.Error())
	}
}

// TestAdoptDiscoveredNameSecretMatchStillMerges is the no-regression guard:
// a discovered record whose name AND secret match a living managed client
// still merges into it (the merge branch runs before the name-conflict
// check), producing no new client and no error.
func TestAdoptDiscoveredNameSecretMatchStillMerges(t *testing.T) {
	now := time.Date(2026, time.July, 15, 12, 0, 0, 0, time.UTC)

	store, err := sqlite.Open(filepath.Join(t.TempDir(), "panvex.db"))
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	fleetGroupID := seedTestFleetGroup(t, store, "default", now.Add(-time.Minute))
	agentA := "agent-merge-A"
	agentB := "agent-merge-B"
	for _, id := range []string{agentA, agentB} {
		if err := store.PutAgent(ctx, storage.AgentRecord{
			ID:           id,
			NodeName:     id,
			FleetGroupID: fleetGroupID,
			Version:      "dev",
			LastSeenAt:   now.Add(-time.Minute),
		}); err != nil {
			t.Fatalf("PutAgent(%q): %v", id, err)
		}
	}

	server := mustNew(t, Options{
		LoginTimingFloor: -1,
		Now:              func() time.Time { return now },
		Store:            store,
	})
	defer server.Close()

	clientName := "external-frank"
	clientSecret := "7777777777777777aaaaaaaaaaaaaaaa"
	// Living managed client already deployed on agentA.
	existing := managedClient{
		ID:        clients.ClientID(server.clientsSvc.NextClientID()),
		Name:      clientName,
		Secret:    clientSecret,
		Enabled:   true,
		CreatedAt: now.Add(-time.Hour),
		UpdatedAt: now.Add(-time.Hour),
	}
	if err := server.clientsSvc.SaveState(ctx, existing, []managedClientAssignment{{
		ID:         "assign-existing",
		ClientID:   existing.ID,
		TargetType: clientAssignmentTargetAgent,
		AgentID:    agentA,
		CreatedAt:  now.Add(-time.Hour),
	}}, nil); err != nil {
		t.Fatalf("SaveState: %v", err)
	}
	server.PublishClientsUpdated(existing.ID)

	// Discovered on agentB with matching name+secret → must merge.
	discoveredID := "discovered-merge-frank"
	seedDiscoveredClient(t, store, discoveredSeed{
		id:           discoveredID,
		agentID:      agentB,
		clientName:   clientName,
		secret:       clientSecret,
		status:       discoveredClientStatusPendingReview,
		discoveredAt: now,
		updatedAt:    now,
	})

	adopted, err := server.clientsSvc.AdoptDiscovered(ctx, discoveredID, "operator-1", now)
	if err != nil {
		t.Fatalf("adoptDiscoveredClient (merge) error = %v, want nil", err)
	}
	if adopted.ID != existing.ID {
		t.Fatalf("merge adopted into client %q, want existing %q", adopted.ID, existing.ID)
	}
	// Still exactly one managed client with this name.
	count := 0
	for _, c := range server.clientsSvc.MirrorSnapshot().Clients {
		if c.DeletedAt == nil && c.Name == clientName {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("managed clients named %q = %d, want 1 (merge, not duplicate)", clientName, count)
	}
	// The merge folded in agentB.
	assignments, _ := server.clientsSvc.MirrorAssignmentsAndDeployments(string(existing.ID))
	gotAgents := map[string]struct{}{}
	for _, a := range assignments {
		gotAgents[a.AgentID] = struct{}{}
	}
	for _, want := range []string{agentA, agentB} {
		if _, ok := gotAgents[want]; !ok {
			t.Fatalf("merged client missing agent %q: %+v", want, assignments)
		}
	}
}
