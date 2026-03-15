package runtime

import (
	"context"
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
			ConnectedUsers: 42,
		},
	}
	agent := New(Config{
		AgentID:       "agent-1",
		NodeName:      "node-a",
		EnvironmentID: "prod",
		FleetGroupID:  "ams-1",
		Version:       "1.0.0",
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
}

func TestAgentHandleJobExecutesRuntimeReload(t *testing.T) {
	client := &fakeTelemtClient{}
	agent := New(Config{
		AgentID:       "agent-1",
		NodeName:      "node-a",
		EnvironmentID: "prod",
		FleetGroupID:  "ams-1",
		Version:       "1.0.0",
	}, client)

	result := agent.HandleJob(context.Background(), &gatewayrpc.JobCommand{
		ID:             "job-1",
		Action:         "runtime.reload",
		IdempotencyKey: "key-1",
	}, time.Date(2026, time.March, 14, 8, 0, 0, 0, time.UTC))

	if !result.Success {
		t.Fatalf("HandleJob() Success = false, want true, message = %q", result.Message)
	}

	if !client.reloadCalled {
		t.Fatal("HandleJob() did not invoke Telemt runtime reload")
	}
}

type fakeTelemtClient struct {
	state        telemt.RuntimeState
	reloadCalled bool
}

func (c *fakeTelemtClient) FetchRuntimeState(context.Context) (telemt.RuntimeState, error) {
	return c.state, nil
}

func (c *fakeTelemtClient) ExecuteRuntimeReload(context.Context) error {
	c.reloadCalled = true
	return nil
}
