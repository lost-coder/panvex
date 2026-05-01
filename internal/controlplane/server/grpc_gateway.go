package server

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/agentrevocation"
	"github.com/lost-coder/panvex/internal/controlplane/agenttransport"
	"github.com/lost-coder/panvex/internal/controlplane/agents"
	"github.com/lost-coder/panvex/internal/controlplane/jobs"
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

type jobResultEffect struct {
	agentID    string
	jobID      string
	success    bool
	message    string
	resultJSON string
	observedAt time.Time
}

type auditEffect struct {
	actorID  string
	action   string
	targetID string
	details  map[string]any
}

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

// Connect accepts live heartbeats, snapshots, and job results from one authenticated agent.
// agentStreamChannels owns the in-process queues shared by the goroutines
// running for one Connect() invocation. Held entirely on the stack — no
// references escape past Connect()'s return.
type agentStreamChannels struct {
	priorityInbound       chan *gatewayrpc.ConnectClientMessage
	priorityAuditEffects  chan auditEffect
	priorityResultEffects chan jobResultEffect
	regularInbound        chan *gatewayrpc.ConnectClientMessage
	regularSnapshots      chan agentSnapshot
	receiveErrors         chan error
	dispatchErrors        chan error
	processErrors         chan error
}

func newAgentStreamChannels() *agentStreamChannels {
	return &agentStreamChannels{
		priorityInbound:       make(chan *gatewayrpc.ConnectClientMessage, 32),
		priorityAuditEffects:  make(chan auditEffect, priorityAuditQueueCapacity),
		priorityResultEffects: make(chan jobResultEffect, priorityResultEffectQueueCapacity),
		regularInbound:        make(chan *gatewayrpc.ConnectClientMessage, 64),
		regularSnapshots:      make(chan agentSnapshot, regularSnapshotQueueCapacity),
		receiveErrors:         make(chan error, 1),
		dispatchErrors:        make(chan error, 1),
		processErrors:         make(chan error, 1),
	}
}

// nonBlockingSend posts err to ch if there is buffer space; otherwise drops
// it on the floor. Used for first-error channels that are guaranteed to be
// drained at most once.
func nonBlockingSend(ch chan<- error, err error) {
	select {
	case ch <- err:
	default:
	}
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

// recoverAgentStreamGoroutine is the deferred panic-recovery handler used by
// every goroutine spawned for a Connect() session. Logs the panic with the
// goroutine's name, bumps the panicRecoveredTotal counter (Q3.U-Q-15) and
// cancels the connection so the rest of the pipeline tears down cleanly.
func (s *Server) recoverAgentStreamGoroutine(agentID, name string, cancel context.CancelFunc) {
	if r := recover(); r != nil {
		slog.Error("goroutine panic recovered", "agent_id", agentID, "goroutine", name, "panic", r)
		if s.obs != nil && s.obs.panicRecoveredTotal != nil {
			s.obs.panicRecoveredTotal.WithLabelValues(name).Inc()
		}
		cancel()
	}
}

// Q4.U-S-23: mid-stream revocation watcher. The Connect-time check only
// catches a cert that was already revoked when the stream opened. A
// long-lived stream still has to honour an operator who revokes the agent
// later — without this ticker, the agent could keep running for the cert's
// full validity window. 30s is fast enough that an operator-initiated
// revocation hits within a dashboard refresh, slow enough not to add
// noticeable RPS.
func (s *Server) startRevocationWatcher(ctx context.Context, cancel context.CancelFunc, agentID, presentedSerial string) {
	go func() {
		defer s.recoverAgentStreamGoroutine(agentID, "revocation-watcher", cancel)
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if s.shouldTerminateForRevocation(ctx, agentID, presentedSerial) {
					cancel()
					return
				}
			}
		}
	}()
}

// shouldTerminateForRevocation returns true when either the in-memory
// revoked set has the agent or the persisted cert pin no longer matches the
// presented serial.
func (s *Server) shouldTerminateForRevocation(ctx context.Context, agentID, presentedSerial string) bool {
	s.mu.RLock()
	_, isRevoked := s.revokedAgentIDs[agentID]
	s.mu.RUnlock()
	if isRevoked {
		s.logger.Info("mid-stream revocation triggered, terminating agent stream", "agent_id", agentID)
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
		s.logger.Warn("mid-stream cert pin lookup failed, terminating",
			"agent_id", agentID, "error", err)
		return true
	}
	if expected != "" && expected != presentedSerial {
		s.logger.Info("mid-stream cert pin mismatch, terminating", "agent_id", agentID)
		return true
	}
	return false
}

// startReceiveLoop reads messages off the gRPC stream and routes them into
// the priority/regular inbound queues until the stream errors out.
func (s *Server) startReceiveLoop(ctx context.Context, cancel context.CancelFunc, agentID string, sess agenttransport.AgentSession, ch *agentStreamChannels) {
	go func() {
		defer s.recoverAgentStreamGoroutine(agentID, "receive", cancel)
		for {
			message, err := sess.Recv()
			if err != nil {
				nonBlockingSend(ch.receiveErrors, err)
				return
			}
			if !enqueueInboundAgentMessage(ctx, ch.priorityInbound, ch.regularInbound, message) {
				return
			}
		}
	}()
}

// startPriorityInboundWorkers spawns priorityInboundWorkerCount goroutines
// that drain the priority queue and route the messages through
// processPriorityAgentMessageAsync.
func (s *Server) startPriorityInboundWorkers(ctx context.Context, cancel context.CancelFunc, agentID string, ch *agentStreamChannels, processErr func(error)) {
	for workerIndex := 0; workerIndex < priorityInboundWorkerCount; workerIndex++ {
		go func() {
			defer s.recoverAgentStreamGoroutine(agentID, "priority-inbound", cancel)
			for {
				select {
				case <-ctx.Done():
					return
				case message := <-ch.priorityInbound:
					if message == nil {
						continue
					}
					if err := s.processPriorityAgentMessageAsync(ctx, ch.priorityResultEffects, ch.priorityAuditEffects, agentID, message); err != nil {
						processErr(err)
						return
					}
				}
			}
		}()
	}
}

// startAuditEffectsLoop drains audit effects published from the priority
// path. On shutdown it flushes any pending effects with a Background ctx so
// the audit trail survives stream cancellation.
func (s *Server) startAuditEffectsLoop(ctx context.Context, cancel context.CancelFunc, agentID string, ch *agentStreamChannels) {
	go func() {
		defer s.recoverAgentStreamGoroutine(agentID, "audit-effects", cancel)
		for {
			select {
			case <-ctx.Done():
				// Drain runs after ctx is cancelled, so reusing it here
				// would immediately abort the audit flush. Background()
				// is the intentional detachment for the shutdown path.
				drainPriorityAuditEffects(ch.priorityAuditEffects, func(actorID string, action string, targetID string, details map[string]any) { //nolint:contextcheck
					s.appendAuditWithContext(context.Background(), actorID, action, targetID, details)
				})
				return
			case effect := <-ch.priorityAuditEffects:
				if effect.action == "" {
					continue
				}
				s.appendAuditWithContext(ctx, effect.actorID, effect.action, effect.targetID, effect.details)
			}
		}
	}()
}

// startResultEffectsLoop drains job-result effects published from the
// priority path. Mirrors startAuditEffectsLoop's shutdown drain semantics.
func (s *Server) startResultEffectsLoop(ctx context.Context, cancel context.CancelFunc, agentID string, ch *agentStreamChannels) {
	go func() {
		defer s.recoverAgentStreamGoroutine(agentID, "result-effects", cancel)
		for {
			select {
			case <-ctx.Done():
				drainPriorityResultEffects(ch.priorityResultEffects, func(agentID string, jobID string, success bool, message string, resultJSON string, observedAt time.Time) { //nolint:contextcheck
					s.recordClientJobResultWithContext(context.Background(), agentID, jobID, success, message, resultJSON, observedAt)
				})
				return
			case effect := <-ch.priorityResultEffects:
				if effect.jobID == "" {
					continue
				}
				s.recordClientJobResultWithContext(
					ctx,
					effect.agentID,
					effect.jobID,
					effect.success,
					effect.message,
					effect.resultJSON,
					effect.observedAt,
				)
			}
		}
	}()
}

// startSnapshotApplyLoop drains agent runtime snapshots and applies them
// against in-memory + batch-write state. Drains pending items on shutdown.
func (s *Server) startSnapshotApplyLoop(ctx context.Context, cancel context.CancelFunc, agentID string, ch *agentStreamChannels, processErr func(error)) {
	go func() {
		defer s.recoverAgentStreamGoroutine(agentID, "snapshot-apply", cancel)
		for {
			select {
			case <-ctx.Done():
				drainRegularSnapshots(ch.regularSnapshots, func(snapshot agentSnapshot) error { //nolint:contextcheck
					return s.applyAgentSnapshotWithContext(context.Background(), snapshot)
				})
				return
			case snapshot := <-ch.regularSnapshots:
				if snapshot.AgentID == "" {
					continue
				}
				if err := s.applyAgentSnapshotWithContext(ctx, snapshot); err != nil {
					processErr(err)
					return
				}
			}
		}
	}()
}

// startRegularInboundLoop drains regular-priority inbound messages and
// dispatches them through processRegularAgentMessage.
func (s *Server) startRegularInboundLoop(ctx context.Context, cancel context.CancelFunc, agentID string, sess agenttransport.AgentSession, ch *agentStreamChannels, processErr func(error)) {
	go func() {
		defer s.recoverAgentStreamGoroutine(agentID, "regular-inbound", cancel)
		for {
			select {
			case <-ctx.Done():
				return
			case message := <-ch.regularInbound:
				if message == nil {
					continue
				}
				if err := s.processRegularAgentMessage(ctx, agentID, sess, ch.regularSnapshots, message); err != nil {
					processErr(err)
					return
				}
			}
		}
	}()
}

// startJobDispatchLoop is the only goroutine that writes back to the agent.
// It runs an initial dispatch + discovery request and then ticks until the
// session is woken (new job ready) or the retry interval fires.
func (s *Server) startJobDispatchLoop(ctx context.Context, cancel context.CancelFunc, agentID string, sess agenttransport.AgentSession, agentSess *agents.Session, ch *agentStreamChannels) {
	go func() {
		defer s.recoverAgentStreamGoroutine(agentID, "job-dispatch", cancel)
		retryTicker := time.NewTicker(jobDispatchRetryInterval)
		defer retryTicker.Stop()

		if err := s.dispatchPendingJobs(ctx, sess, agentID); err != nil {
			nonBlockingSend(ch.dispatchErrors, err)
			return
		}

		// Request a full client list from the agent for user discovery.
		if err := sendClientDataRequest(sess, fmt.Sprintf("discovery-%s-%d", agentID, s.now().Unix())); err != nil {
			s.logger.Error("client discovery request failed", "agent_id", agentID, "error", err)
		}

		for {
			select {
			case <-ctx.Done():
				return
			case <-agentSess.Done:
				return
			case <-agentSess.Wake:
			case <-retryTicker.C:
			}
			if err := s.dispatchPendingJobs(ctx, sess, agentID); err != nil {
				nonBlockingSend(ch.dispatchErrors, err)
				return
			}
		}
	}()
}

// awaitAgentStreamShutdown blocks until one of the dispatch / process /
// receive error channels delivers, then cancels the connection ctx and
// returns the operator-visible error code.
func (s *Server) awaitAgentStreamShutdown(cancel context.CancelFunc, agentID string, ch *agentStreamChannels) error {
	select {
	case err := <-ch.dispatchErrors:
		cancel()
		s.logger.Info(logAgentStreamClosed, "agent_id", agentID, "reason", "dispatch_error", "error", err)
		return err
	case err := <-ch.processErrors:
		cancel()
		s.logger.Info(logAgentStreamClosed, "agent_id", agentID, "reason", "process_error", "error", err)
		return status.Error(codes.Internal, err.Error())
	case err := <-ch.receiveErrors:
		cancel()
		if errors.Is(err, io.EOF) {
			s.logger.Info(logAgentStreamClosed, "agent_id", agentID, "reason", "eof")
			return nil
		}
		s.logger.Info(logAgentStreamClosed, "agent_id", agentID, "reason", "receive_error", "error", err)
		return err
	}
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

func enqueueInboundAgentMessage(
	connectionCtx context.Context,
	priorityInbound chan<- *gatewayrpc.ConnectClientMessage,
	regularInbound chan *gatewayrpc.ConnectClientMessage,
	message *gatewayrpc.ConnectClientMessage,
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
	default:
	}

	return true
}

func (s *Server) dispatchPendingJobs(ctx context.Context, sess agenttransport.AgentSession, agentID string) error {
	pendingJobs := s.pendingJobsForAgent(ctx, agentID)
	if len(pendingJobs) == 0 {
		return nil
	}

	hasMore := len(pendingJobs) > jobDispatchBatchSize
	if hasMore {
		pendingJobs = pendingJobs[:jobDispatchBatchSize]
	}

	for _, job := range pendingJobs {
		s.logger.Debug("job dispatched to agent", "agent_id", agentID, "job_id", job.ID, "action", string(job.Action))
		if err := sess.Send(&gatewayrpc.ConnectServerMessage{
			Body: &gatewayrpc.ConnectServerMessage_Job{
				Job: &gatewayrpc.JobCommand{
					Id:             job.ID,
					Action:         string(job.Action),
					IdempotencyKey: job.IdempotencyKey,
					TargetAgentIds: job.TargetAgentIDs,
					PayloadJson:    job.PayloadJSON,
				},
			},
		}); err != nil {
			return err
		}
		s.markJobDelivered(ctx, agentID, job.ID)
	}

	if hasMore {
		s.notifyAgentSession(agentID)
	}

	return nil
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

// handleSnapshotMessage translates the wire-format snapshot into the internal
// agentSnapshot and enqueues it for the regular processor goroutine. Splitting
// this out keeps processRegularAgentMessage's CC under threshold by isolating
// the per-client and per-clientIP loops behind named helpers.
func (s *Server) handleSnapshotMessage(connectionCtx context.Context, agentID string, regularSnapshots chan agentSnapshot, snap *gatewayrpc.Snapshot) {
	s.logger.Debug(logMessageReceived, "agent_id", agentID, "type", "snapshot")
	observedAt := time.Unix(snap.ObservedAtUnix, 0).UTC()

	instances := convertInstanceSnapshots(snap.Instances)
	clients, usageResolved, usageSkipped := s.convertClientUsageSnapshots(agentID, snap.Clients, observedAt)
	if len(snap.Clients) > 0 {
		s.logger.Info("client usage snapshot received", "agent_id", agentID, "total", len(snap.Clients), "resolved", usageResolved, "skipped", usageSkipped)
	}
	clientIPs, ipResolved, ipSkipped := s.convertClientIPSnapshots(agentID, snap.ClientIps)
	if len(snap.ClientIps) > 0 {
		s.logger.Info("client ip snapshot received", "agent_id", agentID, "total", len(snap.ClientIps), "resolved", ipResolved, "skipped", ipSkipped)
	}

	enqueueRegularSnapshot(connectionCtx, regularSnapshots, agentSnapshot{
		AgentID:                  agentID,
		NodeName:                 snap.NodeName,
		FleetGroupID:             snap.FleetGroupId,
		Version:                  snap.Version,
		ReadOnly:                 snap.ReadOnly,
		Instances:                instances,
		Clients:                  clients,
		HasClients:               snap.HasClientUsage,
		ClientIPs:                clientIPs,
		HasClientIPs:             snap.HasClientIps,
		Runtime:                  snap.Runtime,
		HasRuntime:               snap.Runtime != nil,
		RuntimeDiagnostics:       snap.RuntimeDiagnostics,
		RuntimeSecurityInventory: snap.RuntimeSecurityInventory,
		Metrics:                  snap.Metrics,
		ObservedAt:               observedAt,
	})
}

// convertInstanceSnapshots maps wire instances to the internal type.
func convertInstanceSnapshots(in []*gatewayrpc.InstanceSnapshot) []instanceSnapshot {
	instances := make([]instanceSnapshot, 0, len(in))
	for _, instance := range in {
		instances = append(instances, instanceSnapshot{
			ID:                instance.Id,
			Name:              instance.Name,
			Version:           instance.Version,
			ConfigFingerprint: instance.ConfigFingerprint,
			ConnectedUsers:    int(instance.ConnectedUsers),
			ReadOnly:          instance.ReadOnly,
		})
	}
	return instances
}

// convertClientUsageSnapshots translates wire client usage rows, resolving
// missing client_ids by name. Returns the converted slice plus resolved/skipped
// counters for logging.
func (s *Server) convertClientUsageSnapshots(agentID string, in []*gatewayrpc.ClientUsageSnapshot, observedAt time.Time) ([]clientUsageSnapshot, int, int) {
	clients := make([]clientUsageSnapshot, 0, len(in))
	var resolved, skipped int
	for _, client := range in {
		clientID := client.ClientId
		if clientID == "" && client.ClientName != "" {
			clientID = s.resolveClientIDByName(agentID, client.ClientName)
		}
		if clientID == "" {
			skipped++
			continue
		}
		resolved++
		clients = append(clients, clientUsageSnapshot{
			ClientID:         clientID,
			TrafficUsedBytes: client.TrafficDeltaBytes,
			UniqueIPsUsed:    int(client.UniqueIpsUsed),
			ActiveTCPConns:   int(client.ActiveTcpConns),
			ActiveUniqueIPs:  int(client.ActiveUniqueIps),
			ObservedAt:       observedAt,
			// P2-LOG-06 / L-07: carry the agent-side monotonic snapshot
			// sequence so the CP can dedup replays/restarts.
			Seq: client.Seq,
		})
	}
	return clients, resolved, skipped
}

// convertClientIPSnapshots translates wire client-IP rows, resolving missing
// client_ids by name. Returns the converted slice plus resolved/skipped
// counters for logging.
func (s *Server) convertClientIPSnapshots(agentID string, in []*gatewayrpc.ClientIPSnapshot) ([]clientIPSnapshot, int, int) {
	clientIPs := make([]clientIPSnapshot, 0, len(in))
	var resolved, skipped int
	for _, clientIP := range in {
		ipClientID := clientIP.ClientId
		if ipClientID == "" && clientIP.ClientName != "" {
			ipClientID = s.resolveClientIDByName(agentID, clientIP.ClientName)
		}
		if ipClientID == "" {
			skipped++
			continue
		}
		resolved++
		clientIPs = append(clientIPs, clientIPSnapshot{
			ClientID:  ipClientID,
			ActiveIPs: append([]string(nil), clientIP.ActiveIps...),
		})
	}
	return clientIPs, resolved, skipped
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

func enqueuePriorityResultEffect(
	connectionCtx context.Context,
	priorityResultEffects chan<- jobResultEffect,
	effect jobResultEffect,
) bool {
	if priorityResultEffects == nil {
		return false
	}
	if connectionCtx.Err() != nil {
		return false
	}

	select {
	case <-connectionCtx.Done():
		return false
	case priorityResultEffects <- effect:
		return true
	default:
		return false
	}
}

func enqueuePriorityAuditEffect(
	connectionCtx context.Context,
	priorityAuditEffects chan<- auditEffect,
	effect auditEffect,
) bool {
	if priorityAuditEffects == nil {
		return false
	}
	if connectionCtx.Err() != nil {
		return false
	}

	select {
	case <-connectionCtx.Done():
		return false
	case priorityAuditEffects <- effect:
		return true
	default:
		return false
	}
}

func drainPriorityResultEffects(
	priorityResultEffects <-chan jobResultEffect,
	recordClientJobResult func(agentID string, jobID string, success bool, message string, resultJSON string, observedAt time.Time),
) {
	for {
		select {
		case effect := <-priorityResultEffects:
			if effect.jobID == "" {
				continue
			}
			recordClientJobResult(
				effect.agentID,
				effect.jobID,
				effect.success,
				effect.message,
				effect.resultJSON,
				effect.observedAt,
			)
		default:
			return
		}
	}
}

func drainPriorityAuditEffects(
	priorityAuditEffects <-chan auditEffect,
	appendAudit func(actorID string, action string, targetID string, details map[string]any),
) {
	for {
		select {
		case effect := <-priorityAuditEffects:
			if effect.action == "" {
				continue
			}
			appendAudit(effect.actorID, effect.action, effect.targetID, effect.details)
		default:
			return
		}
	}
}

func enqueueRegularSnapshot(
	connectionCtx context.Context,
	regularSnapshots chan agentSnapshot,
	snapshot agentSnapshot,
) bool {
	if connectionCtx.Err() != nil {
		return false
	}

	select {
	case <-connectionCtx.Done():
		return false
	case regularSnapshots <- snapshot:
		return true
	default:
	}

	// Drop one stale regular snapshot to prioritize the freshest state.
	select {
	case <-regularSnapshots:
	default:
	}

	select {
	case <-connectionCtx.Done():
		return false
	case regularSnapshots <- snapshot:
	default:
	}

	return true
}

func drainRegularSnapshots(
	regularSnapshots <-chan agentSnapshot,
	applyAgentSnapshot func(snapshot agentSnapshot) error,
) {
	for {
		select {
		case snapshot := <-regularSnapshots:
			if snapshot.AgentID == "" {
				continue
			}
			_ = applyAgentSnapshot(snapshot)
		default:
			return
		}
	}
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

func (s *Server) pendingJobsForAgent(ctx context.Context, agentID string) []jobs.Job {
	return s.jobs.PendingForAgent(ctx, agentID, jobDispatchRetryAfter)
}

func (s *Server) markJobDelivered(ctx context.Context, agentID string, jobID string) {
	s.jobs.MarkDelivered(ctx, agentID, jobID, s.now())
}

// Test-only convenience wrappers. Production code drives these flows
// through processPriorityAgentMessageAsync which calls the
// recordJobResultState / recordJobAcknowledgedState halves directly with
// the connection ctx, plus the WithContext audit/result effects helpers.
// The appendAudit / recordClientJobResult helpers used here are part of
// the auth-adjacent legacy cluster that still uses context.Background()
// internally; they will gain ctx in a later remediation pass.
func (s *Server) recordJobAcknowledged(ctx context.Context, agentID string, jobID string, observedAt time.Time) {
	s.recordJobAcknowledgedState(ctx, agentID, jobID, observedAt)
	s.appendAudit(agentID, auditJobsAcknowledged, jobID, map[string]any{}) //nolint:contextcheck
}

func (s *Server) recordJobResult(ctx context.Context, agentID string, jobID string, success bool, message string, resultJSON string, observedAt time.Time) {
	s.recordJobResultState(ctx, agentID, jobID, success, message, resultJSON, observedAt)
	s.recordClientJobResult(agentID, jobID, success, message, resultJSON, observedAt) //nolint:contextcheck
	s.appendAudit(agentID, auditJobsResult, jobID, map[string]any{                      //nolint:contextcheck
		"success": success,
		"message": message,
	})
}

func (s *Server) recordJobResultState(ctx context.Context, agentID string, jobID string, success bool, message string, resultJSON string, observedAt time.Time) {
	if !s.jobs.RecordResult(ctx, agentID, jobID, success, message, resultJSON, observedAt) {
		// P2-LOG-05: the job was evicted (terminal-key TTL, acknowledged
		// expiry worker, or a late result arriving long after the agent's
		// idempotency window) before this result reached the CP. Warn and
		// ignore — the agent's own 2h idempotency cache ensures replay
		// safety, so dropping the late result here is the correct
		// idempotent safety net.
		slog.Warn("job result for unknown or evicted job",
			"agent_id", agentID,
			"job_id", jobID,
			"success", success)
	}
}

func (s *Server) recordJobAcknowledgedState(ctx context.Context, agentID string, jobID string, observedAt time.Time) {
	s.jobs.MarkAcknowledged(ctx, agentID, jobID, observedAt)
}
