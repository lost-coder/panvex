package server

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
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
	fleetGroupID := seedTestFleetGroup(t, store, "default", now.Add(-time.Minute))
	agentID := "agent-discover-1"
	if err := store.PutAgent(ctx, storage.AgentRecord{
		ID:           agentID,
		NodeName:     "node-A",
		FleetGroupID: fleetGroupID,
		Version:      "dev",
		LastSeenAt:   now.Add(-time.Minute),
	}); err != nil {
		t.Fatalf("PutAgent() error = %v", err)
	}

	server := New(Options{
		LoginTimingFloor: -1,
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
	fleetGroupID := seedTestFleetGroup(t, store, "default", now.Add(-time.Minute))
	agentID := "agent-discover-2"
	if err := store.PutAgent(ctx, storage.AgentRecord{
		ID:           agentID,
		NodeName:     "node-B",
		FleetGroupID: fleetGroupID,
		Version:      "dev",
		LastSeenAt:   now.Add(-time.Minute),
	}); err != nil {
		t.Fatalf("PutAgent() error = %v", err)
	}

	server := New(Options{
		LoginTimingFloor: -1,
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

// TestAdoptDiscoveredClientConcurrentIsAtomic verifies that concurrent
// adopt calls on the same discovered record produce exactly ONE managed
// client — the TOCTOU bug covered by P2-LOG-03 (finding L-11 / M-C5).
// Before the fix, N goroutines could all read the pending_review record,
// pass the status check, and each create a managed client with the same
// name. With adoptMu in place, only one goroutine wins; the others must
// observe the flipped status and return ErrAlreadyAdopted.
func TestAdoptDiscoveredClientConcurrentIsAtomic(t *testing.T) {
	now := time.Date(2026, time.April, 17, 12, 0, 0, 0, time.UTC)

	store, err := sqlite.Open(filepath.Join(t.TempDir(), "panvex.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	fleetGroupID := seedTestFleetGroup(t, store, "default", now.Add(-time.Minute))
	agentID := "agent-adopt-race-1"
	if err := store.PutAgent(ctx, storage.AgentRecord{
		ID:           agentID,
		NodeName:     "node-A",
		FleetGroupID: fleetGroupID,
		Version:      "dev",
		LastSeenAt:   now.Add(-time.Minute),
	}); err != nil {
		t.Fatalf("PutAgent() error = %v", err)
	}

	server := New(Options{
		LoginTimingFloor: -1,
		Now:   func() time.Time { return now },
		Store: store,
	})
	defer server.Close()

	discoveredID := "discovered-1"
	if err := store.PutDiscoveredClient(ctx, storage.DiscoveredClientRecord{
		ID:           discoveredID,
		AgentID:      agentID,
		ClientName:   "external-charlie",
		Secret:       "3333333333333333cccccccccccccccc",
		Status:       discoveredClientStatusPendingReview,
		DiscoveredAt: now,
		UpdatedAt:    now,
	}); err != nil {
		t.Fatalf("PutDiscoveredClient() error = %v", err)
	}

	const workers = 5
	var (
		wg             sync.WaitGroup
		mu             sync.Mutex
		successes      int
		alreadyAdopted int
		otherErrs      []error
		createdIDs     []string
	)

	start := make(chan struct{})
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			<-start
			client, err := server.adoptDiscoveredClient(ctx, discoveredID, "operator-1", now)
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				successes++
				createdIDs = append(createdIDs, client.ID)
			case errors.Is(err, ErrAlreadyAdopted):
				alreadyAdopted++
			default:
				otherErrs = append(otherErrs, err)
			}
		}()
	}
	close(start)
	wg.Wait()

	if len(otherErrs) != 0 {
		t.Fatalf("unexpected errors from concurrent adopt: %v", otherErrs)
	}
	if successes != 1 {
		t.Fatalf("concurrent adopt: successes = %d, want 1 (only one goroutine must create the managed client)", successes)
	}
	if alreadyAdopted != workers-1 {
		t.Fatalf("concurrent adopt: alreadyAdopted = %d, want %d (the other goroutines must observe the flipped status)", alreadyAdopted, workers-1)
	}

	// Verify exactly one managed client with this name exists.
	server.clientsMu.RLock()
	var matching []managedClient
	for _, c := range server.clients {
		if c.DeletedAt == nil && c.Name == "external-charlie" {
			matching = append(matching, c)
		}
	}
	server.clientsMu.RUnlock()
	if len(matching) != 1 {
		t.Fatalf("managed clients named %q: got %d, want 1", "external-charlie", len(matching))
	}
	if len(createdIDs) != 1 || createdIDs[0] != matching[0].ID {
		t.Fatalf("createdIDs = %v, matching[0].ID = %q (must agree; only the winner created the client)", createdIDs, matching[0].ID)
	}

	// Discovered record must be in adopted status.
	dc, err := store.GetDiscoveredClient(ctx, discoveredID)
	if err != nil {
		t.Fatalf("GetDiscoveredClient() error = %v", err)
	}
	if dc.Status != discoveredClientStatusAdopted {
		t.Fatalf("discovered status = %q, want %q", dc.Status, discoveredClientStatusAdopted)
	}
}

// TestMergeAdoptNoTOCTOU verifies P2-LOG-04 / L-12: concurrent merge-adopts
// racing against each other cannot silently overwrite state. Before the
// fix, mergeAdoptIntoExistingClient released clientsMu.RUnlock() before
// calling replaceClientStateWithContext — two merges racing over the same
// existing client could each snapshot the old assignment list and one
// would clobber the other's addition.
//
// Two discovered records with the SAME (name, secret) represent the same
// Telemt user reported on two different nodes, so the product semantics
// are: the winning merge folds in BOTH agents at once via the sibling
// scan, the loser sees its discovered row already flipped to "adopted"
// by markDuplicateDiscoveredClientsAdopted and returns ErrAlreadyAdopted
// cleanly. Under adoptMu the loser's observation is deterministic —
// there must be no in-between window where a partially-applied merge is
// visible. The end state must contain assignments+deployments for both
// agents (added in one shot by the winner), with both discovered
// records marked adopted.
func TestMergeAdoptNoTOCTOU(t *testing.T) {
	now := time.Date(2026, time.April, 17, 12, 0, 0, 0, time.UTC)

	store, err := sqlite.Open(filepath.Join(t.TempDir(), "panvex.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
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
			t.Fatalf("PutAgent(%q) error = %v", id, err)
		}
	}

	server := New(Options{
		LoginTimingFloor: -1,
		Now:   func() time.Time { return now },
		Store: store,
	})
	defer server.Close()

	// Pre-seed an already-adopted managed client that both subsequent
	// discovered records (on agentA and agentB) will match on
	// (name, secret). This drives the merge-adopt code path.
	clientName := "external-dave"
	clientSecret := "4444444444444444dddddddddddddddd"
	existing := managedClient{
		ID:        server.nextClientID(),
		Name:      clientName,
		Secret:    clientSecret,
		Enabled:   true,
		CreatedAt: now.Add(-time.Hour),
		UpdatedAt: now.Add(-time.Hour),
	}
	// Zero existing assignments/deployments — the merges should each add
	// one and both must be present at the end.
	if err := server.replaceClientStateWithContext(ctx, existing, nil, nil); err != nil {
		t.Fatalf("replaceClientStateWithContext() error = %v", err)
	}

	// Two discovered records on two different agents, same name+secret.
	discoveredA := "discovered-merge-A"
	discoveredB := "discovered-merge-B"
	for _, tc := range []struct {
		id      string
		agentID string
	}{
		{discoveredA, agentA},
		{discoveredB, agentB},
	} {
		if err := store.PutDiscoveredClient(ctx, storage.DiscoveredClientRecord{
			ID:           tc.id,
			AgentID:      tc.agentID,
			ClientName:   clientName,
			Secret:       clientSecret,
			Status:       discoveredClientStatusPendingReview,
			DiscoveredAt: now,
			UpdatedAt:    now,
		}); err != nil {
			t.Fatalf("PutDiscoveredClient(%q) error = %v", tc.id, err)
		}
	}

	// Fire the two merges concurrently.
	var (
		wg   sync.WaitGroup
		errs [2]error
	)
	wg.Add(2)
	start := make(chan struct{})
	go func() {
		defer wg.Done()
		<-start
		_, errs[0] = server.adoptDiscoveredClient(ctx, discoveredA, "operator-1", now)
	}()
	go func() {
		defer wg.Done()
		<-start
		_, errs[1] = server.adoptDiscoveredClient(ctx, discoveredB, "operator-1", now)
	}()
	close(start)
	wg.Wait()

	var okCount, alreadyCount int
	for i, err := range errs {
		switch {
		case err == nil:
			okCount++
		case errors.Is(err, ErrAlreadyAdopted):
			alreadyCount++
		default:
			t.Fatalf("merge-adopt #%d: unexpected error = %v", i, err)
		}
	}
	if okCount != 1 || alreadyCount != 1 {
		t.Fatalf("merge outcomes: ok=%d already=%d, want ok=1 already=1 (one winner, one sibling flipped by markDuplicate)", okCount, alreadyCount)
	}

	// The winner pulled in both the primary record AND its sibling in a
	// single call, so the existing client should end up with assignments
	// for both agents — without any "half-applied" intermediate state
	// from the loser. Before the merge-adopt sibling fix, the panel
	// would have ended at a 1-agent client and the operator had to
	// follow up with a PUT to extend agent_ids (the bug that drove
	// frontend rate-limit and ad_tag-generation issues during bulk
	// imports).
	server.clientsMu.RLock()
	assignments := append([]managedClientAssignment(nil), server.clientAssignments[existing.ID]...)
	deployments := server.clientDeployments[existing.ID]
	server.clientsMu.RUnlock()

	if len(assignments) != 2 {
		t.Fatalf("assignments on existing client: got %d, want 2 (winner adds primary+sibling in one shot) %+v", len(assignments), assignments)
	}
	gotAgents := map[string]struct{}{}
	for _, a := range assignments {
		gotAgents[a.AgentID] = struct{}{}
	}
	for _, want := range []string{agentA, agentB} {
		if _, ok := gotAgents[want]; !ok {
			t.Fatalf("assignments missing agent %q: %+v", want, assignments)
		}
		if _, ok := deployments[want]; !ok {
			t.Fatalf("deployments missing agent %q: %+v", want, deployments)
		}
	}
	if len(deployments) != 2 {
		t.Fatalf("deployments on existing client: got %d, want 2 %+v", len(deployments), deployments)
	}

	// Both discovered records flipped to adopted (winner by direct
	// update, loser by markDuplicateDiscoveredClientsAdopted).
	for _, id := range []string{discoveredA, discoveredB} {
		dc, err := store.GetDiscoveredClient(ctx, id)
		if err != nil {
			t.Fatalf("GetDiscoveredClient(%q) error = %v", id, err)
		}
		if dc.Status != discoveredClientStatusAdopted {
			t.Fatalf("discovered %q status = %q, want %q", id, dc.Status, discoveredClientStatusAdopted)
		}
	}
}

// TestRestoreStoredClients_RehydratesUsageFromDiscovered locks in the
// fix for the "adopted client shows 0 B traffic after panel restart"
// bug. clientUsage is an in-memory map; on restart we lose the seed
// planted during adopt. Without rehydration from the persisted
// discovered_clients snapshot, the UI reports 0 until the next agent
// tick — and because agents stream deltas, that tick only restores a
// polling interval of traffic, not the lifetime total.
func TestRestoreStoredClients_RehydratesUsageFromDiscovered(t *testing.T) {
	now := time.Date(2026, time.April, 24, 12, 0, 0, 0, time.UTC)

	store, err := sqlite.Open(filepath.Join(t.TempDir(), "panvex.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	fleetGroupID := seedTestFleetGroup(t, store, "default", now.Add(-time.Minute))
	agentID := "agent-rehydrate-1"
	if err := store.PutAgent(ctx, storage.AgentRecord{
		ID:           agentID,
		NodeName:     "node-A",
		FleetGroupID: fleetGroupID,
		Version:      "dev",
		LastSeenAt:   now.Add(-time.Minute),
	}); err != nil {
		t.Fatalf("PutAgent() error = %v", err)
	}

	clientID := "client-rehydrate-1"
	if err := store.PutClient(ctx, storage.ClientRecord{
		ID:               clientID,
		Name:             "external-delta",
		SecretCiphertext: "4444444444444444dddddddddddddddd",
		Enabled:          true,
		CreatedAt:        now,
		UpdatedAt:        now,
	}); err != nil {
		t.Fatalf("PutClient() error = %v", err)
	}
	if err := store.PutClientAssignment(ctx, storage.ClientAssignmentRecord{
		ID:         "assign-rehydrate-1",
		ClientID:   clientID,
		TargetType: clientAssignmentTargetAgent,
		AgentID:    agentID,
		CreatedAt:  now,
	}); err != nil {
		t.Fatalf("PutClientAssignment() error = %v", err)
	}
	if err := store.PutDiscoveredClient(ctx, storage.DiscoveredClientRecord{
		ID:                 "discovered-rehydrate-1",
		AgentID:            agentID,
		ClientName:         "external-delta",
		Secret:             "4444444444444444dddddddddddddddd",
		Status:             discoveredClientStatusAdopted,
		TotalOctets:        9001,
		CurrentConnections: 2,
		ActiveUniqueIPs:    3,
		DiscoveredAt:       now.Add(-time.Hour),
		UpdatedAt:          now.Add(-time.Minute),
	}); err != nil {
		t.Fatalf("PutDiscoveredClient() error = %v", err)
	}

	// Fresh Server simulates a panel restart: the in-memory clientUsage
	// map is empty. restoreStoredClients must repopulate it from the
	// persisted discovered_clients row.
	server := New(Options{
		LoginTimingFloor: -1,
		Now:   func() time.Time { return now },
		Store: store,
	})
	defer server.Close()
	if err := server.restoreStoredClients(); err != nil {
		t.Fatalf("restoreStoredClients() error = %v", err)
	}

	server.clientsMu.RLock()
	got := server.clientUsage[clientID][agentID]
	server.clientsMu.RUnlock()

	if got.TrafficUsedBytes != 9001 {
		t.Fatalf("TrafficUsedBytes after restore = %d, want 9001 (seeded from discovered_clients.total_octets)", got.TrafficUsedBytes)
	}
	if got.ActiveUniqueIPs != 3 {
		t.Fatalf("ActiveUniqueIPs after restore = %d, want 3", got.ActiveUniqueIPs)
	}
}

// TestRestoreStoredClients_PrefersPersistedUsage verifies the primary
// rehydration path: once client_usage has been written-through by an
// agent tick, restart must read from that table, not fall back to
// the discovered_clients snapshot. A drifted discovered row here
// simulates an agent going offline before its latest totals reached
// the discovered table — the persisted client_usage must still win.
func TestRestoreStoredClients_PrefersPersistedUsage(t *testing.T) {
	now := time.Date(2026, time.April, 24, 13, 0, 0, 0, time.UTC)

	store, err := sqlite.Open(filepath.Join(t.TempDir(), "panvex.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	fleetGroupID := seedTestFleetGroup(t, store, "default", now.Add(-time.Minute))
	agentID := "agent-persisted-1"
	if err := store.PutAgent(ctx, storage.AgentRecord{
		ID:           agentID,
		NodeName:     "node-A",
		FleetGroupID: fleetGroupID,
		Version:      "dev",
		LastSeenAt:   now.Add(-time.Minute),
	}); err != nil {
		t.Fatalf("PutAgent() error = %v", err)
	}

	clientID := "client-persisted-1"
	if err := store.PutClient(ctx, storage.ClientRecord{
		ID:               clientID,
		Name:             "external-echo",
		SecretCiphertext: "5555555555555555eeeeeeeeeeeeeeee",
		Enabled:          true,
		CreatedAt:        now,
		UpdatedAt:        now,
	}); err != nil {
		t.Fatalf("PutClient() error = %v", err)
	}
	if err := store.PutClientAssignment(ctx, storage.ClientAssignmentRecord{
		ID:         "assign-persisted-1",
		ClientID:   clientID,
		TargetType: clientAssignmentTargetAgent,
		AgentID:    agentID,
		CreatedAt:  now,
	}); err != nil {
		t.Fatalf("PutClientAssignment() error = %v", err)
	}

	// Persisted total — this is what must win on restart.
	if err := store.UpsertClientUsage(ctx, storage.ClientUsageRecord{
		ClientID:         clientID,
		AgentID:          agentID,
		TrafficUsedBytes: 777_777,
		UniqueIPsUsed:    12,
		ActiveTCPConns:   5,
		ActiveUniqueIPs:  7,
		LastSeq:          42,
		ObservedAt:       now,
	}); err != nil {
		t.Fatalf("UpsertClientUsage() error = %v", err)
	}

	// Stale discovered snapshot — would be picked up by the fallback,
	// but persisted usage should take precedence.
	if err := store.PutDiscoveredClient(ctx, storage.DiscoveredClientRecord{
		ID:           "discovered-persisted-1",
		AgentID:      agentID,
		ClientName:   "external-echo",
		Secret:       "5555555555555555eeeeeeeeeeeeeeee",
		Status:       discoveredClientStatusAdopted,
		TotalOctets:  111, // drifted — must be ignored
		DiscoveredAt: now.Add(-time.Hour),
		UpdatedAt:    now.Add(-time.Hour),
	}); err != nil {
		t.Fatalf("PutDiscoveredClient() error = %v", err)
	}

	server := New(Options{
		LoginTimingFloor: -1,
		Now:   func() time.Time { return now },
		Store: store,
	})
	defer server.Close()
	if err := server.restoreStoredClients(); err != nil {
		t.Fatalf("restoreStoredClients() error = %v", err)
	}

	server.clientsMu.RLock()
	got := server.clientUsage[clientID][agentID]
	seq := server.lastUsageSeq[agentID]
	server.clientsMu.RUnlock()

	if got.TrafficUsedBytes != 777_777 {
		t.Fatalf("TrafficUsedBytes after restore = %d, want 777777 (from client_usage, not discovered)", got.TrafficUsedBytes)
	}
	if seq != 42 {
		t.Fatalf("lastUsageSeq[%q] = %d, want 42 (carried over from persisted row)", agentID, seq)
	}
}
