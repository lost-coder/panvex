package gateway

import (
	"context"
	"errors"
	"time"

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
	// jobDispatchRetryInterval is the SAFETY-NET cadence at which the
	// per-agent dispatch loop re-checks for unacknowledged commands. The
	// primary delivery mechanism is the session Wake channel — every job
	// enqueue/state change calls notifyAgentSession, which wakes the loop
	// immediately. The ticker only covers lost wakes and stale-sent
	// retries, so it does not need to be tight: at a 2000-agent fleet a
	// 5s tick meant 400 idle PendingForAgent scans/s against the jobs
	// RWMutex (P6-6.3g, finding #14). 45s keeps the safety net while
	// cutting that background load ~9x. Job delivery latency is
	// unaffected (Wake path).
	jobDispatchRetryInterval = 45 * time.Second
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

// RenewCertificate rotates the short-lived mTLS material for an authenticated
// agent. The post-authentication core (cert issuance + re-pin + in-memory
// update) lives in the server package behind Deps.RenewAgentCertificate
// because it touches server-only state (s.authority, s.mu, s.store).
func (g *Gateway) RenewCertificate(ctx context.Context, request *gatewayrpc.RenewCertificateRequest) (*gatewayrpc.RenewCertificateResponse, error) {
	agentID, err := authenticatedAgentID(ctx)
	if err != nil {
		return nil, err
	}
	return g.deps.RenewAgentCertificate(ctx, agentID, request)
}

// ReportEnrollmentSteps ingests an agent-reported batch of enrollment
// timeline events. The body (nil-recorder short-circuits + store ingest)
// lives in server behind Deps.RecordEnrollmentSteps because it touches the
// server-only enrollment recorder.
func (g *Gateway) ReportEnrollmentSteps(ctx context.Context, req *gatewayrpc.ReportEnrollmentStepsRequest) (*gatewayrpc.ReportEnrollmentStepsResponse, error) {
	return g.deps.RecordEnrollmentSteps(ctx, req)
}

// runAgentSession runs the bidirectional agent protocol over the given
// transport session. Direction-agnostic — works for both inbound (agent
// dialed the panel) and outbound (panel dialed the agent) sessions.
func (g *Gateway) runAgentSession(ctx context.Context, sess agenttransport.AgentSession) error {
	agentID, presentedSerial, err := g.deps.AuthorizeAgentConnect(ctx, sess)
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
		g.logger.InfoContext(ctx, logAgentStreamClosed,
			"agent_id", agentID,
			"reason", reason,
			"error", err,
		)
		return err
	}

	connectionCtx, cancelConnection := context.WithCancel(ctx)
	defer cancelConnection()

	session, unregisterSession := g.deps.RegisterAgentSession(agentID, cancelConnection)
	defer unregisterSession()
	// P2-LOG-12 / L-05: MarkConnected exactly once per stream open, here.
	// applyAgentSnapshot now only calls Heartbeat so subsequent heartbeat
	// snapshots do not rewrite connectedAt to "now" and thereby mask short
	// disconnects. On reconnect, the next Connect() call produces a fresh
	// MarkConnected and the connectedAt timestamp moves forward.
	g.presence.MarkConnected(agentID, g.now())
	g.deps.MarkTransportSwitchResolved(agentID)
	g.logger.InfoContext(connectionCtx, "accepted agent stream", "agent_id", agentID)

	channels := newAgentStreamChannels()
	processErrorAndCancel := func(err error) {
		nonBlockingSend(channels.processErrors, err)
		cancelConnection()
	}

	g.startRevocationWatcher(connectionCtx, cancelConnection, agentID, presentedSerial)
	g.startReceiveLoop(connectionCtx, cancelConnection, agentID, sess, channels)
	g.startPriorityInboundWorkers(connectionCtx, cancelConnection, agentID, channels, processErrorAndCancel)
	g.startAuditEffectsLoop(connectionCtx, cancelConnection, agentID, channels)
	g.startResultEffectsLoop(connectionCtx, cancelConnection, agentID, channels)
	g.startSnapshotApplyLoop(connectionCtx, cancelConnection, agentID, channels, processErrorAndCancel)
	g.startRegularInboundLoop(connectionCtx, cancelConnection, agentID, sess, channels, processErrorAndCancel)
	g.startJobDispatchLoop(connectionCtx, cancelConnection, agentID, sess, session, channels)

	return g.awaitAgentStreamShutdown(connectionCtx, cancelConnection, agentID, channels)
}

// Connect handles an inbound (agent-dialed) Connect bidi-stream.
func (g *Gateway) Connect(stream gatewayrpc.AgentGateway_ConnectServer) error {
	return g.runAgentSession(stream.Context(), &agenttransport.ServerStreamSession{Stream: stream})
}

// RunAgentSession is the public SessionHandler entry point used by
// agenttransport.Manager. It currently ignores meta — the agent identity
// is rediscovered inside runAgentSession via the gRPC peer context — but
// keeps the SessionHandler signature so future tasks can pass pre-resolved
// metadata without touching this layer.
func (g *Gateway) RunAgentSession(ctx context.Context, sess agenttransport.AgentSession, meta agenttransport.NodeMeta) error {
	_ = meta
	return g.runAgentSession(ctx, sess)
}

func authenticatedAgentID(ctx context.Context) (string, error) {
	id, _, err := AuthenticatedAgentIdentity(ctx)
	return id, err
}

// AuthenticatedAgentIdentity returns the (agent_id, serial-hex) pair for the
// peer's client certificate. The CN is the agent_id; the serial is
// hex-encoded big-endian so callers can compare it against the persisted pin
// (Q4.U-S-04). Exported so the server package's AuthorizeAgentConnect can
// resolve the peer identity from the stream context.
func AuthenticatedAgentIdentity(ctx context.Context) (string, string, error) {
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
