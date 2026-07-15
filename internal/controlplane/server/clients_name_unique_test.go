package server

import (
	"context"
	"errors"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/auth"
	"github.com/lost-coder/panvex/internal/controlplane/storage"
	"github.com/lost-coder/panvex/internal/controlplane/storage/sqlite"
)

// newNameUniqueTestServer builds a server with two agents in a "default"
// fleet group so createClient/updateClient resolve at least one target.
func newNameUniqueTestServer(t *testing.T, now time.Time) (*Server, string) {
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
		ID:         "agent-name-1",
		NodeName:   "node-a",
		Version:    "dev",
		LastSeenAt: now.Add(-time.Minute),
	})
	return server, groupID
}

// TestCreateClientRejectsDuplicateName is the reproduction: before Task 6
// two createClient calls with the same name BOTH succeeded, collapsing into
// one Telemt user on any common node. The second must now fail with
// errClientNameTaken.
func TestCreateClientRejectsDuplicateName(t *testing.T) {
	now := time.Date(2026, time.July, 15, 12, 0, 0, 0, time.UTC)
	server, groupID := newNameUniqueTestServer(t, now)
	ctx := context.Background()

	if _, _, _, err := server.createClient(ctx, "user-1", clientMutationInput{
		Name:          "alice",
		FleetGroupIDs: []string{groupID},
	}, now); err != nil {
		t.Fatalf("first createClient(alice): %v", err)
	}

	_, _, _, err := server.createClient(ctx, "user-1", clientMutationInput{
		Name:          "alice",
		FleetGroupIDs: []string{groupID},
	}, now)
	if !errors.Is(err, errClientNameTaken) {
		t.Fatalf("second createClient(alice) error = %v, want errClientNameTaken", err)
	}
}

// TestUpdateClientNameUniqueness: renaming a client to a name held by ANOTHER
// living client is a conflict; renaming a client to its OWN current name is
// allowed (excludeID skips the client itself).
func TestUpdateClientNameUniqueness(t *testing.T) {
	now := time.Date(2026, time.July, 15, 12, 0, 0, 0, time.UTC)
	server, groupID := newNameUniqueTestServer(t, now)
	ctx := context.Background()

	alice, _, _, err := server.createClient(ctx, "user-1", clientMutationInput{
		Name:          "alice",
		FleetGroupIDs: []string{groupID},
	}, now)
	if err != nil {
		t.Fatalf("createClient(alice): %v", err)
	}
	bob, _, _, err := server.createClient(ctx, "user-1", clientMutationInput{
		Name:          "bob",
		FleetGroupIDs: []string{groupID},
	}, now)
	if err != nil {
		t.Fatalf("createClient(bob): %v", err)
	}

	// Rename bob -> alice: conflict.
	if _, _, _, err := server.updateClient(ctx, string(bob.ID), "user-1", clientMutationInput{
		Name:          "alice",
		FleetGroupIDs: []string{groupID},
	}, now); !errors.Is(err, errClientNameTaken) {
		t.Fatalf("updateClient(bob->alice) error = %v, want errClientNameTaken", err)
	}

	// Rename alice -> alice (its own current name): allowed.
	if _, _, _, err := server.updateClient(ctx, string(alice.ID), "user-1", clientMutationInput{
		Name:          "alice",
		FleetGroupIDs: []string{groupID},
	}, now); err != nil {
		t.Fatalf("updateClient(alice->alice) error = %v, want nil (rename to own name is allowed)", err)
	}

	// Rename bob -> bob-2 (free): allowed.
	if _, _, _, err := server.updateClient(ctx, string(bob.ID), "user-1", clientMutationInput{
		Name:          "bob-2",
		FleetGroupIDs: []string{groupID},
	}, now); err != nil {
		t.Fatalf("updateClient(bob->bob-2) error = %v, want nil", err)
	}
}

// TestCreateClientReusesTombstonedName: deleting a client frees its name for
// reuse (tombstone → name is free).
func TestCreateClientReusesTombstonedName(t *testing.T) {
	now := time.Date(2026, time.July, 15, 12, 0, 0, 0, time.UTC)
	server, groupID := newNameUniqueTestServer(t, now)
	ctx := context.Background()

	alice, _, _, err := server.createClient(ctx, "user-1", clientMutationInput{
		Name:          "alice",
		FleetGroupIDs: []string{groupID},
	}, now)
	if err != nil {
		t.Fatalf("createClient(alice): %v", err)
	}
	if err := server.deleteClient(ctx, string(alice.ID), "user-1", now); err != nil {
		t.Fatalf("deleteClient(alice): %v", err)
	}

	// The name is now free — a fresh create must succeed.
	if _, _, _, err := server.createClient(ctx, "user-1", clientMutationInput{
		Name:          "alice",
		FleetGroupIDs: []string{groupID},
	}, now); err != nil {
		t.Fatalf("createClient(alice) after delete error = %v, want nil (tombstoned name is free)", err)
	}
}

// TestHTTPCreateDuplicateNameReturnsConflict exercises the full HTTP path and
// asserts the wire contract: a duplicate-name create returns 409 with the
// fixed operator-safe message.
func TestHTTPCreateDuplicateNameReturnsConflict(t *testing.T) {
	now := time.Date(2026, time.July, 15, 12, 0, 0, 0, time.UTC)
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "panvex.db"))
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	defer store.Close()

	server := mustNew(t, Options{
		LoginTimingFloor: -1,
		Now:              func() time.Time { return now },
		Store:            store,
	})
	defer server.Close()
	if _, _, err := server.auth.BootstrapUser(context.Background(), auth.BootstrapInput{
		Username: "admin",
		Password: "Admin1password",
		Role:     auth.RoleAdmin,
	}, now); err != nil {
		t.Fatalf("BootstrapUser: %v", err)
	}
	groupID := seedClientTargetAgent(t, store, server, "default", now.Add(-2*time.Minute), storage.AgentRecord{
		ID:         "agent-http-1",
		NodeName:   "node-a",
		Version:    "dev",
		LastSeenAt: now.Add(-time.Minute),
	})

	cookies := loginAdminForClients(t, server)
	body := map[string]any{
		"name":            "alice",
		"enabled":         true,
		"fleet_group_ids": []string{groupID},
	}
	if resp := performJSONRequest(t, server, http.MethodPost, "/api/clients", body, cookies); resp.Code != http.StatusCreated {
		t.Fatalf("first create status = %d, want %d", resp.Code, http.StatusCreated)
	}
	resp := performJSONRequest(t, server, http.MethodPost, "/api/clients", body, cookies)
	if resp.Code != http.StatusConflict {
		t.Fatalf("duplicate create status = %d, want %d", resp.Code, http.StatusConflict)
	}
	if got := resp.Body.String(); !strings.Contains(got, msgClientNameTaken) {
		t.Fatalf("duplicate create body = %q, want it to contain %q", got, msgClientNameTaken)
	}
}
