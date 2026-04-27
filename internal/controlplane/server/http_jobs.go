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
		listed := s.jobs.ListRecentWithContext(r.Context(), parseListLimit(r))
		listed = s.filterJobsByScope(listed, scope)
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

// parseListLimit returns the operator-supplied ?limit= when present and
// positive, falling back to jobs.DefaultListRecentLimit. The store
// applies its own absolute cap, so this only relaxes between the
// default and the cap (Q2.U-P-13).
func parseListLimit(r *http.Request) int {
	raw := strings.TrimSpace(r.URL.Query().Get("limit"))
	if raw == "" {
		return jobs.DefaultListRecentLimit
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil || parsed <= 0 {
		return jobs.DefaultListRecentLimit
	}
	return parsed
}

// filterJobsByScope drops jobs whose target agents are entirely outside
// the operator's scope. Global scope is a no-op shortcut so the lock
// is only taken on the constrained path.
func (s *Server) filterJobsByScope(listed []jobs.Job, scope FleetScopeAccess) []jobs.Job {
	if scope.Global {
		return listed
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	filtered := listed[:0]
	for _, job := range listed {
		if s.jobHasInScopeTargetLocked(job, scope) {
			filtered = append(filtered, job)
		}
	}
	return filtered
}

func (s *Server) jobHasInScopeTargetLocked(job jobs.Job, scope FleetScopeAccess) bool {
	for _, agentID := range job.TargetAgentIDs {
		agent, ok := s.agents[agentID]
		if ok && scope.IsAllowed(agent.FleetGroupID) {
			return true
		}
	}
	return false
}

// targetsInScope reports whether every target agent is reachable for
// the operator. Global scope short-circuits the lookup.
func (s *Server) targetsInScope(targetIDs []string, scope FleetScopeAccess) bool {
	if scope.Global {
		return true
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, agentID := range targetIDs {
		agent, ok := s.agents[agentID]
		if !ok || !scope.IsAllowed(agent.FleetGroupID) {
			return false
		}
	}
	return true
}

// readOnlyAgents snapshots the ReadOnly flag for each requested target
// so the jobs service can refuse mutating actions against read-only
// agents without re-reading agent state.
func (s *Server) readOnlyAgents(targetIDs []string) map[string]bool {
	out := make(map[string]bool, len(targetIDs))
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, agentID := range targetIDs {
		if agent, ok := s.agents[agentID]; ok {
			out[agentID] = agent.ReadOnly
		}
	}
	return out
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
		if !s.targetsInScope(request.TargetAgentIDs, scope) {
			writeError(w, http.StatusForbidden, "target agent outside operator scope")
			return
		}

		readOnlyAgents := s.readOnlyAgents(request.TargetAgentIDs)

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
