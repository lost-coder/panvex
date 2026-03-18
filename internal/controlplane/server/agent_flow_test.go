package server

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/panvex/panvex/internal/gatewayrpc"
	"github.com/panvex/panvex/internal/controlplane/storage/sqlite"
	"github.com/panvex/panvex/internal/security"
)

func TestServerEnrollAgentConsumesTokenAndIssuesIdentity(t *testing.T) {
	now := time.Date(2026, time.March, 14, 8, 0, 0, 0, time.UTC)
	server := New(Options{
		Now: func() time.Time { return now },
	})
	token, err := server.enrollment.IssueToken(security.EnrollmentScope{
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
		Runtime: gatewayRuntimeSnapshotForTest(),
		HasRuntime: true,
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
	if !server.agents[identity.AgentID].Runtime.AcceptingNewConnections {
		t.Fatal("agent runtime accepting_new_connections = false, want true")
	}
	if server.agents[identity.AgentID].Runtime.TransportMode != "middle_proxy" {
		t.Fatalf("agent runtime transport_mode = %q, want %q", server.agents[identity.AgentID].Runtime.TransportMode, "middle_proxy")
	}
}

func TestServerApplyAgentSnapshotPersistsInventoryAndMetricsAcrossRestart(t *testing.T) {
	now := time.Date(2026, time.March, 15, 10, 0, 0, 0, time.UTC)
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
		FleetGroupID:  "ams-1",
		TTL:           time.Minute,
	}, now)
	if err != nil {
		t.Fatalf("issueEnrollmentToken() error = %v", err)
	}

	identity, err := first.enrollAgent(agentEnrollmentRequest{
		Token:    token.Value,
		NodeName: "node-a",
		Version:  "1.0.0",
	}, now.Add(10*time.Second))
	if err != nil {
		t.Fatalf("enrollAgent() error = %v", err)
	}

	first.applyAgentSnapshot(agentSnapshot{
		AgentID:       identity.AgentID,
		NodeName:      "node-a",
		FleetGroupID:  "ams-1",
		Version:       "1.0.0",
		ReadOnly:      true,
		Instances: []instanceSnapshot{
			{
				ID:                "instance-1",
				Name:              "telemt-a",
				Version:           "2026.03",
				ConfigFingerprint: "cfg-1",
				ConnectedUsers:    42,
				ReadOnly:          true,
			},
		},
		Metrics: map[string]uint64{
			"requests_total": 128,
		},
		Runtime: gatewayRuntimeSnapshotForTest(),
		HasRuntime: true,
		ObservedAt: now.Add(15 * time.Second),
	})

	restored := New(Options{
		Now: func() time.Time { return now.Add(time.Minute) },
		Store: store,
	})

	restoredAgents, err := restored.store.ListAgents(context.Background())
	if err != nil {
		t.Fatalf("ListAgents() error = %v", err)
	}
	if len(restoredAgents) != 1 {
		t.Fatalf("len(ListAgents()) = %d, want %d", len(restoredAgents), 1)
	}

	restoredInstances, err := restored.store.ListInstances(context.Background())
	if err != nil {
		t.Fatalf("ListInstances() error = %v", err)
	}
	if len(restoredInstances) != 1 {
		t.Fatalf("len(ListInstances()) = %d, want %d", len(restoredInstances), 1)
	}

	restoredMetrics, err := restored.store.ListMetricSnapshots(context.Background())
	if err != nil {
		t.Fatalf("ListMetricSnapshots() error = %v", err)
	}
	if len(restoredMetrics) != 1 {
		t.Fatalf("len(ListMetricSnapshots()) = %d, want %d", len(restoredMetrics), 1)
	}
}

func gatewayRuntimeSnapshotForTest() gatewayrpc.RuntimeSnapshot {
	return gatewayrpc.RuntimeSnapshot{
		AcceptingNewConnections:   true,
		MERuntimeReady:            true,
		ME2DCFallbackEnabled:      true,
		UseMiddleProxy:            true,
		StartupStatus:             "ready",
		StartupStage:              "serving",
		StartupProgressPct:        100,
		InitializationStatus:      "ready",
		Degraded:                  false,
		InitializationStage:       "serving",
		InitializationProgressPct: 100,
		TransportMode:             "middle_proxy",
		CurrentConnections:        42,
		CurrentConnectionsME:      39,
		CurrentConnectionsDirect:  3,
		ActiveUsers:               7,
		ConnectionsTotal:          512,
		ConnectionsBadTotal:       9,
		HandshakeTimeoutsTotal:    4,
		ConfiguredUsers:           12,
		DCs: []gatewayrpc.RuntimeDCSnapshot{
			{
				DC:                 2,
				AvailableEndpoints: 3,
				AvailablePct:       100,
				RequiredWriters:    4,
				AliveWriters:       4,
				CoveragePct:        100,
				RTTMs:              21.5,
				Load:               18,
			},
		},
		Upstreams: gatewayrpc.RuntimeUpstreamSnapshot{
			ConfiguredTotal: 2,
			HealthyTotal:    1,
			UnhealthyTotal:  1,
			DirectTotal:     1,
			SOCKS5Total:     1,
		},
		RecentEvents: []gatewayrpc.RuntimeEventSnapshot{
			{
				Sequence:      1,
				TimestampUnix: 1_763_226_400,
				EventType:     "upstream_recovered",
				Context:       "dc=2 upstream=1",
			},
		},
	}
}

func TestServerApplyAgentSnapshotKeepsEnrolledScopeWhenSnapshotDiffers(t *testing.T) {
	now := time.Date(2026, time.March, 16, 11, 0, 0, 0, time.UTC)
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "panvex.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	defer store.Close()

	server := New(Options{
		Now: func() time.Time { return now },
		Store: store,
	})
	token, err := server.issueEnrollmentToken(security.EnrollmentScope{
		FleetGroupID:  "default",
		TTL:           time.Minute,
	}, now)
	if err != nil {
		t.Fatalf("issueEnrollmentToken() error = %v", err)
	}

	identity, err := server.enrollAgent(agentEnrollmentRequest{
		Token:    token.Value,
		NodeName: "node-a",
		Version:  "1.0.0",
	}, now.Add(10*time.Second))
	if err != nil {
		t.Fatalf("enrollAgent() error = %v", err)
	}

	func() {
		defer func() {
			if recovered := recover(); recovered != nil {
				t.Fatalf("applyAgentSnapshot() panic = %v", recovered)
			}
		}()

		server.applyAgentSnapshot(agentSnapshot{
			AgentID:       identity.AgentID,
			NodeName:      "node-a",
			FleetGroupID:  "ams-1",
			Version:       "1.0.0",
			ObservedAt:    now.Add(20 * time.Second),
		})
	}()

	record, err := store.ListAgents(context.Background())
	if err != nil {
		t.Fatalf("ListAgents() error = %v", err)
	}
	if len(record) != 1 {
		t.Fatalf("len(ListAgents()) = %d, want %d", len(record), 1)
	}
	if record[0].FleetGroupID != "default" {
		t.Fatalf("ListAgents()[0].FleetGroupID = %q, want %q", record[0].FleetGroupID, "default")
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
