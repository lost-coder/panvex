package server

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/clients"
	"github.com/lost-coder/panvex/internal/controlplane/jobs"
)

// Lock ordering invariant for the Server struct (P2-LOG-11 / M-C11 / L-08):
//
//	s.mu  ->  s.metricsAuditMu
//
// Whenever two of these locks must be observed together, they MUST be taken
// in the order above and released in the reverse order.
//
// Client/usage/deployment state is owned by clients.Service, which guards it
// with its own internal lock. The server takes the Service lock (via Service
// methods) while holding s.mu in applyAgentSnapshot/purgeAgentInMemory, so the
// effective ordering is s.mu -> Service.mu. Functions that need data from BOTH
// the agent maps and the Service snapshot the agent fields under s.mu, release
// it, then call into the Service — they never nest the two. See
// resolveClientTargetAgentIDs and resolveClientIDByName below for the
// snapshot pattern.

// Client-mutation error sentinels are aliases to the clients-package
// sentinels so existing errors.Is checks (handleClientMutationError) keep
// matching after the mutation flows moved onto clients.Service, which
// returns the clients.Err* values directly.
var (
	errClientNameRequired    = clients.ErrNameRequired
	errClientNameInvalid     = clients.ErrNameInvalid
	errClientNameTaken       = clients.ErrNameTaken
	errClientUserADTag       = clients.ErrUserADTag
	errClientExpiration      = clients.ErrExpiration
	errClientTargetsRequired = clients.ErrTargetsRequired
	errClientLimitNegative   = clients.ErrLimitNegative
)

// clientJobTTL is the compiled-in default TTL for client-mutation jobs.
// The live value is resolved via s.effectiveClientJobTTL() so operator
// changes to jobs.client_job_ttl take effect without a panel restart.
const clientJobTTL = 10 * time.Minute

// effectiveClientJobTTL returns the current client-job TTL. When the
// operational settings store is wired, the live value is used; falls back
// to the compiled-in constant otherwise.
func (s *Server) effectiveClientJobTTL() time.Duration {
	if s.settings != nil {
		return s.settings.JobsClientJobTTL()
	}
	return clientJobTTL
}

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

func (s *Server) replaceClientStateWithContext(ctx context.Context, client managedClient, assignments []managedClientAssignment, deployments []managedClientDeployment) error {
	if s.clientsSvc.HasRepo() {
		// NewService path: SaveState atomically writes to the Repository and
		// updates the Service mirror (the single owner of client state).
		if err := s.clientsSvc.SaveState(ctx, client, assignments, deployments); err != nil {
			return err
		}
	} else {
		// No-repo fallback (test doubles / pre-migrate stores): update the
		// in-memory mirror directly.
		s.replaceClientStateInMemory(client, assignments, deployments)
	}
	s.publishClientsUpdated(client.ID)
	return nil
}

// replaceClientStateInMemory updates the clients.Service mirror for one
// client without touching the store. Factored out of
// replaceClientStateWithContext so callers that drive persistence through a
// UnitOfWork can apply the in-memory update only after the transaction
// commits (see adoptDiscoveredClient, P2-ARCH-01). For the SaveState path
// the mirror was already updated inside SaveState, so this is an idempotent
// re-write with the same (plaintext-secret) values.
func (s *Server) replaceClientStateInMemory(client managedClient, assignments []managedClientAssignment, deployments []managedClientDeployment) {
	s.clientsSvc.MirrorReplaceInMemory(client, assignments, deployments)
}

// resolveClientTargetAgentIDs snapshots the current agent topology under
// the live store and delegates the deterministic deduplication + sorting to
// clients.Service.ResolveTargetAgentIDs.
//
// Lock discipline (P2-LOG-11 / M-C11 / L-08): the assignments are read from
// the clients.Service mirror (its own lock) by the caller. The registered-
// agent IDs and fleet-group membership are snapshotted from s.live (its own
// lock) into local maps, and the pure helper iterates against those local
// snapshots. The snapshot can race with a concurrent agent mutation, but callers tolerate
// that: the result builds deployment rows that are re-reconciled on the next
// snapshot. The race is benign and lock-order-safe.
func (s *Server) resolveClientTargetAgentIDs(assignments []managedClientAssignment) []string {
	liveAgents := s.live.List()
	registeredAgents := make(map[string]struct{}, len(liveAgents))
	fleetMembers := make(map[string][]string)
	for _, agent := range liveAgents {
		registeredAgents[agent.ID] = struct{}{}
		if agent.FleetGroupID != "" {
			fleetMembers[agent.FleetGroupID] = append(fleetMembers[agent.FleetGroupID], agent.ID)
		}
	}

	return s.clientsSvc.ResolveTargetAgentIDs(assignments, clients.AgentTopology{
		RegisteredAgents: registeredAgents,
		FleetMembers:     fleetMembers,
	})
}

func (s *Server) recordClientJobResultWithContext(ctx context.Context, agentID, jobID string, success bool, message, resultJSON string, observedAt time.Time) {
	job, ok := s.jobByID(ctx, jobID)
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

	var payload clients.ClientJobPayload
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
	if success && s.clientReconcile != nil {
		s.clientReconcile.confirm(reconcileKey(clients.ClientID(payload.ClientID), agentID))
	}

	if s.clientsSvc != nil {
		if err := s.clientsSvc.PersistDeployment(ctx, deployment); err != nil {
			s.logger.ErrorContext(ctx, "client deployment persistence failed", "client_id", payload.ClientID, "agent_id", agentID, "error", err)
		} else {
			// Tell the web dashboard live (R10b/Task 5, C8): before this, a
			// job result only updated the deployment row, and the operator
			// learned of the transition on the next poll. Publish on both
			// success and failure — a node reporting a failure is exactly
			// the kind of transition that needs to surface immediately, not
			// just a success. Mirrors onClientJobsExpired, which likewise
			// only publishes after a successful PersistDeployment.
			s.publishClientsUpdated(payload.ClientID)
		}
	}
}

// clientJobExpiredMessage is stamped on a deployment whose client job expired
// before the node confirmed it. It is operator-facing (rendered as LastError).
const clientJobExpiredMessage = "node did not confirm before the job expired; will retry on reconnect"

// onClientJobsExpired flips the affected client deployments to awaiting_node so
// the operator sees "waiting for the node", not an eternal "queued". It is the
// jobs service's expiry hook (wired via jobs.SetExpiryHook in lifecycle.go),
// invoked AFTER the sweep releases its lock, once per sweep with every
// (job, agent) target that expired. This is presentation, not retry logic: the
// reconciler (clientReconciler) still owns re-sending, and awaiting_node is the
// same re-send class as queued in divergentDeploymentAction.
//
// This is a NEW functional channel from the jobs domain into the clients
// domain, deliberately unlike the metrics-only SetJobFailureHook P5 removed.
func (s *Server) onClientJobsExpired(expired []jobs.ExpiredTarget) {
	if s.clientsSvc == nil {
		return
	}
	ctx := s.Context()
	now := s.now().UTC()
	// Invalidate the client list once per affected client (matches the existing
	// publishClientsUpdated callers, which pass a concrete client ID) rather
	// than once with an empty ID — publishClientsUpdated takes `any` so "" is
	// accepted, but no caller establishes it as a "refresh everything" sentinel.
	affected := make(map[string]struct{})
	for _, target := range expired {
		if !isClientJobAction(target.Job.Action) {
			continue
		}
		var payload clients.ClientJobPayload
		if err := json.Unmarshal([]byte(target.Job.PayloadJSON), &payload); err != nil {
			continue
		}
		deployment, ok := s.clientsSvc.MirrorDeployment(payload.ClientID, target.AgentID)
		if !ok || deployment.Status == clientDeploymentStatusSucceeded {
			// The node answered before the job expired — never clobber a success.
			continue
		}
		deployment.Status = clients.DeploymentStatusAwaitingNode
		deployment.LastError = clientJobExpiredMessage
		deployment.UpdatedAt = now
		if err := s.clientsSvc.PersistDeployment(ctx, deployment); err != nil {
			s.logger.ErrorContext(ctx, "client deployment expiry persistence failed",
				"client_id", payload.ClientID, "agent_id", target.AgentID, "error", err)
			continue
		}
		affected[payload.ClientID] = struct{}{}
	}
	for clientID := range affected {
		s.publishClientsUpdated(clientID)
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
func (s *Server) applyClientResetQuotaResult(ctx context.Context, agentID string, job jobs.Job, success bool, resultJSON string, observedAt time.Time) {
	if !success {
		return
	}

	var payload clients.ClientResetQuotaJobPayload
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

	if s.clientsSvc != nil {
		if err := s.clientsSvc.PersistDeployment(ctx, deployment); err != nil {
			s.logger.ErrorContext(ctx, "client deployment persistence failed",
				"client_id", payload.ClientID, "agent_id", agentID,
				"action", string(jobs.ActionClientResetQuota), "error", err)
		}
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
func (s *Server) recordClientResetQuotaTimestamp(clientID, agentID string, lastResetEpochSecs uint64, observedAt time.Time) (managedClientDeployment, bool) {
	if !s.clientsSvc.MirrorClientExists(clientID) {
		return managedClientDeployment{}, false
	}
	deployment, ok := s.clientsSvc.MirrorDeployment(clientID, agentID)
	if !ok {
		return managedClientDeployment{}, false
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
func (s *Server) applyClientJobDeployment(ctx context.Context, clientID, agentID string, job jobs.Job, success bool, message, resultJSON string, observedAt time.Time) (managedClientDeployment, bool) {
	if !s.clientsSvc.MirrorClientExists(clientID) {
		return managedClientDeployment{}, false
	}
	// Current deployment may not exist yet (first apply for this agent) — a
	// zero deployment is the correct starting point, matching the prior
	// map-read which returned the zero value for a missing inner key.
	deployment, _ := s.clientsSvc.MirrorDeployment(clientID, agentID)

	deployment.ClientID = clients.ClientID(clientID)
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

func applyClientJobOutcome(ctx context.Context, deployment *managedClientDeployment, action jobs.Action, success bool, message, resultJSON string, observedAt time.Time) {
	if !success {
		// Leave LinkDiagnostic untouched: it reflects the prior
		// successful-apply state, which a failed job does not change.
		deployment.Status = clientDeploymentStatusFailed
		deployment.LastError = message
		return
	}
	deployment.Status = clientDeploymentStatusSucceeded
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

// jobByID returns the job with the given ID. P-4: backed by the O(1)
// jobs.Service.Get index — historically this iterated ListWithContext,
// which was O(jobs) per result-recording call.
func (s *Server) jobByID(_ context.Context, jobID string) (jobs.Job, bool) {
	return s.jobs.Get(jobID)
}

// resolveClientIDByName finds the panel client ID for a given client name
// assigned to a specific agent. Used when the agent sends usage snapshots
// without a panel-assigned client_id (e.g. adopted clients).
//
// A client matches when it is either directly assigned to the agent OR
// assigned to a fleet group the agent belongs to (P2-LOG-07 / M-C3). Without
// the fleet-group fallback, usage stats for clients attached via fleet-group
// assignments were silently dropped.
// resolveClientIDByName snapshots the agent's current fleet group under
// s.mu then delegates the name lookup to the clients.Service mirror. The
// two locks (s.mu and the Service's own lock) are never held together,
// which preserves the documented lock ordering.
func (s *Server) resolveClientIDByName(agentID, clientName string) string {
	agentFleetGroupID := ""
	if agent, ok := s.live.Get(agentID); ok {
		agentFleetGroupID = agent.FleetGroupID
	}

	return s.clientsSvc.MirrorResolveIDByName(agentID, agentFleetGroupID, clientName)
}

func (s *Server) nextClientID() clients.ClientID {
	return clients.ClientID(s.clientsSvc.NextClientID())
}

func (s *Server) nextClientAssignmentID() clients.AssignmentID {
	return clients.AssignmentID(s.clientsSvc.NextAssignmentID())
}

// buildClientDeployments delegates to clients.BuildDeployments.
// Agents no longer in the target set are marked for deletion; see
// deployments.go in the clients package.
func buildClientDeployments(current []managedClientDeployment, clientID clients.ClientID, targetAgentIDs []string, desiredOperation string, observedAt time.Time) []managedClientDeployment {
	return clients.BuildDeployments(current, clientID, targetAgentIDs, desiredOperation, string(jobs.ActionClientDelete), observedAt)
}

// deploymentAgentIDs delegates to clients.DeploymentAgentIDs.
func deploymentAgentIDs(deployments []managedClientDeployment) []string {
	return clients.DeploymentAgentIDs(deployments)
}

// normalizedIDs delegates to clients.NormalizedIDs.
func normalizedIDs(values []string) []string {
	return clients.NormalizedIDs(values)
}

// normalizedExpiration delegates to clients.NormalizeExpiration.
func normalizedExpiration(value string) (string, error) {
	out, err := clients.NormalizeExpiration(value)
	if errors.Is(err, clients.ErrExpiration) {
		return "", errClientExpiration
	}
	return out, err
}

// randomHexString delegates to clients.RandomHexString.
func randomHexString(size int) (string, error) {
	return clients.RandomHexString(size)
}

// isValidHexSecret delegates to clients.IsValidHexSecret.
func isValidHexSecret(s string) bool {
	return clients.IsValidHexSecret(s)
}
