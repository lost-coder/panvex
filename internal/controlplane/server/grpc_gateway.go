package server

import (
	"context"
	"errors"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/agentrevocation"
	"github.com/lost-coder/panvex/internal/controlplane/agenttransport"
	"github.com/lost-coder/panvex/internal/controlplane/enrollment"
	"github.com/lost-coder/panvex/internal/gatewayrpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

const (
	// jobDispatchRetryAfter bounds how long a sent command can remain unacknowledged before redelivery.
	jobDispatchRetryAfter = 30 * time.Second
	// jobDispatchRetryInterval defines how often the dispatcher checks for unacknowledged commands.
	jobDispatchRetryInterval = 5 * time.Second
	// discoveryRefreshInterval is how often the panel re-requests a full client
	// list from each connected agent, so a Telemt that recovered without a stream
	// reconnect (and any other drift) is reconciled within one interval even if
	// the recovery-edge trigger is missed.
	discoveryRefreshInterval = 10 * time.Minute
	// jobDispatchBatchSize bounds one dispatch pass to avoid monopolizing one stream under large backlogs.
	jobDispatchBatchSize = 32
	// priorityInboundWorkerCount defines how many workers consume critical job acknowledgements and results.
	priorityInboundWorkerCount = 2
	// priorityAuditQueueCapacity bounds asynchronous audit persistence from priority stream events.
	priorityAuditQueueCapacity = 256
	// priorityResultEffectQueueCapacity bounds asynchronous client deployment updates from job results.
	priorityResultEffectQueueCapacity = 128
	// regularSnapshotQueueCapacity bounds asynchronous snapshot processing per live stream.
	regularSnapshotQueueCapacity = 64
)

// RenewCertificate rotates the short-lived mTLS material for an authenticated agent.
func (s *Server) RenewCertificate(ctx context.Context, request *gatewayrpc.RenewCertificateRequest) (*gatewayrpc.RenewCertificateResponse, error) {
	agentID, err := authenticatedAgentID(ctx)
	if err != nil {
		return nil, err
	}
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
	issued, err := s.authority.issueClientCertificate(agentID, now)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
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
		s.batchWriter.agents.Enqueue(agentToRecord(agent))
	}
	s.mu.Unlock()
	// Q4.U-S-04: pin the new serial so the in-flight stream (and any
	// reconnect that follows) only accept the freshly-issued cert.
	if s.store != nil {
		if err := s.store.UpdateAgentCertSerial(ctx, agentID, issued.Serial); err != nil {
			s.logger.Warn("persist renewed agent cert serial failed", "agent_id", agentID, "error", err)
		}
	}

	return &gatewayrpc.RenewCertificateResponse{
		CertificatePem: issued.CertificatePEM,
		PrivateKeyPem:  issued.PrivateKeyPEM,
		CaPem:          issued.CAPEM,
		ExpiresAtUnix:  issued.ExpiresAt.Unix(),
	}, nil
}

// ReportEnrollmentSteps ingests an agent-reported batch of enrollment
// timeline events for the attempt identified by attempt_id. The agent
// calls this once the Connect stream is up so the panel timeline gains
// the agent-side stages it cannot otherwise observe (cert persisted,
// gateway dialed, tls handshake). An empty attempt_id, a nil recorder
// (mock stores without DB() — see lifecycle.go), or zero events all
// short-circuit to a successful no-op so the agent never panics on its
// best-effort flush. A genuine store error surfaces back to the caller
// so the agent's slog.Warn captures it.
func (s *Server) ReportEnrollmentSteps(ctx context.Context, req *gatewayrpc.ReportEnrollmentStepsRequest) (*gatewayrpc.ReportEnrollmentStepsResponse, error) {
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

// authorizeAgentConnect runs the synchronous handshake required before the
// stream can stay open: identity check, in-memory revocation lookup,
// per-store cert serial pin, and per-agent rate limit. Returns the agent id
// and the cert serial it presented (so the mid-stream watcher can re-check
// the pin) on success.
func (s *Server) authorizeAgentConnect(ctx context.Context, sess agenttransport.AgentSession) (string, string, error) {
	// authenticatedAgentIdentity reads the peer cert from the stream's
	// gRPC context; sess.Context() is the canonical source for that
	// metadata even though `ctx` is plumbed for the downstream DB call.
	agentID, presentedSerial, err := authenticatedAgentIdentity(sess.Context()) //nolint:contextcheck // peer cert lives on sess.Context() (gRPC peer metadata), not the parent request ctx.
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
	// authenticatedAgentID) only guarantees the cert was issued by our CA
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
			s.logger.Error("agent cert serial lookup failed — rejecting connect",
				"agent_id", agentID,
				"error", err)
			return "", "", status.Error(codes.Unavailable, "agent cert pin check unavailable")
		}
		if expected != "" && expected != presentedSerial {
			s.logger.Warn("agent cert serial mismatch — rejecting connect",
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

// runAgentSession runs the bidirectional agent protocol over the given
// transport session. Direction-agnostic — works for both inbound (agent
// dialed the panel) and outbound (panel dialed the agent) sessions.
func (s *Server) runAgentSession(ctx context.Context, sess agenttransport.AgentSession) error {
	agentID, presentedSerial, err := s.authorizeAgentConnect(ctx, sess)
	if err != nil {
		// P2-LOG-11 / L-11: ensure every stream-close path produces a
		// single "agent stream closed" log line. The post-auth paths
		// log via awaitAgentStreamShutdown; this branch covers the
		// pre-auth reject so the close is observable from logs alone.
		// agentID may be empty here (auth never produced a CN); slog
		// drops empty values cleanly.
		reason := "auth_rejected"
		if errors.Is(err, context.Canceled) {
			reason = "context_cancelled"
		}
		s.logger.InfoContext(ctx, logAgentStreamClosed,
			"agent_id", agentID,
			"reason", reason,
			"error", err,
		)
		return err
	}

	session, unregisterSession := s.registerAgentSession(agentID)
	defer unregisterSession()
	// P2-LOG-12 / L-05: MarkConnected exactly once per stream open, here.
	// applyAgentSnapshot now only calls Heartbeat so subsequent heartbeat
	// snapshots do not rewrite connectedAt to "now" and thereby mask short
	// disconnects. On reconnect, the next Connect() call produces a fresh
	// MarkConnected and the connectedAt timestamp moves forward.
	s.presence.MarkConnected(agentID, s.now())
	s.logger.Info("accepted agent stream", "agent_id", agentID)

	connectionCtx, cancelConnection := context.WithCancel(ctx)
	defer cancelConnection()

	channels := newAgentStreamChannels()
	processErrorAndCancel := func(err error) {
		nonBlockingSend(channels.processErrors, err)
		cancelConnection()
	}

	s.startRevocationWatcher(connectionCtx, cancelConnection, agentID, presentedSerial)
	s.startReceiveLoop(connectionCtx, cancelConnection, agentID, sess, channels)
	s.startPriorityInboundWorkers(connectionCtx, cancelConnection, agentID, channels, processErrorAndCancel)
	s.startAuditEffectsLoop(connectionCtx, cancelConnection, agentID, channels)
	s.startResultEffectsLoop(connectionCtx, cancelConnection, agentID, channels)
	s.startSnapshotApplyLoop(connectionCtx, cancelConnection, agentID, channels, processErrorAndCancel)
	s.startRegularInboundLoop(connectionCtx, cancelConnection, agentID, sess, channels, processErrorAndCancel)
	s.startJobDispatchLoop(connectionCtx, cancelConnection, agentID, sess, session, channels)

	return s.awaitAgentStreamShutdown(cancelConnection, agentID, channels)
}

func (s *Server) Connect(stream gatewayrpc.AgentGateway_ConnectServer) error {
	return s.runAgentSession(stream.Context(), &agenttransport.ServerStreamSession{Stream: stream})
}

// RunAgentSession is the public SessionHandler entry point used by
// agenttransport.Manager. It currently ignores meta — the agent identity
// is rediscovered inside runAgentSession via the gRPC peer context — but
// keeps the SessionHandler signature so future tasks can pass pre-resolved
// metadata without touching this layer.
func (s *Server) RunAgentSession(ctx context.Context, sess agenttransport.AgentSession, meta agenttransport.NodeMeta) error {
	_ = meta
	return s.runAgentSession(ctx, sess)
}

func authenticatedAgentID(ctx context.Context) (string, error) {
	id, _, err := authenticatedAgentIdentity(ctx)
	return id, err
}

// authenticatedAgentIdentity returns the (agent_id, serial-hex) pair
// for the peer's client certificate. The CN is the agent_id; the
// serial is hex-encoded big-endian so callers can compare it against
// the persisted pin (Q4.U-S-04).
func authenticatedAgentIdentity(ctx context.Context) (string, string, error) {
	peerInfo, ok := peer.FromContext(ctx)
	if !ok || peerInfo.AuthInfo == nil {
		return "", "", status.Error(codes.Unauthenticated, "missing peer identity")
	}

	tlsInfo, ok := peerInfo.AuthInfo.(credentials.TLSInfo)
	if !ok {
		return "", "", status.Error(codes.Unauthenticated, "unexpected peer auth type")
	}
	if len(tlsInfo.State.PeerCertificates) == 0 {
		return "", "", status.Error(codes.Unauthenticated, "missing client certificate")
	}

	cert := tlsInfo.State.PeerCertificates[0]
	return cert.Subject.CommonName, cert.SerialNumber.Text(16), nil
}
