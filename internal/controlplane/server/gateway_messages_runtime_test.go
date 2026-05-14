package server

import (
	"context"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/eventbus"
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
//  2. the events bus emits one "runtime.event" payload per record
//
// Using processRegularAgentMessage matches the style of the existing
// grpc_gateway_test.go tests which exercise the heartbeat / job-result
// paths the same way.
func TestHandleRuntimeEventsBatchPopulatesBufferAndPublishes(t *testing.T) {
	currentTime := time.Date(2026, time.May, 14, 10, 0, 0, 0, time.UTC)
	srv := mustNew(t, Options{
		LoginTimingFloor: -1,
		Now:              func() time.Time { return currentTime },
	})
	if srv.runtimeEvents == nil {
		t.Fatal("runtimeEvents buffer not wired in test fixture")
	}

	// Subscribe BEFORE dispatching so the Publish calls land in our
	// channel. The hub fans out to active subscribers only — late
	// subscribers will miss the emit and the assertion below would
	// flake.
	subCh, unsub := srv.events.Subscribe()
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

	regularSnapshots := make(chan agentSnapshot, 1)
	if err := srv.processRegularAgentMessage(context.Background(), "agent-x", nil, regularSnapshots, msg); err != nil {
		t.Fatalf("processRegularAgentMessage() error = %v", err)
	}

	// Buffer side effect — Snapshot returns newest first; no filter, no
	// limit so we get every record we just inserted.
	snap := srv.runtimeEvents.Snapshot("agent-x", nil, 0)
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

	// Events bus side effect — one runtime.event per record, in the
	// same order as the batch. drainRuntimeEvents waits a short window
	// for both deliveries so the test does not race the publisher.
	got := drainRuntimeEvents(t, subCh, 2, time.Second)
	if len(got) != 2 {
		t.Fatalf("got %d runtime.event publishes, want 2", len(got))
	}
	if data0, _ := got[0].Data.(map[string]any); data0["message"] != "boom" {
		t.Fatalf("event[0].message = %v, want %q", data0["message"], "boom")
	}
	if data1, _ := got[1].Data.(map[string]any); data1["message"] != "still alive" {
		t.Fatalf("event[1].message = %v, want %q", data1["message"], "still alive")
	}
	if data0, _ := got[0].Data.(map[string]any); data0["agent_id"] != "agent-x" {
		t.Fatalf("event[0].agent_id = %v, want %q", data0["agent_id"], "agent-x")
	}
}

// drainRuntimeEvents reads up to want runtime.event entries from sub
// within timeout, ignoring any other event types the bus may carry
// (none expected for this test path today, but the assertion stays
// robust if a future code path publishes an unrelated event during
// dispatch).
func drainRuntimeEvents(t *testing.T, ch <-chan eventbus.Event, want int, timeout time.Duration) []eventbus.Event {
	t.Helper()
	out := make([]eventbus.Event, 0, want)
	deadline := time.After(timeout)
	for len(out) < want {
		select {
		case ev, ok := <-ch:
			if !ok {
				return out
			}
			if ev.Type == "runtime.event" {
				out = append(out, ev)
			}
		case <-deadline:
			return out
		}
	}
	return out
}
