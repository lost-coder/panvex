package server

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/lost-coder/panvex/internal/controlplane/agents"
	"github.com/lost-coder/panvex/internal/controlplane/clients"
	"github.com/lost-coder/panvex/internal/controlplane/eventbus"
	"github.com/lost-coder/panvex/internal/controlplane/storage"
	controltelemetry "github.com/lost-coder/panvex/internal/controlplane/telemetry"
	"github.com/lost-coder/panvex/internal/gatewayrpc"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)

// tracer is the package-level OTel tracer used by control-plane hot
// paths (P3-OBS-01). It pulls from the global TracerProvider so when
// tracing is disabled at startup all Start/End calls are free.
var tracer = otel.Tracer("github.com/lost-coder/panvex/internal/controlplane/server")

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
	AgentID                  string
	NodeName                 string
	FleetGroupID             string
	Version                  string
	ReadOnly                 bool
	Instances                []instanceSnapshot
	Clients                  []clientUsageSnapshot
	HasClients               bool
	ClientIPs                []clientIPSnapshot
	HasClientIPs             bool
	Runtime                  *gatewayrpc.RuntimeSnapshot
	HasRuntime               bool
	RuntimeDiagnostics       *gatewayrpc.RuntimeDiagnosticsSnapshot
	RuntimeSecurityInventory *gatewayrpc.RuntimeSecurityInventorySnapshot
	Metrics                  map[string]uint64
	ObservedAt               time.Time
}

// clientUsageSnapshot now lives in controlplane/clients as
// UsageSnapshot. Kept as a server-local alias so the usage-aggregator
// hot path (applyClientUsageSnapshot, grpc_gateway.Connect,
// chaos_test.go) keeps compiling until the rename lands.
type clientUsageSnapshot = clients.UsageSnapshot

type clientIPSnapshot struct {
	ClientID  string
	ActiveIPs []string
}

func (s *Server) enrollAgent(request agentEnrollmentRequest, now time.Time) (agentEnrollmentResponse, error) {
	return s.enrollAgentWithContext(context.Background(), request, now)
}

func (s *Server) enrollAgentWithContext(ctx context.Context, request agentEnrollmentRequest, now time.Time) (agentEnrollmentResponse, error) {
	// P3-OBS-01: agent enrollment is a low-volume, high-value path
	// (token consumption + cert issuance + first DB write). Wrap it in
	// a custom span so operators can diagnose slow enrollments even
	// when the HTTP-level span is lost across reverse proxies.
	ctx, span := tracer.Start(ctx, "agents.enroll")
	defer span.End()

	token, err := s.consumeEnrollmentTokenWithContext(ctx, request.Token, now)
	if err != nil {
		span.RecordError(err)
		return agentEnrollmentResponse{}, err
	}
	span.SetAttributes(
		attribute.String("panvex.node_name", request.NodeName),
		attribute.String("panvex.agent_version", request.Version),
		attribute.String("panvex.fleet_group_id", token.FleetGroupID),
	)

	// Agent IDs are UUIDv7 instead of the old monotonic "agent_<N>" scheme
	// because the old counter was process-local and reset on CP restart,
	// which could re-issue an ID whose prior owner still held a valid 30-day
	// client certificate (P1-SEC-05 / C5 CN collision).
	id7, err := uuid.NewV7()
	if err != nil {
		span.RecordError(err)
		return agentEnrollmentResponse{}, fmt.Errorf("generate agent id: %w", err)
	}
	agentID := id7.String()
	span.SetAttributes(attribute.String("panvex.agent_id", agentID))

	s.mu.Lock()
	agent := Agent{
		ID:           agentID,
		NodeName:     request.NodeName,
		FleetGroupID: token.FleetGroupID,
		Version:      request.Version,
		LastSeenAt:   now.UTC(),
	}
	s.mu.Unlock()

	if s.store != nil {
		// Fleet-group existence is resolved/auto-created at token-issue
		// time (see issueEnrollmentTokenWithContext), so by the time an
		// agent bootstraps with a consumed token the group row is
		// guaranteed to exist. If a group was deleted between issue and
		// bootstrap, the FK on agents.fleet_group_id surfaces the
		// failure here.
		if err := s.store.PutAgent(ctx, agentToRecord(agent)); err != nil {
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

	// Record certificate dates in the agent so they are persisted and
	// returned via the API instead of being fabricated on the frontend.
	certIssuedAt := now.UTC()
	certExpiresAt := issued.ExpiresAt.UTC()
	s.mu.Lock()
	storedAgent := s.agents[agentID]
	storedAgent.CertIssuedAt = &certIssuedAt
	storedAgent.CertExpiresAt = &certExpiresAt
	storedAgent.CertSerial = issued.Serial
	s.agents[agentID] = storedAgent
	s.mu.Unlock()
	if s.batchWriter != nil {
		s.batchWriter.agents.Enqueue(agentToRecord(storedAgent))
	}
	// Q4.U-S-04: persist the freshly-issued cert serial as the
	// authoritative pin. Best-effort — a write failure is logged but
	// does not block enrollment, since the in-memory pin in
	// storedAgent already protects this process lifetime.
	if s.store != nil {
		if err := s.store.UpdateAgentCertSerial(ctx, agentID, issued.Serial); err != nil {
			s.logger.Warn("persist agent cert serial failed", "agent_id", agentID, "error", err)
		}
	}

	s.appendAuditWithContext(ctx, agentID, "agents.enrolled", agentID, map[string]any{
		"node_name":      request.NodeName,
		"fleet_group_id": token.FleetGroupID,
	})
	s.events.Publish(eventbus.Event{
		Type: "agents.enrolled",
		Data: storedAgent,
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

// updateAgentRecordFromSnapshot folds the snapshot's identity / runtime
// fields into the existing agent record under s.mu, refreshing the
// initialization-watch cooldown table along the way. Returns the new agent
// value (still owned by the caller, who is responsible for committing it
// back into s.agents).
//
// Caller must hold s.mu.
func (s *Server) updateAgentRecordFromSnapshot(snapshot agentSnapshot) Agent {
	agent := s.agents[snapshot.AgentID]
	agent.ID = snapshot.AgentID
	// Enrollment fixes the node name. Subsequent heartbeats must not
	// overwrite it: operators rename nodes via the panel API, and the
	// agent's reported name (often defaulted to the system hostname) would
	// otherwise revert the rename on the next snapshot.
	if agent.NodeName == "" {
		agent.NodeName = snapshot.NodeName
	}
	// Enrollment fixes the agent group. Runtime snapshots may be stale or
	// misconfigured, so they must not move an enrolled agent into a
	// different fleet group.
	if agent.FleetGroupID == "" {
		agent.FleetGroupID = snapshot.FleetGroupID
	}
	agent.Version = snapshot.Version
	agent.ReadOnly = snapshot.ReadOnly
	agent.LastSeenAt = snapshot.ObservedAt.UTC()
	if snapshot.HasRuntime && snapshot.Runtime != nil {
		previousRuntime := agent.Runtime
		agent.Runtime = agentRuntimeFromSnapshot(snapshot.Runtime, snapshot.ObservedAt)
		s.refreshInitializationWatchCooldown(snapshot, agent.Runtime, previousRuntime)
	}
	return agent
}

// refreshInitializationWatchCooldown maintains the per-agent cooldown so the
// "initialization watch" UI signal does not flap on every heartbeat once the
// agent has finished initializing. Caller must hold s.mu.
func (s *Server) refreshInitializationWatchCooldown(snapshot agentSnapshot, current, previous AgentRuntime) {
	currentNeedsWatch := runtimeNeedsInitializationWatch(current)
	previousNeedsWatch := runtimeNeedsInitializationWatch(previous)
	switch {
	case currentNeedsWatch:
		delete(s.initializationWatchCooldowns, snapshot.AgentID)
	case previousNeedsWatch:
		s.initializationWatchCooldowns[snapshot.AgentID] = snapshot.ObservedAt.UTC().Add(telemetryInitializationWatchCooldown)
	default:
		expiresAt := s.initializationWatchCooldowns[snapshot.AgentID]
		if !expiresAt.IsZero() && !expiresAt.After(snapshot.ObservedAt.UTC()) {
			delete(s.initializationWatchCooldowns, snapshot.AgentID)
		}
	}
}

// instancesFromSnapshot projects the snapshot's instances into the in-memory
// Instance shape. Pure function — does no map mutation.
func instancesFromSnapshot(snapshot agentSnapshot) []Instance {
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
	return instances
}

// commitInstancesLocked replaces the per-agent slice of instances with the
// freshly-snapshotted set, pruning any previously-known instances that are
// absent from `instances` so s.instances does not leak stale entries
// (P2-LOG-09 / L-04). Caller must hold s.mu.
func (s *Server) commitInstancesLocked(agentID string, instances []Instance) {
	liveIDs := make(map[string]struct{}, len(instances))
	for _, instance := range instances {
		liveIDs[instance.ID] = struct{}{}
	}
	for id, entry := range s.instances {
		if entry.AgentID != agentID {
			continue
		}
		if _, ok := liveIDs[id]; ok {
			continue
		}
		delete(s.instances, id)
	}
	for _, instance := range instances {
		s.instances[instance.ID] = instance
	}
}

// applyFallbackStateTransitionLocked classifies the agent's operating mode
// from runtime flags and updates the in-memory fallbackEnteredAt map. The
// 30-min escalation timer tracks ME-pool downtime (the underlying outage),
// not the agent's me2dc_fallback_enabled flag, which can flap independently
// while the ME pool is still down. The mode→action table is therefore:
//
//	ModeFallback: stamp+enqueue Put on first entry; idempotent on repeat.
//	ModeMeDown:   keep any existing timestamp (ME is still down — flag flap
//	              alone must not reset the escalation timer). No enqueue.
//	ModeME:       ME pool is healthy again — clear timestamp + enqueue Delete.
//	ModeDirect:   fallback is no longer relevant — clear timestamp + Delete.
//
// Caller must hold s.mu.
func (s *Server) applyFallbackStateTransitionLocked(agent Agent) {
	mode := controltelemetry.ClassifyMode(controltelemetry.SeverityInput{
		UseMiddleProxy:       agent.Runtime.UseMiddleProxy,
		MERuntimeReady:       agent.Runtime.MERuntimeReady,
		ME2DCFallbackEnabled: agent.Runtime.ME2DCFallbackEnabled,
	})
	_, hadPrev := s.fallbackEnteredAt[agent.ID]
	switch mode {
	case controltelemetry.ModeFallback:
		if !hadPrev {
			now := time.Now().UTC()
			s.fallbackEnteredAt[agent.ID] = now
			if s.batchWriter != nil {
				s.batchWriter.EnqueueFallbackPut(agent.ID, now)
			}
		}
	case controltelemetry.ModeMeDown:
		// ME is still down. Operator may have flipped the fallback flag off,
		// but the underlying outage continues — keep the original entered-at
		// so severity escalation crosses the 30-min boundary on time.
	case controltelemetry.ModeME, controltelemetry.ModeDirect:
		if hadPrev {
			delete(s.fallbackEnteredAt, agent.ID)
			if s.batchWriter != nil {
				s.batchWriter.EnqueueFallbackDelete(agent.ID)
			}
		}
	}
}

// commitClientSnapshotsLocked applies any client usage / IP snapshot data
// under s.clientsMu. Caller must hold s.mu.
func (s *Server) commitClientSnapshotsLocked(ctx context.Context, snapshot agentSnapshot) {
	if !snapshot.HasClients && !snapshot.HasClientIPs {
		return
	}
	s.clientsMu.Lock()
	defer s.clientsMu.Unlock()
	if snapshot.HasClients {
		s.applyClientUsageSnapshot(ctx, snapshot.AgentID, snapshot.Clients)
	}
	if snapshot.HasClientIPs {
		s.applyClientIPSnapshot(snapshot.AgentID, snapshot.ClientIPs)
	}
}

// commitMetricSnapshotLocked appends a new metric sample to the in-memory
// ring buffer (capped at maxInMemoryMetricSnapshots) and returns it for
// downstream batch-writer enqueueing. Returns nil when the snapshot carries
// no metrics. Caller must hold s.mu.
func (s *Server) commitMetricSnapshotLocked(snapshot agentSnapshot) *MetricSnapshot {
	if len(snapshot.Metrics) == 0 {
		return nil
	}
	s.metricsAuditMu.Lock()
	defer s.metricsAuditMu.Unlock()
	s.metricSeq++
	metric := MetricSnapshot{
		ID:         newSequenceID("metric", s.metricSeq),
		AgentID:    snapshot.AgentID,
		CapturedAt: snapshot.ObservedAt.UTC(),
		Values:     snapshot.Metrics,
	}
	if len(s.metrics) < maxInMemoryMetricSnapshots {
		s.metrics = append(s.metrics, metric)
	} else {
		copy(s.metrics, s.metrics[1:])
		s.metrics[len(s.metrics)-1] = metric
	}
	return &metric
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

func (s *Server) applyAgentSnapshotWithContext(ctx context.Context, snapshot agentSnapshot) error {
	s.logger.Debug("agent heartbeat applied", "agent_id", snapshot.AgentID, "node", snapshot.NodeName)

	// Lock section: build all state objects AND commit to in-memory maps
	// atomically. No DB I/O happens under the locks.
	// Lock ordering: mu -> clientsMu -> metricsAuditMu.
	s.mu.Lock()
	// Drop snapshots from agents that have been deregistered. Without this
	// guard, an in-flight heartbeat that arrives between the operator's
	// DELETE and the gRPC stream tear-down would re-create the in-memory
	// agent record (snapshot.AgentID is unconditionally written into
	// s.agents below), and the agent would resurrect itself in the panel
	// — typically with a "DEGRADED" badge as its telemetry caught up.
	if _, revoked := s.revokedAgentIDs[snapshot.AgentID]; revoked {
		s.mu.Unlock()
		s.logger.Info("dropping snapshot from revoked agent", "agent_id", snapshot.AgentID)
		return nil
	}
	agent := s.updateAgentRecordFromSnapshot(snapshot)
	instances := instancesFromSnapshot(snapshot)
	s.agents[snapshot.AgentID] = agent
	s.commitInstancesLocked(snapshot.AgentID, instances)
	s.commitClientSnapshotsLocked(ctx, snapshot)
	metricSnapshot := s.commitMetricSnapshotLocked(snapshot)
	s.applyFallbackStateTransitionLocked(agent)
	s.mu.Unlock()

	// Enqueue all DB writes asynchronously via the batch writer. No DB I/O
	// blocks the caller — the background flush goroutine handles persistence.
	s.enqueueAgentSnapshotBatchWrites(agent, instances, metricSnapshot, snapshot)

	// P2-LOG-12 / L-05: only Heartbeat on every snapshot. MarkConnected is
	// called exactly once per gRPC stream open (see Connect in
	// grpc_gateway.go) so the recorded connectedAt reflects the real
	// stream-open moment instead of being rewritten to "now" by every
	// heartbeat snapshot, which masked short disconnects.
	s.presence.Heartbeat(snapshot.AgentID, snapshot.ObservedAt)

	s.events.Publish(eventbus.Event{
		Type: "agents.updated",
		Data: agent,
	})

	return nil
}

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
		FailRatePct5m:             failRatePct5m,
		FailRateKnown:             failRateKnown,
		ConnectAttemptTotal:       connectAttemptTotal,
		ConnectSuccessTotal:       connectSuccessTotal,
		ConnectFailTotal:          connectFailTotal,
		ConnectFailfastTotal:      connectFailfastTotal,
		DCs:                       dcs,
		Upstreams:                 upstreams,
		RecentEvents:              recentEvents,
		SystemLoad:                systemLoadFromSnapshot(snapshot.SystemLoad),
		MeWritersSummary:          meWritersSummaryFromSnapshot(snapshot.MeWritersSummary),
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

func (s *Server) applyClientUsageSnapshot(ctx context.Context, agentID string, clients []clientUsageSnapshot) {
	applyTrafficDelta := s.shouldApplyClientUsageDelta(agentID, clients)

	seen, toPersist := s.mergeClientUsageBatch(agentID, clients, applyTrafficDelta)
	s.persistClientUsageRecords(ctx, toPersist)
	s.zeroLiveGaugesForUntouchedClients(agentID, seen)
}

// shouldApplyClientUsageDelta evaluates the seq-dedup rules (P2-LOG-06 / L-07)
// and returns whether traffic deltas in the batch should be accumulated.
// As a side effect it advances or rewinds s.lastUsageSeq for this agent.
//
// Dedup rules:
//   - seq == 0 on the wire: legacy agent, fall back to unconditional
//     accumulation (old behavior).
//   - seq <= lastSeen: duplicate / replay after stream reconnect — skip
//     deltas, but still refresh live gauges.
//   - lastSeen > 0 && seq == 1: agent restarted with zero-ed counters — treat
//     as baseline, skip deltas, just record new seq.
//   - otherwise: accept and accumulate.
func (s *Server) shouldApplyClientUsageDelta(agentID string, clients []clientUsageSnapshot) bool {
	batchSeq := firstNonZeroSeq(clients)
	if batchSeq == 0 {
		return true
	}
	lastSeen := s.lastUsageSeq[agentID]
	switch {
	case lastSeen > 0 && batchSeq == 1:
		// Agent restart: counters reset to zero on the agent side, so the
		// incoming "delta" is actually an absolute baseline. Skip addition to
		// avoid double-counting and rewind the CP-side cursor so subsequent
		// in-order deltas (seq 2, 3, ...) are accepted.
		s.lastUsageSeq[agentID] = 1
		return false
	case batchSeq <= lastSeen:
		// Duplicate or stale (in-flight retry, out-of-order reconnect). Live
		// gauges may still be refreshed below, but do not re-accumulate.
		return false
	default:
		s.lastUsageSeq[agentID] = batchSeq
		return true
	}
}

// firstNonZeroSeq returns the first usage.Seq encountered across the batch,
// or zero if every entry omits the field (legacy wire format).
func firstNonZeroSeq(clients []clientUsageSnapshot) uint64 {
	for _, usage := range clients {
		if usage.Seq != 0 {
			return usage.Seq
		}
	}
	return 0
}

// mergeClientUsageBatch updates s.clientUsage for every entry in the batch
// and returns the seen-set plus the list of records to persist write-through.
//
// Persisted backing lets clientUsage survive a panel restart — otherwise each
// adopted client would snap to zero bytes and wait for the next agent tick,
// which only carries a single polling interval worth of delta.
func (s *Server) mergeClientUsageBatch(agentID string, clients []clientUsageSnapshot, applyTrafficDelta bool) (map[string]struct{}, []storage.ClientUsageRecord) {
	seen := make(map[string]struct{}, len(clients))
	toPersist := make([]storage.ClientUsageRecord, 0, len(clients))
	for _, usage := range clients {
		seen[usage.ClientID] = struct{}{}
		if s.clientUsage[usage.ClientID] == nil {
			s.clientUsage[usage.ClientID] = make(map[string]clientUsageSnapshot)
		}
		current := s.clientUsage[usage.ClientID][agentID]
		current.ClientID = usage.ClientID
		if applyTrafficDelta {
			current.TrafficUsedBytes += usage.TrafficUsedBytes
		}
		current.UniqueIPsUsed = usage.UniqueIPsUsed
		current.ActiveTCPConns = usage.ActiveTCPConns
		current.ActiveUniqueIPs = usage.ActiveUniqueIPs
		current.ObservedAt = usage.ObservedAt
		s.clientUsage[usage.ClientID][agentID] = current
		toPersist = append(toPersist, storage.ClientUsageRecord{
			ClientID:         usage.ClientID,
			AgentID:          agentID,
			TrafficUsedBytes: current.TrafficUsedBytes,
			UniqueIPsUsed:    current.UniqueIPsUsed,
			ActiveTCPConns:   current.ActiveTCPConns,
			ActiveUniqueIPs:  current.ActiveUniqueIPs,
			LastSeq:          s.lastUsageSeq[agentID],
			ObservedAt:       current.ObservedAt,
		})
	}
	return seen, toPersist
}

// persistClientUsageRecords write-throughs the merged usage state to storage
// (when configured). Per-row failures are logged but never abort the loop.
func (s *Server) persistClientUsageRecords(ctx context.Context, toPersist []storage.ClientUsageRecord) {
	if s.store == nil {
		return
	}
	for _, rec := range toPersist {
		if err := s.store.UpsertClientUsage(ctx, rec); err != nil {
			s.logger.Warn("persist client_usage",
				"client_id", rec.ClientID, "agent_id", rec.AgentID, "error", err)
		}
	}
}

// zeroLiveGaugesForUntouchedClients zeros the live connection/IP gauges of
// any client that this agent did not include in the snapshot. Accumulated
// traffic is preserved — deleting the entry would lose the seeded/historical
// total.
func (s *Server) zeroLiveGaugesForUntouchedClients(agentID string, seen map[string]struct{}) {
	for clientID, usageByAgent := range s.clientUsage {
		current, ok := usageByAgent[agentID]
		if !ok {
			continue
		}
		if _, ok := seen[clientID]; ok {
			continue
		}
		current.ActiveTCPConns = 0
		current.ActiveUniqueIPs = 0
		usageByAgent[agentID] = current
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
		current.UniqueIPsUsed = len(snapshot.ActiveIPs)
		current.ActiveUniqueIPs = len(snapshot.ActiveIPs)
		usageByAgent[agentID] = current
	}
}
