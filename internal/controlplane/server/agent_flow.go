package server

import (
	"context"
	"time"

	"github.com/panvex/panvex/internal/controlplane/storage"
)

type agentEnrollmentRequest struct {
	Token    string
	NodeName string
	Version  string
}

type agentEnrollmentResponse struct {
	AgentID        string
	CertificatePEM string
	PrivateKeyPEM  string
	CAPEM          string
	ExpiresAt      time.Time
}

type instanceSnapshot struct {
	ID                string
	Name              string
	Version           string
	ConfigFingerprint string
	ConnectedUsers    int
	ReadOnly          bool
}

type agentSnapshot struct {
	AgentID       string
	NodeName      string
	EnvironmentID string
	FleetGroupID  string
	Version       string
	ReadOnly      bool
	Instances     []instanceSnapshot
	Metrics       map[string]uint64
	ObservedAt    time.Time
}

func (s *Server) enrollAgent(request agentEnrollmentRequest, now time.Time) (agentEnrollmentResponse, error) {
	token, err := s.consumeEnrollmentToken(request.Token, now)
	if err != nil {
		return agentEnrollmentResponse{}, err
	}

	s.mu.Lock()
	s.agentSeq++
	agentID := newSequenceID("agent", s.agentSeq)
	agent := Agent{
		ID:            agentID,
		NodeName:      request.NodeName,
		EnvironmentID: token.EnvironmentID,
		FleetGroupID:  token.FleetGroupID,
		Version:       request.Version,
		LastSeenAt:    now.UTC(),
	}
	s.mu.Unlock()

	if s.store != nil {
		if err := s.store.PutEnvironment(context.Background(), storage.EnvironmentRecord{
			ID:        token.EnvironmentID,
			Name:      token.EnvironmentID,
			CreatedAt: now.UTC(),
		}); err != nil {
			return agentEnrollmentResponse{}, err
		}
		if err := s.store.PutFleetGroup(context.Background(), storage.FleetGroupRecord{
			ID:            token.FleetGroupID,
			EnvironmentID: token.EnvironmentID,
			Name:          token.FleetGroupID,
			CreatedAt:     now.UTC(),
		}); err != nil {
			return agentEnrollmentResponse{}, err
		}
		if err := s.store.PutAgent(context.Background(), agentToRecord(agent)); err != nil {
			return agentEnrollmentResponse{}, err
		}
	}

	s.mu.Lock()
	s.agents[agentID] = agent
	s.mu.Unlock()

	issued, err := s.authority.issueClientCertificate(agentID, now)
	if err != nil {
		return agentEnrollmentResponse{}, err
	}

	s.appendAudit(agentID, "agents.enrolled", agentID, map[string]any{
		"node_name":      request.NodeName,
		"environment_id": token.EnvironmentID,
		"fleet_group_id": token.FleetGroupID,
	})
	s.events.publish(eventEnvelope{
		Type: "agents.enrolled",
		Data: s.agents[agentID],
	})

	return agentEnrollmentResponse{
		AgentID:        agentID,
		CertificatePEM: issued.CertificatePEM,
		PrivateKeyPEM:  issued.PrivateKeyPEM,
		CAPEM:          issued.CAPEM,
		ExpiresAt:      issued.ExpiresAt,
	}, nil
}

func (s *Server) applyAgentSnapshot(snapshot agentSnapshot) {
	s.presence.MarkConnected(snapshot.AgentID, snapshot.ObservedAt)
	s.presence.Heartbeat(snapshot.AgentID, snapshot.ObservedAt)

	s.mu.Lock()
	agent := s.agents[snapshot.AgentID]
	agent.ID = snapshot.AgentID
	agent.NodeName = snapshot.NodeName
	// Enrollment fixes the agent scope. Runtime snapshots may be stale or misconfigured,
	// so they must not move an enrolled agent into a different environment or fleet group.
	if agent.EnvironmentID == "" {
		agent.EnvironmentID = snapshot.EnvironmentID
	}
	if agent.FleetGroupID == "" {
		agent.FleetGroupID = snapshot.FleetGroupID
	}
	agent.Version = snapshot.Version
	agent.ReadOnly = snapshot.ReadOnly
	agent.LastSeenAt = snapshot.ObservedAt.UTC()

	instances := make([]Instance, 0, len(snapshot.Instances))
	for _, instance := range snapshot.Instances {
		instances = append(instances, Instance{
			ID:                instance.ID,
			AgentID:           snapshot.AgentID,
			Name:              instance.Name,
			Version:           instance.Version,
			ConfigFingerprint: instance.ConfigFingerprint,
			ConnectedUsers:    instance.ConnectedUsers,
			ReadOnly:          instance.ReadOnly,
			UpdatedAt:         snapshot.ObservedAt.UTC(),
		})
	}

	var metricSnapshot *MetricSnapshot
	if len(snapshot.Metrics) > 0 {
		s.metricSeq++
		metric := MetricSnapshot{
			ID:         newSequenceID("metric", s.metricSeq),
			AgentID:    snapshot.AgentID,
			CapturedAt: snapshot.ObservedAt.UTC(),
			Values:     snapshot.Metrics,
		}
		metricSnapshot = &metric
	}
	s.mu.Unlock()

	if s.store != nil {
		if err := s.store.PutAgent(context.Background(), agentToRecord(agent)); err != nil {
			panic(err)
		}
		for _, instance := range instances {
			if err := s.store.PutInstance(context.Background(), instanceToRecord(instance)); err != nil {
				panic(err)
			}
		}
		if metricSnapshot != nil {
			if err := s.store.AppendMetricSnapshot(context.Background(), metricSnapshotToRecord(*metricSnapshot)); err != nil {
				panic(err)
			}
		}
	}

	s.mu.Lock()
	s.agents[snapshot.AgentID] = agent
	for _, instance := range instances {
		s.instances[instance.ID] = instance
	}
	if metricSnapshot != nil {
		s.metrics = append(s.metrics, *metricSnapshot)
	}
	s.mu.Unlock()

	s.events.publish(eventEnvelope{
		Type: "agents.updated",
		Data: agent,
	})
}
