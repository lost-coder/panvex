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

// newSubscriptionTestServer creates a Server backed by a temporary SQLite
// store, seeds one fleet-group + one agent, and returns the server together
// with the fleet-group ID. The Server is registered for Close cleanup.
func newSubscriptionTestServer(t *testing.T, now time.Time) (*Server, string) {
	t.Helper()

	store, err := sqlite.Open(filepath.Join(t.TempDir(), "panvex.db"))
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}

	server := mustNew(t, Options{
		LoginTimingFloor: -1,
		Now:              func() time.Time { return now },
		Store:            store,
	})
	t.Cleanup(func() {
		server.Close()
		store.Close()
	})

	groupID := seedClientTargetAgent(t, store, server, "default", now.Add(-2*time.Minute), storage.AgentRecord{
		ID:         "agent-sub-1",
		NodeName:   "node-sub-1",
		Version:    "dev",
		LastSeenAt: now.Add(-time.Minute),
	})

	return server, groupID
}

// TestResolveBySubscriptionToken_KnownToken verifies that a client
// created via createClient can be resolved by its subscription token.
func TestResolveBySubscriptionToken_KnownToken(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 13, 10, 0, 0, 0, time.UTC)
	server, groupID := newSubscriptionTestServer(t, now)
	ctx := context.Background()

	created, _, _, err := server.createClient(ctx, "user-000001", clientMutationInput{
		Name:          "alice",
		FleetGroupIDs: []string{groupID},
	}, now)
	if err != nil {
		t.Fatalf("createClient: %v", err)
	}
	if created.SubscriptionToken == "" {
		t.Fatal("createClient: SubscriptionToken is empty")
	}

	got, err := server.clientsSvc.ResolveBySubscriptionToken(ctx, created.SubscriptionToken)
	if err != nil {
		t.Fatalf("ResolveBySubscriptionToken(known): %v", err)
	}
	if string(got.ID) != string(created.ID) {
		t.Fatalf("ResolveBySubscriptionToken: got ID %q, want %q", got.ID, created.ID)
	}
	if got.Name != created.Name {
		t.Fatalf("ResolveBySubscriptionToken: got Name %q, want %q", got.Name, created.Name)
	}
}

// TestResolveBySubscriptionToken_UnknownToken verifies that an unknown
// token returns ErrNotFound (from the repository layer, which is
// storage.ErrNotFound).
func TestResolveBySubscriptionToken_UnknownToken(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 13, 10, 0, 0, 0, time.UTC)
	server, _ := newSubscriptionTestServer(t, now)
	ctx := context.Background()

	_, err := server.clientsSvc.ResolveBySubscriptionToken(ctx, "nonexistent-token-xxxxxxxxxxxxxxxx")
	if !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("ResolveBySubscriptionToken(unknown): err = %v, want storage.ErrNotFound", err)
	}
}

// TestResolveBySubscriptionToken_BlankToken verifies that a blank token
// returns ErrNotFound immediately without hitting the DB.
func TestResolveBySubscriptionToken_BlankToken(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 13, 10, 0, 0, 0, time.UTC)
	server, _ := newSubscriptionTestServer(t, now)
	ctx := context.Background()

	_, err := server.clientsSvc.ResolveBySubscriptionToken(ctx, "")
	if err == nil {
		t.Fatal("ResolveBySubscriptionToken(blank): expected error, got nil")
	}
}

// TestRotateSubscriptionToken_ChangesToken verifies that
// rotateSubscriptionToken assigns a new token, persists it, and that
// the old token can no longer be resolved.
func TestRotateSubscriptionToken_ChangesToken(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 13, 10, 0, 0, 0, time.UTC)
	server, groupID := newSubscriptionTestServer(t, now)
	ctx := context.Background()

	// Create the client; captures the initial token.
	created, _, _, err := server.createClient(ctx, "user-000001", clientMutationInput{
		Name:          "bob",
		FleetGroupIDs: []string{groupID},
	}, now)
	if err != nil {
		t.Fatalf("createClient: %v", err)
	}
	oldToken := created.SubscriptionToken
	if oldToken == "" {
		t.Fatal("createClient: SubscriptionToken is empty")
	}

	// Rotate.
	rotatedAt := now.Add(time.Minute)
	updated, _, _, err := server.rotateSubscriptionToken(ctx, string(created.ID), "user-000001", rotatedAt)
	if err != nil {
		t.Fatalf("rotateSubscriptionToken: %v", err)
	}

	newToken := updated.SubscriptionToken

	// The token must have changed.
	if newToken == oldToken {
		t.Fatalf("rotateSubscriptionToken: token unchanged (%q)", newToken)
	}
	if newToken == "" {
		t.Fatal("rotateSubscriptionToken: new token is empty")
	}

	// The new token must resolve to the same client.
	got, err := server.clientsSvc.ResolveBySubscriptionToken(ctx, newToken)
	if err != nil {
		t.Fatalf("ResolveBySubscriptionToken(new token): %v", err)
	}
	if string(got.ID) != string(created.ID) {
		t.Fatalf("ResolveBySubscriptionToken(new token): got ID %q, want %q", got.ID, created.ID)
	}

	// The old token must no longer resolve.
	_, err = server.clientsSvc.ResolveBySubscriptionToken(ctx, oldToken)
	if !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("ResolveBySubscriptionToken(old token after rotate): err = %v, want storage.ErrNotFound", err)
	}
}

// TestRotateSubscriptionToken_DeletedClientReturnsNotFound verifies that
// rotating a token for a soft-deleted client returns ErrNotFound.
func TestRotateSubscriptionToken_DeletedClientReturnsNotFound(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 13, 10, 0, 0, 0, time.UTC)
	server, groupID := newSubscriptionTestServer(t, now)
	ctx := context.Background()

	created, _, _, err := server.createClient(ctx, "user-000001", clientMutationInput{
		Name:          "carol",
		FleetGroupIDs: []string{groupID},
	}, now)
	if err != nil {
		t.Fatalf("createClient: %v", err)
	}

	if err := server.deleteClient(ctx, string(created.ID), "user-000001", now.Add(time.Minute)); err != nil {
		t.Fatalf("deleteClient: %v", err)
	}

	_, _, _, err = server.rotateSubscriptionToken(ctx, string(created.ID), "user-000001", now.Add(2*time.Minute))
	if !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("rotateSubscriptionToken(deleted): err = %v, want storage.ErrNotFound", err)
	}
}
