package conn

import (
	"context"
	"testing"

	"github.com/lost-coder/panvex/internal/agent/runtime"
	"github.com/lost-coder/panvex/internal/agent/telemt"
	"github.com/lost-coder/panvex/internal/gatewayrpc"
)

// configuredTelemtClient reports a stable managed config, so the delta gate
// sees an unchanged hash on every fetch.
type configuredTelemtClient struct {
	fakeInitialSyncTelemtClient
}

func (c *configuredTelemtClient) GetManagedConfig(context.Context) (map[string]any, string, error) {
	return map[string]any{"general": map[string]any{"log_level": "silent"}}, "rev-1", nil
}

// TestSendInitialMessagesResendsObservedConfigOnEverySession pins the
// panel-restart contract: the panel keeps the observed managed config only in
// its in-memory live store, so every newly established session must start with
// the full body. Without it the agent's process-lifetime delta gate stays
// closed and the panel is left with an empty `observed` / "unknown" drift for
// as long as the Telemt config does not change.
func TestSendInitialMessagesResendsObservedConfigOnEverySession(t *testing.T) {
	telemtClient := &configuredTelemtClient{
		fakeInitialSyncTelemtClient: fakeInitialSyncTelemtClient{
			state: telemt.RuntimeState{Version: "3.4.25"},
		},
	}
	agent := runtime.New(runtime.Config{AgentID: "agent-1", NodeName: "node-a"}, telemtClient)

	managedConfigJSON := func(t *testing.T) string {
		t.Helper()
		outbound := make(chan *gatewayrpc.ConnectClientMessage, 4)
		if err := sendInitialMessages(t.Context(), outbound, agent); err != nil {
			t.Fatalf("sendInitialMessages() error = %v", err)
		}
		close(outbound)
		for message := range outbound {
			snapshot := message.GetSnapshot()
			if snapshot == nil || len(snapshot.Instances) == 0 {
				continue
			}
			return snapshot.Instances[0].GetManagedConfigJson()
		}
		t.Fatal("no runtime snapshot with instances in the initial messages")
		return ""
	}

	if got := managedConfigJSON(t); got == "" {
		t.Fatal("first session carries no managed config JSON, want the full body")
	}
	if got := managedConfigJSON(t); got == "" {
		t.Fatal("second session carries no managed config JSON — the panel would stay without observed config")
	}
}
