package server

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/gatewayrpc"
	"github.com/lost-coder/panvex/internal/controlplane/storage/sqlite"
	"github.com/lost-coder/panvex/internal/security"
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

	if err := server.applyAgentSnapshot(agentSnapshot{
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
	}); err != nil {
		t.Fatalf("applyAgentSnapshot() error = %v", err)
	}

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
	if server.agents[identity.AgentID].Runtime.UptimeSeconds != 90_061 {
		t.Fatalf("agent runtime uptime_seconds = %v, want %v", server.agents[identity.AgentID].Runtime.UptimeSeconds, 90_061)
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

	if err := first.applyAgentSnapshot(agentSnapshot{
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
	}); err != nil {
		t.Fatalf("applyAgentSnapshot() error = %v", err)
	}

	first.Close()

	restored := New(Options{
		Now: func() time.Time { return now.Add(time.Minute) },
		Store: store,
	})
	defer restored.Close()

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

// TestServerApplyAgentSnapshotUpdatesInMemoryStateEvenWhenPersistenceFails verifies
// that the in-memory state is always updated regardless of DB write failures, since
// persistence is now handled asynchronously by the batch writer.
func TestServerApplyAgentSnapshotUpdatesInMemoryStateEvenWhenPersistenceFails(t *testing.T) {
	now := time.Date(2026, time.March, 18, 13, 20, 0, 0, time.UTC)
	sqliteStore, err := sqlite.Open(filepath.Join(t.TempDir(), "panvex.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	defer sqliteStore.Close()

	store := &failingStore{Store: sqliteStore}
	server := New(Options{
		Now:   func() time.Time { return now },
		Store: store,
	})
	defer server.Close()
	token, err := server.issueEnrollmentToken(security.EnrollmentScope{
		FleetGroupID: "ams-1",
		TTL:          time.Minute,
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

	store.putAgentErr = errors.New("put agent failed")

	// Async batch writer means persistence failures do not block the caller.
	if err := server.applyAgentSnapshot(agentSnapshot{
		AgentID:      identity.AgentID,
		NodeName:     "node-a",
		FleetGroupID: "ams-1",
		Version:      "1.0.0",
		Runtime:      gatewayRuntimeSnapshotForTest(),
		HasRuntime:   true,
		ObservedAt:   now.Add(20 * time.Second),
	}); err != nil {
		t.Fatalf("applyAgentSnapshot() error = %v, want nil (async persistence)", err)
	}

	// In-memory state should still reflect the snapshot.
	server.mu.RLock()
	agent, exists := server.agents[identity.AgentID]
	server.mu.RUnlock()
	if !exists {
		t.Fatal("agent not found in in-memory state after snapshot with failing store")
	}
	if agent.Version != "1.0.0" {
		t.Fatalf("agent.Version = %q, want %q", agent.Version, "1.0.0")
	}
}

func TestServerApplyAgentSnapshotTracksRuntimeLifecycleState(t *testing.T) {
	now := time.Date(2026, time.March, 16, 11, 0, 0, 0, time.UTC)
	server := New(Options{Now: func() time.Time { return now }})
	token, err := server.issueEnrollmentToken(security.EnrollmentScope{FleetGroupID: "ams-1", TTL: time.Minute}, now)
	if err != nil {
		t.Fatalf("issueEnrollmentToken() error = %v", err)
	}
	identity, err := server.enrollAgent(agentEnrollmentRequest{Token: token.Value, NodeName: "node-a", Version: "1.0.0"}, now.Add(10*time.Second))
	if err != nil {
		t.Fatalf("enrollAgent() error = %v", err)
	}

	runtime := gatewayRuntimeSnapshotForTest()
	runtime.StartupStatus = "starting"
	runtime.StartupStage = "booting"
	runtime.StartupProgressPct = 10
	runtime.InitializationStatus = "starting"
	runtime.InitializationStage = "booting"
	runtime.InitializationProgressPct = 10
	runtime.Degraded = true

	if err := server.applyAgentSnapshot(agentSnapshot{
		AgentID:      identity.AgentID,
		NodeName:     "node-a",
		FleetGroupID: "ams-1",
		Version:      "1.0.0",
		Runtime:      runtime,
		HasRuntime:   true,
		ObservedAt:   now.Add(20 * time.Second),
	}); err != nil {
		t.Fatalf("applyAgentSnapshot() error = %v", err)
	}

	if server.agents[identity.AgentID].Runtime.LifecycleState != "degraded" {
		t.Fatalf("runtime lifecycle_state = %q, want %q", server.agents[identity.AgentID].Runtime.LifecycleState, "degraded")
	}
}

func TestServerApplyAgentSnapshotStartsInitializationWatchCooldownAfterReadyTransition(t *testing.T) {
	now := time.Date(2026, time.March, 29, 18, 0, 0, 0, time.UTC)
	server := New(Options{Now: func() time.Time { return now }})
	token, err := server.issueEnrollmentToken(security.EnrollmentScope{FleetGroupID: "ams-1", TTL: time.Minute}, now)
	if err != nil {
		t.Fatalf("issueEnrollmentToken() error = %v", err)
	}
	identity, err := server.enrollAgent(agentEnrollmentRequest{Token: token.Value, NodeName: "node-a", Version: "1.0.0"}, now.Add(10*time.Second))
	if err != nil {
		t.Fatalf("enrollAgent() error = %v", err)
	}

	initializingRuntime := gatewayRuntimeSnapshotForTest()
	initializingRuntime.AcceptingNewConnections = false
	initializingRuntime.MeRuntimeReady = false
	initializingRuntime.StartupStatus = "starting"
	initializingRuntime.StartupStage = "me_pool_bootstrap"
	initializingRuntime.StartupProgressPct = 42
	initializingRuntime.InitializationStatus = "starting"
	initializingRuntime.InitializationStage = "warming_me_pool"
	initializingRuntime.InitializationProgressPct = 38

	if err := server.applyAgentSnapshot(agentSnapshot{
		AgentID:      identity.AgentID,
		NodeName:     "node-a",
		FleetGroupID: "ams-1",
		Version:      "1.0.0",
		Runtime:      initializingRuntime,
		HasRuntime:   true,
		ObservedAt:   now.Add(20 * time.Second),
	}); err != nil {
		t.Fatalf("applyAgentSnapshot(initializing) error = %v", err)
	}

	if expiresAt := server.initializationWatchCooldowns[identity.AgentID]; !expiresAt.IsZero() {
		t.Fatalf("initialization watch cooldown during active startup = %v, want zero", expiresAt)
	}

	readyObservedAt := now.Add(50 * time.Second)
	if err := server.applyAgentSnapshot(agentSnapshot{
		AgentID:      identity.AgentID,
		NodeName:     "node-a",
		FleetGroupID: "ams-1",
		Version:      "1.0.0",
		Runtime:      gatewayRuntimeSnapshotForTest(),
		HasRuntime:   true,
		ObservedAt:   readyObservedAt,
	}); err != nil {
		t.Fatalf("applyAgentSnapshot(ready) error = %v", err)
	}

	expectedExpiresAt := readyObservedAt.UTC().Add(telemetryInitializationWatchCooldown)
	if expiresAt := server.initializationWatchCooldowns[identity.AgentID]; !expiresAt.Equal(expectedExpiresAt) {
		t.Fatalf("initialization watch cooldown = %v, want %v", expiresAt, expectedExpiresAt)
	}
}

func gatewayRuntimeSnapshotForTest() *gatewayrpc.RuntimeSnapshot {
	return &gatewayrpc.RuntimeSnapshot{
		AcceptingNewConnections:   true,
		MeRuntimeReady:           true,
		Me2DcFallbackEnabled:     true,
		UseMiddleProxy:            true,
		StartupStatus:             "ready",
		StartupStage:              "serving",
		StartupProgressPct:        100,
		InitializationStatus:      "ready",
		Degraded:                  false,
		InitializationStage:       "serving",
		InitializationProgressPct: 100,
		TransportMode:             "middle_proxy",
		UptimeSeconds:             90_061,
		CurrentConnections:        42,
		CurrentConnectionsMe:     39,
		CurrentConnectionsDirect: 3,
		ActiveUsers:               7,
		ConnectionsTotal:          512,
		ConnectionsBadTotal:       9,
		HandshakeTimeoutsTotal:    4,
		ConfiguredUsers:           12,
		Dcs: []*gatewayrpc.RuntimeDCSnapshot{
			{
				Dc:                 2,
				AvailableEndpoints: 3,
				AvailablePct:       100,
				RequiredWriters:    4,
				AliveWriters:       4,
				CoveragePct:        100,
				RttMs:              21.5,
				Load:               18,
			},
		},
		Upstreams: &gatewayrpc.RuntimeUpstreamSnapshot{
			ConfiguredTotal: 2,
			HealthyTotal:    1,
			UnhealthyTotal:  1,
			DirectTotal:     1,
			Socks5Total:     1,
		},
		RecentEvents: []*gatewayrpc.RuntimeEventSnapshot{
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
	defer server.Close()
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

	if err := server.applyAgentSnapshot(agentSnapshot{
		AgentID:      identity.AgentID,
		NodeName:     "node-a",
		FleetGroupID: "ams-1",
		Version:      "1.0.0",
		ObservedAt:   now.Add(20 * time.Second),
	}); err != nil {
		t.Fatalf("applyAgentSnapshot() error = %v", err)
	}

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

// TestApplyAgentSnapshotPrunesStaleInstances verifies that each snapshot is
// treated as the complete instance set for its agent: previously-reported
// instances that are absent from a subsequent snapshot must be removed from
// s.instances so the map cannot leak entries for instances that no longer
// exist on the agent (P2-LOG-09 / L-04).
func TestApplyAgentSnapshotPrunesStaleInstances(t *testing.T) {
	now := time.Date(2026, time.March, 18, 9, 0, 0, 0, time.UTC)
	server := New(Options{
		Now: func() time.Time { return now },
	})
	defer server.Close()

	token, err := server.enrollment.IssueToken(security.EnrollmentScope{
		FleetGroupID: "ams-1",
		TTL:          time.Minute,
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

	// Seed three instances for agent A.
	if err := server.applyAgentSnapshot(agentSnapshot{
		AgentID:      identity.AgentID,
		NodeName:     "node-a",
		FleetGroupID: "ams-1",
		Version:      "1.0.0",
		Instances: []instanceSnapshot{
			{ID: "inst-1", Name: "telemt-1", Version: "2026.03"},
			{ID: "inst-2", Name: "telemt-2", Version: "2026.03"},
			{ID: "inst-3", Name: "telemt-3", Version: "2026.03"},
		},
		ObservedAt: now.Add(10 * time.Second),
	}); err != nil {
		t.Fatalf("applyAgentSnapshot(initial) error = %v", err)
	}

	server.mu.RLock()
	initial := len(server.instances)
	server.mu.RUnlock()
	if initial != 3 {
		t.Fatalf("len(server.instances) after seed = %d, want %d", initial, 3)
	}

	// Apply a new snapshot reporting only two instances — inst-3 must be pruned.
	if err := server.applyAgentSnapshot(agentSnapshot{
		AgentID:      identity.AgentID,
		NodeName:     "node-a",
		FleetGroupID: "ams-1",
		Version:      "1.0.0",
		Instances: []instanceSnapshot{
			{ID: "inst-1", Name: "telemt-1", Version: "2026.03"},
			{ID: "inst-2", Name: "telemt-2", Version: "2026.03"},
		},
		ObservedAt: now.Add(20 * time.Second),
	}); err != nil {
		t.Fatalf("applyAgentSnapshot(pruned) error = %v", err)
	}

	server.mu.RLock()
	defer server.mu.RUnlock()
	if len(server.instances) != 2 {
		t.Fatalf("len(server.instances) after prune = %d, want %d", len(server.instances), 2)
	}
	if _, ok := server.instances["inst-3"]; ok {
		t.Fatal("server.instances still contains pruned inst-3, want removed")
	}
	if _, ok := server.instances["inst-1"]; !ok {
		t.Fatal("server.instances missing inst-1, want present")
	}
	if _, ok := server.instances["inst-2"]; !ok {
		t.Fatal("server.instances missing inst-2, want present")
	}
}

// TestApplyAgentSnapshotDoesNotPruneOtherAgentsInstances verifies that the
// prune step only removes instances owned by the agent emitting the current
// snapshot — instances belonging to other agents must never be touched.
func TestApplyAgentSnapshotDoesNotPruneOtherAgentsInstances(t *testing.T) {
	now := time.Date(2026, time.March, 18, 9, 0, 0, 0, time.UTC)
	server := New(Options{
		Now: func() time.Time { return now },
	})
	defer server.Close()

	tokenA, err := server.enrollment.IssueToken(security.EnrollmentScope{
		FleetGroupID: "ams-1",
		TTL:          time.Minute,
	}, now)
	if err != nil {
		t.Fatalf("IssueToken(A) error = %v", err)
	}
	agentA, err := server.enrollAgent(agentEnrollmentRequest{
		Token:    tokenA.Value,
		NodeName: "node-a",
		Version:  "1.0.0",
	}, now)
	if err != nil {
		t.Fatalf("enrollAgent(A) error = %v", err)
	}

	tokenB, err := server.enrollment.IssueToken(security.EnrollmentScope{
		FleetGroupID: "ams-1",
		TTL:          time.Minute,
	}, now)
	if err != nil {
		t.Fatalf("IssueToken(B) error = %v", err)
	}
	agentB, err := server.enrollAgent(agentEnrollmentRequest{
		Token:    tokenB.Value,
		NodeName: "node-b",
		Version:  "1.0.0",
	}, now)
	if err != nil {
		t.Fatalf("enrollAgent(B) error = %v", err)
	}

	if err := server.applyAgentSnapshot(agentSnapshot{
		AgentID:      agentA.AgentID,
		NodeName:     "node-a",
		FleetGroupID: "ams-1",
		Instances: []instanceSnapshot{
			{ID: "inst-a1", Name: "telemt-a1"},
			{ID: "inst-a2", Name: "telemt-a2"},
		},
		ObservedAt: now.Add(10 * time.Second),
	}); err != nil {
		t.Fatalf("applyAgentSnapshot(A) error = %v", err)
	}
	if err := server.applyAgentSnapshot(agentSnapshot{
		AgentID:      agentB.AgentID,
		NodeName:     "node-b",
		FleetGroupID: "ams-1",
		Instances: []instanceSnapshot{
			{ID: "inst-b1", Name: "telemt-b1"},
		},
		ObservedAt: now.Add(11 * time.Second),
	}); err != nil {
		t.Fatalf("applyAgentSnapshot(B) error = %v", err)
	}

	// Agent A reports only inst-a1 now. inst-a2 must be pruned; inst-b1 must remain.
	if err := server.applyAgentSnapshot(agentSnapshot{
		AgentID:      agentA.AgentID,
		NodeName:     "node-a",
		FleetGroupID: "ams-1",
		Instances: []instanceSnapshot{
			{ID: "inst-a1", Name: "telemt-a1"},
		},
		ObservedAt: now.Add(20 * time.Second),
	}); err != nil {
		t.Fatalf("applyAgentSnapshot(A-prune) error = %v", err)
	}

	server.mu.RLock()
	defer server.mu.RUnlock()
	if _, ok := server.instances["inst-a2"]; ok {
		t.Fatal("inst-a2 still present, want pruned for agent A")
	}
	if _, ok := server.instances["inst-a1"]; !ok {
		t.Fatal("inst-a1 missing, want present for agent A")
	}
	if _, ok := server.instances["inst-b1"]; !ok {
		t.Fatal("inst-b1 missing, must not be touched when agent A reports")
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
	defer first.Close()
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
	defer restored.Close()
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

func TestServerRestoresPersistedCertificateAuthority(t *testing.T) {
	now := time.Date(2026, time.March, 19, 8, 0, 0, 0, time.UTC)
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "panvex.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	defer store.Close()

	first := New(Options{
		Now:   func() time.Time { return now },
		Store: store,
	})
	defer first.Close()
	firstAuthority := first.authority.caPEM
	if firstAuthority == "" {
		t.Fatal("first.authority.caPEM = empty, want persisted authority")
	}

	restored := New(Options{
		Now:   func() time.Time { return now.Add(30 * time.Second) },
		Store: store,
	})
	defer restored.Close()
	if restored.authority.caPEM != firstAuthority {
		t.Fatalf("restored.authority.caPEM = %q, want %q", restored.authority.caPEM, firstAuthority)
	}
}

func TestServerEnrollmentIssuesOperationalCertificateLifetime(t *testing.T) {
	now := time.Date(2026, time.March, 19, 8, 0, 0, 0, time.UTC)
	server := New(Options{
		Now: func() time.Time { return now },
	})
	token, err := server.enrollment.IssueToken(security.EnrollmentScope{
		FleetGroupID: "ams-1",
		TTL:          time.Minute,
	}, now)
	if err != nil {
		t.Fatalf("IssueToken() error = %v", err)
	}

	issuedAt := now.Add(10 * time.Second)
	response, err := server.enrollAgent(agentEnrollmentRequest{
		Token:    token.Value,
		NodeName: "node-a",
		Version:  "1.0.0",
	}, issuedAt)
	if err != nil {
		t.Fatalf("enrollAgent() error = %v", err)
	}

	if lifetime := response.ExpiresAt.Sub(issuedAt); lifetime != 30*24*time.Hour {
		t.Fatalf("certificate lifetime = %v, want %v", lifetime, 30*24*time.Hour)
	}
}

func TestServerRecordsStartupErrorInsteadOfPanickingOnRestoreFailure(t *testing.T) {
	now := time.Date(2026, time.March, 19, 8, 30, 0, 0, time.UTC)
	sqliteStore, err := sqlite.Open(filepath.Join(t.TempDir(), "panvex.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	defer sqliteStore.Close()

	store := &failingStore{
		Store:        sqliteStore,
		listAgentsErr: errors.New("list agents failed"),
	}
	server := New(Options{
		Now:   func() time.Time { return now },
		Store: store,
	})
	defer server.Close()

	if server.StartupError() == nil {
		t.Fatal("StartupError() = nil, want restore failure")
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
	defer first.Close()
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
	defer restored.Close()
	if _, err := restored.enrollAgent(agentEnrollmentRequest{
		Token:    token.Value,
		NodeName: "node-b",
		Version:  "1.0.1",
	}, now.Add(20*time.Second)); err != security.ErrEnrollmentTokenConsumed {
		t.Fatalf("enrollAgent() error = %v, want %v", err, security.ErrEnrollmentTokenConsumed)
	}
}

func TestEnrollmentSetsCertificateDates(t *testing.T) {
	now := time.Date(2026, time.April, 10, 9, 0, 0, 0, time.UTC)
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "panvex.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	defer store.Close()

	server := New(Options{
		Now:   func() time.Time { return now },
		Store: store,
	})
	defer server.Close()

	token, err := server.issueEnrollmentToken(security.EnrollmentScope{
		FleetGroupID: "ams-1",
		TTL:          time.Minute,
	}, now)
	if err != nil {
		t.Fatalf("issueEnrollmentToken() error = %v", err)
	}

	enrolledAt := now.Add(10 * time.Second)
	identity, err := server.enrollAgent(agentEnrollmentRequest{
		Token:    token.Value,
		NodeName: "node-a",
		Version:  "1.0.0",
	}, enrolledAt)
	if err != nil {
		t.Fatalf("enrollAgent() error = %v", err)
	}

	server.mu.RLock()
	agent := server.agents[identity.AgentID]
	server.mu.RUnlock()

	if agent.CertIssuedAt == nil {
		t.Fatal("agent.CertIssuedAt = nil, want non-nil")
	}
	if agent.CertExpiresAt == nil {
		t.Fatal("agent.CertExpiresAt = nil, want non-nil")
	}

	// CertIssuedAt should match the enrollment time.
	if !agent.CertIssuedAt.Equal(enrolledAt.UTC()) {
		t.Fatalf("agent.CertIssuedAt = %v, want %v", *agent.CertIssuedAt, enrolledAt.UTC())
	}

	// CertExpiresAt should be ~30 days after CertIssuedAt.
	expectedExpiry := enrolledAt.UTC().Add(30 * 24 * time.Hour)
	if !agent.CertExpiresAt.Equal(expectedExpiry) {
		t.Fatalf("agent.CertExpiresAt = %v, want %v", *agent.CertExpiresAt, expectedExpiry)
	}
}

// TestTrafficDedupViaSnapshotSeq guards P2-LOG-06 / L-07: two client-usage
// snapshots carrying the same monotonic seq (e.g. the agent resent an
// in-flight batch after a stream reconnect) must only contribute traffic
// once — live gauges may still update.
func TestTrafficDedupViaSnapshotSeq(t *testing.T) {
	now := time.Date(2026, time.April, 18, 8, 0, 0, 0, time.UTC)
	server := New(Options{Now: func() time.Time { return now }})
	defer server.Close()

	const agentID = "agent-dedup"
	const clientID = "client-dedup"

	first := []clientUsageSnapshot{{
		ClientID:         clientID,
		TrafficUsedBytes: 1000,
		ActiveTCPConns:   2,
		ObservedAt:       now,
		Seq:              5,
	}}
	duplicate := []clientUsageSnapshot{{
		ClientID:         clientID,
		TrafficUsedBytes: 1000,
		ActiveTCPConns:   3, // live gauge changed — still accept
		ObservedAt:       now.Add(time.Second),
		Seq:              5, // same seq -> duplicate
	}}

	server.mu.Lock()
	server.applyClientUsageSnapshot(agentID, first)
	server.applyClientUsageSnapshot(agentID, duplicate)
	got := server.clientUsage[clientID][agentID]
	server.mu.Unlock()

	if got.TrafficUsedBytes != 1000 {
		t.Fatalf("TrafficUsedBytes = %d, want %d (dedup failed — delta double-counted)", got.TrafficUsedBytes, 1000)
	}
	if got.ActiveTCPConns != 3 {
		t.Fatalf("ActiveTCPConns = %d, want 3 (live gauge must still refresh)", got.ActiveTCPConns)
	}
}

// TestUsageSeqResetOnAgentRestart guards P2-LOG-06 / L-07: when seq rewinds
// from a higher value back to 1 (agent restart, counters back to zero), the
// incoming "delta" is actually an absolute baseline and must not be added to
// accumulated traffic. Subsequent in-order snapshots resume accumulation.
func TestUsageSeqResetOnAgentRestart(t *testing.T) {
	now := time.Date(2026, time.April, 18, 10, 0, 0, 0, time.UTC)
	server := New(Options{Now: func() time.Time { return now }})
	defer server.Close()

	const agentID = "agent-restart"
	const clientID = "client-restart"

	prior := []clientUsageSnapshot{{
		ClientID:         clientID,
		TrafficUsedBytes: 4096,
		ObservedAt:       now,
		Seq:              10,
	}}
	restart := []clientUsageSnapshot{{
		ClientID:         clientID,
		TrafficUsedBytes: 512, // fresh baseline after restart, not a delta
		ObservedAt:       now.Add(time.Minute),
		Seq:              1,
	}}
	afterRestart := []clientUsageSnapshot{{
		ClientID:         clientID,
		TrafficUsedBytes: 200,
		ObservedAt:       now.Add(2 * time.Minute),
		Seq:              2,
	}}

	server.mu.Lock()
	server.applyClientUsageSnapshot(agentID, prior)
	server.applyClientUsageSnapshot(agentID, restart)
	afterReset := server.clientUsage[clientID][agentID].TrafficUsedBytes
	server.applyClientUsageSnapshot(agentID, afterRestart)
	final := server.clientUsage[clientID][agentID].TrafficUsedBytes
	storedSeq := server.lastUsageSeq[agentID]
	server.mu.Unlock()

	if afterReset != 4096 {
		t.Fatalf("after restart baseline: TrafficUsedBytes = %d, want 4096 (restart delta must not accumulate)", afterReset)
	}
	if final != 4096+200 {
		t.Fatalf("final TrafficUsedBytes = %d, want %d (post-restart deltas should accumulate)", final, 4096+200)
	}
	if storedSeq != 2 {
		t.Fatalf("lastUsageSeq = %d, want 2", storedSeq)
	}
}

// TestUsageDedupIgnoresOutOfOrderStaleSnapshots guards against older seq
// values arriving after a newer one (e.g. race between in-flight snapshots
// after reconnect). Stale seqs must not contribute traffic.
func TestUsageDedupIgnoresOutOfOrderStaleSnapshots(t *testing.T) {
	now := time.Date(2026, time.April, 18, 11, 0, 0, 0, time.UTC)
	server := New(Options{Now: func() time.Time { return now }})
	defer server.Close()

	const agentID = "agent-stale"
	const clientID = "client-stale"

	server.mu.Lock()
	server.applyClientUsageSnapshot(agentID, []clientUsageSnapshot{{
		ClientID: clientID, TrafficUsedBytes: 100, Seq: 7, ObservedAt: now,
	}})
	server.applyClientUsageSnapshot(agentID, []clientUsageSnapshot{{
		ClientID: clientID, TrafficUsedBytes: 999, Seq: 3, ObservedAt: now, // stale
	}})
	got := server.clientUsage[clientID][agentID].TrafficUsedBytes
	server.mu.Unlock()

	if got != 100 {
		t.Fatalf("TrafficUsedBytes = %d, want 100 (stale seq must be ignored)", got)
	}
}

// TestUsageLegacySeqZeroFallsBackToUnconditionalAccumulation preserves the
// pre-P2-LOG-06 behavior for agents that have not yet been upgraded: when
// seq == 0 on the wire, the CP accumulates every delta it sees. Dev-stage
// cutover still keeps this safety net so partial upgrades don't silently
// drop traffic.
func TestUsageLegacySeqZeroFallsBackToUnconditionalAccumulation(t *testing.T) {
	now := time.Date(2026, time.April, 18, 12, 0, 0, 0, time.UTC)
	server := New(Options{Now: func() time.Time { return now }})
	defer server.Close()

	const agentID = "agent-legacy"
	const clientID = "client-legacy"
	legacy := []clientUsageSnapshot{{ClientID: clientID, TrafficUsedBytes: 500, ObservedAt: now}} // Seq = 0

	server.mu.Lock()
	server.applyClientUsageSnapshot(agentID, legacy)
	server.applyClientUsageSnapshot(agentID, legacy)
	got := server.clientUsage[clientID][agentID].TrafficUsedBytes
	server.mu.Unlock()

	if got != 1000 {
		t.Fatalf("legacy accumulation: TrafficUsedBytes = %d, want 1000", got)
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
	defer first.Close()
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
	defer restored.Close()
	if _, err := restored.enrollAgent(agentEnrollmentRequest{
		Token:    token.Value,
		NodeName: "node-b",
		Version:  "1.0.1",
	}, now.Add(2*time.Second)); err != security.ErrEnrollmentTokenExpired {
		t.Fatalf("enrollAgent() error = %v, want %v", err, security.ErrEnrollmentTokenExpired)
	}
}
