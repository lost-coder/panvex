package runtime

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/agent/telemt"
	"github.com/lost-coder/panvex/internal/gatewayrpc"
)

func TestAgentBuildSnapshotMarksLifecycleRegressionAsDegraded(t *testing.T) {
	client := &fakeTelemtClient{
		state: telemt.RuntimeState{
			Version:        "2026.03",
			ReadOnly:       false,
			UptimeSeconds:  120,
			ConnectedUsers: 8,
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
			Version:        "2026.03",
			ReadOnly:       true,
			UptimeSeconds:  90_061,
			ConnectedUsers: 42,
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
				State:       "fresh",
				Enabled:     true,
				EntriesTotal: 2,
				EntriesJSON: `["10.0.0.0/24","192.168.0.0/24"]`,
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
				CPUUsagePct:     37.5,
				MemoryUsedBytes: 6_442_450_944,
				MemoryTotalBytes: 8_589_934_592,
				MemoryUsagePct:  75.0,
				DiskUsedBytes:   214_748_364_800,
				DiskTotalBytes:  536_870_912_000,
				DiskUsagePct:    40.0,
				Load1M:          1.22,
				Load5M:          0.98,
				Load15M:         0.73,
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
			Version:        "2026.03",
			ReadOnly:       false,
			ConnectedUsers: 7,
			Clients: []telemt.ClientUsage{
				{
					ClientID:         "client-1",
					TrafficUsedBytes: 1024,
					UniqueIPsUsed:    2,
					ActiveTCPConns:   3,
				},
			},
		},
	}
	agent := New(Config{
		AgentID:      "agent-1",
		NodeName:     "node-a",
		FleetGroupID: "ams-1",
		Version:      "1.0.0",
	}, client)

	snapshot, err := agent.BuildUsageSnapshot(context.Background(), time.Date(2026, time.March, 14, 8, 5, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("BuildSnapshot() error = %v", err)
	}

	if len(snapshot.Clients) != 1 {
		t.Fatalf("len(snapshot.Clients) = %d, want %d", len(snapshot.Clients), 1)
	}
	if snapshot.Clients[0].ClientId != "client-1" {
		t.Fatalf("snapshot.Clients[0].ClientId = %q, want %q", snapshot.Clients[0].ClientId, "client-1")
	}
	if snapshot.Clients[0].TrafficDeltaBytes != 1024 {
		t.Fatalf("snapshot.Clients[0].TrafficDeltaBytes = %d, want %d", snapshot.Clients[0].TrafficDeltaBytes, 1024)
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
			ConnectionLink: "tg://proxy?server=node-a&secret=create",
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
		ConnectionLink string `json:"connection_link"`
	}
	if err := json.Unmarshal([]byte(result.ResultJson), &payload); err != nil {
		t.Fatalf("json.Unmarshal(ResultJSON) error = %v", err)
	}
	if payload.ConnectionLink != "tg://proxy?server=node-a&secret=create" {
		t.Fatalf("connection_link = %q, want %q", payload.ConnectionLink, "tg://proxy?server=node-a&secret=create")
	}
	if client.createdClient.Name != "alice" {
		t.Fatalf("created client name = %q, want %q", client.createdClient.Name, "alice")
	}
}

func TestAgentHandleJobUpdatesManagedClientUsingPreviousName(t *testing.T) {
	client := &fakeTelemtClient{
		updateResult: telemt.ClientApplyResult{
			ConnectionLink: "tg://proxy?server=node-a&secret=update",
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
			ConnectionLink: "tg://proxy?server=node-a&secret=create",
		},
		state: telemt.RuntimeState{
			Version:        "2026.03",
			ReadOnly:       false,
			ConnectedUsers: 1,
			Clients: []telemt.ClientUsage{
				{
					ClientName:       "alice",
					TrafficUsedBytes: 2048,
					UniqueIPsUsed:    2,
					ActiveTCPConns:   1,
				},
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
	state             telemt.RuntimeState
	metricsUsage      []telemt.ClientUsage
	metricsUptime     float64
	activeIPs         []telemt.UserActiveIPs
	reloadCalled      bool
	reloadCalls       int
	createdClient     telemt.ManagedClient
	updatedClient     telemt.ManagedClient
	deletedClientName string
	createResult      telemt.ClientApplyResult
	updateResult      telemt.ClientApplyResult
	invalidateSlowDataCalls int
}

func (c *fakeTelemtClient) FetchRuntimeState(context.Context) (telemt.RuntimeState, error) {
	return c.state, nil
}

func (c *fakeTelemtClient) FetchClientUsageFromMetrics(context.Context) (telemt.ClientUsageMetricsSnapshot, error) {
	usage := c.metricsUsage
	if usage == nil {
		usage = c.state.Clients
	}
	return telemt.ClientUsageMetricsSnapshot{
		Users:         usage,
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
	c.createdClient = client
	return c.createResult, nil
}

func (c *fakeTelemtClient) UpdateClient(_ context.Context, client telemt.ManagedClient) (telemt.ClientApplyResult, error) {
	c.updatedClient = client
	return c.updateResult, nil
}

func (c *fakeTelemtClient) DeleteClient(_ context.Context, clientName string) error {
	c.deletedClientName = clientName
	return nil
}

func (c *fakeTelemtClient) InvalidateSlowDataCache() {
	c.invalidateSlowDataCalls++
}

func (c *fakeTelemtClient) FetchSystemInfo(context.Context) (telemt.SystemInfo, error) {
	return telemt.SystemInfo{}, nil
}

func (c *fakeTelemtClient) FetchDiscoveredUsers(_ context.Context, _ string) ([]telemt.DiscoveredUser, error) {
	return nil, nil
}
