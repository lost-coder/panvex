package server

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"strconv"
	"strings"
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

		// R-S-14: load the operator's scope so the response can be
		// narrowed to jobs whose targets sit inside their visible
		// fleet groups.
		scope, ok := s.requireFleetScope(w, r, user)
		if !ok {
			return
		}

		// Q2.U-P-13: cap the response to the most recent N jobs so the
		// payload stays bounded as the table grows. ?limit= can override
		// up to 5x the default but never disables the cap.
		limit := jobs.DefaultListRecentLimit
		if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
			if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
				limit = parsed
			}
		}
		listed := s.jobs.ListRecentWithContext(r.Context(), limit)

		// R-S-14: drop jobs whose target agents are entirely outside
		// scope. A job is visible if at least one target is in scope —
		// the per-target deployment view will already be redacted by
		// the deployment endpoints.
		if !scope.Global {
			s.mu.RLock()
			filtered := listed[:0]
			for _, job := range listed {
				keep := false
				for _, agentID := range job.TargetAgentIDs {
					if agent, agentOK := s.agents[agentID]; agentOK && scope.IsAllowed(agent.FleetGroupID) {
						keep = true
						break
					}
				}
				if keep {
					filtered = append(filtered, job)
				}
			}
			s.mu.RUnlock()
			listed = filtered
		}

		// Q2.U-S-07: redact PayloadJSON for ALL roles in the list
		// endpoint. Mutating jobs (rollout_client_config, rotate_secret)
		// embed client secrets in the payload; admins and operators do
		// not need them for routine browsing. Internal dispatch keeps
		// the payload via grpc_gateway.go and the per-action handlers
		// in clients_flow.go which read job.PayloadJSON directly from
		// the in-memory store, never from the HTTP response.
		for i := range listed {
			listed[i].PayloadJSON = ""
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

		// R-S-14: every target agent must sit inside the operator's
		// scope. We deny the whole request if any one falls out so an
		// operator cannot accidentally fire a job into a fleet they
		// don't manage.
		scope, ok := s.requireFleetScope(w, r, user)
		if !ok {
			return
		}
		if !scope.Global {
			s.mu.RLock()
			outOfScope := false
			for _, agentID := range request.TargetAgentIDs {
				agent, agentOK := s.agents[agentID]
				if !agentOK || !scope.IsAllowed(agent.FleetGroupID) {
					outOfScope = true
					break
				}
			}
			s.mu.RUnlock()
			if outOfScope {
				writeError(w, http.StatusForbidden, "target agent outside operator scope")
				return
			}
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

		job, err := s.jobs.Enqueue(r.Context(), jobs.CreateJobInput{
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
