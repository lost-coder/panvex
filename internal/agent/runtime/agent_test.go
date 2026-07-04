package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/lost-coder/panvex/internal/agent/telemt"
	"github.com/lost-coder/panvex/internal/gatewayrpc"
)

func TestAgentBuildSnapshotMarksLifecycleRegressionAsDegraded(t *testing.T) {
	client := &fakeTelemtClient{
		state: telemt.RuntimeState{
			Version:       "2026.03",
			ReadOnly:      false,
			UptimeSeconds: 120,
			Connections:   8,
			Gates: telemt.RuntimeGates{
				AcceptingNewConnections: false,
				MERuntimeReady:          false,
				ME2DCFallbackEnabled:    false,
				UseMiddleProxy:          true,
				StartupStatus:           "ready",
				StartupStage:            "serving",
				StartupProgressPct:      100,
			},
			Initialization: telemt.RuntimeInitialization{
				Status:        "ready",
				Degraded:      false,
				CurrentStage:  "serving",
				ProgressPct:   100,
				TransportMode: "middle_proxy",
			},
		},
	}
	agent := New(Config{AgentID: "agent-1", NodeName: "node-a", FleetGroupID: "ams-1", Version: "1.0.0"}, client)

	if _, err := agent.BuildRuntimeSnapshot(context.Background(), time.Date(2026, time.March, 14, 8, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("first BuildRuntimeSnapshot() error = %v", err)
	}

	client.state.Gates.StartupStatus = "starting"
	client.state.Gates.StartupStage = "booting"
	client.state.Gates.StartupProgressPct = 10
	client.state.Initialization.Status = "starting"
	client.state.Initialization.CurrentStage = "booting"
	client.state.Initialization.ProgressPct = 10

	snapshot, err := agent.BuildRuntimeSnapshot(context.Background(), time.Date(2026, time.March, 14, 8, 1, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("second BuildRuntimeSnapshot() error = %v", err)
	}
	if !snapshot.Runtime.Degraded {
		t.Fatal("snapshot.Runtime.Degraded = false, want true for lifecycle regression")
	}
	if snapshot.Runtime.StartupStatus != "starting" {
		t.Fatalf("snapshot.Runtime.StartupStatus = %q, want %q", snapshot.Runtime.StartupStatus, "starting")
	}
}

func TestAgentRuntimeSnapshotIntervalUsesFastCadenceDuringInitializationAndCooldown(t *testing.T) {
	client := &fakeTelemtClient{
		state: telemt.RuntimeState{
			Gates: telemt.RuntimeGates{
				AcceptingNewConnections: false,
				MERuntimeReady:          false,
				StartupStatus:           "starting",
				StartupStage:            "booting",
				StartupProgressPct:      12,
			},
			Initialization: telemt.RuntimeInitialization{
				Status:        "starting",
				CurrentStage:  "warming_me_pool",
				ProgressPct:   18,
				TransportMode: "direct",
			},
		},
	}
	agent := New(Config{AgentID: "agent-1", NodeName: "node-a", FleetGroupID: "ams-1", Version: "1.0.0"}, client)

	baseInterval := time.Minute
	fastInterval := 3 * time.Second
	now := time.Date(2026, time.March, 29, 18, 0, 0, 0, time.UTC)

	if interval := agent.RuntimeSnapshotInterval(baseInterval, fastInterval, now); interval != baseInterval {
		t.Fatalf("RuntimeSnapshotInterval() before first runtime snapshot = %v, want %v", interval, baseInterval)
	}

	if _, err := agent.BuildRuntimeSnapshot(context.Background(), now); err != nil {
		t.Fatalf("BuildRuntimeSnapshot(initializing) error = %v", err)
	}
	if interval := agent.RuntimeSnapshotInterval(baseInterval, fastInterval, now.Add(5*time.Second)); interval != fastInterval {
		t.Fatalf("RuntimeSnapshotInterval() during initialization = %v, want %v", interval, fastInterval)
	}

	client.state.Gates.AcceptingNewConnections = true
	client.state.Gates.MERuntimeReady = true
	client.state.Gates.StartupStatus = "ready"
	client.state.Gates.StartupStage = "steady_state"
	client.state.Gates.StartupProgressPct = 100
	client.state.Initialization.Status = "ready"
	client.state.Initialization.CurrentStage = "steady_state"
	client.state.Initialization.ProgressPct = 100

	readyAt := now.Add(20 * time.Second)
	if _, err := agent.BuildRuntimeSnapshot(context.Background(), readyAt); err != nil {
		t.Fatalf("BuildRuntimeSnapshot(ready) error = %v", err)
	}
	if interval := agent.RuntimeSnapshotInterval(baseInterval, fastInterval, readyAt.Add(30*time.Second)); interval != fastInterval {
		t.Fatalf("RuntimeSnapshotInterval() during cooldown = %v, want %v", interval, fastInterval)
	}
	if interval := agent.RuntimeSnapshotInterval(baseInterval, fastInterval, readyAt.Add(runtimeInitializationCooldown).Add(time.Second)); interval != baseInterval {
		t.Fatalf("RuntimeSnapshotInterval() after cooldown = %v, want %v", interval, baseInterval)
	}
}

func TestAgentBuildSnapshotUsesTelemtRuntimeState(t *testing.T) {
	client := &fakeTelemtClient{
		state: telemt.RuntimeState{
			Version:       "2026.03",
			ReadOnly:      true,
			UptimeSeconds: 90_061,
			Connections:   42,
			Gates: telemt.RuntimeGates{
				AcceptingNewConnections: true,
				MERuntimeReady:          true,
				ME2DCFallbackEnabled:    true,
				UseMiddleProxy:          true,
				StartupStatus:           "ready",
				StartupStage:            "serving",
				StartupProgressPct:      100,
			},
			Initialization: telemt.RuntimeInitialization{
				Status:        "ready",
				Degraded:      false,
				CurrentStage:  "serving",
				ProgressPct:   100,
				TransportMode: "middle_proxy",
			},
			ConnectionTotals: telemt.RuntimeConnectionTotals{
				CurrentConnections:       42,
				CurrentConnectionsME:     39,
				CurrentConnectionsDirect: 3,
				ActiveUsers:              7,
			},
			Summary: telemt.RuntimeSummary{
				ConnectionsTotal:       512,
				ConnectionsBadTotal:    9,
				HandshakeTimeoutsTotal: 4,
				ConfiguredUsers:        12,
			},
			DCs: []telemt.RuntimeDC{
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
			Upstreams: telemt.RuntimeUpstreamSummary{
				ConfiguredTotal: 2,
				HealthyTotal:    1,
				UnhealthyTotal:  1,
				DirectTotal:     1,
				SOCKS5Total:     1,
				Rows: []telemt.RuntimeUpstream{
					{
						UpstreamID:         1,
						RouteKind:          "direct",
						Address:            "direct",
						Healthy:            true,
						Fails:              0,
						EffectiveLatencyMs: 11.2,
					},
				},
			},
			RecentEvents: []telemt.RuntimeEvent{
				{
					Sequence:      1,
					TimestampUnix: 1_763_226_400,
					EventType:     "upstream_recovered",
					Context:       "dc=2 upstream=1",
				},
			},
			Diagnostics: telemt.RuntimeDiagnostics{
				State:               "fresh",
				SystemInfoJSON:      `{"version":"2026.03","config_hash":"cfg-1"}`,
				EffectiveLimitsJSON: `{"update_every_secs":5}`,
				SecurityPostureJSON: `{"api_read_only":true}`,
				MinimalAllJSON:      `{"enabled":true}`,
				MEPoolJSON:          `{"enabled":true}`,
			},
			SecurityInventory: telemt.RuntimeSecurityInventory{
				State:        "fresh",
				Enabled:      true,
				EntriesTotal: 2,
				EntriesJSON:  `["10.0.0.0/24","192.168.0.0/24"]`,
			},
		},
	}
	agent := New(Config{
		AgentID:      "agent-1",
		NodeName:     "node-a",
		FleetGroupID: "ams-1",
		Version:      "1.0.0",
	}, client)

	snapshot, err := agent.BuildRuntimeSnapshot(context.Background(), time.Date(2026, time.March, 14, 8, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("BuildSnapshot() error = %v", err)
	}

	if !snapshot.ReadOnly {
		t.Fatal("snapshot.ReadOnly = false, want true")
	}

	if len(snapshot.Instances) != 1 {
		t.Fatalf("len(snapshot.Instances) = %d, want %d", len(snapshot.Instances), 1)
	}
	if !snapshot.Runtime.AcceptingNewConnections {
		t.Fatal("snapshot.Runtime.AcceptingNewConnections = false, want true")
	}
	if snapshot.Runtime.TransportMode != "middle_proxy" {
		t.Fatalf("snapshot.Runtime.TransportMode = %q, want %q", snapshot.Runtime.TransportMode, "middle_proxy")
	}
	if snapshot.Runtime.CurrentConnectionsMe != 39 {
		t.Fatalf("snapshot.Runtime.CurrentConnectionsMe = %d, want %d", snapshot.Runtime.CurrentConnectionsMe, 39)
	}
	if snapshot.Runtime.ConnectionsTotal != 512 {
		t.Fatalf("snapshot.Runtime.ConnectionsTotal = %d, want %d", snapshot.Runtime.ConnectionsTotal, 512)
	}
	if snapshot.Runtime.UptimeSeconds != 90_061 {
		t.Fatalf("snapshot.Runtime.UptimeSeconds = %v, want %v", snapshot.Runtime.UptimeSeconds, 90_061)
	}
	if len(snapshot.Runtime.Dcs) != 1 {
		t.Fatalf("len(snapshot.Runtime.Dcs) = %d, want %d", len(snapshot.Runtime.Dcs), 1)
	}
	if snapshot.Runtime.Upstreams.HealthyTotal != 1 {
		t.Fatalf("snapshot.Runtime.Upstreams.HealthyTotal = %d, want %d", snapshot.Runtime.Upstreams.HealthyTotal, 1)
	}
	if len(snapshot.Runtime.RecentEvents) != 1 {
		t.Fatalf("len(snapshot.Runtime.RecentEvents) = %d, want %d", len(snapshot.Runtime.RecentEvents), 1)
	}
	if snapshot.RuntimeDiagnostics == nil {
		t.Fatal("snapshot.RuntimeDiagnostics = nil, want diagnostics payload")
	}
	if snapshot.RuntimeDiagnostics.SystemInfoJson == "" {
		t.Fatal("snapshot.RuntimeDiagnostics.SystemInfoJson = empty, want system info payload")
	}
	if snapshot.RuntimeSecurityInventory == nil {
		t.Fatal("snapshot.RuntimeSecurityInventory = nil, want security inventory payload")
	}
	if snapshot.RuntimeSecurityInventory.EntriesTotal != 2 {
		t.Fatalf("snapshot.RuntimeSecurityInventory.EntriesTotal = %d, want %d", snapshot.RuntimeSecurityInventory.EntriesTotal, 2)
	}
}

func TestAgentBuildSnapshotIncludesSystemLoad(t *testing.T) {
	client := &fakeTelemtClient{
		state: telemt.RuntimeState{
			Version: "2026.03",
			Gates: telemt.RuntimeGates{
				AcceptingNewConnections: true,
				MERuntimeReady:          true,
				StartupStatus:           "ready",
				StartupStage:            "serving",
				StartupProgressPct:      100,
			},
			Initialization: telemt.RuntimeInitialization{
				Status:        "ready",
				CurrentStage:  "serving",
				ProgressPct:   100,
				TransportMode: "middle_proxy",
			},
			SystemLoad: telemt.RuntimeSystemLoad{
				CPUUsagePct:      37.5,
				MemoryUsedBytes:  6_442_450_944,
				MemoryTotalBytes: 8_589_934_592,
				MemoryUsagePct:   75.0,
				DiskUsedBytes:    214_748_364_800,
				DiskTotalBytes:   536_870_912_000,
				DiskUsagePct:     40.0,
				Load1M:           1.22,
				Load5M:           0.98,
				Load15M:          0.73,
			},
		},
	}
	agent := New(Config{
		AgentID:      "agent-1",
		NodeName:     "node-a",
		FleetGroupID: "ams-1",
		Version:      "1.0.0",
	}, client)

	snapshot, err := agent.BuildRuntimeSnapshot(context.Background(), time.Date(2026, time.March, 30, 10, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("BuildRuntimeSnapshot() error = %v", err)
	}
	if snapshot.Runtime == nil {
		t.Fatal("snapshot.Runtime = nil, want runtime payload")
	}
	if snapshot.Runtime.SystemLoad == nil {
		t.Fatal("snapshot.Runtime.SystemLoad = nil, want typed runtime system load payload")
	}
	if snapshot.Runtime.SystemLoad.CpuUsagePct != 37.5 {
		t.Fatalf("snapshot.Runtime.SystemLoad.CpuUsagePct = %v, want %v", snapshot.Runtime.SystemLoad.CpuUsagePct, 37.5)
	}
	if snapshot.Runtime.SystemLoad.MemoryUsedBytes != 6_442_450_944 {
		t.Fatalf("snapshot.Runtime.SystemLoad.MemoryUsedBytes = %d, want %d", snapshot.Runtime.SystemLoad.MemoryUsedBytes, 6_442_450_944)
	}
	if snapshot.Runtime.SystemLoad.MemoryTotalBytes != 8_589_934_592 {
		t.Fatalf("snapshot.Runtime.SystemLoad.MemoryTotalBytes = %d, want %d", snapshot.Runtime.SystemLoad.MemoryTotalBytes, 8_589_934_592)
	}
	if snapshot.Runtime.SystemLoad.MemoryUsagePct != 75.0 {
		t.Fatalf("snapshot.Runtime.SystemLoad.MemoryUsagePct = %v, want %v", snapshot.Runtime.SystemLoad.MemoryUsagePct, 75.0)
	}
	if snapshot.Runtime.SystemLoad.DiskUsedBytes != 214_748_364_800 {
		t.Fatalf("snapshot.Runtime.SystemLoad.DiskUsedBytes = %d, want %d", snapshot.Runtime.SystemLoad.DiskUsedBytes, 214_748_364_800)
	}
	if snapshot.Runtime.SystemLoad.DiskTotalBytes != 536_870_912_000 {
		t.Fatalf("snapshot.Runtime.SystemLoad.DiskTotalBytes = %d, want %d", snapshot.Runtime.SystemLoad.DiskTotalBytes, 536_870_912_000)
	}
	if snapshot.Runtime.SystemLoad.DiskUsagePct != 40.0 {
		t.Fatalf("snapshot.Runtime.SystemLoad.DiskUsagePct = %v, want %v", snapshot.Runtime.SystemLoad.DiskUsagePct, 40.0)
	}
	if snapshot.Runtime.SystemLoad.GetLoad_1M() != 1.22 {
		t.Fatalf("snapshot.Runtime.SystemLoad.GetLoad_1M() = %v, want %v", snapshot.Runtime.SystemLoad.GetLoad_1M(), 1.22)
	}
	if snapshot.Runtime.SystemLoad.GetLoad_5M() != 0.98 {
		t.Fatalf("snapshot.Runtime.SystemLoad.GetLoad_5M() = %v, want %v", snapshot.Runtime.SystemLoad.GetLoad_5M(), 0.98)
	}
	if snapshot.Runtime.SystemLoad.GetLoad_15M() != 0.73 {
		t.Fatalf("snapshot.Runtime.SystemLoad.GetLoad_15M() = %v, want %v", snapshot.Runtime.SystemLoad.GetLoad_15M(), 0.73)
	}
}

func TestAgentBuildSnapshotIncludesClientUsageEntries(t *testing.T) {
	client := &fakeTelemtClient{
		state: telemt.RuntimeState{
			Version:     "2026.03",
			ReadOnly:    false,
			Connections: 7,
		},
		metricsUsage: []telemt.ClientUsage{
			{
				ClientID:         "client-1",
				TrafficUsedBytes: 1024,
				UniqueIPsUsed:    2,
				ActiveTCPConns:   3,
			},
		},
	}
	agent := New(Config{
		AgentID:      "agent-1",
		NodeName:     "node-a",
		FleetGroupID: "ams-1",
		Version:      "1.0.0",
	}, client)

	// First tick is the process baseline (delta 0); advance traffic and take
	// a second tick to observe a real delta.
	if _, err := agent.BuildUsageSnapshot(context.Background(), time.Date(2026, time.March, 14, 8, 5, 0, 0, time.UTC)); err != nil {
		t.Fatalf("BuildSnapshot(baseline) error = %v", err)
	}
	client.metricsUsage[0].TrafficUsedBytes = 2048
	snapshot, err := agent.BuildUsageSnapshot(context.Background(), time.Date(2026, time.March, 14, 8, 6, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("BuildSnapshot() error = %v", err)
	}

	if len(snapshot.Clients) != 1 {
		t.Fatalf("len(snapshot.Clients) = %d, want %d", len(snapshot.Clients), 1)
	}
	if snapshot.Clients[0].ClientId != "client-1" {
		t.Fatalf("snapshot.Clients[0].ClientId = %q, want %q", snapshot.Clients[0].ClientId, "client-1")
	}
	if snapshot.Clients[0].TrafficTotalBytes != 1024 {
		t.Fatalf("snapshot.Clients[0].TrafficTotalBytes = %d, want %d (2048-1024)", snapshot.Clients[0].TrafficTotalBytes, 1024)
	}
	if snapshot.AgentBootId == "" {
		t.Fatal("snapshot.AgentBootId is empty")
	}
}

func TestAgentHandleJobExecutesRuntimeReload(t *testing.T) {
	client := &fakeTelemtClient{}
	agent := New(Config{
		AgentID:      "agent-1",
		NodeName:     "node-a",
		FleetGroupID: "ams-1",
		Version:      "1.0.0",
	}, client)

	result := agent.HandleJob(context.Background(), &gatewayrpc.JobCommand{
		Id:             "job-1",
		Action:         "runtime.reload",
		IdempotencyKey: "key-1",
		PayloadJson:    `{"scope":"telemt"}`,
	}, time.Date(2026, time.March, 14, 8, 0, 0, 0, time.UTC))

	if !result.Success {
		t.Fatalf("HandleJob() Success = false, want true, message = %q", result.Message)
	}
	if result.ResultJson != "" {
		t.Fatalf("HandleJob() ResultJson = %q, want empty string", result.ResultJson)
	}

	if !client.reloadCalled {
		t.Fatal("HandleJob() did not invoke Telemt runtime reload")
	}
}

func TestAgentHandleJobDeduplicatesRepeatedDelivery(t *testing.T) {
	client := &fakeTelemtClient{}
	agent := New(Config{
		AgentID:      "agent-1",
		NodeName:     "node-a",
		FleetGroupID: "ams-1",
		Version:      "1.0.0",
	}, client)

	job := &gatewayrpc.JobCommand{
		Id:             "job-duplicate",
		Action:         "runtime.reload",
		IdempotencyKey: "key-dup",
	}

	first := agent.HandleJob(context.Background(), job, time.Date(2026, time.March, 18, 10, 0, 0, 0, time.UTC))
	second := agent.HandleJob(context.Background(), job, time.Date(2026, time.March, 18, 10, 0, 5, 0, time.UTC))

	if !first.Success {
		t.Fatalf("first HandleJob() Success = false, want true, message = %q", first.Message)
	}
	if !second.Success {
		t.Fatalf("second HandleJob() Success = false, want true, message = %q", second.Message)
	}
	if second.Message != first.Message {
		t.Fatalf("second HandleJob() Message = %q, want %q", second.Message, first.Message)
	}
	if client.reloadCalls != 1 {
		t.Fatalf("reload call count = %d, want %d", client.reloadCalls, 1)
	}
}

func TestAgentHandleJobReexecutesAfterDedupRetentionWindow(t *testing.T) {
	client := &fakeTelemtClient{}
	agent := New(Config{
		AgentID:      "agent-1",
		NodeName:     "node-a",
		FleetGroupID: "ams-1",
		Version:      "1.0.0",
	}, client)
	agent.completedJobRetention = time.Second

	job := &gatewayrpc.JobCommand{
		Id:             "job-expired-cache",
		Action:         "runtime.reload",
		IdempotencyKey: "key-expired-cache",
	}

	first := agent.HandleJob(context.Background(), job, time.Date(2026, time.March, 18, 10, 0, 0, 0, time.UTC))
	second := agent.HandleJob(context.Background(), job, time.Date(2026, time.March, 18, 10, 0, 2, 0, time.UTC))

	if !first.Success {
		t.Fatalf("first HandleJob() Success = false, want true, message = %q", first.Message)
	}
	if !second.Success {
		t.Fatalf("second HandleJob() Success = false, want true, message = %q", second.Message)
	}
	if client.reloadCalls != 2 {
		t.Fatalf("reload call count = %d, want %d", client.reloadCalls, 2)
	}
}

func TestAgentHandleJobCreatesManagedClientAndReturnsConnectionLink(t *testing.T) {
	client := &fakeTelemtClient{
		createResult: telemt.ClientApplyResult{
			ConnectionLinks: []string{"tg://proxy?server=node-a&secret=create"},
		},
	}
	agent := New(Config{
		AgentID:      "agent-1",
		NodeName:     "node-a",
		FleetGroupID: "ams-1",
		Version:      "1.0.0",
	}, client)

	result := agent.HandleJob(context.Background(), &gatewayrpc.JobCommand{
		Id:          "job-2",
		Action:      "client.create",
		PayloadJson: `{"client_id":"client-1","name":"alice","secret":"secret-1","user_ad_tag":"0123456789abcdef0123456789abcdef","enabled":true,"max_tcp_conns":4,"max_unique_ips":2,"data_quota_bytes":1024,"expiration_rfc3339":"2026-04-01T00:00:00Z"}`,
	}, time.Date(2026, time.March, 17, 18, 0, 0, 0, time.UTC))

	if !result.Success {
		t.Fatalf("HandleJob() Success = false, want true, message = %q", result.Message)
	}
	var payload struct {
		ConnectionLinks []string `json:"connection_links"`
	}
	if err := json.Unmarshal([]byte(result.ResultJson), &payload); err != nil {
		t.Fatalf("json.Unmarshal(ResultJSON) error = %v", err)
	}
	if got := payload.ConnectionLinks; len(got) != 1 || got[0] != "tg://proxy?server=node-a&secret=create" {
		t.Fatalf("connection_links = %v, want [tg://proxy?server=node-a&secret=create]", got)
	}
	if client.createdClient.Name != "alice" {
		t.Fatalf("created client name = %q, want %q", client.createdClient.Name, "alice")
	}
}

// TestAgentHandleJobDisabledClientCreateSkipsTelemt guards H-1: a client
// created in the disabled state must not be registered on the node (Telemt
// has no enabled flag, so registering it would proxy traffic).
func TestAgentHandleJobDisabledClientCreateSkipsTelemt(t *testing.T) {
	client := &fakeTelemtClient{}
	agent := New(Config{AgentID: "agent-1", NodeName: "node-a"}, client)

	result := agent.HandleJob(context.Background(), &gatewayrpc.JobCommand{
		Id:          "job-disabled-create",
		Action:      "client.create",
		PayloadJson: `{"client_id":"client-1","name":"alice","secret":"secret-1","enabled":false}`,
	}, time.Date(2026, time.May, 29, 12, 0, 0, 0, time.UTC))

	if !result.Success {
		t.Fatalf("HandleJob() Success = false, want true, message = %q", result.Message)
	}
	if client.createCalls != 0 {
		t.Fatalf("CreateClient called %d times for a disabled client, want 0", client.createCalls)
	}
}

// TestAgentHandleJobDisableRemovesTelemtUser guards H-1: disabling a client
// (update with enabled=false) must delete the user from Telemt so it stops
// proxying. An already-absent user is an idempotent success.
func TestAgentHandleJobDisableRemovesTelemtUser(t *testing.T) {
	client := &fakeTelemtClient{}
	agent := New(Config{AgentID: "agent-1", NodeName: "node-a"}, client)

	result := agent.HandleJob(context.Background(), &gatewayrpc.JobCommand{
		Id:          "job-disable",
		Action:      "client.update",
		PayloadJson: `{"client_id":"client-1","name":"alice","secret":"secret-1","enabled":false}`,
	}, time.Date(2026, time.May, 29, 12, 1, 0, 0, time.UTC))

	if !result.Success {
		t.Fatalf("HandleJob() Success = false, want true, message = %q", result.Message)
	}
	if client.deleteCalls != 1 || client.deletedClientName != "alice" {
		t.Fatalf("DeleteClient calls=%d name=%q, want 1 / alice", client.deleteCalls, client.deletedClientName)
	}
	if client.updateCalls != 0 {
		t.Fatalf("UpdateClient called %d times while disabling, want 0", client.updateCalls)
	}

	// Already absent → still success (idempotent disable).
	client2 := &fakeTelemtClient{deleteErr: telemt.ErrClientNotFound}
	agent2 := New(Config{AgentID: "agent-1", NodeName: "node-a"}, client2)
	r2 := agent2.HandleJob(context.Background(), &gatewayrpc.JobCommand{
		Id:          "job-disable-absent",
		Action:      "client.update",
		PayloadJson: `{"client_id":"client-1","name":"alice","secret":"secret-1","enabled":false}`,
	}, time.Date(2026, time.May, 29, 12, 2, 0, 0, time.UTC))
	if !r2.Success {
		t.Fatalf("disable of absent user Success = false, want true, message = %q", r2.Message)
	}
}

// TestAgentHandleJobReenableFallsBackToCreate guards H-1: re-enabling a
// client that Telemt no longer has (404 on PATCH) must fall back to a
// create so the user is restored on the node.
func TestAgentHandleJobReenableFallsBackToCreate(t *testing.T) {
	client := &fakeTelemtClient{
		updateErr:    telemt.ErrClientNotFound,
		createResult: telemt.ClientApplyResult{ConnectionLinks: []string{"tg://proxy?server=node-a&secret=reenable"}},
	}
	agent := New(Config{AgentID: "agent-1", NodeName: "node-a"}, client)

	result := agent.HandleJob(context.Background(), &gatewayrpc.JobCommand{
		Id:          "job-reenable",
		Action:      "client.update",
		PayloadJson: `{"client_id":"client-1","name":"alice","secret":"secret-1","enabled":true}`,
	}, time.Date(2026, time.May, 29, 12, 3, 0, 0, time.UTC))

	if !result.Success {
		t.Fatalf("HandleJob() Success = false, want true, message = %q", result.Message)
	}
	if client.updateCalls != 1 || client.createCalls != 1 {
		t.Fatalf("calls update=%d create=%d, want 1/1 (PATCH 404 → create fallback)", client.updateCalls, client.createCalls)
	}
	var payload struct {
		ConnectionLinks []string `json:"connection_links"`
	}
	if err := json.Unmarshal([]byte(result.ResultJson), &payload); err != nil {
		t.Fatalf("json.Unmarshal(ResultJSON) error = %v", err)
	}
	if len(payload.ConnectionLinks) != 1 || payload.ConnectionLinks[0] != "tg://proxy?server=node-a&secret=reenable" {
		t.Fatalf("connection_links = %v, want the create-fallback link", payload.ConnectionLinks)
	}
}

func TestAgentHandleJobUpdatesManagedClientUsingPreviousName(t *testing.T) {
	client := &fakeTelemtClient{
		updateResult: telemt.ClientApplyResult{
			ConnectionLinks: []string{"tg://proxy?server=node-a&secret=update"},
		},
	}
	agent := New(Config{
		AgentID:      "agent-1",
		NodeName:     "node-a",
		FleetGroupID: "ams-1",
		Version:      "1.0.0",
	}, client)

	result := agent.HandleJob(context.Background(), &gatewayrpc.JobCommand{
		Id:          "job-3",
		Action:      "client.update",
		PayloadJson: `{"client_id":"client-1","previous_name":"alice","name":"alice-new","secret":"secret-2","user_ad_tag":"0123456789abcdef0123456789abcdef","enabled":true}`,
	}, time.Date(2026, time.March, 17, 18, 5, 0, 0, time.UTC))

	if !result.Success {
		t.Fatalf("HandleJob() Success = false, want true, message = %q", result.Message)
	}
	if client.updatedClient.PreviousName != "alice" {
		t.Fatalf("updated previous name = %q, want %q", client.updatedClient.PreviousName, "alice")
	}
	if client.updatedClient.Name != "alice-new" {
		t.Fatalf("updated client name = %q, want %q", client.updatedClient.Name, "alice-new")
	}
}

func TestAgentHandleJobDeletesManagedClient(t *testing.T) {
	client := &fakeTelemtClient{}
	agent := New(Config{
		AgentID:      "agent-1",
		NodeName:     "node-a",
		FleetGroupID: "ams-1",
		Version:      "1.0.0",
	}, client)

	result := agent.HandleJob(context.Background(), &gatewayrpc.JobCommand{
		Id:          "job-4",
		Action:      "client.delete",
		PayloadJson: `{"client_id":"client-1","name":"alice"}`,
	}, time.Date(2026, time.March, 17, 18, 10, 0, 0, time.UTC))

	if !result.Success {
		t.Fatalf("HandleJob() Success = false, want true, message = %q", result.Message)
	}
	if client.deletedClientName != "alice" {
		t.Fatalf("deleted client name = %q, want %q", client.deletedClientName, "alice")
	}
}

// TestAgentHandleJobDeleteAbsentClientIsIdempotent guards the delete path's
// idempotency: deleting a client Telemt no longer has (404 → ErrClientNotFound)
// must report success, mirroring the disable path. A re-delivered client.delete
// (panel retry after a lost ack) where the client is already gone would
// otherwise fail forever.
func TestAgentHandleJobDeleteAbsentClientIsIdempotent(t *testing.T) {
	client := &fakeTelemtClient{deleteErr: telemt.ErrClientNotFound}
	agent := New(Config{AgentID: "agent-1", NodeName: "node-a"}, client)

	result := agent.HandleJob(context.Background(), &gatewayrpc.JobCommand{
		Id:          "job-delete-absent",
		Action:      "client.delete",
		PayloadJson: `{"client_id":"client-1","name":"alice"}`,
	}, time.Date(2026, time.May, 29, 12, 5, 0, 0, time.UTC))

	if !result.Success {
		t.Fatalf("delete of absent client Success = false, want true (idempotent), message = %q", result.Message)
	}
}

func TestAgentHandleJobRefreshDiagnosticsInvalidatesSlowData(t *testing.T) {
	client := &fakeTelemtClient{}
	agent := New(Config{
		AgentID:      "agent-1",
		NodeName:     "node-a",
		FleetGroupID: "ams-1",
		Version:      "1.0.0",
	}, client)

	result := agent.HandleJob(context.Background(), &gatewayrpc.JobCommand{
		Id:     "job-refresh",
		Action: "telemetry.refresh_diagnostics",
	}, time.Date(2026, time.March, 30, 9, 0, 0, 0, time.UTC))

	if !result.Success {
		t.Fatalf("HandleJob() Success = false, want true, message = %q", result.Message)
	}
	if client.invalidateSlowDataCalls != 1 {
		t.Fatalf("invalidateSlowDataCalls = %d, want %d", client.invalidateSlowDataCalls, 1)
	}
}

func TestAgentBuildSnapshotMapsTelemtClientNamesBackToManagedClientIDs(t *testing.T) {
	client := &fakeTelemtClient{
		createResult: telemt.ClientApplyResult{
			ConnectionLinks: []string{"tg://proxy?server=node-a&secret=create"},
		},
		state: telemt.RuntimeState{
			Version:     "2026.03",
			ReadOnly:    false,
			Connections: 1,
		},
		metricsUsage: []telemt.ClientUsage{
			{
				ClientName:       "alice",
				TrafficUsedBytes: 2048,
				UniqueIPsUsed:    2,
				ActiveTCPConns:   1,
			},
		},
	}
	agent := New(Config{
		AgentID:      "agent-1",
		NodeName:     "node-a",
		FleetGroupID: "ams-1",
		Version:      "1.0.0",
	}, client)

	result := agent.HandleJob(context.Background(), &gatewayrpc.JobCommand{
		Id:          "job-5",
		Action:      "client.create",
		PayloadJson: `{"client_id":"client-1","name":"alice","secret":"secret-1","user_ad_tag":"0123456789abcdef0123456789abcdef","enabled":true}`,
	}, time.Date(2026, time.March, 17, 18, 15, 0, 0, time.UTC))
	if !result.Success {
		t.Fatalf("HandleJob() Success = false, want true, message = %q", result.Message)
	}

	snapshot, err := agent.BuildUsageSnapshot(context.Background(), time.Date(2026, time.March, 17, 18, 16, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("BuildSnapshot() error = %v", err)
	}
	if len(snapshot.Clients) != 1 {
		t.Fatalf("len(snapshot.Clients) = %d, want %d", len(snapshot.Clients), 1)
	}
	if snapshot.Clients[0].ClientId != "client-1" {
		t.Fatalf("snapshot.Clients[0].ClientId = %q, want %q", snapshot.Clients[0].ClientId, "client-1")
	}
}

type fakeTelemtClient struct {
	state                   telemt.RuntimeState
	metricsUsage            []telemt.ClientUsage
	metricsUptime           float64
	activeIPs               []telemt.UserActiveIPs
	reloadCalled            bool
	reloadCalls             int
	createdClient           telemt.ManagedClient
	updatedClient           telemt.ManagedClient
	deletedClientName       string
	createResult            telemt.ClientApplyResult
	updateResult            telemt.ClientApplyResult
	createErr               error
	updateErr               error
	deleteErr               error
	createCalls             int
	updateCalls             int
	deleteCalls             int
	invalidateSlowDataCalls int
	resetQuotaUsername      string
	resetQuotaCalls         int
	resetQuotaResult        telemt.ResetUserQuotaResult
	resetQuotaErr           error
	discoverErr             error
	managedConfig           map[string]any
	managedRevision         string
}

func (c *fakeTelemtClient) FetchRuntimeState(context.Context) (telemt.RuntimeState, error) {
	return c.state, nil
}

func (c *fakeTelemtClient) FetchClientUsageFromMetrics(context.Context) (telemt.ClientUsageMetricsSnapshot, error) {
	return telemt.ClientUsageMetricsSnapshot{
		Users:         c.metricsUsage,
		UptimeSeconds: c.metricsUptime,
	}, nil
}

func (c *fakeTelemtClient) FetchActiveIPs(context.Context) ([]telemt.UserActiveIPs, error) {
	return c.activeIPs, nil
}

func (c *fakeTelemtClient) ExecuteRuntimeReload(context.Context) error {
	c.reloadCalls++
	c.reloadCalled = true
	return nil
}

func (c *fakeTelemtClient) CreateClient(_ context.Context, client telemt.ManagedClient) (telemt.ClientApplyResult, error) {
	c.createCalls++
	c.createdClient = client
	if c.createErr != nil {
		return telemt.ClientApplyResult{}, c.createErr
	}
	return c.createResult, nil
}

func (c *fakeTelemtClient) UpdateClient(_ context.Context, client telemt.ManagedClient) (telemt.ClientApplyResult, error) {
	c.updateCalls++
	c.updatedClient = client
	if c.updateErr != nil {
		return telemt.ClientApplyResult{}, c.updateErr
	}
	return c.updateResult, nil
}

func (c *fakeTelemtClient) DeleteClient(_ context.Context, clientName string) error {
	c.deleteCalls++
	c.deletedClientName = clientName
	return c.deleteErr
}

func (c *fakeTelemtClient) InvalidateSlowDataCache() {
	c.invalidateSlowDataCalls++
}

func (c *fakeTelemtClient) PatchConfig(context.Context, map[string]any, string) (telemt.PatchConfigResult, error) {
	return telemt.PatchConfigResult{}, nil
}

func (c *fakeTelemtClient) GetManagedConfig(context.Context) (map[string]any, string, error) {
	return c.managedConfig, c.managedRevision, nil
}

func (c *fakeTelemtClient) HealthReady(context.Context) (bool, string, error) {
	return true, "", nil
}

func (c *fakeTelemtClient) FetchSystemInfo(context.Context) (telemt.SystemInfo, error) {
	return telemt.SystemInfo{}, nil
}

func (c *fakeTelemtClient) FetchDiscoveredUsers(_ context.Context, _ string) ([]telemt.DiscoveredUser, error) {
	if c.discoverErr != nil {
		return nil, c.discoverErr
	}
	return nil, nil
}

func (c *fakeTelemtClient) ResetUserQuota(_ context.Context, username string) (telemt.ResetUserQuotaResult, error) {
	c.resetQuotaUsername = username
	c.resetQuotaCalls++
	if c.resetQuotaErr != nil {
		return telemt.ResetUserQuotaResult{}, c.resetQuotaErr
	}
	return c.resetQuotaResult, nil
}

// TestAgentUsageTotalsAreCumulative verifies the P4 wire contract: every
// emitted ClientUsageSnapshot carries the process-cumulative total
// (traffic counted by this agent process since start), alongside the
// legacy delta until the panel cutover. The first (baseline) tick adopts
// Telemt's pre-existing counter without counting it — that traffic
// belongs to the previous process epoch.
func TestAgentUsageTotalsAreCumulative(t *testing.T) {
	// ActiveTCPConns is non-zero so the first (baseline) tick still emits a
	// row (gauge change) — its delta and total are 0 because the process
	// just started; counting begins on the next tick.
	client := &fakeTelemtClient{
		metricsUsage: []telemt.ClientUsage{
			{ClientID: "client-1", TrafficUsedBytes: 500, ActiveTCPConns: 3},
		},
	}
	agent := New(Config{AgentID: "agent-1", NodeName: "node-a"}, client)

	now := time.Date(2026, time.April, 18, 12, 0, 0, 0, time.UTC)
	first, err := agent.BuildUsageSnapshot(context.Background(), now)
	if err != nil {
		t.Fatalf("BuildUsageSnapshot(1) error = %v", err)
	}
	if len(first.Clients) == 0 {
		t.Fatalf("first snapshot has no clients")
	}
	if first.Clients[0].TrafficTotalBytes != 0 {
		t.Fatalf("baseline tick total = %d, want 0 (pre-existing telemt counter is the previous epoch)", first.Clients[0].TrafficTotalBytes)
	}
	if first.AgentBootId == "" {
		t.Fatal("snapshot AgentBootId is empty, want process uuid")
	}

	// +800 bytes, then +200 more: totals must accumulate 800 -> 1000.
	client.metricsUsage[0].TrafficUsedBytes = 1300
	second, err := agent.BuildUsageSnapshot(context.Background(), now.Add(time.Minute))
	if err != nil {
		t.Fatalf("BuildUsageSnapshot(2) error = %v", err)
	}
	if len(second.Clients) == 0 || second.Clients[0].TrafficTotalBytes != 800 {
		t.Fatalf("second tick total = %d, want 800", second.Clients[0].TrafficTotalBytes)
	}

	client.metricsUsage[0].TrafficUsedBytes = 1500
	third, err := agent.BuildUsageSnapshot(context.Background(), now.Add(2*time.Minute))
	if err != nil {
		t.Fatalf("BuildUsageSnapshot(3) error = %v", err)
	}
	if len(third.Clients) == 0 || third.Clients[0].TrafficTotalBytes != 1000 {
		t.Fatalf("third tick total = %d, want 1000 (800+200)", third.Clients[0].TrafficTotalBytes)
	}
	if third.AgentBootId != first.AgentBootId {
		t.Fatalf("AgentBootId changed within one process: %q -> %q", first.AgentBootId, third.AgentBootId)
	}
}

// TestAgentUsageTotalSurvivesTelemtRestart: when Telemt restarts (uptime
// rewinds, its counter resets), the agent counts the entire fresh counter
// as new traffic and the cumulative total keeps growing monotonically —
// the process epoch (bootID) does NOT change.
func TestAgentUsageTotalSurvivesTelemtRestart(t *testing.T) {
	client := &fakeTelemtClient{
		metricsUsage: []telemt.ClientUsage{
			{ClientID: "client-1", TrafficUsedBytes: 1000, ActiveTCPConns: 1},
		},
		metricsUptime: 100,
	}
	agent := New(Config{AgentID: "agent-1", NodeName: "node-a"}, client)

	now := time.Date(2026, time.April, 18, 12, 0, 0, 0, time.UTC)
	if _, err := agent.BuildUsageSnapshot(context.Background(), now); err != nil {
		t.Fatalf("baseline tick: %v", err)
	}
	client.metricsUsage[0].TrafficUsedBytes = 1600 // +600
	if _, err := agent.BuildUsageSnapshot(context.Background(), now.Add(time.Minute)); err != nil {
		t.Fatalf("second tick: %v", err)
	}

	// Telemt restart: uptime rewound, counter reset to a small fresh value.
	client.metricsUptime = 5
	client.metricsUsage[0].TrafficUsedBytes = 50
	third, err := agent.BuildUsageSnapshot(context.Background(), now.Add(2*time.Minute))
	if err != nil {
		t.Fatalf("post-restart tick: %v", err)
	}
	if len(third.Clients) == 0 || third.Clients[0].TrafficTotalBytes != 650 {
		t.Fatalf("post-telemt-restart total = %d, want 650 (600 + full fresh counter 50)", third.Clients[0].TrafficTotalBytes)
	}
}

// TestAgentRestartDoesNotReplayCumulativeAsDelta is the regression guard for
// the traffic double-count defect (C-2). lastOctets (the per-client delta
// baseline) lives only in memory and is empty after a process restart, while
// telemt keeps the full cumulative counter. Without the baseline-tick guard
// the agent would emit the entire pre-existing cumulative total as the first
// "delta"/total of the fresh process epoch. The first tick must therefore
// adopt current telemt counters as the baseline (delta=0, total=0) and resume
// counting from there — the panel's boot-id watermark scopes the epoch.
func TestAgentRestartDoesNotReplayCumulativeAsDelta(t *testing.T) {
	client := &fakeTelemtClient{
		metricsUsage: []telemt.ClientUsage{
			{ClientID: "client-1", TrafficUsedBytes: 1_000_000, ActiveTCPConns: 2},
		},
	}
	// Simulate a restart: fresh Agent (empty lastOctets, new bootID).
	agent := New(Config{
		AgentID:  "agent-1",
		NodeName: "node-a",
	}, client)

	now := time.Date(2026, time.May, 29, 12, 0, 0, 0, time.UTC)
	first, err := agent.BuildUsageSnapshot(context.Background(), now)
	if err != nil {
		t.Fatalf("BuildUsageSnapshot(1) error = %v", err)
	}
	if len(first.Clients) > 0 && first.Clients[0].TrafficTotalBytes != 0 {
		t.Fatalf("baseline tick total = %d, want 0 (pre-existing counter is the previous epoch)", first.Clients[0].TrafficTotalBytes)
	}

	// Subsequent real traffic counts normally from the adopted baseline: the
	// cumulative total advances by the 300-byte increment (not the full
	// pre-existing counter).
	client.metricsUsage[0].TrafficUsedBytes = 1_000_300
	second, err := agent.BuildUsageSnapshot(context.Background(), now.Add(time.Minute))
	if err != nil {
		t.Fatalf("BuildUsageSnapshot(2) error = %v", err)
	}
	if len(second.Clients) == 0 || second.Clients[0].TrafficTotalBytes != 300 {
		t.Fatalf("post-baseline total = %+v, want a single 300-byte total", second.Clients)
	}
}

func TestBuildRuntimeUnreachableSnapshot(t *testing.T) {
	stub := &errTelemt{}
	agent := New(Config{
		AgentID:      "agent-1",
		NodeName:     "node-1",
		FleetGroupID: "fleet-a",
		Version:      "1.2.3",
	}, stub)

	since := time.Date(2026, 5, 5, 12, 0, 0, 0, time.UTC)
	observedAt := since.Add(45 * time.Second)

	snap := agent.BuildRuntimeUnreachableSnapshot(observedAt, since)

	if snap == nil {
		t.Fatal("BuildRuntimeUnreachableSnapshot = nil, want snapshot")
	}
	if snap.AgentId != "agent-1" {
		t.Fatalf("AgentId = %q, want agent-1", snap.AgentId)
	}
	if snap.ObservedAtUnix != observedAt.Unix() {
		t.Fatalf("ObservedAtUnix = %d, want %d", snap.ObservedAtUnix, observedAt.Unix())
	}
	if snap.Runtime == nil {
		t.Fatal("Runtime = nil, want non-nil RuntimeSnapshot")
	}
	if !snap.Runtime.TelemtUnreachable {
		t.Fatal("Runtime.TelemtUnreachable = false, want true")
	}
	if snap.Runtime.TelemtUnreachableSinceUnix != since.Unix() {
		t.Fatalf("Runtime.TelemtUnreachableSinceUnix = %d, want %d",
			snap.Runtime.TelemtUnreachableSinceUnix, since.Unix())
	}
	// Runtime data fields must be zero — we have no telemt data.
	if snap.Runtime.UseMiddleProxy || snap.Runtime.MeRuntimeReady ||
		snap.Runtime.AcceptingNewConnections {
		t.Fatal("expected zero runtime gates while telemt unreachable")
	}
	if snap.Runtime.CurrentConnections != 0 || snap.Runtime.ActiveUsers != 0 {
		t.Fatal("expected zero runtime counters while telemt unreachable")
	}
	if snap.RuntimeDiagnostics == nil || snap.RuntimeSecurityInventory == nil {
		t.Fatal("expected empty diagnostics / security inventory shells, not nil")
	}
}

// TestBuildRuntimeSnapshotHealthyTelemtUnreachableFalse guards the wire
// contract: every successful runtime snapshot must leave TelemtUnreachable
// at proto3's bool default (false) and TelemtUnreachableSinceUnix=0. The
// inverted semantic means proto3's default already represents the healthy
// case, but a regression that flips the field would still mis-classify
// healthy agents as Telemt-unreachable. The check is performed on both the
// in-memory proto struct and the marshalled wire bytes to guard against
// presence-wrapper or oneOf changes that might alter wire semantics.
func TestBuildRuntimeSnapshotHealthyTelemtUnreachableFalse(t *testing.T) {
	client := &fakeTelemtClient{
		state: telemt.RuntimeState{
			Version:       "2026.03",
			UptimeSeconds: 60,
			Connections:   1,
			Gates: telemt.RuntimeGates{
				AcceptingNewConnections: true,
				MERuntimeReady:          true,
				UseMiddleProxy:          true,
				StartupStatus:           "ready",
			},
			Initialization: telemt.RuntimeInitialization{
				Status: "ready",
			},
		},
	}
	agent := New(Config{AgentID: "agent-1", NodeName: "node-a"}, client)
	snap, err := agent.BuildRuntimeSnapshot(context.Background(), time.Date(2026, time.May, 7, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("BuildRuntimeSnapshot() error = %v", err)
	}
	if snap.Runtime == nil {
		t.Fatal("Runtime = nil, want non-nil")
	}
	if snap.Runtime.TelemtUnreachable {
		t.Fatal("Runtime.TelemtUnreachable = true, want false for a successful snapshot")
	}
	if snap.Runtime.TelemtUnreachableSinceUnix != 0 {
		t.Fatalf("Runtime.TelemtUnreachableSinceUnix = %d, want 0 (healthy path)",
			snap.Runtime.TelemtUnreachableSinceUnix)
	}

	wire, err := proto.Marshal(snap.Runtime)
	if err != nil {
		t.Fatalf("proto.Marshal(Runtime) error = %v", err)
	}
	var roundtrip gatewayrpc.RuntimeSnapshot
	if err := proto.Unmarshal(wire, &roundtrip); err != nil {
		t.Fatalf("proto.Unmarshal(Runtime) error = %v", err)
	}
	if roundtrip.TelemtUnreachable {
		t.Fatal("roundtrip Runtime.TelemtUnreachable = true, want false")
	}
	if roundtrip.TelemtUnreachableSinceUnix != 0 {
		t.Fatalf("roundtrip Runtime.TelemtUnreachableSinceUnix = %d, want 0",
			roundtrip.TelemtUnreachableSinceUnix)
	}
}

func TestHandleClientDataRequestFlagsTelemtUnreachable(t *testing.T) {
	client := &fakeTelemtClient{discoverErr: errors.New("connection refused")}
	agent := New(Config{
		AgentID:      "agent-1",
		NodeName:     "node-a",
		FleetGroupID: "ams-1",
		Version:      "1.0.0",
	}, client)

	resp := agent.HandleClientDataRequest(context.Background(), "req-1")

	if resp == nil {
		t.Fatal("response must not be nil")
	}
	if !resp.GetTelemtUnreachable() {
		t.Fatal("telemt_unreachable should be true when FetchDiscoveredUsers fails")
	}
	if len(resp.GetClients()) != 0 {
		t.Fatalf("expected no clients on failure, got %d", len(resp.GetClients()))
	}
	if resp.GetRequestId() != "req-1" {
		t.Fatalf("request id = %q, want %q", resp.GetRequestId(), "req-1")
	}
}

// TestBuildRuntimeSnapshotHashGatesDiagnostics guards D5: the bulky
// diagnostics / security-inventory JSON bodies are sent only when their
// content hash changes; every snapshot still carries the hash; an
// unreachable snapshot resets the gates.
func TestBuildRuntimeSnapshotHashGatesDiagnostics(t *testing.T) {
	client := &fakeTelemtClient{
		state: telemt.RuntimeState{
			Diagnostics: telemt.RuntimeDiagnostics{
				State:          "ok",
				SystemInfoJSON: `{"cpu":4}`,
				MEPoolJSON:     `{"pool":1}`,
			},
			SecurityInventory: telemt.RuntimeSecurityInventory{
				State:       "ok",
				Enabled:     true,
				EntriesJSON: `[{"rule":1}]`,
			},
		},
	}
	agent := New(Config{AgentID: "agent-1", NodeName: "node"}, client)
	now := time.Date(2026, time.June, 9, 12, 0, 0, 0, time.UTC)

	first, err := agent.BuildRuntimeSnapshot(context.Background(), now)
	if err != nil {
		t.Fatalf("first snapshot: %v", err)
	}
	if first.RuntimeDiagnostics.ContentHash == "" || first.RuntimeDiagnostics.SystemInfoJson == "" {
		t.Fatalf("first snapshot must carry hash AND body, got %+v", first.RuntimeDiagnostics)
	}
	if first.RuntimeSecurityInventory.ContentHash == "" || first.RuntimeSecurityInventory.EntriesJson == "" {
		t.Fatalf("first snapshot must carry security hash AND body, got %+v", first.RuntimeSecurityInventory)
	}

	second, err := agent.BuildRuntimeSnapshot(context.Background(), now.Add(time.Minute))
	if err != nil {
		t.Fatalf("second snapshot: %v", err)
	}
	if second.RuntimeDiagnostics.ContentHash != first.RuntimeDiagnostics.ContentHash {
		t.Fatal("hash must be stable for unchanged diagnostics")
	}
	if second.RuntimeDiagnostics.SystemInfoJson != "" || second.RuntimeDiagnostics.MePoolJson != "" || second.RuntimeDiagnostics.State != "" {
		t.Fatalf("unchanged diagnostics must omit the body, got %+v", second.RuntimeDiagnostics)
	}
	if second.RuntimeSecurityInventory.EntriesJson != "" {
		t.Fatalf("unchanged security inventory must omit the body, got %+v", second.RuntimeSecurityInventory)
	}

	client.state.Diagnostics.SystemInfoJSON = `{"cpu":8}`
	third, err := agent.BuildRuntimeSnapshot(context.Background(), now.Add(2*time.Minute))
	if err != nil {
		t.Fatalf("third snapshot: %v", err)
	}
	if third.RuntimeDiagnostics.SystemInfoJson != `{"cpu":8}` {
		t.Fatalf("changed diagnostics must re-send the body, got %+v", third.RuntimeDiagnostics)
	}

	_ = agent.BuildRuntimeUnreachableSnapshot(now.Add(3*time.Minute), now.Add(2*time.Minute))
	fourth, err := agent.BuildRuntimeSnapshot(context.Background(), now.Add(4*time.Minute))
	if err != nil {
		t.Fatalf("fourth snapshot: %v", err)
	}
	if fourth.RuntimeDiagnostics.SystemInfoJson == "" || fourth.RuntimeSecurityInventory.EntriesJson == "" {
		t.Fatal("post-unreachable snapshot must re-send full bodies")
	}
}
