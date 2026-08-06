package server

import (
	"context"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
	"github.com/lost-coder/panvex/internal/security"
)

// TestEnrollAutoAppliesGroupConfig verifies P3 Task 3: enrolling a node
// directly into a fleet group that has config auto-applies that config to
// the freshly-enrolled agent (config_group_apply.go's applyGroupToAgent),
// instead of leaving the node stuck with no config until an unrelated
// config-apply run picks it up. Mirrors TestReassignAutoAppliesGroupConfig
// (http_agents_test.go, P3 Task 2) but for the enrollment path.
func TestEnrollAutoAppliesGroupConfig(t *testing.T) {
	now := time.Date(2026, time.August, 6, 10, 0, 0, 0, time.UTC)
	server := testServerWithSQLite(t, now)

	fleetGroupID := seedTestFleetGroup(t, server.store, "g-enroll", now)
	if err := server.configTargets.Upsert(context.Background(), storage.ConfigScopeGroup, fleetGroupID,
		map[string]any{"general": map[string]any{"fast_mode": false}}); err != nil {
		t.Fatalf("seed group config: %v", err)
	}

	token, err := server.issueEnrollmentToken(security.EnrollmentScope{
		FleetGroupID: fleetGroupID,
		TTL:          time.Minute,
	}, now)
	if err != nil {
		t.Fatalf("issueEnrollmentToken() error = %v", err)
	}

	response, err := server.enrollAgent(context.Background(), agentEnrollmentRequest{
		Token:    token.Value,
		NodeName: "enroll-autoapply-node",
		Version:  "1.0.0",
		CSRPEM:   testCSRPEM(t),
	}, now.Add(10*time.Second))
	if err != nil {
		t.Fatalf("enrollAgent() error = %v", err)
	}
	if response.AgentID == "" {
		t.Fatal("response.AgentID = empty, want issued agent identity")
	}

	batches, err := server.store.ListRunningConfigApplyBatches(context.Background())
	if err != nil {
		t.Fatalf("ListRunningConfigApplyBatches() error = %v", err)
	}
	found := false
	for _, b := range batches {
		_, targets, err := server.store.GetConfigApplyBatch(context.Background(), b.ID)
		if err != nil {
			t.Fatalf("GetConfigApplyBatch(%s) error = %v", b.ID, err)
		}
		for _, target := range targets {
			if target.AgentID == response.AgentID {
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("expected a config_apply batch targeting agent %s after enrolling into %s, found none", response.AgentID, fleetGroupID)
	}
}
