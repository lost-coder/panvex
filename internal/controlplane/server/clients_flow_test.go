package server

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
	"github.com/lost-coder/panvex/internal/controlplane/storage/sqlite"
)

// TestDeleteClientPersistsStateBeforeJob verifies that when persisting the
// client tombstone fails, no delete job is enqueued. Before the P2-LOG-01
// fix, the job was enqueued before persistence — a persist failure left the
// agent deleting the client on Telemt while the DB still held DeletedAt=nil
// (ghost state, M-C1).
func TestDeleteClientPersistsStateBeforeJob(t *testing.T) {
	now := time.Date(2026, time.April, 17, 12, 0, 0, 0, time.UTC)

	baseStore, err := sqlite.Open(filepath.Join(t.TempDir(), "panvex.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	defer baseStore.Close()

	ctx := context.Background()
	if err := baseStore.PutFleetGroup(ctx, storage.FleetGroupRecord{
		ID:        "default",
		Name:      "Default",
		CreatedAt: now.Add(-time.Minute),
	}); err != nil {
		t.Fatalf("PutFleetGroup() error = %v", err)
	}
	if err := baseStore.PutAgent(ctx, storage.AgentRecord{
		ID:           "agent-A",
		NodeName:     "node-a",
		FleetGroupID: "default",
		Version:      "dev",
		LastSeenAt:   now.Add(-time.Minute),
	}); err != nil {
		t.Fatalf("PutAgent() error = %v", err)
	}

	persistErr := errors.New("persist failure injected")
	failing := &failingStore{Store: baseStore, putClientErr: persistErr}

	server := New(Options{
		Now:   func() time.Time { return now },
		Store: failing,
	})
	defer server.Close()

	// Seed in-memory state directly: a live client assigned to the agent.
	clientID := "client-1"
	server.mu.Lock()
	server.agents["agent-A"] = Agent{
		ID:           "agent-A",
		NodeName:     "node-a",
		FleetGroupID: "default",
		Version:      "dev",
		LastSeenAt:   now.Add(-time.Minute),
	}
	server.mu.Unlock()

	server.clientsMu.Lock()
	server.clients[clientID] = managedClient{
		ID:        clientID,
		Name:      "alice",
		Secret:    "0123456789abcdef0123456789abcdef",
		Enabled:   true,
		CreatedAt: now.Add(-time.Minute),
		UpdatedAt: now.Add(-time.Minute),
	}
	server.clientAssignments[clientID] = []managedClientAssignment{{
		ID:         "assign-1",
		ClientID:   clientID,
		TargetType: clientAssignmentTargetAgent,
		AgentID:    "agent-A",
		CreatedAt:  now.Add(-time.Minute),
	}}
	server.clientDeployments[clientID] = map[string]managedClientDeployment{
		"agent-A": {
			ClientID:         clientID,
			AgentID:          "agent-A",
			DesiredOperation: "create",
			Status:           clientDeploymentStatusSucceeded,
			UpdatedAt:        now.Add(-time.Minute),
		},
	}
	server.clientsMu.Unlock()

	jobsBefore := len(server.jobs.List())

	err = server.deleteClientWithContext(ctx, clientID, "user-1", now)
	if !errors.Is(err, persistErr) {
		t.Fatalf("deleteClientWithContext() error = %v, want %v", err, persistErr)
	}

	jobsAfter := server.jobs.List()
	if len(jobsAfter) != jobsBefore {
		t.Fatalf("jobs queued after failed persist = %d, want %d (no job should be enqueued when persist fails)", len(jobsAfter)-jobsBefore, 0)
	}

	// The in-memory record must remain live (DeletedAt=nil) because persist
	// returned an error; replaceClientStateWithContext bails out before
	// touching the in-memory map.
	server.clientsMu.RLock()
	stored, ok := server.clients[clientID]
	server.clientsMu.RUnlock()
	if !ok {
		t.Fatalf("client %s missing from in-memory map after failed delete", clientID)
	}
	if stored.DeletedAt != nil {
		t.Fatalf("client DeletedAt = %v, want nil (state must remain consistent when persist fails)", stored.DeletedAt)
	}
}

// TestResolveClientIDByNameHitsFleetGroupAssignment verifies that
// resolveClientIDByName resolves a client whose only assignment is to a
// fleet group the agent belongs to (P2-LOG-07 / M-C3). Without the fix,
// fleet-group-assigned clients produced no match and their usage stats
// were silently dropped.
func TestResolveClientIDByNameHitsFleetGroupAssignment(t *testing.T) {
	now := time.Date(2026, time.April, 17, 12, 30, 0, 0, time.UTC)

	server := New(Options{
		Now: func() time.Time { return now },
	})
	defer server.Close()

	server.mu.Lock()
	server.agents["agent-EU"] = Agent{
		ID:           "agent-EU",
		NodeName:     "node-eu-1",
		FleetGroupID: "eu",
		Version:      "dev",
		LastSeenAt:   now,
	}
	// Agent in a different fleet group — must NOT match.
	server.agents["agent-US"] = Agent{
		ID:           "agent-US",
		NodeName:     "node-us-1",
		FleetGroupID: "us",
		Version:      "dev",
		LastSeenAt:   now,
	}
	// Agent without any fleet group — must NOT match a fleet-group assignment.
	server.agents["agent-solo"] = Agent{
		ID:           "agent-solo",
		NodeName:     "node-solo",
		FleetGroupID: "",
		Version:      "dev",
		LastSeenAt:   now,
	}
	server.mu.Unlock()

	clientID := "client-42"
	server.clientsMu.Lock()
	server.clients[clientID] = managedClient{
		ID:        clientID,
		Name:      "bob",
		Secret:    "0123456789abcdef0123456789abcdef",
		Enabled:   true,
		CreatedAt: now,
		UpdatedAt: now,
	}
	server.clientAssignments[clientID] = []managedClientAssignment{{
		ID:           "assign-fg",
		ClientID:     clientID,
		TargetType:   clientAssignmentTargetFleetGroup,
		FleetGroupID: "eu",
		CreatedAt:    now,
	}}
	server.clientsMu.Unlock()

	if got := server.resolveClientIDByName("agent-EU", "bob"); got != clientID {
		t.Fatalf("resolveClientIDByName(agent-EU, bob) = %q, want %q (fleet-group member should resolve)", got, clientID)
	}
	if got := server.resolveClientIDByName("agent-US", "bob"); got != "" {
		t.Fatalf("resolveClientIDByName(agent-US, bob) = %q, want empty (different fleet group must not match)", got)
	}
	if got := server.resolveClientIDByName("agent-solo", "bob"); got != "" {
		t.Fatalf("resolveClientIDByName(agent-solo, bob) = %q, want empty (agent without fleet group must not match)", got)
	}
	if got := server.resolveClientIDByName("agent-EU", "nonexistent"); got != "" {
		t.Fatalf("resolveClientIDByName(agent-EU, nonexistent) = %q, want empty", got)
	}

	// Sanity: direct agent assignments still work alongside fleet-group ones.
	directClientID := "client-direct"
	server.clientsMu.Lock()
	server.clients[directClientID] = managedClient{
		ID:        directClientID,
		Name:      "carol",
		Secret:    "0123456789abcdef0123456789abcdef",
		Enabled:   true,
		CreatedAt: now,
		UpdatedAt: now,
	}
	server.clientAssignments[directClientID] = []managedClientAssignment{{
		ID:         "assign-direct",
		ClientID:   directClientID,
		TargetType: clientAssignmentTargetAgent,
		AgentID:    "agent-US",
		CreatedAt:  now,
	}}
	server.clientsMu.Unlock()

	if got := server.resolveClientIDByName("agent-US", "carol"); got != directClientID {
		t.Fatalf("resolveClientIDByName(agent-US, carol) = %q, want %q (direct agent assignment)", got, directClientID)
	}
}
