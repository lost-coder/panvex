package runtime

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/panvex/panvex/internal/agent/telemt"
	"github.com/panvex/panvex/internal/gatewayrpc"
)

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
		},
	}
	agent := New(Config{
		AgentID:      "agent-1",
		NodeName:     "node-a",
		FleetGroupID: "ams-1",
		Version:      "1.0.0",
	}, client)

	snapshot, err := agent.BuildSnapshot(context.Background(), time.Date(2026, time.March, 14, 8, 0, 0, 0, time.UTC))
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

	snapshot, err := agent.BuildSnapshot(context.Background(), time.Date(2026, time.March, 14, 8, 5, 0, 0, time.UTC))
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
		Id:     "job-2",
		Action: "client.create",
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
		Id:     "job-3",
		Action: "client.update",
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
		Id:     "job-5",
		Action: "client.create",
		PayloadJson: `{"client_id":"client-1","name":"alice","secret":"secret-1","user_ad_tag":"0123456789abcdef0123456789abcdef","enabled":true}`,
	}, time.Date(2026, time.March, 17, 18, 15, 0, 0, time.UTC))
	if !result.Success {
		t.Fatalf("HandleJob() Success = false, want true, message = %q", result.Message)
	}

	snapshot, err := agent.BuildSnapshot(context.Background(), time.Date(2026, time.March, 17, 18, 16, 0, 0, time.UTC))
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
	reloadCalled      bool
	createdClient     telemt.ManagedClient
	updatedClient     telemt.ManagedClient
	deletedClientName string
	createResult      telemt.ClientApplyResult
	updateResult      telemt.ClientApplyResult
}

func (c *fakeTelemtClient) FetchRuntimeState(context.Context) (telemt.RuntimeState, error) {
	return c.state, nil
}

func (c *fakeTelemtClient) ExecuteRuntimeReload(context.Context) error {
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
