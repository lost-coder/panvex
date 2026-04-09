package server

import (
	"log"
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

		if s.store != nil {
			if err := s.store.UpdateAgentNodeName(r.Context(), agentID, req.NodeName); err != nil {
				log.Printf("update agent node_name in store failed: %v", err)
			}
		}

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

		// 1. Close gRPC stream by removing the agent stream session.
		//    The wake channel must be closed while sessionMu is held to prevent
		//    a concurrent notifyAgentSession from sending to a closed channel.
		s.sessionMu.Lock()
		streamSession, hasStream := s.agentSessions[agentID]
		if hasStream {
			delete(s.agentSessions, agentID)
			if streamSession.wake != nil {
				close(streamSession.wake)
			}
		}
		s.sessionMu.Unlock()

		// 2. Revoke any pending certificate recovery grant.
		if s.store != nil {
			if _, err := s.store.RevokeAgentCertificateRecoveryGrant(r.Context(), agentID, s.now()); err != nil && err != storage.ErrNotFound {
				log.Printf("revoke cert recovery grant failed for %s: %v", agentID, err)
			}
		}

		// 3. Clean up in-memory state.
		s.mu.Lock()
		_, exists := s.agents[agentID]
		if !exists {
			s.mu.Unlock()
			writeError(w, http.StatusNotFound, "agent not found")
			return
		}
		delete(s.agents, agentID)
		delete(s.detailBoosts, agentID)
		delete(s.initializationWatchCooldowns, agentID)
		// Remove instances belonging to this agent.
		for instID, inst := range s.instances {
			if inst.AgentID == agentID {
				delete(s.instances, instID)
			}
		}
		// Remove client usage for this agent.
		delete(s.clientUsage, agentID)
		s.mu.Unlock()

		// 4. Remove from presence tracker.
		s.presence.Remove(agentID)

		// 5. Persist deletion to storage.
		if s.store != nil {
			if err := s.store.DeleteInstancesByAgent(r.Context(), agentID); err != nil {
				log.Printf("delete instances by agent failed for %s: %v", agentID, err)
			}
			if err := s.store.DeleteAgent(r.Context(), agentID); err != nil && err != storage.ErrNotFound {
				log.Printf("delete agent from store failed for %s: %v", agentID, err)
			}
		}

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
