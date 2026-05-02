package server

import (
	"context"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/eventbus"
	controltelemetry "github.com/lost-coder/panvex/internal/controlplane/telemetry"
)

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
