package server

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/auth"
	"github.com/lost-coder/panvex/internal/controlplane/storage"
	"github.com/lost-coder/panvex/internal/controlplane/storage/sqlite"
)

// TestHandleRotateSubscriptionToken_RotatesToken verifies that
// POST /api/clients/{id}/rotate-subscription returns HTTP 200, changes the
// client's subscription token, and responds with a client-detail JSON body.
func TestHandleRotateSubscriptionToken_RotatesToken(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 13, 10, 0, 0, 0, time.UTC)

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

	if _, _, err := server.auth.BootstrapUser(context.Background(), auth.BootstrapInput{
		Username: "admin",
		Password: "Admin1password",
		Role:     auth.RoleAdmin,
	}, now); err != nil {
		t.Fatalf("BootstrapUser() error = %v", err)
	}

	defaultGroupID := seedClientTargetAgent(t, store, server, "default", now.Add(-2*time.Minute), storage.AgentRecord{
		ID:         "agent-sub-http-1",
		NodeName:   "node-sub-http-1",
		Version:    "dev",
		LastSeenAt: now.Add(-time.Minute),
	})

	cookies := loginAdminForClients(t, server)

	// Create a client so we have an ID and an initial subscription token.
	createResp := performJSONRequest(t, server, http.MethodPost, "/api/clients", map[string]any{
		"name":            "sub-rotate-client",
		"enabled":         true,
		"fleet_group_ids": []string{defaultGroupID},
	}, cookies)
	if createResp.Code != http.StatusCreated {
		t.Fatalf("POST /api/clients status = %d, want %d\nbody: %s", createResp.Code, http.StatusCreated, createResp.Body.String())
	}

	var created struct {
		ID                string `json:"id"`
		SubscriptionToken string `json:"subscription_token"`
	}
	if err := json.Unmarshal(createResp.Body.Bytes(), &created); err != nil {
		t.Fatalf("json.Unmarshal(create response): %v", err)
	}
	if created.ID == "" {
		t.Fatal("created client has empty ID")
	}

	// Capture initial token from the store so we can compare after rotation.
	initial, err := store.GetClientByID(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("GetClientByID: %v", err)
	}
	initialToken := initial.SubscriptionToken

	// POST /api/clients/{id}/rotate-subscription.
	rotateResp := performJSONRequest(t, server, http.MethodPost, "/api/clients/"+created.ID+"/rotate-subscription", nil, cookies)
	if rotateResp.Code != http.StatusOK {
		t.Fatalf("POST /api/clients/{id}/rotate-subscription status = %d, want %d\nbody: %s",
			rotateResp.Code, http.StatusOK, rotateResp.Body.String())
	}

	// Assert the response is a valid client-detail shape (JSON object with "id").
	var rotated struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rotateResp.Body.Bytes(), &rotated); err != nil {
		t.Fatalf("json.Unmarshal(rotate response): %v", err)
	}
	if rotated.ID != created.ID {
		t.Fatalf("rotate response id = %q, want %q", rotated.ID, created.ID)
	}

	// Reload from store and confirm token changed and is non-empty.
	updated, err := store.GetClientByID(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("GetClientByID after rotate: %v", err)
	}
	if updated.SubscriptionToken == "" {
		t.Fatal("subscription token is empty after rotation")
	}
	if updated.SubscriptionToken == initialToken {
		t.Fatalf("subscription token unchanged after rotation (%q)", updated.SubscriptionToken)
	}
}

// TestHandleRotateSubscriptionToken_NotFound verifies that rotating a
// non-existent client returns HTTP 404.
func TestHandleRotateSubscriptionToken_NotFound(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 13, 10, 0, 0, 0, time.UTC)

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

	if _, _, err := server.auth.BootstrapUser(context.Background(), auth.BootstrapInput{
		Username: "admin",
		Password: "Admin1password",
		Role:     auth.RoleAdmin,
	}, now); err != nil {
		t.Fatalf("BootstrapUser() error = %v", err)
	}

	cookies := loginAdminForClients(t, server)

	resp := performJSONRequest(t, server, http.MethodPost, "/api/clients/nonexistent-client-id/rotate-subscription", nil, cookies)
	if resp.Code != http.StatusNotFound {
		t.Fatalf("POST rotate-subscription (not found) status = %d, want %d\nbody: %s",
			resp.Code, http.StatusNotFound, resp.Body.String())
	}
}
