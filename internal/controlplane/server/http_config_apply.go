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

// configApplyResponse is the JSON shape returned by both apply handlers. It
// summarizes the rolling fan-out: how many agents applied successfully, the
// agent id that failed (empty when none), and a human-readable error string
// ("" when the rollout completed cleanly).
type configApplyResponse struct {
	Applied int    `json:"applied"`
	Failed  string `json:"failed"`
	Error   string `json:"error"`
}

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

// applyConfigToAgent resolves the agent's effective config target and applies
// it by enqueueing a config.apply job, then BLOCKS until that job's target
// reaches a terminal status. Returns nil on success, an error on
// failure/timeout. A no-op (nil) when the effective config is empty.
func (s *Server) applyConfigToAgent(ctx context.Context, actorID, agentID string) error {
	effective := s.effectiveConfigForAgent(ctx, agentID)
	if len(effective) == 0 {
		return nil
	}
	payload, err := json.Marshal(configApplyJobPayload{
		Patch:          effective,
		HealthTimeoutS: configApplyHealthTimeoutSec,
	})
	if err != nil {
		return fmt.Errorf("marshal config.apply payload: %w", err)
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
		return fmt.Errorf("enqueue config.apply: %w", err)
	}
	s.notifyAgentSessions(job.TargetAgentIDs)
	return s.waitJobTargetTerminal(ctx, job.ID, agentID)
}

// waitJobTargetTerminal polls the job until its target for agentID reaches a
// terminal status or ctx/the deadline fires.
func (s *Server) waitJobTargetTerminal(ctx context.Context, jobID, agentID string) error {
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
					return fmt.Errorf("config.apply failed on %s: %s", agentID, tgt.ResultText)
				case jobs.TargetStatusExpired:
					return fmt.Errorf("config.apply expired on %s", agentID)
				}
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return fmt.Errorf("config.apply timed out on %s", agentID)
		case <-ticker.C:
		}
	}
}

// handleApplyGroupConfig applies the effective config target to every in-scope
// agent in a fleet group, one at a time (stop-on-failure). The fan-out blocks
// on each agent's config.apply job reaching a terminal status before moving on.
func (s *Server) handleApplyGroupConfig() http.HandlerFunc {
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
		res := rollingApply(r.Context(), agentIDs, func(c context.Context, a string) error {
			return s.applyConfigToAgent(c, user.ID, a)
		})
		writeJSON(w, http.StatusOK, configApplyResponse{
			Applied: res.Applied,
			Failed:  res.Failed,
			Error:   errString(res.Err),
		})
	}
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
