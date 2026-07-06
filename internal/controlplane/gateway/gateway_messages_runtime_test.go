package gateway

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/eventbus"
	"github.com/lost-coder/panvex/internal/controlplane/runtimeevents"
	"github.com/lost-coder/panvex/internal/gatewayrpc"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// TestHandleRuntimeEventsBatchPopulatesBufferAndPublishes drives a
// ConnectClientMessage carrying a RuntimeEventsBatch through the same
// processRegularAgentMessage helper that the streaming dispatcher uses
// in production, then asserts both observable side effects of the new
// handler:
//
//  1. the panel-side per-agent ring buffer holds the converted events
//  2. the events bus emits exactly ONE "runtime.events" batch payload
//     carrying all records (D6a)
//
// Using processRegularAgentMessage matches the style of the grpc_gateway_test.go
// tests which exercise the heartbeat / job-result paths the same way.
func TestHandleRuntimeEventsBatchPopulatesBufferAndPublishes(t *testing.T) {
	currentTime := time.Date(2026, time.May, 14, 10, 0, 0, 0, time.UTC)
	g := &Gateway{
		deps:          stubDeps{},
		logger:        slog.New(slog.NewTextHandler(io.Discard, nil)),
		runtimeEvents: runtimeevents.New(500),
		events:        eventbus.NewHub(),
		now:           func() time.Time { return currentTime },
	}

	// Subscribe BEFORE dispatching so the Publish calls land in our
	// channel. The hub fans out to active subscribers only — late
	// subscribers will miss the emit and the assertion below would
	// flake.
	subCh, unsub := g.events.Subscribe()
	defer unsub()

	eventTs := currentTime.Add(time.Second)
	msg := &gatewayrpc.ConnectClientMessage{
		Body: &gatewayrpc.ConnectClientMessage_RuntimeEvents{
			RuntimeEvents: &gatewayrpc.RuntimeEventsBatch{
				AgentId: "agent-x",
				Events: []*gatewayrpc.AgentRuntimeEvent{
					{
						Ts:      timestamppb.New(eventTs),
						Level:   "warn",
						Message: "boom",
						Fields:  map[string]string{"component": "telemt"},
					},
					{
						Ts:      timestamppb.New(eventTs.Add(time.Millisecond)),
						Level:   "info",
						Message: "still alive",
					},
				},
			},
		},
	}

	regularSnapshots := make(chan AgentSnapshot, 1)
	if err := g.processRegularAgentMessage(context.Background(), "agent-x", nil, regularSnapshots, msg); err != nil {
		t.Fatalf("processRegularAgentMessage() error = %v", err)
	}

	// Buffer side effect — Snapshot returns newest first; no filter, no
	// limit so we get every record we just inserted.
	snap := g.runtimeEvents.Snapshot("agent-x", nil, 0)
	if len(snap) != 2 {
		t.Fatalf("snapshot len = %d, want 2", len(snap))
	}
	if snap[0].Message != "still alive" {
		t.Fatalf("snapshot[0].Message = %q, want %q (newest first)", snap[0].Message, "still alive")
	}
	if snap[1].Message != "boom" {
		t.Fatalf("snapshot[1].Message = %q, want %q", snap[1].Message, "boom")
	}
	if snap[1].Fields["component"] != "telemt" {
		t.Fatalf("snapshot[1].Fields[component] = %q, want %q", snap[1].Fields["component"], "telemt")
	}

	// Events bus side effect — D6a: exactly ONE runtime.events event for the
	// whole batch. drainBatchEvent waits a short window for the delivery.
	batchEvt := drainBatchEvent(t, subCh, time.Second)
	if batchEvt == nil {
		t.Fatal("expected one runtime.events bus event, got none")
	}
	data, ok := batchEvt.Data.(map[string]any)
	if !ok {
		t.Fatalf("event data type = %T, want map", batchEvt.Data)
	}
	if data["agent_id"] != "agent-x" {
		t.Fatalf("event.agent_id = %v, want %q", data["agent_id"], "agent-x")
	}
	records, ok := data["events"].([]map[string]any)
	if !ok || len(records) != 2 {
		t.Fatalf("events payload = %#v, want 2 records", data["events"])
	}
	if records[0]["message"] != "boom" {
		t.Fatalf("records[0].message = %v, want %q", records[0]["message"], "boom")
	}
	if records[1]["message"] != "still alive" {
		t.Fatalf("records[1].message = %v, want %q", records[1]["message"], "still alive")
	}
}

// drainBatchEvent reads at most one runtime.events batch event from ch
// within timeout, ignoring any other event types the bus may carry.
// Returns nil if no matching event arrives before the deadline.
func drainBatchEvent(t *testing.T, ch <-chan eventbus.Event, timeout time.Duration) *eventbus.Event {
	t.Helper()
	deadline := time.After(timeout)
	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				return nil
			}
			if ev.Type == "runtime.events" {
				return &ev
			}
		case <-deadline:
			return nil
		}
	}
}
