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

// Enroll consumes a bootstrap token and returns an agent identity plus mTLS material.
func (s *Server) Enroll(_ context.Context, request *gatewayrpc.EnrollRequest) (*gatewayrpc.EnrollResponse, error) {
	response, err := s.enrollAgent(agentEnrollmentRequest{
		Token:    request.Token,
		NodeName: request.NodeName,
		Version:  request.Version,
	}, s.now())
	if err != nil {
		return nil, status.Error(codes.PermissionDenied, err.Error())
	}

	return &gatewayrpc.EnrollResponse{
		AgentID:        response.AgentID,
		CertificatePEM: response.CertificatePEM,
		PrivateKeyPEM:  response.PrivateKeyPEM,
		CAPEM:          response.CAPEM,
		ExpiresAtUnix:  response.ExpiresAt.Unix(),
	}, nil
}

// RenewCertificate rotates the short-lived mTLS material for an authenticated agent.
func (s *Server) RenewCertificate(ctx context.Context, request *gatewayrpc.RenewCertificateRequest) (*gatewayrpc.RenewCertificateResponse, error) {
	agentID, err := authenticatedAgentID(ctx)
	if err != nil {
		return nil, err
	}
	if agentID != request.AgentID {
		return nil, status.Error(codes.PermissionDenied, "certificate agent mismatch")
	}

	issued, err := s.authority.issueClientCertificate(agentID, s.now())
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &gatewayrpc.RenewCertificateResponse{
		CertificatePEM: issued.CertificatePEM,
		PrivateKeyPEM:  issued.PrivateKeyPEM,
		CAPEM:          issued.CAPEM,
		ExpiresAtUnix:  issued.ExpiresAt.Unix(),
	}, nil
}

// Connect accepts live heartbeats, snapshots, and job results from one authenticated agent.
func (s *Server) Connect(stream gatewayrpc.Gateway_ConnectServer) error {
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

		if message.Heartbeat != nil {
			s.applyAgentSnapshot(agentSnapshot{
				AgentID:      agentID,
				NodeName:     message.Heartbeat.NodeName,
				FleetGroupID: message.Heartbeat.FleetGroupID,
				Version:      message.Heartbeat.Version,
				ReadOnly:     message.Heartbeat.ReadOnly,
				ObservedAt:   time.Unix(message.Heartbeat.ObservedAtUnix, 0).UTC(),
			})
		}

		if message.Snapshot != nil {
			instances := make([]instanceSnapshot, 0, len(message.Snapshot.Instances))
			for _, instance := range message.Snapshot.Instances {
				instances = append(instances, instanceSnapshot{
					ID:                instance.ID,
					Name:              instance.Name,
					Version:           instance.Version,
					ConfigFingerprint: instance.ConfigFingerprint,
					ConnectedUsers:    instance.ConnectedUsers,
					ReadOnly:          instance.ReadOnly,
				})
			}
			clients := make([]clientUsageSnapshot, 0, len(message.Snapshot.Clients))
			for _, client := range message.Snapshot.Clients {
				clients = append(clients, clientUsageSnapshot{
					ClientID:         client.ClientID,
					TrafficUsedBytes: client.TrafficUsedBytes,
					UniqueIPsUsed:    int(client.UniqueIPsUsed),
					ActiveTCPConns:   int(client.ActiveTCPConns),
					ObservedAt:       time.Unix(message.Snapshot.ObservedAtUnix, 0).UTC(),
				})
			}
			s.applyAgentSnapshot(agentSnapshot{
				AgentID:      agentID,
				NodeName:     message.Snapshot.NodeName,
				FleetGroupID: message.Snapshot.FleetGroupID,
				Version:      message.Snapshot.Version,
				ReadOnly:     message.Snapshot.ReadOnly,
				Instances:    instances,
				Clients:      clients,
				HasClients:   true,
				Metrics:      message.Snapshot.Metrics,
				ObservedAt:   time.Unix(message.Snapshot.ObservedAtUnix, 0).UTC(),
			})
		}

		if message.JobResult != nil {
			s.recordJobResult(
				agentID,
				message.JobResult.JobID,
				message.JobResult.Success,
				message.JobResult.Message,
				message.JobResult.ResultJSON,
				time.Unix(message.JobResult.ObservedAtUnix, 0).UTC(),
			)
		}

		for _, job := range s.pendingJobsForAgent(agentID) {
			if err := stream.Send(&gatewayrpc.ConnectServerMessage{
				Job: &gatewayrpc.JobCommand{
					ID:             job.ID,
					Action:         string(job.Action),
					IdempotencyKey: job.IdempotencyKey,
					TargetAgentIDs: job.TargetAgentIDs,
					PayloadJSON:    job.PayloadJSON,
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
