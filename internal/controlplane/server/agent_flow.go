package server

import (
	"context"
	"time"

	"github.com/panvex/panvex/internal/controlplane/storage"
	"github.com/panvex/panvex/internal/gatewayrpc"
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
	AgentID      string
	NodeName     string
	FleetGroupID string
	Version      string
	ReadOnly     bool
	Instances    []instanceSnapshot
	Clients      []clientUsageSnapshot
	HasClients   bool
	Runtime      gatewayrpc.RuntimeSnapshot
	HasRuntime   bool
	Metrics      map[string]uint64
	ObservedAt   time.Time
}

type clientUsageSnapshot struct {
	ClientID         string
	TrafficUsedBytes uint64
	UniqueIPsUsed    int
	ActiveTCPConns   int
	ObservedAt       time.Time
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
		ID:           agentID,
		NodeName:     request.NodeName,
		FleetGroupID: token.FleetGroupID,
		Version:      request.Version,
		LastSeenAt:   now.UTC(),
	}
	s.mu.Unlock()

	if s.store != nil {
		if token.FleetGroupID != "" {
			if err := s.store.PutFleetGroup(context.Background(), storage.FleetGroupRecord{
				ID:        token.FleetGroupID,
				Name:      token.FleetGroupID,
				CreatedAt: now.UTC(),
			}); err != nil {
				return agentEnrollmentResponse{}, err
			}
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

func (s *Server) applyAgentSnapshot(snapshot agentSnapshot) error {
	s.presence.MarkConnected(snapshot.AgentID, snapshot.ObservedAt)
	s.presence.Heartbeat(snapshot.AgentID, snapshot.ObservedAt)

	s.mu.Lock()
	agent := s.agents[snapshot.AgentID]
	agent.ID = snapshot.AgentID
	agent.NodeName = snapshot.NodeName
	// Enrollment fixes the agent group. Runtime snapshots may be stale or misconfigured,
	// so they must not move an enrolled agent into a different fleet group.
	if agent.FleetGroupID == "" {
		agent.FleetGroupID = snapshot.FleetGroupID
	}
	agent.Version = snapshot.Version
	agent.ReadOnly = snapshot.ReadOnly
	agent.LastSeenAt = snapshot.ObservedAt.UTC()
	if snapshot.HasRuntime {
		agent.Runtime = agentRuntimeFromSnapshot(snapshot.Runtime, snapshot.ObservedAt)
	}

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
			return err
		}
		for _, instance := range instances {
			if err := s.store.PutInstance(context.Background(), instanceToRecord(instance)); err != nil {
				return err
			}
		}
		if metricSnapshot != nil {
			if err := s.store.AppendMetricSnapshot(context.Background(), metricSnapshotToRecord(*metricSnapshot)); err != nil {
				return err
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
	if snapshot.HasClients {
		s.applyClientUsageSnapshot(snapshot.AgentID, snapshot.Clients)
	}
	s.mu.Unlock()

	s.events.publish(eventEnvelope{
		Type: "agents.updated",
		Data: agent,
	})

	return nil
}

func agentRuntimeFromSnapshot(snapshot gatewayrpc.RuntimeSnapshot, observedAt time.Time) AgentRuntime {
	dcs := make([]RuntimeDC, 0, len(snapshot.DCs))
	coveragePct := 0.0
	for index, dc := range snapshot.DCs {
		dcs = append(dcs, RuntimeDC{
			DC:                 dc.DC,
			AvailableEndpoints: dc.AvailableEndpoints,
			AvailablePct:       dc.AvailablePct,
			RequiredWriters:    dc.RequiredWriters,
			AliveWriters:       dc.AliveWriters,
			CoveragePct:        dc.CoveragePct,
			RTTMs:              dc.RTTMs,
			Load:               dc.Load,
		})
		if index == 0 || dc.CoveragePct < coveragePct {
			coveragePct = dc.CoveragePct
		}
	}

	upstreams := make([]RuntimeUpstream, 0, len(snapshot.Upstreams.Rows))
	for _, upstream := range snapshot.Upstreams.Rows {
		upstreams = append(upstreams, RuntimeUpstream{
			UpstreamID:         upstream.UpstreamID,
			RouteKind:          upstream.RouteKind,
			Address:            upstream.Address,
			Healthy:            upstream.Healthy,
			Fails:              upstream.Fails,
			EffectiveLatencyMs: upstream.EffectiveLatencyMs,
		})
	}

	recentEvents := make([]RuntimeEvent, 0, len(snapshot.RecentEvents))
	for _, event := range snapshot.RecentEvents {
		recentEvents = append(recentEvents, RuntimeEvent{
			Sequence:      event.Sequence,
			TimestampUnix: event.TimestampUnix,
			EventType:     event.EventType,
			Context:       event.Context,
		})
	}

	return AgentRuntime{
		AcceptingNewConnections:   snapshot.AcceptingNewConnections,
		MERuntimeReady:            snapshot.MERuntimeReady,
		ME2DCFallbackEnabled:      snapshot.ME2DCFallbackEnabled,
		UseMiddleProxy:            snapshot.UseMiddleProxy,
		StartupStatus:             snapshot.StartupStatus,
		StartupStage:              snapshot.StartupStage,
		StartupProgressPct:        snapshot.StartupProgressPct,
		InitializationStatus:      snapshot.InitializationStatus,
		Degraded:                  snapshot.Degraded,
		InitializationStage:       snapshot.InitializationStage,
		InitializationProgressPct: snapshot.InitializationProgressPct,
		TransportMode:             snapshot.TransportMode,
		CurrentConnections:        snapshot.CurrentConnections,
		CurrentConnectionsME:      snapshot.CurrentConnectionsME,
		CurrentConnectionsDirect:  snapshot.CurrentConnectionsDirect,
		ActiveUsers:               snapshot.ActiveUsers,
		UptimeSeconds:             snapshot.UptimeSeconds,
		ConnectionsTotal:          snapshot.ConnectionsTotal,
		ConnectionsBadTotal:       snapshot.ConnectionsBadTotal,
		HandshakeTimeoutsTotal:    snapshot.HandshakeTimeoutsTotal,
		ConfiguredUsers:           snapshot.ConfiguredUsers,
		DCCoveragePct:             coveragePct,
		HealthyUpstreams:          snapshot.Upstreams.HealthyTotal,
		TotalUpstreams:            snapshot.Upstreams.ConfiguredTotal,
		DCs:                       dcs,
		Upstreams:                 upstreams,
		RecentEvents:              recentEvents,
		UpdatedAt:                 observedAt.UTC(),
	}
}

func (s *Server) applyClientUsageSnapshot(agentID string, clients []clientUsageSnapshot) {
	for clientID, agentUsage := range s.clientUsage {
		delete(agentUsage, agentID)
		if len(agentUsage) == 0 {
			delete(s.clientUsage, clientID)
		}
	}

	for _, usage := range clients {
		if s.clientUsage[usage.ClientID] == nil {
			s.clientUsage[usage.ClientID] = make(map[string]clientUsageSnapshot)
		}
		s.clientUsage[usage.ClientID][agentID] = usage
	}
}
