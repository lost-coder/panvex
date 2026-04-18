package postgres

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

// These tests verify the ON DELETE CASCADE FKs introduced by
// db/migrations/postgres/0011_cascade_fk.sql (P2-DB-03 / DF-24 / M-F11).
// They require a live Postgres instance — see PANVEX_POSTGRES_TEST_DSN.

func openForCascade(t *testing.T) *Store {
	t.Helper()

	dsn := os.Getenv("PANVEX_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("PANVEX_POSTGRES_TEST_DSN is not set")
	}

	store, err := Open(dsn)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	if err := resetForTest(store); err != nil {
		t.Fatalf("resetForTest() error = %v", err)
	}

	return store
}

// TestCascadeDeleteUserRemovesSessions verifies sessions cascade-delete with
// their owning user.
func TestCascadeDeleteUserRemovesSessions(t *testing.T) {
	store := openForCascade(t)

	ctx := context.Background()
	user := storage.UserRecord{
		ID:           "user-cascade",
		Username:     "cascade-user",
		PasswordHash: "argon2id$hash",
		Role:         "admin",
		CreatedAt:    time.Date(2026, time.April, 18, 10, 0, 0, 0, time.UTC),
	}
	if err := store.PutUser(ctx, user); err != nil {
		t.Fatalf("PutUser() error = %v", err)
	}

	session := storage.SessionRecord{
		ID:        "sess-cascade",
		UserID:    user.ID,
		CreatedAt: time.Date(2026, time.April, 18, 10, 5, 0, 0, time.UTC),
	}
	if err := store.PutSession(ctx, session); err != nil {
		t.Fatalf("PutSession() error = %v", err)
	}

	if err := store.DeleteUser(ctx, user.ID); err != nil {
		t.Fatalf("DeleteUser() error = %v", err)
	}

	if _, err := store.GetSession(ctx, session.ID); err != storage.ErrNotFound {
		t.Fatalf("GetSession() after DeleteUser error = %v, want ErrNotFound (session should have cascaded)", err)
	}
}

// TestCascadeDeleteClientRemovesDeployments verifies ON DELETE CASCADE from
// clients → client_deployments and client_assignments when the parent row is
// hard-deleted via SQL.
func TestCascadeDeleteClientRemovesDeployments(t *testing.T) {
	store := openForCascade(t)

	ctx := context.Background()
	group := storage.FleetGroupRecord{
		ID:        "group-cascade",
		Name:      "Cascade",
		CreatedAt: time.Date(2026, time.April, 18, 11, 0, 0, 0, time.UTC),
	}
	if err := store.PutFleetGroup(ctx, group); err != nil {
		t.Fatalf("PutFleetGroup() error = %v", err)
	}
	agent := storage.AgentRecord{
		ID:           "agent-cascade",
		NodeName:     "node-cascade",
		FleetGroupID: group.ID,
		LastSeenAt:   time.Date(2026, time.April, 18, 11, 0, 0, 0, time.UTC),
	}
	if err := store.PutAgent(ctx, agent); err != nil {
		t.Fatalf("PutAgent() error = %v", err)
	}
	client := storage.ClientRecord{
		ID:               "client-cascade",
		Name:             "cascade-client",
		SecretCiphertext: "enc",
		UserADTag:        "0123456789abcdef0123456789abcdef",
		Enabled:          true,
		CreatedAt:        time.Date(2026, time.April, 18, 11, 5, 0, 0, time.UTC),
		UpdatedAt:        time.Date(2026, time.April, 18, 11, 5, 0, 0, time.UTC),
	}
	if err := store.PutClient(ctx, client); err != nil {
		t.Fatalf("PutClient() error = %v", err)
	}
	assignment := storage.ClientAssignmentRecord{
		ID:         "assign-cascade",
		ClientID:   client.ID,
		TargetType: "agent",
		AgentID:    agent.ID,
		CreatedAt:  time.Date(2026, time.April, 18, 11, 6, 0, 0, time.UTC),
	}
	if err := store.PutClientAssignment(ctx, assignment); err != nil {
		t.Fatalf("PutClientAssignment() error = %v", err)
	}
	deployment := storage.ClientDeploymentRecord{
		ClientID:         client.ID,
		AgentID:          agent.ID,
		DesiredOperation: "client.create",
		Status:           "succeeded",
		UpdatedAt:        time.Date(2026, time.April, 18, 11, 7, 0, 0, time.UTC),
	}
	if err := store.PutClientDeployment(ctx, deployment); err != nil {
		t.Fatalf("PutClientDeployment() error = %v", err)
	}

	if _, err := store.sqlDB.ExecContext(ctx, `DELETE FROM clients WHERE id = $1`, client.ID); err != nil {
		t.Fatalf("DELETE FROM clients error = %v", err)
	}

	assignments, err := store.ListClientAssignments(ctx, client.ID)
	if err != nil {
		t.Fatalf("ListClientAssignments() error = %v", err)
	}
	if len(assignments) != 0 {
		t.Fatalf("ListClientAssignments() after client delete = %d rows, want 0", len(assignments))
	}

	deployments, err := store.ListClientDeployments(ctx, client.ID)
	if err != nil {
		t.Fatalf("ListClientDeployments() error = %v", err)
	}
	if len(deployments) != 0 {
		t.Fatalf("ListClientDeployments() after client delete = %d rows, want 0", len(deployments))
	}
}

// TestCascadeDeleteAgentRemovesMetricSnapshots verifies ON DELETE CASCADE
// from agents → metric_snapshots (replaces the non-cascading FK shipped in
// 0001_init.sql on Postgres).
func TestCascadeDeleteAgentRemovesMetricSnapshots(t *testing.T) {
	store := openForCascade(t)

	ctx := context.Background()
	agent := storage.AgentRecord{
		ID:         "agent-metric-cascade",
		NodeName:   "node-metric",
		LastSeenAt: time.Date(2026, time.April, 18, 12, 0, 0, 0, time.UTC),
	}
	if err := store.PutAgent(ctx, agent); err != nil {
		t.Fatalf("PutAgent() error = %v", err)
	}

	snap := storage.MetricSnapshotRecord{
		ID:         "metric-cascade-001",
		AgentID:    agent.ID,
		CapturedAt: time.Date(2026, time.April, 18, 12, 1, 0, 0, time.UTC),
		Values:     map[string]uint64{"cpu": 25},
	}
	if err := store.AppendMetricSnapshot(ctx, snap); err != nil {
		t.Fatalf("AppendMetricSnapshot() error = %v", err)
	}

	if err := store.DeleteAgent(ctx, agent.ID); err != nil {
		t.Fatalf("DeleteAgent() error = %v", err)
	}

	var remaining int
	if err := store.sqlDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM metric_snapshots WHERE agent_id = $1`, agent.ID).Scan(&remaining); err != nil {
		t.Fatalf("count metric_snapshots error = %v", err)
	}
	if remaining != 0 {
		t.Fatalf("metric_snapshots remaining after DeleteAgent = %d, want 0", remaining)
	}
}
