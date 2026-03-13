package server

import "time"

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
	token, err := s.enrollment.ConsumeToken(request.Token, now)
	if err != nil {
		return agentEnrollmentResponse{}, err
	}

	s.mu.Lock()
	s.agentSeq++
	agentID := newSequenceID("agent", s.agentSeq)
	s.agents[agentID] = Agent{
		ID:            agentID,
		NodeName:      request.NodeName,
		EnvironmentID: token.EnvironmentID,
		FleetGroupID:  token.FleetGroupID,
		Version:       request.Version,
		LastSeenAt:    now.UTC(),
	}
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
	defer s.mu.Unlock()

	agent := s.agents[snapshot.AgentID]
	agent.NodeName = snapshot.NodeName
	agent.EnvironmentID = snapshot.EnvironmentID
	agent.FleetGroupID = snapshot.FleetGroupID
	agent.Version = snapshot.Version
	agent.ReadOnly = snapshot.ReadOnly
	agent.LastSeenAt = snapshot.ObservedAt.UTC()
	s.agents[snapshot.AgentID] = agent

	for _, instance := range snapshot.Instances {
		s.instances[instance.ID] = Instance{
			ID:                instance.ID,
			AgentID:           snapshot.AgentID,
			Name:              instance.Name,
			Version:           instance.Version,
			ConfigFingerprint: instance.ConfigFingerprint,
			ConnectedUsers:    instance.ConnectedUsers,
			ReadOnly:          instance.ReadOnly,
			UpdatedAt:         snapshot.ObservedAt.UTC(),
		}
	}

	if len(snapshot.Metrics) > 0 {
		s.metricSeq++
		s.metrics = append(s.metrics, MetricSnapshot{
			ID:         newSequenceID("metric", s.metricSeq),
			AgentID:    snapshot.AgentID,
			CapturedAt: snapshot.ObservedAt.UTC(),
			Values:     snapshot.Metrics,
		})
	}

	s.events.publish(eventEnvelope{
		Type: "agents.updated",
		Data: agent,
	})
}
