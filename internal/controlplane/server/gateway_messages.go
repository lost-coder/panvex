package server

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/agenttransport"
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

func (s *Server) processRegularAgentMessage(
	connectionCtx context.Context,
	agentID string,
	sess agenttransport.AgentSession,
	regularSnapshots chan agentSnapshot,
	message *gatewayrpc.ConnectClientMessage,
) error {
	if hb := message.GetHeartbeat(); hb != nil {
		s.logger.Debug(logMessageReceived, "agent_id", agentID, "type", "heartbeat")
		// P2-LOG-11 / L-11: heartbeat-specific Debug log with the
		// agent-supplied observed_at so silent-agent troubleshooting can
		// confirm the panel actually received the heartbeat (vs the
		// agent never sending it). Debug-level — off by default in
		// prod, no per-tick spam at Info.
		s.logger.DebugContext(connectionCtx, "heartbeat received",
			"agent_id", agentID,
			"observed_at_unix", hb.ObservedAtUnix,
		)
		enqueueRegularSnapshot(connectionCtx, regularSnapshots, agentSnapshot{
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
		s.handleSnapshotMessage(connectionCtx, agentID, regularSnapshots, snap)
		return nil
	}

	if resp := message.GetClientDataResponse(); resp != nil {
		s.logger.Debug(logMessageReceived, "agent_id", agentID, "type", "client_data_response")
		// Run synchronously within the regular message processor goroutine
		// to prevent unbounded goroutine accumulation from repeated responses.
		s.reconcileDiscoveredClients(connectionCtx, agentID, resp.GetClients(), s.now())
		return nil
	}

	if req := message.GetRenewalRequest(); req != nil {
		s.logger.Debug(logMessageReceived, "agent_id", agentID, "type", "renewal_request")
		s.handleInStreamRenewalRequest(connectionCtx, agentID, sess, req)
		return nil
	}

	return s.processPriorityAgentMessage(connectionCtx, agentID, message)
}

func (s *Server) processPriorityAgentMessage(ctx context.Context, agentID string, message *gatewayrpc.ConnectClientMessage) error {
	return s.processPriorityAgentMessageAsync(ctx, nil, nil, agentID, message)
}

func (s *Server) processPriorityAgentMessageAsync(
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
		s.logger.Debug(logMessageReceived, "agent_id", agentID, "type", "job_result", "job_id", jr.JobId, "success", jr.Success)
		observedAt := time.Unix(jr.ObservedAtUnix, 0).UTC()
		s.recordJobResultState(
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
			s.recordClientJobResultWithContext(connectionCtx, agentID, jr.JobId, jr.Success, jr.Message, jr.ResultJson, observedAt)
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
			s.appendAuditWithContext(connectionCtx, agentID, auditJobsResult, jr.JobId, details)
		}
	}
	if ack := message.GetJobAcknowledgement(); ack != nil {
		observedAt := time.Unix(ack.ObservedAtUnix, 0).UTC()
		s.recordJobAcknowledgedState(
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
			s.appendAuditWithContext(connectionCtx, agentID, auditJobsAcknowledged, ack.JobId, map[string]any{})
		}
	}

	return nil
}

// handleInStreamRenewalRequest processes a cert renewal request from an agent
// over the existing Connect bidi-stream. The response is sent back inline;
// errors are reported via RenewalResponse.error so the stream stays open.
func (s *Server) handleInStreamRenewalRequest(ctx context.Context, agentID string, sess agenttransport.AgentSession, req *gatewayrpc.RenewalRequest) {
	if req.GetAgentId() != agentID {
		s.logger.Warn("renewal request agent_id mismatch", "stream_agent_id", agentID, "request_agent_id", req.GetAgentId())
		_ = sess.Send(&gatewayrpc.ConnectServerMessage{
			Body: &gatewayrpc.ConnectServerMessage_RenewalResponse{
				RenewalResponse: &gatewayrpc.RenewalResponse{
					Error: "agent_id mismatch",
				},
			},
		})
		return
	}

	csrPEM := req.GetCsrPem()
	certPEM, caPEM, expiresAt, err := s.authority.SignCSR(csrPEM, agentID, agentCertificateLifetime)
	if err != nil {
		s.logger.Warn("in-stream cert renewal: sign CSR failed", "agent_id", agentID, "error", err)
		_ = sess.Send(&gatewayrpc.ConnectServerMessage{
			Body: &gatewayrpc.ConnectServerMessage_RenewalResponse{
				RenewalResponse: &gatewayrpc.RenewalResponse{
					Error: err.Error(),
				},
			},
		})
		return
	}

	// Parse the new serial so we can update the pin.
	newSerial := ""
	if block, _ := pem.Decode([]byte(certPEM)); block != nil {
		if parsed, parseErr := x509.ParseCertificate(block.Bytes); parseErr == nil {
			newSerial = parsed.SerialNumber.Text(16)
		}
	}

	// Update in-memory cert dates.
	certIssuedAt := s.now().UTC()
	certExpiresAtUTC := expiresAt.UTC()
	s.mu.Lock()
	if agent, ok := s.agents[agentID]; ok {
		agent.CertIssuedAt = &certIssuedAt
		agent.CertExpiresAt = &certExpiresAtUTC
		if newSerial != "" {
			agent.CertSerial = newSerial
		}
		s.agents[agentID] = agent
		if s.batchWriter != nil {
			s.batchWriter.agents.Enqueue(agentToRecord(agent))
		}
	}
	s.mu.Unlock()

	// Persist the new serial so future connects and revocation checks use it.
	if s.store != nil && newSerial != "" {
		if err := s.store.UpdateAgentCertSerial(ctx, agentID, newSerial); err != nil {
			s.logger.Warn("in-stream cert renewal: persist cert serial failed", "agent_id", agentID, "error", err)
		}
	}

	sendErr := sess.Send(&gatewayrpc.ConnectServerMessage{
		Body: &gatewayrpc.ConnectServerMessage_RenewalResponse{
			RenewalResponse: &gatewayrpc.RenewalResponse{
				CertificatePem: certPEM,
				CaPem:          caPEM,
				ExpiresAtUnix:  expiresAt.Unix(),
			},
		},
	})
	if sendErr != nil {
		s.logger.Warn("in-stream cert renewal: send response failed", "agent_id", agentID, "error", sendErr)
		return
	}

	s.logger.Info("in-stream cert renewal completed", "agent_id", agentID, "expires_at", expiresAt.UTC())
	s.appendAuditWithContext(ctx, agentID, auditAgentsCertRenewed, agentID, map[string]any{
		"expires_at": expiresAt.UTC().Format(time.RFC3339),
	})
}
