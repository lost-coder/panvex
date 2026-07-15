package clients

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/jobs"
)

type clientJobResultPayload struct {
	ConnectionLinks []string `json:"connection_links"`
}

// clientResetQuotaJobResultPayload mirrors the agent-side
// clientResetQuotaJobResult JSON (internal/agent/runtime/agent.go). Only
// the fields the panel acts on are decoded here; the typed-failure flags
// (unsupported_telemt / read_only_telemt) are inspected at the per-target
// UI layer via the raw result_json, not by this struct.
type clientResetQuotaJobResultPayload struct {
	UsedBytes          uint64 `json:"used_bytes"`
	LastResetEpochSecs uint64 `json:"last_reset_epoch_secs"`
}

// RecordJobResult updates client deployment state from a job result.
func (s *Service) RecordJobResult(ctx context.Context, agentID, jobID string, success bool, message, resultJSON string, observedAt time.Time) {
	job, ok := s.jobQueue.Get(jobID)
	if !ok {
		return
	}

	// Phase 3 (reset-quota): reset_quota is structurally a client job but
	// it does NOT change the deployment's desired-state (the client is
	// already deployed; only the byte counter is reset). Route it through
	// a slim path that updates LastResetEpochSecs on success without
	// rewriting DesiredOperation/Status/ConnectionLinks, then persists.
	if job.Action == jobs.ActionClientResetQuota {
		s.applyClientResetQuotaResult(ctx, agentID, job, success, resultJSON, observedAt)
		return
	}

	if !isClientJobAction(job.Action) {
		return
	}

	var payload ClientJobPayload
	if err := json.Unmarshal([]byte(job.PayloadJSON), &payload); err != nil {
		return
	}

	deployment, ok := s.applyClientJobDeployment(ctx, payload.ClientID, agentID, job, success, message, resultJSON, observedAt)
	if !ok {
		return
	}

	// R10: the node answered, so the reconciler's retry bookkeeping for this
	// pair starts fresh — a deployment that diverges again later gets its full
	// attempt budget rather than inheriting the last round's counter.
	if success {
		s.reconcile.confirm(reconcileKey(ClientID(payload.ClientID), agentID))
	}

	if err := s.PersistDeployment(ctx, deployment); err != nil {
		s.logger.ErrorContext(ctx, "client deployment persistence failed", "client_id", payload.ClientID, "agent_id", agentID, "error", err)
	} else {
		// Tell the web dashboard live (R10b/Task 5, C8): before this, a
		// job result only updated the deployment row, and the operator
		// learned of the transition on the next poll. Publish on both
		// success and failure — a node reporting a failure is exactly
		// the kind of transition that needs to surface immediately, not
		// just a success. Mirrors OnClientJobsExpired, which likewise
		// only publishes after a successful PersistDeployment.
		s.deps.PublishClientsUpdated(ClientID(payload.ClientID))
	}
}

// clientJobExpiredMessage is stamped on a deployment whose client job expired
// before the node confirmed it. It is operator-facing (rendered as LastError).
const clientJobExpiredMessage = "node did not confirm before the job expired; will retry on reconnect"

// OnClientJobsExpired flips the affected client deployments to awaiting_node so
// the operator sees "waiting for the node", not an eternal "queued". It is the
// jobs service's expiry hook (wired via jobs.SetExpiryHook in lifecycle.go),
// invoked AFTER the sweep releases its lock, once per sweep with every
// (job, agent) target that expired. This is presentation, not retry logic: the
// reconciler still owns re-sending, and awaiting_node is the same re-send class
// as queued in divergentDeploymentAction.
//
// This is a NEW functional channel from the jobs domain into the clients
// domain, deliberately unlike the metrics-only SetJobFailureHook P5 removed.
func (s *Service) OnClientJobsExpired(expired []jobs.ExpiredTarget) {
	ctx := s.deps.Context()
	now := s.now().UTC()
	// Invalidate the client list once per affected client (matches the existing
	// PublishClientsUpdated callers, which pass a concrete client ID) rather
	// than once with an empty ID.
	affected := make(map[string]struct{})
	for _, target := range expired {
		if !isClientJobAction(target.Job.Action) {
			continue
		}
		var payload ClientJobPayload
		if err := json.Unmarshal([]byte(target.Job.PayloadJSON), &payload); err != nil {
			continue
		}
		deployment, ok := s.MirrorDeployment(payload.ClientID, target.AgentID)
		if !ok || deployment.Status == DeploymentStatusSucceeded {
			// The node answered before the job expired — never clobber a success.
			continue
		}
		deployment.Status = DeploymentStatusAwaitingNode
		deployment.LastError = clientJobExpiredMessage
		deployment.UpdatedAt = now
		if err := s.PersistDeployment(ctx, deployment); err != nil {
			s.logger.ErrorContext(ctx, "client deployment expiry persistence failed",
				"client_id", payload.ClientID, "agent_id", target.AgentID, "error", err)
			continue
		}
		affected[payload.ClientID] = struct{}{}
	}
	for clientID := range affected {
		s.deps.PublishClientsUpdated(ClientID(clientID))
	}
}

// applyClientResetQuotaResult records the panel-side view of a completed
// client.reset_quota job: on success it extracts last_reset_epoch_secs
// from the agent's typed result envelope and stamps it onto the
// (client, agent) deployment row, then write-throughs to storage so the
// next ClientUsage snapshot can be drift-checked against it. On
// failure (including the typed unsupported_telemt / read_only_telemt
// flags) it leaves the deployment row untouched — the per-target
// reason is already in the Job.Targets[i].ResultJSON the UI reads.
func (s *Service) applyClientResetQuotaResult(ctx context.Context, agentID string, job jobs.Job, success bool, resultJSON string, observedAt time.Time) {
	if !success {
		return
	}

	var payload ClientResetQuotaJobPayload
	if err := json.Unmarshal([]byte(job.PayloadJSON), &payload); err != nil {
		return
	}

	// L-1: prefer telemt's authoritative reset epoch, but fall back to the
	// observation time when an older telemt (or a success response without
	// the typed envelope) omits it. Previously such a success was silently
	// dropped, so the job showed "succeeded" while the panel's reset history
	// stayed empty — a status/state divergence for the operator.
	//nolint:gosec // G115: observedAt is a wall-clock timestamp (well past the epoch), so Unix() is always positive.
	effectiveEpoch := uint64(observedAt.UTC().Unix())
	if strings.TrimSpace(resultJSON) != "" {
		var resetPayload clientResetQuotaJobResultPayload
		if err := json.Unmarshal([]byte(resultJSON), &resetPayload); err == nil && resetPayload.LastResetEpochSecs != 0 {
			effectiveEpoch = resetPayload.LastResetEpochSecs
		}
	}

	deployment, ok := s.recordClientResetQuotaTimestamp(payload.ClientID, agentID, effectiveEpoch, observedAt)
	if !ok {
		return
	}

	if err := s.PersistDeployment(ctx, deployment); err != nil {
		s.logger.ErrorContext(ctx, "client deployment persistence failed",
			"client_id", payload.ClientID, "agent_id", agentID,
			"action", string(jobs.ActionClientResetQuota), "error", err)
	}
}

// recordClientResetQuotaTimestamp computes the deployment carrying the new
// last-reset timestamp for persistence. Returns ok=false when the (client,
// agent) pair is no longer tracked (e.g. the operator unassigned the agent
// between job enqueue and result).
//
// This performs two independent Service mirror reads (MirrorClientExists
// then MirrorDeployment) rather than holding a single lock across both;
// the brief non-atomic window between them is benign — a concurrent delete
// is re-reconciled on the next telemetry tick, and the caller persists via
// PersistDeployment which writes the mirror under its own lock.
func (s *Service) recordClientResetQuotaTimestamp(clientID, agentID string, lastResetEpochSecs uint64, observedAt time.Time) (Deployment, bool) {
	if !s.MirrorClientExists(clientID) {
		return Deployment{}, false
	}
	deployment, ok := s.MirrorDeployment(clientID, agentID)
	if !ok {
		return Deployment{}, false
	}
	deployment.LastResetEpochSecs = lastResetEpochSecs
	deployment.UpdatedAt = observedAt.UTC()
	// The caller persists via PersistDeployment, which writes the mirror.
	return deployment, true
}

func isClientJobAction(action jobs.Action) bool {
	switch action {
	case jobs.ActionClientCreate, jobs.ActionClientUpdate, jobs.ActionClientDelete, jobs.ActionClientRotateSecret:
		return true
	default:
		return false
	}
}

// applyClientJobDeployment updates the in-memory deployment state for a
// client job result and returns the updated deployment. Returns ok=false
// when the client is no longer tracked.
func (s *Service) applyClientJobDeployment(ctx context.Context, clientID, agentID string, job jobs.Job, success bool, message, resultJSON string, observedAt time.Time) (Deployment, bool) {
	if !s.MirrorClientExists(clientID) {
		return Deployment{}, false
	}
	// Current deployment may not exist yet (first apply for this agent) — a
	// zero deployment is the correct starting point, matching the prior
	// map-read which returned the zero value for a missing inner key.
	deployment, _ := s.MirrorDeployment(clientID, agentID)

	deployment.ClientID = ClientID(clientID)
	deployment.AgentID = agentID
	deployment.DesiredOperation = string(job.Action)
	deployment.UpdatedAt = observedAt.UTC()
	applyClientJobOutcome(ctx, &deployment, job.Action, success, message, resultJSON, observedAt)

	// The caller persists via PersistDeployment, which writes the mirror.
	return deployment, true
}

// staleLinkDiagnostic is the operator-facing warning stamped on a
// non-delete apply that succeeded without the node returning any
// connection links (IN-M2). The existing ConnectionLinks are preserved
// but may no longer be valid after a host/secret change.
const staleLinkDiagnostic = "apply succeeded but the node returned no connection links; existing links may be stale"

func applyClientJobOutcome(ctx context.Context, deployment *Deployment, action jobs.Action, success bool, message, resultJSON string, observedAt time.Time) {
	if !success {
		// Leave LinkDiagnostic untouched: it reflects the prior
		// successful-apply state, which a failed job does not change.
		deployment.Status = DeploymentStatusFailed
		deployment.LastError = message
		return
	}
	deployment.Status = DeploymentStatusSucceeded
	deployment.LastError = ""
	lastAppliedAt := observedAt.UTC()
	deployment.LastAppliedAt = &lastAppliedAt

	if action == jobs.ActionClientDelete {
		deployment.ConnectionLinks = nil
		deployment.LinkDiagnostic = ""
		return
	}

	links := parseClientJobResultLinks(resultJSON)
	if len(links) > 0 {
		deployment.ConnectionLinks = links
		deployment.LinkDiagnostic = ""
		return
	}

	// IN-M2: success without links. Keep the old links (they may still
	// be the only thing the operator has) but record a diagnostic so the
	// UI can flag them as possibly-stale instead of serving them blind.
	deployment.LinkDiagnostic = staleLinkDiagnostic
	slog.WarnContext(ctx, "client apply succeeded but node returned no connection links; existing links may be stale",
		"client_id", string(deployment.ClientID),
		"agent_id", deployment.AgentID,
		"action", string(action))
}

// parseClientJobResultLinks extracts the connection links from a job
// result envelope, returning nil when the payload is empty or malformed.
func parseClientJobResultLinks(resultJSON string) []string {
	if strings.TrimSpace(resultJSON) == "" {
		return nil
	}
	var resultPayload clientJobResultPayload
	if err := json.Unmarshal([]byte(resultJSON), &resultPayload); err != nil {
		return nil
	}
	return resultPayload.ConnectionLinks
}

// ResolveIDByName finds the panel client ID for a given client name
// assigned to a specific agent. Used when the agent sends usage snapshots
// without a panel-assigned client_id (e.g. adopted clients).
//
// A client matches when it is either directly assigned to the agent OR
// assigned to a fleet group the agent belongs to (P2-LOG-07 / M-C3). Without
// the fleet-group fallback, usage stats for clients attached via fleet-group
// assignments were silently dropped.
//
// The agent's current fleet group is resolved via deps.AgentFleetGroupID
// (the live store, its own lock) and the name lookup delegates to the
// clients mirror (the Service's own lock); the two locks are never held
// together, which preserves the documented lock ordering.
func (s *Service) ResolveIDByName(agentID, clientName string) string {
	agentFleetGroupID, _ := s.deps.AgentFleetGroupID(agentID)
	return s.MirrorResolveIDByName(agentID, agentFleetGroupID, clientName)
}
