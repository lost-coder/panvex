package server

import (
	"context"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/eventbus"
	controltelemetry "github.com/lost-coder/panvex/internal/controlplane/telemetry"
)

// updateAgentRecordFromSnapshot folds the snapshot's identity / runtime
// fields into the existing agent record, refreshing the initialization-watch
// cooldown table along the way. Returns the new agent value (still owned by
// the caller, who is responsible for committing it back via
// live.ApplySnapshot). The previous value is read from the live store; the
// cooldown table is mutated under s.mu, so callers hold s.mu.
//
// Caller must hold s.mu (for the cooldown table). The live store has its own
// lock; live.Get is taken under s.mu, preserving the s.mu -> live ordering.
func (s *Server) updateAgentRecordFromSnapshot(snapshot agentSnapshot) Agent {
	agent, _ := s.live.Get(snapshot.AgentID)
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
	// IN-H6: a partial snapshot may carry blanked version / read_only, so
	// preserve the last-known values instead of flapping the UI to empty
	// during a transient telemt sub-endpoint outage. LastSeenAt still
	// advances — the agent is demonstrably alive.
	if !snapshot.Partial {
		agent.Version = snapshot.Version
		agent.ReadOnly = snapshot.ReadOnly
	}
	// P3-3.2 (аудит #25b): все три "last seen"-величины — панельные часы.
	// presence.Heartbeat уже штампуется s.now() (M-5, см. applyAgentSnapshot);
	// LastSeenAt и Runtime.UpdatedAt обязаны использовать ТЕ ЖЕ часы, иначе
	// при skew агент одновременно "online" (presence), "stale" (freshness)
	// и "last seen 10 min ago" (LastSeenAt). Агентский ObservedAt остаётся
	// в Runtime.ReportedObservedAt для диагностики.
	receivedAt := s.now().UTC()
	agent.LastSeenAt = receivedAt
	if snapshot.HasRuntime && snapshot.Runtime != nil {
		previousRuntime := agent.Runtime
		next := agentRuntimeFromSnapshot(snapshot.Runtime, receivedAt)
		next.ReportedObservedAt = snapshot.ObservedAt.UTC()
		if snapshot.Partial && next.UptimeSeconds == 0 {
			// uptime comes from the slow /v1/system/info fetch; preserve the
			// last-known value rather than reporting a regressed 0.
			next.UptimeSeconds = previousRuntime.UptimeSeconds
		}
		agent.Runtime = next
		s.refreshInitializationWatchCooldown(snapshot, agent.Runtime, previousRuntime, receivedAt)
	}
	return agent
}

// updateAgentIdentity applies an identity-only mutation to an agent in the
// live store, PRESERVING the agent's instance set. live.ApplySnapshot replaces
// the instance set, so the current instances must be re-supplied — this helper
// re-reads them via live.InstancesForAgent so callers can never accidentally
// pass nil and wipe an agent's instances. Returns the updated agent and true,
// or (zero, false) if the agent is not present.
//
// The helper only touches s.live, which has its own lock; it does not require
// s.mu. Callers that hold s.mu do so to order the surrounding read-modify-write
// (and any batchWriter enqueue) against other s.mu holders, not for this call.
func (s *Server) updateAgentIdentity(id string, mutate func(*Agent)) (Agent, bool) {
	agent, ok := s.live.Get(id)
	if !ok {
		return Agent{}, false
	}
	mutate(&agent)
	s.live.ApplySnapshot(id, agent, s.live.InstancesForAgent(id))
	return agent, true
}

// refreshInitializationWatchCooldown maintains the per-agent cooldown so the
// "initialization watch" UI signal does not flap on every heartbeat once the
// agent has finished initializing. Caller must hold s.mu. `now` — панельные
// часы приёма снапшота (P3-3.2): cooldown сравнивается с ними же.
func (s *Server) refreshInitializationWatchCooldown(snapshot agentSnapshot, current, previous AgentRuntime, now time.Time) {
	currentNeedsWatch := runtimeNeedsInitializationWatch(current)
	previousNeedsWatch := runtimeNeedsInitializationWatch(previous)
	switch {
	case currentNeedsWatch:
		delete(s.initializationWatchCooldowns, snapshot.AgentID)
	case previousNeedsWatch:
		s.initializationWatchCooldowns[snapshot.AgentID] = now.Add(telemetryInitializationWatchCooldown)
	default:
		expiresAt := s.initializationWatchCooldowns[snapshot.AgentID]
		if !expiresAt.IsZero() && !expiresAt.After(now) {
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
			ManagedConfigHash: instance.ManagedConfigHash,
			ManagedConfigJSON: instance.ManagedConfigJSON,
			Connections:       instance.Connections,
			ReadOnly:          instance.ReadOnly,
			UpdatedAt:         snapshot.ObservedAt.UTC(),
		})
	}
	return instances
}

// carryForwardObservedConfig keeps the last non-empty ManagedConfigJSON when the
// agent omitted it (delta-gated: the full JSON is only sent on the snapshot where
// the observed config changed, empty otherwise). Matches instances by ID. The
// hash is sent every snapshot, so it is not carried forward.
func carryForwardObservedConfig(next []Instance, prev []Instance) {
	byID := make(map[string]Instance, len(prev))
	for _, p := range prev {
		byID[p.ID] = p
	}
	for i := range next {
		if next[i].ManagedConfigJSON == "" {
			if p, ok := byID[next[i].ID]; ok {
				next[i].ManagedConfigJSON = p.ManagedConfigJSON
			}
		}
	}
}

// applyFallbackStateTransition classifies the agent's operating mode from
// runtime flags and updates the in-memory fallback tracker. The 30-min
// escalation timer tracks ME-pool downtime (the underlying outage), not the
// agent's me2dc_fallback_enabled flag, which can flap independently while the
// ME pool is still down. The mode→action table is therefore:
//
//	ModeFallback: stamp+enqueue Put on first entry; idempotent on repeat.
//	ModeMeDown:   keep any existing timestamp (ME is still down — flag flap
//	              alone must not reset the escalation timer). No enqueue.
//	ModeME:       ME pool is healthy again — clear timestamp + enqueue Delete.
//	ModeDirect:   fallback is no longer relevant — clear timestamp + Delete.
//
// The in-memory set/clear and the hadPrev transition-edge read go through the
// fallback tracker, which owns its own lock; this function does NOT itself
// require s.mu. It is called from applyAgentSnapshot under s.mu so the
// classification observes the same agent value committed to the live store in
// that critical section; the tracker call nests harmlessly inside (s.mu ->
// tracker ordering, the tracker never calls back).
func (s *Server) applyFallbackStateTransition(agent Agent) {
	mode := controltelemetry.ClassifyMode(controltelemetry.SeverityInput{
		UseMiddleProxy:             agent.Runtime.UseMiddleProxy,
		MERuntimeReady:             agent.Runtime.MERuntimeReady,
		ME2DCFallbackEnabled:       agent.Runtime.ME2DCFallbackEnabled,
		TelemtUnreachable:          agent.Runtime.TelemtUnreachable,
		TelemtUnreachableSinceUnix: agent.Runtime.TelemtUnreachableSinceUnix,
	})
	_, hadPrev := s.fallback.Get(agent.ID)
	switch mode {
	case controltelemetry.ModeFallback:
		if !hadPrev {
			now := time.Now().UTC()
			s.fallback.Set(agent.ID, now)
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
			s.fallback.Clear(agent.ID)
			if s.batchWriter != nil {
				s.batchWriter.EnqueueFallbackDelete(agent.ID)
			}
		}
	}
}

// applyTelemtReachabilityTransition detects the Telemt unreachable→reachable
// edge for an agent and asks its live stream session to re-request a full
// client list. Discovery is otherwise only requested at stream open and on the
// periodic timer; a Telemt that recovered without a stream reconnect would
// otherwise leave the panel's discovered-clients view stale for that node until
// the next periodic refresh. Best-effort: if the agent has no live session
// (e.g. reverse-mode not yet connected) the request is simply dropped and the
// periodic refresh covers it.
//
// Called OUTSIDE s.mu (the agent value is a committed copy by then) so waking
// the writer goroutine never happens while the snapshot critical section is
// held.
func (s *Server) applyTelemtReachabilityTransition(ctx context.Context, agent Agent) {
	if s.telemtReach.Observe(agent.ID, agent.Runtime.TelemtUnreachable) {
		s.logger.InfoContext(ctx, "telemt reachable again; requesting client re-discovery", "agent_id", agent.ID)
		s.sessions.RequestRediscovery(agent.ID)
	}
}

// commitClientSnapshotsLocked applies any client usage snapshot data through
// the clients.Service mirror. Caller must hold s.mu.
//
// IP-only snapshots (HasClientIPs without HasClients) used to seed a
// zero-valued placeholder usage entry in the Server-owned maps; that
// placeholder was value-neutral (a zero entry is JSON-identical to an absent
// key and contributes 0 to traffic/gauges) and was dropped with the Server
// maps in C1. The subsequent usage tick populates the mirror via
// applyClientUsageSnapshot regardless, so no information is lost. The IP
// snapshot's addresses are still persisted to client_ip_history by
// enqueueClientIPHistory elsewhere in the snapshot pipeline.
func (s *Server) commitClientSnapshotsLocked(ctx context.Context, snapshot agentSnapshot) {
	if snapshot.HasClients {
		s.applyClientUsageSnapshot(ctx, snapshot.AgentID, snapshot.Clients)
	}
}

// commitMetricSnapshotLocked mints a new metric sample (ID + timestamp) and
// returns it for downstream batch-writer enqueueing, which persists it to the
// store. Returns nil when the snapshot carries no metrics. Caller must hold
// s.mu. The store is the sole source of truth for metric history (A2: the old
// in-memory ring is gone); the metricsAuditMu guards metricSeq so concurrent
// minting stays race-free.
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
	return &metric
}

func (s *Server) applyAgentSnapshot(ctx context.Context, snapshot agentSnapshot) error {
	s.logger.DebugContext(ctx, "agent heartbeat applied", "agent_id", snapshot.AgentID, "node", snapshot.NodeName)

	// Lock section: build all state objects AND commit to in-memory maps
	// atomically. No DB I/O happens under the locks.
	// Lock ordering: mu -> metricsAuditMu (client state lives in
	// clients.Service, taken via its own lock while mu is held). The live
	// store also has its own lock and is taken under s.mu (s.mu -> live);
	// it never calls back into the server, so the ordering cannot invert.
	s.mu.Lock()
	// Drop snapshots from agents that have been deregistered. The resurrection
	// guard runs BEFORE any live-store write, so a revoked agent's snapshot
	// never lands in s.live. Without this guard, an in-flight heartbeat that
	// arrives between the operator's DELETE and the gRPC stream tear-down would
	// re-create the live agent record (snapshot.AgentID is unconditionally
	// written via live.ApplySnapshot below), and the agent would resurrect
	// itself in the panel — typically with a "DEGRADED" badge as its telemetry
	// caught up.
	if _, revoked := s.revokedAgentIDs[snapshot.AgentID]; revoked {
		s.mu.Unlock()
		s.logger.InfoContext(ctx, "dropping snapshot from revoked agent", "agent_id", snapshot.AgentID)
		return nil
	}
	agent := s.updateAgentRecordFromSnapshot(snapshot)
	// IN-H6: on a partial snapshot the instance rows are blanked
	// (version/connections/read_only); preserve the last-known instances
	// instead of committing/persisting zeros. The agent is alive (LastSeenAt
	// advanced); its telemt detail simply could not be fully read this cycle.
	// We read the last-known instances back from the live store and re-commit
	// the agent value WITHOUT touching the instance set.
	//
	// Lock ordering: s.mu is held here; the live.* calls below take the live
	// store's own lock under s.mu (s.mu -> live), consistent with every other
	// call site. The live store never calls back into the server, so this can
	// never invert.
	var instances []Instance
	if snapshot.Partial {
		instances = s.live.InstancesForAgent(snapshot.AgentID)
	} else {
		instances = instancesFromSnapshot(snapshot)
		// Observed managed config is delta-gated: the agent sends the full JSON
		// only on the snapshot where it changed (empty otherwise). Carry forward
		// the last non-empty JSON from the previously stored instances so the
		// cached observed config persists between changes.
		carryForwardObservedConfig(instances, s.live.InstancesForAgent(snapshot.AgentID))
	}
	s.live.ApplySnapshot(snapshot.AgentID, agent, instances)
	s.commitClientSnapshotsLocked(ctx, snapshot)
	metricSnapshot := s.commitMetricSnapshotLocked(snapshot)
	s.applyFallbackStateTransition(agent)
	s.mu.Unlock()

	// Outside s.mu: detect Telemt recovery and nudge re-discovery for this agent.
	s.applyTelemtReachabilityTransition(ctx, agent)

	// Enqueue all DB writes asynchronously via the batch writer. No DB I/O
	// blocks the caller — the background flush goroutine handles persistence.
	s.enqueueAgentSnapshotBatchWrites(ctx, agent, instances, metricSnapshot, snapshot)

	// P2-LOG-12 / L-05: only Heartbeat on every snapshot. MarkConnected is
	// called exactly once per gRPC stream open (see Connect in
	// grpc_gateway.go) so the recorded connectedAt reflects the real
	// stream-open moment instead of being rewritten to "now" by every
	// heartbeat snapshot, which masked short disconnects.
	//
	// M-5: stamp the heartbeat with the PANEL clock, not the agent-supplied
	// ObservedAt. presence.Evaluate measures idle time against the panel's
	// now(); mixing the agent's clock here means clock skew (agent ahead)
	// yields a negative idle and the agent never transitions to
	// degraded/offline (it "sticks" online after a real disconnect).
	s.presence.Heartbeat(snapshot.AgentID, s.now().UTC())

	// D6b: coalesced — the background flusher publishes the latest value per
	// agent on a 300ms tick instead of one bus broadcast per inbound
	// snapshot. The nil-fallback keeps literal-constructed test Servers
	// (which bypass newServerFromOptions) on the immediate-publish path.
	if s.agentsUpdated != nil {
		s.agentsUpdated.Offer(agent)
	} else {
		s.events.Publish(eventbus.Event{
			Type: "agents.updated",
			Data: agent,
		})
	}

	return nil
}
