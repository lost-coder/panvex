package server

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/panvex/panvex/internal/controlplane/storage/sqlite"
	"github.com/panvex/panvex/internal/security"
)

func TestServerEnrollAgentConsumesTokenAndIssuesIdentity(t *testing.T) {
	now := time.Date(2026, time.March, 14, 8, 0, 0, 0, time.UTC)
	server := New(Options{
		Now: func() time.Time { return now },
	})
	token, err := server.enrollment.IssueToken(security.EnrollmentScope{
		EnvironmentID: "prod",
		FleetGroupID:  "ams-1",
		TTL:           time.Minute,
	}, now)
	if err != nil {
		t.Fatalf("IssueToken() error = %v", err)
	}

	response, err := server.enrollAgent(agentEnrollmentRequest{
		Token:    token.Value,
		NodeName: "node-a",
		Version:  "1.0.0",
	}, now.Add(10*time.Second))
	if err != nil {
		t.Fatalf("enrollAgent() error = %v", err)
	}

	if response.AgentID == "" {
		t.Fatal("response.AgentID = empty, want issued agent identity")
	}

	if response.CertificatePEM == "" {
		t.Fatal("response.CertificatePEM = empty, want issued certificate")
	}
}

func TestServerApplyAgentSnapshotUpdatesInventoryMetricsAndPresence(t *testing.T) {
	now := time.Date(2026, time.March, 14, 8, 0, 0, 0, time.UTC)
	server := New(Options{
		Now: func() time.Time { return now },
	})
	token, err := server.enrollment.IssueToken(security.EnrollmentScope{
		EnvironmentID: "prod",
		FleetGroupID:  "ams-1",
		TTL:           time.Minute,
	}, now)
	if err != nil {
		t.Fatalf("IssueToken() error = %v", err)
	}

	identity, err := server.enrollAgent(agentEnrollmentRequest{
		Token:    token.Value,
		NodeName: "node-a",
		Version:  "1.0.0",
	}, now)
	if err != nil {
		t.Fatalf("enrollAgent() error = %v", err)
	}

	server.applyAgentSnapshot(agentSnapshot{
		AgentID:       identity.AgentID,
		NodeName:      "node-a",
		EnvironmentID: "prod",
		FleetGroupID:  "ams-1",
		Version:       "1.0.0",
		ReadOnly:      true,
		Instances: []instanceSnapshot{
			{
				ID:               "instance-1",
				Name:             "telemt-a",
				Version:          "2026.03",
				ConfigFingerprint:"cfg-1",
				ConnectedUsers:   42,
				ReadOnly:         true,
			},
		},
		Metrics: map[string]uint64{
			"requests_total": 128,
		},
		ObservedAt: now.Add(15 * time.Second),
	})

	if state := server.presence.Evaluate(identity.AgentID, now.Add(20*time.Second)); state == "" {
		t.Fatal("presence state = empty, want tracked presence")
	}

	server.mu.RLock()
	defer server.mu.RUnlock()

	if len(server.instances) != 1 {
		t.Fatalf("len(server.instances) = %d, want %d", len(server.instances), 1)
	}

	if len(server.metrics) != 1 {
		t.Fatalf("len(server.metrics) = %d, want %d", len(server.metrics), 1)
	}
}

func TestServerEnrollmentTokenPersistsAcrossRestart(t *testing.T) {
	now := time.Date(2026, time.March, 15, 8, 0, 0, 0, time.UTC)
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "panvex.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	defer store.Close()

	first := New(Options{
		Now: func() time.Time { return now },
		Store: store,
	})
	token, err := first.issueEnrollmentToken(security.EnrollmentScope{
		EnvironmentID: "prod",
		FleetGroupID:  "ams-1",
		TTL:           time.Minute,
	}, now)
	if err != nil {
		t.Fatalf("issueEnrollmentToken() error = %v", err)
	}

	restored := New(Options{
		Now: func() time.Time { return now.Add(10 * time.Second) },
		Store: store,
	})
	response, err := restored.enrollAgent(agentEnrollmentRequest{
		Token:    token.Value,
		NodeName: "node-a",
		Version:  "1.0.0",
	}, now.Add(10*time.Second))
	if err != nil {
		t.Fatalf("enrollAgent() error = %v", err)
	}

	if response.AgentID == "" {
		t.Fatal("response.AgentID = empty, want issued agent identity")
	}
}

func TestServerConsumedEnrollmentTokenRemainsRejectedAfterRestart(t *testing.T) {
	now := time.Date(2026, time.March, 15, 8, 0, 0, 0, time.UTC)
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "panvex.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	defer store.Close()

	first := New(Options{
		Now: func() time.Time { return now },
		Store: store,
	})
	token, err := first.issueEnrollmentToken(security.EnrollmentScope{
		EnvironmentID: "prod",
		FleetGroupID:  "ams-1",
		TTL:           time.Minute,
	}, now)
	if err != nil {
		t.Fatalf("issueEnrollmentToken() error = %v", err)
	}

	if _, err := first.enrollAgent(agentEnrollmentRequest{
		Token:    token.Value,
		NodeName: "node-a",
		Version:  "1.0.0",
	}, now.Add(10*time.Second)); err != nil {
		t.Fatalf("enrollAgent() error = %v", err)
	}

	restored := New(Options{
		Now: func() time.Time { return now.Add(20 * time.Second) },
		Store: store,
	})
	if _, err := restored.enrollAgent(agentEnrollmentRequest{
		Token:    token.Value,
		NodeName: "node-b",
		Version:  "1.0.1",
	}, now.Add(20*time.Second)); err != security.ErrEnrollmentTokenConsumed {
		t.Fatalf("enrollAgent() error = %v, want %v", err, security.ErrEnrollmentTokenConsumed)
	}
}

func TestServerExpiredEnrollmentTokenRemainsRejectedAfterRestart(t *testing.T) {
	now := time.Date(2026, time.March, 15, 8, 0, 0, 0, time.UTC)
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "panvex.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	defer store.Close()

	first := New(Options{
		Now: func() time.Time { return now },
		Store: store,
	})
	token, err := first.issueEnrollmentToken(security.EnrollmentScope{
		EnvironmentID: "prod",
		FleetGroupID:  "ams-1",
		TTL:           time.Second,
	}, now)
	if err != nil {
		t.Fatalf("issueEnrollmentToken() error = %v", err)
	}

	restored := New(Options{
		Now: func() time.Time { return now.Add(2 * time.Second) },
		Store: store,
	})
	if _, err := restored.enrollAgent(agentEnrollmentRequest{
		Token:    token.Value,
		NodeName: "node-b",
		Version:  "1.0.1",
	}, now.Add(2*time.Second)); err != security.ErrEnrollmentTokenExpired {
		t.Fatalf("enrollAgent() error = %v, want %v", err, security.ErrEnrollmentTokenExpired)
	}
}
