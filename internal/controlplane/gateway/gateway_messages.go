package gateway

import (
	"context"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/agenttransport"
	"github.com/lost-coder/panvex/internal/controlplane/eventbus"
	cpevents "github.com/lost-coder/panvex/internal/controlplane/events"
	"github.com/lost-coder/panvex/internal/controlplane/runtimeevents"
	"github.com/lost-coder/panvex/internal/gatewayrpc"
	"github.com/prometheus/client_golang/prometheus"
)

// enqueueInboundAgentMessage routes a freshly-received agent message into
// either the priority or the regular inbound queue. Priority messages
// (job_result / job_acknowledgement) block until the queue accepts them.
// Regular messages follow drop-oldest semantics: try once, drain one stale
// slot, try again. If a concurrent reader races with the drain the final
// send may still find the channel full — in that rare case we bump
// dropCounter (when non-nil) so operators get visibility into the silent
// drop (D-2). The counter is intentionally label-less; see metrics.go for
// the cardinality rationale.
func enqueueInboundAgentMessage(
	connectionCtx context.Context,
	priorityInbound chan<- *gatewayrpc.ConnectClientMessage,
	regularInbound chan *gatewayrpc.ConnectClientMessage,
	message *gatewayrpc.ConnectClientMessage,
	dropCounter prometheus.Counter,
) bool {
	if connectionCtx.Err() != nil {
		return false
	}

	if isPriorityAgentMessage(message) {
		select {
		case <-connectionCtx.Done():
			return false
		case priorityInbound <- message:
			return true
		}
	}

	select {
	case <-connectionCtx.Done():
		return false
	case regularInbound <- message:
		return true
	default:
	}

	// Drop one stale non-critical update to keep room for the most recent snapshot/heartbeat.
	select {
	case <-regularInbound:
	default:
	}

	select {
	case <-connectionCtx.Done():
		return false
	case regularInbound <- message:
		return true
	default:
	}

	// Backpressure — drop-oldest semantics failed because a concurrent reader
	// raced with the drain. Record the drop so operators can see it.
	if dropCounter != nil {
		dropCounter.Inc()
	}
	return true
}

func isPriorityAgentMessage(message *gatewayrpc.ConnectClientMessage) bool {
	return message.GetJobResult() != nil || message.GetJobAcknowledgement() != nil
}

func (g *Gateway) processRegularAgentMessage(
	connectionCtx context.Context,
	agentID string,
	sess agenttransport.AgentSession,
	regularSnapshots chan AgentSnapshot,
	message *gatewayrpc.ConnectClientMessage,
) error {
	if hb := message.GetHeartbeat(); hb != nil {
		g.logger.DebugContext(connectionCtx, logMessageReceived, "agent_id", agentID, "type", "heartbeat")
		// P2-LOG-11 / L-11: heartbeat-specific Debug log with the
		// agent-supplied observed_at so silent-agent troubleshooting can
		// confirm the panel actually received the heartbeat (vs the
		// agent never sending it). Debug-level — off by default in
		// prod, no per-tick spam at Info.
		g.logger.DebugContext(connectionCtx, "heartbeat received",
			"agent_id", agentID,
			"observed_at_unix", hb.ObservedAtUnix,
		)
		enqueueRegularSnapshot(connectionCtx, regularSnapshots, AgentSnapshot{
			AgentID:      agentID,
			NodeName:     hb.NodeName,
			FleetGroupID: hb.FleetGroupId,
			Version:      hb.Version,
			ReadOnly:     hb.ReadOnly,
			ObservedAt:   time.Unix(hb.ObservedAtUnix, 0).UTC(),
		})
		return nil
	}

	if snap := message.GetSnapshot(); snap != nil {
		g.handleSnapshotMessage(connectionCtx, agentID, regularSnapshots, snap)
		return nil
	}

	if resp := message.GetClientDataResponse(); resp != nil {
		g.logger.DebugContext(connectionCtx, logMessageReceived, "agent_id", agentID, "type", "client_data_response")
		// Run synchronously within the regular message processor goroutine
		// to prevent unbounded goroutine accumulation from repeated responses.
		g.deps.ReconcileDiscoveredClients(connectionCtx, agentID, resp.GetClients(), resp.GetTelemtUnreachable(), g.now())
		return nil
	}

	if req := message.GetRenewalRequest(); req != nil {
		g.logger.DebugContext(connectionCtx, logMessageReceived, "agent_id", agentID, "type", "renewal_request")
		g.deps.HandleInStreamRenewalRequest(connectionCtx, agentID, sess, req)
		return nil
	}

	if batch := message.GetRuntimeEvents(); batch != nil {
		g.logger.DebugContext(connectionCtx, logMessageReceived, "agent_id", agentID, "type", "runtime_events")
		g.handleRuntimeEventsBatch(agentID, batch)
		return nil
	}

	return g.processPriorityAgentMessage(connectionCtx, agentID, message)
}

// handleRuntimeEventsBatch converts a proto RuntimeEventsBatch into the
// panel-side runtimeevents.Event shape, appends the events to the
// per-agent ring buffer, and publishes one runtime.events batch event
// on the events bus. The dashboard's /events websocket subscribes to
// that bus so warnings and errors surface live without polling. The
// buffer is constructed unconditionally in newServerFromOptions; a
// nil-check is kept for defence in depth so a future code path that
// constructs a Server without going through the canonical constructor
// cannot panic here.
func (g *Gateway) handleRuntimeEventsBatch(agentID string, batch *gatewayrpc.RuntimeEventsBatch) {
	if batch == nil {
		return
	}
	protoEvents := batch.GetEvents()
	if len(protoEvents) == 0 {
		return
	}
	events := make([]runtimeevents.Event, 0, len(protoEvents))
	for _, e := range protoEvents {
		if e == nil {
			continue
		}
		var fields map[string]string
		if pf := e.GetFields(); len(pf) > 0 {
			fields = make(map[string]string, len(pf))
			for k, v := range pf {
				fields[k] = v
			}
		}
		events = append(events, runtimeevents.Event{
			Seq:     e.GetSeq(),
			Ts:      e.GetTs().AsTime(),
			Level:   e.GetLevel(),
			Message: e.GetMessage(),
			Fields:  fields,
		})
	}
	if len(events) == 0 {
		return
	}
	if g.runtimeEvents != nil {
		// Audit #9b: AppendBatch drops reconnect replays via the per-agent
		// (seq, ts) watermark; publish only what was actually stored so the
		// dashboard's live feed does not repeat up to 200 events either.
		events = g.runtimeEvents.AppendBatch(agentID, events)
	}
	if len(events) == 0 || g.events == nil {
		return
	}
	// D6a: one bus event per inbound batch instead of one per record. A
	// chatty agent shipping 50 records per push used to cost 50 hub
	// broadcasts × N subscribers and burn 50 of the 64 per-subscriber buffer
	// slots in a single burst; now it costs one. The dashboard's per-agent
	// hook (useAgentRuntimeEvents) consumes the batched shape.
	records := make([]map[string]any, 0, len(events))
	for _, ev := range events {
		records = append(records, map[string]any{
			"agent_id": agentID,
			"ts":       ev.Ts,
			"level":    ev.Level,
			"message":  ev.Message,
			"fields":   ev.Fields,
		})
	}
	g.events.Publish(eventbus.Event{
		Type: cpevents.TypeRuntimeEvents,
		Data: map[string]any{
			"agent_id": agentID,
			"events":   records,
		},
	})
}

func (g *Gateway) processPriorityAgentMessage(ctx context.Context, agentID string, message *gatewayrpc.ConnectClientMessage) error {
	return g.processPriorityAgentMessageAsync(ctx, nil, nil, agentID, message)
}

func (g *Gateway) processPriorityAgentMessageAsync(
	connectionCtx context.Context,
	priorityResultEffects chan<- jobResultEffect,
	priorityAuditEffects chan<- auditEffect,
	agentID string,
	message *gatewayrpc.ConnectClientMessage,
) error {
	if message.GetJobResult() == nil && message.GetJobAcknowledgement() == nil {
		return nil
	}

	if jr := message.GetJobResult(); jr != nil {
		g.logger.DebugContext(connectionCtx, logMessageReceived, "agent_id", agentID, "type", "job_result", "job_id", jr.JobId, "success", jr.Success)
		observedAt := time.Unix(jr.ObservedAtUnix, 0).UTC()
		g.recordJobResultState(
			connectionCtx,
			agentID,
			jr.JobId,
			jr.Success,
			jr.Message,
			jr.ResultJson,
			observedAt,
		)
		if !enqueuePriorityResultEffect(connectionCtx, priorityResultEffects, jobResultEffect{
			agentID:    agentID,
			jobID:      jr.JobId,
			success:    jr.Success,
			message:    jr.Message,
			resultJSON: jr.ResultJson,
			observedAt: observedAt,
		}) {
			g.deps.RecordClientJobResult(connectionCtx, agentID, jr.JobId, jr.Success, jr.Message, jr.ResultJson, observedAt)
		}
		details := map[string]any{
			"success": jr.Success,
			"message": jr.Message,
		}
		if !enqueuePriorityAuditEffect(connectionCtx, priorityAuditEffects, auditEffect{
			actorID:  agentID,
			action:   auditJobsResult,
			targetID: jr.JobId,
			details:  details,
		}) {
			g.deps.AppendAudit(connectionCtx, agentID, auditJobsResult, jr.JobId, details)
		}
	}
	if ack := message.GetJobAcknowledgement(); ack != nil {
		observedAt := time.Unix(ack.ObservedAtUnix, 0).UTC()
		g.recordJobAcknowledgedState(
			connectionCtx,
			agentID,
			ack.JobId,
			observedAt,
		)
		if !enqueuePriorityAuditEffect(connectionCtx, priorityAuditEffects, auditEffect{
			actorID:  agentID,
			action:   auditJobsAcknowledged,
			targetID: ack.JobId,
			details:  map[string]any{},
		}) {
			g.deps.AppendAudit(connectionCtx, agentID, auditJobsAcknowledged, ack.JobId, map[string]any{})
		}
	}

	return nil
}
