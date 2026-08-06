package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/lost-coder/panvex/internal/agent/telemtupdate"
	"github.com/lost-coder/panvex/internal/controlplane/jobs"
	"github.com/lost-coder/panvex/internal/controlplane/updates"
)

// telemtUpdateStrategyPayload is the wire shape of an
// updates.Strategy — used both as the PUT request body and nested in the
// GET response.
type telemtUpdateStrategyPayload struct {
	Mode        string `json:"mode"`
	RestartSpec string `json:"restart_spec"`
	BinaryPath  string `json:"binary_path"`
	AssetFlavor string `json:"asset_flavor"`
}

// telemtUpdateStrategyResponse is the GET response shape: the persisted
// strategy (nil when the agent has never had one configured) plus the
// agent's live probe (nil until the agent has sent at least one snapshot
// carrying it — see api.TelemtUpdateProbe / Task 7).
type telemtUpdateStrategyResponse struct {
	Strategy *telemtUpdateStrategyPayload `json:"strategy"`
	Probe    *TelemtUpdateProbe           `json:"probe"`
}

// handleGetTelemtUpdateStrategy returns the persisted Telemt update
// strategy for an agent (null when unconfigured) alongside its live probe,
// so the dashboard can render "not configured yet, but here's what we
// detected" without a second round trip.
func (s *Server) handleGetTelemtUpdateStrategy() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		agentID := chi.URLParam(r, "id")
		if agentID == "" {
			writeError(w, http.StatusBadRequest, "agent id is required")
			return
		}
		agent, exists := s.live.Get(agentID)
		if !exists {
			writeError(w, http.StatusNotFound, msgAgentNotFound)
			return
		}

		st, ok, err := s.telemtStrategySvc.Get(r.Context(), agentID)
		if err != nil {
			writeErrorLogged(r.Context(), w, http.StatusInternalServerError, "failed to read update strategy", err)
			return
		}

		resp := telemtUpdateStrategyResponse{Probe: agent.TelemtUpdateProbe}
		if ok {
			resp.Strategy = &telemtUpdateStrategyPayload{
				Mode:        st.Mode,
				RestartSpec: st.RestartSpec,
				BinaryPath:  st.BinaryPath,
				AssetFlavor: st.AssetFlavor,
			}
		}
		writeJSON(w, http.StatusOK, resp)
	}
}

// handlePutTelemtUpdateStrategy validates and persists the Telemt update
// strategy for an agent. Admin-only (see routes.go) — the strategy governs
// which local command the panel will later run to restart telemt after a
// binary swap, so it carries the same trust bar as the panel/agent update
// settings it complements.
func (s *Server) handlePutTelemtUpdateStrategy() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, _, err := s.requireSession(r)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}

		agentID := chi.URLParam(r, "id")
		if agentID == "" {
			writeError(w, http.StatusBadRequest, "agent id is required")
			return
		}
		if _, exists := s.live.Get(agentID); !exists {
			writeError(w, http.StatusNotFound, msgAgentNotFound)
			return
		}

		var req telemtUpdateStrategyPayload
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request payload")
			return
		}

		st := updates.Strategy{
			Mode:        req.Mode,
			RestartSpec: req.RestartSpec,
			BinaryPath:  req.BinaryPath,
			AssetFlavor: req.AssetFlavor,
		}
		if err := s.telemtStrategySvc.Put(r.Context(), agentID, st); err != nil {
			if errors.Is(err, updates.ErrInvalidStrategy) {
				writeError(w, http.StatusBadRequest, err.Error())
				return
			}
			writeErrorLogged(r.Context(), w, http.StatusInternalServerError, "failed to save update strategy", err)
			return
		}

		s.appendAuditWithContext(r.Context(), session.UserID, "agents.update_strategy.set", agentID, map[string]any{
			"mode": st.Mode,
		})
		w.WriteHeader(http.StatusNoContent)
	}
}

// telemtUpdateJobTTL bounds how long a single telemt.update job may stay
// outstanding before its target expires. Mirrors agentSelfUpdateJobTTL's
// reasoning: a binary swap goes through download + preflight + swap +
// supervised restart + health-gate (telemtupdate.Execute's health-gate alone
// budgets up to 60s in production), so 10 minutes comfortably covers the
// whole sequence plus retries without leaving a wedged job outstanding
// forever.
const telemtUpdateJobTTL = 10 * time.Minute

// dispatchTelemtUpdateRequest is the JSON payload accepted by
// POST /api/agents/{id}/telemt/update. Version empty means "use the latest
// known Telemt release" (State.TelemtLatestVersion).
type dispatchTelemtUpdateRequest struct {
	Version        string `json:"version,omitempty"`
	AllowDowngrade bool   `json:"allow_downgrade,omitempty"`
}

// dispatchTelemtUpdateResponse is the 202 body returned by
// POST /api/agents/{id}/telemt/update.
type dispatchTelemtUpdateResponse struct {
	JobID string `json:"job_id"`
}

// buildTelemtUpdateJobPayload assembles the telemt.update job payload from
// the agent's configured strategy and the resolved target version. Marshals
// the actual telemtupdate.Payload struct (not a hand-built map) so the wire
// JSON carries EXACTLY its six keys by construction — the contract Task 5
// fixed and Task 12's agent-side handler decodes against.
func buildTelemtUpdateJobPayload(strategy updates.Strategy, version string, allowDowngrade bool) (string, error) {
	payload := telemtupdate.Payload{
		Version:        version,
		ReleaseBaseURL: updates.TelemtReleaseBaseURLForVersion(version),
		AllowDowngrade: allowDowngrade,
		RestartSpec:    strategy.RestartSpec,
		BinaryPath:     strategy.BinaryPath,
		AssetFlavor:    strategy.AssetFlavor,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// telemtUpdateJobInput builds the CreateJobInput for one agent telemt.update
// dispatch. Extracted so the TTL/action contract is unit-testable without the
// HTTP handler scaffolding (mirrors agentSelfUpdateJobInput).
func telemtUpdateJobInput(agentID, payloadJSON, actorID string) jobs.CreateJobInput {
	return jobs.CreateJobInput{
		Action:         jobs.ActionTelemtUpdate,
		TargetAgentIDs: []string{agentID},
		TTL:            telemtUpdateJobTTL,
		PayloadJSON:    payloadJSON,
		ActorID:        actorID,
	}
}

// handleDispatchTelemtUpdate enqueues a telemt.update job for one agent's
// managed Telemt install. Guard chain (each a 409 with a typed `code` the
// dashboard can branch on, checked in this fixed order):
//
//  1. strategy_not_configured — no updates.Strategy has been PUT for this
//     agent yet (see handlePutTelemtUpdateStrategy).
//  2. mode_not_binary — the configured strategy's Mode is "docker" or
//     "none": no in-place binary swap path exists.
//  3. update_unavailable — the agent's live probe reports it cannot offer
//     an in-place update right now (never reported, or Available=false).
//  4. no_known_release — no version was given in the request AND the panel
//     has never successfully checked the Telemt release feed.
//
// On success: builds the job payload from the strategy + resolved version,
// enqueues telemt.update, records the desired state (SetPendingTelemtUpdate)
// so an offline/flapping node still converges on reconnect, and responds 202
// with the job id. Admin-only (see routes.go) — same trust bar as PUT
// telemt/update-strategy: this actually dispatches the binary swap that
// strategy governs.
func (s *Server) handleDispatchTelemtUpdate() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		agentID := chi.URLParam(r, "id")
		if agentID == "" {
			writeError(w, http.StatusBadRequest, "agent id is required")
			return
		}
		agent, exists := s.live.Get(agentID)
		if !exists {
			writeError(w, http.StatusNotFound, msgAgentNotFound)
			return
		}

		var req dispatchTelemtUpdateRequest
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request payload")
			return
		}

		strategy, ok, err := s.telemtStrategySvc.Get(r.Context(), agentID)
		if err != nil {
			writeErrorLogged(r.Context(), w, http.StatusInternalServerError, "failed to read update strategy", err)
			return
		}
		if !ok {
			writeErrorWithCode(w, http.StatusConflict,
				"no Telemt update strategy is configured for this agent", "strategy_not_configured")
			return
		}
		if strategy.Mode != updates.StrategyModeBinary {
			writeErrorWithCode(w, http.StatusConflict,
				"the configured update strategy does not support an in-place binary swap", "mode_not_binary")
			return
		}
		if agent.TelemtUpdateProbe == nil || !agent.TelemtUpdateProbe.Available {
			writeErrorWithCode(w, http.StatusConflict,
				"the agent reports an in-place Telemt update is not available", "update_unavailable")
			return
		}

		version := strings.TrimPrefix(strings.TrimSpace(req.Version), "v")
		if version == "" {
			s.settingsMu.RLock()
			version = s.updateState.TelemtLatestVersion
			s.settingsMu.RUnlock()
		}
		if version == "" {
			writeErrorWithCode(w, http.StatusConflict,
				"no known Telemt release; check for updates first or specify a version", "no_known_release")
			return
		}

		payloadJSON, err := buildTelemtUpdateJobPayload(strategy, version, req.AllowDowngrade)
		if err != nil {
			s.logger.ErrorContext(r.Context(), "telemt update: build payload failed", "agent_id", agentID, "error", err)
			writeError(w, http.StatusInternalServerError, "failed to build update payload")
			return
		}

		// Never discard the requireSession error — an empty ActorID would
		// make the action untraceable. Fail closed (mirrors handleAgentUpdate).
		session, _, err := s.requireSession(r)
		if err != nil {
			s.logger.WarnContext(r.Context(), "telemt update: session check failed in handler body", "error", err)
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}

		job, err := s.jobs.Enqueue(r.Context(), telemtUpdateJobInput(agentID, payloadJSON, session.UserID), s.now())
		if err != nil {
			s.logger.ErrorContext(r.Context(), "telemt update: enqueue job failed", "agent_id", agentID, "error", err)
			writeError(w, http.StatusInternalServerError, "failed to create update job")
			return
		}

		// Record the desired state so an offline/flapping node still converges
		// on reconnect — the job itself is one-shot and expires unescalated.
		// Best-effort: the job is already enqueued, so a failure here only
		// costs the reconcile safety net.
		if err := s.updatesSvc.SetPendingTelemtUpdate(r.Context(), agentID, version); err != nil {
			s.logger.WarnContext(r.Context(), "telemt update: persist pending target failed",
				"agent_id", agentID, "error", err)
		}

		s.notifyAgentSessions(job.TargetAgentIDs)
		s.publishJobCreated(job)

		writeJSON(w, http.StatusAccepted, dispatchTelemtUpdateResponse{JobID: job.ID})
	}
}
