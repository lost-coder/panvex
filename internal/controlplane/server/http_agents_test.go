package server

import (
	"context"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/auth"
	"github.com/lost-coder/panvex/internal/controlplane/storage"
	"github.com/lost-coder/panvex/internal/controlplane/storage/sqlite"
)

// TestReassignAutoAppliesGroupConfig verifies P3 Task 2: reassigning an agent
// into a fleet group that has config auto-applies that config to the agent
// (config_group_apply.go's applyGroupToAgent), instead of leaving the node
// stuck on its old snapshot until the next unrelated config-apply run.
func TestReassignAutoAppliesGroupConfig(t *testing.T) {
	now := time.Date(2026, time.August, 6, 10, 0, 0, 0, time.UTC)

	store, err := sqlite.Open(filepath.Join(t.TempDir(), "panvex.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}

	srv := mustNew(t, Options{
		LoginTimingFloor: -1,
		Now:              func() time.Time { return now },
		Store:            store,
	})
	t.Cleanup(func() {
		srv.Close()
		store.Close()
	})

	if _, _, err := srv.auth.BootstrapUser(context.Background(), auth.BootstrapInput{
		Username: "admin",
		Password: "Admin1password",
		Role:     auth.RoleAdmin,
	}, now); err != nil {
		t.Fatalf("BootstrapUser() error = %v", err)
	}

	// Target group "g2" has config — a node joining it should get that
	// config pushed automatically.
	if err := store.CreateFleetGroup(context.Background(), storage.FleetGroupRecord{
		ID:        "g2",
		Name:      "g2",
		Label:     "Group 2",
		CreatedAt: now,
		UpdatedAt: now,
	}); err != nil {
		t.Fatalf("CreateFleetGroup(g2) error = %v", err)
	}
	if err := srv.configTargets.Upsert(context.Background(), storage.ConfigScopeGroup, "g2",
		map[string]any{"general": map[string]any{"fast_mode": false}}); err != nil {
		t.Fatalf("seed group config: %v", err)
	}

	// Seed the agent, currently in no group, both in storage and the
	// in-memory live snapshot (mirrors setupTransportModeServer's pattern).
	if err := store.PutAgent(context.Background(), storage.AgentRecord{
		ID:           "agent-1",
		NodeName:     "reassign-test-node",
		FleetGroupID: "",
		LastSeenAt:   now,
	}); err != nil {
		t.Fatalf("PutAgent() error = %v", err)
	}
	srv.mu.Lock()
	srv.seedLiveAgentKeyed("agent-1", Agent{
		ID:           "agent-1",
		NodeName:     "reassign-test-node",
		FleetGroupID: "",
	})
	srv.mu.Unlock()

	loginResp := performJSONRequest(t, srv, http.MethodPost, "/api/auth/login", map[string]string{
		"username": "admin",
		"password": "Admin1password",
	}, nil)
	if loginResp.Code != http.StatusOK {
		t.Fatalf("admin login status = %d, want %d", loginResp.Code, http.StatusOK)
	}
	cookies := loginResp.Result().Cookies()

	resp := performJSONRequest(t, srv, http.MethodPut, "/api/agents/agent-1/fleet-group",
		map[string]string{"fleet_group_id": "g2"}, cookies)
	if resp.Code != http.StatusOK {
		t.Fatalf("PUT /agents/agent-1/fleet-group status = %d, want %d (body=%s)",
			resp.Code, http.StatusOK, resp.Body.String())
	}

	batches, err := srv.store.ListRunningConfigApplyBatches(context.Background())
	if err != nil {
		t.Fatalf("ListRunningConfigApplyBatches() error = %v", err)
	}
	found := false
	for _, b := range batches {
		_, targets, err := srv.store.GetConfigApplyBatch(context.Background(), b.ID)
		if err != nil {
			t.Fatalf("GetConfigApplyBatch(%s) error = %v", b.ID, err)
		}
		for _, target := range targets {
			if target.AgentID == "agent-1" {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("expected a config_apply batch targeting agent-1 after reassign into g2, found none")
	}
}
