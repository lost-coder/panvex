package gateway

import (
	"testing"

	"github.com/lost-coder/panvex/internal/controlplane/eventbus"
	"github.com/lost-coder/panvex/internal/controlplane/runtimeevents"
	"github.com/lost-coder/panvex/internal/gatewayrpc"
)

// TestHandleRuntimeEventsBatchPublishesOneBusEvent guards D6a: one inbound
// agent batch must produce exactly ONE bus event (type runtime.events)
// carrying all records — not one publish per record.
func TestHandleRuntimeEventsBatchPublishesOneBusEvent(t *testing.T) {
	g := &Gateway{events: eventbus.NewHub(), runtimeEvents: runtimeevents.New(10)}
	ch, cancel := g.events.Subscribe()
	defer cancel()

	g.handleRuntimeEventsBatch("agent-1", &gatewayrpc.RuntimeEventsBatch{
		Events: []*gatewayrpc.AgentRuntimeEvent{
			{Level: "warn", Message: "m1"},
			{Level: "info", Message: "m2"},
			{Level: "error", Message: "m3"},
		},
	})

	select {
	case evt := <-ch:
		if evt.Type != "runtime.events" {
			t.Fatalf("event type = %q, want runtime.events", evt.Type)
		}
		data, ok := evt.Data.(map[string]any)
		if !ok {
			t.Fatalf("event data type = %T, want map", evt.Data)
		}
		if data["agent_id"] != "agent-1" {
			t.Fatalf("agent_id = %v, want agent-1", data["agent_id"])
		}
		records, ok := data["events"].([]map[string]any)
		if !ok || len(records) != 3 {
			t.Fatalf("events payload = %#v, want 3 records", data["events"])
		}
	default:
		t.Fatal("expected one published bus event")
	}
	select {
	case extra := <-ch:
		t.Fatalf("unexpected extra bus event %q — must be exactly one per batch", extra.Type)
	default:
	}
}
