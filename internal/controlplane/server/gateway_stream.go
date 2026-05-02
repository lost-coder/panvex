package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/agents"
	"github.com/lost-coder/panvex/internal/controlplane/agenttransport"
	"github.com/lost-coder/panvex/internal/gatewayrpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

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
