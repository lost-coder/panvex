package main

import (
	"context"

	"github.com/lost-coder/panvex/internal/agent/runtimeevents"
	"github.com/lost-coder/panvex/internal/gatewayrpc"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// runtimeEventsBatchMessage converts an agent-side batch of Events into the
// gatewayrpc ConnectClientMessage wire shape. Extracted as a pure helper so
// the conversion can be unit-tested independently of the outbound channel /
// stream machinery.
func runtimeEventsBatchMessage(agentID string, batch []runtimeevents.Event) *gatewayrpc.ConnectClientMessage {
	proto := make([]*gatewayrpc.AgentRuntimeEvent, 0, len(batch))
	for _, ev := range batch {
		proto = append(proto, &gatewayrpc.AgentRuntimeEvent{
			Ts:      timestamppb.New(ev.Ts),
			Level:   ev.Level,
			Message: ev.Message,
			Fields:  ev.Fields,
		})
	}
	return &gatewayrpc.ConnectClientMessage{
		Body: &gatewayrpc.ConnectClientMessage_RuntimeEvents{
			RuntimeEvents: &gatewayrpc.RuntimeEventsBatch{
				AgentId: agentID,
				Events:  proto,
			},
		},
	}
}

// sendRuntimeEventsFunc returns a runtimeEventsPusher-compatible send
// function that enqueues the batch onto the supplied telemetry outbound
// channel. Routing through the telemetry channel (rather than calling
// stream.Send directly) preserves gRPC's single-sender-per-stream
// invariant — the outbound pump goroutine is the only caller of
// stream.Send for a given connection.
//
// The send respects connectionCtx: if the connection has gone away the
// batch is dropped (returns nil so the pusher advances its cursor) since
// the next connection will start with an empty cursor and any unsent
// events still live in the ring.
func sendRuntimeEventsFunc(connectionCtx context.Context, telemetryOutbound chan<- *gatewayrpc.ConnectClientMessage, agentID string) func([]runtimeevents.Event) error {
	return func(batch []runtimeevents.Event) error {
		if len(batch) == 0 {
			return nil
		}
		msg := runtimeEventsBatchMessage(agentID, batch)
		select {
		case telemetryOutbound <- msg:
			return nil
		case <-connectionCtx.Done():
			return connectionCtx.Err()
		}
	}
}
