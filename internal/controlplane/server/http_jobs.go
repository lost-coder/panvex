package server

import (
	"errors"
	"net/http"
	"time"

	"github.com/panvex/panvex/internal/controlplane/auth"
	"github.com/panvex/panvex/internal/controlplane/jobs"
)

type createJobRequest struct {
	Action         string   `json:"action"`
	TargetAgentIDs []string `json:"target_agent_ids"`
	IdempotencyKey string   `json:"idempotency_key"`
	TTLSeconds     int      `json:"ttl_seconds"`
}

func (s *Server) handleJobs() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, _, err := s.requireSession(r); err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}

		writeJSON(w, http.StatusOK, s.jobs.List())
	}
}

func (s *Server) handleCreateJob() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, user, err := s.requireSession(r)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}

		if user.Role == auth.RoleViewer {
			writeError(w, http.StatusForbidden, "viewer role cannot create jobs")
			return
		}

		var request createJobRequest
		if err := decodeJSON(r, &request); err != nil {
			writeError(w, http.StatusBadRequest, "invalid job payload")
			return
		}

		readOnlyAgents := make(map[string]bool, len(request.TargetAgentIDs))
		s.mu.RLock()
		for _, agentID := range request.TargetAgentIDs {
			agent, ok := s.agents[agentID]
			if ok {
				readOnlyAgents[agentID] = agent.ReadOnly
			}
		}
		s.mu.RUnlock()

		job, err := s.jobs.Enqueue(jobs.CreateJobInput{
			Action:         jobs.Action(request.Action),
			TargetAgentIDs: request.TargetAgentIDs,
			TTL:            time.Duration(request.TTLSeconds) * time.Second,
			IdempotencyKey: request.IdempotencyKey,
			ActorID:        session.UserID,
			ReadOnlyAgents: readOnlyAgents,
		}, s.now())
		if err != nil {
			switch {
			case errors.Is(err, jobs.ErrDuplicateIdempotencyKey):
				writeError(w, http.StatusConflict, err.Error())
			case errors.Is(err, jobs.ErrReadOnlyTarget):
				writeError(w, http.StatusConflict, err.Error())
			default:
				writeError(w, http.StatusBadRequest, err.Error())
			}
			return
		}
		s.notifyAgentSessions(job.TargetAgentIDs)

		s.appendAuditWithContext(r.Context(), session.UserID, "jobs.create", job.ID, map[string]any{
			"action":            request.Action,
			"target_agent_ids":  request.TargetAgentIDs,
			"idempotency_key":   request.IdempotencyKey,
			"requested_role":    user.Role,
			"requested_ttl_sec": request.TTLSeconds,
		})
		s.events.publish(eventEnvelope{
			Type: "jobs.created",
			Data: job,
		})
		writeJSON(w, http.StatusAccepted, job)
	}
}
