package server

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/auth"
	"github.com/lost-coder/panvex/internal/controlplane/eventbus"
	"github.com/lost-coder/panvex/internal/controlplane/jobs"
)

type createJobRequest struct {
	Action         string   `json:"action"`
	TargetAgentIDs []string `json:"target_agent_ids"`
	IdempotencyKey string   `json:"idempotency_key"`
	TTLSeconds     int      `json:"ttl_seconds"`
}

func (s *Server) handleJobs() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, user, err := s.requireSession(r)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}

		listed := s.jobs.List()

		// Redact payload for viewers to prevent leaking client secrets
		// embedded in job payloads for client mutation actions.
		if user.Role == auth.RoleViewer {
			for i := range listed {
				listed[i].PayloadJSON = ""
			}
		}

		writeJSON(w, http.StatusOK, listed)
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

		if !jobs.IsValidAction(jobs.Action(request.Action)) {
			writeError(w, http.StatusBadRequest, "unknown job action")
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

		idempotencyKey := request.IdempotencyKey
		if idempotencyKey == "" {
			var buf [16]byte
			_, _ = rand.Read(buf[:])
			idempotencyKey = hex.EncodeToString(buf[:])
		}

		job, err := s.jobs.Enqueue(jobs.CreateJobInput{
			Action:         jobs.Action(request.Action),
			TargetAgentIDs: request.TargetAgentIDs,
			TTL:            time.Duration(request.TTLSeconds) * time.Second,
			IdempotencyKey: idempotencyKey,
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
		s.events.Publish(eventbus.Event{
			Type: "jobs.created",
			Data: job,
		})
		writeJSON(w, http.StatusAccepted, job)
	}
}
