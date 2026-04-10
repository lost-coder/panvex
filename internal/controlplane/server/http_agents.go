package server

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/panvex/panvex/internal/controlplane/storage"
)

type renameAgentRequest struct {
	NodeName string `json:"node_name"`
}

func (s *Server) handleRenameAgent() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, _, err := s.requireSession(r)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}

		agentID := chi.URLParam(r, "id")
		if agentID == "" {
			writeError(w, http.StatusBadRequest, "missing agent id")
			return
		}

		var req renameAgentRequest
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}

		req.NodeName = strings.TrimSpace(req.NodeName)
		if req.NodeName == "" {
			writeError(w, http.StatusBadRequest, "node_name is required")
			return
		}

		// Persist to storage first so a failure does not leave in-memory and
		// persistent state diverged.
		if s.store != nil {
			if err := s.store.UpdateAgentNodeName(r.Context(), agentID, req.NodeName); err != nil {
				s.logger.Error("update agent node_name in store failed", "error", err)
				writeError(w, http.StatusInternalServerError, "storage error")
				return
			}
		}

		s.mu.Lock()
		agent, exists := s.agents[agentID]
		if !exists {
			s.mu.Unlock()
			writeError(w, http.StatusNotFound, "agent not found")
			return
		}
		oldName := agent.NodeName
		agent.NodeName = req.NodeName
		s.agents[agentID] = agent
		s.mu.Unlock()

		s.appendAuditWithContext(r.Context(), session.UserID, "agents.rename", agentID, map[string]any{
			"old_name": oldName,
			"new_name": req.NodeName,
		})

		writeJSON(w, http.StatusOK, agent)
	}
}

func (s *Server) handleDeregisterAgent() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, _, err := s.requireSession(r)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}

		agentID := chi.URLParam(r, "id")
		if agentID == "" {
			writeError(w, http.StatusBadRequest, "missing agent id")
			return
		}

		// 1. Signal the gRPC stream to shut down by closing the done channel.
		//    This is safe against concurrent notifyAgentSession because the
		//    notify path checks done before sending to wake.
		s.sessionMu.Lock()
		streamSession, hasStream := s.agentSessions[agentID]
		if hasStream {
			delete(s.agentSessions, agentID)
			if streamSession.done != nil {
				close(streamSession.done)
			}
		}
		s.sessionMu.Unlock()

		// 2. Verify agent exists before doing any work.
		s.mu.RLock()
		_, exists := s.agents[agentID]
		s.mu.RUnlock()
		if !exists {
			writeError(w, http.StatusNotFound, "agent not found")
			return
		}

		// 3. Persist deletion to storage first so a failure does not leave
		//    the agent absent from memory but present in the database.
		if s.store != nil {
			if _, err := s.store.RevokeAgentCertificateRecoveryGrant(r.Context(), agentID, s.now()); err != nil && err != storage.ErrNotFound {
				s.logger.Error("revoke cert recovery grant failed", "agent_id", agentID, "error", err)
			}
			if err := s.store.DeleteInstancesByAgent(r.Context(), agentID); err != nil {
				s.logger.Error("delete instances by agent failed", "agent_id", agentID, "error", err)
				writeError(w, http.StatusInternalServerError, "storage error")
				return
			}
			if err := s.store.DeleteAgent(r.Context(), agentID); err != nil && err != storage.ErrNotFound {
				s.logger.Error("delete agent from store failed", "agent_id", agentID, "error", err)
				writeError(w, http.StatusInternalServerError, "storage error")
				return
			}
		}

		// 4. Clean up in-memory state only after storage succeeds.
		s.mu.Lock()
		delete(s.agents, agentID)
		delete(s.detailBoosts, agentID)
		delete(s.initializationWatchCooldowns, agentID)
		for instID, inst := range s.instances {
			if inst.AgentID == agentID {
				delete(s.instances, instID)
			}
		}
		delete(s.clientUsage, agentID)
		s.mu.Unlock()

		// 5. Remove from presence tracker.
		s.presence.Remove(agentID)

		s.appendAuditWithContext(r.Context(), session.UserID, "agents.deregister", agentID, map[string]any{})

		w.WriteHeader(http.StatusNoContent)
	}
}

func (s *Server) handleFleetGroups() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, _, err := s.requireSession(r); err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}

		s.mu.RLock()
		groupSet := make(map[string]int)
		for _, agent := range s.agents {
			if agent.FleetGroupID != "" {
				groupSet[agent.FleetGroupID]++
			}
		}
		s.mu.RUnlock()

		type fleetGroupEntry struct {
			ID         string `json:"id"`
			AgentCount int    `json:"agent_count"`
		}
		groups := make([]fleetGroupEntry, 0, len(groupSet))
		for id, count := range groupSet {
			groups = append(groups, fleetGroupEntry{ID: id, AgentCount: count})
		}

		writeJSON(w, http.StatusOK, groups)
	}
}
