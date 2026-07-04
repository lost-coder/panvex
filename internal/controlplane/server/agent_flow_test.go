package server

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/clients"
	"github.com/lost-coder/panvex/internal/controlplane/storage/sqlite"
	"github.com/lost-coder/panvex/internal/gatewayrpc"
	"github.com/lost-coder/panvex/internal/security"
)

func TestServerEnrollAgentConsumesTokenAndIssuesIdentity(t *testing.T) {
	now := time.Date(2026, time.March, 14, 8, 0, 0, 0, time.UTC)
	server := testServerWithSQLite(t, now)
	fleetGroupID := seedTestFleetGroup(t, server.store, "ams-1", now)
	token, err := server.issueEnrollmentToken(security.EnrollmentScope{
		FleetGroupID: fleetGroupID,
		TTL:          time.Minute,
	}, now)
	if err != nil {
		t.Fatalf("issueEnrollmentToken() error = %v", err)
	}

	response, err := server.enrollAgent(context.Background(), agentEnrollmentRequest{
		Token:    token.Value,
		NodeName: "node-a",
		Version:  "1.0.0",
		CSRPEM:   testCSRPEM(t),
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

// TestServerEnrollAgentRejectsAlreadyConsumedToken (C2): once a token is
// consumed by a successful enrollment, a second enrollment with the same
// token value must fail via the ConsumeEnrollmentToken conflict path and
// must NOT reset or re-burn the already-consumed token. This guards the
// idempotency half of the atomic consume; the rollback half (consume
// rolls back together with a failed PutAgent inside the Transact) is
// covered by the storagetest contract "consume token inside transact
// rolls back with the tx".
func TestServerEnrollAgentRejectsAlreadyConsumedToken(t *testing.T) {
	now := time.Date(2026, time.March, 14, 8, 0, 0, 0, time.UTC)
	server := testServerWithSQLite(t, now)
	fleetGroupID := seedTestFleetGroup(t, server.store, "ams-1", now)
	token, err := server.issueEnrollmentToken(security.EnrollmentScope{
		FleetGroupID: fleetGroupID,
		TTL:          time.Minute,
	}, now)
	if err != nil {
		t.Fatalf("issueEnrollmentToken() error = %v", err)
	}

	// First enrollment must succeed.
	if _, err := server.enrollAgent(context.Background(), agentEnrollmentRequest{
		Token:    token.Value,
		NodeName: "node-a",
		Version:  "1.0.0",
		CSRPEM:   testCSRPEM(t),
	}, now.Add(10*time.Second)); err != nil {
		t.Fatalf("first enrollAgent() error = %v", err)
	}

	// Second enrollment with the same (now-consumed) token must fail.
	if _, err := server.enrollAgent(context.Background(), agentEnrollmentRequest{
		Token:    token.Value,
		NodeName: "node-b",
		Version:  "1.0.0",
		CSRPEM:   testCSRPEM(t),
	}, now.Add(20*time.Second)); err == nil {
		t.Fatal("second enrollAgent() error = nil, want token-already-consumed error")
	}

	// The token must remain consumed — not double-burned or reset.
	rec, err := server.store.GetEnrollmentToken(context.Background(), token.Value)
	if err != nil {
		t.Fatalf("GetEnrollmentToken() error = %v", err)
	}
	if rec.ConsumedAt == nil {
		t.Fatal("token ConsumedAt = nil after successful first enrollment, want non-nil")
	}
}

// TestServerEnrollTokenScopeNulledWhenFleetGroupDeleted (C1): verifies the
// ON DELETE SET NULL behaviour introduced in migration 0050.  When the
// fleet group that was baked into a token's scope is deleted, the token's
// fleet_group_id becomes NULL rather than blocking the delete or cascading
// to destroy the token, leaving the operator free to re-scope or revoke it.
func TestServerEnrollTokenScopeNulledWhenFleetGroupDeleted(t *testing.T) {
	now := time.Date(2026, time.March, 14, 8, 0, 0, 0, time.UTC)
	server := testServerWithSQLite(t, now)
	fleetGroupID := seedTestFleetGroup(t, server.store, "ams-1", now)
	token, err := server.issueEnrollmentToken(security.EnrollmentScope{
		FleetGroupID: fleetGroupID,
		TTL:          time.Minute,
	}, now)
	if err != nil {
		t.Fatalf("issueEnrollmentToken() error = %v", err)
	}

	// Delete the fleet group — must succeed (SET NULL, not RESTRICT).
	if err := server.store.DeleteFleetGroup(context.Background(), fleetGroupID); err != nil {
		t.Fatalf("DeleteFleetGroup() error = %v, want nil (SET NULL should allow delete)", err)
	}

	// Token must survive with fleet_group_id = "" (null).
	rec, err := server.store.GetEnrollmentToken(context.Background(), token.Value)
	if err != nil {
		t.Fatalf("GetEnrollmentToken() error = %v", err)
	}
	if rec.FleetGroupID != "" {
		t.Fatalf("token FleetGroupID = %q after group delete, want empty (SET NULL)", rec.FleetGroupID)
	}
	if rec.ConsumedAt != nil {
		t.Fatalf("token burned by group delete: ConsumedAt = %v, want nil", rec.ConsumedAt)
	}
}

func TestServerApplyAgentSnapshotUpdatesInventoryMetricsAndPresence(t *testing.T) {
	now := time.Date(2026, time.March, 14, 8, 0, 0, 0, time.UTC)
	server := testServerWithSQLite(t, now)
	fleetGroupID := seedTestFleetGroup(t, server.store, "ams-1", now)
	token, err := server.issueEnrollmentToken(security.EnrollmentScope{
		FleetGroupID: fleetGroupID,
		TTL:          time.Minute,
	}, now)
	if err != nil {
		t.Fatalf("issueEnrollmentToken() error = %v", err)
	}

	identity, err := server.enrollAgent(context.Background(), agentEnrollmentRequest{
		Token:    token.Value,
		NodeName: "node-a",
		Version:  "1.0.0",
		CSRPEM:   testCSRPEM(t),
	}, now)
	if err != nil {
		t.Fatalf("enrollAgent() error = %v", err)
	}

	if err := server.applyAgentSnapshot(context.Background(), agentSnapshot{
		AgentID:      identity.AgentID,
		NodeName:     "node-a",
		FleetGroupID: fleetGroupID,
		Version:      "1.0.0",
		ReadOnly:     true,
		Instances: []instanceSnapshot{
			{
				ID:                "instance-1",
				Name:              "telemt-a",
				Version:           "2026.03",
				ConfigFingerprint: "cfg-1",
				Connections:       42,
				ReadOnly:          true,
			},
		},
		Metrics: map[string]uint64{
			"requests_total": 128,
		},
		Runtime:    gatewayRuntimeSnapshotForTest(),
		HasRuntime: true,
		ObservedAt: now.Add(15 * time.Second),
	}); err != nil {
		t.Fatalf("applyAgentSnapshot() error = %v", err)
	}

	if server.presence.Evaluate(identity.AgentID, now.Add(20*time.Second)) == "" {
		t.Fatal("presence state = empty, want tracked presence")
	}

	// A2: metric history now lives in the store, not an in-memory ring. Flush
	// the async batch writer synchronously, then read back through the same
	// store-backed path the /api/metrics handler uses.
	server.batchWriter.metricsBuf.Drain(context.Background())
	metrics, err := server.listMetricSnapshots(context.Background())
	if err != nil {
		t.Fatalf("listMetricSnapshots() error = %v", err)
	}
	if len(metrics) != 1 {
		t.Fatalf("len(metrics) = %d, want %d", len(metrics), 1)
	}

	server.mu.RLock()
	defer server.mu.RUnlock()

	if len(server.live.AllInstances()) != 1 {
		t.Fatalf("len(server.live.AllInstances()) = %d, want %d", len(server.live.AllInstances()), 1)
	}

	if !server.liveAgent(identity.AgentID).Runtime.AcceptingNewConnections {
		t.Fatal("agent runtime accepting_new_connections = false, want true")
	}
	if server.liveAgent(identity.AgentID).Runtime.TransportMode != "middle_proxy" {
		t.Fatalf("agent runtime transport_mode = %q, want %q", server.liveAgent(identity.AgentID).Runtime.TransportMode, "middle_proxy")
	}
	if server.liveAgent(identity.AgentID).Runtime.UptimeSeconds != 90_061 {
		t.Fatalf("agent runtime uptime_seconds = %v, want %v", server.liveAgent(identity.AgentID).Runtime.UptimeSeconds, 90_061)
	}
	if got := server.liveAgent(identity.AgentID).Runtime.FailRatePct5m; got != 12.5 {
		t.Fatalf("agent runtime fail_rate_pct_5m = %v, want %v", got, 12.5)
	}
	if !server.liveAgent(identity.AgentID).Runtime.FailRateKnown {
		t.Fatal("agent runtime fail_rate_known = false, want true")
	}
	if got := server.liveAgent(identity.AgentID).Runtime.ConnectAttemptTotal; got != 1000 {
		t.Fatalf("agent runtime connect_attempt_total = %d, want %d", got, 1000)
	}
	if got := server.liveAgent(identity.AgentID).Runtime.ConnectSuccessTotal; got != 875 {
		t.Fatalf("agent runtime connect_success_total = %d, want %d", got, 875)
	}
	if got := server.liveAgent(identity.AgentID).Runtime.ConnectFailTotal; got != 125 {
		t.Fatalf("agent runtime connect_fail_total = %d, want %d", got, 125)
	}
	if got := server.liveAgent(identity.AgentID).Runtime.ConnectFailfastTotal; got != 25 {
		t.Fatalf("agent runtime connect_failfast_total = %d, want %d", got, 25)
	}
}

// TestApplyAgentSnapshotIgnoresRevokedAgent asserts that a heartbeat
// arriving for a deregistered agent does not resurrect the in-memory
// record. Regression guard: prior to the fix, an in-flight snapshot from
// the gRPC stream's tear-down window would fall through to
//
//	s.agents[snapshot.AgentID] = updateAgentRecordFromSnapshot(...)
//
// re-adding the agent to the panel (typically as DEGRADED while the
// telemetry caught up).
func TestApplyAgentSnapshotIgnoresRevokedAgent(t *testing.T) {
	now := time.Date(2026, time.March, 14, 8, 0, 0, 0, time.UTC)
	server := testServerWithSQLite(t, now)
	fleetGroupID := seedTestFleetGroup(t, server.store, "ams-1", now)
	token, err := server.issueEnrollmentToken(security.EnrollmentScope{
		FleetGroupID: fleetGroupID,
		TTL:          time.Minute,
	}, now)
	if err != nil {
		t.Fatalf("issueEnrollmentToken() error = %v", err)
	}
	identity, err := server.enrollAgent(context.Background(), agentEnrollmentRequest{
		Token:    token.Value,
		NodeName: "node-a",
		Version:  "1.0.0",
		CSRPEM:   testCSRPEM(t),
	}, now)
	if err != nil {
		t.Fatalf("enrollAgent() error = %v", err)
	}

	// Mark the agent as revoked the same way handleDeregisterAgent →
	// purgeAgentInMemory does.
	server.mu.Lock()
	server.live.Remove(identity.AgentID)
	server.revokedAgentIDs[identity.AgentID] = struct{}{}
	server.mu.Unlock()

	if err := server.applyAgentSnapshot(context.Background(), agentSnapshot{
		AgentID:      identity.AgentID,
		NodeName:     "node-a",
		FleetGroupID: fleetGroupID,
		Version:      "1.0.0",
		ObservedAt:   now.Add(20 * time.Second),
	}); err != nil {
		t.Fatalf("applyAgentSnapshot() error = %v", err)
	}

	server.mu.RLock()
	_, present := server.liveAgentGet(identity.AgentID)
	server.mu.RUnlock()
	if present {
		t.Fatal("revoked agent reappeared in s.agents after a heartbeat snapshot")
	}
}

func TestServerApplyAgentSnapshotPersistsInventoryAndMetricsAcrossRestart(t *testing.T) {
	now := time.Date(2026, time.March, 15, 10, 0, 0, 0, time.UTC)
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "panvex.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	defer store.Close()

	first := mustNew(t, Options{
		LoginTimingFloor: -1,
		Now:              func() time.Time { return now },
		Store:            store,
	})
	fleetGroupID := seedTestFleetGroup(t, store, "ams-1", now)
	token, err := first.issueEnrollmentToken(security.EnrollmentScope{
		FleetGroupID: fleetGroupID,
		TTL:          time.Minute,
	}, now)
	if err != nil {
		t.Fatalf("issueEnrollmentToken() error = %v", err)
	}

	identity, err := first.enrollAgent(context.Background(), agentEnrollmentRequest{
		Token:    token.Value,
		NodeName: "node-a",
		Version:  "1.0.0",
		CSRPEM:   testCSRPEM(t),
	}, now.Add(10*time.Second))
	if err != nil {
		t.Fatalf("enrollAgent() error = %v", err)
	}

	if err := first.applyAgentSnapshot(context.Background(), agentSnapshot{
		AgentID:      identity.AgentID,
		NodeName:     "node-a",
		FleetGroupID: fleetGroupID,
		Version:      "1.0.0",
		ReadOnly:     true,
		Instances: []instanceSnapshot{
			{
				ID:                "instance-1",
				Name:              "telemt-a",
				Version:           "2026.03",
				ConfigFingerprint: "cfg-1",
				Connections:       42,
				ReadOnly:          true,
			},
		},
		Metrics: map[string]uint64{
			"requests_total": 128,
		},
		Runtime:    gatewayRuntimeSnapshotForTest(),
		HasRuntime: true,
		ObservedAt: now.Add(15 * time.Second),
	}); err != nil {
		t.Fatalf("applyAgentSnapshot() error = %v", err)
	}

	first.Close()

	restored := mustNew(t, Options{
		LoginTimingFloor: -1,
		Now:              func() time.Time { return now.Add(time.Minute) },
		Store:            store,
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

	store := &failingStore{MigrationStore: sqliteStore}
	server := mustNew(t, Options{
		LoginTimingFloor: -1,
		Now:              func() time.Time { return now },
		Store:            store,
	})
	defer server.Close()
	fleetGroupID := seedTestFleetGroup(t, sqliteStore, "ams-1", now)
	token, err := server.issueEnrollmentToken(security.EnrollmentScope{
		FleetGroupID: fleetGroupID,
		TTL:          time.Minute,
	}, now)
	if err != nil {
		t.Fatalf("issueEnrollmentToken() error = %v", err)
	}

	identity, err := server.enrollAgent(context.Background(), agentEnrollmentRequest{
		Token:    token.Value,
		NodeName: "node-a",
		Version:  "1.0.0",
		CSRPEM:   testCSRPEM(t),
	}, now.Add(10*time.Second))
	if err != nil {
		t.Fatalf("enrollAgent() error = %v", err)
	}

	store.putAgentErr = errors.New("put agent failed")

	// Async batch writer means persistence failures do not block the caller.
	if err := server.applyAgentSnapshot(context.Background(), agentSnapshot{
		AgentID:      identity.AgentID,
		NodeName:     "node-a",
		FleetGroupID: fleetGroupID,
		Version:      "1.0.0",
		Runtime:      gatewayRuntimeSnapshotForTest(),
		HasRuntime:   true,
		ObservedAt:   now.Add(20 * time.Second),
	}); err != nil {
		t.Fatalf("applyAgentSnapshot() error = %v, want nil (async persistence)", err)
	}

	// In-memory state should still reflect the snapshot.
	server.mu.RLock()
	agent, exists := server.liveAgentGet(identity.AgentID)
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
	server := testServerWithSQLite(t, now)
	fleetGroupID := seedTestFleetGroup(t, server.store, "ams-1", now)
	token, err := server.issueEnrollmentToken(security.EnrollmentScope{FleetGroupID: fleetGroupID, TTL: time.Minute}, now)
	if err != nil {
		t.Fatalf("issueEnrollmentToken() error = %v", err)
	}
	identity, err := server.enrollAgent(context.Background(), agentEnrollmentRequest{Token: token.Value, NodeName: "node-a", Version: "1.0.0", CSRPEM: testCSRPEM(t)}, now.Add(10*time.Second))
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

	if err := server.applyAgentSnapshot(context.Background(), agentSnapshot{
		AgentID:      identity.AgentID,
		NodeName:     "node-a",
		FleetGroupID: fleetGroupID,
		Version:      "1.0.0",
		Runtime:      runtime,
		HasRuntime:   true,
		ObservedAt:   now.Add(20 * time.Second),
	}); err != nil {
		t.Fatalf("applyAgentSnapshot() error = %v", err)
	}

	if server.liveAgent(identity.AgentID).Runtime.LifecycleState != "degraded" {
		t.Fatalf("runtime lifecycle_state = %q, want %q", server.liveAgent(identity.AgentID).Runtime.LifecycleState, "degraded")
	}
}

func TestServerApplyAgentSnapshotStartsInitializationWatchCooldownAfterReadyTransition(t *testing.T) {
	now := time.Date(2026, time.March, 29, 18, 0, 0, 0, time.UTC)
	server := testServerWithSQLite(t, now)
	fleetGroupID := seedTestFleetGroup(t, server.store, "ams-1", now)
	token, err := server.issueEnrollmentToken(security.EnrollmentScope{FleetGroupID: fleetGroupID, TTL: time.Minute}, now)
	if err != nil {
		t.Fatalf("issueEnrollmentToken() error = %v", err)
	}
	identity, err := server.enrollAgent(context.Background(), agentEnrollmentRequest{Token: token.Value, NodeName: "node-a", Version: "1.0.0", CSRPEM: testCSRPEM(t)}, now.Add(10*time.Second))
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

	if err := server.applyAgentSnapshot(context.Background(), agentSnapshot{
		AgentID:      identity.AgentID,
		NodeName:     "node-a",
		FleetGroupID: fleetGroupID,
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
	if err := server.applyAgentSnapshot(context.Background(), agentSnapshot{
		AgentID:      identity.AgentID,
		NodeName:     "node-a",
		FleetGroupID: fleetGroupID,
		Version:      "1.0.0",
		Runtime:      gatewayRuntimeSnapshotForTest(),
		HasRuntime:   true,
		ObservedAt:   readyObservedAt,
	}); err != nil {
		t.Fatalf("applyAgentSnapshot(ready) error = %v", err)
	}

	// P3-3.2: the cooldown is stamped with the panel clock (s.now()), not the
	// agent-reported ObservedAt. The fixed test clock returns `now`.
	expectedExpiresAt := now.UTC().Add(telemetryInitializationWatchCooldown)
	if expiresAt := server.initializationWatchCooldowns[identity.AgentID]; !expiresAt.Equal(expectedExpiresAt) {
		t.Fatalf("initialization watch cooldown = %v, want %v", expiresAt, expectedExpiresAt)
	}
}

func gatewayRuntimeSnapshotForTest() *gatewayrpc.RuntimeSnapshot {
	return &gatewayrpc.RuntimeSnapshot{
		AcceptingNewConnections:   true,
		MeRuntimeReady:            true,
		Me2DcFallbackEnabled:      true,
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
		CurrentConnectionsMe:      39,
		CurrentConnectionsDirect:  3,
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
			ConfiguredTotal:      2,
			HealthyTotal:         1,
			UnhealthyTotal:       1,
			DirectTotal:          1,
			Socks5Total:          1,
			FailRatePct_5M:       12.5,
			FailRateKnown:        true,
			ConnectAttemptTotal:  1000,
			ConnectSuccessTotal:  875,
			ConnectFailTotal:     125,
			ConnectFailfastTotal: 25,
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

	server := mustNew(t, Options{
		LoginTimingFloor: -1,
		Now:              func() time.Time { return now },
		Store:            store,
	})
	defer server.Close()
	// Enrollment pins the agent to the "default" group; the snapshot
	// later reports a different group id ("ams-1") and the server
	// must keep the enrolled scope regardless.
	defaultGroupID := resolveTestFleetGroupID(t, store, "default")
	amsGroupID := seedTestFleetGroup(t, store, "ams-1", now)
	token, err := server.issueEnrollmentToken(security.EnrollmentScope{
		FleetGroupID: defaultGroupID,
		TTL:          time.Minute,
	}, now)
	if err != nil {
		t.Fatalf("issueEnrollmentToken() error = %v", err)
	}

	identity, err := server.enrollAgent(context.Background(), agentEnrollmentRequest{
		Token:    token.Value,
		NodeName: "node-a",
		Version:  "1.0.0",
		CSRPEM:   testCSRPEM(t),
	}, now.Add(10*time.Second))
	if err != nil {
		t.Fatalf("enrollAgent() error = %v", err)
	}

	if err := server.applyAgentSnapshot(context.Background(), agentSnapshot{
		AgentID:      identity.AgentID,
		NodeName:     "node-a",
		FleetGroupID: amsGroupID,
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
	if record[0].FleetGroupID != defaultGroupID {
		t.Fatalf("ListAgents()[0].FleetGroupID = %q, want %q", record[0].FleetGroupID, defaultGroupID)
	}
}

// TestApplyAgentSnapshotPrunesStaleInstances verifies that each snapshot is
// treated as the complete instance set for its agent: previously-reported
// instances that are absent from a subsequent snapshot must be removed from
// s.instances so the map cannot leak entries for instances that no longer
// exist on the agent (P2-LOG-09 / L-04).
func TestApplyAgentSnapshotPrunesStaleInstances(t *testing.T) {
	now := time.Date(2026, time.March, 18, 9, 0, 0, 0, time.UTC)
	server := testServerWithSQLite(t, now)
	fleetGroupID := seedTestFleetGroup(t, server.store, "ams-1", now)

	token, err := server.issueEnrollmentToken(security.EnrollmentScope{
		FleetGroupID: fleetGroupID,
		TTL:          time.Minute,
	}, now)
	if err != nil {
		t.Fatalf("IssueToken() error = %v", err)
	}
	identity, err := server.enrollAgent(context.Background(), agentEnrollmentRequest{
		Token:    token.Value,
		NodeName: "node-a",
		Version:  "1.0.0",
		CSRPEM:   testCSRPEM(t),
	}, now)
	if err != nil {
		t.Fatalf("enrollAgent() error = %v", err)
	}

	// Seed three instances for agent A.
	if err := server.applyAgentSnapshot(context.Background(), agentSnapshot{
		AgentID:      identity.AgentID,
		NodeName:     "node-a",
		FleetGroupID: fleetGroupID,
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
	initial := len(server.live.AllInstances())
	server.mu.RUnlock()
	if initial != 3 {
		t.Fatalf("len(server.live.AllInstances()) after seed = %d, want %d", initial, 3)
	}

	// Apply a new snapshot reporting only two instances — inst-3 must be pruned.
	if err := server.applyAgentSnapshot(context.Background(), agentSnapshot{
		AgentID:      identity.AgentID,
		NodeName:     "node-a",
		FleetGroupID: fleetGroupID,
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
	if len(server.live.AllInstances()) != 2 {
		t.Fatalf("len(server.live.AllInstances()) after prune = %d, want %d", len(server.live.AllInstances()), 2)
	}
	if _, ok := server.liveInstanceGet("inst-3"); ok {
		t.Fatal("server.instances still contains pruned inst-3, want removed")
	}
	if _, ok := server.liveInstanceGet("inst-1"); !ok {
		t.Fatal("server.instances missing inst-1, want present")
	}
	if _, ok := server.liveInstanceGet("inst-2"); !ok {
		t.Fatal("server.instances missing inst-2, want present")
	}
}

// TestApplyAgentSnapshotDoesNotPruneOtherAgentsInstances verifies that the
// prune step only removes instances owned by the agent emitting the current
// snapshot — instances belonging to other agents must never be touched.
func TestApplyAgentSnapshotDoesNotPruneOtherAgentsInstances(t *testing.T) {
	now := time.Date(2026, time.March, 18, 9, 0, 0, 0, time.UTC)
	server := testServerWithSQLite(t, now)
	fleetGroupID := seedTestFleetGroup(t, server.store, "ams-1", now)

	tokenA, err := server.issueEnrollmentToken(security.EnrollmentScope{
		FleetGroupID: fleetGroupID,
		TTL:          time.Minute,
	}, now)
	if err != nil {
		t.Fatalf("IssueToken(A) error = %v", err)
	}
	agentA, err := server.enrollAgent(context.Background(), agentEnrollmentRequest{
		Token:    tokenA.Value,
		NodeName: "node-a",
		Version:  "1.0.0",
		CSRPEM:   testCSRPEM(t),
	}, now)
	if err != nil {
		t.Fatalf("enrollAgent(A) error = %v", err)
	}

	tokenB, err := server.issueEnrollmentToken(security.EnrollmentScope{
		FleetGroupID: fleetGroupID,
		TTL:          time.Minute,
	}, now)
	if err != nil {
		t.Fatalf("IssueToken(B) error = %v", err)
	}
	agentB, err := server.enrollAgent(context.Background(), agentEnrollmentRequest{
		Token:    tokenB.Value,
		NodeName: "node-b",
		Version:  "1.0.0",
		CSRPEM:   testCSRPEM(t),
	}, now)
	if err != nil {
		t.Fatalf("enrollAgent(B) error = %v", err)
	}

	if err := server.applyAgentSnapshot(context.Background(), agentSnapshot{
		AgentID:      agentA.AgentID,
		NodeName:     "node-a",
		FleetGroupID: fleetGroupID,
		Instances: []instanceSnapshot{
			{ID: "inst-a1", Name: "telemt-a1"},
			{ID: "inst-a2", Name: "telemt-a2"},
		},
		ObservedAt: now.Add(10 * time.Second),
	}); err != nil {
		t.Fatalf("applyAgentSnapshot(A) error = %v", err)
	}
	if err := server.applyAgentSnapshot(context.Background(), agentSnapshot{
		AgentID:      agentB.AgentID,
		NodeName:     "node-b",
		FleetGroupID: fleetGroupID,
		Instances: []instanceSnapshot{
			{ID: "inst-b1", Name: "telemt-b1"},
		},
		ObservedAt: now.Add(11 * time.Second),
	}); err != nil {
		t.Fatalf("applyAgentSnapshot(B) error = %v", err)
	}

	// Agent A reports only inst-a1 now. inst-a2 must be pruned; inst-b1 must remain.
	if err := server.applyAgentSnapshot(context.Background(), agentSnapshot{
		AgentID:      agentA.AgentID,
		NodeName:     "node-a",
		FleetGroupID: fleetGroupID,
		Instances: []instanceSnapshot{
			{ID: "inst-a1", Name: "telemt-a1"},
		},
		ObservedAt: now.Add(20 * time.Second),
	}); err != nil {
		t.Fatalf("applyAgentSnapshot(A-prune) error = %v", err)
	}

	server.mu.RLock()
	defer server.mu.RUnlock()
	if _, ok := server.liveInstanceGet("inst-a2"); ok {
		t.Fatal("inst-a2 still present, want pruned for agent A")
	}
	if _, ok := server.liveInstanceGet("inst-a1"); !ok {
		t.Fatal("inst-a1 missing, want present for agent A")
	}
	if _, ok := server.liveInstanceGet("inst-b1"); !ok {
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

	first := mustNew(t, Options{
		LoginTimingFloor: -1,
		Now:              func() time.Time { return now },
		Store:            store,
	})
	defer first.Close()
	fleetGroupID := seedTestFleetGroup(t, store, "ams-1", now)
	token, err := first.issueEnrollmentToken(security.EnrollmentScope{
		FleetGroupID: fleetGroupID,
		TTL:          time.Minute,
	}, now)
	if err != nil {
		t.Fatalf("issueEnrollmentToken() error = %v", err)
	}

	restored := mustNew(t, Options{
		LoginTimingFloor: -1,
		Now:              func() time.Time { return now.Add(10 * time.Second) },
		Store:            store,
	})
	defer restored.Close()
	response, err := restored.enrollAgent(context.Background(), agentEnrollmentRequest{
		Token:    token.Value,
		NodeName: "node-a",
		Version:  "1.0.0",
		CSRPEM:   testCSRPEM(t),
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

	first := mustNew(t, Options{
		LoginTimingFloor: -1,
		Now:              func() time.Time { return now },
		Store:            store,
	})
	defer first.Close()
	firstAuthority := first.authority.caPEM
	if firstAuthority == "" {
		t.Fatal("first.authority.caPEM = empty, want persisted authority")
	}

	restored := mustNew(t, Options{
		LoginTimingFloor: -1,
		Now:              func() time.Time { return now.Add(30 * time.Second) },
		Store:            store,
	})
	defer restored.Close()
	if restored.authority.caPEM != firstAuthority {
		t.Fatalf("restored.authority.caPEM = %q, want %q", restored.authority.caPEM, firstAuthority)
	}
}

func TestServerEnrollmentIssuesOperationalCertificateLifetime(t *testing.T) {
	now := time.Date(2026, time.March, 19, 8, 0, 0, 0, time.UTC)
	server := testServerWithSQLite(t, now)
	fleetGroupID := seedTestFleetGroup(t, server.store, "ams-1", now)
	token, err := server.issueEnrollmentToken(security.EnrollmentScope{
		FleetGroupID: fleetGroupID,
		TTL:          time.Minute,
	}, now)
	if err != nil {
		t.Fatalf("issueEnrollmentToken() error = %v", err)
	}

	issuedAt := now.Add(10 * time.Second)
	response, err := server.enrollAgent(context.Background(), agentEnrollmentRequest{
		Token:    token.Value,
		NodeName: "node-a",
		Version:  "1.0.0",
		CSRPEM:   testCSRPEM(t),
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
		MigrationStore: sqliteStore,
		listAgentsErr:  errors.New("list agents failed"),
	}
	server := mustNew(t, Options{
		LoginTimingFloor: -1,
		Now:              func() time.Time { return now },
		Store:            store,
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

	first := mustNew(t, Options{
		LoginTimingFloor: -1,
		Now:              func() time.Time { return now },
		Store:            store,
	})
	defer first.Close()
	fleetGroupID := seedTestFleetGroup(t, store, "ams-1", now)
	token, err := first.issueEnrollmentToken(security.EnrollmentScope{
		FleetGroupID: fleetGroupID,
		TTL:          time.Minute,
	}, now)
	if err != nil {
		t.Fatalf("issueEnrollmentToken() error = %v", err)
	}

	if _, err := first.enrollAgent(context.Background(), agentEnrollmentRequest{
		Token:    token.Value,
		NodeName: "node-a",
		Version:  "1.0.0",
		CSRPEM:   testCSRPEM(t),
	}, now.Add(10*time.Second)); err != nil {
		t.Fatalf("enrollAgent() error = %v", err)
	}

	restored := mustNew(t, Options{
		LoginTimingFloor: -1,
		Now:              func() time.Time { return now.Add(20 * time.Second) },
		Store:            store,
	})
	defer restored.Close()
	if _, err := restored.enrollAgent(context.Background(), agentEnrollmentRequest{
		Token:    token.Value,
		NodeName: "node-b",
		Version:  "1.0.1",
		CSRPEM:   testCSRPEM(t),
	}, now.Add(20*time.Second)); !errors.Is(err, security.ErrEnrollmentTokenConsumed) {
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

	server := mustNew(t, Options{
		LoginTimingFloor: -1,
		Now:              func() time.Time { return now },
		Store:            store,
	})
	defer server.Close()
	fleetGroupID := seedTestFleetGroup(t, store, "ams-1", now)

	token, err := server.issueEnrollmentToken(security.EnrollmentScope{
		FleetGroupID: fleetGroupID,
		TTL:          time.Minute,
	}, now)
	if err != nil {
		t.Fatalf("issueEnrollmentToken() error = %v", err)
	}

	enrolledAt := now.Add(10 * time.Second)
	identity, err := server.enrollAgent(context.Background(), agentEnrollmentRequest{
		Token:    token.Value,
		NodeName: "node-a",
		Version:  "1.0.0",
		CSRPEM:   testCSRPEM(t),
	}, enrolledAt)
	if err != nil {
		t.Fatalf("enrollAgent() error = %v", err)
	}

	server.mu.RLock()
	agent := server.liveAgent(identity.AgentID)
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

// TestEnrollAgentCertIssuanceFailureLeavesNoPartialState pins D-1: when
// cert issuance fails, enrollAgent must not leave the agent persisted in
// the DB or in the in-memory mirror. Pre-D-1 ordering wrote PutAgent first
// and only then issued the cert, so a cert-issuance error left a partial
// row + memory entry that required manual cleanup.
//
// We inject the failure by swapping the CA signing key for one on a
// different curve so x509.CreateCertificate fails inside
// issueAgentCertificateFromCSR before any cert bytes are produced.
func TestEnrollAgentCertIssuanceFailureLeavesNoPartialState(t *testing.T) {
	now := time.Date(2026, time.May, 12, 10, 0, 0, 0, time.UTC)
	server := testServerWithSQLite(t, now)
	fleetGroupID := seedTestFleetGroup(t, server.store, "ams-1", now)
	token, err := server.issueEnrollmentToken(security.EnrollmentScope{
		FleetGroupID: fleetGroupID,
		TTL:          time.Minute,
	}, now)
	if err != nil {
		t.Fatalf("issueEnrollmentToken() error = %v", err)
	}

	// Sabotage cert issuance: swap the CA signing key for one on a
	// different curve so x509.CreateCertificate fails ("provided
	// PrivateKey doesn't match parent's PublicKey") instead of
	// panicking on nil.
	mismatched, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	if err != nil {
		t.Fatalf("ecdsa.GenerateKey() error = %v", err)
	}
	server.authority.privateKey = mismatched

	_, err = server.enrollAgent(context.Background(), agentEnrollmentRequest{
		Token:    token.Value,
		NodeName: "node-fail",
		Version:  "1.0.0",
		CSRPEM:   testCSRPEM(t),
	}, now.Add(10*time.Second))
	if err == nil {
		t.Fatal("enrollAgent() error = nil, want cert issuance failure")
	}

	// No agent row should be persisted.
	rows, listErr := server.store.ListAgents(context.Background())
	if listErr != nil {
		t.Fatalf("ListAgents() error = %v", listErr)
	}
	for _, r := range rows {
		if r.NodeName == "node-fail" {
			t.Fatalf("partial agent row persisted despite cert failure: %+v", r)
		}
	}

	// No in-memory mirror entry either.
	server.mu.RLock()
	defer server.mu.RUnlock()
	for id, a := range server.liveAgentsForTest() {
		if a.NodeName == "node-fail" {
			t.Fatalf("partial in-memory agent entry id=%s: %+v", id, a)
		}
	}
}

// TestUsageAccumulatesDeltasFromCumulativeTotals: the panel derives the
// accumulation delta as total - last_total within one agent boot epoch;
// a replayed/duplicate total contributes exactly zero (P4).
func TestUsageAccumulatesDeltasFromCumulativeTotals(t *testing.T) {
	now := time.Date(2026, time.April, 18, 8, 0, 0, 0, time.UTC)
	server := testServerWithSQLite(t, now)
	defer server.Close()

	const agentID = "agent-cumulative"
	const clientID = "client-cumulative"
	const bootID = "boot-a"
	seedClientAndAgentRows(t, server, clientID, agentID, now)

	first := []clients.UsageReport{{
		ClientID: clientID, TotalBytes: 1000, ActiveTCPConns: 2, ObservedAt: now,
	}}
	second := []clients.UsageReport{{
		ClientID: clientID, TotalBytes: 1600, ActiveTCPConns: 2, ObservedAt: now.Add(time.Minute),
	}}
	duplicate := []clients.UsageReport{{
		ClientID: clientID, TotalBytes: 1600, ActiveTCPConns: 3, // live gauge changed — still refreshes
		ObservedAt: now.Add(2 * time.Minute),
	}}

	server.mu.Lock()
	server.applyClientUsageSnapshot(context.Background(), agentID, bootID, first)
	server.applyClientUsageSnapshot(context.Background(), agentID, bootID, second)
	server.applyClientUsageSnapshot(context.Background(), agentID, bootID, duplicate)
	got := mirrorUsage(server, clientID, agentID)
	server.mu.Unlock()

	// 1000 (first report of the pair = whole boot epoch) + 600 + 0 (dup).
	if got.TrafficUsedBytes != 1600 {
		t.Fatalf("TrafficUsedBytes = %d, want 1600 (replay must contribute 0)", got.TrafficUsedBytes)
	}
	if got.ActiveTCPConns != 3 {
		t.Fatalf("ActiveTCPConns = %d, want 3 (live gauge must still refresh)", got.ActiveTCPConns)
	}
	if got.AgentBootID != bootID || got.LastTotalBytes != 1600 {
		t.Fatalf("watermark = (%q, %d), want (%q, 1600)", got.AgentBootID, got.LastTotalBytes, bootID)
	}
}

// TestUsageAgentRestartCountsFreshEpochInFull: a new agent_boot_id means
// the agent's counter restarted from zero — the whole reported total is
// new traffic (the old protocol DROPPED the first post-restart batch;
// this is the audit-#8 fix).
func TestUsageAgentRestartCountsFreshEpochInFull(t *testing.T) {
	now := time.Date(2026, time.April, 18, 10, 0, 0, 0, time.UTC)
	server := testServerWithSQLite(t, now)
	defer server.Close()

	const agentID = "agent-restart"
	const clientID = "client-restart"
	seedClientAndAgentRows(t, server, clientID, agentID, now)

	server.mu.Lock()
	server.applyClientUsageSnapshot(context.Background(), agentID, "boot-1", []clients.UsageReport{{
		ClientID: clientID, TotalBytes: 4096, ObservedAt: now,
	}})
	// Agent restarted: fresh epoch, counter began at zero, first report 512.
	server.applyClientUsageSnapshot(context.Background(), agentID, "boot-2", []clients.UsageReport{{
		ClientID: clientID, TotalBytes: 512, ObservedAt: now.Add(time.Minute),
	}})
	afterRestart := mirrorUsage(server, clientID, agentID)
	// Next tick within boot-2.
	server.applyClientUsageSnapshot(context.Background(), agentID, "boot-2", []clients.UsageReport{{
		ClientID: clientID, TotalBytes: 712, ObservedAt: now.Add(2 * time.Minute),
	}})
	final := mirrorUsage(server, clientID, agentID)
	server.mu.Unlock()

	if afterRestart.TrafficUsedBytes != 4096+512 {
		t.Fatalf("post-restart total = %d, want %d (fresh epoch counts in full, not dropped)",
			afterRestart.TrafficUsedBytes, 4096+512)
	}
	if final.TrafficUsedBytes != 4096+712 {
		t.Fatalf("final total = %d, want %d", final.TrafficUsedBytes, 4096+712)
	}
	if final.AgentBootID != "boot-2" || final.LastTotalBytes != 712 {
		t.Fatalf("watermark = (%q, %d), want (boot-2, 712)", final.AgentBootID, final.LastTotalBytes)
	}
}

// TestUsageTotalRewindClampsToZeroAndRebaselines: within one boot epoch a
// cumulative counter must never decrease. If it does (agent bug, counter
// corruption), the panel must not accumulate garbage: delta clamps to 0,
// the watermark adopts the new total so counting resumes, and the alert
// key client_usage_total_rewind is logged.
func TestUsageTotalRewindClampsToZeroAndRebaselines(t *testing.T) {
	now := time.Date(2026, time.April, 18, 11, 0, 0, 0, time.UTC)
	server := testServerWithSQLite(t, now)
	defer server.Close()

	const agentID = "agent-rewind"
	const clientID = "client-rewind"
	const bootID = "boot-x"
	seedClientAndAgentRows(t, server, clientID, agentID, now)

	server.mu.Lock()
	server.applyClientUsageSnapshot(context.Background(), agentID, bootID, []clients.UsageReport{{
		ClientID: clientID, TotalBytes: 100, ObservedAt: now,
	}})
	server.applyClientUsageSnapshot(context.Background(), agentID, bootID, []clients.UsageReport{{
		ClientID: clientID, TotalBytes: 40, ObservedAt: now.Add(time.Minute), // rewind!
	}})
	afterRewind := mirrorUsage(server, clientID, agentID)
	server.applyClientUsageSnapshot(context.Background(), agentID, bootID, []clients.UsageReport{{
		ClientID: clientID, TotalBytes: 90, ObservedAt: now.Add(2 * time.Minute), // resumes from 40
	}})
	final := mirrorUsage(server, clientID, agentID)
	server.mu.Unlock()

	if afterRewind.TrafficUsedBytes != 100 {
		t.Fatalf("after rewind total = %d, want 100 (clamped, no garbage delta)", afterRewind.TrafficUsedBytes)
	}
	if afterRewind.LastTotalBytes != 40 {
		t.Fatalf("after rewind watermark = %d, want 40 (rebaseline so counting resumes)", afterRewind.LastTotalBytes)
	}
	if final.TrafficUsedBytes != 150 {
		t.Fatalf("final total = %d, want 150 (100 + (90-40))", final.TrafficUsedBytes)
	}
}

// TestUsageAdoptionSeededRowBaselinesFirstReport: discovery adoption seeds
// the client_usage row from Telemt's OWN cumulative counter
// (seedClientUsage), which already contains the current agent-boot
// window. The first cumulative report for such a row (empty AgentBootID
// watermark) must baseline, not accumulate — otherwise that window is
// double-counted.
func TestUsageAdoptionSeededRowBaselinesFirstReport(t *testing.T) {
	now := time.Date(2026, time.April, 18, 12, 0, 0, 0, time.UTC)
	server := testServerWithSQLite(t, now)
	defer server.Close()

	const agentID = "agent-adopt"
	const clientID = "client-adopt"
	seedClientAndAgentRows(t, server, clientID, agentID, now)

	// Discovery adoption seeded 5000 bytes from Telemt's counter.
	server.seedClientUsage(context.Background(), clientID, agentID, 5000, 1, 1, now)

	server.mu.Lock()
	// First cumulative report: the agent counted 300 bytes since ITS boot —
	// a subset of the 5000 already seeded. Must not add.
	server.applyClientUsageSnapshot(context.Background(), agentID, "boot-1", []clients.UsageReport{{
		ClientID: clientID, TotalBytes: 300, ObservedAt: now.Add(time.Minute),
	}})
	afterBaseline := mirrorUsage(server, clientID, agentID)
	// Second report: +200 new bytes — accumulates normally.
	server.applyClientUsageSnapshot(context.Background(), agentID, "boot-1", []clients.UsageReport{{
		ClientID: clientID, TotalBytes: 500, ObservedAt: now.Add(2 * time.Minute),
	}})
	final := mirrorUsage(server, clientID, agentID)
	server.mu.Unlock()

	if afterBaseline.TrafficUsedBytes != 5000 {
		t.Fatalf("after first report total = %d, want 5000 (baseline adopt, no double count)", afterBaseline.TrafficUsedBytes)
	}
	if afterBaseline.AgentBootID != "boot-1" || afterBaseline.LastTotalBytes != 300 {
		t.Fatalf("watermark = (%q, %d), want (boot-1, 300)", afterBaseline.AgentBootID, afterBaseline.LastTotalBytes)
	}
	if final.TrafficUsedBytes != 5200 {
		t.Fatalf("final total = %d, want 5200 (5000 + (500-300))", final.TrafficUsedBytes)
	}
}

func TestServerExpiredEnrollmentTokenRemainsRejectedAfterRestart(t *testing.T) {
	now := time.Date(2026, time.March, 15, 8, 0, 0, 0, time.UTC)
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "panvex.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	defer store.Close()

	first := mustNew(t, Options{
		LoginTimingFloor: -1,
		Now:              func() time.Time { return now },
		Store:            store,
	})
	defer first.Close()
	fleetGroupID := seedTestFleetGroup(t, store, "ams-1", now)
	token, err := first.issueEnrollmentToken(security.EnrollmentScope{
		FleetGroupID: fleetGroupID,
		TTL:          time.Second,
	}, now)
	if err != nil {
		t.Fatalf("issueEnrollmentToken() error = %v", err)
	}

	restored := mustNew(t, Options{
		LoginTimingFloor: -1,
		Now:              func() time.Time { return now.Add(2 * time.Second) },
		Store:            store,
	})
	defer restored.Close()
	if _, err := restored.enrollAgent(context.Background(), agentEnrollmentRequest{
		Token:    token.Value,
		NodeName: "node-b",
		Version:  "1.0.1",
		CSRPEM:   testCSRPEM(t),
	}, now.Add(2*time.Second)); !errors.Is(err, security.ErrEnrollmentTokenExpired) {
		t.Fatalf("enrollAgent() error = %v, want %v", err, security.ErrEnrollmentTokenExpired)
	}
}

// TestZeroLiveGaugesForUntouchedClientsTouchedSubset verifies P-11: when an
// agent reports a snapshot covering exactly the clients it currently owns
// gauges for, no entry should be zeroed. The in-memory gauges remain intact
// because every clientID is in `seen`.
func TestZeroLiveGaugesForUntouchedClientsTouchedSubset(t *testing.T) {
	now := time.Date(2026, time.April, 20, 9, 0, 0, 0, time.UTC)
	server := testServerWithSQLite(t, now)
	defer server.Close()

	const agentID = "agent-p11-touched"

	batch := []clients.UsageReport{
		{ClientID: "client-1", ActiveTCPConns: 7, ActiveUniqueIPs: 3, ObservedAt: now},
		{ClientID: "client-2", ActiveTCPConns: 5, ActiveUniqueIPs: 2, ObservedAt: now},
		{ClientID: "client-3", ActiveTCPConns: 1, ActiveUniqueIPs: 1, ObservedAt: now},
	}
	for _, c := range batch {
		seedClientAndAgentRows(t, server, string(c.ClientID), agentID, now)
	}

	// Seed 3 clients via a snapshot, then re-publish the same set — none
	// should be zeroed because every clientID is "touched" again.
	server.mu.Lock()
	server.applyClientUsageSnapshot(context.Background(), agentID, "boot-1", batch)
	server.applyClientUsageSnapshot(context.Background(), agentID, "boot-1", batch)
	server.mu.Unlock()

	for _, c := range batch {
		got := mirrorUsage(server, string(c.ClientID), agentID)
		if got.ActiveTCPConns != c.ActiveTCPConns {
			t.Fatalf("client %s ActiveTCPConns = %d, want %d (touched client must keep its gauge)", c.ClientID, got.ActiveTCPConns, c.ActiveTCPConns)
		}
	}
}

// TestZeroLiveGaugesForUntouchedClientsZerosUntouched verifies P-11: when
// an agent reports a snapshot that omits a previously-tracked client, that
// client's live gauges (ActiveTCPConns, ActiveUniqueIPs) must be zeroed
// for this agent while accumulated traffic stays intact.
func TestZeroLiveGaugesForUntouchedClientsZerosUntouched(t *testing.T) {
	now := time.Date(2026, time.April, 20, 10, 0, 0, 0, time.UTC)
	server := testServerWithSQLite(t, now)
	defer server.Close()

	const agentID = "agent-p11-untouched"
	for _, id := range []string{"client-A", "client-B", "client-C"} {
		seedClientAndAgentRows(t, server, id, agentID, now)
	}

	// Cumulative totals (first report of each pair counts in full).
	full := []clients.UsageReport{
		{ClientID: "client-A", ActiveTCPConns: 9, ActiveUniqueIPs: 3, TotalBytes: 1024, ObservedAt: now},
		{ClientID: "client-B", ActiveTCPConns: 4, ActiveUniqueIPs: 2, TotalBytes: 512, ObservedAt: now},
		{ClientID: "client-C", ActiveTCPConns: 1, ActiveUniqueIPs: 1, TotalBytes: 256, ObservedAt: now},
	}
	// Second snapshot drops client-B; A and C advance their cumulative totals.
	partial := []clients.UsageReport{
		{ClientID: "client-A", ActiveTCPConns: 11, ActiveUniqueIPs: 4, TotalBytes: 1124, ObservedAt: now.Add(time.Second)},
		{ClientID: "client-C", ActiveTCPConns: 2, ActiveUniqueIPs: 1, TotalBytes: 306, ObservedAt: now.Add(time.Second)},
	}

	server.mu.Lock()
	server.applyClientUsageSnapshot(context.Background(), agentID, "boot-1", full)
	server.applyClientUsageSnapshot(context.Background(), agentID, "boot-1", partial)
	server.mu.Unlock()

	gotB := mirrorUsage(server, "client-B", agentID)
	if gotB.ActiveTCPConns != 0 {
		t.Fatalf("client-B ActiveTCPConns = %d, want 0 (untouched client must be zeroed)", gotB.ActiveTCPConns)
	}
	if gotB.ActiveUniqueIPs != 0 {
		t.Fatalf("client-B ActiveUniqueIPs = %d, want 0 (untouched client must be zeroed)", gotB.ActiveUniqueIPs)
	}
	if gotB.TrafficUsedBytes != 512 {
		t.Fatalf("client-B TrafficUsedBytes = %d, want 512 (accumulated traffic must be preserved)", gotB.TrafficUsedBytes)
	}

	// Touched clients keep their fresh gauges.
	gotA := mirrorUsage(server, "client-A", agentID)
	if gotA.ActiveTCPConns != 11 {
		t.Fatalf("client-A ActiveTCPConns = %d, want 11", gotA.ActiveTCPConns)
	}
}

// TestClientUsageMirrorTracksWrites verifies that applying a usage snapshot
// records every (client, agent) pair in the clients.Service mirror — the
// single owner of usage state after C1 removed the Server-owned maps and the
// agentClientUsage reverse index.
func TestClientUsageMirrorTracksWrites(t *testing.T) {
	now := time.Date(2026, time.April, 20, 11, 0, 0, 0, time.UTC)
	server := testServerWithSQLite(t, now)
	defer server.Close()

	const agentID = "agent-p11-index"
	seedClientAndAgentRows(t, server, "c1", agentID, now)
	seedClientAndAgentRows(t, server, "c2", agentID, now)

	server.mu.Lock()
	server.applyClientUsageSnapshot(context.Background(), agentID, "boot-1", []clients.UsageReport{
		{ClientID: "c1", ObservedAt: now},
		{ClientID: "c2", ObservedAt: now},
	})
	server.mu.Unlock()

	if _, ok := server.clientsSvc.MirrorUsageEntryFor("c1", agentID); !ok {
		t.Fatal("mirror missing c1 usage after write")
	}
	if _, ok := server.clientsSvc.MirrorUsageEntryFor("c2", agentID); !ok {
		t.Fatal("mirror missing c2 usage after write")
	}
}

// TestZeroLiveGaugesForUntouchedClientsScalesWithAgentNotPanel verifies
// P-11's headline benefit: zeroLiveGaugesForUntouchedClients does NOT
// touch clients owned by other agents. Two agents A and B each own 100
// disjoint clients; A then sends a partial snapshot covering only its
// first client. The 99 clients dropped from A's snapshot must be zeroed
// for A; B's 100 clients must be untouched (the test fails if the old
// outer×inner full scan zeroed any of B's gauges).
func TestZeroLiveGaugesForUntouchedClientsScalesWithAgentNotPanel(t *testing.T) {
	now := time.Date(2026, time.April, 20, 12, 0, 0, 0, time.UTC)
	server := testServerWithSQLite(t, now)
	defer server.Close()

	const agentA = "agent-A"
	const agentB = "agent-B"

	mkBatch := func(prefix string, count int) []clients.UsageReport {
		out := make([]clients.UsageReport, 0, count)
		for i := 0; i < count; i++ {
			out = append(out, clients.UsageReport{
				ClientID:        clients.ClientID(fmt.Sprintf("%s-c%03d", prefix, i)),
				ActiveTCPConns:  3,
				ActiveUniqueIPs: 2,
				ObservedAt:      now,
			})
		}
		return out
	}

	for i := 0; i < 100; i++ {
		seedClientAndAgentRows(t, server, fmt.Sprintf("A-c%03d", i), agentA, now)
		seedClientAndAgentRows(t, server, fmt.Sprintf("B-c%03d", i), agentB, now)
	}

	server.mu.Lock()
	server.applyClientUsageSnapshot(context.Background(), agentA, "boot-a", mkBatch("A", 100))
	server.applyClientUsageSnapshot(context.Background(), agentB, "boot-b", mkBatch("B", 100))

	// Agent A reports a snapshot containing only its very first client; the
	// other 99 must be zeroed for agent A. Agent B's gauges must NOT change.
	server.applyClientUsageSnapshot(context.Background(), agentA, "boot-a", []clients.UsageReport{
		{ClientID: "A-c000", ActiveTCPConns: 3, ActiveUniqueIPs: 2, ObservedAt: now.Add(time.Second)},
	})
	server.mu.Unlock()

	// Agent A: c000 stays, c001..c099 zeroed.
	if got := mirrorUsage(server, "A-c000", agentA).ActiveTCPConns; got != 3 {
		t.Fatalf("agentA c000 ActiveTCPConns = %d, want 3", got)
	}
	for i := 1; i < 100; i++ {
		key := fmt.Sprintf("A-c%03d", i)
		if got := mirrorUsage(server, key, agentA).ActiveTCPConns; got != 0 {
			t.Fatalf("agentA %s ActiveTCPConns = %d, want 0 (untouched)", key, got)
		}
	}

	// Agent B's 100 gauges are owned by a different agent and must not be
	// touched by A's snapshot processing.
	for i := 0; i < 100; i++ {
		key := fmt.Sprintf("B-c%03d", i)
		if got := mirrorUsage(server, key, agentB).ActiveTCPConns; got != 3 {
			t.Fatalf("agentB %s ActiveTCPConns = %d, want 3 (other agent must not affect B)", key, got)
		}
	}
}
