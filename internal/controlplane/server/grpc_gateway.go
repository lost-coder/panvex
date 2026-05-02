package server

import (
	"context"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/agentrevocation"
	"github.com/lost-coder/panvex/internal/controlplane/agenttransport"
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
	if agent, ok := s.agents[agentID]; ok {
		agent.CertIssuedAt = &certIssuedAt
		agent.CertExpiresAt = &certExpiresAt
		agent.CertSerial = issued.Serial
		s.agents[agentID] = agent
		if s.batchWriter != nil {
			s.batchWriter.agents.Enqueue(agentToRecord(agent))
		}
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

// authorizeAgentConnect runs the synchronous handshake required before the
// stream can stay open: identity check, in-memory revocation lookup,
// per-store cert serial pin, and per-agent rate limit. Returns the agent id
// and the cert serial it presented (so the mid-stream watcher can re-check
// the pin) on success.
func (s *Server) authorizeAgentConnect(sess agenttransport.AgentSession) (string, string, error) {
	agentID, presentedSerial, err := authenticatedAgentIdentity(sess.Context())
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
		expected, err := s.store.GetAgentCertSerial(sess.Context(), agentID)
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
	agentID, presentedSerial, err := s.authorizeAgentConnect(sess)
	if err != nil {
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

