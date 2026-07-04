package server

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/lost-coder/panvex/internal/controlplane/jobs"
)

// runtimeRestartJobTTL bounds how long a runtime.restart job may stay
// deliverable. A restart is a quick `systemctl restart`-style operation, so a
// short window is enough — past it the agent was almost certainly offline.
const runtimeRestartJobTTL = 2 * time.Minute

type restartAgentResponse struct {
	AgentID string `json:"agent_id"`
	Status  string `json:"status"`
}

// handleRestartAgent enqueues a runtime.restart job for one agent and waits for
// the target to reach a terminal state so the operator gets an immediate
// success/failure (e.g. "restart not available" when the agent has no restart
// strategy). Restarting the local Telemt process is the one-tap remediation
// surfaced from the dashboard "needs attention" alert and the server detail
// page. The agent must be in the live snapshot (a restart can only be
// delivered to a connected agent) and within the operator's fleet scope.
func (s *Server) handleRestartAgent() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, user, err := s.requireSession(r)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		id := chi.URLParam(r, "id")
		if id == "" {
			writeError(w, http.StatusBadRequest, msgAgentNotFound)
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

		job, err := s.jobs.Enqueue(r.Context(), jobs.CreateJobInput{
			Action:         jobs.ActionRuntimeRestart,
			TargetAgentIDs: []string{id},
			TTL:            runtimeRestartJobTTL,
			ActorID:        user.ID,
			ReadOnlyAgents: s.readOnlyAgents([]string{id}),
		}, s.now())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "enqueue restart failed")
			return
		}
		s.notifyAgentSessions(job.TargetAgentIDs)
		s.publishJobCreated(job)

		if err := s.waitJobTargetTerminal(r.Context(), job.ID, id, "runtime.restart"); err != nil {
			// The agent ran the job but reported failure (e.g. no restart
			// strategy configured), or it timed out — surface the reason.
			writeError(w, http.StatusBadGateway, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, restartAgentResponse{AgentID: id, Status: "restarted"})
	}
}

// waitJobTargetTerminal polls the job until its target for agentID reaches a
// terminal status or ctx/the deadline fires. `action` only labels the error
// messages so callers read clearly; on failure the agent's own ResultText is
// surfaced verbatim. P3-3.4: config.apply is now async (batch-of-one), so the
// sole remaining caller is the synchronous runtime.restart wait above; the
// poll constants live in http_config_apply.go (same package).
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
