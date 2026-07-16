package server

import (
	"context"
	"strings"
)

// selfUpdateReconcileActor attributes re-dispatched self-update jobs to the
// reconciler rather than to the operator who clicked, matching the
// "system:transport-drift" convention of the transport self-heal.
const selfUpdateReconcileActor = "system:selfupdate-reconcile"

// reconcileAgentSelfUpdate re-sends an operator-requested agent update that the
// node missed while offline. The original job carries a 10m TTL
// (agentSelfUpdateJobTTL) and, unlike client.* jobs, an expired self-update is
// never escalated anywhere — without this the request is silently lost. The
// desired target persists in updates.Service until the agent reports it; the
// re-enqueue is throttled to one per TTL per agent, mirroring the
// transport-drift self-heal.
//
// Every failure path returns silently: reconcile is best-effort and a later
// reconnect retries. The throttle is stamped before the enqueue, so a failure
// after that point costs one throttle window rather than an immediate retry —
// acceptable for a self-heal whose whole job is to converge eventually. Only
// the throttle map is touched under s.mu, and never across the
// enqueue/notify/publish.
//
// This file deliberately stays outside the s.store allowlist: state flows only
// through updates.Service, the live store and the jobs service.
func (s *Server) reconcileAgentSelfUpdate(ctx context.Context, agentID string) {
	if s.updatesSvc == nil {
		return
	}
	target, ok, err := s.updatesSvc.PendingAgentUpdate(ctx, agentID)
	if err != nil {
		s.logger.WarnContext(ctx, "self-update reconcile: read pending target failed",
			"agent_id", agentID, "error", err)
		return
	}
	if !ok {
		return
	}

	// A racing disconnect: dispatching at a node that is not live would just
	// burn the throttle window. The next reconnect reconciles again.
	agent, live := s.live.Get(agentID)
	if !live {
		return
	}

	if versionsEqual(agent.Version, target) {
		if err := s.updatesSvc.ClearPendingAgentUpdate(ctx, agentID); err != nil {
			s.logger.WarnContext(ctx, "self-update reconcile: clear satisfied target failed",
				"agent_id", agentID, "error", err)
		}
		return
	}

	now := s.now()
	s.mu.Lock()
	last, seen := s.selfUpdateReenqueuedAt[agentID]
	throttled := seen && now.Sub(last) < agentSelfUpdateJobTTL
	if !throttled {
		s.selfUpdateReenqueuedAt[agentID] = now
	}
	s.mu.Unlock()
	if throttled {
		return
	}

	s.settingsMu.RLock()
	settings := s.updateSettings
	s.settingsMu.RUnlock()

	// The dispatch handler rejects a bad repo with a 400; the repo may have
	// been blanked or corrupted since the click, and buildAgentDirectUpdatePayload
	// would happily marshal a download URL the agent can only fail on.
	if err := validateGitHubRepo(settings.GitHubRepo); err != nil {
		s.logger.WarnContext(ctx, "self-update reconcile: invalid github_repo configured",
			"agent_id", agentID, "error", err)
		return
	}

	payloadJSON, err := buildAgentDirectUpdatePayload(settings.GitHubRepo, target)
	if err != nil {
		s.logger.ErrorContext(ctx, "self-update reconcile: build payload failed",
			"agent_id", agentID, "error", err)
		return
	}

	job, err := s.jobs.Enqueue(ctx,
		agentSelfUpdateJobInput(agentID, string(payloadJSON), selfUpdateReconcileActor), now)
	if err != nil {
		s.logger.ErrorContext(ctx, "self-update reconcile: enqueue job failed",
			"agent_id", agentID, "error", err)
		return
	}

	s.logger.InfoContext(ctx, "self-update reconcile: re-dispatched pending agent update",
		"agent_id", agentID, "target_version", target, "reported_version", agent.Version)
	s.notifyAgentSessions(job.TargetAgentIDs)
	s.publishJobCreated(job)
}

// versionsEqual compares a reported agent version against a requested target,
// tolerating the optional "v" prefix each side may carry.
func versionsEqual(reported, target string) bool {
	trim := func(v string) string { return strings.TrimPrefix(strings.TrimSpace(v), "v") }
	return trim(reported) == trim(target)
}
