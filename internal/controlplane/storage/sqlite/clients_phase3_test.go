// Phase 3 (reset-quota) storage tests: verify that the new
// last_reset_epoch_secs column persists across PutClientDeployment /
// ListClientDeployments and the repository-layer equivalent.

package sqlite

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/clients"
	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

// TestPutListClientDeploymentRoundTripsLastReset covers the legacy
// raw-SQL Store path used by MigrationStore + CLI tools (the
// production hot path runs through clientsRepository, exercised by
// the test below).
func TestPutListClientDeploymentRoundTripsLastReset(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "phase3.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	ctx := context.Background()
	now := time.Date(2026, time.May, 15, 12, 0, 0, 0, time.UTC)
	if err := store.PutClient(ctx, storage.ClientRecord{
		ID:               "client-1",
		Name:             "alice",
		SecretCiphertext: "ciphertext",
		CreatedAt:        now,
		UpdatedAt:        now,
	}); err != nil {
		t.Fatalf("PutClient() error = %v", err)
	}
	if err := store.PutAgent(ctx, storage.AgentRecord{
		ID:         "agent-A",
		NodeName:   "node-a",
		LastSeenAt: now,
	}); err != nil {
		t.Fatalf("PutAgent() error = %v", err)
	}

	deployment := storage.ClientDeploymentRecord{
		ClientID:           "client-1",
		AgentID:            "agent-A",
		DesiredOperation:   "client.create",
		Status:             "succeeded",
		ConnectionLinks:    []string{"tg://link"},
		UpdatedAt:          now,
		LastResetEpochSecs: 1_747_332_000,
	}
	if err := store.PutClientDeployment(ctx, deployment); err != nil {
		t.Fatalf("PutClientDeployment() error = %v", err)
	}

	got, err := store.ListClientDeployments(ctx, "client-1")
	if err != nil {
		t.Fatalf("ListClientDeployments() error = %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("ListClientDeployments() len = %d, want 1", len(got))
	}
	if got[0].LastResetEpochSecs != 1_747_332_000 {
		t.Fatalf("LastResetEpochSecs = %d, want 1_747_332_000", got[0].LastResetEpochSecs)
	}
}

// TestClientsRepositoryRoundTripsLastReset is the canonical Phase 3
// persistence assertion: the clients.Repository path used by
// PersistDeployment (job-completion + drift-advance) must write and
// read the timestamp without loss. Schema regressions in either
// upsertDeployment or scanDeployment fail this test.
func TestClientsRepositoryRoundTripsLastReset(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "phase3-repo.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	ctx := context.Background()
	now := time.Date(2026, time.May, 15, 12, 0, 0, 0, time.UTC)
	if err := store.PutClient(ctx, storage.ClientRecord{
		ID:               "client-1",
		Name:             "alice",
		SecretCiphertext: "ciphertext",
		CreatedAt:        now,
		UpdatedAt:        now,
	}); err != nil {
		t.Fatalf("PutClient() error = %v", err)
	}
	if err := store.PutAgent(ctx, storage.AgentRecord{
		ID:         "agent-A",
		NodeName:   "node-a",
		LastSeenAt: now,
	}); err != nil {
		t.Fatalf("PutAgent() error = %v", err)
	}

	repo := NewClientsRepository(store.DB())
	deployment := clients.Deployment{
		ClientID:           clients.ClientID("client-1"),
		AgentID:            "agent-A",
		DesiredOperation:   "client.create",
		Status:             "succeeded",
		ConnectionLinks:    []string{"tg://link"},
		UpdatedAt:          now,
		LastResetEpochSecs: 1_747_332_000,
	}
	if err := repo.PutDeployment(ctx, deployment); err != nil {
		t.Fatalf("repo.PutDeployment() error = %v", err)
	}

	rows, err := repo.ListDeployments(ctx, deployment.ClientID)
	if err != nil {
		t.Fatalf("repo.ListDeployments() error = %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("repo.ListDeployments() len = %d, want 1", len(rows))
	}
	if rows[0].LastResetEpochSecs != 1_747_332_000 {
		t.Fatalf("LastResetEpochSecs = %d, want 1_747_332_000", rows[0].LastResetEpochSecs)
	}

	// Upserting again with a newer timestamp must overwrite the
	// previous value — operators may reset multiple times and each
	// successful job should advance the panel-side record.
	deployment.LastResetEpochSecs = 1_747_500_000
	if err := repo.PutDeployment(ctx, deployment); err != nil {
		t.Fatalf("repo.PutDeployment() second call error = %v", err)
	}
	rows, err = repo.ListDeployments(ctx, deployment.ClientID)
	if err != nil {
		t.Fatalf("repo.ListDeployments() second call error = %v", err)
	}
	if rows[0].LastResetEpochSecs != 1_747_500_000 {
		t.Fatalf("LastResetEpochSecs after re-upsert = %d, want 1_747_500_000", rows[0].LastResetEpochSecs)
	}
}
