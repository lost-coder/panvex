package server

import (
	"context"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/clients"
	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

// seedClientAndAgentRows persists a minimal client row and agent row to the
// DB so that client_usage writes (FK -> clients/agents, foreign_keys=ON)
// succeed, then refreshes the clients.Service mirror. Requires a repo-backed
// Service (testServerWithSQLite / mustNew with a real Store).
func seedClientAndAgentRows(t *testing.T, s *Server, clientID, agentID string, now time.Time) {
	t.Helper()
	ctx := context.Background()
	if err := s.clientsSvc.Save(ctx, managedClient{
		ID:        clients.ClientID(clientID),
		Name:      clientID,
		Enabled:   true,
		CreatedAt: now,
		UpdatedAt: now,
	}); err != nil {
		t.Fatalf("seedClientAndAgentRows: save client: %v", err)
	}
	if s.store != nil {
		if err := s.store.PutAgent(ctx, storage.AgentRecord{
			ID:         agentID,
			NodeName:   agentID,
			LastSeenAt: now,
		}); err != nil {
			t.Fatalf("seedClientAndAgentRows: put agent: %v", err)
		}
	}
}

// These helpers let tests seed and read the clients.Service mirror — the
// single owner of client/assignment/deployment/usage state after C1 removed
// the Server-owned client maps. They replace direct s.clients / s.clientUsage
// / s.clientDeployments map access from the pre-C1 tests.

// seedMirrorClient inserts (or overwrites) a client + its assignments +
// deployments into the Service mirror without touching the DB.
func seedMirrorClient(t *testing.T, s *Server, client managedClient, assignments []managedClientAssignment, deployments []managedClientDeployment) {
	t.Helper()
	s.clientsSvc.MirrorReplaceInMemory(client, assignments, deployments)
}

// seedMirrorDeployment inserts a single deployment for (clientID, agentID)
// into the mirror, preserving any existing deployments for the client.
func seedMirrorDeployment(t *testing.T, s *Server, clientID string, deployment managedClientDeployment) {
	t.Helper()
	client, err := s.clientsSvc.Get(context.Background(), clients.ClientID(clientID))
	if err != nil {
		client = managedClient{ID: clients.ClientID(clientID)}
	}
	assignments, deployments := s.clientsSvc.MirrorAssignmentsAndDeployments(clientID)
	replaced := false
	for i := range deployments {
		if deployments[i].AgentID == deployment.AgentID {
			deployments[i] = deployment
			replaced = true
		}
	}
	if !replaced {
		deployments = append(deployments, deployment)
	}
	s.clientsSvc.MirrorReplaceInMemory(client, assignments, deployments)
}

// seedMirrorUsage writes a usage row for (clientID, agentID) into the mirror
// (and the DB, since these tests use a repo-backed Service). Requires the
// Service to be wired with a Repository.
func seedMirrorUsage(t *testing.T, s *Server, clientID, agentID string, usage clientUsageSnapshot) {
	t.Helper()
	if err := s.clientsSvc.UpsertUsage(context.Background(), clients.Usage{
		ClientID:           clients.ClientID(clientID),
		AgentID:            agentID,
		TrafficUsedBytes:   usage.TrafficUsedBytes,
		UniqueIPsUsed:      usage.UniqueIPsUsed,
		ActiveTCPConns:     usage.ActiveTCPConns,
		ActiveUniqueIPs:    usage.ActiveUniqueIPs,
		QuotaUsedBytes:     usage.QuotaUsedBytes,
		QuotaLastResetUnix: usage.QuotaLastResetUnix,
		LastSeq:            usage.Seq,
		ObservedAt:         tsOrNow(usage.ObservedAt),
	}); err != nil {
		t.Fatalf("seedMirrorUsage: %v", err)
	}
}

func tsOrNow(ts time.Time) time.Time {
	if ts.IsZero() {
		return time.Now().UTC()
	}
	return ts
}

// mirrorUsage reads the usage entry (with P4 watermark) for
// (clientID, agentID) from the mirror.
func mirrorUsage(s *Server, clientID, agentID string) clients.MirrorUsageEntry {
	u, _ := s.clientsSvc.MirrorUsageEntryFor(clientID, agentID)
	return u
}

// mirrorDeployment reads the deployment for (clientID, agentID) from the mirror.
func mirrorDeployment(s *Server, clientID, agentID string) managedClientDeployment {
	d, _ := s.clientsSvc.MirrorDeployment(clientID, agentID)
	return d
}

// mirrorLastUsageSeq reads the per-agent usage seq cursor from the mirror.
func mirrorLastUsageSeq(s *Server, agentID string) uint64 {
	return s.clientsSvc.MirrorLastUsageSeq(agentID)
}
