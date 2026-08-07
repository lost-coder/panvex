package server

import (
	"context"
	"encoding/json"

	"github.com/lost-coder/panvex/internal/controlplane/jobs"
	"github.com/lost-coder/panvex/internal/controlplane/updates"
)

// telemtUpdateReconcileActor attributes both re-dispatched telemt.update jobs
// and job-outcome bookkeeping (succeeded/failed/abandoned) to the system
// rather than to whichever operator originally clicked — matching
// selfUpdateReconcileActor's dual usage for agent self-update.
const telemtUpdateReconcileActor = "system:telemtupdate-reconcile"

// reconcileTelemtUpdate re-sends an operator-requested Telemt update that the
// node missed while offline. The original job carries a 10m TTL
// (telemtUpdateJobTTL) and, like agent.self-update, an expired telemt.update
// is never escalated anywhere — without this the request is silently lost.
// The desired target persists in updates.Service until the job's result
// clears it (recordTelemtUpdateJobOutcome, success case); the re-enqueue here
// is throttled to one per TTL per agent, mirroring reconcileAgentSelfUpdate.
//
// Unlike reconcileAgentSelfUpdate this does not compare a live-reported
// Telemt version to decide whether to clear the pending target. That signal
// does exist — s.live.InstancesForAgent(agentID) surfaces each managed
// instance's live api.Instance.Version, see http_config_targets.go — but
// recordTelemtUpdateJobOutcome's version-checked clear on the job result
// (ClearPendingTelemtUpdateIfVersion) is simpler and fully deterministic: it
// fires exactly once, keyed off the version the job itself carried, instead
// of a lazy per-reconnect comparison that would need its own
// staleness/ordering reasoning. Wiring InstancesForAgent into this path is a
// possible defense-in-depth follow-up, not a correctness requirement. Its
// only job here is "re-send if still pending and not throttled".
//
// Every failure path returns silently: reconcile is best-effort and a later
// reconnect retries. The throttle is stamped before the enqueue, so a
// failure after that point costs one throttle window rather than an
// immediate retry — acceptable for a self-heal whose whole job is to
// converge eventually.
//
// This file deliberately stays outside the s.store allowlist: state flows
// only through updates.Service, the live store and the jobs service.
func (s *Server) reconcileTelemtUpdate(ctx context.Context, agentID string) {
	if s.updatesSvc == nil {
		return
	}
	target, ok, err := s.updatesSvc.PendingTelemtUpdate(ctx, agentID)
	if err != nil {
		s.logger.WarnContext(ctx, "telemt update reconcile: read pending target failed",
			"agent_id", agentID, "error", err)
		return
	}
	if !ok {
		return
	}

	// A racing disconnect: dispatching at a node that is not live would just
	// burn the throttle window. The next reconnect reconciles again.
	if _, live := s.live.Get(agentID); !live {
		return
	}

	now := s.now()
	s.mu.Lock()
	last, seen := s.telemtUpdateReenqueuedAt[agentID]
	throttled := seen && now.Sub(last) < telemtUpdateJobTTL
	if !throttled {
		s.telemtUpdateReenqueuedAt[agentID] = now
	}
	s.mu.Unlock()
	if throttled {
		return
	}

	// The strategy may have been unconfigured or switched away from "binary"
	// since the original click; dispatching against a stale strategy would
	// just fail on the agent (or worse, on a Docker/none deployment, be
	// silently meaningless). Skip quietly — the pending target survives for
	// the next reconnect, same as an offline-node skip above.
	strategy, ok, err := s.telemtStrategySvc.Get(ctx, agentID)
	if err != nil {
		s.logger.WarnContext(ctx, "telemt update reconcile: read strategy failed",
			"agent_id", agentID, "error", err)
		return
	}
	if !ok || strategy.Mode != updates.StrategyModeBinary {
		return
	}

	// AllowDowngrade is a one-off operator intent tied to the original click
	// and is not persisted in the pending map — reconcile never allows a
	// downgrade automatically.
	payloadJSON, err := buildTelemtUpdateJobPayload(strategy, target, false)
	if err != nil {
		s.logger.ErrorContext(ctx, "telemt update reconcile: build payload failed",
			"agent_id", agentID, "error", err)
		return
	}

	job, err := s.jobs.Enqueue(ctx,
		telemtUpdateJobInput(agentID, payloadJSON, telemtUpdateReconcileActor), now)
	if err != nil {
		s.logger.ErrorContext(ctx, "telemt update reconcile: enqueue job failed",
			"agent_id", agentID, "error", err)
		return
	}

	s.logger.InfoContext(ctx, "telemt update reconcile: re-dispatched pending update",
		"agent_id", agentID, "target_version", target)
	s.notifyAgentSessions(job.TargetAgentIDs)
	s.publishJobCreated(job)
}

// recordTelemtUpdateJobOutcome folds a telemt.update job result into the
// pending-desired-state budget and audit trail. Called for EVERY job result
// (RecordClientJobResult sees them all), so it keys off the action and stays
// cheap for the common client.* case.
//
// Unlike recordSelfUpdateJobOutcome (which only tracks failures — the
// pending target clears lazily via a live-version comparison on reconnect),
// this handles BOTH outcomes explicitly: success clears the pending target
// immediately if it still matches the job's own version
// (ClearPendingTelemtUpdateIfVersion — a live Telemt version signal does
// exist, see reconcileTelemtUpdate's doc comment, but this version-checked
// clear on the job result is simpler and avoids a second lazy comparison
// path), and failure counts against the shared MaxPendingAgentUpdateFailures
// budget, giving up (dropping the desired state) once it is spent.
func (s *Server) recordTelemtUpdateJobOutcome(ctx context.Context, agentID, jobID string, success bool) {
	if s.updatesSvc == nil || jobID == "" {
		return
	}
	job, ok, err := s.jobs.GetWithContext(ctx, jobID)
	if err != nil || !ok || job.Action != jobs.ActionTelemtUpdate {
		return
	}
	// The target version comes from the job's own payload. Both outcomes
	// below version-check against the pending target before mutating it
	// (ClearPendingTelemtUpdateIfVersion on success,
	// RecordPendingTelemtUpdateFailure's own check on failure), so a result
	// for a superseded click — an operator re-clicked with a newer version
	// while this job was still in flight — can neither spend the failure
	// budget nor clear the newer pending target.
	var payload struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal([]byte(job.PayloadJSON), &payload); err != nil || payload.Version == "" {
		return
	}

	if success {
		if err := s.updatesSvc.ClearPendingTelemtUpdateIfVersion(ctx, agentID, payload.Version); err != nil {
			s.logger.WarnContext(ctx, "telemt update: clear pending target on success failed",
				"agent_id", agentID, "error", err)
			return
		}
		s.logger.InfoContext(ctx, "telemt update: succeeded",
			"agent_id", agentID, "version", payload.Version,
			"alert", "telemt_update_succeeded")
		s.appendAuditWithContext(ctx, telemtUpdateReconcileActor, "agents.telemt_update.succeeded", agentID, map[string]any{
			"version": payload.Version,
		})
		return
	}

	failures, gaveUp, err := s.updatesSvc.RecordPendingTelemtUpdateFailure(ctx, agentID, payload.Version)
	if err != nil {
		s.logger.WarnContext(ctx, "telemt update: record failure failed",
			"agent_id", agentID, "error", err)
		return
	}
	if failures == 0 {
		// A stale/no-op report (version no longer pending, or nothing
		// pending at all) — nothing to log or audit.
		return
	}
	s.logger.WarnContext(ctx, "telemt update: delivery failed",
		"agent_id", agentID, "version", payload.Version, "failures", failures,
		"alert", "telemt_update_failed")
	s.appendAuditWithContext(ctx, telemtUpdateReconcileActor, "agents.telemt_update.failed", agentID, map[string]any{
		"version": payload.Version, "failures": failures,
	})
	if gaveUp {
		s.logger.WarnContext(ctx, "telemt update: giving up on the pending update",
			"agent_id", agentID,
			"target_version", payload.Version,
			"failures", failures,
			"alert", "telemt_update_abandoned",
			"remediation", "fix the release asset or the node's update strategy, then dispatch the update again")
		s.appendAuditWithContext(ctx, telemtUpdateReconcileActor, "agents.telemt_update.abandoned", agentID, map[string]any{
			"version": payload.Version, "failures": failures,
		})
	}
}
