package server

import (
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/agents"
	"github.com/lost-coder/panvex/internal/controlplane/storage"
	"github.com/lost-coder/panvex/internal/gatewayrpc"
)

// runtimeLifecycleState is a thin back-compat wrapper over
// controlplane/agents.RuntimeLifecycleState. Kept so the server package's
// existing call sites and tests continue to compile; new code in the
// server package should call agents.RuntimeLifecycleState directly.
// See P3-ARCH-01a.
func runtimeLifecycleState(snapshot *gatewayrpc.RuntimeSnapshot) string {
	return agents.RuntimeLifecycleState(snapshot)
}

func agentRuntimeFromSnapshot(snapshot *gatewayrpc.RuntimeSnapshot, observedAt time.Time) AgentRuntime {
	dcs := make([]RuntimeDC, 0, len(snapshot.Dcs))
	coverageSum := 0.0
	for _, dc := range snapshot.Dcs {
		dcs = append(dcs, RuntimeDC{
			DC:                 int(dc.Dc),
			AvailableEndpoints: int(dc.AvailableEndpoints),
			AvailablePct:       dc.AvailablePct,
			RequiredWriters:    int(dc.RequiredWriters),
			AliveWriters:       int(dc.AliveWriters),
			CoveragePct:        dc.CoveragePct,
			FreshAliveWriters:  int(dc.FreshAliveWriters),
			FreshCoveragePct:   dc.FreshCoveragePct,
			RTTMs:              dc.RttMs,
			Load:               int(dc.Load),
		})
		coverageSum += dc.CoveragePct
	}
	coveragePct := 0.0
	if len(snapshot.Dcs) > 0 {
		coveragePct = coverageSum / float64(len(snapshot.Dcs))
	}

	var upstreamRows []*gatewayrpc.RuntimeUpstreamRowSnapshot
	var healthyTotal, configuredTotal int32
	var (
		failRatePct5m        float64
		failRateKnown        bool
		connectAttemptTotal  uint64
		connectSuccessTotal  uint64
		connectFailTotal     uint64
		connectFailfastTotal uint64
	)
	if snapshot.Upstreams != nil {
		upstreamRows = snapshot.Upstreams.Rows
		healthyTotal = snapshot.Upstreams.HealthyTotal
		configuredTotal = snapshot.Upstreams.ConfiguredTotal
		failRatePct5m = snapshot.Upstreams.FailRatePct_5M
		failRateKnown = snapshot.Upstreams.FailRateKnown
		connectAttemptTotal = snapshot.Upstreams.ConnectAttemptTotal
		connectSuccessTotal = snapshot.Upstreams.ConnectSuccessTotal
		connectFailTotal = snapshot.Upstreams.ConnectFailTotal
		connectFailfastTotal = snapshot.Upstreams.ConnectFailfastTotal
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
			Weight:             int(upstream.Weight),
			LastCheckAgeSecs:   int(upstream.LastCheckAgeSecs),
			Scopes:             upstream.Scopes,
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
		AcceptingNewConnections:    snapshot.AcceptingNewConnections,
		MERuntimeReady:             snapshot.MeRuntimeReady,
		ME2DCFallbackEnabled:       snapshot.Me2DcFallbackEnabled,
		UseMiddleProxy:             snapshot.UseMiddleProxy,
		StartupStatus:              snapshot.StartupStatus,
		StartupStage:               snapshot.StartupStage,
		StartupProgressPct:         snapshot.StartupProgressPct,
		InitializationStatus:       snapshot.InitializationStatus,
		Degraded:                   snapshot.Degraded,
		LifecycleState:             runtimeLifecycleState(snapshot),
		InitializationStage:        snapshot.InitializationStage,
		InitializationProgressPct:  snapshot.InitializationProgressPct,
		TransportMode:              snapshot.TransportMode,
		CurrentConnections:         int(snapshot.CurrentConnections),
		CurrentConnectionsME:       int(snapshot.CurrentConnectionsMe),
		CurrentConnectionsDirect:   int(snapshot.CurrentConnectionsDirect),
		ActiveUsers:                int(snapshot.ActiveUsers),
		UptimeSeconds:              snapshot.UptimeSeconds,
		ConnectionsTotal:           snapshot.ConnectionsTotal,
		ConnectionsBadTotal:        snapshot.ConnectionsBadTotal,
		HandshakeTimeoutsTotal:     snapshot.HandshakeTimeoutsTotal,
		ConfiguredUsers:            int(snapshot.ConfiguredUsers),
		DCCoveragePct:              coveragePct,
		HealthyUpstreams:           int(healthyTotal),
		TotalUpstreams:             int(configuredTotal),
		FailRatePct5m:              failRatePct5m,
		FailRateKnown:              failRateKnown,
		ConnectAttemptTotal:        connectAttemptTotal,
		ConnectSuccessTotal:        connectSuccessTotal,
		ConnectFailTotal:           connectFailTotal,
		ConnectFailfastTotal:       connectFailfastTotal,
		DCs:                        dcs,
		Upstreams:                  upstreams,
		RecentEvents:               recentEvents,
		SystemLoad:                 systemLoadFromSnapshot(snapshot.SystemLoad),
		MeWritersSummary:           meWritersSummaryFromSnapshot(snapshot.MeWritersSummary),
		TelemtUnreachable:          snapshot.TelemtUnreachable,
		TelemtUnreachableSinceUnix: snapshot.TelemtUnreachableSinceUnix,
		UpdatedAt:                  observedAt.UTC(),
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

func meWritersSummaryFromSnapshot(s *gatewayrpc.RuntimeMeWritersSummary) *RuntimeMeWritersSummary {
	if s == nil {
		return nil
	}
	return &RuntimeMeWritersSummary{
		ConfiguredEndpoints: int(s.ConfiguredEndpoints),
		AvailableEndpoints:  int(s.AvailableEndpoints),
		CoveragePct:         s.CoveragePct,
		FreshAliveWriters:   int(s.FreshAliveWriters),
		FreshCoveragePct:    s.FreshCoveragePct,
		RequiredWriters:     int(s.RequiredWriters),
		AliveWriters:        int(s.AliveWriters),
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

// telemetryWriteUnitForRuntime assembles the telemetry payload for one agent
// snapshot when runtime data is present. Returns the unit ready to enqueue.
func telemetryWriteUnitForRuntime(agent Agent, snapshot agentSnapshot) telemetryWriteUnit {
	rec := runtimeCurrentRecordFromAgent(agent)
	unit := telemetryWriteUnit{
		agentID:   agent.ID,
		runtime:   &rec,
		dcs:       runtimeDCRecordsFromAgent(agent),
		upstreams: runtimeUpstreamRecordsFromAgent(agent),
		events:    runtimeEventRecordsFromAgent(agent),
	}
	if snapshot.RuntimeDiagnostics != nil {
		diag := storage.TelemetryDiagnosticsCurrentRecord{
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
		}
		unit.diagnostics = &diag
	}
	if snapshot.RuntimeSecurityInventory != nil {
		sec := storage.TelemetrySecurityInventoryCurrentRecord{
			AgentID:      agent.ID,
			ObservedAt:   snapshot.ObservedAt.UTC(),
			State:        snapshot.RuntimeSecurityInventory.State,
			StateReason:  snapshot.RuntimeSecurityInventory.StateReason,
			Enabled:      snapshot.RuntimeSecurityInventory.Enabled,
			EntriesTotal: int(snapshot.RuntimeSecurityInventory.EntriesTotal),
			EntriesJSON:  snapshot.RuntimeSecurityInventory.EntriesJson,
		}
		unit.security = &sec
	}
	return unit
}

// enqueueRuntimeBatchWrites pushes runtime telemetry, server-load and DC
// health points for one snapshot. No-op when the snapshot has no runtime.
func (s *Server) enqueueRuntimeBatchWrites(agent Agent, snapshot agentSnapshot) {
	if !snapshot.HasRuntime || snapshot.Runtime == nil {
		return
	}
	s.batchWriter.telemetry.Enqueue(telemetryWriteUnitForRuntime(agent, snapshot))
	s.batchWriter.serverLoad.Enqueue(serverLoadPointFromSnapshot(agent, snapshot))
	for _, dcPoint := range dcHealthPointsFromSnapshot(agent, snapshot) {
		s.batchWriter.dcHealth.Enqueue(dcPoint)
	}
}

// enqueueClientIPHistory pushes one ClientIPHistoryRecord per active IP in
// the snapshot.
func (s *Server) enqueueClientIPHistory(snapshot agentSnapshot) {
	if !snapshot.HasClientIPs {
		return
	}
	now := snapshot.ObservedAt.UTC()
	var ipRecords int
	for _, clientIP := range snapshot.ClientIPs {
		for _, ip := range clientIP.ActiveIPs {
			s.batchWriter.clientIPs.Enqueue(storage.ClientIPHistoryRecord{
				AgentID:   snapshot.AgentID,
				ClientID:  clientIP.ClientID,
				IPAddress: ip,
				FirstSeen: now,
				LastSeen:  now,
			})
			ipRecords++
		}
	}
	if ipRecords > 0 {
		s.logger.Info("client ip records enqueued", "agent_id", snapshot.AgentID, "clients", len(snapshot.ClientIPs), "ip_records", ipRecords)
	}
}

// enqueueAgentSnapshotBatchWrites runs the asynchronous DB-write side of one
// agent snapshot. No-op when the batch writer is disabled.
func (s *Server) enqueueAgentSnapshotBatchWrites(agent Agent, instances []Instance, metric *MetricSnapshot, snapshot agentSnapshot) {
	if s.batchWriter == nil {
		return
	}
	s.batchWriter.agents.Enqueue(agentToRecord(agent))
	for _, instance := range instances {
		s.batchWriter.instances.Enqueue(instanceToRecord(instance))
	}
	if metric != nil {
		s.batchWriter.metricsBuf.Enqueue(metricSnapshotToRecord(*metric))
	}
	s.enqueueRuntimeBatchWrites(agent, snapshot)
	s.enqueueClientIPHistory(snapshot)
}
