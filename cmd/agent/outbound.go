package main

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/lost-coder/panvex/internal/agent/runtime"
	agentTransport "github.com/lost-coder/panvex/internal/agent/transport"
	"github.com/lost-coder/panvex/internal/gatewayrpc"
)

func enqueueOutboundMessage(
	connectionCtx context.Context,
	outbound chan<- *gatewayrpc.ConnectClientMessage,
	message *gatewayrpc.ConnectClientMessage,
) bool {
	if message == nil {
		return false
	}
	if connectionCtx.Err() != nil {
		return false
	}

	select {
	case <-connectionCtx.Done():
		return false
	case outbound <- message:
		return true
	default:
		return false
	}
}

// startOutboundPump spawns the goroutine that pulls messages off the
// critical and telemetry channels and forwards them on the gateway
// stream. Critical messages are drained before the telemetry channel
// is even consulted so a backed-up snapshot pipeline cannot starve a
// heartbeat.
func startOutboundPump(
	connectionCtx context.Context,
	streamWG *sync.WaitGroup,
	stream agentTransport.BidiStream,
	criticalOutbound, telemetryOutbound <-chan *gatewayrpc.ConnectClientMessage,
	sendErrorAndCancel func(error),
) {
	streamWG.Add(1)
	go func() {
		defer streamWG.Done()
		for {
			var message *gatewayrpc.ConnectClientMessage
			select {
			case <-connectionCtx.Done():
				return
			case message = <-criticalOutbound:
			default:
				select {
				case <-connectionCtx.Done():
					return
				case message = <-criticalOutbound:
				case message = <-telemetryOutbound:
				}
			}

			if message == nil {
				continue
			}
			if err := stream.Send(message); err != nil {
				sendErrorAndCancel(err)
				return
			}
		}
	}()
}

func heartbeatMessage(agent *runtime.Agent, observedAt time.Time) *gatewayrpc.ConnectClientMessage {
	return &gatewayrpc.ConnectClientMessage{
		Body: &gatewayrpc.ConnectClientMessage_Heartbeat{
			Heartbeat: &gatewayrpc.Heartbeat{
				AgentId:        agent.AgentID(),
				NodeName:       agent.NodeName(),
				FleetGroupId:   agent.FleetGroupID(),
				Version:        agent.Version(),
				ObservedAtUnix: observedAt.UTC().Unix(),
			},
		},
	}
}

// sendOrAbort enqueues msg unless ctx is already done. The initial-sync
// path runs before the per-connection workers are guaranteed alive, so a
// bare `outbound <- msg` could block forever on a dead connection.
func sendOrAbort(ctx context.Context, outbound chan<- *gatewayrpc.ConnectClientMessage, msg *gatewayrpc.ConnectClientMessage) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case outbound <- msg:
		return nil
	}
}

func sendInitialMessages(ctx context.Context, outbound chan<- *gatewayrpc.ConnectClientMessage, agent *runtime.Agent) error {
	if err := sendOrAbort(ctx, outbound, heartbeatMessage(agent, time.Now())); err != nil {
		return err
	}

	runtimeCtx, cancelRuntime := context.WithTimeout(ctx, runtimeOperationTimeout)
	runtimeSnapshot, err := agent.BuildRuntimeSnapshot(runtimeCtx, time.Now())
	cancelRuntime()
	if err != nil {
		return fmt.Errorf("initial runtime snapshot failed: %w", err)
	}
	if err := sendOrAbort(ctx, outbound, &gatewayrpc.ConnectClientMessage{
		Body: &gatewayrpc.ConnectClientMessage_Snapshot{Snapshot: runtimeSnapshot},
	}); err != nil {
		return err
	}
	slog.Info("initial runtime snapshot sent", "agent_id", agent.AgentID(), "node", agent.NodeName())

	usageCtx, cancelUsage := context.WithTimeout(ctx, runtimeOperationTimeout)
	usageSnapshot, err := agent.BuildUsageSnapshot(usageCtx, time.Now())
	cancelUsage()
	if err == nil {
		if err := sendOrAbort(ctx, outbound, &gatewayrpc.ConnectClientMessage{
			Body: &gatewayrpc.ConnectClientMessage_Snapshot{Snapshot: usageSnapshot},
		}); err != nil {
			return err
		}
	} else {
		slog.Warn("initial usage snapshot unavailable, continuing without metrics", "error", err)
	}

	ipPollCtx, cancelIPPoll := context.WithTimeout(ctx, runtimeOperationTimeout)
	if err := agent.PollActiveIPs(ipPollCtx); err == nil {
		ipSnapshot := agent.BuildIPSnapshot(time.Now())
		slog.Info("initial ip snapshot built", "client_ips_count", len(ipSnapshot.ClientIps))
		if len(ipSnapshot.ClientIps) > 0 {
			if err := sendOrAbort(ctx, outbound, &gatewayrpc.ConnectClientMessage{
				Body: &gatewayrpc.ConnectClientMessage_Snapshot{Snapshot: ipSnapshot},
			}); err != nil {
				cancelIPPoll()
				return err
			}
		}
	} else {
		slog.Warn("initial ip poll unavailable, continuing without active IPs", "error", err)
	}
	cancelIPPoll()

	return nil
}

func handleClientDataRequest(
	connectionCtx context.Context,
	agent *runtime.Agent,
	criticalOutbound chan<- *gatewayrpc.ConnectClientMessage,
	req *gatewayrpc.ClientDataRequest,
) {
	reqCtx, cancel := context.WithTimeout(connectionCtx, runtimeOperationTimeout)
	response := agent.HandleClientDataRequest(reqCtx, req.GetRequestId())
	cancel()

	select {
	case <-connectionCtx.Done():
	case criticalOutbound <- &gatewayrpc.ConnectClientMessage{
		Body: &gatewayrpc.ConnectClientMessage_ClientDataResponse{ClientDataResponse: response},
	}:
	}
}

func sendError(sendErrors chan<- error, err error) {
	select {
	case sendErrors <- err:
	default:
	}
}
