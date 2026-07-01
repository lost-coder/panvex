package server

import (
	"context"
	"encoding/json"
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
	configApplyJobTTL = 5 * time.Minute
	// configApplyPollInterval is how often the apply handler polls the job
	// store for the target's terminal status.
	configApplyPollInterval = 500 * time.Millisecond
	// configApplyPollGrace is added to the job TTL to form the wait deadline,
	// so a target that expires at the TTL boundary is observed before we bail.
	configApplyPollGrace = 30 * time.Second
)

// configApplyResponse is the JSON shape returned by the single-agent apply
// handler. It summarizes the rolling fan-out: how many agents applied
// successfully, the agent id that failed (empty when none), and a
// human-readable error string ("" when the rollout completed cleanly).
type configApplyResponse struct {
	Applied int    `json:"applied"`
	Failed  string `json:"failed"`
	Error   string `json:"error"`
}

// groupApplyAcceptedResponse is the 202 body returned by the ASYNC group
// apply handler. Instead of blocking on K × TTL, the handler enqueues one
// config.apply job per in-scope agent and returns immediately. The client
// correlates the rollout via batch_id (a display token) and polls the
// status endpoint with the returned job ids. jobs[i] pairs each agent with
// the job enqueued for it — an agent whose effective config was empty
// carries an empty job_id (nothing to apply, already in sync).
type groupApplyAcceptedResponse struct {
	BatchID string                `json:"batch_id"`
	Jobs    []groupApplyJobHandle `json:"jobs"`
}

// groupApplyJobHandle pairs an in-scope agent with the config.apply job
// enqueued for it. JobID is empty when the agent resolved an empty
// effective config (no-op, treated as already succeeded by the poller).
type groupApplyJobHandle struct {
	AgentID string `json:"agent_id"`
	JobID   string `json:"job_id"`
}

// groupApplyStatusResponse is the aggregate returned by the status endpoint.
// Done is true once every target reached a terminal state; Failed counts the
// terminally-unsuccessful targets so the UI can surface a PARTIAL rollout
// (some succeeded, some failed) rather than masking it.
type groupApplyStatusResponse struct {
	Done    bool                    `json:"done"`
	Total   int                     `json:"total"`
	Applied int                     `json:"applied"`
	Failed  int                     `json:"failed"`
	Pending int                     `json:"pending"`
	Agents  []groupApplyAgentStatus `json:"agents"`
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

// errString renders err as a string, or "" when err is nil. Used to fill the
// apply response's error field.
func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
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
	return job.ID, nil
}

// applyConfigToAgent resolves the agent's effective config target and applies
// it by enqueueing a config.apply job, then BLOCKS until that job's target
// reaches a terminal status. Returns nil on success, an error on
// failure/timeout. A no-op (nil) when the effective config is empty. Used by
// the SYNCHRONOUS single-agent apply path, which blocks on exactly one job.
func (s *Server) applyConfigToAgent(ctx context.Context, actorID, agentID string) error {
	jobID, err := s.enqueueConfigApplyJob(ctx, actorID, agentID)
	if err != nil {
		return err
	}
	if jobID == "" {
		return nil
	}
	return s.waitJobTargetTerminal(ctx, jobID, agentID, "config.apply")
}

// waitJobTargetTerminal polls the job until its target for agentID reaches a
// terminal status or ctx/the deadline fires. `action` only labels the error
// messages so callers (config.apply, runtime.restart) read clearly; on
// failure the agent's own ResultText is surfaced verbatim.
func (s *Server) waitJobTargetTerminal(ctx context.Context, jobID, agentID, action string) error {
	ticker := time.NewTicker(configApplyPollInterval)
	defer ticker.Stop()
	deadline := time.NewTimer(configApplyJobTTL + configApplyPollGrace)
	defer deadline.Stop()
	for {
		if job, ok := s.jobs.Get(jobID); ok {
			for _, tgt := range job.Targets {
				if targetAgentID(tgt) != agentID {
					continue
				}
				switch tgt.Status {
				case jobs.TargetStatusSucceeded:
					return nil
				case jobs.TargetStatusFailed:
					return fmt.Errorf("%s failed on %s: %s", action, agentID, tgt.ResultText)
				case jobs.TargetStatusExpired:
					return fmt.Errorf("%s expired on %s", action, agentID)
				}
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return fmt.Errorf("%s timed out on %s", action, agentID)
		case <-ticker.C:
		}
	}
}

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
		handles := make([]groupApplyJobHandle, 0, len(agentIDs))
		for _, agentID := range agentIDs {
			jobID, err := s.enqueueConfigApplyJob(ctx, user.ID, agentID)
			if err != nil {
				writeErrorLogged(ctx, w, http.StatusInternalServerError,
					"failed to enqueue config apply", err)
				return
			}
			handles = append(handles, groupApplyJobHandle{AgentID: agentID, JobID: jobID})
		}
		writeJSON(w, http.StatusAccepted, groupApplyAcceptedResponse{
			BatchID: newGroupApplyBatchID(s.now()),
			Jobs:    handles,
		})
	}
}

// newGroupApplyBatchID mints a display-only correlation token for a group
// apply. The job ids are the real source of truth (the status endpoint
// aggregates over them); the batch id only gives the UI a stable handle for
// the in-flight rollout, so it is derived from the panel clock without
// touching the jobs store.
func newGroupApplyBatchID(now time.Time) string {
	return fmt.Sprintf("cfgapply-%d", now.UTC().UnixNano())
}

// handleGroupConfigApplyStatus aggregates the per-agent state of an in-flight
// group config apply. The client passes the job ids it received from the 202
// response as repeated ?job= query params (agent ids ride along as ?agent= in
// the same order so a no-op agent — empty job id — is still represented). The
// handler looks each job up in the jobs store and folds the matching target's
// status into pending / running / succeeded / failed, returning the aggregate
// plus a done flag once every target is terminal. Partial failure is
// first-class: applied and failed counts are reported independently.
func (s *Server) handleGroupConfigApplyStatus() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
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
		query := r.URL.Query()
		jobIDs := query["job"]
		agentIDs := query["agent"]
		resp := s.aggregateGroupApplyStatus(agentIDs, jobIDs)
		writeJSON(w, http.StatusOK, resp)
	}
}

// aggregateGroupApplyStatus folds the paired agent/job id slices into the
// status response. An empty job id is a no-op agent (empty effective config),
// counted as succeeded. A job that is no longer resident in the store (evicted
// after completing, or never found) is also treated as succeeded so a slow
// poller does not wedge on a job that already terminated and rolled off.
func (s *Server) aggregateGroupApplyStatus(agentIDs, jobIDs []string) groupApplyStatusResponse {
	resp := groupApplyStatusResponse{
		Done:   true,
		Agents: make([]groupApplyAgentStatus, 0, len(jobIDs)),
	}
	for i, jobID := range jobIDs {
		agentID := ""
		if i < len(agentIDs) {
			agentID = agentIDs[i]
		}
		row := groupApplyAgentStatus{AgentID: agentID, JobID: jobID, Status: applyStatusSucceeded}
		if jobID != "" {
			row.Status, row.Message = s.groupApplyTargetStatus(jobID, agentID)
		}
		switch row.Status {
		case applyStatusSucceeded:
			resp.Applied++
		case applyStatusFailed:
			resp.Failed++
		default:
			resp.Pending++
			resp.Done = false
		}
		resp.Agents = append(resp.Agents, row)
	}
	resp.Total = len(resp.Agents)
	return resp
}

// groupApplyTargetStatus resolves a single job/agent pair to a per-agent apply
// status and message. A job absent from the store is treated as succeeded (it
// terminated and rolled off before the poll landed). Failed/expired targets
// surface the agent's own ResultText.
func (s *Server) groupApplyTargetStatus(jobID, agentID string) (status, message string) {
	job, ok := s.jobs.Get(jobID)
	if !ok {
		return applyStatusSucceeded, ""
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

// handleApplyAgentConfig applies the effective config target to a single
// in-scope agent, blocking on the config.apply job's terminal status.
func (s *Server) handleApplyAgentConfig() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
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
		res := rollingApply(r.Context(), []string{id}, func(c context.Context, a string) error {
			return s.applyConfigToAgent(c, user.ID, a)
		})
		writeJSON(w, http.StatusOK, configApplyResponse{
			Applied: res.Applied,
			Failed:  res.Failed,
			Error:   errString(res.Err),
		})
	}
}
