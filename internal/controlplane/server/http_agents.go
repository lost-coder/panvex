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

type updateAgentFleetGroupRequest struct {
	FleetGroupID string `json:"fleet_group_id"`
}

func (s *Server) handleRenameAgent() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, user, err := s.requireSession(r)
		if err != nil {
			writeErrorLogged(r.Context(), w, http.StatusUnauthorized, "unauthorized", err)
			return
		}

		agentID, nodeName, ok := decodeRenameAgentRequest(w, r)
		if !ok {
			return
		}

		if !s.checkRenameAgentScope(w, r, user, agentID) {
			return
		}

		if !s.persistAgentNodeName(w, r, agentID, nodeName) {
			return
		}

		agent, oldName, ok := s.applyAgentRename(w, agentID, nodeName)
		if !ok {
			return
		}

		s.appendAuditWithContext(r.Context(), session.UserID, "agents.rename", agentID, map[string]any{
			"old_name": oldName,
			"new_name": nodeName,
		})

		writeJSON(w, http.StatusOK, agent)
	}
}

// decodeRenameAgentRequest extracts and validates the agent id + new
// node name. Writes a 400 / response on failure and returns ok=false.
func decodeRenameAgentRequest(w http.ResponseWriter, r *http.Request) (string, string, bool) {
	agentID := chi.URLParam(r, "id")
	if agentID == "" {
		writeError(w, http.StatusBadRequest, "missing agent id")
		return "", "", false
	}

	var req renameAgentRequest
	if err := decodeJSON(r, &req); err != nil {
		writeErrorLogged(r.Context(), w, http.StatusBadRequest, "invalid request body", err)
		return "", "", false
	}

	nodeName := strings.TrimSpace(req.NodeName)
	if nodeName == "" {
		writeError(w, http.StatusBadRequest, "node_name is required")
		return "", "", false
	}
	return agentID, nodeName, true
}

// checkRenameAgentScope confirms the target agent exists in memory and
// the caller's fleet scope covers it. R-S-14: scope-check before any
// write so an out-of-scope rename leaks no information.
func (s *Server) checkRenameAgentScope(w http.ResponseWriter, r *http.Request, user auth.User, agentID string) bool {
	// Verify the agent exists in memory before touching the store so a
	// 404 does not leave an orphaned store update.
	s.mu.RLock()
	existing, exists := s.agents[agentID]
	s.mu.RUnlock()
	if !exists {
		writeError(w, http.StatusNotFound, msgAgentNotFound)
		return false
	}

	scope, ok := s.requireFleetScope(w, r, user)
	if !ok {
		return false
	}
	if !scope.IsAllowed(existing.FleetGroupID) {
		writeError(w, http.StatusNotFound, msgAgentNotFound)
		return false
	}
	return true
}

// persistAgentNodeName writes the new node name to storage when one is
// configured, returning false (after writing the HTTP error) on any
// storage failure.
func (s *Server) persistAgentNodeName(w http.ResponseWriter, r *http.Request, agentID, nodeName string) bool {
	if s.store == nil {
		return true
	}
	if err := s.store.UpdateAgentNodeName(r.Context(), agentID, nodeName); err != nil {
		s.logger.Error("update agent node_name in store failed", "error", err)
		writeError(w, http.StatusInternalServerError, msgStorageError)
		return false
	}
	return true
}

// applyAgentRename mutates the in-memory agent record under the write
// lock. Returns the updated record + previous name, or ok=false (after
// writing the 404) when the agent disappeared between checks.
func (s *Server) applyAgentRename(w http.ResponseWriter, agentID, nodeName string) (Agent, string, bool) {
	s.mu.Lock()
	agent, exists := s.agents[agentID]
	if !exists {
		s.mu.Unlock()
		writeError(w, http.StatusNotFound, msgAgentNotFound)
		return Agent{}, "", false
	}
	oldName := agent.NodeName
	agent.NodeName = nodeName
	s.agents[agentID] = agent
	s.mu.Unlock()
	return agent, oldName, true
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
	// Remove every (clientID, agentID) usage entry that belongs to this
	// agent. The previous implementation called delete(s.clientUsage,
	// agentID) which used the wrong outer key — clientUsage is keyed by
	// clientID. The agentClientUsage reverse index (P-11) records exactly
	// the clientIDs this agent owns gauges for, so we can iterate the
	// small set rather than the full clientUsage map.
	for clientID := range s.agentClientUsage[agentID] {
		inner := s.clientUsage[clientID]
		if inner == nil {
			continue
		}
		delete(inner, agentID)
		if len(inner) == 0 {
			delete(s.clientUsage, clientID)
		}
	}
	delete(s.agentClientUsage, agentID)
	s.clientsMu.Unlock()
	// D1 (B3): drop the same agent's rows from the clients.Service mirror
	// so C1 can remove the server usage maps without leaving stale usage
	// for a deregistered agent visible through the mirror-backed HTTP
	// reads. Service.mu is acquired after Server.mu+clientsMu, preserving
	// the documented Server.mu -> Service.mu lock ordering.
	if s.clientsSvc != nil {
		s.clientsSvc.DropAgentUsageMirror(agentID)
	}
	s.revokedAgentIDs[agentID] = struct{}{}
	s.mu.Unlock()
}

func (s *Server) handleDeregisterAgent() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, user, err := s.requireSession(r)
		if err != nil {
			writeErrorLogged(r.Context(), w, http.StatusUnauthorized, "unauthorized", err)
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

// handleUpdateAgentFleetGroup reassigns an agent to a different fleet
// group. Mirrors handleRenameAgent: scope-checks BOTH the current group
// (caller must already manage it) AND the target group (caller must be
// allowed to add the agent to it). Persists via the storage layer,
// patches the in-memory snapshot, and audit-logs the move with old/new
// group ids so reassignments are reversible from the audit trail.
func (s *Server) handleUpdateAgentFleetGroup() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, user, err := s.requireSession(r)
		if err != nil {
			writeErrorLogged(r.Context(), w, http.StatusUnauthorized, "unauthorized", err)
			return
		}

		agentID := chi.URLParam(r, "id")
		if agentID == "" {
			writeError(w, http.StatusBadRequest, "missing agent id")
			return
		}

		var req updateAgentFleetGroupRequest
		if err := decodeJSON(r, &req); err != nil {
			writeErrorLogged(r.Context(), w, http.StatusBadRequest, "invalid request body", err)
			return
		}
		newGroupID := strings.TrimSpace(req.FleetGroupID)
		if newGroupID == "" {
			writeError(w, http.StatusBadRequest, "fleet_group_id is required")
			return
		}

		// Verify the agent exists and the caller is scoped over its
		// CURRENT group before any work — otherwise an out-of-scope
		// caller would learn the agent's existence via the 4xx code.
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
		// Operator must also be allowed to add agents to the TARGET
		// group; otherwise a partial-scope user could exfiltrate an
		// agent into a group they otherwise cannot manage.
		if !scope.IsAllowed(newGroupID) {
			writeError(w, http.StatusForbidden, "fleet group out of scope")
			return
		}

		if existing.FleetGroupID == newGroupID {
			// No-op: return the current shape so the client can
			// refresh without a special-cased "no change" branch.
			writeJSON(w, http.StatusOK, existing)
			return
		}

		// Reject targeting a non-existent group up front rather than
		// surfacing the storage-layer FK error to the operator.
		if s.store != nil {
			if _, err := s.store.GetFleetGroup(r.Context(), newGroupID); err != nil {
				if errors.Is(err, storage.ErrNotFound) {
					writeErrorLogged(r.Context(), w, http.StatusNotFound, "fleet group not found", err)
					return
				}
				s.logger.Error("get fleet group for reassign failed", "error", err)
				writeError(w, http.StatusInternalServerError, msgStorageError)
				return
			}
			if err := s.store.UpdateAgentFleetGroup(r.Context(), agentID, newGroupID); err != nil {
				if errors.Is(err, storage.ErrNotFound) {
					writeErrorLogged(r.Context(), w, http.StatusNotFound, msgAgentNotFound, err)
					return
				}
				s.logger.Error("update agent fleet group in store failed", "error", err)
				writeError(w, http.StatusInternalServerError, msgStorageError)
				return
			}
		}

		// Patch the in-memory snapshot so subsequent /agents and
		// /fleet-groups reads reflect the move without waiting for the
		// next heartbeat. Captures oldGroupID for the audit event.
		s.mu.Lock()
		current, stillExists := s.agents[agentID]
		if !stillExists {
			s.mu.Unlock()
			writeError(w, http.StatusNotFound, msgAgentNotFound)
			return
		}
		oldGroupID := current.FleetGroupID
		current.FleetGroupID = newGroupID
		s.agents[agentID] = current
		s.mu.Unlock()

		s.appendAuditWithContext(r.Context(), session.UserID, "agents.reassign_fleet_group", agentID, map[string]any{
			"old_fleet_group_id": oldGroupID,
			"new_fleet_group_id": newGroupID,
		})

		writeJSON(w, http.StatusOK, current)
	}
}

// Fleet-group HTTP handlers moved to http_fleet_groups.go as part of
// the groups redesign (UUID ids, CRUD + integrations). The previous
// handleFleetGroups derived the list from the live agent snapshot;
// the replacement reads the fleet_groups table so empty groups also
// appear in the list.
