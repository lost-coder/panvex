package server

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
	"github.com/lost-coder/panvex/internal/controlplane/storage/sqlite"
	"github.com/lost-coder/panvex/internal/gatewayrpc"
)

// TestUpsertDiscoveredClientDedupes verifies that repeated FULL_SNAPSHOT
// observations of the same (agent_id, client_name) produce exactly one
// discovered_clients row — the bug covered by P2-LOG-02 (finding L-10 / M-C4).
// Previously every agent reconnect burned a new sequence ID and appended a
// new pending_review row, so the pending-review list grew unbounded.
func TestUpsertDiscoveredClientDedupes(t *testing.T) {
	now := time.Date(2026, time.April, 17, 12, 0, 0, 0, time.UTC)

	store, err := sqlite.Open(filepath.Join(t.TempDir(), "panvex.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	if err := store.PutFleetGroup(ctx, storage.FleetGroupRecord{
		ID:        "default",
		Name:      "Default",
		CreatedAt: now.Add(-time.Minute),
	}); err != nil {
		t.Fatalf("PutFleetGroup() error = %v", err)
	}
	agentID := "agent-discover-1"
	if err := store.PutAgent(ctx, storage.AgentRecord{
		ID:           agentID,
		NodeName:     "node-A",
		FleetGroupID: "default",
		Version:      "dev",
		LastSeenAt:   now.Add(-time.Minute),
	}); err != nil {
		t.Fatalf("PutAgent() error = %v", err)
	}

	server := New(Options{
		Now:   func() time.Time { return now },
		Store: store,
	})
	defer server.Close()

	record := &gatewayrpc.ClientDetailRecord{
		ClientName:         "external-alice",
		Secret:             "1111111111111111aaaaaaaaaaaaaaaa",
		TotalOctets:        1024,
		CurrentConnections: 1,
		ActiveUniqueIps:    1,
		ConnectionLink:     "tg://proxy?...",
		MaxTcpConns:        0,
		MaxUniqueIps:       0,
		DataQuotaBytes:     0,
		Expiration:         "",
	}

	// First observation -> one new pending_review row.
	server.upsertDiscoveredClient(ctx, agentID, record, now)

	// Simulate a later FULL_SNAPSHOT with refreshed traffic counters.
	record2 := &gatewayrpc.ClientDetailRecord{
		ClientName:         record.ClientName,
		Secret:             record.Secret,
		TotalOctets:        2048, // increased
		CurrentConnections: 3,
		ActiveUniqueIps:    2,
		ConnectionLink:     record.ConnectionLink,
	}
	later := now.Add(5 * time.Minute)
	server.upsertDiscoveredClient(ctx, agentID, record2, later)

	// And a third time — mimics another agent reconnect.
	server.upsertDiscoveredClient(ctx, agentID, record2, later.Add(time.Minute))

	got, err := server.listDiscoveredClients(ctx)
	if err != nil {
		t.Fatalf("listDiscoveredClients() error = %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len(listDiscoveredClients()) = %d, want 1 (dedupe on agent_id, client_name)", len(got))
	}
	if got[0].TotalOctets != 2048 {
		t.Fatalf("TotalOctets = %d, want 2048 (updated in place)", got[0].TotalOctets)
	}
	if got[0].CurrentConnections != 3 {
		t.Fatalf("CurrentConnections = %d, want 3", got[0].CurrentConnections)
	}
	if got[0].Status != discoveredClientStatusPendingReview {
		t.Fatalf("Status = %q, want %q", got[0].Status, discoveredClientStatusPendingReview)
	}
	if !got[0].DiscoveredAt.Equal(now.UTC()) {
		t.Fatalf("DiscoveredAt = %s, want %s (preserved on update)", got[0].DiscoveredAt, now.UTC())
	}
	if got[0].UpdatedAt.Before(later.UTC()) {
		t.Fatalf("UpdatedAt = %s, want >= %s (refreshed on update)", got[0].UpdatedAt, later.UTC())
	}
}

// TestUpsertDiscoveredClientPreservesIgnoredStatus ensures a later reconcile
// cannot resurrect an ignored row back to pending_review.
func TestUpsertDiscoveredClientPreservesIgnoredStatus(t *testing.T) {
	now := time.Date(2026, time.April, 17, 12, 0, 0, 0, time.UTC)

	store, err := sqlite.Open(filepath.Join(t.TempDir(), "panvex.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	if err := store.PutFleetGroup(ctx, storage.FleetGroupRecord{
		ID:        "default",
		Name:      "Default",
		CreatedAt: now.Add(-time.Minute),
	}); err != nil {
		t.Fatalf("PutFleetGroup() error = %v", err)
	}
	agentID := "agent-discover-2"
	if err := store.PutAgent(ctx, storage.AgentRecord{
		ID:           agentID,
		NodeName:     "node-B",
		FleetGroupID: "default",
		Version:      "dev",
		LastSeenAt:   now.Add(-time.Minute),
	}); err != nil {
		t.Fatalf("PutAgent() error = %v", err)
	}

	server := New(Options{
		Now:   func() time.Time { return now },
		Store: store,
	})
	defer server.Close()

	record := &gatewayrpc.ClientDetailRecord{
		ClientName: "external-bob",
		Secret:     "2222222222222222bbbbbbbbbbbbbbbb",
	}
	server.upsertDiscoveredClient(ctx, agentID, record, now)

	existing, err := server.listDiscoveredClients(ctx)
	if err != nil {
		t.Fatalf("listDiscoveredClients() error = %v", err)
	}
	if len(existing) != 1 {
		t.Fatalf("precondition: want 1 discovered client, got %d", len(existing))
	}
	if err := store.UpdateDiscoveredClientStatus(ctx, existing[0].ID, discoveredClientStatusIgnored, now); err != nil {
		t.Fatalf("UpdateDiscoveredClientStatus() error = %v", err)
	}

	// Another reconcile pass arrives with the same (agent, name). The upsert
	// must NOT flip the status back to pending_review.
	server.upsertDiscoveredClient(ctx, agentID, record, now.Add(time.Minute))

	got, err := server.listDiscoveredClients(ctx)
	if err != nil {
		t.Fatalf("listDiscoveredClients() error = %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len(listDiscoveredClients()) = %d, want 1", len(got))
	}
	if got[0].Status != discoveredClientStatusIgnored {
		t.Fatalf("Status = %q, want %q (ignored must not be resurrected)", got[0].Status, discoveredClientStatusIgnored)
	}
}
