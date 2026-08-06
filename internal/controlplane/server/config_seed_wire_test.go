package server

import (
	"context"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/gateway"
	"github.com/lost-coder/panvex/internal/gatewayrpc"
	"github.com/lost-coder/panvex/internal/security"
)

// TestSnapshotSeedsDesiredConfigOnce verifies that applyAgentSnapshot seeds
// the agent's desired config from the first observed managed-config JSON it
// receives, wiring config_seed.go's seedDesiredConfig into the live snapshot
// path.
func TestSnapshotSeedsDesiredConfigOnce(t *testing.T) {
	now := time.Date(2026, time.March, 18, 9, 0, 0, 0, time.UTC)
	s := testServerWithSQLite(t, now)
	fleetGroupID := seedTestFleetGroup(t, s.store, "ams-1", now)

	token, err := s.issueEnrollmentToken(security.EnrollmentScope{
		FleetGroupID: fleetGroupID,
		TTL:          time.Minute,
	}, now)
	if err != nil {
		t.Fatalf("IssueToken() error = %v", err)
	}
	identity, err := s.enrollAgent(context.Background(), agentEnrollmentRequest{
		Token:    token.Value,
		NodeName: "node-a",
		Version:  "1.0.0",
		CSRPEM:   testCSRPEM(t),
	}, now)
	if err != nil {
		t.Fatalf("enrollAgent() error = %v", err)
	}

	snap := gateway.AgentSnapshot{
		AgentID: identity.AgentID,
		Snap: &gatewayrpc.Snapshot{
			NodeName:     "node-a",
			FleetGroupId: fleetGroupID,
			Version:      "3.4.25",
			Instances: []*gatewayrpc.InstanceSnapshot{{
				Id:                "telemt-primary",
				ManagedConfigHash: "h1",
				ManagedConfigJson: `{"general":{"log_level":"silent"}}`,
			}},
		},
		ObservedAt: now,
	}
	if err := s.applyAgentSnapshot(context.Background(), snap); err != nil {
		t.Fatalf("applyAgentSnapshot() error = %v", err)
	}

	got, err := s.configTargets.Sections(context.Background(), "agent", identity.AgentID)
	if err != nil {
		t.Fatalf("Sections() error = %v", err)
	}
	general, ok := got["general"].(map[string]any)
	if !ok {
		t.Fatalf("snapshot did not seed desired config: %#v", got)
	}
	if general["log_level"] != "silent" {
		t.Fatalf("snapshot did not seed desired config: %#v", got)
	}
}
