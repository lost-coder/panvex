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
	ClientIPs    []clientIPSnapshot
	HasClientIPs bool
	Runtime      *gatewayrpc.RuntimeSnapshot
	HasRuntime   bool
	Metrics      map[string]uint64
	ObservedAt   time.Time
}

type clientUsageSnapshot struct {
	ClientID         string
	TrafficUsedBytes uint64
	UniqueIPsUsed    int
	ActiveTCPConns   int
	ActiveUniqueIPs  int
	ObservedAt       time.Time
}

type clientIPSnapshot struct {
	ClientID  string
	ActiveIPs []string
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

	if s.store != nil {
		if token.FleetGroupID != "" {
			if err := s.store.PutFleetGroup(context.Background(), storage.FleetGroupRecord{
				ID:        token.FleetGroupID,
				Name:      token.FleetGroupID,
				CreatedAt: now.UTC(),
			}); err != nil {
				s.mu.Unlock()
				return agentEnrollmentResponse{}, err
			}
		}
		if err := s.store.PutAgent(context.Background(), agentToRecord(agent)); err != nil {
			s.mu.Unlock()
			return agentEnrollmentResponse{}, err
		}
	}

	s.agents[agentID] = agent
	agentEvent := agent
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
		Data: agentEvent,
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
	if snapshot.HasRuntime && snapshot.Runtime != nil {
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

	if s.store != nil {
		if err := s.store.PutAgent(context.Background(), agentToRecord(agent)); err != nil {
			s.mu.Unlock()
			return err
		}
		for _, instance := range instances {
			if err := s.store.PutInstance(context.Background(), instanceToRecord(instance)); err != nil {
				s.mu.Unlock()
				return err
			}
		}
		if metricSnapshot != nil {
			if err := s.store.AppendMetricSnapshot(context.Background(), metricSnapshotToRecord(*metricSnapshot)); err != nil {
				s.mu.Unlock()
				return err
			}
		}
	}

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
	if snapshot.HasClientIPs {
		s.applyClientIPSnapshot(snapshot.AgentID, snapshot.ClientIPs)
	}
	s.mu.Unlock()

	s.events.publish(eventEnvelope{
		Type: "agents.updated",
		Data: agent,
	})

	return nil
}

func runtimeLifecycleState(snapshot *gatewayrpc.RuntimeSnapshot) string {
	switch {
	case snapshot == nil:
		return "unknown"
	case snapshot.Degraded:
		return "degraded"
	case snapshot.InitializationStatus != "" && snapshot.InitializationStatus != "ready":
		return snapshot.InitializationStatus
	case snapshot.StartupStatus != "" && snapshot.StartupStatus != "ready":
		return snapshot.StartupStatus
	case !snapshot.AcceptingNewConnections || !snapshot.MeRuntimeReady:
		return "starting"
	default:
		return "ready"
	}
}

func agentRuntimeFromSnapshot(snapshot *gatewayrpc.RuntimeSnapshot, observedAt time.Time) AgentRuntime {
	dcs := make([]RuntimeDC, 0, len(snapshot.Dcs))
	coveragePct := 0.0
	for index, dc := range snapshot.Dcs {
		dcs = append(dcs, RuntimeDC{
			DC:                 int(dc.Dc),
			AvailableEndpoints: int(dc.AvailableEndpoints),
			AvailablePct:       dc.AvailablePct,
			RequiredWriters:    int(dc.RequiredWriters),
			AliveWriters:       int(dc.AliveWriters),
			CoveragePct:        dc.CoveragePct,
			RTTMs:              dc.RttMs,
			Load:               int(dc.Load),
		})
		if index == 0 || dc.CoveragePct < coveragePct {
			coveragePct = dc.CoveragePct
		}
	}

	var upstreamRows []*gatewayrpc.RuntimeUpstreamRowSnapshot
	var healthyTotal, configuredTotal int32
	if snapshot.Upstreams != nil {
		upstreamRows = snapshot.Upstreams.Rows
		healthyTotal = snapshot.Upstreams.HealthyTotal
		configuredTotal = snapshot.Upstreams.ConfiguredTotal
	}
	upstreams := make([]RuntimeUpstream, 0, len(upstreamRows))
	for _, upstream := range upstreamRows {
		upstreams = append(upstreams, RuntimeUpstream{
			UpstreamID:         int(upstream.UpstreamId),
			RouteKind:          upstream.RouteKind,
			Address:            upstream.Address,
			Healthy:            upstream.Healthy,
			Fails:              int(upstream.Fails),
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
		MERuntimeReady:            snapshot.MeRuntimeReady,
		ME2DCFallbackEnabled:      snapshot.Me2DcFallbackEnabled,
		UseMiddleProxy:            snapshot.UseMiddleProxy,
		StartupStatus:             snapshot.StartupStatus,
		StartupStage:              snapshot.StartupStage,
		StartupProgressPct:        snapshot.StartupProgressPct,
		InitializationStatus:      snapshot.InitializationStatus,
		Degraded:                  snapshot.Degraded,
		LifecycleState:            runtimeLifecycleState(snapshot),
		InitializationStage:       snapshot.InitializationStage,
		InitializationProgressPct: snapshot.InitializationProgressPct,
		TransportMode:             snapshot.TransportMode,
		CurrentConnections:        int(snapshot.CurrentConnections),
		CurrentConnectionsME:      int(snapshot.CurrentConnectionsMe),
		CurrentConnectionsDirect:  int(snapshot.CurrentConnectionsDirect),
		ActiveUsers:               int(snapshot.ActiveUsers),
		UptimeSeconds:             snapshot.UptimeSeconds,
		ConnectionsTotal:          snapshot.ConnectionsTotal,
		ConnectionsBadTotal:       snapshot.ConnectionsBadTotal,
		HandshakeTimeoutsTotal:    snapshot.HandshakeTimeoutsTotal,
		ConfiguredUsers:           int(snapshot.ConfiguredUsers),
		DCCoveragePct:             coveragePct,
		HealthyUpstreams:          int(healthyTotal),
		TotalUpstreams:            int(configuredTotal),
		DCs:                       dcs,
		Upstreams:                 upstreams,
		RecentEvents:              recentEvents,
		UpdatedAt:                 observedAt.UTC(),
	}
}

func (s *Server) applyClientUsageSnapshot(agentID string, clients []clientUsageSnapshot) {
	seen := make(map[string]struct{}, len(clients))
	for _, usage := range clients {
		seen[usage.ClientID] = struct{}{}
		if s.clientUsage[usage.ClientID] == nil {
			s.clientUsage[usage.ClientID] = make(map[string]clientUsageSnapshot)
		}
		current := s.clientUsage[usage.ClientID][agentID]
		current.ClientID = usage.ClientID
		current.TrafficUsedBytes += usage.TrafficUsedBytes
		current.UniqueIPsUsed = usage.UniqueIPsUsed
		current.ActiveTCPConns = usage.ActiveTCPConns
		current.ActiveUniqueIPs = usage.ActiveUniqueIPs
		current.ObservedAt = usage.ObservedAt
		s.clientUsage[usage.ClientID][agentID] = current
	}

	for clientID, usageByAgent := range s.clientUsage {
		if _, ok := usageByAgent[agentID]; !ok {
			continue
		}
		if _, ok := seen[clientID]; ok {
			continue
		}
		delete(usageByAgent, agentID)
		if len(usageByAgent) == 0 {
			delete(s.clientUsage, clientID)
		}
	}
}

func (s *Server) applyClientIPSnapshot(agentID string, clients []clientIPSnapshot) {
	for _, snapshot := range clients {
		usageByAgent := s.clientUsage[snapshot.ClientID]
		if usageByAgent == nil {
			usageByAgent = make(map[string]clientUsageSnapshot)
			s.clientUsage[snapshot.ClientID] = usageByAgent
		}
		current := usageByAgent[agentID]
		current.ClientID = snapshot.ClientID
		current.ActiveUniqueIPs = len(snapshot.ActiveIPs)
		usageByAgent[agentID] = current
	}
}
