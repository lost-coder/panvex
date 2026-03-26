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

	for {
		message, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}

		if hb := message.GetHeartbeat(); hb != nil {
			if err := s.applyAgentSnapshot(agentSnapshot{
				AgentID:      agentID,
				NodeName:     hb.NodeName,
				FleetGroupID: hb.FleetGroupId,
				Version:      hb.Version,
				ReadOnly:     hb.ReadOnly,
				ObservedAt:   time.Unix(hb.ObservedAtUnix, 0).UTC(),
			}); err != nil {
				return status.Error(codes.Internal, err.Error())
			}
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
			if err := s.applyAgentSnapshot(agentSnapshot{
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
			}); err != nil {
				return status.Error(codes.Internal, err.Error())
			}
		}

		if jr := message.GetJobResult(); jr != nil {
			s.recordJobResult(
				agentID,
				jr.JobId,
				jr.Success,
				jr.Message,
				jr.ResultJson,
				time.Unix(jr.ObservedAtUnix, 0).UTC(),
			)
		}

		for _, job := range s.pendingJobsForAgent(agentID) {
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
	allJobs := s.jobs.List()
	result := make([]jobs.Job, 0)
	for _, job := range allJobs {
		if s.isJobDelivered(agentID, job.ID) {
			continue
		}
		for _, target := range job.Targets {
			if target.AgentID != agentID {
				continue
			}
			if target.Status != jobs.TargetStatusQueued {
				break
			}
			result = append(result, job)
			break
		}
	}

	return result
}

func (s *Server) isJobDelivered(agentID string, jobID string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.deliveredJobs[agentID][jobID]
}

func (s *Server) markJobDelivered(agentID string, jobID string) {
	s.jobs.MarkDelivered(agentID, jobID, s.now())

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.deliveredJobs[agentID] == nil {
		s.deliveredJobs[agentID] = make(map[string]bool)
	}
	s.deliveredJobs[agentID][jobID] = true
}

func (s *Server) recordJobResult(agentID string, jobID string, success bool, message string, resultJSON string, observedAt time.Time) {
	s.jobs.RecordResult(agentID, jobID, success, message, resultJSON, observedAt)
	s.recordClientJobResult(agentID, jobID, success, message, resultJSON, observedAt)
	s.appendAudit(agentID, "jobs.result", jobID, map[string]any{
		"success": success,
		"message": message,
	})
}
