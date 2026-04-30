package server

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/lost-coder/panvex/internal/controlplane/jobs"
	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

type updateAgentTransportRequest struct {
	TransportMode string `json:"transport_mode"`
	DialAddress   string `json:"dial_address,omitempty"`
}

// handleUpdateAgentTransportMode handles PUT /agents/{id}/transport-mode.
// It updates the agent's transport_mode + dial_address in the DB, enqueues a
// switch_transport_mode job for the agent, and notifies agenttransport.Manager
// to (de)spawn outbound supervisors accordingly.
func (s *Server) handleUpdateAgentTransportMode() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, user, err := s.requireSession(r)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}

		agentID := chi.URLParam(r, "id")
		if agentID == "" {
			writeError(w, http.StatusBadRequest, "missing agent id")
			return
		}

		var req updateAgentTransportRequest
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}

		req.TransportMode = strings.TrimSpace(req.TransportMode)
		req.DialAddress = strings.TrimSpace(req.DialAddress)

		if req.TransportMode != "inbound" && req.TransportMode != "outbound" {
			writeError(w, http.StatusBadRequest, "transport_mode must be inbound or outbound")
			return
		}
		if req.TransportMode == "outbound" && req.DialAddress == "" {
			writeError(w, http.StatusBadRequest, "dial_address required for outbound mode")
			return
		}

		// Verify the agent exists in memory and the caller can reach it.
		s.mu.RLock()
		existing, exists := s.agents[agentID]
		s.mu.RUnlock()
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

		// Persist to storage.
		if s.store != nil {
			dialAddr := req.DialAddress
			if req.TransportMode == "inbound" {
				dialAddr = ""
			}
			if err := s.store.UpdateAgentTransportMode(r.Context(), agentID, req.TransportMode, dialAddr); err != nil {
				if errors.Is(err, storage.ErrNotFound) {
					writeError(w, http.StatusNotFound, msgAgentNotFound)
					return
				}
				s.logger.Error("update agent transport mode in store failed", "error", err)
				writeError(w, http.StatusInternalServerError, msgStorageError)
				return
			}
		}

		// Map DB transport_mode to agent-level naming for the job payload.
		// DB "inbound"  → agent "dial"   (agent dials the panel)
		// DB "outbound" → agent "listen" (agent listens; panel dials it)
		agentMode := "dial"
		listenAddr := ""
		if req.TransportMode == "outbound" {
			agentMode = "listen"
			listenAddr = req.DialAddress
		}

		jobPayload, _ := json.Marshal(map[string]string{
			"mode":        agentMode,
			"listen_addr": listenAddr,
		})

		var idempotencyKey [16]byte
		_, _ = rand.Read(idempotencyKey[:])

		job, err := s.jobs.Enqueue(r.Context(), jobs.CreateJobInput{
			Action:         jobs.ActionSwitchTransportMode,
			TargetAgentIDs: []string{agentID},
			IdempotencyKey: hex.EncodeToString(idempotencyKey[:]),
			ActorID:        session.UserID,
			ReadOnlyAgents: s.readOnlyAgents([]string{agentID}),
			PayloadJSON:    string(jobPayload),
		}, s.now())
		if err != nil {
			s.logger.Error("enqueue switch_transport_mode job failed", "agent_id", agentID, "error", err)
			writeError(w, http.StatusInternalServerError, "failed to enqueue transport mode switch job")
			return
		}
		s.notifyAgentSessions([]string{agentID})

		// Notify the transport manager so outbound supervisors are
		// spawned or torn down immediately (best-effort: no error if nil).
		s.notifyTransportManager(agentID)

		s.appendAuditWithContext(r.Context(), session.UserID, "agents.update_transport_mode", agentID, map[string]any{
			"transport_mode": req.TransportMode,
			"dial_address":   req.DialAddress,
			"job_id":         job.ID,
		})

		w.WriteHeader(http.StatusNoContent)
	}
}
