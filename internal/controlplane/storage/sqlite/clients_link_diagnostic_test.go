// IN-M2 persistence: the link_diagnostic column must survive
// PutClientDeployment / ListClientDeployment on the raw Store path and
// the clients.Repository path. A schema regression in either upsert or
// scan fails these tests.

package sqlite

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/clients"
	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

const wantDiagnostic = "apply succeeded but the node returned no connection links; existing links may be stale"

func seedClientAndAgent(t *testing.T, store *Store, ctx context.Context, now time.Time) {
	t.Helper()
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
}

func TestPutListClientDeploymentRoundTripsLinkDiagnostic(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "linkdiag.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	ctx := context.Background()
	now := time.Date(2026, time.May, 29, 12, 0, 0, 0, time.UTC)
	seedClientAndAgent(t, store, ctx, now)

	if err := store.PutClientDeployment(ctx, storage.ClientDeploymentRecord{
		ClientID:         "client-1",
		AgentID:          "agent-A",
		DesiredOperation: "client.update",
		Status:           "succeeded",
		ConnectionLinks:  []string{"tg://stale"},
		LinkDiagnostic:   wantDiagnostic,
		UpdatedAt:        now,
	}); err != nil {
		t.Fatalf("PutClientDeployment() error = %v", err)
	}

	got, err := store.ListClientDeployments(ctx, "client-1")
	if err != nil {
		t.Fatalf("ListClientDeployments() error = %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("ListClientDeployments() len = %d, want 1", len(got))
	}
	if got[0].LinkDiagnostic != wantDiagnostic {
		t.Fatalf("LinkDiagnostic = %q, want %q", got[0].LinkDiagnostic, wantDiagnostic)
	}
}

func TestClientsRepositoryRoundTripsLinkDiagnostic(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "linkdiag-repo.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	ctx := context.Background()
	now := time.Date(2026, time.May, 29, 12, 0, 0, 0, time.UTC)
	seedClientAndAgent(t, store, ctx, now)

	repo := NewClientsRepository(store.DB())
	deployment := clients.Deployment{
		ClientID:         clients.ClientID("client-1"),
		AgentID:          "agent-A",
		DesiredOperation: "client.update",
		Status:           "succeeded",
		ConnectionLinks:  []string{"tg://stale"},
		LinkDiagnostic:   wantDiagnostic,
		UpdatedAt:        now,
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
	if rows[0].LinkDiagnostic != wantDiagnostic {
		t.Fatalf("LinkDiagnostic = %q, want %q", rows[0].LinkDiagnostic, wantDiagnostic)
	}

	// A subsequent apply that DOES return links clears the diagnostic —
	// the upsert must overwrite the column, not leave the stale warning.
	deployment.LinkDiagnostic = ""
	deployment.ConnectionLinks = []string{"tg://fresh"}
	if err := repo.PutDeployment(ctx, deployment); err != nil {
		t.Fatalf("repo.PutDeployment() second call error = %v", err)
	}
	rows, err = repo.ListDeployments(ctx, deployment.ClientID)
	if err != nil {
		t.Fatalf("repo.ListDeployments() second call error = %v", err)
	}
	if rows[0].LinkDiagnostic != "" {
		t.Fatalf("LinkDiagnostic after clear = %q, want empty", rows[0].LinkDiagnostic)
	}
}
