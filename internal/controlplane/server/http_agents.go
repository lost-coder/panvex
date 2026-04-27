package server

import (
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/lost-coder/panvex/internal/controlplane/auth"
	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

type renameAgentRequest struct {
	NodeName string `json:"node_name"`
}

func (s *Server) handleRenameAgent() http.HandlerFunc {
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

		// Verify the agent exists in memory before touching the store so a
		// 404 does not leave an orphaned store update.
		s.mu.RLock()
		existing, exists := s.agents[agentID]
		s.mu.RUnlock()
		if !exists {
			writeError(w, http.StatusNotFound, msgAgentNotFound)
			return
		}

		// R-S-14: scope-check by the agent's fleet group.
		scope, ok := s.requireFleetScope(w, r, user)
		if !ok {
			return
		}
		if !scope.IsAllowed(existing.FleetGroupID) {
			writeError(w, http.StatusNotFound, msgAgentNotFound)
			return
		}

		if s.store != nil {
			if err := s.store.UpdateAgentNodeName(r.Context(), agentID, req.NodeName); err != nil {
				s.logger.Error("update agent node_name in store failed", "error", err)
				writeError(w, http.StatusInternalServerError, msgStorageError)
				return
			}
		}

		s.mu.Lock()
		agent, exists := s.agents[agentID]
		if !exists {
			s.mu.Unlock()
			writeError(w, http.StatusNotFound, msgAgentNotFound)
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

// agentDeregisterScope checks the URL/auth/scope preconditions for
// deregistering an agent and returns the chi-extracted agent id when allowed.
// On any failure it has already written the HTTP error response.
func (s *Server) agentDeregisterScope(w http.ResponseWriter, r *http.Request, user auth.User) (string, bool) {
	agentID := chi.URLParam(r, "id")
	if agentID == "" {
		writeError(w, http.StatusBadRequest, "missing agent id")
		return "", false
	}
	s.mu.RLock()
	preCheck, preExists := s.agents[agentID]
	s.mu.RUnlock()
	if !preExists {
		writeError(w, http.StatusNotFound, msgAgentNotFound)
		return "", false
	}
	scope, ok := s.requireFleetScope(w, r, user)
	if !ok {
		return "", false
	}
	if !scope.IsAllowed(preCheck.FleetGroupID) {
		writeError(w, http.StatusNotFound, msgAgentNotFound)
		return "", false
	}
	return agentID, true
}

// persistAgentDeregister wipes the agent's persistent state. Returns false
// after writing an HTTP error if a fatal storage operation failed; the
// caller must abort. Recovery-grant revocation and revocation persistence
// are best-effort.
func (s *Server) persistAgentDeregister(w http.ResponseWriter, r *http.Request, agentID string, agent Agent) bool {
	if s.store == nil {
		return true
	}
	if _, err := s.store.RevokeAgentCertificateRecoveryGrant(r.Context(), agentID, s.now()); err != nil && !errors.Is(err, storage.ErrNotFound) {
		s.logger.Error("revoke cert recovery grant failed", "agent_id", agentID, "error", err)
	}
	if err := s.store.DeleteInstancesByAgent(r.Context(), agentID); err != nil {
		s.logger.Error("delete instances by agent failed", "agent_id", agentID, "error", err)
		writeError(w, http.StatusInternalServerError, msgStorageError)
		return false
	}
	if err := s.store.DeleteAgent(r.Context(), agentID); err != nil && !errors.Is(err, storage.ErrNotFound) {
		s.logger.Error("delete agent from store failed", "agent_id", agentID, "error", err)
		writeError(w, http.StatusInternalServerError, msgStorageError)
		return false
	}
	// P1-SEC-06: persist the revocation so the ID stays rejected
	// across restarts until the underlying cert expires.
	certExpires := s.now().AddDate(0, 0, 30) // fallback to default lifetime if unknown
	if agent.CertExpiresAt != nil {
		certExpires = *agent.CertExpiresAt
	}
	if err := s.store.PutAgentRevocation(r.Context(), storage.AgentRevocationRecord{
		AgentID:       agentID,
		RevokedAt:     s.now().UTC(),
		CertExpiresAt: certExpires.UTC(),
	}); err != nil {
		s.logger.Error("persist agent revocation failed", "agent_id", agentID, "error", err)
		// Non-fatal: in-memory revocation below still blocks the
		// current process. Restart recovery will see this as a gap.
	}
	return true
}

// purgeAgentInMemory clears every in-memory map associated with the agent.
// Lock ordering: mu -> clientsMu.
func (s *Server) purgeAgentInMemory(agentID string) {
	s.mu.Lock()
	delete(s.agents, agentID)
	delete(s.detailBoosts, agentID)
	delete(s.initializationWatchCooldowns, agentID)
	delete(s.lastUsageSeq, agentID)
	for instID, inst := range s.instances {
		if inst.AgentID == agentID {
			delete(s.instances, instID)
		}
	}
	s.clientsMu.Lock()
	delete(s.clientUsage, agentID)
	s.clientsMu.Unlock()
	s.revokedAgentIDs[agentID] = struct{}{}
	s.mu.Unlock()
}

func (s *Server) handleDeregisterAgent() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, user, err := s.requireSession(r)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		agentID, ok := s.agentDeregisterScope(w, r, user)
		if !ok {
			return
		}

		// 1. Signal the gRPC stream to shut down by closing the done channel.
		//    This is safe against concurrent notifyAgentSession because the
		//    notify path checks done before sending to wake. The session
		//    manager (controlplane/agents.SessionManager) encapsulates the
		//    map + close(done) bookkeeping; see P3-ARCH-01a.
		s.sessions.Terminate(agentID)

		// 2. Verify agent exists before doing any work.
		s.mu.RLock()
		agent, exists := s.agents[agentID]
		s.mu.RUnlock()
		if !exists {
			writeError(w, http.StatusNotFound, msgAgentNotFound)
			return
		}

		// 3. Persist deletion to storage first so a failure does not leave
		//    the agent absent from memory but present in the database.
		if !s.persistAgentDeregister(w, r, agentID, agent) {
			return
		}

		// 4. Clean up in-memory state, including the revocation flag so a
		//    reconnect attempt with still-valid mTLS material is rejected
		//    at Connect.
		s.purgeAgentInMemory(agentID)

		// 5. Remove from presence tracker.
		s.presence.Remove(agentID)

		s.appendAuditWithContext(r.Context(), session.UserID, "agents.deregister", agentID, map[string]any{})

		w.WriteHeader(http.StatusNoContent)
	}
}

// Fleet-group HTTP handlers moved to http_fleet_groups.go as part of
// the groups redesign (UUID ids, CRUD + integrations). The previous
// handleFleetGroups derived the list from the live agent snapshot;
// the replacement reads the fleet_groups table so empty groups also
// appear in the list.
