package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/lost-coder/panvex/internal/controlplane/jobs"
	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

const (
	// configApplyHealthTimeoutSec is the per-agent health-probe budget the
	// agent honours after applying the config patch (carried in the payload).
	configApplyHealthTimeoutSec = 30
	// configApplyJobTTL bounds how long a single config.apply job may stay
	// outstanding before the target is expired.
	//
	// P3-3.4: config apply itself is now async (batch-of-one); the three
	// poll-* constants below survive only for waitJobTargetTerminal, whose
	// sole remaining caller is the synchronous runtime.restart wait
	// (http_agent_restart.go). config.apply no longer polls in-handler.
	configApplyJobTTL = 5 * time.Minute
	// configApplyPollInterval is how often waitJobTargetTerminal polls the job
	// store for the target's terminal status.
	configApplyPollInterval = 500 * time.Millisecond
	// configApplyPollGrace is added to the job TTL to form the wait deadline,
	// so a target that expires at the TTL boundary is observed before we bail.
	configApplyPollGrace = 30 * time.Second
)

// groupApplyAcceptedResponse is the 202 body returned by the ASYNC apply
// handlers (group fan-out and single-agent batch-of-one, P3-3.4). The client
// polls the persistent-batch endpoint by batch_id; per-job handles were
// removed along with the legacy job-id poller.
type groupApplyAcceptedResponse struct {
	BatchID string `json:"batch_id"`
}

// groupApplyAgentStatus is the per-agent status row in the status response.
// Status is one of pending / running / succeeded / failed. Message carries
// the agent's own ResultText on failure (empty otherwise).
type groupApplyAgentStatus struct {
	AgentID string `json:"agent_id"`
	JobID   string `json:"job_id"`
	Status  string `json:"status"`
	Message string `json:"message"`
}

// Per-agent apply status values returned by the group-apply status endpoint.
const (
	applyStatusPending   = "pending"
	applyStatusRunning   = "running"
	applyStatusSucceeded = "succeeded"
	applyStatusFailed    = "failed"
)

// configApplyJobPayload is the config.apply job body the agent decodes: the
// effective config patch plus the per-apply health-probe timeout.
type configApplyJobPayload struct {
	Patch          map[string]any `json:"patch"`
	HealthTimeoutS int            `json:"health_timeout_s"`
}

// targetAgentID returns the agent id a JobTarget belongs to.
func targetAgentID(tgt jobs.JobTarget) string {
	return tgt.AgentID
}

// effectiveConfigForAgent resolves the agent's effective config target —
// the agent's fleet-group target deep-merged with its own override. The
// fleet group is read from the live snapshot; an agent with no group (or no
// stored sections) resolves an empty map. Mirrors handleGetAgentConfigTarget.
func (s *Server) effectiveConfigForAgent(ctx context.Context, agentID string) map[string]any {
	groupSections := map[string]any{}
	if existing, ok := s.live.Get(agentID); ok && existing.FleetGroupID != "" {
		if sections, err := s.loadConfigTargetSectionsCtx(ctx, storage.ConfigScopeGroup, existing.FleetGroupID); err == nil {
			groupSections = sections
		}
	}
	overrideSections, err := s.loadConfigTargetSectionsCtx(ctx, storage.ConfigScopeAgent, agentID)
	if err != nil {
		overrideSections = map[string]any{}
	}
	return resolveEffectiveConfig(groupSections, overrideSections)
}

// enqueueConfigApplyJob resolves the agent's effective config target and
// enqueues a config.apply job for it WITHOUT blocking on the result. Returns
// the enqueued job id, or an empty id when the effective config is empty (a
// no-op — the agent is already in sync). The non-blocking half of the apply
// flow: applyConfigToAgent wraps this with a terminal-status wait for the
// synchronous single-agent path, while the async group fan-out returns the
// job ids to the client for polling.
func (s *Server) enqueueConfigApplyJob(ctx context.Context, actorID, agentID string) (string, error) {
	effective := s.effectiveConfigForAgent(ctx, agentID)
	if len(effective) == 0 {
		return "", nil
	}
	payload, err := json.Marshal(configApplyJobPayload{
		Patch:          effective,
		HealthTimeoutS: configApplyHealthTimeoutSec,
	})
	if err != nil {
		return "", fmt.Errorf("marshal config.apply payload: %w", err)
	}
	job, err := s.jobs.Enqueue(ctx, jobs.CreateJobInput{
		Action:         jobs.ActionConfigApply,
		TargetAgentIDs: []string{agentID},
		TTL:            configApplyJobTTL,
		ActorID:        actorID,
		PayloadJSON:    string(payload),
		ReadOnlyAgents: s.readOnlyAgents([]string{agentID}),
	}, s.now())
	if err != nil {
		return "", fmt.Errorf("enqueue config.apply: %w", err)
	}
	s.notifyAgentSessions(job.TargetAgentIDs)
	s.publishJobCreated(job)
	return job.ID, nil
}

// applyConfigToAgent resolves the agent's effective config target and applies
// it by enqueueing a config.apply job, then BLOCKS until that job's target
// reaches a terminal status. Returns nil on success, an error on
// failure/timeout. A no-op (nil) when the effective config is empty. Used by
// the SYNCHRONOUS single-agent apply path, which blocks on exactly one job.
// handleApplyGroupConfig applies the effective config target to every in-scope
// agent in a fleet group ASYNCHRONOUSLY. It enqueues one config.apply job per
// agent and returns 202 Accepted immediately with a batch id + the per-agent
// job ids, WITHOUT blocking on the jobs' terminal status. This avoids the old
// K × (TTL + grace) synchronous fan-out that let a large group trip the
// request-timeout middleware and hand the operator a confusing 5xx while jobs
// kept running server-side (audit MEDIUM). Progress is observed via
// handleGroupConfigApplyStatus, which the dashboard polls. Enqueueing is
// concurrent (not stop-on-failure): with async delivery there is no serialized
// wait to gate the next agent on, and the operator sees each agent's outcome
// individually — a bad config surfaces per-agent in the status view.
func (s *Server) handleApplyGroupConfig() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		_, user, err := s.requireSession(r)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		id := chi.URLParam(r, "id")
		if id == "" {
			writeError(w, http.StatusBadRequest, msgConfigTargetIDReq)
			return
		}
		// R-S-14: scope-check the fleet-group id before any work so an
		// out-of-scope operator receives the same not-found response the
		// sibling /fleet-groups/{id} endpoints return.
		scope, ok := s.requireFleetScope(w, r, user)
		if !ok {
			return
		}
		if !scope.IsAllowed(id) {
			writeError(w, http.StatusNotFound, msgFleetGroupNotFound)
			return
		}
		agentIDs := []string{}
		for _, agent := range s.live.List() {
			if agent.FleetGroupID == id {
				agentIDs = append(agentIDs, agent.ID)
			}
		}
		batchID, err := s.createConfigApplyBatch(ctx, user.ID, id, storage.ConfigApplyBatchModeAllAtOnce, 1, agentIDs)
		if err != nil {
			writeErrorLogged(ctx, w, http.StatusInternalServerError,
				"failed to enqueue config apply", err)
			return
		}
		writeJSON(w, http.StatusAccepted, groupApplyAcceptedResponse{BatchID: batchID})
	}
}

// configApplyMsgJobLost is the operator-facing message recorded for a
// target whose config.apply job disappeared from the in-memory jobs store
// before a terminal status was observed and persisted (TTL eviction,
// panel restart losing the job, or a foreign/mistyped job id on the
// legacy poll path). Rendered verbatim in the dashboard's rollout view.
const configApplyMsgJobLost = "job lost before terminal status was persisted"

// configApplyJobStatus resolves a single config.apply job/agent pair to a
// per-agent apply status (applyStatus* consts) and message. A job absent
// from the store resolves to failed with configApplyMsgJobLost: the only
// callers pass job ids for targets that are not yet persisted as terminal,
// so "missing" means the terminal outcome was lost, not that it succeeded
// (audit 2026-07-02 #2 — the old succeeded default finalized failed
// rollouts as successful batches). Failed/expired targets surface the
// agent's own ResultText. Shared by the legacy job-id status endpoint
// (aggregateGroupApplyStatus) and the persistent-batch orchestrator
// (advanceConfigApplyBatch in config_apply_batches.go) so the two status
// paths cannot drift out of sync (DRY).
func (s *Server) configApplyJobStatus(jobID, agentID string) (status, message string) {
	job, ok := s.jobs.Get(jobID)
	if !ok {
		return applyStatusFailed, configApplyMsgJobLost
	}
	for _, tgt := range job.Targets {
		if targetAgentID(tgt) != agentID {
			continue
		}
		switch tgt.Status {
		case jobs.TargetStatusSucceeded:
			return applyStatusSucceeded, ""
		case jobs.TargetStatusFailed:
			return applyStatusFailed, tgt.ResultText
		case jobs.TargetStatusExpired:
			return applyStatusFailed, "timed out before completion"
		case jobs.TargetStatusSent, jobs.TargetStatusAcknowledged:
			return applyStatusRunning, ""
		default:
			return applyStatusPending, ""
		}
	}
	return applyStatusPending, ""
}

// groupApplyBatchStatusResponse is the aggregate returned by the persistent
// batch-status endpoint (GET .../config/apply/batches/{batchId}). Unlike
// groupApplyStatusResponse (built from the job/agent ids the client happened
// to receive from the 202 response), this is derived entirely from the
// stored batch + target rows, so it can be reconstructed after a panel
// restart, a browser refresh days later, or once the originating config.apply
// jobs have rolled off the in-memory jobs store — the whole point of Phase A
// persisting batches in the first place. Done mirrors the batch's own
// persisted status rather than re-deriving "all terminal" from the targets,
// so it agrees with advanceConfigApplyBatch's finalization exactly.
type groupApplyBatchStatusResponse struct {
	BatchID string                  `json:"batch_id"`
	Mode    string                  `json:"mode"`
	Status  string                  `json:"status"`
	Done    bool                    `json:"done"`
	Total   int                     `json:"total"`
	Applied int                     `json:"applied"`
	Failed  int                     `json:"failed"`
	Pending int                     `json:"pending"`
	Skipped int                     `json:"skipped"`
	Agents  []groupApplyAgentStatus `json:"agents"`
}

// groupApplyActiveBatchResponse is the 200 body for
// GET .../config/apply/batches?active=1: the id of the fleet group's
// currently running batch. The handler returns 204 No Content (no body)
// instead of this shape when the group has no batch in-flight.
type groupApplyActiveBatchResponse struct {
	BatchID string `json:"batch_id"`
}

// handleGetGroupApplyBatchStatus returns the persisted-batch aggregate for a
// single config-apply rollout. batchId is looked up independently of the
// fleet-group scope check, then cross-checked against the URL's group id —
// a batch that exists but belongs to a different group (or one outside the
// operator's fleet scope) is reported as not-found, matching the sibling
// endpoints' scope-mismatch behaviour rather than leaking the batch's
// existence.
func (s *Server) handleGetGroupApplyBatchStatus() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		_, user, err := s.requireSession(r)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		id := chi.URLParam(r, "id")
		if id == "" {
			writeError(w, http.StatusBadRequest, msgConfigTargetIDReq)
			return
		}
		scope, ok := s.requireFleetScope(w, r, user)
		if !ok {
			return
		}
		if !scope.IsAllowed(id) {
			writeError(w, http.StatusNotFound, msgFleetGroupNotFound)
			return
		}
		batchID := chi.URLParam(r, "batchId")
		batch, targets, err := s.store.GetConfigApplyBatch(ctx, batchID)
		if err != nil {
			if errors.Is(err, storage.ErrNotFound) {
				writeError(w, http.StatusNotFound, msgConfigApplyBatchNotFound)
				return
			}
			writeErrorLogged(ctx, w, http.StatusInternalServerError, "failed to load config-apply batch", err)
			return
		}
		if batch.FleetGroupID != id {
			writeError(w, http.StatusNotFound, msgConfigApplyBatchNotFound)
			return
		}
		writeJSON(w, http.StatusOK, s.aggregateGroupApplyBatchStatus(batch, targets))
	}
}

// handleGetActiveGroupApplyBatch returns the id of the fleet group's
// currently running config-apply batch (?active=1), or 204 No Content when
// none is in-flight. This is the entry point a dashboard uses to discover
// whether there is anything to resume-poll after a page load, without the
// client needing to remember a batch id across a refresh.
func (s *Server) handleGetActiveGroupApplyBatch() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		_, user, err := s.requireSession(r)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		id := chi.URLParam(r, "id")
		if id == "" {
			writeError(w, http.StatusBadRequest, msgConfigTargetIDReq)
			return
		}
		scope, ok := s.requireFleetScope(w, r, user)
		if !ok {
			return
		}
		if !scope.IsAllowed(id) {
			writeError(w, http.StatusNotFound, msgFleetGroupNotFound)
			return
		}
		active, ok, err := s.store.ActiveConfigApplyBatchForGroup(ctx, id)
		if err != nil {
			writeErrorLogged(ctx, w, http.StatusInternalServerError, "failed to load active config-apply batch", err)
			return
		}
		if !ok {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		writeJSON(w, http.StatusOK, groupApplyActiveBatchResponse{BatchID: active.ID})
	}
}

// aggregateGroupApplyBatchStatus folds a batch's persisted target rows into
// the status response. A target already in a terminal state
// (succeeded/failed/skipped) uses its persisted Status/Message verbatim —
// this is what survives job eviction and makes the view resumable. A target
// still pending/running falls back to the live job via configApplyJobStatus
// for a fresher read (the persisted row is only refreshed periodically by
// advanceConfigApplyBatch); a job already evicted at that point reads as
// failed/configApplyMsgJobLost, matching what the worker will persist. Done mirrors the
// batch's own persisted status rather than being re-derived from the
// targets, so it agrees exactly with when advanceConfigApplyBatch finalizes
// the batch.
func (s *Server) aggregateGroupApplyBatchStatus(batch storage.ConfigApplyBatchRecord, targets []storage.ConfigApplyBatchTargetRecord) groupApplyBatchStatusResponse {
	resp := groupApplyBatchStatusResponse{
		BatchID: batch.ID,
		Mode:    batch.Mode,
		Status:  batch.Status,
		Done:    batch.Status != storage.ConfigApplyBatchStatusRunning,
		Total:   len(targets),
		Agents:  make([]groupApplyAgentStatus, 0, len(targets)),
	}
	for _, tgt := range targets {
		status, message := tgt.Status, tgt.Message
		if !isTerminalConfigApplyTargetStatus(status) {
			status, message = s.configApplyJobStatus(tgt.JobID, tgt.AgentID)
		}
		switch status {
		case storage.ConfigApplyTargetStatusSucceeded:
			resp.Applied++
		case storage.ConfigApplyTargetStatusFailed:
			resp.Failed++
		case storage.ConfigApplyTargetStatusSkipped:
			resp.Skipped++
		default:
			resp.Pending++
		}
		resp.Agents = append(resp.Agents, groupApplyAgentStatus{
			AgentID: tgt.AgentID,
			JobID:   tgt.JobID,
			Status:  status,
			Message: message,
		})
	}
	return resp
}

// handleApplyAgentConfig applies the effective config target to a single
// in-scope agent as a persistent BATCH-OF-ONE (P3-3.4, audit #25a): creates a
// config_apply_batches row with no fleet-group scope, enqueues one config.apply
// job and immediately returns 202 + batch_id. Progress is observed via
// handleGetAgentApplyBatchStatus — the same mechanism as the group rollout; the
// old in-handler 500ms/5min poll is gone.
func (s *Server) handleApplyAgentConfig() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		_, user, err := s.requireSession(r)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		id := chi.URLParam(r, "id")
		if id == "" {
			writeError(w, http.StatusBadRequest, msgConfigTargetIDReq)
			return
		}
		// R-S-14: the agent must exist in the live snapshot and the
		// operator's fleet scope must cover its group before any work.
		existing, exists := s.live.Get(id)
		if !exists {
			writeError(w, http.StatusNotFound, msgAgentNotFound)
			return
		}
		scope, ok := s.requireFleetScope(w, r, user)
		if !ok {
			return
		}
		if !scope.IsAllowed(existing.FleetGroupID) {
			writeError(w, http.StatusNotFound, msgAgentNotFound)
			return
		}
		// fleetGroupID intentionally empty: the batch is agent-scoped and must
		// not surface as the group's "active batch" on the fleet-group page.
		batchID, err := s.createConfigApplyBatch(ctx, user.ID, "", storage.ConfigApplyBatchModeAllAtOnce, 1, []string{id})
		if err != nil {
			writeErrorLogged(ctx, w, http.StatusInternalServerError,
				"failed to enqueue config apply", err)
			return
		}
		writeJSON(w, http.StatusAccepted, groupApplyAcceptedResponse{BatchID: batchID})
	}
}

// handleGetAgentApplyBatchStatus returns the persisted-batch aggregate for a
// single-agent config apply. The batch is agent-scoped (fleet_group_id NULL),
// so membership is checked via its targets: the batch is readable through an
// agent URL only if its sole target is that agent; otherwise 404 (do not leak
// the existence of another agent's batch, symmetric with the group endpoint).
func (s *Server) handleGetAgentApplyBatchStatus() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		_, user, err := s.requireSession(r)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		id := chi.URLParam(r, "id")
		if id == "" {
			writeError(w, http.StatusBadRequest, msgConfigTargetIDReq)
			return
		}
		existing, exists := s.live.Get(id)
		if !exists {
			writeError(w, http.StatusNotFound, msgAgentNotFound)
			return
		}
		scope, ok := s.requireFleetScope(w, r, user)
		if !ok {
			return
		}
		if !scope.IsAllowed(existing.FleetGroupID) {
			writeError(w, http.StatusNotFound, msgAgentNotFound)
			return
		}
		batchID := chi.URLParam(r, "batchId")
		batch, targets, err := s.store.GetConfigApplyBatch(ctx, batchID)
		if err != nil {
			if errors.Is(err, storage.ErrNotFound) {
				writeError(w, http.StatusNotFound, msgConfigApplyBatchNotFound)
				return
			}
			writeErrorLogged(ctx, w, http.StatusInternalServerError, "failed to load config-apply batch", err)
			return
		}
		if len(targets) != 1 || targets[0].AgentID != id {
			writeError(w, http.StatusNotFound, msgConfigApplyBatchNotFound)
			return
		}
		writeJSON(w, http.StatusOK, s.aggregateGroupApplyBatchStatus(batch, targets))
	}
}
