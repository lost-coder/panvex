package storagetest

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

// runAgentsContract extracts the agent and instance snapshot contract blocks from
// the historic store_contract.go monolith (R-Q-18). RunStoreContract
// dispatches into it so each backend exercises the same coverage.
func runAgentsContract(t *testing.T, open OpenStore) {
	t.Helper()

	t.Run("EarliestAgentCertExpiry returns min across agents", func(t *testing.T) {
		store := open(t)
		defer store.Close()

		ctx := context.Background()
		// Пусто — nil без ошибки.
		got, err := store.EarliestAgentCertExpiry(ctx)
		if err != nil {
			t.Fatalf("EarliestAgentCertExpiry(empty): %v", err)
		}
		if got != nil {
			t.Fatalf("empty store: got %v, want nil", got)
		}

		ts := time.Date(2026, time.July, 2, 12, 0, 0, 0, time.UTC)
		early := ts.Add(24 * time.Hour)
		late := ts.Add(240 * time.Hour)
		agents := []storage.AgentRecord{
			{ID: "a-late", NodeName: "late", Version: "v1", LastSeenAt: ts, CertExpiresAt: &late},
			{ID: "a-early", NodeName: "early", Version: "v1", LastSeenAt: ts, CertExpiresAt: &early},
			{ID: "a-none", NodeName: "none", Version: "v1", LastSeenAt: ts}, // NULL — игнорируется
		}
		if err := store.PutAgentsBulk(ctx, agents); err != nil {
			t.Fatalf("seed agents: %v", err)
		}

		got, err = store.EarliestAgentCertExpiry(ctx)
		if err != nil {
			t.Fatalf("EarliestAgentCertExpiry: %v", err)
		}
		if got == nil || !got.Equal(early) {
			t.Fatalf("earliest = %v, want %v", got, early)
		}
	})

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
			Connections:       42,
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

		// IN-M3: the connections counter must round-trip through storage.
		if instances[0].Connections != instance.Connections {
			t.Fatalf("ListInstances()[0].Connections = %d, want %d", instances[0].Connections, instance.Connections)
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
		seedDiscoveredClientForCascade(t, ctx, store, "discovered-deregister", agent.ID, "stranger", agent.LastSeenAt)

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
		// via raw SQL.
		if n, ok := discoveredRowsForAgent(t, store, agent.ID); ok && n != 0 {
			t.Fatalf("discovered_clients for deregistered agent = %d rows, want 0 (cascade did not purge)", n)
		}
	})

	// TestUpdateAgentCertPinRoundTrip verifies that an agent's SPKI pin
	// survives a Put/Get round-trip on every backend (S-02).
	t.Run("UpdateAgentCertPin round-trip", func(t *testing.T) {
		store := open(t)
		defer store.Close()

		ctx := context.Background()
		group := storage.FleetGroupRecord{
			ID:        testFleetGroupID,
			Name:      "Default",
			CreatedAt: time.Date(2026, time.March, 15, 8, 20, 0, 0, time.UTC),
		}
		agent := storage.AgentRecord{
			ID:           "agent-pin-test",
			NodeName:     "node-pin",
			FleetGroupID: group.ID,
			Version:      "dev",
			LastSeenAt:   time.Date(2026, time.March, 15, 8, 25, 0, 0, time.UTC),
		}
		if err := store.PutFleetGroup(ctx, group); err != nil {
			t.Fatalf("PutFleetGroup() error = %v", err)
		}
		if err := store.PutAgent(ctx, agent); err != nil {
			t.Fatalf("PutAgent() error = %v", err)
		}

		pin := bytes.Repeat([]byte{0xAB}, 32)
		if err := store.UpdateAgentCertPin(ctx, agent.ID, pin); err != nil {
			t.Fatalf("UpdateAgentCertPin() error = %v", err)
		}
		got, err := store.GetAgentCertPin(ctx, agent.ID)
		if err != nil {
			t.Fatalf("GetAgentCertPin() error = %v", err)
		}
		if !bytes.Equal(got, pin) {
			t.Fatalf("GetAgentCertPin() = %x, want %x", got, pin)
		}
	})

	// TestGetAgentCertPinUnknownAgent verifies the missing-agent error path.
	t.Run("GetAgentCertPin unknown agent returns ErrNotFound", func(t *testing.T) {
		store := open(t)
		defer store.Close()

		ctx := context.Background()
		_, err := store.GetAgentCertPin(ctx, "no-such-agent")
		if !errors.Is(err, storage.ErrNotFound) {
			t.Fatalf("GetAgentCertPin() err = %v, want ErrNotFound", err)
		}
	})

	t.Run("UpdateAgentCertPin unknown agent returns ErrNotFound", func(t *testing.T) {
		store := open(t)
		defer store.Close()
		ctx := context.Background()
		pin := bytes.Repeat([]byte{0xCD}, 32)
		err := store.UpdateAgentCertPin(ctx, "no-such-agent", pin)
		if !errors.Is(err, storage.ErrNotFound) {
			t.Fatalf("err = %v, want ErrNotFound", err)
		}
	})

	t.Run("delete expired agent revocations keeps live rows", func(t *testing.T) {
		store := open(t)
		defer store.Close()

		ctx := context.Background()
		now := time.Date(2026, time.June, 1, 12, 0, 0, 0, time.UTC)
		expired := storage.AgentRevocationRecord{
			AgentID:       "agent-expired",
			RevokedAt:     now.Add(-72 * time.Hour),
			CertExpiresAt: now.Add(-time.Hour),
		}
		live := storage.AgentRevocationRecord{
			AgentID:       "agent-live",
			RevokedAt:     now.Add(-time.Hour),
			CertExpiresAt: now.Add(24 * time.Hour),
		}
		for _, rec := range []storage.AgentRevocationRecord{expired, live} {
			if err := store.PutAgentRevocation(ctx, rec); err != nil {
				t.Fatalf("PutAgentRevocation(%s) error = %v", rec.AgentID, err)
			}
		}

		deleted, err := store.DeleteExpiredAgentRevocations(ctx, now)
		if err != nil {
			t.Fatalf("DeleteExpiredAgentRevocations() error = %v", err)
		}
		if deleted != 1 {
			t.Fatalf("DeleteExpiredAgentRevocations() = %d, want 1", deleted)
		}
		remaining, err := store.ListAgentRevocations(ctx)
		if err != nil {
			t.Fatalf("ListAgentRevocations() error = %v", err)
		}
		if len(remaining) != 1 || remaining[0].AgentID != "agent-live" {
			t.Fatalf("remaining revocations = %+v, want only agent-live", remaining)
		}
	})
}

// seedDiscoveredClientForCascade inserts a minimal discovered_clients row
// directly. The typed PutDiscoveredClient store method was removed in the P5
// ballast cleanup, so the FK-cascade contract seeds the row via raw SQL. The
// statement is dialect-branched because SQLite names the timestamp columns
// discovered_at_unix/updated_at_unix while Postgres uses discovered_at/
// updated_at. All other columns rely on their schema defaults. Backends that
// do not expose a raw *sql.DB (the in-memory contract double) have no
// discovered_clients table and are skipped — they don't enforce real FKs, so
// the cascade assertion is meaningful only on the SQL backends.
func seedDiscoveredClientForCascade(t *testing.T, ctx context.Context, store storage.MigrationStore, id, agentID, clientName string, at time.Time) {
	t.Helper()
	h, ok := store.(dbHandleStore)
	if !ok {
		return
	}
	db := h.DB()
	var err error
	if isPostgresHandle(db) {
		_, err = db.ExecContext(ctx, `
			INSERT INTO discovered_clients (id, agent_id, client_name, discovered_at, updated_at)
			VALUES ($1, $2, $3, $4, $5)
		`, id, agentID, clientName, at, at)
	} else {
		_, err = db.ExecContext(ctx, `
			INSERT INTO discovered_clients (id, agent_id, client_name, discovered_at_unix, updated_at_unix)
			VALUES (?, ?, ?, ?, ?)
		`, id, agentID, clientName, at.UTC().Unix(), at.UTC().Unix())
	}
	if err != nil {
		t.Fatalf("seed discovered_clients: %v", err)
	}
}

// discoveredRowsForAgent counts discovered_clients rows for an agent via raw
// SQL, replacing the removed ListDiscoveredClientsByAgent store method. The
// second return reports whether the backend exposes a raw *sql.DB; when false
// (in-memory double) callers skip the cascade assertion.
func discoveredRowsForAgent(t *testing.T, store storage.MigrationStore, agentID string) (int, bool) {
	t.Helper()
	h, ok := store.(dbHandleStore)
	if !ok {
		return 0, false
	}
	db := h.DB()
	query := "SELECT COUNT(*) FROM discovered_clients WHERE agent_id = ?"
	if isPostgresHandle(db) {
		query = "SELECT COUNT(*) FROM discovered_clients WHERE agent_id = $1"
	}
	var n int
	if err := db.QueryRowContext(context.Background(), query, agentID).Scan(&n); err != nil {
		t.Fatalf("count discovered rows: %v", err)
	}
	return n, true
}
