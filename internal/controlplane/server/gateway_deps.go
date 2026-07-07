package server

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/agentrevocation"
	"github.com/lost-coder/panvex/internal/controlplane/agents"
	"github.com/lost-coder/panvex/internal/controlplane/agenttransport"
	"github.com/lost-coder/panvex/internal/controlplane/enrollment"
	"github.com/lost-coder/panvex/internal/controlplane/gateway"
	"github.com/lost-coder/panvex/internal/gatewayrpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// gateway_deps.go implements gateway.Deps on *Server: the cross-domain
// operations the extracted gateway package (P8.2d) reaches back into the
// server for. The authorization/revocation/renewal/enrollment bodies live
// here (not in the gateway package) because they touch server-only state
// (s.mu, s.revokedAgentIDs, s.authority, s.enrollmentRec,
// s.grpcConnectRateLimiter, s.transportSwitchPendingAt). The remaining
// methods are thin exported wrappers over existing private server methods.
var _ gateway.Deps = (*Server)(nil)

// AuthorizeAgentConnect runs the synchronous handshake required before the
// stream can stay open: identity check, in-memory revocation lookup,
// per-store cert serial pin, and per-agent rate limit. Returns the agent id
// and the cert serial it presented (so the mid-stream watcher can re-check
// the pin) on success.
func (s *Server) AuthorizeAgentConnect(ctx context.Context, sess agenttransport.AgentSession) (string, string, error) {
	// AuthenticatedAgentIdentity reads the peer cert from the stream's
	// gRPC context; sess.Context() is the canonical source for that
	// metadata even though `ctx` is plumbed for the downstream DB call.
	agentID, presentedSerial, err := gateway.AuthenticatedAgentIdentity(sess.Context()) //nolint:contextcheck // peer cert lives on sess.Context() (gRPC peer metadata), not the parent request ctx.
	if err != nil {
		return "", "", err
	}
	s.mu.RLock()
	_, revoked := s.revokedAgentIDs[agentID]
	s.mu.RUnlock()
	if revoked {
		return "", "", agentrevocation.RevokedStatus("agent certificate has been revoked").Err()
	}
	// Q4.U-S-04: cert pinning. The CN match (already covered by
	// AuthenticatedAgentIdentity) only guarantees the cert was issued by our CA
	// for someone claiming this agent_id. The serial pin proves the cert
	// is the one we last issued — replays of an older still-valid cert
	// (e.g. harvested from a backup or a rotation log) are rejected here.
	//
	// Fail closed on lookup error: pin verification is the only line
	// against harvested-cert replay (CN/CA validation does not dedupe
	// rotated serials). A transient DB error must not silently disable
	// it — we'd rather break the new connect attempt than let a leaked
	// cert resurrect a stream during a pool blip.
	if s.store != nil {
		expected, err := s.store.GetAgentCertSerial(ctx, agentID)
		if err != nil {
			// Still fail closed, but a client that walked away mid-handshake
			// (context canceled/deadline) is expected connection churn, not a
			// store fault — keep it out of the ERROR stream so real DB lookup
			// failures stay visible.
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				s.logger.InfoContext(ctx, "agent cert serial lookup aborted before completion — rejecting connect",
					"agent_id", agentID,
					"error", err)
			} else {
				s.logger.ErrorContext(ctx, "agent cert serial lookup failed — rejecting connect",
					"agent_id", agentID,
					"error", err)
			}
			return "", "", status.Error(codes.Unavailable, "agent cert pin check unavailable")
		}
		if expected != "" && expected != presentedSerial {
			s.logger.WarnContext(ctx, "agent cert serial mismatch — rejecting connect",
				"agent_id", agentID,
				"expected_serial", expected,
				"presented_serial", presentedSerial)
			return "", "", status.Error(codes.PermissionDenied, "agent certificate not pinned for this agent")
		}
	}
	if s.grpcConnectRateLimiter != nil && !s.grpcConnectRateLimiter.Allow(agentID, s.now()) {
		s.obs.ObserveRateLimitReject("grpc_connect")
		return "", "", status.Error(codes.ResourceExhausted, "connect rate limit exceeded")
	}
	return agentID, presentedSerial, nil
}

// ShouldTerminateForRevocation returns true when either the in-memory
// revoked set has the agent or the persisted cert pin no longer matches the
// presented serial.
func (s *Server) ShouldTerminateForRevocation(ctx context.Context, agentID, presentedSerial string) bool {
	s.mu.RLock()
	_, isRevoked := s.revokedAgentIDs[agentID]
	s.mu.RUnlock()
	if isRevoked {
		s.logger.InfoContext(ctx, "mid-stream revocation triggered, terminating agent stream", "agent_id", agentID)
		return true
	}
	if s.store == nil {
		return false
	}
	expected, err := s.store.GetAgentCertSerial(ctx, agentID)
	if err != nil {
		// Fail closed: a transient lookup failure must not silently
		// strip the only defense against harvested-cert replay. Tearing
		// the stream down forces the agent to reconnect, which retries
		// pin verification from scratch under authorizeAgentConnect.
		// On context cancel (graceful shutdown) the caller is already
		// going away, so the extra termination is a no-op.
		if ctx.Err() != nil {
			return false
		}
		s.logger.WarnContext(ctx, "mid-stream cert pin lookup failed, terminating",
			"agent_id", agentID, "error", err)
		return true
	}
	if expected != "" && expected != presentedSerial {
		s.logger.InfoContext(ctx, "mid-stream cert pin mismatch, terminating", "agent_id", agentID)
		return true
	}
	return false
}

// MarkTransportSwitchResolved clears the A2 "switched but never reconnected"
// marker: any accepted agent stream (inbound or outbound) proves the agent
// is reachable in its current transport mode.
func (s *Server) MarkTransportSwitchResolved(agentID string) {
	s.mu.Lock()
	delete(s.transportSwitchPendingAt, agentID)
	s.mu.Unlock()
}

// RenewAgentCertificate is the post-authentication core of the unary
// RenewCertificate RPC: revocation check, agent/request identity match,
// CSR issuance, in-memory cert-date update, serial persist and pin. The
// caller (Gateway.RenewCertificate) has already resolved agentID from the
// peer context.
func (s *Server) RenewAgentCertificate(ctx context.Context, agentID string, request *gatewayrpc.RenewCertificateRequest) (*gatewayrpc.RenewCertificateResponse, error) {
	s.mu.RLock()
	_, revoked := s.revokedAgentIDs[agentID]
	s.mu.RUnlock()
	if revoked {
		return nil, agentrevocation.RevokedStatus("agent certificate has been revoked").Err()
	}
	if agentID != request.AgentId {
		return nil, status.Error(codes.PermissionDenied, "certificate agent mismatch")
	}

	now := s.now()
	issued, err := s.authority.issueAgentCertificateFromCSR(request.GetCsrPem(), agentID, agentCertificateLifetime, true, now)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	// Update in-memory cert dates so the dashboard reflects the renewal.
	certIssuedAt := now.UTC()
	certExpiresAt := issued.ExpiresAt.UTC()
	s.mu.Lock()
	if agent, ok := s.updateAgentIdentity(agentID, func(a *Agent) {
		a.CertIssuedAt = &certIssuedAt
		a.CertExpiresAt = &certExpiresAt
		a.CertSerial = issued.Serial
	}); ok && s.batchWriter != nil {
		s.batchWriter.EnqueueAgent(agentToRecord(agent))
	}
	s.mu.Unlock()
	// Q4.U-S-04: pin the new serial so the in-flight stream (and any
	// reconnect that follows) only accept the freshly-issued cert.
	if s.store != nil {
		if err := s.store.UpdateAgentCertSerial(ctx, agentID, issued.Serial); err != nil {
			s.logger.WarnContext(ctx, "persist renewed agent cert serial failed", "agent_id", agentID, "error", err)
		}
	}
	s.persistAgentCertPin(ctx, agentID, issued.CertificatePEM)

	return &gatewayrpc.RenewCertificateResponse{
		CertificatePem: issued.CertificatePEM,
		CaPem:          issued.CAPEM,
		ExpiresAtUnix:  issued.ExpiresAt.Unix(),
	}, nil
}

// RecordEnrollmentSteps ingests an agent-reported batch of enrollment
// timeline events for the attempt identified by attempt_id. The agent
// calls this once the Connect stream is up so the panel timeline gains
// the agent-side stages it cannot otherwise observe (cert persisted,
// gateway dialed, tls handshake). An empty attempt_id, a nil recorder
// (mock stores without DB() — see lifecycle.go), or zero events all
// short-circuit to a successful no-op so the agent never panics on its
// best-effort flush. A genuine store error surfaces back to the caller
// so the agent's slog.Warn captures it.
func (s *Server) RecordEnrollmentSteps(ctx context.Context, req *gatewayrpc.ReportEnrollmentStepsRequest) (*gatewayrpc.ReportEnrollmentStepsResponse, error) {
	if req == nil || req.GetAttemptId() == "" {
		return &gatewayrpc.ReportEnrollmentStepsResponse{}, nil
	}
	rec := s.enrollmentRec
	if rec == nil {
		return &gatewayrpc.ReportEnrollmentStepsResponse{}, nil
	}
	events := req.GetEvents()
	if len(events) == 0 {
		return &gatewayrpc.ReportEnrollmentStepsResponse{}, nil
	}
	out := make([]enrollment.AgentReportedEvent, 0, len(events))
	for _, e := range events {
		if e == nil {
			continue
		}
		var fields map[string]any
		if raw := e.GetFields(); len(raw) > 0 {
			fields = make(map[string]any, len(raw))
			for k, v := range raw {
				fields[k] = v
			}
		}
		out = append(out, enrollment.AgentReportedEvent{
			Step:    enrollment.Step(e.GetStep()),
			Level:   enrollment.Level(e.GetLevel()),
			Ts:      e.GetTs().AsTime(),
			Message: e.GetMessage(),
			Fields:  fields,
		})
	}
	if err := rec.Ingest(ctx, req.GetAttemptId(), out); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &gatewayrpc.ReportEnrollmentStepsResponse{}, nil
}

// HandleInStreamRenewalRequest processes a cert renewal request from an agent
// over the existing Connect bidi-stream. The response is sent back inline;
// errors are reported via RenewalResponse.error so the stream stays open.
func (s *Server) HandleInStreamRenewalRequest(ctx context.Context, agentID string, sess agenttransport.AgentSession, req *gatewayrpc.RenewalRequest) {
	if req.GetAgentId() != agentID {
		s.logger.WarnContext(ctx, "renewal request agent_id mismatch", "stream_agent_id", agentID, "request_agent_id", req.GetAgentId())
		_ = sess.Send(&gatewayrpc.ConnectServerMessage{
			Body: &gatewayrpc.ConnectServerMessage_RenewalResponse{
				RenewalResponse: &gatewayrpc.RenewalResponse{
					Error: "agent_id mismatch",
				},
			},
		})
		return
	}

	// Reject renewals from revoked agents. Without this, an agent whose
	// stream is still alive in the window before the 30s revocation watcher
	// tears it down could obtain a fresh 30-day cert (agentCertificateLifetime) AND have the panel
	// re-pin its new serial (line ~323) — defeating both revocation and the
	// serial-pin replay defense. Mirrors the unary RenewCertificate guard.
	s.mu.RLock()
	_, revoked := s.revokedAgentIDs[agentID]
	s.mu.RUnlock()
	if revoked {
		s.logger.WarnContext(ctx, "in-stream cert renewal: agent revoked", "agent_id", agentID)
		_ = sess.Send(&gatewayrpc.ConnectServerMessage{
			Body: &gatewayrpc.ConnectServerMessage_RenewalResponse{
				RenewalResponse: &gatewayrpc.RenewalResponse{
					Error: "agent certificate has been revoked",
				},
			},
		})
		return
	}

	csrPEM := req.GetCsrPem()
	certPEM, caPEM, expiresAt, err := s.authority.SignCSR(csrPEM, agentID, agentCertificateLifetime)
	if err != nil {
		s.logger.WarnContext(ctx, "in-stream cert renewal: sign CSR failed", "agent_id", agentID, "error", err)
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
	if agent, ok := s.updateAgentIdentity(agentID, func(a *Agent) {
		a.CertIssuedAt = &certIssuedAt
		a.CertExpiresAt = &certExpiresAtUTC
		if newSerial != "" {
			a.CertSerial = newSerial
		}
	}); ok && s.batchWriter != nil {
		s.batchWriter.EnqueueAgent(agentToRecord(agent))
	}
	s.mu.Unlock()

	// Persist the new serial so future connects and revocation checks use it.
	if s.store != nil && newSerial != "" {
		if err := s.store.UpdateAgentCertSerial(ctx, agentID, newSerial); err != nil {
			s.logger.WarnContext(ctx, "in-stream cert renewal: persist cert serial failed", "agent_id", agentID, "error", err)
		}
	}
	// In-stream renewal rotates the agent key — must persist the new SPKI pin
	// or the fail-closed dial verifier (Task 5 / A1) would block the agent
	// after its first in-stream renewal.
	s.persistAgentCertPin(ctx, agentID, certPEM)

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
		s.logger.WarnContext(ctx, "in-stream cert renewal: send response failed", "agent_id", agentID, "error", sendErr)
		return
	}

	s.logger.InfoContext(ctx, "in-stream cert renewal completed", "agent_id", agentID, "expires_at", expiresAt.UTC())
	s.appendAuditWithContext(ctx, agentID, auditAgentsCertRenewed, agentID, map[string]any{
		"expires_at": expiresAt.UTC().Format(time.RFC3339),
	})
}

// --- thin wrappers over existing private server methods ---------------------

// RegisterAgentSession installs a new gRPC stream session for agentID.
func (s *Server) RegisterAgentSession(agentID string, cancelConn context.CancelFunc) (*agents.Session, func()) {
	return s.registerAgentSession(agentID, cancelConn)
}

// NotifyAgentSession wakes the session currently attached to agentID.
func (s *Server) NotifyAgentSession(agentID string) {
	s.notifyAgentSession(agentID)
}

// ApplyAgentSnapshot applies an agent runtime snapshot against panel state.
func (s *Server) ApplyAgentSnapshot(ctx context.Context, snap gateway.AgentSnapshot) error {
	return s.applyAgentSnapshot(ctx, snap)
}

// AppendAudit records one audit-trail entry (best-effort).
func (s *Server) AppendAudit(ctx context.Context, actorID, action, targetID string, details map[string]any) {
	s.appendAuditWithContext(ctx, actorID, action, targetID, details)
}

// RecordClientJobResult updates client deployment state from a job result.
func (s *Server) RecordClientJobResult(ctx context.Context, agentID, jobID string, success bool, message, resultJSON string, observedAt time.Time) {
	s.recordClientJobResultWithContext(ctx, agentID, jobID, success, message, resultJSON, observedAt)
}

// ReconcileDiscoveredClients reconciles a full client-list response.
func (s *Server) ReconcileDiscoveredClients(ctx context.Context, agentID string, records []*gatewayrpc.ClientDetailRecord, telemtUnreachable bool, observedAt time.Time) {
	s.reconcileDiscoveredClients(ctx, agentID, records, telemtUnreachable, observedAt)
}

// ResolveClientIDByName resolves a client_id from an agent-scoped name.
func (s *Server) ResolveClientIDByName(agentID, clientName string) string {
	return s.resolveClientIDByName(agentID, clientName)
}
