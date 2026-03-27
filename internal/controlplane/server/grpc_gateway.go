package server

import (
	"context"
	"errors"
	"io"
	"time"

	"github.com/panvex/panvex/internal/controlplane/jobs"
	"github.com/panvex/panvex/internal/gatewayrpc"
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
	if agentID != request.AgentId {
		return nil, status.Error(codes.PermissionDenied, "certificate agent mismatch")
	}

	issued, err := s.authority.issueClientCertificate(agentID, s.now())
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &gatewayrpc.RenewCertificateResponse{
		CertificatePem: issued.CertificatePEM,
		PrivateKeyPem:  issued.PrivateKeyPEM,
		CaPem:          issued.CAPEM,
		ExpiresAtUnix:  issued.ExpiresAt.Unix(),
	}, nil
}

// Connect accepts live heartbeats, snapshots, and job results from one authenticated agent.
func (s *Server) Connect(stream gatewayrpc.AgentGateway_ConnectServer) error {
	agentID, err := authenticatedAgentID(stream.Context())
	if err != nil {
		return err
	}
	session, unregisterSession := s.registerAgentSession(agentID)
	defer unregisterSession()

	connectionCtx, cancelConnection := context.WithCancel(stream.Context())
	defer cancelConnection()

	priorityInbound := make(chan *gatewayrpc.ConnectClientMessage, 32)
	priorityAuditEffects := make(chan auditEffect, priorityAuditQueueCapacity)
	priorityResultEffects := make(chan jobResultEffect, priorityResultEffectQueueCapacity)
	regularInbound := make(chan *gatewayrpc.ConnectClientMessage, 64)
	regularSnapshots := make(chan agentSnapshot, regularSnapshotQueueCapacity)
	receiveErrors := make(chan error, 1)
	dispatchErrors := make(chan error, 1)
	processErrors := make(chan error, 1)
	processErrorAndCancel := func(err error) {
		select {
		case processErrors <- err:
		default:
		}
		cancelConnection()
	}

	go func() {
		for {
			message, err := stream.Recv()
			if err != nil {
				select {
				case receiveErrors <- err:
				default:
				}
				return
			}

			if !enqueueInboundAgentMessage(connectionCtx, priorityInbound, regularInbound, message) {
				return
			}
		}
	}()

	for workerIndex := 0; workerIndex < priorityInboundWorkerCount; workerIndex++ {
		go func() {
			for {
				select {
				case <-connectionCtx.Done():
					return
				case message := <-priorityInbound:
					if message == nil {
						continue
					}
					if err := s.processPriorityAgentMessageAsync(connectionCtx, priorityResultEffects, priorityAuditEffects, agentID, message); err != nil {
						processErrorAndCancel(err)
						return
					}
				}
			}
		}()
	}

	go func() {
		for {
			select {
			case <-connectionCtx.Done():
				drainPriorityAuditEffects(priorityAuditEffects, s.appendAudit)
				return
			case effect := <-priorityAuditEffects:
				if effect.action == "" {
					continue
				}
				s.appendAudit(effect.actorID, effect.action, effect.targetID, effect.details)
			}
		}
	}()

	go func() {
		for {
			select {
			case <-connectionCtx.Done():
				drainPriorityResultEffects(priorityResultEffects, s.recordClientJobResult)
				return
			case effect := <-priorityResultEffects:
				if effect.jobID == "" {
					continue
				}
				s.recordClientJobResult(
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

	go func() {
		for {
			select {
			case <-connectionCtx.Done():
				drainRegularSnapshots(regularSnapshots, s.applyAgentSnapshot)
				return
			case snapshot := <-regularSnapshots:
				if snapshot.AgentID == "" {
					continue
				}
				if err := s.applyAgentSnapshot(snapshot); err != nil {
					processErrorAndCancel(err)
					return
				}
			}
		}
	}()

	go func() {
		for {
			select {
			case <-connectionCtx.Done():
				return
			case message := <-regularInbound:
				if message == nil {
					continue
				}
				if err := s.processRegularAgentMessage(connectionCtx, agentID, regularSnapshots, message); err != nil {
					processErrorAndCancel(err)
					return
				}
			}
		}
	}()

	go func() {
		retryTicker := time.NewTicker(jobDispatchRetryInterval)
		defer retryTicker.Stop()

		if err := s.dispatchPendingJobs(stream, agentID); err != nil {
			select {
			case dispatchErrors <- err:
			default:
			}
			return
		}

		for {
			select {
			case <-connectionCtx.Done():
				return
			case <-session.wake:
			case <-retryTicker.C:
			}

			if err := s.dispatchPendingJobs(stream, agentID); err != nil {
				select {
				case dispatchErrors <- err:
				default:
				}
				return
			}
		}
	}()

	for {
		select {
		case err := <-dispatchErrors:
			cancelConnection()
			return err
		case err := <-processErrors:
			cancelConnection()
			return status.Error(codes.Internal, err.Error())
		case err := <-receiveErrors:
			cancelConnection()
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
	}
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

func (s *Server) dispatchPendingJobs(stream gatewayrpc.AgentGateway_ConnectServer, agentID string) error {
	pendingJobs := s.pendingJobsForAgent(agentID)
	if len(pendingJobs) == 0 {
		return nil
	}

	hasMore := len(pendingJobs) > jobDispatchBatchSize
	if hasMore {
		pendingJobs = pendingJobs[:jobDispatchBatchSize]
	}

	for _, job := range pendingJobs {
		if err := stream.Send(&gatewayrpc.ConnectServerMessage{
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
		s.markJobDelivered(agentID, job.ID)
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
	regularSnapshots chan agentSnapshot,
	message *gatewayrpc.ConnectClientMessage,
) error {
	if hb := message.GetHeartbeat(); hb != nil {
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
		instances := make([]instanceSnapshot, 0, len(snap.Instances))
		for _, instance := range snap.Instances {
			instances = append(instances, instanceSnapshot{
				ID:                instance.Id,
				Name:              instance.Name,
				Version:           instance.Version,
				ConfigFingerprint: instance.ConfigFingerprint,
				ConnectedUsers:    int(instance.ConnectedUsers),
				ReadOnly:          instance.ReadOnly,
			})
		}
		clients := make([]clientUsageSnapshot, 0, len(snap.Clients))
		for _, client := range snap.Clients {
			clients = append(clients, clientUsageSnapshot{
				ClientID:         client.ClientId,
				TrafficUsedBytes: client.TrafficDeltaBytes,
				UniqueIPsUsed:    int(client.UniqueIpsUsed),
				ActiveTCPConns:   int(client.ActiveTcpConns),
				ActiveUniqueIPs:  int(client.ActiveUniqueIps),
				ObservedAt:       time.Unix(snap.ObservedAtUnix, 0).UTC(),
			})
		}
		clientIPs := make([]clientIPSnapshot, 0, len(snap.ClientIps))
		for _, clientIP := range snap.ClientIps {
			clientIPs = append(clientIPs, clientIPSnapshot{
				ClientID:  clientIP.ClientId,
				ActiveIPs: append([]string(nil), clientIP.ActiveIps...),
			})
		}
		enqueueRegularSnapshot(connectionCtx, regularSnapshots, agentSnapshot{
			AgentID:      agentID,
			NodeName:     snap.NodeName,
			FleetGroupID: snap.FleetGroupId,
			Version:      snap.Version,
			ReadOnly:     snap.ReadOnly,
			Instances:    instances,
			Clients:      clients,
			HasClients:   snap.HasClientUsage,
			ClientIPs:    clientIPs,
			HasClientIPs: snap.HasClientIps,
			Runtime:      snap.Runtime,
			HasRuntime:   snap.Runtime != nil,
			Metrics:      snap.Metrics,
			ObservedAt:   time.Unix(snap.ObservedAtUnix, 0).UTC(),
		})
		return nil
	}

	return s.processPriorityAgentMessage(agentID, message)
}

func (s *Server) processPriorityAgentMessage(agentID string, message *gatewayrpc.ConnectClientMessage) error {
	return s.processPriorityAgentMessageAsync(context.Background(), nil, nil, agentID, message)
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
		observedAt := time.Unix(jr.ObservedAtUnix, 0).UTC()
		s.recordJobResultState(
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
			s.recordClientJobResult(agentID, jr.JobId, jr.Success, jr.Message, jr.ResultJson, observedAt)
		}
		details := map[string]any{
			"success": jr.Success,
			"message": jr.Message,
		}
		if !enqueuePriorityAuditEffect(connectionCtx, priorityAuditEffects, auditEffect{
			actorID:  agentID,
			action:   "jobs.result",
			targetID: jr.JobId,
			details:  details,
		}) {
			s.appendAudit(agentID, "jobs.result", jr.JobId, details)
		}
	}
	if ack := message.GetJobAcknowledgement(); ack != nil {
		observedAt := time.Unix(ack.ObservedAtUnix, 0).UTC()
		s.recordJobAcknowledgedState(
			agentID,
			ack.JobId,
			observedAt,
		)
		if !enqueuePriorityAuditEffect(connectionCtx, priorityAuditEffects, auditEffect{
			actorID:  agentID,
			action:   "jobs.acknowledged",
			targetID: ack.JobId,
			details:  map[string]any{},
		}) {
			s.appendAudit(agentID, "jobs.acknowledged", ack.JobId, map[string]any{})
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
	peerInfo, ok := peer.FromContext(ctx)
	if !ok || peerInfo.AuthInfo == nil {
		return "", status.Error(codes.Unauthenticated, "missing peer identity")
	}

	tlsInfo, ok := peerInfo.AuthInfo.(credentials.TLSInfo)
	if !ok {
		return "", status.Error(codes.Unauthenticated, "unexpected peer auth type")
	}
	if len(tlsInfo.State.PeerCertificates) == 0 {
		return "", status.Error(codes.Unauthenticated, "missing client certificate")
	}

	return tlsInfo.State.PeerCertificates[0].Subject.CommonName, nil
}

func (s *Server) pendingJobsForAgent(agentID string) []jobs.Job {
	return s.jobs.PendingForAgent(agentID, jobDispatchRetryAfter)
}

func (s *Server) markJobDelivered(agentID string, jobID string) {
	s.jobs.MarkDelivered(agentID, jobID, s.now())
}

func (s *Server) recordJobAcknowledged(agentID string, jobID string, observedAt time.Time) {
	s.recordJobAcknowledgedState(agentID, jobID, observedAt)
	s.appendAudit(agentID, "jobs.acknowledged", jobID, map[string]any{})
}

func (s *Server) recordJobResult(agentID string, jobID string, success bool, message string, resultJSON string, observedAt time.Time) {
	s.recordJobResultState(agentID, jobID, success, message, resultJSON, observedAt)
	s.recordClientJobResult(agentID, jobID, success, message, resultJSON, observedAt)
	s.appendAudit(agentID, "jobs.result", jobID, map[string]any{
		"success": success,
		"message": message,
	})
}

func (s *Server) recordJobResultState(agentID string, jobID string, success bool, message string, resultJSON string, observedAt time.Time) {
	s.jobs.RecordResult(agentID, jobID, success, message, resultJSON, observedAt)
}

func (s *Server) recordJobAcknowledgedState(agentID string, jobID string, observedAt time.Time) {
	s.jobs.MarkAcknowledged(agentID, jobID, observedAt)
}
