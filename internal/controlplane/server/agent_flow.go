package server

import (
	"context"
	"log"
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
	RuntimeDiagnostics *gatewayrpc.RuntimeDiagnosticsSnapshot
	RuntimeSecurityInventory *gatewayrpc.RuntimeSecurityInventorySnapshot
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
	return s.enrollAgentWithContext(context.Background(), request, now)
}

func (s *Server) enrollAgentWithContext(ctx context.Context, request agentEnrollmentRequest, now time.Time) (agentEnrollmentResponse, error) {
	token, err := s.consumeEnrollmentTokenWithContext(ctx, request.Token, now)
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
			if err := s.store.PutFleetGroup(ctx, storage.FleetGroupRecord{
				ID:        token.FleetGroupID,
				Name:      token.FleetGroupID,
				CreatedAt: now.UTC(),
			}); err != nil {
				return agentEnrollmentResponse{}, err
			}
		}
		if err := s.store.PutAgent(ctx, agentToRecord(agent)); err != nil {
			return agentEnrollmentResponse{}, err
		}
	}

	s.mu.Lock()
	s.agents[agentID] = agent
	agentEvent := agent
	s.mu.Unlock()

	issued, err := s.authority.issueClientCertificate(agentID, now)
	if err != nil {
		return agentEnrollmentResponse{}, err
	}

	s.appendAuditWithContext(ctx, agentID, "agents.enrolled", agentID, map[string]any{
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
	return s.applyAgentSnapshotWithContext(context.Background(), snapshot)
}

func (s *Server) applyAgentSnapshotWithContext(ctx context.Context, snapshot agentSnapshot) error {
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
		previousRuntime := agent.Runtime
		agent.Runtime = agentRuntimeFromSnapshot(snapshot.Runtime, snapshot.ObservedAt)
		currentNeedsWatch := runtimeNeedsInitializationWatch(agent.Runtime)
		previousNeedsWatch := runtimeNeedsInitializationWatch(previousRuntime)
		if currentNeedsWatch {
			delete(s.initializationWatchCooldowns, snapshot.AgentID)
		} else if previousNeedsWatch && !currentNeedsWatch {
			s.initializationWatchCooldowns[snapshot.AgentID] = snapshot.ObservedAt.UTC().Add(telemetryInitializationWatchCooldown)
		} else if expiresAt := s.initializationWatchCooldowns[snapshot.AgentID]; !expiresAt.IsZero() && !expiresAt.After(snapshot.ObservedAt.UTC()) {
			delete(s.initializationWatchCooldowns, snapshot.AgentID)
		}
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
		if err := s.store.PutAgent(ctx, agentToRecord(agent)); err != nil {
			return err
		}
		if snapshot.HasRuntime && snapshot.Runtime != nil {
			if err := s.store.PutTelemetryRuntimeCurrent(ctx, runtimeCurrentRecordFromAgent(agent)); err != nil {
				return err
			}
			if err := s.store.ReplaceTelemetryRuntimeDCs(ctx, agent.ID, runtimeDCRecordsFromAgent(agent)); err != nil {
				return err
			}
			if err := s.store.ReplaceTelemetryRuntimeUpstreams(ctx, agent.ID, runtimeUpstreamRecordsFromAgent(agent)); err != nil {
				return err
			}
			if err := s.store.AppendTelemetryRuntimeEvents(ctx, agent.ID, runtimeEventRecordsFromAgent(agent)); err != nil {
				return err
			}
			if snapshot.RuntimeDiagnostics != nil {
				if err := s.store.PutTelemetryDiagnosticsCurrent(ctx, storage.TelemetryDiagnosticsCurrentRecord{
					AgentID:             agent.ID,
					ObservedAt:          snapshot.ObservedAt.UTC(),
					State:               snapshot.RuntimeDiagnostics.State,
					StateReason:         snapshot.RuntimeDiagnostics.StateReason,
					SystemInfoJSON:      snapshot.RuntimeDiagnostics.SystemInfoJson,
					EffectiveLimitsJSON: snapshot.RuntimeDiagnostics.EffectiveLimitsJson,
					SecurityPostureJSON: snapshot.RuntimeDiagnostics.SecurityPostureJson,
					MinimalAllJSON:      snapshot.RuntimeDiagnostics.MinimalAllJson,
					MEPoolJSON:          snapshot.RuntimeDiagnostics.MePoolJson,
					DcsJSON:             snapshot.RuntimeDiagnostics.DcsJson,
				}); err != nil {
					return err
				}
			}
			if snapshot.RuntimeSecurityInventory != nil {
				if err := s.store.PutTelemetrySecurityInventoryCurrent(ctx, storage.TelemetrySecurityInventoryCurrentRecord{
					AgentID:      agent.ID,
					ObservedAt:   snapshot.ObservedAt.UTC(),
					State:        snapshot.RuntimeSecurityInventory.State,
					StateReason:  snapshot.RuntimeSecurityInventory.StateReason,
					Enabled:      snapshot.RuntimeSecurityInventory.Enabled,
					EntriesTotal: int(snapshot.RuntimeSecurityInventory.EntriesTotal),
					EntriesJSON:  snapshot.RuntimeSecurityInventory.EntriesJson,
				}); err != nil {
					return err
				}
			}
		}
		for _, instance := range instances {
			if err := s.store.PutInstance(ctx, instanceToRecord(instance)); err != nil {
				return err
			}
		}
		if metricSnapshot != nil {
			if err := s.store.AppendMetricSnapshot(ctx, metricSnapshotToRecord(*metricSnapshot)); err != nil {
				return err
			}
		}
		if snapshot.HasRuntime && snapshot.Runtime != nil {
			if err := s.store.AppendServerLoadPoint(ctx, serverLoadPointFromSnapshot(agent, snapshot)); err != nil {
				log.Printf("timeseries: append server load failed for agent %s: %v", agent.ID, err)
			}
			for _, dcPoint := range dcHealthPointsFromSnapshot(agent, snapshot) {
				if err := s.store.AppendDCHealthPoint(ctx, dcPoint); err != nil {
					log.Printf("timeseries: append dc health failed for agent %s dc %d: %v", agent.ID, dcPoint.DC, err)
					break
				}
			}
		}
	}

	s.mu.Lock()
	s.agents[snapshot.AgentID] = agent
	for _, instance := range instances {
		s.instances[instance.ID] = instance
	}
	if metricSnapshot != nil {
		if len(s.metrics) < maxInMemoryMetricSnapshots {
			s.metrics = append(s.metrics, *metricSnapshot)
		} else {
			copy(s.metrics, s.metrics[1:])
			s.metrics[len(s.metrics)-1] = *metricSnapshot
		}
	}
	if snapshot.HasClients {
		s.applyClientUsageSnapshot(snapshot.AgentID, snapshot.Clients)
	}
	if snapshot.HasClientIPs {
		s.applyClientIPSnapshot(snapshot.AgentID, snapshot.ClientIPs)
	}
	s.mu.Unlock()

	s.presence.MarkConnected(snapshot.AgentID, snapshot.ObservedAt)
	s.presence.Heartbeat(snapshot.AgentID, snapshot.ObservedAt)

	if snapshot.HasClientIPs && s.store != nil {
		now := snapshot.ObservedAt.UTC()
		var ipErrors int
		for _, clientIP := range snapshot.ClientIPs {
			for _, ip := range clientIP.ActiveIPs {
				if err := s.store.UpsertClientIPHistory(ctx, storage.ClientIPHistoryRecord{
					AgentID:   snapshot.AgentID,
					ClientID:  clientIP.ClientID,
					IPAddress: ip,
					FirstSeen: now,
					LastSeen:  now,
				}); err != nil {
					ipErrors++
					if ipErrors == 1 {
						log.Printf("timeseries: upsert client ip history failed for agent %s: %v", snapshot.AgentID, err)
					}
				}
			}
		}
		if ipErrors > 1 {
			log.Printf("timeseries: %d additional client ip upsert errors suppressed for agent %s", ipErrors-1, snapshot.AgentID)
		}
	}

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
		SystemLoad:                systemLoadFromSnapshot(snapshot.SystemLoad),
		UpdatedAt:                 observedAt.UTC(),
	}
}

func systemLoadFromSnapshot(load *gatewayrpc.RuntimeSystemLoadSnapshot) RuntimeSystemLoad {
	if load == nil {
		return RuntimeSystemLoad{}
	}
	return RuntimeSystemLoad{
		CPUUsagePct:      load.CpuUsagePct,
		MemoryUsedBytes:  load.MemoryUsedBytes,
		MemoryTotalBytes: load.MemoryTotalBytes,
		MemoryUsagePct:   load.MemoryUsagePct,
		DiskUsedBytes:    load.DiskUsedBytes,
		DiskTotalBytes:   load.DiskTotalBytes,
		DiskUsagePct:     load.DiskUsagePct,
		Load1M:           load.Load_1M,
		Load5M:           load.Load_5M,
		Load15M:          load.Load_15M,
		NetBytesSent:     load.NetBytesSent,
		NetBytesRecv:     load.NetBytesRecv,
	}
}

func serverLoadPointFromSnapshot(agent Agent, snapshot agentSnapshot) storage.ServerLoadPointRecord {
	rt := snapshot.Runtime
	record := storage.ServerLoadPointRecord{
		AgentID:                agent.ID,
		CapturedAt:             snapshot.ObservedAt.UTC(),
		ConnectionsTotal:       agent.Runtime.ConnectionsTotal,
		ConnectionsBadTotal:    agent.Runtime.ConnectionsBadTotal,
		HandshakeTimeoutsTotal: agent.Runtime.HandshakeTimeoutsTotal,
		HealthyUpstreams:       agent.Runtime.HealthyUpstreams,
		TotalUpstreams:         agent.Runtime.TotalUpstreams,
		DCCoverageMinPct:       agent.Runtime.DCCoveragePct,
		DCCoverageAvgPct:       agent.Runtime.DCCoveragePct,
		SampleCount:            1,
	}

	if agg := rt.GetAggregatedSystemLoad(); agg != nil {
		record.CPUPctAvg = agg.CpuPctAvg
		record.CPUPctMax = agg.CpuPctMax
		record.MemPctAvg = agg.MemPctAvg
		record.MemPctMax = agg.MemPctMax
		record.DiskPctAvg = agg.DiskPctAvg
		record.DiskPctMax = agg.DiskPctMax
		record.Load1M = agg.Load_1M
		record.Load5M = agg.Load_5M
		record.Load15M = agg.Load_15M
		record.NetBytesSent = agg.NetBytesSent
		record.NetBytesRecv = agg.NetBytesRecv
	} else if sl := rt.GetSystemLoad(); sl != nil {
		record.CPUPctAvg = sl.CpuUsagePct
		record.CPUPctMax = sl.CpuUsagePct
		record.MemPctAvg = sl.MemoryUsagePct
		record.MemPctMax = sl.MemoryUsagePct
		record.DiskPctAvg = sl.DiskUsagePct
		record.DiskPctMax = sl.DiskUsagePct
		record.Load1M = sl.Load_1M
		record.Load5M = sl.Load_5M
		record.Load15M = sl.Load_15M
		record.NetBytesSent = sl.NetBytesSent
		record.NetBytesRecv = sl.NetBytesRecv
	}

	if agg := rt.GetAggregatedConnections(); agg != nil {
		record.ConnectionsAvg = int(agg.ConnectionsAvg)
		record.ConnectionsMax = int(agg.ConnectionsMax)
		record.ConnectionsMEAvg = int(agg.ConnectionsMeAvg)
		record.ConnectionsDirectAvg = int(agg.ConnectionsDirectAvg)
		record.ActiveUsersAvg = int(agg.ActiveUsersAvg)
		record.ActiveUsersMax = int(agg.ActiveUsersMax)
	} else {
		record.ConnectionsAvg = agent.Runtime.CurrentConnections
		record.ConnectionsMax = agent.Runtime.CurrentConnections
		record.ConnectionsMEAvg = agent.Runtime.CurrentConnectionsME
		record.ConnectionsDirectAvg = agent.Runtime.CurrentConnectionsDirect
		record.ActiveUsersAvg = agent.Runtime.ActiveUsers
		record.ActiveUsersMax = agent.Runtime.ActiveUsers
	}

	if rt.GetAggregationSamples() > 0 {
		record.SampleCount = int(rt.GetAggregationSamples())
	}

	// Compute DC coverage from aggregated DCs if available.
	if aggDCs := rt.GetAggregatedDcs(); len(aggDCs) > 0 {
		minCov := aggDCs[0].CoveragePctMin
		avgSum := 0.0
		for _, dc := range aggDCs {
			if dc.CoveragePctMin < minCov {
				minCov = dc.CoveragePctMin
			}
			avgSum += dc.CoveragePctAvg
		}
		record.DCCoverageMinPct = minCov
		record.DCCoverageAvgPct = avgSum / float64(len(aggDCs))
	}

	return record
}

func dcHealthPointsFromSnapshot(agent Agent, snapshot agentSnapshot) []storage.DCHealthPointRecord {
	rt := snapshot.Runtime
	capturedAt := snapshot.ObservedAt.UTC()

	if aggDCs := rt.GetAggregatedDcs(); len(aggDCs) > 0 {
		points := make([]storage.DCHealthPointRecord, 0, len(aggDCs))
		for _, dc := range aggDCs {
			points = append(points, storage.DCHealthPointRecord{
				AgentID:         agent.ID,
				CapturedAt:      capturedAt,
				DC:              int(dc.Dc),
				CoveragePctAvg:  dc.CoveragePctAvg,
				CoveragePctMin:  dc.CoveragePctMin,
				RTTMsAvg:        dc.RttMsAvg,
				RTTMsMax:        dc.RttMsMax,
				AliveWritersMin: int(dc.AliveWritersMin),
				RequiredWriters: int(dc.RequiredWriters),
				LoadMax:         int(dc.LoadMax),
				SampleCount:     int(rt.GetAggregationSamples()),
			})
		}
		return points
	}

	points := make([]storage.DCHealthPointRecord, 0, len(rt.GetDcs()))
	for _, dc := range rt.GetDcs() {
		points = append(points, storage.DCHealthPointRecord{
			AgentID:         agent.ID,
			CapturedAt:      capturedAt,
			DC:              int(dc.Dc),
			CoveragePctAvg:  dc.CoveragePct,
			CoveragePctMin:  dc.CoveragePct,
			RTTMsAvg:        dc.RttMs,
			RTTMsMax:        dc.RttMs,
			AliveWritersMin: int(dc.AliveWriters),
			RequiredWriters: int(dc.RequiredWriters),
			LoadMax:         int(dc.Load),
			SampleCount:     1,
		})
	}
	return points
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
